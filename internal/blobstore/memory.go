package blobstore

import (
	"bytes"
	"context"
	"sync"
)

// Op is one call a Memory store received, in order, for tests that assert
// what a caller did (and did not) send.
type Op struct {
	Kind  string // "put" | "get" | "has"
	Frame FrameID
	RIDs  []string
	Bytes int64 // frame length for put, object length for get, 0 for has
}

// Memory is the fake: a real store with the semantics of every other, kept in
// a map, that also remembers each call it answered.
type Memory struct {
	mu      sync.Mutex
	objects map[string][]byte
	log     []Op
}

// NewMemory returns an empty store.
func NewMemory() *Memory {
	return &Memory{objects: map[string][]byte{}}
}

// Log returns a copy of every call so far, oldest first.
func (m *Memory) Log() []Op {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Op(nil), m.log...)
}

// PutFrame implements Store. It checks every object before it stores any, so a
// conflicting frame leaves the store exactly as it was.
func (m *Memory) PutFrame(_ context.Context, frame []byte) (FrameID, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	op := Op{Kind: "put", Bytes: int64(len(frame))}
	defer func() { m.log = append(m.log, op) }()

	_, objects, err := Decode(frame)
	if err != nil {
		return "", err
	}
	op.Frame = IDOf(frame)
	op.RIDs = ridsOf(objects)
	if err := m.conflict(objects); err != nil {
		return "", err
	}
	for _, o := range objects {
		m.objects[o.RID] = bytes.Clone(o.Bytes)
	}
	return op.Frame, nil
}

// conflict reports ErrConflict when any object is already held with other bytes.
func (m *Memory) conflict(objects []Object) error {
	for _, o := range objects {
		if have, ok := m.objects[o.RID]; ok && !bytes.Equal(have, o.Bytes) {
			return ErrConflict
		}
	}
	return nil
}

// Get implements Store. The answer is a copy, so a caller cannot change what is stored.
func (m *Memory) Get(_ context.Context, rid string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	op := Op{Kind: "get", RIDs: []string{rid}}
	defer func() { m.log = append(m.log, op) }()

	if err := checkGet(rid); err != nil {
		return nil, err
	}
	have, ok := m.objects[rid]
	if !ok {
		return nil, ErrNotFound
	}
	op.Bytes = int64(len(have))
	return bytes.Clone(have), nil
}

// Has implements Store.
func (m *Memory) Has(_ context.Context, rids []string) ([]bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.log = append(m.log, Op{Kind: "has", RIDs: append([]string(nil), rids...)})

	if err := checkHas(rids); err != nil {
		return nil, err
	}
	have := make([]bool, len(rids))
	for i, rid := range rids {
		_, have[i] = m.objects[rid]
	}
	return have, nil
}

func ridsOf(objects []Object) []string {
	rids := make([]string, len(objects))
	for i, o := range objects {
		rids[i] = o.RID
	}
	return rids
}
