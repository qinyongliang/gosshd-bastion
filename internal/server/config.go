package server

type Config struct {
	HTTPListen string
	SSHListen  string
	PublicHost string
	AgentToken string
	AgentPath  string
}

func (c Config) publicHost() string {
	if c.PublicHost != "" {
		return c.PublicHost
	}
	return "localhost"
}
