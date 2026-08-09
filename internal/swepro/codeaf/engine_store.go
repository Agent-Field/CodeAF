package codeaf

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/msgmodel"
)

// turnStore is the in-memory fallback for direct engine tests. The shipped
// codeaf runtime supplies its run-scoped durable session store instead.
type turnStore struct {
	mu    sync.Mutex
	order []string
	infos map[string]msgmodel.Info
	parts map[string][]msgmodel.Part
}

func newTurnStore() *turnStore {
	return &turnStore{
		infos: map[string]msgmodel.Info{},
		parts: map[string][]msgmodel.Part{},
	}
}

func (store *turnStore) Messages(
	_ context.Context, sessionID string,
) ([]msgmodel.WithParts, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	out := make([]msgmodel.WithParts, 0, len(store.order))
	for _, id := range store.order {
		info := store.infos[id]
		if info == nil {
			continue
		}
		withParts := msgmodel.WithParts{Info: info, Parts: store.parts[id]}
		if sessionID != "" {
			switch typed := info.(type) {
			case msgmodel.User:
				if typed.SessionID != sessionID {
					continue
				}
			case msgmodel.Assistant:
				if typed.SessionID != sessionID {
					continue
				}
			}
		}
		copied, err := copyTurnMessage(withParts)
		if err != nil {
			return nil, fmt.Errorf("codeaf turn store: copy %s: %w", id, err)
		}
		out = append(out, copied)
	}
	return out, nil
}

func (store *turnStore) UpdateMessage(_ context.Context, info msgmodel.Info) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	id := info.MessageID()
	if _, exists := store.infos[id]; !exists {
		store.order = append(store.order, id)
	}
	store.infos[id] = info
	return nil
}

func (store *turnStore) UpdatePart(_ context.Context, part msgmodel.Part) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	base := part.Base()
	parts := store.parts[base.MessageID]
	for index := range parts {
		if parts[index].Base().ID == base.ID {
			parts[index] = part
			store.parts[base.MessageID] = parts
			return nil
		}
	}
	store.parts[base.MessageID] = append(parts, part)
	return nil
}

func copyTurnMessage(input msgmodel.WithParts) (msgmodel.WithParts, error) {
	if input.Parts == nil {
		input.Parts = msgmodel.Parts{}
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return msgmodel.WithParts{}, err
	}
	var output msgmodel.WithParts
	if err := json.Unmarshal(raw, &output); err != nil {
		return msgmodel.WithParts{}, err
	}
	return output, nil
}
