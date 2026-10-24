package syncsetup

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/store"
	"github.com/Agent-Field/codeaf/internal/vaultsync"
)

// memoryKeys keeps immutable memory events in independent encrypted slots.
// Nothing edits a captured event, so different lines and offline edits survive
// the vault's union; the store resolves each line by its actual edit time.
type memoryKeys struct{ home string }

func (m memoryKeys) Values() (map[string]string, error) {
	events, err := store.SharedMemoryEventsAt(filepath.Join(m.home, "graph.db"))
	if err != nil {
		return nil, err
	}
	values := map[string]string{}
	for _, e := range events {
		raw, err := json.Marshal(e)
		if err != nil {
			return nil, err
		}
		hash := sha256.Sum256(raw)
		values[hex.EncodeToString(hash[:])] = string(raw)
	}
	return values, nil
}
func (m memoryKeys) Holds(string) bool { return true }
func (m memoryKeys) Apply(changes map[string]string) error {
	var events []store.LedgerMemoryEvent
	for _, raw := range changes {
		if raw == "" {
			continue
		}
		var e store.LedgerMemoryEvent
		if err := json.Unmarshal([]byte(raw), &e); err != nil {
			return err
		}
		events = append(events, e)
	}
	brain, err := store.Open(filepath.Join(m.home, "graph.db"))
	if err != nil {
		return err
	}
	defer brain.Close()
	return brain.ImportSharedMemoryEvents(events)
}
func sharedMemories(home string) vaultsync.Keyed {
	return vaultsync.Keys("memories", memoryKeys{home}, filepath.Join(home, "memory-sync.ledger"))
}
