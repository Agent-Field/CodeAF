package cellsync

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
	"golang.org/x/crypto/chacha20poly1305"
)

// The contract these tests state, in a person's terms: when a chat moves onto a
// computer that already holds an older copy of it, the conversation records the
// chat opens on are byte for byte the ones the incoming head names. A file on
// this disk may stand in for a fetched one only when it is the same bytes; a
// file that merely has the same length, or merely exists, never does.

func TestBlake3SumMatchesTheSpecification(t *testing.T) {
	// Vectors from the BLAKE3 reference: the empty input, a short one, and a
	// multi-chunk input that exercises the tree (5000 bytes of i mod 251).
	long := make([]byte, 5000)
	for i := range long {
		long[i] = byte(i % 251)
	}
	for _, c := range []struct {
		name string
		in   []byte
		want string
	}{
		{"empty", nil, "af1349b9f5f9a1a6a0404dea36dcc9499bcb25c9adc112b7cc9a93cae41f3262"},
		{"abc", []byte("abc"), "6437b3ac38465133ffb63b75273a8db548c558465d79db03fd359c6cd5bd9d85"},
		{"five chunks", long, "ee78d92070de3df1c57c37002abf0a6b1a6589acdeef4d8ffac7cf3d9e8f2836"},
	} {
		got := blake3Sum(c.in)
		if hex.EncodeToString(got[:]) != c.want {
			t.Errorf("%s: blake3 = %x, want %s", c.name, got, c.want)
		}
	}
}

// openWorld is a head the way furrow publishes it, with the objects a test
// chooses to carry in the head's frame and the rest held only by the store.
type openWorld struct {
	t       *testing.T
	cellKey [32]byte
	dedup   [32]byte
	all     map[string]blobstore.Object // every object the head can reach
	carried map[string]bool             // the rids the head's frame carries
	head    string
}

func newOpenWorld(t *testing.T) *openWorld {
	t.Helper()
	w := &openWorld{t: t, all: map[string]blobstore.Object{}, carried: map[string]bool{}}
	if _, err := rand.Read(w.cellKey[:]); err != nil {
		t.Fatal(err)
	}
	if _, err := rand.Read(w.dedup[:]); err != nil {
		t.Fatal(err)
	}
	return w
}

func nums(b []byte) []uint16 {
	out := make([]uint16, len(b))
	for i, v := range b {
		out[i] = uint16(v)
	}
	return out
}

type wBlob struct {
	Chunks   []wChunk `json:"chunks"`
	TotalLen uint64   `json:"total_len"`
}
type wChunk struct {
	ID  []uint16 `json:"id"`
	Len uint64   `json:"len"`
}
type wEntry struct {
	Name   []uint16 `json:"name"`
	Kind   string   `json:"kind"`
	Target []uint16 `json:"target"`
	Mode   uint32   `json:"mode"`
}
type wDir struct {
	Entries []wEntry `json:"entries"`
	Pages   []any    `json:"pages"`
}
type wSnap struct {
	RootTree []uint16 `json:"root_tree"`
	Label    string   `json:"label"`
}

// meta seals one furrow metadata object and returns its object id. carry says
// whether the head's frame holds it.
func (w *openWorld) meta(kind byte, body any, carry bool) [32]byte {
	w.t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		w.t.Fatal(err)
	}
	oid := sha256.Sum256(append([]byte{kind}, raw...))
	rid := metaRID(w.cellKey, oid)
	aad, _ := hex.DecodeString(rid)
	aead, err := chacha20poly1305.NewX(w.cellKey[:])
	if err != nil {
		w.t.Fatal(err)
	}
	var nonce [24]byte
	_, _ = rand.Read(nonce[:])
	plain := append(append([]byte{kind}, oid[:]...), raw...)
	sealed := aead.Seal(nil, nonce[:], plain, aad)
	obj := append(append([]byte("AGEO\x01"), nonce[:]...), sealed...)
	w.all[rid] = blobstore.Object{RID: rid, Bytes: obj}
	w.carried[rid] = carry
	return oid
}

// file seals content as a blob of one chunk per part and returns the blob's
// object id. carryMeta says whether the frame holds the blob's metadata, and
// carryChunk(i) whether it holds part i's bytes.
func (w *openWorld) file(parts [][]byte, carryMeta bool, carryChunk func(i int) bool) [32]byte {
	w.t.Helper()
	var blob wBlob
	for i, part := range parts {
		cid := furrowChunkID(part)
		obj, ok := encryptChunkObject(w.dedup, cid, part)
		if !ok {
			w.t.Fatal("cannot seal chunk")
		}
		w.all[obj.RID] = obj
		w.carried[obj.RID] = carryChunk != nil && carryChunk(i)
		blob.Chunks = append(blob.Chunks, wChunk{ID: nums(cid[:]), Len: uint64(len(part))})
		blob.TotalLen += uint64(len(part))
	}
	return w.meta(2, blob, carryMeta)
}

func (w *openWorld) dir(entries ...wEntry) [32]byte {
	return w.meta(3, wDir{Entries: entries, Pages: []any{}}, true)
}

