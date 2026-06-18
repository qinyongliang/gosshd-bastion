package bastion

import "github.com/qinyongliang/gosshd/internal/store"

type Service struct {
	repo *store.Repository
}

type Decision struct {
	Action string
	Reason string
}

func NewService(repo *store.Repository) *Service {
	return &Service{repo: repo}
}
