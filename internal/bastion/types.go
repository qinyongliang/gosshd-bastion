package bastion

import "github.com/qinyongliang/gosshd/internal/store"

type Service struct {
	repo *store.Repository
}

func NewService(repo *store.Repository) *Service {
	return &Service{repo: repo}
}