func fileEntry(name string, target [32]byte) wEntry {
	return wEntry{Name: nums([]byte(name)), Kind: "File", Target: nums(target[:]), Mode: 0o600}
}

func dirEntry(name string, target [32]byte) wEntry {
	return wEntry{Name: nums([]byte(name)), Kind: "Directory", Target: nums(target[:]), Mode: 0o700}
}

// seal finishes the head: a snapshot over a root that holds one .cell.
func (w *openWorld) seal(cellDir [32]byte, rootFiles ...wEntry) {
	root := w.dir(append([]wEntry{dirEntry(cell.StateDir, cellDir)}, rootFiles...)...)
	snap := w.meta(4, wSnap{RootTree: nums(root[:]), Label: "turn-1"}, true)
	w.head = hex.EncodeToString(snap[:])
}

// keyedEngine answers only what stageOpenFromInbox asks of an engine: the keys
// and the place the open objects are cached.
type keyedEngine struct {
	Engine
	w     *openWorld
	cache string
}

func (e keyedEngine) OpenKeys() ([]byte, []byte) { return e.w.cellKey[:], e.w.dedup[:] }
func (e keyedEngine) OpenCache(cell.Cell) string { return e.cache }

// countingStore is the relay: it holds every object of the world and records
// each rid it is asked for.
type countingStore struct {
	blobstore.Store
	mu    sync.Mutex
	asked map[string]int
}

func (s *countingStore) GetMany(ctx context.Context, rids []string) ([]blobstore.Object, error) {
	s.mu.Lock()
	for _, rid := range rids {
		s.asked[rid]++
	}
	s.mu.Unlock()
	return s.Store.GetMany(ctx, rids)
}

// open stages the head onto a stage folder that already holds local, an older
// copy of the chat's records, the way a warm receiver's stage is seeded.
func (w *openWorld) open(local map[string][]byte) (stage string, store *countingStore, f *Fetcher) {
	w.t.Helper()
	stage = w.t.TempDir()
	for rel, body := range local {
		p := filepath.Join(stage, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			w.t.Fatal(err)
		}
		if err := os.WriteFile(p, body, 0o600); err != nil {
			w.t.Fatal(err)
		}
	}
	mem := blobstore.NewMemory()
	var objs []blobstore.Object
	stageInbox := filepath.Join(w.t.TempDir(), "stage")
	if err := os.MkdirAll(stageInbox, 0o700); err != nil {
		w.t.Fatal(err)
	}
	for rid, o := range w.all {
		objs = append(objs, o)
		if w.carried[rid] {
			if err := writeWhole(stageInbox, o); err != nil {
				w.t.Fatal(err)
			}
		}
	}
	frame, err := blobstore.Encode(strings.Repeat("a", 32), objs)
	if err != nil {
		w.t.Fatal(err)
	}
	if _, err := mem.PutFrame(context.Background(), frame); err != nil {
		w.t.Fatal(err)
	}
	store = &countingStore{Store: mem, asked: map[string]int{}}
	f = &Fetcher{Engine: keyedEngine{w: w, cache: filepath.Join(w.t.TempDir(), "open")}, Store: store}
	ok, err := f.stageOpenFromInbox(context.Background(), cell.Cell{ID: "chat", Root: stage}, w.head, stageInbox, filepath.Join(w.t.TempDir(), "main"))
	if err != nil || !ok {
		w.t.Fatalf("stageOpenFromInbox = %v, %v; want the open state staged", ok, err)
	}
	return stage, store, f
}

