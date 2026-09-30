package cellsync

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
)

// fakeKeyID is the cell_key_id the fake writes into every frame header.
const fakeKeyID = "0123456789abcdef0123456789abcdef"

// maxWant is how many remote ids one Want answers, as the engine does.
const maxWant = 1000

var objectMagic = []byte("AGEO\x01")

// The fake's two object kinds. Each kind knows how to name its children, so
// walking the graph needs no switch on kind.
const (
	kindBlob byte = 'b'
	kindSnap byte = 's'
)

var childrenOf = map[byte]func(payload []byte) ([]string, error){
	kindBlob: func([]byte) ([]string, error) { return nil, nil },
	kindSnap: snapshotChildren,
}

// snapshot is the fake's tree and snapshot in one: the files of a turn and the
// snapshot of the turn before it.
type snapshot struct {
	Parent string            `json:"parent,omitempty"`
	Files  map[string]string `json:"files"` // path -> blob rid
}

func snapshotChildren(payload []byte) ([]string, error) {
	s, err := decodeSnapshot(payload)
	if err != nil {
		return nil, err
	}
	kids := make([]string, 0, len(s.Files)+1)
	if s.Parent != "" {
		kids = append(kids, s.Parent)
	}
	rids := make([]string, 0, len(s.Files))
	for _, rid := range s.Files {
		rids = append(rids, rid)
	}
	sort.Strings(rids)
	return append(kids, rids...), nil
}

func decodeSnapshot(payload []byte) (s snapshot, err error) {
	err = json.Unmarshal(payload, &s)
	return s, err
}

// pack builds an object: the engine magic, its kind, then the payload. Its rid
// is the hash of those bytes, so the same content always has the same rid.
func pack(kind byte, payload []byte) (rid string, raw []byte) {
	raw = append(append(bytes.Clone(objectMagic), kind), payload...)
	return ridOf(raw), raw
}

