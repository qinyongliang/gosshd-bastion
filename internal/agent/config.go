package agent

import "github.com/qinyongliang/gosshd-bastion/internal/desktop"

type Config struct {
	Desktop         desktop.Config
	Server          string
	EnrollmentToken string
	IDFile          string
	Shell           string
	Root            string
	SSHHost         string
	SSHPort         string
	Version         string
}
