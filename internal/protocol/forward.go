package protocol

import protocoltypes "github.com/qinyongliang/gosshd-bastion/internal/protocol/types"

const (
	StreamForwardSync = "forward-sync"
	StreamForwardLink = "forward-link"
	StreamForwardPeer = "forward-peer"
)

type ForwardRoute struct {
	Network string         `json:"network"`
	ID      string         `json:"id"`
	Address string         `json:"address"`
	Hops    []TunnelSSHHop `json:"hops,omitempty"`
	LinkID  string         `json:"link_id,omitempty"`
}
type ForwardDeployment struct {
	ID            string                    `json:"id"`
	Revision      int64                     `json:"revision"`
	Protocol      string                    `json:"protocol"`
	ListenAddress string                    `json:"listen_address"`
	ListenHops    []TunnelSSHHop            `json:"listen_hops,omitempty"`
	HTTP          *protocoltypes.HTTPConfig `json:"http,omitempty"`
	Transports    []string                  `json:"transports,omitempty"`
	Destination   *protocoltypes.Location   `json:"destination,omitempty"`
	Routes        []ForwardRoute            `json:"routes"`
	ExpiresAt     int64                     `json:"expires_at,omitempty"`
}
type ForwardSync struct {
	ReportOnly       bool                `json:"report_only,omitempty"`
	DesiredRevisions map[string]int64    `json:"desired_revisions,omitempty"`
	Configs          []ForwardDeployment `json:"configs"`
	KnownIDs         []string            `json:"known_ids"`
	RetentionDays    int                 `json:"retention_days"`
	TrafficAcks      map[string]int64    `json:"traffic_acks,omitempty"`
	Acks             map[string]int64    `json:"acks,omitempty"`
}
type ForwardReport struct {
	Traffic  []protocoltypes.TrafficBucket     `json:"traffic,omitempty"`
	Paths    map[string]protocoltypes.PeerInfo `json:"paths,omitempty"`
	ID       string                            `json:"id"`
	Revision int64                             `json:"revision"`
	Status   string                            `json:"status"`
	Error    string                            `json:"error,omitempty"`
	State    protocoltypes.RuntimeState        `json:"state"`
	Stats    protocoltypes.Stats               `json:"stats"`
	Links    map[string]string                 `json:"links"`
	Events   []protocoltypes.Event             `json:"events,omitempty"`
}
type ForwardSyncResponse struct {
	More    bool            `json:"more,omitempty"`
	OK      bool            `json:"ok"`
	Error   string          `json:"error,omitempty"`
	Reports []ForwardReport `json:"reports"`
}
type ForwardPeer struct {
	LinkID    string         `json:"link_id"`
	ConfigID  string         `json:"config_id"`
	Revision  int64          `json:"revision"`
	Routes    []ForwardRoute `json:"routes"`
	ExpiresAt int64          `json:"expires_at,omitempty"`
}
