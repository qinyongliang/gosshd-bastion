package agent

type Config struct {
	Server          string
	EnrollmentToken string
	IDFile          string
	Shell           string
	Root            string
	SSHHost         string
	SSHPort         string
	Version         string
}
