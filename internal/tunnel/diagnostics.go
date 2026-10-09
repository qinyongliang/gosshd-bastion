package tunnel

import (
	"encoding/json"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/pion/webrtc/v4"
)

// PeerInfo describes the selected ICE route, not all advertised interfaces.
type CandidateInfo struct {
	Address      string `json:"address"`
	Port         uint16 `json:"port"`
	Protocol     string `json:"protocol"`
	Type         string `json:"type"`
	Network      string `json:"network"`
	Interface    string `json:"interface,omitempty"`
	LocalAddress string `json:"local_address,omitempty"`
	LocalPort    uint16 `json:"local_port,omitempty"`
}
type PeerInfo struct {
	Active          bool           `json:"active"`
	State           string         `json:"state"`
	Local           *CandidateInfo `json:"local,omitempty"`
	Remote          *CandidateInfo `json:"remote,omitempty"`
	RTTMilliseconds float64        `json:"rtt_ms,omitempty"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

func interfaceForAddress(address string) string {
	ip := net.ParseIP(strings.Split(address, "%")[0])
	if ip == nil {
		return ""
	}
	interfaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range interfaces {
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			host, _, err := net.ParseCIDR(addr.String())
			if err == nil && host.Equal(ip) {
				return iface.Name
			}
		}
	}
	return ""
}
func candidateInfo(candidate *webrtc.ICECandidate, local bool) CandidateInfo {
	info := CandidateInfo{Address: candidate.Address, Port: candidate.Port, Protocol: candidate.Protocol.String(), Type: candidate.Typ.String(), Network: "IPv4"}
	if ip := net.ParseIP(candidate.Address); ip != nil && ip.To4() == nil {
		info.Network = "IPv6"
	}
	if local {
		info.LocalAddress = candidate.Address
		info.LocalPort = candidate.Port
		if ip := net.ParseIP(candidate.RelatedAddress); ip != nil && !ip.IsUnspecified() {
			info.LocalAddress = candidate.RelatedAddress
			info.LocalPort = candidate.RelatedPort
		}
		info.Interface = interfaceForAddress(info.LocalAddress)
	}
	return info
}
func (n *Negotiator) Snapshot() PeerInfo {
	info := PeerInfo{Active: n.conn.Direct(), State: "new", UpdatedAt: time.Now().UTC()}
	n.mu.Lock()
	pc := n.peer
	n.mu.Unlock()
	if pc == nil {
		return info
	}
	info.State = pc.ConnectionState().String()
	sctp := pc.SCTP()
	if sctp == nil || sctp.Transport() == nil {
		return info
	}
	transport := sctp.Transport().ICETransport()
	if transport == nil {
		return info
	}
	pair, err := transport.GetSelectedCandidatePair()
	if err != nil || pair == nil || pair.Local == nil || pair.Remote == nil {
		return info
	}
	key := pair.Local.Address + ":" + strconv.Itoa(int(pair.Local.Port)) + ":" + pair.Local.RelatedAddress
	n.infoMu.Lock()
	if key != n.infoKey {
		n.infoLocal = candidateInfo(pair.Local, true)
		n.infoKey = key
	}
	local := n.infoLocal
	n.infoMu.Unlock()
	remote := candidateInfo(pair.Remote, false)
	info.Local = &local
	info.Remote = &remote
	if stats, ok := transport.GetSelectedCandidatePairStats(); ok {
		info.RTTMilliseconds = stats.CurrentRoundTripTime * 1000
	}
	return info
}
func (n *Negotiator) Report() error {
	b, err := json.Marshal(n.Snapshot())
	if err != nil {
		return err
	}
	return n.conn.relay.Send(Packet{Kind: PathInfo, Body: b})
}
