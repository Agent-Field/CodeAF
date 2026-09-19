package wsapi

import (
	"context"
	"fmt"
	"sync"

	"github.com/Agent-Field/codeaf/internal/workspace"
)

// fakeStore implements the frozen store contract. Tests talk to this, not the
// adapter, so provenance, atomic Move, WhyHere, and cycle refusals are real.
type fakeStore struct {
	mu          sync.Mutex
	seq         int
	rootRev     int
	cols        []workspace.Collection
	members     map[string][]workspace.Ref
	events      []workspace.MembershipEvent
	keys        map[string]keyedOp
	revisionErr error
	at          string
}

type keyedOp struct {
	action, collection, from string
	ref                      workspace.Ref
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		members: map[string][]workspace.Ref{},
		keys:    map[string]keyedOp{},
		at:      "2026-09-18T00:00:00Z",
	}
}

var _ store = (*fakeStore)(nil)

func (f *fakeStore) Close() error { return nil }

func (f *fakeStore) SchemaVersion() int { return 2 }

func (f *fakeStore) Create(_ context.Context, name string) (workspace.Collection, error) {
	if err := workspace.ValidateName(name); err != nil {
		return workspace.Collection{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq++
	f.rootRev++
	created := workspace.Collection{ID: fmt.Sprintf("c%02d", f.seq), Name: name, Lifecycle: workspace.LifecycleActive, Revision: 1}
	f.cols = append(f.cols, created)
	f.members[created.ID] = []workspace.Ref{}
	return created, nil
}

func (f *fakeStore) Rename(_ context.Context, id, name string) error {
	if err := workspace.ValidateName(name); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.refuse(); err != nil {
		return err
	}
	for i, collection := range f.cols {
		if collection.ID == id {
			f.cols[i].Name = name
			return nil
		}
	}
	return workspace.ErrNotFound
}

func (f *fakeStore) Collections(context.Context) ([]workspace.Collection, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]workspace.Collection, len(f.cols))
	copy(out, f.cols)
	return out, nil
}

func (f *fakeStore) Members(_ context.Context, id string) ([]workspace.Ref, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.members[id]; !ok {
		return nil, workspace.ErrNotFound
	}
	out := make([]workspace.Ref, len(f.members[id]))
	copy(out, f.members[id])
	return out, nil
}

func (f *fakeStore) CollectionsFor(_ context.Context, ref workspace.Ref) ([]workspace.Collection, error) {
	if err := ref.Validate(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]workspace.Collection, 0)
	for _, collection := range f.cols {
		if f.hasLocked(collection.ID, ref) {
			out = append(out, collection)
		}
	}
	return out, nil
}

func (f *fakeStore) Add(ctx context.Context, id string, ref workspace.Ref) error {
	return f.AddWith(ctx, id, ref, workspace.Provenance{Origin: workspace.OriginPerson})
}

func (f *fakeStore) Remove(ctx context.Context, id string, ref workspace.Ref) error {
	return f.RemoveWith(ctx, id, ref, workspace.Provenance{Origin: workspace.OriginPerson})
}

