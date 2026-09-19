package wsexec

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// memStore is the test collections stand-in. Production has no equivalent:
// [Open] leaves a nil store as absence rather than falling back to memory.
type memStore struct {
	mu      sync.Mutex
	grants  map[string]Grant
	byKey   map[string]ExecutionBinding
	byEquiv map[string]string
	now     string
	idSeq   int
}

func newMemStore() *memStore {
	return &memStore{
		grants:  map[string]Grant{},
		byKey:   map[string]ExecutionBinding{},
		byEquiv: map[string]string{},
		now:     "2026-09-19T12:00:00Z",
	}
}

func (m *memStore) putGrant(g Grant) Grant {
	m.mu.Lock()
	defer m.mu.Unlock()
	if g.ID == "" {
		g.ID = m.nextID()
	}
	if g.Status == "" {
		g.Status = GrantActive
	}
	g.CreatedAt, g.UpdatedAt = m.now, m.now
	m.grants[g.ID] = g
	return g
}

func (m *memStore) GetGrant(_ context.Context, id string) (Grant, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	got, ok := m.grants[id]
	if !ok {
		return Grant{}, ErrNotFound
	}
	return got, nil
}

func (m *memStore) PutBinding(_ context.Context, b ExecutionBinding) (ExecutionBinding, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if b.RequestKey != "" {
		if existing, ok := m.byKey[b.RequestKey]; ok {
			return existing, nil
		}
	}
	if b.ID == "" {
		b.ID = m.nextID()
	}
	if b.WorkID == "" {
		b.WorkID = b.RequestKey
	}
	if b.State == "" {
		b.State = BindReserved
	}
	b.CreatedAt, b.UpdatedAt = m.now, m.now
	m.byKey[b.RequestKey] = b
	if b.EquivalenceKey != "" {
		if _, ok := m.byEquiv[b.EquivalenceKey]; !ok {
			m.byEquiv[b.EquivalenceKey] = b.RequestKey
		}
	}
	return b, nil
}

func (m *memStore) BindingByRequestKey(_ context.Context, requestKey string) (ExecutionBinding, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	got, ok := m.byKey[requestKey]
	if !ok {
		return ExecutionBinding{}, ErrNotFound
	}
	return got, nil
}

func (m *memStore) BindingByEquivalence(_ context.Context, equivalenceKey string) (ExecutionBinding, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key, ok := m.byEquiv[equivalenceKey]
	if !ok {
		return ExecutionBinding{}, ErrNotFound
	}
	return m.byKey[key], nil
}

func (m *memStore) BindRuntime(_ context.Context, requestKey, runInstanceID, runtimeRef string) (ExecutionBinding, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	got, ok := m.byKey[requestKey]
	if !ok {
		return ExecutionBinding{}, ErrNotFound
	}
	if got.RunInstanceID != "" && got.RunInstanceID != runInstanceID {
		return ExecutionBinding{}, bindingConflict()
	}
	got.RunInstanceID = runInstanceID
	got.RuntimeRef = runtimeRef
	got.State = BindBound
	got.BoundAt = m.now
	if got.AdmittedAt == "" {
		got.AdmittedAt = m.now
	}
	got.UpdatedAt = m.now
	m.byKey[requestKey] = got
	return got, nil
}

func (m *memStore) RecordJoiner(_ context.Context, requestKey, chatID string) (ExecutionBinding, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	got, ok := m.byKey[requestKey]
	if !ok {
		return ExecutionBinding{}, ErrNotFound
	}
	chatID = strings.TrimSpace(chatID)
	if chatID == "" || chatID == got.OwnerChatID || chatID == got.CoordinatorID {
		return got, nil
	}
	ids := memJoinerIDs(got.JoinerJSON)
	for _, id := range ids {
		if id == chatID {
			return got, nil
		}
	}
	raw, err := json.Marshal(append(ids, chatID))
	if err != nil {
		return ExecutionBinding{}, err
	}
	got.JoinerJSON = string(raw)
	got.UpdatedAt = m.now
	m.byKey[requestKey] = got
	return got, nil
}

func memJoinerIDs(raw string) []string {
	var items []string
	if raw == "" || json.Unmarshal([]byte(raw), &items) != nil {
		return nil
	}
	return items
}

func (m *memStore) nextID() string {
	m.idSeq++
	return fmt.Sprintf("id-%d", m.idSeq)
}

func (m *memStore) expireLease(requestKey, until string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	got := m.byKey[requestKey]
	got.LeaseUntil = until
	m.byKey[requestKey] = got
}

