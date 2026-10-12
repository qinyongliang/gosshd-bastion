package agent

import "github.com/qinyongliang/gosshd-bastion/internal/protocol"

type edgeSnapshot struct {
	Configs       []protocol.ForwardDeployment `json:"configs"`
	RetentionDays int                          `json:"retention_days"`
}