func ridOf(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// unpack is pack's inverse and refuses bytes the fake did not write.
func unpack(raw []byte) (kind byte, payload []byte, err error) {
	if len(raw) <= len(objectMagic) || !bytes.HasPrefix(raw, objectMagic) {
		return 0, nil, errors.New("fake engine: not an engine object")
	}
	return raw[len(objectMagic)], raw[len(objectMagic)+1:], nil
}

// kids answers the rids an object points to.
func kids(raw []byte) ([]string, error) {
	kind, payload, err := unpack(raw)
	if err != nil {
		return nil, err
	}
	fn, ok := childrenOf[kind]
	if !ok {
		return nil, fmt.Errorf("fake engine: unknown object kind %q", kind)
	}
	return fn(payload)
}

// fakeCell is one cell's object graph and its published set on one device.
type fakeCell struct {
	objects   map[string][]byte
	published map[string]bool
	last      string // head of the newest Seal, the parent of the next one
}

// FakeEngine is a small real Engine: an in-memory object graph per cell that
// writes real frames and answers Want, Import and Materialize by walking it.
// Two FakeEngines are two devices; frames and inbox files are the only thing
// that passes between them.
type FakeEngine struct {
	// Dir holds the outbox frame files.
	Dir string
	// MaxObjects closes a frame after this many objects; 0 puts them all in one.
	MaxObjects int

	mu    sync.Mutex
	cells map[string]*fakeCell
}

// NewFakeEngine returns an engine that keeps its frame files under dir.
func NewFakeEngine(dir string) *FakeEngine {
	return &FakeEngine{Dir: dir, cells: map[string]*fakeCell{}}
}

// Ledger returns an engine over the same local objects that has published
// nothing: what a device's store looks like to a second identity, whose ledger
// starts empty. Frames go under dir.
func (f *FakeEngine) Ledger(dir string) *FakeEngine {
	f.mu.Lock()
	defer f.mu.Unlock()
	other := NewFakeEngine(dir)
	other.MaxObjects = f.MaxObjects
	for id, fc := range f.cells {
		other.cells[id] = &fakeCell{objects: maps.Clone(fc.objects), published: map[string]bool{}, last: fc.last}
	}
	return other
}

func (f *FakeEngine) cell(c cell.Cell) *fakeCell {
	fc, ok := f.cells[c.ID]
	if !ok {
		fc = &fakeCell{objects: map[string][]byte{}, published: map[string]bool{}}
		f.cells[c.ID] = fc
	}
	return fc
}

// Seal records a turn holding files (path -> content) on top of the previous
// one and returns its head.
func (f *FakeEngine) Seal(c cell.Cell, files map[string]string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	fc := f.cell(c)
	snap := snapshot{Parent: fc.last, Files: map[string]string{}}
	for path, content := range files {
		rid, raw := pack(kindBlob, []byte(content))
		fc.objects[rid] = raw
		snap.Files[path] = rid
	}
	payload, _ := json.Marshal(snap) // a struct of strings cannot fail to encode
	head, raw := pack(kindSnap, payload)
	fc.objects[head] = raw
	fc.last = head
	return head
}

// Export implements Engine.
func (f *FakeEngine) Export(_ context.Context, c cell.Cell, head string) (Export, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fc := f.cell(c)
	order, err := fc.unsent(head)
	if err != nil {
		return Export{}, err
	}
	ex := Export{HeadRID: head}
	for _, part := range f.split(order) {
		ff, err := f.writeFrame(c, fc, part)
		if err != nil {
			return Export{}, err
		}
		ex.Frames = append(ex.Frames, ff)
		ex.Objects += ff.Objects
		ex.Bytes += ff.Bytes
	}
	return ex, nil
}

// unsent lists the objects reachable from head that were not published,
// children before the objects that name them.
func (fc *fakeCell) unsent(head string) ([]string, error) {
	var order []string
	seen := map[string]bool{}
	var visit func(rid string) error
	visit = func(rid string) error {
		if seen[rid] || fc.published[rid] {
			return nil
		}
		seen[rid] = true
		raw, ok := fc.objects[rid]
		if !ok {
			return fmt.Errorf("fake engine: object %s is not held here", rid)
		}
		children, err := kids(raw)
		if err != nil {
			return err
		}
		for _, k := range children {
			if err := visit(k); err != nil {
				return err
			}
		}
		order = append(order, rid)
		return nil
	}
	return order, visit(head)
}

// split cuts order into frames of at most MaxObjects objects.
func (f *FakeEngine) split(order []string) [][]string {
	if len(order) == 0 {
		return nil
	}
	if f.MaxObjects <= 0 {
		return [][]string{order}
	}
	var parts [][]string
	for len(order) > f.MaxObjects {
		parts = append(parts, order[:f.MaxObjects])
		order = order[f.MaxObjects:]
	}
	return append(parts, order)
}

// writeFrame encodes rids as one frame file named by the frame's own id, so
// exporting twice writes the same file.
func (f *FakeEngine) writeFrame(c cell.Cell, fc *fakeCell, rids []string) (FrameFile, error) {
	objects := make([]blobstore.Object, len(rids))
	for i, rid := range rids {
		objects[i] = blobstore.Object{RID: rid, Bytes: fc.objects[rid]}
	}
	frame, err := blobstore.Encode(fakeKeyID, objects)
	if err != nil {
		return FrameFile{}, err
	}
	dir := filepath.Join(f.Dir, "outbox", c.ID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return FrameFile{}, err
	}
	path := filepath.Join(dir, blobstore.IDOf(frame)+".frame")
	if err := os.WriteFile(path, frame, 0o600); err != nil {
		return FrameFile{}, err
	}
	return FrameFile{Path: path, Objects: len(rids), Bytes: int64(len(frame))}, nil
}

// Published implements Engine.
func (f *FakeEngine) Published(_ context.Context, c cell.Cell, frames []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	fc := f.cell(c)
	for _, path := range frames {
		frame, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		_, objects, err := blobstore.Decode(frame)
		if err != nil {
			return err
		}
		for _, o := range objects {
			fc.published[o.RID] = true
		}
		if err := os.Remove(path); err != nil {
			return err
		}
	}
	return nil
}

// Want implements Engine: breadth first from head, an absent object is wanted
// and its children are not yet known.
func (f *FakeEngine) Want(_ context.Context, c cell.Cell, head string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cell(c).want(head)
}

func (fc *fakeCell) want(head string) ([]string, error) {
	var want []string
	seen := map[string]bool{head: true}
	for queue := []string{head}; len(queue) > 0 && len(want) < maxWant; queue = queue[1:] {
		rid := queue[0]
		raw, ok := fc.objects[rid]
		if !ok {
			want = append(want, rid)
			continue
		}
		children, err := kids(raw)
		if err != nil {
			return nil, err
		}
		for _, k := range children {
			if !seen[k] {
				seen[k] = true
				queue = append(queue, k)
			}
		}
	}
	return want, nil
}

// Import implements Engine. It checks every file before it stores any, so a
// bad file leaves the graph as it was.
func (f *FakeEngine) Import(_ context.Context, c cell.Cell, head, inbox string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fc := f.cell(c)
	wanted, err := fc.want(head)
	if err != nil {
		return 0, err
	}
	files, err := readInbox(inbox, wanted)
	if err != nil {
		return 0, err
	}
	for rid, raw := range files {
		fc.objects[rid] = raw
		// What the store handed over is in the store, so it is never sent back.
		fc.published[rid] = true
		if err := os.Remove(filepath.Join(inbox, rid)); err != nil {
			return 0, err
		}
	}
	return len(files), nil
}

// readInbox loads every file in inbox, each of which must be wanted and must
// hash to its own name.
func readInbox(inbox string, wanted []string) (map[string][]byte, error) {
	entries, err := os.ReadDir(inbox)
	if err != nil {
		return nil, err
	}
	isWanted := map[string]bool{}
	for _, rid := range wanted {
		isWanted[rid] = true
	}
	files := map[string][]byte{}
	for _, e := range entries {
		raw, err := readVerified(inbox, e.Name(), isWanted)
		if err != nil {
			return nil, err
		}
		files[e.Name()] = raw
	}
	return files, nil
}

func readVerified(inbox, rid string, wanted map[string]bool) ([]byte, error) {
	if !wanted[rid] {
		return nil, fmt.Errorf("fake engine: object %s was not wanted", rid)
	}
	raw, err := os.ReadFile(filepath.Join(inbox, rid))
	if err != nil {
		return nil, err
	}
	if _, _, err := unpack(raw); err != nil || ridOf(raw) != rid {
		return nil, fmt.Errorf("fake engine: object %s failed verification", rid)
	}
	return raw, nil
}

// Materialize implements Engine: it writes the files of head's snapshot under
// the cell's folder.
func (f *FakeEngine) Materialize(_ context.Context, c cell.Cell, head string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	fc := f.cell(c)
	if missing, err := fc.want(head); err != nil || len(missing) > 0 {
		return fmt.Errorf("fake engine: head %s is incomplete (missing %v, err %v)", head, missing, err)
	}
	_, payload, err := unpack(fc.objects[head])
	if err != nil {
		return err
	}
	snap, err := decodeSnapshot(payload)
	if err != nil {
		return err
	}
	return fc.writeFiles(c.Root, snap)
}

func (fc *fakeCell) writeFiles(root string, snap snapshot) error {
	for path, rid := range snap.Files {
		_, content, err := unpack(fc.objects[rid])
		if err != nil {
			return err
		}
		target := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(target, content, 0o600); err != nil {
			return err
		}
	}
	return nil
}
