package server

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/yamux"
	"github.com/qinyongliang/gosshd-bastion/internal/protocol"
	"github.com/qinyongliang/gosshd-bastion/internal/tunnel"
)

func (m *tunnelManager) forwardPeer(r *tunnelRun, entry *yamux.Stream, reader *bufio.Reader) {
	if !r.track(entry) {
		return
	}
	defer r.untrack(entry)
	exit, err := r.peerExit.session.OpenStream()
	if err != nil {
		_ = protocol.WriteJSONLine(entry, protocol.StreamResponse{Error: err.Error()})
		return
	}
	if !r.track(exit) {
		return
	}
	defer r.untrack(exit)
	defer exit.Close()
	_ = exit.SetDeadline(time.Now().Add(10 * time.Second))
	servers := []string{}
	for _, s := range strings.Split(m.app.cfg.TunnelSTUNServers, ",") {
		if s = strings.TrimSpace(s); s != "" {
			servers = append(servers, s)
		}
	}
	err = protocol.WriteJSONLine(exit, protocol.StreamRequest{Type: protocol.StreamTunnelPeer, Target: net.JoinHostPort(r.config.DestinationHost, strconv.Itoa(r.config.DestinationPort)), TunnelHops: r.peerExit.hops, STUNServers: servers})
	if err != nil {
		return
	}
	exitReader := bufio.NewReader(exit)
	response, e := protocol.ReadJSONLine[protocol.StreamResponse](exitReader)
	if e != nil {
		err = e
	} else if !response.OK {
		err = errors.New(response.Error)
	}
	if err != nil {
		_ = protocol.WriteJSONLine(entry, protocol.StreamResponse{Error: err.Error()})
		return
	}
	_ = exit.SetDeadline(time.Time{})
	if protocol.WriteJSONLine(entry, protocol.StreamResponse{OK: true, Peer: true, STUNServers: servers}) != nil {
		return
	}
	_ = entry.SetDeadline(time.Time{})
	r.metrics.opened()
	pathID := uuid.NewString()
	r.metrics.mu.Lock()
	r.metrics.paths[pathID] = tunnelConnectionPath{ID: pathID, EntryAgentID: r.entryAgent, ExitAgentID: r.peerExit.id, UpdatedAt: time.Now().UTC()}
	r.metrics.mu.Unlock()
	defer func() {
		r.metrics.mu.Lock()
		delete(r.metrics.paths, pathID)
		r.metrics.mu.Unlock()
		r.metrics.closed()
	}()
	a := &tunnel.Relay{Reader: reader, Writer: entry, Closer: entry}
	b := &tunnel.Relay{Reader: exitReader, Writer: exit, Closer: exit}
	var wg sync.WaitGroup
	wg.Add(2)
	var mu sync.Mutex
	pending := [2]map[uint64]int64{map[uint64]int64{}, map[uint64]int64{}}
	var directUp, directDown uint64
	wasDirect := false
	entryDone := make(chan struct{})
	defer func() {
		if wasDirect {
			r.metrics.mu.Lock()
			r.metrics.direct--
			r.metrics.mu.Unlock()
		}
	}()
	pump := func(source, destination *tunnel.Relay, direction int) {
		defer wg.Done()
		defer source.Close()
		defer func() {
			if direction == 0 {
				close(entryDone)
			} else {
				timer := time.NewTimer(2 * time.Second)
				defer timer.Stop()
				select {
				case <-entryDone:
				case <-r.ctx.Done():
				case <-timer.C:
				}
			}
			destination.Close()
		}()
		for {
			packet, err := source.Read()
			if err != nil {
				return
			}
			mu.Lock()
			switch packet.Kind {
			case tunnel.Data:
				if len(packet.Body) > tunnel.MaxPayload || len(pending[direction]) >= 128 {
					mu.Unlock()
					return
				}
				pending[direction][packet.Seq] = int64(len(packet.Body))
			case tunnel.Ack:
				if n, ok := pending[1-direction][packet.Seq]; ok {
					if len(packet.Body) == 1 && packet.Body[0] == 0 {
						r.metrics.bytes(direction == 1, false, n)
					}
					delete(pending[1-direction], packet.Seq)
				}
			case tunnel.PathInfo:
				var info tunnel.PeerInfo
				if len(packet.Body) <= 4096 && json.Unmarshal(packet.Body, &info) == nil {
					r.metrics.mu.Lock()
					path := r.metrics.paths[pathID]
					if direction == 0 {
						path.Entry = &info
					} else {
						path.Exit = &info
					}
					path.UpdatedAt = time.Now().UTC()
					r.metrics.paths[pathID] = path
					r.metrics.mu.Unlock()
				}
			case tunnel.Stats:
				if direction == 0 && len(packet.Body) == 17 {
					up, down := binary.BigEndian.Uint64(packet.Body), binary.BigEndian.Uint64(packet.Body[8:])
					if up >= directUp && down >= directDown {
						r.metrics.bytes(true, true, int64(up-directUp))
						r.metrics.bytes(false, true, int64(down-directDown))
						directUp, directDown = up, down
					}
					active := packet.Body[16] == 1
					if active != wasDirect {
						r.metrics.mu.Lock()
						if active {
							r.metrics.direct++
						} else {
							r.metrics.direct--
						}
						r.metrics.mu.Unlock()
						wasDirect = active
					}
				}
			}
			mu.Unlock()
			if packet.Kind == tunnel.Stats || packet.Kind == tunnel.PathInfo {
				continue
			}
			if err = destination.Send(packet); err != nil {
				return
			}
		}
	}
	go pump(a, b, 0)
	go pump(b, a, 1)
	wg.Wait()
}
