package crewagent

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// Public crew threads contain a colon; the wave-1 agent persists only filename-safe thread names.
// The adapter preserves the contract name on every response and event without changing that store.
func storageThread(thread string) string {
	if !strings.HasPrefix(thread, "crew:") {
		return thread
	}
	hash := sha256.Sum256([]byte(thread))
	return "c_" + hex.EncodeToString(hash[:30])
}

type threadAlias struct{ topic, request string }

type threadHub struct {
	svc.Hub
	mu      sync.RWMutex
	aliases map[string]threadAlias
}

func (h *threadHub) alias(app, stored, public, request string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.aliases[bridge.TopicAgent(app, stored)] = threadAlias{topic: bridge.TopicAgent(app, public), request: request}
}
func (h *threadHub) Publish(topic string, data any) {
	h.mu.RLock()
	alias := h.aliases[topic]
	h.mu.RUnlock()
	if alias.topic != "" {
		topic = alias.topic
		if event, ok := data.(bridge.AgentEvent); ok && event.Kind == bridge.AgentShapeThis {
			event.Request = alias.request
			data = event
		}
	}
	h.Hub.Publish(topic, data)
}
