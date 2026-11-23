package session

import (
	"strings"
	"sync"
)

// Slots are reserved before a paid render starts and owned until the provider
// returns AND its latest fact is saved. Closed session objects are not owners
// of unsaved outcomes. Refusal at this fixed process bound never evicts one.
const renderHistoryOwners = 64

var processRenderHistory = newRenderRetention(renderHistoryOwners)

type retainedRender struct {
	path     string
	active   bool
	revision uint64
	record   *RenderRecord
	event    string
}
type renderRetention struct {
	mu    sync.Mutex
	limit int
	rows  map[string]*retainedRender
}
type pendingRender struct {
	key      string
	revision uint64
	record   RenderRecord
	event    string
}

func newRenderRetention(limit int) *renderRetention {
	return &renderRetention{limit: limit, rows: make(map[string]*retainedRender)}
}
func renderRetentionKey(path, id string) string { return path + "\x00" + id }
func (r *renderRetention) reserve(path, id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.rows) >= r.limit {
		return false
	}
	key := renderRetentionKey(path, id)
	if _, exists := r.rows[key]; exists {
		return false
	}
	r.rows[key] = &retainedRender{path: strings.Clone(path), active: true}
	return true
}
func (r *renderRetention) retain(path string, record RenderRecord, event string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry := r.rows[renderRetentionKey(path, record.ID)]
	if entry == nil {
		return
	} // Every provider has reserved its slot before starting.
	copy := cloneRenderRecord(record)
	entry.record = &copy
	if entry.event != renderEvEnd {
		entry.event = event
	}
	entry.revision++
}
func (r *renderRetention) release(path, id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := renderRetentionKey(path, id)
	if entry := r.rows[key]; entry != nil {
		entry.active = false
		if entry.record == nil {
			delete(r.rows, key)
		}
	}
}
func (r *renderRetention) pending(path string) []pendingRender {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []pendingRender
	for key, entry := range r.rows {
		if entry.path == path && entry.record != nil {
			out = append(out, pendingRender{key: key, revision: entry.revision, record: cloneRenderRecord(*entry.record), event: entry.event})
		}
	}
	return out
}
func (r *renderRetention) saved(key string, revision uint64, event string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry := r.rows[key]
	if entry == nil {
		return false
	}
	if event == renderEvEnd {
		entry.event = renderEvCharge
	}
	if entry.revision != revision {
		return false
	}
	entry.record = nil
	entry.event = ""
	if !entry.active {
		delete(r.rows, key)
	}
	return true
}
func (r *renderRetention) get(path, id string) (RenderRecord, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if entry := r.rows[renderRetentionKey(path, id)]; entry != nil && entry.record != nil {
		return cloneRenderRecord(*entry.record), true
	}
	return RenderRecord{}, false
}
func (r *renderRetention) hasPending(path string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, entry := range r.rows {
		if entry.path == path && entry.record != nil {
			return true
		}
	}
	return false
}

func cloneRenderRecord(record RenderRecord) RenderRecord {
	record.Options = append([]string(nil), record.Options...)
	if record.Ended != nil {
		value := *record.Ended
		record.Ended = &value
	}
	if record.DurationMS != nil {
		value := *record.DurationMS
		record.DurationMS = &value
	}
	if record.CostUSD != nil {
		value := *record.CostUSD
		record.CostUSD = &value
	}
	return record
}
