package tunnel

import (
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/pion/ice/v4"
	"github.com/pion/webrtc/v4"
)

// WebRTC supplies authenticated, encrypted ICE connectivity and UDP hole punching.
// The stable Conn handles path migration independently of the TCP connection.
type rtcPath struct{ channel *webrtc.DataChannel }

func (p *rtcPath) Send(packet Packet) error {
	if p.channel.ReadyState() != webrtc.DataChannelStateOpen || p.channel.BufferedAmount() > 1024*1024 {
		return errors.New("direct path unavailable")
	}
	return p.channel.Send(packet.Bytes())
}
func (p *rtcPath) Close() error { return p.channel.Close() }

type Negotiator struct {
	infoMu    sync.Mutex
	infoKey   string
	infoLocal CandidateInfo
	conn      *Conn
	initiator bool
	servers   []string
	messages  chan []byte
	stop      chan struct{}
	once      sync.Once
	mu        sync.Mutex
	peer      *webrtc.PeerConnection
}

func NewNegotiator(conn *Conn, initiator bool, servers []string) *Negotiator {
	n := &Negotiator{conn: conn, initiator: initiator, servers: servers, messages: make(chan []byte, 8), stop: make(chan struct{})}
	conn.SignalHandler = func(b []byte) {
		select {
		case n.messages <- append([]byte(nil), b...):
		case <-n.stop:
		}
	}
	return n
}
func (n *Negotiator) Close() {
	n.once.Do(func() {
		close(n.stop)
		n.mu.Lock()
		pc := n.peer
		n.peer = nil
		n.mu.Unlock()
		if pc != nil {
			_ = pc.Close()
		}
	})
}
func (n *Negotiator) newPeer() (*webrtc.PeerConnection, error) {
	settings := webrtc.SettingEngine{}
	settings.SetICEMulticastDNSMode(ice.MulticastDNSModeDisabled)
	settings.SetICETimeouts(3*time.Second, 10*time.Second, time.Second)
	api := webrtc.NewAPI(webrtc.WithSettingEngine(settings))
	config := webrtc.Configuration{}
	if len(n.servers) > 0 {
		config.ICEServers = []webrtc.ICEServer{{URLs: n.servers}}
	}
	pc, err := api.NewPeerConnection(config)
	if err != nil {
		return nil, err
	}
	n.mu.Lock()
	old := n.peer
	n.peer = pc
	n.mu.Unlock()
	if old != nil {
		_ = old.Close()
	}
	pc.OnDataChannel(func(dc *webrtc.DataChannel) { n.attach(pc, dc) })
	return pc, nil
}
func (n *Negotiator) attach(pc *webrtc.PeerConnection, dc *webrtc.DataChannel) {
	dc.OnMessage(func(msg webrtc.DataChannelMessage) {
		packet, err := Parse(msg.Data)
		if err == nil {
			n.conn.receive(packet, true)
		}
	})
	dc.OnOpen(func() {
		n.mu.Lock()
		alive := n.peer == pc
		n.mu.Unlock()
		if alive {
			n.conn.SetDirect(&rtcPath{dc})
		}
	})
	dc.OnClose(n.conn.DisableDirect)
	dc.OnError(func(error) { n.conn.DisableDirect() })
}
func (n *Negotiator) description(pc *webrtc.PeerConnection, description webrtc.SessionDescription) error {
	gathering := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(description); err != nil {
		return err
	}
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case <-gathering:
	case <-timer.C:
	case <-n.stop:
		return errors.New("direct negotiation closed")
	case <-n.conn.done:
		return errors.New("tunnel closed")
	}
	b, err := json.Marshal(pc.LocalDescription())
	if err != nil {
		return err
	}
	return n.conn.relay.Send(Packet{Kind: Signal, Body: b})
}
func (n *Negotiator) offer() {
	pc, err := n.newPeer()
	if err != nil {
		return
	}
	dc, err := pc.CreateDataChannel("tcp-tunnel", nil)
	if err != nil {
		return
	}
	n.attach(pc, dc)
	sdp, err := pc.CreateOffer(nil)
	if err == nil {
		_ = n.description(pc, sdp)
	}
}
func (n *Negotiator) Run() {
	defer n.Close()
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	if n.initiator {
		n.offer()
	}
	for {
		select {
		case <-n.stop:
			return
		case <-n.conn.done:
			return
		case <-ticker.C:
			if n.initiator && !n.conn.Direct() {
				n.offer()
			}
		case b := <-n.messages:
			var description webrtc.SessionDescription
			if json.Unmarshal(b, &description) != nil {
				continue
			}
			if description.Type == webrtc.SDPTypeOffer && !n.initiator {
				pc, err := n.newPeer()
				if err != nil {
					continue
				}
				if pc.SetRemoteDescription(description) != nil {
					continue
				}
				answer, err := pc.CreateAnswer(nil)
				if err == nil {
					_ = n.description(pc, answer)
				}
			} else if description.Type == webrtc.SDPTypeAnswer && n.initiator {
				n.mu.Lock()
				pc := n.peer
				n.mu.Unlock()
				if pc != nil {
					_ = pc.SetRemoteDescription(description)
				}
			}
		}
	}
}