func (f *fakeStore) AddWith(_ context.Context, id string, ref workspace.Ref, p workspace.Provenance) error {
	if err := ref.Validate(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.refuse(); err != nil {
		return err
	}
	if _, ok := f.members[id]; !ok {
		return workspace.ErrNotFound
	}
	if replayed, err := f.replay(p, workspace.ActionAdd, id, ref, ""); replayed || err != nil {
		return err
	}
	if ref.Kind == workspace.CollectionKind {
		if _, ok := f.members[ref.ID]; !ok {
			return workspace.ErrNotFound
		}
		if f.wouldCycle(id, ref.ID) {
			return workspace.ErrCycle
		}
	}
	if f.hasLocked(id, ref) {
		return nil
	}
	f.members[id] = append(f.members[id], ref)
	f.record(id, ref, workspace.ActionAdd, p)
	f.bump(id)
	return nil
}

func (f *fakeStore) RemoveWith(_ context.Context, id string, ref workspace.Ref, p workspace.Provenance) error {
	if err := ref.Validate(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.refuse(); err != nil {
		return err
	}
	if _, ok := f.members[id]; !ok {
		return workspace.ErrNotFound
	}
	if replayed, err := f.replay(p, workspace.ActionRemove, id, ref, ""); replayed || err != nil {
		return err
	}
	kept := make([]workspace.Ref, 0, len(f.members[id]))
	found := false
	for _, member := range f.members[id] {
		if sameRef(member, ref) {
			found = true
			continue
		}
		kept = append(kept, member)
	}
	f.members[id] = kept
	if found {
		f.record(id, ref, workspace.ActionRemove, p)
		f.bump(id)
	}
	return nil
}

func (f *fakeStore) Move(_ context.Context, fromID, toID string, ref workspace.Ref, p workspace.Provenance) error {
	if err := ref.Validate(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.refuse(); err != nil {
		return err
	}
	if _, ok := f.members[fromID]; !ok {
		return workspace.ErrNotFound
	}
	if _, ok := f.members[toID]; !ok {
		return workspace.ErrNotFound
	}
	if fromID == toID {
		return nil
	}
	if replayed, err := f.replay(p, workspace.ActionAdd, toID, ref, fromID); replayed || err != nil {
		return err
	}
	if ref.Kind == workspace.CollectionKind && f.wouldCycle(toID, ref.ID) {
		return workspace.ErrCycle
	}
	if !f.hasLocked(toID, ref) {
		f.members[toID] = append(f.members[toID], ref)
		f.record(toID, ref, workspace.ActionAdd, p)
	}
	kept := make([]workspace.Ref, 0, len(f.members[fromID]))
	found := false
	for _, member := range f.members[fromID] {
		if sameRef(member, ref) {
			found = true
			continue
		}
		kept = append(kept, member)
	}
	f.members[fromID] = kept
	if found {
		f.record(fromID, ref, workspace.ActionRemove, p)
	}
	f.bump(fromID)
	f.bump(toID)
	return nil
}

func (f *fakeStore) WhyHere(_ context.Context, id string, ref workspace.Ref) (workspace.MembershipEvent, error) {
	if err := ref.Validate(); err != nil {
		return workspace.MembershipEvent{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.events) - 1; i >= 0; i-- {
		event := f.events[i]
		if event.CollectionID == id && event.Kind == ref.Kind && event.RefID == ref.ID && event.SessionID == ref.SessionID {
			return event, nil
		}
	}
	return workspace.MembershipEvent{}, workspace.ErrNotFound
}

func (f *fakeStore) Events(_ context.Context, id string, ref workspace.Ref) ([]workspace.MembershipEvent, error) {
	if err := ref.Validate(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]workspace.MembershipEvent, 0)
	for _, event := range f.events {
		if event.CollectionID == id && event.Kind == ref.Kind && event.RefID == ref.ID && event.SessionID == ref.SessionID {
			out = append(out, event)
		}
	}
	return out, nil
}

func (f *fakeStore) RootState(context.Context) (int, string, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.rootRev == 0 {
		return 0, "", "", workspace.ErrNotFound
	}
	return f.rootRev, "", f.at, nil
}

func (f *fakeStore) refuse() error { return f.revisionErr }

func (f *fakeStore) replay(p workspace.Provenance, action, collection string, ref workspace.Ref, from string) (bool, error) {
	if p.IdempotencyKey == "" {
		return false, nil
	}
	prev, seen := f.keys[p.IdempotencyKey]
	if !seen {
		f.keys[p.IdempotencyKey] = keyedOp{action: action, collection: collection, from: from, ref: ref}
		return false, nil
	}
	if prev.action == action && prev.collection == collection && prev.from == from && sameRef(prev.ref, ref) {
		return true, nil
	}
	return false, fmt.Errorf("%w: idempotency key already used for a different operation", workspace.ErrInvalid)
}

func (f *fakeStore) bump(id string) {
	f.rootRev++
	for i, collection := range f.cols {
		if collection.ID == id {
			f.cols[i].Revision++
			return
		}
	}
}

func (f *fakeStore) record(id string, ref workspace.Ref, action string, p workspace.Provenance) {
	f.events = append(f.events, workspace.MembershipEvent{
		CollectionID:   id,
		Kind:           ref.Kind,
		RefID:          ref.ID,
		SessionID:      ref.SessionID,
		Action:         action,
		Origin:         p.Origin,
		Reason:         p.Reason,
		Actor:          p.Actor,
		Evidence:       p.Evidence,
		At:             f.at,
		IdempotencyKey: p.IdempotencyKey,
	})
}

func (f *fakeStore) hasLocked(id string, ref workspace.Ref) bool {
	for _, member := range f.members[id] {
		if sameRef(member, ref) {
			return true
		}
	}
	return false
}

func (f *fakeStore) wouldCycle(parentID, childID string) bool {
	seen := map[string]struct{}{}
	stack := []string{childID}
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		if id == parentID {
			return true
		}
		for _, ref := range f.members[id] {
			if ref.Kind == workspace.CollectionKind {
				stack = append(stack, ref.ID)
			}
		}
	}
	return false
}

func sameRef(a, b workspace.Ref) bool {
	return a.Kind == b.Kind && a.ID == b.ID && a.SessionID == b.SessionID
}

type orderedInventory struct {
	ids    []string
	titles map[string]string
}

func (o orderedInventory) ConversationIDs() []string { return append([]string{}, o.ids...) }

func (o orderedInventory) Title(id string) string { return o.titles[id] }
