// Package forward contains the shared server/Agent forwarding data plane.
package forward

import (
	"errors"
	"fmt"
	"net"
	"regexp"
	"sort"
	"strings"
)

type compiledLocation struct {
	Location
	regex *regexp.Regexp
}
type compiledServer struct {
	ServerBlock
	locations []compiledLocation
	exact     map[string]*Location
	prefixes  []*compiledLocation
	regexes   []*compiledLocation
}
type hostWildcard struct {
	suffix string
	server *compiledServer
}
type Router struct {
	servers  []compiledServer
	exact    map[string]*compiledServer
	wildcard []hostWildcard
	fallback *compiledServer
}

func Compile(c *HTTPConfig) (*Router, error) {
	if c == nil || len(c.Servers) == 0 || len(c.Servers) > 128 {
		return nil, errors.New("HTTP requires 1–128 server blocks")
	}
	r := &Router{}
	ids := map[string]bool{}
	hosts := map[string]bool{}
	count := 0
	for _, s := range c.Servers {
		if len(s.Name) > 128 {
			return nil, errors.New("server name must be at most 128 characters")
		}
		if s.ID == "" || len(s.ID) > 128 || strings.ContainsAny(s.ID, "\r\n") || ids[s.ID] {
			return nil, errors.New("server/location IDs must be nonempty and unique")
		}
		ids[s.ID] = true
		if len(s.Hosts) == 0 || len(s.Hosts) > 128 || len(s.Locations) == 0 {
			return nil, errors.New("each server requires hosts and locations")
		}
		cs := compiledServer{ServerBlock: s}
		paths := map[string]bool{}
		for _, h := range s.Hosts {
			h = strings.ToLower(strings.TrimSpace(h))
			name := strings.TrimPrefix(h, "*.")
			if h == "" || len(h) > 253 || strings.ContainsAny(h, " /\\\t\r\n:") || (h != "_" && h != "*" && strings.Contains(name, "*")) {
				return nil, fmt.Errorf("invalid server host %q", h)
			}
			if h == "*" {
				h = "_"
			}
			if hosts[h] {
				return nil, fmt.Errorf("duplicate server host %q", h)
			}
			hosts[h] = true
		}
		for _, l := range s.Locations {
			count++
			if count > 1024 {
				return nil, errors.New("maximum 1024 locations")
			}
			if len(l.Name) > 128 {
				return nil, errors.New("location name must be at most 128 characters")
			}
			if l.ID == "" || len(l.ID) > 128 || strings.ContainsAny(l.ID, "\r\n") || ids[l.ID] {
				return nil, errors.New("server/location IDs must be nonempty and unique")
			}
			ids[l.ID] = true
			if l.Scheme != "http" && l.Scheme != "https" {
				return nil, errors.New("upstream scheme must be http or https")
			}
			if strings.TrimSpace(l.Host) == "" || len(l.Host) > 253 || strings.ContainsAny(l.Host, " /\\\t\r\n") || l.Port < 1 || l.Port > 65535 {
				return nil, errors.New("invalid upstream host or port")
			}
			ipHost, _, _ := strings.Cut(l.Host, "%")
			if strings.Contains(l.Host, ":") && net.ParseIP(ipHost) == nil {
				return nil, errors.New("upstream host must not include a port or IPv6 brackets")
			}
			if len(l.Path) == 0 || len(l.Path) > 4096 {
				return nil, errors.New("location path must contain 1–4096 characters")
			}
			modifier := l.Match
			if modifier == "" || modifier == "prefix" || modifier == "^~" {
				modifier = "prefix"
			}
			key := modifier + "\x00" + l.Path
			if paths[key] {
				return nil, errors.New("duplicate location match")
			}
			paths[key] = true
			cl := compiledLocation{Location: l}
			switch l.Match {
			case "", "prefix", "=", "^~":
				if !strings.HasPrefix(l.Path, "/") {
					return nil, errors.New("literal location must start with /")
				}
			case "~", "~*":
				pattern := l.Path
				if l.Match == "~*" {
					pattern = "(?i)" + pattern
				}
				var err error
				cl.regex, err = regexp.Compile(pattern)
				if err != nil {
					return nil, fmt.Errorf("location %s: %w", l.ID, err)
				}
				if l.StripPrefix {
					return nil, errors.New("strip_prefix is only supported for literal locations")
				}
			default:
				return nil, errors.New("location match must be prefix, =, ^~, ~ or ~*")
			}
			for k, v := range l.Headers {
				if !validHeader(k) || strings.ContainsAny(v, "\r\n\x00") {
					return nil, errors.New("invalid proxy header")
				}
				switch strings.ToLower(k) {
				case "connection", "upgrade", "transfer-encoding", "content-length", "host", "forwarded", "x-forwarded-for", "x-forwarded-host", "x-forwarded-proto":
					return nil, errors.New("hop-by-hop and forwarding headers cannot be overridden")
				}
			}
			cs.locations = append(cs.locations, cl)
		}
		r.servers = append(r.servers, cs)
	}
	r.exact = map[string]*compiledServer{}
	for i := range r.servers {
		s := &r.servers[i]
		s.exact = map[string]*Location{}
		for j := range s.locations {
			l := &s.locations[j]
			if l.regex != nil {
				s.regexes = append(s.regexes, l)
			} else if l.Match == "=" {
				s.exact[l.Path] = &l.Location
			} else {
				s.prefixes = append(s.prefixes, l)
			}
		}
		sort.SliceStable(s.prefixes, func(i, j int) bool { return len(s.prefixes[i].Path) > len(s.prefixes[j].Path) })
		for _, raw := range s.Hosts {
			h := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(raw), "."))
			if h == "_" || h == "*" {
				r.fallback = s
			} else if strings.HasPrefix(h, "*.") {
				r.wildcard = append(r.wildcard, hostWildcard{suffix: h[1:], server: s})
			} else {
				r.exact[h] = s
			}
		}
	}
	sort.SliceStable(r.wildcard, func(i, j int) bool { return len(r.wildcard[i].suffix) > len(r.wildcard[j].suffix) })
	return r, nil
}
func validHeader(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("!#$%&'*+-.^_\x60|~", c)) {
			return false
		}
	}
	return true
}
func (r *Router) Match(host, path string) (*Location, string) {
	if strings.Contains(host, ":") {
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
	}
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	selected := r.exact[host]
	if selected == nil {
		for _, w := range r.wildcard {
			if strings.HasSuffix(host, w.suffix) && len(host) > len(w.suffix) {
				selected = w.server
				break
			}
		}
	}
	if selected == nil {
		selected = r.fallback
	}
	if selected == nil {
		return nil, ""
	}
	if l := selected.exact[path]; l != nil {
		return l, selected.ID
	}
	var prefix *compiledLocation
	for _, l := range selected.prefixes {
		if strings.HasPrefix(path, l.Path) {
			prefix = l
			break
		}
	}
	if prefix != nil && prefix.Match == "^~" {
		return &prefix.Location, selected.ID
	}
	for _, l := range selected.regexes {
		if l.regex.MatchString(path) {
			return &l.Location, selected.ID
		}
	}
	if prefix != nil {
		return &prefix.Location, selected.ID
	}
	return nil, selected.ID
}
func SourceIP(address string) string {
	h, _, err := net.SplitHostPort(address)
	if err == nil {
		return h
	}
	return address
}