func read(t *testing.T, root, rel string) []byte {
	t.Helper()
	got, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// A same-length stale transcript on this disk must never be passed off as the
// chunk the head names. The head's second chunk is held only by the relay; the
// local file has the right length and the wrong bytes.
func TestOpenNeverFabricatesAChunkFromASameLengthStaleLocalFile(t *testing.T) {
	w := newOpenWorld(t)
	first := bytes.Repeat([]byte("first chunk of the transcript\n"), 40)
	second := bytes.Repeat([]byte("B turn: the far side wrote this\n"), 40)
	stale := bytes.Repeat([]byte("A"), len(second))
	if len(stale) != len(second) || bytes.Equal(stale, second) {
		t.Fatalf("fixture: stale %d bytes, second %d bytes", len(stale), len(second))
	}
	meta := w.file([][]byte{[]byte(`{"id":"chat"}`)}, true, func(int) bool { return true })
	transcript := w.file([][]byte{first, second}, true, func(i int) bool { return i == 0 })
	w.seal(w.dir(fileEntry("meta.json", meta), fileEntry("transcript.jsonl", transcript)))

	stage, _, _ := w.open(map[string][]byte{
		".cell/meta.json":        []byte(`{"id":"chat"}`),
		".cell/transcript.jsonl": append(append([]byte{}, first...), stale...),
	})
	want := append(append([]byte{}, first...), second...)
	if got := read(t, stage, ".cell/transcript.jsonl"); !bytes.Equal(got, want) {
		t.Fatalf("the transcript the chat opens on is not the head's: it holds the stale copy's second chunk (%d bytes, head has %d)", len(got), len(want))
	}
}

// A file the head changed, whose metadata the head's frame does not carry, is
// not "unchanged because it exists": it is fetched.
func TestOpenFetchesAChangedFileTheFrameDidNotCarryInsteadOfKeepingTheLocalOne(t *testing.T) {
	w := newOpenWorld(t)
	meta := w.file([][]byte{[]byte(`{"id":"chat"}`)}, true, func(int) bool { return true })
	settings := w.file([][]byte{[]byte(`{"model":"new"}`)}, false, nil)
	w.seal(w.dir(fileEntry("meta.json", meta), fileEntry("settings.json", settings)))

	stage, _, _ := w.open(map[string][]byte{
		".cell/meta.json":     []byte(`{"id":"chat"}`),
		".cell/settings.json": []byte(`{"model":"old"}`),
	})
	if got := string(read(t, stage, ".cell/settings.json")); got != `{"model":"new"}` {
		t.Fatalf(".cell/settings.json = %q, want the head's %q", got, `{"model":"new"}`)
	}
}

// The same for a folder the frame did not describe: the receiver's own env
// folder is not the head's just because a folder of that name exists.
func TestOpenFetchesTheEnvFolderTheFrameDidNotCarryInsteadOfKeepingTheLocalOne(t *testing.T) {
	w := newOpenWorld(t)
	meta := w.file([][]byte{[]byte(`{"id":"chat"}`)}, true, func(int) bool { return true })
	vars := w.file([][]byte{[]byte("KEY=new\n")}, true, func(int) bool { return true })
	env := w.meta(3, wDir{Entries: []wEntry{fileEntry("vars", vars)}, Pages: []any{}}, false)
	w.seal(w.dir(fileEntry("meta.json", meta), dirEntry("env", env)))

	stage, _, _ := w.open(map[string][]byte{
		".cell/meta.json": []byte(`{"id":"chat"}`),
		".cell/env/vars":  []byte("KEY=old\n"),
	})
	if got := string(read(t, stage, ".cell/env/vars")); got != "KEY=new\n" {
		t.Fatalf(".cell/env/vars = %q, want the head's %q", got, "KEY=new\n")
	}
}

// Nothing changed: the local file IS the head's bytes, so the open reuses it
// and asks the relay for neither its metadata nor its chunk.
func TestOpenReusesAnUnchangedLocalFileWithoutAskingTheRelay(t *testing.T) {
	w := newOpenWorld(t)
	body := bytes.Repeat([]byte("unchanged transcript line\n"), 60)
	meta := w.file([][]byte{[]byte(`{"id":"chat"}`)}, true, func(int) bool { return true })
	transcript := w.file([][]byte{body}, true, nil)
	w.seal(w.dir(fileEntry("meta.json", meta), fileEntry("transcript.jsonl", transcript)))

	stage, store, f := w.open(map[string][]byte{
		".cell/meta.json":        []byte(`{"id":"chat"}`),
		".cell/transcript.jsonl": body,
	})
	if got := read(t, stage, ".cell/transcript.jsonl"); !bytes.Equal(got, body) {
		t.Fatalf("transcript changed: %d bytes, want %d", len(got), len(body))
	}
	if len(store.asked) != 0 || f.OpenRounds() != 0 {
		t.Fatalf("an unchanged open asked the relay for %d objects in %d rounds, want none", len(store.asked), f.OpenRounds())
	}
}

// The workspace's instruction files come in the open wave, under a bound: small
// ones are staged for the take to place, and a head whose instruction files are
// larger than the bound leaves them to the workspace arriving behind the open
// without failing the open or fetching their chunks.
func TestOpenWaveStagesSmallInstructionFilesAndLeavesLargeOnesBehind(t *testing.T) {
	rules := []byte("# house rules\nrun the gate\n")
	for _, c := range []struct {
		name   string
		agents []byte
		staged bool
	}{
		{"small", rules, true},
		{"larger than the bound", bytes.Repeat([]byte("long rules line\n"), (openInstructionBudget/16)+64), false},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := newOpenWorld(t)
			meta := w.file([][]byte{[]byte(`{"id":"chat"}`)}, true, func(int) bool { return true })
			agents := w.file([][]byte{c.agents}, false, nil)
			w.seal(w.dir(fileEntry("meta.json", meta)), fileEntry("AGENTS.md", agents))

			stage, store, _ := w.open(nil)
			got, err := os.ReadFile(filepath.Join(stage, OpenInstructionDir, "AGENTS.md"))
			if c.staged {
				if err != nil || !bytes.Equal(got, c.agents) {
					t.Fatalf("AGENTS.md was not staged with the open wave: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("an instruction file over the %d-byte bound was staged", openInstructionBudget)
			}
			chunk := furrowChunkID(c.agents)
			if store.asked[chunkRID(w.dedup, chunk)] != 0 {
				t.Fatalf("the open fetched the chunks of an instruction file over the bound")
			}
		})
	}
}