// fakeRuntime is the test task/run door. It must not appear in production
// files: a missing Runtime is absence, never this type.
type fakeRuntime struct {
	mu         sync.Mutex
	admitCalls int
	admits     []AdmitRequest
	byKey      map[string]AdmitResult
	works      map[string]*fakeWork
}

type fakeWork struct {
	req     AdmitRequest
	result  AdmitResult
	state   string
	steers  []SteerRevision
	paused  bool
	stopped bool
}

func newFakeRuntime() *fakeRuntime {
	return &fakeRuntime{
		byKey: map[string]AdmitResult{},
		works: map[string]*fakeWork{},
	}
}

func (r *fakeRuntime) Admit(_ context.Context, req AdmitRequest) (AdmitResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.admitCalls++
	r.admits = append(r.admits, req)
	if existing, ok := r.byKey[req.RequestKey]; ok {
		existing.Already = true
		return existing, nil
	}
	id := "ri-" + req.RequestKey
	got := AdmitResult{RunInstanceID: id, RuntimeRef: "fake/" + id, Road: req.Road}
	r.byKey[req.RequestKey] = got
	r.works[id] = &fakeWork{req: req, result: got, state: BindBound}
	return got, nil
}

func (r *fakeRuntime) FindByRequestKey(_ context.Context, requestKey string) (AdmitResult, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	got, ok := r.byKey[requestKey]
	return got, ok, nil
}

func (r *fakeRuntime) Inspect(_ context.Context, runInstanceID string) (WorkView, error) {
	work, err := r.must(runInstanceID)
	if err != nil {
		return WorkView{}, err
	}
	return WorkView{
		WorkID:        work.req.RequestKey,
		RequestKey:    work.req.RequestKey,
		RunInstanceID: runInstanceID,
		Road:          work.req.Road,
		State:         work.state,
		OwnerChatID:   work.req.OwnerChatID,
	}, nil
}

func (r *fakeRuntime) Steer(_ context.Context, runInstanceID string, rev SteerRevision) error {
	work, err := r.must(runInstanceID)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	work.steers = append(work.steers, rev)
	return nil
}

func (r *fakeRuntime) Pause(_ context.Context, runInstanceID string) error {
	work, err := r.must(runInstanceID)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	work.paused = true
	work.state = BindPaused
	return nil
}

func (r *fakeRuntime) Stop(_ context.Context, runInstanceID string) error {
	work, err := r.must(runInstanceID)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	work.stopped = true
	work.state = BindStopped
	return nil
}

func (r *fakeRuntime) Observe(_ context.Context, runInstanceID string) (ResultView, error) {
	work, err := r.must(runInstanceID)
	if err != nil {
		return ResultView{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	detail := "running"
	if work.stopped {
		detail = "stopped"
	}
	if work.paused {
		detail = "paused"
	}
	return ResultView{
		WorkID:        work.req.RequestKey,
		RunInstanceID: runInstanceID,
		State:         work.state,
		Detail:        detail,
	}, nil
}

func (r *fakeRuntime) must(runInstanceID string) (*fakeWork, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	work, ok := r.works[runInstanceID]
	if !ok {
		return nil, ErrNotFound
	}
	return work, nil
}

func (r *fakeRuntime) seedAdmitted(req AdmitRequest) AdmitResult {
	r.mu.Lock()
	defer r.mu.Unlock()
	id := "ri-" + req.RequestKey
	got := AdmitResult{RunInstanceID: id, RuntimeRef: "fake/" + id, Road: req.Road, Already: true}
	r.byKey[req.RequestKey] = got
	r.works[id] = &fakeWork{req: req, result: got, state: BindBound}
	return got
}

func executeGrant(actions ...string) Grant {
	if len(actions) == 0 {
		actions = []string{ClassExecute, ClassSteer, ClassStop}
	}
	return Grant{ActionJSON: jsonActions(actions), Status: GrantActive, Origin: OriginPerson}
}

func jsonActions(actions []string) string {
	buf := []byte{'['}
	for i, a := range actions {
		if i > 0 {
			buf = append(buf, ',')
		}
		buf = append(buf, '"')
		buf = append(buf, a...)
		buf = append(buf, '"')
	}
	buf = append(buf, ']')
	return string(buf)
}

func launchReq(key, equiv, grantID string) LaunchRequest {
	return LaunchRequest{
		RequestKey:     key,
		EquivalenceKey: equiv,
		GrantID:        grantID,
		CoordinatorID:  "coord-1",
		OwnerChatID:    "chat-owner",
		Brief:          "add a README comment",
	}
}

func pastLease() string {
	return time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339)
}
