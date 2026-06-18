package bastion

import "github.com/qinyongliang/gosshd-bastion/internal/store"

type Service struct {
	repo      *store.Repository
	llmClient *LLMClient
}

type Decision struct {
	Action string
	Reason string
}

func NewService(repo *store.Repository) *Service {
	return &Service{repo: repo, llmClient: NewLLMClient()}
}
