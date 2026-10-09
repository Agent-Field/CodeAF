package desktopbridge

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/placegraph"
	"github.com/Agent-Field/codeaf/internal/workspacestore"
)

type conversationTarget [sha256.Size]byte
type workspaceConversationPane struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	SessionFile string `json:"sessionFile"`
}

func conversationPanes(doc json.RawMessage) []workspaceConversationPane {
	var state struct {
		Tabs []struct {
			workspaceConversationPane
			Split *struct {
				Panes []workspaceConversationPane `json:"panes"`
			} `json:"split"`
		} `json:"tabs"`
	}
	if json.Unmarshal(doc, &state) != nil {
		return nil
	}
	var panes []workspaceConversationPane
	for _, tab := range state.Tabs {
		if tab.Split != nil {
			panes = append(panes, tab.Split.Panes...)
		} else {
			panes = append(panes, tab.workspaceConversationPane)
		}
	}
	return panes
}
func durableConversation(p workspaceConversationPane) (conversationTarget, bool) {
	if p.Kind != "conversation" || strings.TrimSpace(p.SessionFile) == "" {
		return conversationTarget{}, false
	}
	// This is an identity comparison, never a filesystem read. Keeping the whole
	// cleaned target avoids same-basename chats in distinct projects colliding.
	return sha256.Sum256([]byte(filepath.Clean(p.SessionFile))), true
}

type workspaceOpenEntry struct {
	signature workspacestore.FileSignature
	checked   time.Time
	targets   []conversationTarget
}
type workspaceOpenIndex struct {
	mu      sync.Mutex
	store   *workspacestore.Store
	entries map[string]workspaceOpenEntry
	// A narrow read seam proves that unchanged files do not repeatedly decode
	// their drafts; production always uses the store's bounded, validated Get.
	read func(string) (workspacestore.Record, error)
}

func (b *Bridge) workspaceOpenCache(store *workspacestore.Store) *workspaceOpenIndex {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.openElsewhere == nil || b.openElsewhere.store != store {
		b.openElsewhere = &workspaceOpenIndex{store: store, entries: map[string]workspaceOpenEntry{}, read: store.Get}
	}
	return b.openElsewhere
}
func (index *workspaceOpenIndex) contains(key string, target conversationTarget, now time.Time) bool {
	signature, err := index.store.Signature(key)
	if err != nil {
		index.mu.Lock()
		delete(index.entries, key)
		index.mu.Unlock()
		return false
	}
	index.mu.Lock()
	defer index.mu.Unlock()
	entry, ok := index.entries[key]
	// Periodic verification handles an external editor preserving timestamps.
	if !ok || entry.signature != signature || now.Sub(entry.checked) >= 30*time.Second {
		entry = workspaceOpenEntry{signature: signature, checked: now}
		if signature != (workspacestore.FileSignature{}) {
			rec, err := index.read(key)
			if err != nil {
				delete(index.entries, key)
				return false
			}
			for _, p := range conversationPanes(rec.Workspace) {
				if t, ok := durableConversation(p); ok {
					entry.targets = append(entry.targets, t)
				}
			}
			sort.Slice(entry.targets, func(i, j int) bool { return bytes.Compare(entry.targets[i][:], entry.targets[j][:]) < 0 })
		}
		index.entries[key] = entry
	}
	pos := sort.Search(len(entry.targets), func(i int) bool { return bytes.Compare(entry.targets[i][:], target[:]) >= 0 })
	return pos < len(entry.targets) && entry.targets[pos] == target
}
func (index *workspaceOpenIndex) retain(places []placegraph.Place) {
	known := make(map[string]bool, len(places))
	for _, p := range places {
		known[p.ID] = true
	}
	index.mu.Lock()
	defer index.mu.Unlock()
	for key := range index.entries {
		if !known[key] {
			delete(index.entries, key)
		}
	}
}

type openElsewherePlace struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Places6e: opening one durable conversation in another Place is a view of the
// same conversation. Membership alone never proves that the other view is open.
// Only stored open conversation panes authorize/match; closed/task/Inbox/Now
// candidates are excluded. No session paths or draft text leave this endpoint.
func (b *Bridge) workspaceOpenElsewhere(w http.ResponseWriter, r *http.Request, store *workspacestore.Store, key string) {
	if !needGet(w, r) {
		return
	}
	result := struct {
		Places []openElsewherePlace `json:"places"`
	}{Places: []openElsewherePlace{}}
	record, err := store.Get(key)
	if err != nil {
		workspaceError(w, err)
		return
	}
	var target conversationTarget
	found := false
	for _, p := range conversationPanes(record.Workspace) {
		if p.ID == r.URL.Query().Get("pane") {
			target, found = durableConversation(p)
			break
		}
	}
	if !found {
		write(w, result)
		return
	}
	b.mu.Lock()
	places := b.places
	b.mu.Unlock()
	if places == nil || places.Store == nil {
		write(w, result)
		return
	}
	graph, err := places.Store.Snapshot()
	if err != nil {
		write(w, result)
		return
	}
	index := b.workspaceOpenCache(store)
	index.retain(graph.Places)
	// The graph already bounds this list to MaxPlaces; unknown or deleted store
	// keys cannot become scan targets or user-facing names.
	for _, p := range graph.Places {
		if p.ID == key || !workspacestore.ValidKey(p.ID) {
			continue
		}
		if index.contains(p.ID, target, time.Now()) {
			result.Places = append(result.Places, openElsewherePlace{ID: p.ID, Name: p.Name})
		}
	}
	sort.Slice(result.Places, func(i, j int) bool {
		a, z := strings.ToLower(result.Places[i].Name), strings.ToLower(result.Places[j].Name)
		if a == z {
			return result.Places[i].ID < result.Places[j].ID
		}
		return a < z
	})
	write(w, result)
}
