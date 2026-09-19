package wscollab

import (
	"context"
	"sync"
)

// memStore is the test outbox. Production has no equivalent: [New] refuses a
// nil store rather than falling back to memory.
type memStore struct {
	mu          sync.Mutex
	rows        map[DeliveryID]Record
	discussions map[string]Discussion
	invocations map[string]Invocation
}

func newMemStore() *memStore {
	return &memStore{
		rows:        map[DeliveryID]Record{},
		discussions: map[string]Discussion{},
		invocations: map[string]Invocation{},
	}
}

func (m *memStore) Put(_ context.Context, env Envelope) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.rows[env.ID]; ok {
		return nil
	}
	m.rows[env.ID] = Record{Envelope: env, Queue: QueuePending}
	return nil
}

func (m *memStore) Get(_ context.Context, id DeliveryID) (Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	got, ok := m.rows[id]
	if !ok {
		return Record{}, ErrNotFound
	}
	return got, nil
}

func (m *memStore) SetQueue(_ context.Context, id DeliveryID, queue string) error {
	return m.patch(id, func(row *Record) { row.Queue = queue })
}

func (m *memStore) SetRecorded(_ context.Context, id DeliveryID) error {
	return m.patch(id, func(row *Record) { row.Recorded = true })
}

func (m *memStore) SetProcessed(_ context.Context, id DeliveryID) error {
	return m.patch(id, func(row *Record) { row.Processed = true })
}

func (m *memStore) patch(id DeliveryID, edit func(*Record)) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	row, ok := m.rows[id]
	if !ok {
		return ErrNotFound
	}
	edit(&row)
	m.rows[id] = row
	return nil
}

func (m *memStore) Pending(_ context.Context, conversationID string) ([]Envelope, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Envelope
	for _, row := range m.rows {
		if row.Envelope.To == conversationID && !row.Recorded {
			out = append(out, row.Envelope)
		}
	}
	return out, nil
}

func (m *memStore) PutDiscussion(_ context.Context, d Discussion) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	copied := d
	copied.Participants = append([]Participant(nil), d.Participants...)
	m.discussions[d.ID] = copied
	return nil
}

func (m *memStore) GetDiscussion(_ context.Context, id string) (Discussion, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	got, ok := m.discussions[id]
	if !ok {
		return Discussion{}, ErrNotFound
	}
	got.Participants = append([]Participant(nil), got.Participants...)
	return got, nil
}

func (m *memStore) PutInvocation(_ context.Context, inv Invocation) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.invocations[inv.ID] = inv
	return nil
}

func (m *memStore) GetInvocation(_ context.Context, id string) (Invocation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	got, ok := m.invocations[id]
	if !ok {
		return Invocation{}, ErrNotFound
	}
	return got, nil
}

type memSeam struct {
	mu       sync.Mutex
	journal  map[DeliveryID]Envelope
	accepted []DeliveryID
	acceptAs string
	appended int
	accepts  int
}

func newSeam() *memSeam {
	return &memSeam{journal: map[DeliveryID]Envelope{}, acceptAs: QueueAccepted}
}

func (s *memSeam) Recorded(id DeliveryID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.journal[id]
	return ok
}

func (s *memSeam) Append(_ context.Context, env Envelope) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.journal[env.ID]; ok {
		return nil
	}
	s.journal[env.ID] = env
	s.appended++
	return nil
}

func (s *memSeam) Accept(_ context.Context, env Envelope) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.accepted = append(s.accepted, env.ID)
	s.accepts++
	return s.acceptAs
}

func (s *memSeam) lines() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.appended
}

type memHost struct {
	alive  bool
	wakes  int
	err    error
	onWake func()
}

func (h *memHost) Alive() bool { return h.alive }

func (h *memHost) Wake(context.Context, string) error {
	h.wakes++
	if h.onWake != nil {
		h.onWake()
	}
	return h.err
}

type memFinder struct {
	host Host
	err  error
	hits int
}

func (f *memFinder) Find(context.Context, string) (Host, error) {
	f.hits++
	return f.host, f.err
}
