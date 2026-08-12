package server

import "strings"

const DefaultVersion = "dev"

type Config struct {
	ClientMode             bool
	HTTPListen             string
	SSHListen              string
	HostKeyPath            string
	DatabasePath           string
	AuditDatabasePath      string
	AuditRecordingPath     string
	SecretKey              string
	SecretKeyPath          string
	BootstrapAdminPassword string
	SessionCookieName      string
	PublicHost             string
	PublicSSHPort          int
	KnownHostsPath         string
	AgentPath              string
	AgentCachePath         string
	Version                string
	ReleaseBaseURL         string
	ReleaseProxyURL        string
}

func (c Config) publicHost() string {
	return c.PublicHost
}

func (c Config) version() string {
	if c.Version != "" {
		return c.Version
	}
	return DefaultVersion
}

func (c Config) releaseBaseURL() string {
	if c.ReleaseBaseURL != "" {
		return c.ReleaseBaseURL
	}
	return "https://github.com/qinyongliang/gosshd-bastion/releases/download"
}

func (c Config) releaseProxyURLs() []string {
	proxies := strings.Split(c.ReleaseProxyURL, ",")
	proxies = append(proxies,
		"https://ghfast.top/",
		"https://fastgit.cc/",
		"https://gh.dpik.top/",
		"https://github.tbap.top/",
		"https://cdn.gh-proxy.com/",
	)
	seen := map[string]bool{}
	result := make([]string, 0, len(proxies))
	for _, proxy := range proxies {
		proxy = strings.TrimRight(strings.TrimSpace(proxy), "/")
		if proxy != "" && !seen[proxy] {
			seen[proxy] = true
			result = append(result, proxy)
		}
	}
	return result
}
