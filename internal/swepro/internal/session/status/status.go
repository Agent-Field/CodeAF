// Package status ports src/session/status.ts:1-99 from swe-pro commit
// 3b25a1a.
package status

import (
	"sync"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/bus"
)

type RetryAction struct {
	Reason   string  `json:"reason"`
	Provider string  `json:"provider"`
	Title    string  `json:"title"`
	Message  string  `json:"message"`
	Label    string  `json:"label"`
	Link     *string `json:"link,omitempty"`
}

type Info struct {
	Type    string       `json:"type"`
	Attempt int          `json:"attempt,omitempty"`
	Message string       `json:"message,omitempty"`
	Action  *RetryAction `json:"action,omitempty"`
	Next    int          `json:"next,omitempty"`
}

type StatusProperties struct {
	SessionID string `json:"sessionID"`
	Status    Info   `json:"status"`
}

type IdleProperties struct {
	SessionID string `json:"sessionID"`
}

var (
	StatusEvent = bus.Define("session.status", StatusProperties{})
	IdleEvent   = bus.Define("session.idle", IdleProperties{})
)

type Publisher interface {
	Publish(bus.Definition, any, ...bus.PublishOptions)
}

type Service struct {
	mu        sync.RWMutex
	state     map[string]Info
	publisher Publisher
}

func New(publisher Publisher) *Service {
	return &Service{state: make(map[string]Info), publisher: publisher}
}

func (service *Service) Get(sessionID string) Info {
	service.mu.RLock()
	defer service.mu.RUnlock()
	value, ok := service.state[sessionID]
	if !ok {
		return Info{Type: "idle"}
	}
	return value
}

func (service *Service) List() map[string]Info {
	service.mu.RLock()
	defer service.mu.RUnlock()
	out := make(map[string]Info, len(service.state))
	for key, value := range service.state {
		out[key] = value
	}
	return out
}

func (service *Service) Set(sessionID string, value Info) {
	// The source publishes before mutating its shared Map.
	if service.publisher != nil {
		service.publisher.Publish(StatusEvent, StatusProperties{
			SessionID: sessionID, Status: value,
		})
	}
	if value.Type == "idle" {
		if service.publisher != nil {
			service.publisher.Publish(IdleEvent, IdleProperties{SessionID: sessionID})
		}
		service.mu.Lock()
		delete(service.state, sessionID)
		service.mu.Unlock()
		return
	}
	service.mu.Lock()
	service.state[sessionID] = value
	service.mu.Unlock()
}

func Validate(value Info) bool {
	switch value.Type {
	case "idle", "busy":
		return true
	case "retry":
		return value.Attempt >= 0 && value.Next >= 0
	default:
		return false
	}
}
