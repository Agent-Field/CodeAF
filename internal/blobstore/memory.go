package blobstore

import (
	"bytes"
	"context"
	"sync"
)

// Op is one call a Memory store received, in order, for tests that assert
// what a caller did (and did not) send.
type Op struct {
	Kind  string // "put" | "get" | "has" | "getframe" | "locate"
	Frame FrameID
	RIDs  []string
	Bytes int64 // frame length for put and getframe, object length for get, 0 for has and locate
}

// Memory is the fake: a real store with the semantics of every other, kept in
// a map, that also remembers each call it answered.
type Memory struct {
	mu      sync.Mutex
	objects map[string][]byte
	frames  map[string][]byte   // whole frames, by id, for GetFrame
	locs    map[string]Location // where each object lies, learned at put time
	log     []Op
	fail    *failure // the next call to fail, if any
}

// failure is a call that will fail on purpose: after skip more calls succeed.
type failure struct {
	skip int
	err  error
}

// NewMemory returns an empty store.
func NewMemory() *Memory {
	return &Memory{
		objects: map[string][]byte{},
		frames:  map[string][]byte{},
		locs:    map[string]Location{},
	}
}

// Log returns a copy of every call so far, oldest first.
func (m *Memory) Log() []Op {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Op(nil), m.log...)
}

// FailAfter makes the call after the next n successful ones return err, once,
// so a test can watch a caller meet ErrFull or ErrUnreachable and then carry on.
func (m *Memory) FailAfter(n int, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fail = &failure{skip: n, err: err}
}

// due reports the injected error when it is this call's turn, and spends it.
func (m *Memory) due() error {
	f := m.fail
	if f == nil {
		return nil
	}
	if f.skip > 0 {
		f.skip--
		return nil
	}
	m.fail = nil
	return f.err
}

// PutFrame implements Store. It checks every object before it stores any, so a
// conflicting frame leaves the store exactly as it was.
func (m *Memory) PutFrame(_ context.Context, frame []byte) (FrameID, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	op := Op{Kind: "put", Bytes: int64(len(frame))}
	defer func() { m.log = append(m.log, op) }()

	if err := m.due(); err != nil {
		return "", err
	}
	h, objects, err := Decode(frame)
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
	// The frame and its locations are learned here, where the frame is already
	// decoded, so GetFrame and Locate read rather than decode. First location
	// wins, as with every store: a repeated rid keeps the pointer it had.
	m.frames[op.Frame] = bytes.Clone(frame)
	start := payloadStart(frame, h)
	for _, ref := range h.Objects {
		if _, held := m.locs[ref.RID]; !held {
			m.locs[ref.RID] = Location{Frame: op.Frame, Off: start + ref.Off, Len: ref.Len}
		}
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

	if err := m.due(); err != nil {
		return nil, err
	}
	if err := checkGet(rid); err != nil {
		return nil, err
	}
	have, err := m.lookup(rid)
	op.Bytes = int64(len(have))
	return have, err
}

// GetMany implements Store. It is one call in the log, however many objects it
// answers, because it is one request on every wire.
func (m *Memory) GetMany(_ context.Context, rids []string) ([]Object, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	op := Op{Kind: "get"}
	defer func() { m.log = append(m.log, op) }()

	if err := m.due(); err != nil {
		return nil, err
	}
	if err := checkMany(rids); err != nil {
		return nil, err
	}
	got, err := gatherPrefix(rids, m.lookup)
	op.RIDs, op.Bytes = ridsOf(got), sizeOf(got)
	return got, err
}

// lookup is a copy of the object held under rid; the caller holds the lock.
func (m *Memory) lookup(rid string) ([]byte, error) {
	have, ok := m.objects[rid]
	if !ok {
		return nil, ErrNotFound
	}
	return bytes.Clone(have), nil
}

// Has implements Store.
func (m *Memory) Has(_ context.Context, rids []string) ([]bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.log = append(m.log, Op{Kind: "has", RIDs: append([]string(nil), rids...)})

	if err := m.due(); err != nil {
		return nil, err
	}
	if err := checkHas(rids); err != nil {
		return nil, err
	}
	have := make([]bool, len(rids))
	for i, rid := range rids {
		_, have[i] = m.objects[rid]
	}
	return have, nil
}

// GetFrame implements Store: the frame it holds under id, as it was put.
func (m *Memory) GetFrame(_ context.Context, frame string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	op := Op{Kind: "getframe", Frame: frame}
	defer func() { m.log = append(m.log, op) }()

	if err := m.due(); err != nil {
		return nil, err
	}
	if err := checkGet(frame); err != nil {
		return nil, err
	}
	have, ok := m.frames[frame]
	if !ok {
		return nil, ErrNotFound
	}
	op.Bytes = int64(len(have))
	return bytes.Clone(have), nil
}

// Locate implements Store: where each held rid lies, read from the locations
// the puts learned.
func (m *Memory) Locate(_ context.Context, rids []string) (map[string]Location, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.log = append(m.log, Op{Kind: "locate", RIDs: append([]string(nil), rids...)})

	if err := m.due(); err != nil {
		return nil, err
	}
	if err := checkHas(rids); err != nil {
		return nil, err
	}
	at := make(map[string]Location, len(rids))
	for _, rid := range rids {
		if loc, ok := m.locs[rid]; ok {
			at[rid] = loc
		}
	}
	return at, nil
}

func ridsOf(objects []Object) []string {
	rids := make([]string, len(objects))
	for i, o := range objects {
		rids[i] = o.RID
	}
	return rids
}
