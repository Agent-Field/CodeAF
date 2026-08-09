// Package todo ports src/session/todo.ts:1-87 from swe-pro commit 3b25a1a.
package todo

import (
	"sync"

	"github.com/Agent-Field/swe-pro-go/internal/bus"
)

type Info struct {
	Content  string `json:"content"`
	Status   string `json:"status"`
	Priority string `json:"priority"`
}

type UpdateInput struct {
	SessionID string `json:"sessionID"`
	Todos     []Info `json:"todos"`
}

var UpdatedEvent = bus.Define("todo.updated", UpdateInput{})

type Publisher interface {
	Publish(bus.Definition, any, ...bus.PublishOptions)
}

// Repository is the transactional delete-then-insert and ordered select seam
// around TodoTable.
type Repository interface {
	Replace(sessionID string, todos []Info) error
	Get(sessionID string) ([]Info, error)
}

type Service struct {
	repository Repository
	publisher  Publisher
}

func New(repository Repository, publisher Publisher) *Service {
	return &Service{repository: repository, publisher: publisher}
}

func (service *Service) Update(input UpdateInput) error {
	if err := service.repository.Replace(input.SessionID, input.Todos); err != nil {
		return err
	}
	if service.publisher != nil {
		service.publisher.Publish(UpdatedEvent, input)
	}
	return nil
}

func (service *Service) Get(sessionID string) ([]Info, error) {
	return service.repository.Get(sessionID)
}

// MemoryRepository is a transactionally replaced, position-preserving
// repository useful to non-SQL hosts.
type MemoryRepository struct {
	mu    sync.RWMutex
	state map[string][]Info
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{state: make(map[string][]Info)}
}

func (repository *MemoryRepository) Replace(sessionID string, todos []Info) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if len(todos) == 0 {
		delete(repository.state, sessionID)
		return nil
	}
	repository.state[sessionID] = append([]Info(nil), todos...)
	return nil
}

func (repository *MemoryRepository) Get(sessionID string) ([]Info, error) {
	repository.mu.RLock()
	defer repository.mu.RUnlock()
	return append([]Info(nil), repository.state[sessionID]...), nil
}
