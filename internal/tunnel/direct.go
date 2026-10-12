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

type signalMessage struct {
	Type       string                   `json:"type"`
	SDP        string                   `json:"sdp,omitempty"`
	Candidate  *webrtc.ICECandidateInit `json:"candidate,omitempty"`
	Generation uint64                   `json:"generation,omitempty"`
}

const maxPendingCandidates = 64

type Negotiator struct {
	infoMu     sync.Mutex
	infoKey    string
	infoLocal  CandidateInfo
	conn       *Conn
	initiator  bool
	servers    []string
	messages   chan []byte
	stop       chan struct{}
	once       sync.Once
	mu         sync.Mutex
	peer       *webrtc.PeerConnection
	generation uint64
	pending    map[uint64][]webrtc.ICECandidateInit
}

func NewNegotiator(conn *Conn, initiator bool, servers []string) *Negotiator {
	n := &Negotiator{conn: conn, initiator: initiator, servers: servers, messages: make(chan []byte, 8), stop: make(chan struct{}), pending: map[uint64][]webrtc.ICECandidateInit{}}
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
			n.conn.DisableDirect()
			_ = pc.Close()
		}
	})
}
func (n *Negotiator) newPeer(generations ...uint64) (*webrtc.PeerConnection, error) {
	settings := webrtc.SettingEngine{}
	// Browsers may advertise LAN candidates using mDNS hostnames.
	settings.SetICEMulticastDNSMode(ice.MulticastDNSModeQueryOnly)
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
	generation := n.generation
	if len(generations) > 0 {
		generation = generations[0]
	}
	n.peer, n.generation = pc, generation
	n.mu.Unlock()
	queued := n.pending[generation]
	n.pending = map[uint64][]webrtc.ICECandidateInit{generation: queued}
	if old != nil {
		n.conn.DisableDirect()
		_ = old.Close()
	}
	pc.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		if candidate == nil {
			return
		}
		n.mu.Lock()
		alive := n.peer == pc && n.generation == generation
		n.mu.Unlock()
		if !alive {
			return
		}
		select {
		case <-n.stop:
			return
		case <-n.conn.done:
			return
		default:
		}
		value := candidate.ToJSON()
		_ = n.signal(signalMessage{Type: "candidate", Candidate: &value, Generation: generation})
	})
	pc.OnDataChannel(func(dc *webrtc.DataChannel) { n.attach(pc, dc) })
	return pc, nil
}
func (n *Negotiator) attach(pc *webrtc.PeerConnection, dc *webrtc.DataChannel) {
	dc.OnMessage(func(msg webrtc.DataChannelMessage) {
		n.mu.Lock()
		alive := n.peer == pc
		n.mu.Unlock()
		if !alive {
			return
		}
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
	disable := func() {
		n.mu.Lock()
		alive := n.peer == pc
		n.mu.Unlock()
		if alive {
			n.conn.DisableDirect()
		}
	}
	dc.OnClose(disable)
	dc.OnError(func(error) { disable() })
}
func (n *Negotiator) signal(message signalMessage) error {
	b, err := json.Marshal(message)
	if err != nil {
		return err
	}
	return n.conn.relay.Send(Packet{Kind: Signal, Body: b})
}

func (n *Negotiator) description(pc *webrtc.PeerConnection, description webrtc.SessionDescription) error {
	select {
	case <-n.stop:
		return errors.New("direct negotiation closed")
	case <-n.conn.done:
		return errors.New("tunnel closed")
	default:
	}
	if err := pc.SetLocalDescription(description); err != nil {
		return err
	}
	n.mu.Lock()
	generation, alive := n.generation, n.peer == pc
	n.mu.Unlock()
	if !alive {
		return errors.New("direct negotiation replaced")
	}
	// Send SDP immediately. Candidate discovery is incremental, never a startup gate.
	return n.signal(signalMessage{Type: description.Type.String(), SDP: description.SDP, Generation: generation})
}

func (n *Negotiator) candidate(message signalMessage) {
	if message.Candidate == nil {
		return
	}
	n.mu.Lock()
	pc, generation := n.peer, n.generation
	n.mu.Unlock()
	if message.Generation < generation {
		return
	}
	if message.Generation == generation && pc != nil && pc.RemoteDescription() != nil {
		_ = pc.AddICECandidate(*message.Candidate)
		return
	}
	// Candidates may overtake SDP; retain only a bounded set until its description arrives.
	count := 0
	for _, candidates := range n.pending {
		count += len(candidates)
	}
	if count < maxPendingCandidates {
		n.pending[message.Generation] = append(n.pending[message.Generation], *message.Candidate)
	}
}

func (n *Negotiator) remote(pc *webrtc.PeerConnection, description webrtc.SessionDescription) error {
	if err := pc.SetRemoteDescription(description); err != nil {
		return err
	}
	n.mu.Lock()
	generation := n.generation
	n.mu.Unlock()
	for _, candidate := range n.pending[generation] {
		_ = pc.AddICECandidate(candidate)
	}
	delete(n.pending, generation)
	return nil
}

func (n *Negotiator) offer() {
	n.mu.Lock()
	generation := n.generation + 1
	n.mu.Unlock()
	pc, err := n.newPeer(generation)
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
			var message signalMessage
			if json.Unmarshal(b, &message) != nil {
				continue
			}
			if message.Type == "candidate" {
				n.candidate(message)
				continue
			}
			if message.Type == "offer" && !n.initiator {
				pc, err := n.newPeer(message.Generation)
				if err != nil {
					continue
				}
				if n.remote(pc, webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: message.SDP}) != nil {
					continue
				}
				answer, err := pc.CreateAnswer(nil)
				if err == nil {
					_ = n.description(pc, answer)
				}
			} else if message.Type == "answer" && n.initiator {
				n.mu.Lock()
				pc, generation := n.peer, n.generation
				n.mu.Unlock()
				// Legacy full-SDP peers do not include a generation field.
				if pc != nil && (message.Generation == 0 || message.Generation == generation) {
					_ = n.remote(pc, webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: message.SDP})
				}
			}
		}
	}
}
