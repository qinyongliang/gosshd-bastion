// Package types contains transport/configuration DTOs only. It must not depend
// on the server, Agent, persistence backend or forwarding data plane.
package types

import (
	"fmt"
	"net"
	"time"
)

type HTTPConfig struct {
	Servers []ServerBlock `json:"servers"`
}
type ServerBlock struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Hosts     []string   `json:"hosts"`
	Locations []Location `json:"locations"`
}
type Location struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Match        string            `json:"match"`
	Path         string            `json:"path"`
	ExitTargetID string            `json:"exit_target_id"`
	Scheme       string            `json:"scheme"`
	Host         string            `json:"host"`
	Port         int               `json:"port"`
	StripPrefix  bool              `json:"strip_prefix"`
	PreserveHost bool              `json:"preserve_host"`
	Headers      map[string]string `json:"headers,omitempty"`
}

func (l Location) Address() string { return net.JoinHostPort(l.Host, fmt.Sprint(l.Port)) }

type Event struct {
	Sequence         int64  `json:"sequence,omitempty"`
	ID               string `json:"id"`
	Time             int64  `json:"time"`
	SourceIP         string `json:"source_ip"`
	Method           string `json:"method"`
	Host             string `json:"host"`
	Path             string `json:"path"`
	ServerID         string `json:"server_id"`
	RouteID          string `json:"route_id"`
	Upstream         string `json:"upstream"`
	Status           int    `json:"status"`
	Up               int64  `json:"up"`
	Down             int64  `json:"down"`
	DurationMS       int64  `json:"duration_ms"`
	SourceConcurrent int64  `json:"source_concurrent"`
	DirectUp         int64  `json:"direct_up"`
	DirectDown       int64  `json:"direct_down"`
	Concurrent       int64  `json:"concurrent"`
	Direct           bool   `json:"direct"`
	Error            string `json:"error,omitempty"`
}
type LogFilter struct {
	Before   int64  `json:"before,omitempty"`
	From     int64  `json:"from,omitempty"`
	To       int64  `json:"to,omitempty"`
	Limit    int    `json:"limit,omitempty"`
	SourceIP string `json:"source_ip,omitempty"`
	Path     string `json:"path,omitempty"`
	RouteID  string `json:"route_id,omitempty"`
	Status   int    `json:"status,omitempty"`
}
type Stats struct {
	DirectUp      int64  `json:"direct_up"`
	DirectDown    int64  `json:"direct_down"`
	Requests      int64  `json:"requests"`
	Up            int64  `json:"up"`
	Down          int64  `json:"down"`
	Peak          int64  `json:"peak"`
	Errors        int64  `json:"errors"`
	Dropped       uint64 `json:"dropped"`
	WriteFailures uint64 `json:"write_failures"`
}
type SourceStats struct {
	Active   int64  `json:"active"`
	SourceIP string `json:"source_ip"`
	Stats
}
type RuntimeState struct {
	InflightDirectUp   int64            `json:"inflight_direct_up"`
	InflightDirectDown int64            `json:"inflight_direct_down"`
	InflightUp         int64            `json:"inflight_up"`
	InflightDown       int64            `json:"inflight_down"`
	Sources            map[string]int64 `json:"sources,omitempty"`
	Active             int64            `json:"active"`
	Peak               int64            `json:"peak"`
	Requests           int64            `json:"requests"`
	ListenAddress      string           `json:"listen_address"`
}

type TrafficBucket struct {
	Sequence int64 `json:"sequence"`
	SourceIP string
	Bucket   int64
	Stats
}

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
