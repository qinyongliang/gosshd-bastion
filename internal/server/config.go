package server

const DefaultVersion = "dev"

type Config struct {
	HTTPListen        string
	SSHListen         string
	HostKeyPath       string
	DatabasePath      string
	SecretKey         string
	SecretKeyPath     string
	SessionCookieName string
	PublicHost        string
	PublicSSHHost     string
	PublicSSHPort     string
	AgentToken        string
	AgentPath         string
	AgentCachePath    string
	Version           string
	ReleaseBaseURL    string
	ReleaseProxyURL   string
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
	return "https://github.com/qinyongliang/gosshd/releases/download"
}

func (c Config) releaseProxyURL() string {
	if c.ReleaseProxyURL != "" {
		return c.ReleaseProxyURL
	}
	return "https://gh-proxy.com/"
}
