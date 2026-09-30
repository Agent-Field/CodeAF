//go:build engine

package cellsync

import (
	"context"
	"crypto/rand"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/furrow"
)

// The own-fleet dedup rig: one identity (one pair of secrets) publishing from
// several chats and machines into one relay store, on the real engine. Every
// test measures what crossed the wire, because "the store dedups it" is only
// worth something if the client also never sends it.

// machine is one computer of the identity: its own engine data root, so its own
// ledger of what it sent, and nothing else in common with the others.
type machine struct {
	t    *testing.T
	bin  string
	data string
	keys cellstore.SyncKeys
	url  string
}

// wire is what one publish put on the wire.
type wire struct{ Puts, Has, Bytes, Objects int64 }

func (w wire) String() string {
	return fmt.Sprintf("%d bytes in %d frame puts (%d objects), %d has requests", w.Bytes, w.Puts, w.Objects, w.Has)
}

func newIdentityKeys(t *testing.T) cellstore.SyncKeys {
	t.Helper()
	k := cellstore.SyncKeys{CellKey: make([]byte, 32), Dedup: make([]byte, 32)}
	_, _ = rand.Read(k.CellKey)
	_, _ = rand.Read(k.Dedup)
	return k
}

func newMachine(t *testing.T, keys cellstore.SyncKeys) *machine {
	t.Helper()
	bin, err := furrow.ResolveOwned()
	if err != nil {
		t.Skipf("no engine binary: %v", err)
	}
	return &machine{t: t, bin: bin, data: t.TempDir(), keys: keys, url: "http://relay"}
}

func (m *machine) local() cellstore.Engine {
	return cellstore.Engine{Binary: m.bin, DataRoot: m.data, Transport: cellstore.Spawn{Binary: m.bin}}
}

func (m *machine) sync() cellstore.SyncEngine {
	local := m.local()
	return cellstore.SyncEngine{
		Transport: cellstore.Spawn{Binary: m.bin},
		Keys:      m.keys,
		Ledger:    cellstore.LedgerName(m.url, "id_test"),
		Target: func(c cell.Cell) cellstore.Target {
			return cellstore.Target{DataDir: local.LocalDir(c), Tree: c.Root}
		},
		Outbox: func(cell.Cell) string { return m.t.TempDir() },
	}
}

// chat makes a new chat whose folder holds files, and seals it.
func (m *machine) chat(files map[string][]byte) (cell.Cell, string) {
	m.t.Helper()
	c, err := cell.CreateIn(m.t.TempDir(), cell.Options{Class: cell.Sandboxed})
	if err != nil {
		m.t.Fatal(err)
	}
	m.write(c, files)
	return c, m.seal(c)
}

func (m *machine) write(c cell.Cell, files map[string][]byte) {
	m.t.Helper()
	for name, body := range files {
		path := filepath.Join(c.Root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			m.t.Fatal(err)
		}
		if err := os.WriteFile(path, body, 0o600); err != nil {
			m.t.Fatal(err)
		}
	}
	restampAll(m.t, c, preserved)
}

// restampAll gives every file and directory of the chat one modification time,
// as cp -a keeps them: a tree records the time of each entry it holds.
func restampAll(t *testing.T, c cell.Cell, at time.Time) {
	t.Helper()
	err := filepath.WalkDir(c.Root, func(path string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return os.Chtimes(path, at, at)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func (m *machine) seal(c cell.Cell) string {
	m.t.Helper()
	sealed, err := m.local().Seal(context.Background(), c, cellstore.TurnInfo{})
	if err != nil {
		m.t.Fatal(err)
	}
	return sealed.Turn.ID
}

// publish uploads head to store and reports what crossed the wire.
func (m *machine) publish(store blobstore.Store, c cell.Cell, head string) wire {
	m.t.Helper()
	n := &blobstore.Counters{}
	pub := &Publisher{Engine: m.sync(), Store: blobstore.Counting{Inner: store, C: n}}
	if _, err := pub.Upload(context.Background(), c, head); err != nil {
		m.t.Fatal(err)
	}
	return wire{n.Puts.Load(), n.Has.Load(), n.BytesUp.Load(), n.ObjectsUp.Load()}
}

// tree is a stand-in for a node_modules copy: many files of a few tens of KiB.
func tree(files, size int) map[string][]byte {
	out := make(map[string][]byte, files)
	for i := 0; i < files; i++ {
		body := make([]byte, size)
		_, _ = rand.Read(body)
		out[fmt.Sprintf("node_modules/pkg%03d/index.js", i)] = body
	}
	return out
}

func (w wire) total() int64 { return w.Bytes }

// maxHasRequests is the most Has requests one flush of n objects may make.
func maxHasRequests(n int64) int64 { return (n + blobstore.MaxHas - 1) / blobstore.MaxHas }

// preserved is the modification time every file of a copy made the way cp -a
// makes it carries. A tree records each entry's time, so a copy that does not
// keep times is different content to the engine and is measured on its own.
var preserved = time.Unix(1_700_000_000, 0)

const (
	treeFiles = 1600
	treeBytes = 32 << 10 // 1600 x 32 KiB = 50 MiB
)

// TestOwnFleetSecondChatSendsNothingRealEngine is the same tree in a new chat of
// the same identity on the same machine.
func TestOwnFleetSecondChatSendsNothingRealEngine(t *testing.T) {
	m := newMachine(t, newIdentityKeys(t))
	store := blobstore.NewMemory()
	files := tree(treeFiles, treeBytes)
	a, headA := m.chat(files)
	first := m.publish(store, a, headA)
	b, headB := m.chat(files)
	second := m.publish(store, b, headB)
	t.Logf("scenario 1 same machine, new chat: first %s; second %s", first, second)
	assertOverheadOnly(t, first, second)
}

// TestOwnFleetCopyWithNewTimesSendsOnlyTreesRealEngine is the same tree copied
// the way cp -r makes it, every file stamped with a new time. Each directory
// records its entries' times, so the trees are new content and are sent; the
// file data, which is where the megabytes are, must not be.
func TestOwnFleetCopyWithNewTimesSendsOnlyTreesRealEngine(t *testing.T) {
	m := newMachine(t, newIdentityKeys(t))
	store := blobstore.NewMemory()
	files := tree(treeFiles, treeBytes)
	a, headA := m.chat(files)
	first := m.publish(store, a, headA)
	b, _ := m.chat(files)
	restampAll(t, b, time.Now())
	second := m.publish(store, b, m.seal(b))
	t.Logf("scenario 1b copy with new file times: first %s; second %s", first, second)
	if limit := first.Bytes / 20; second.Bytes > limit {
		t.Errorf("a copy with new times sent %d bytes, file data must not cross again (limit %d)", second.Bytes, limit)
	}
}

// TestOwnFleetSecondMachineSendsNothingRealEngine is the same tree in a chat on
// a second computer of the identity: its ledger is empty, so only the store can
// say what is already held.
func TestOwnFleetSecondMachineSendsNothingRealEngine(t *testing.T) {
	keys := newIdentityKeys(t)
	one, two := newMachine(t, keys), newMachine(t, keys)
	store := blobstore.NewMemory()
	files := tree(treeFiles, treeBytes)
	a, headA := one.chat(files)
	first := one.publish(store, a, headA)
	b, headB := two.chat(files)
	second := two.publish(store, b, headB)
	t.Logf("scenario 2 second machine, same identity: first %s; second %s", first, second)
	assertOverheadOnly(t, first, second)
}

// TestOwnFleetRenameSendsNothingRealEngine moves a directory inside one
// workspace and seals again: the content is unchanged, only trees are new.
func TestOwnFleetRenameSendsNothingRealEngine(t *testing.T) {
	m := newMachine(t, newIdentityKeys(t))
	store := blobstore.NewMemory()
	a, head := m.chat(tree(treeFiles, treeBytes))
	first := m.publish(store, a, head)
	if err := os.Rename(filepath.Join(a.Root, "node_modules"), filepath.Join(a.Root, "vendor")); err != nil {
		t.Fatal(err)
	}
	second := m.publish(store, a, m.seal(a))
	t.Logf("scenario 3 rename inside one workspace: first %s; second %s", first, second)
	assertOverheadOnly(t, first, second)
}

// TestOwnFleetIdenticalFilesInOneSealRealEngine seals two copies of one file
// and sees the bytes cross once.
func TestOwnFleetIdenticalFilesInOneSealRealEngine(t *testing.T) {
	m := newMachine(t, newIdentityKeys(t))
	body := tree(1, 4<<20)["node_modules/pkg000/index.js"]
	one, headOne := m.chat(map[string][]byte{"a/file": body})
	single := m.publish(blobstore.NewMemory(), one, headOne)
	two, headTwo := m.chat(map[string][]byte{"a/file": body, "b/file": body})
	double := m.publish(blobstore.NewMemory(), two, headTwo)
	t.Logf("scenario 4 identical files in one seal: one copy %s; two copies %s", single, double)
	if double.Bytes > single.Bytes+askFloor {
		t.Fatalf("two identical files sent %d bytes, one sent %d", double.Bytes, single.Bytes)
	}
}

// TestOwnFleetRotationSharesNothingRealEngine pins the design: a rotated
// identity has new secrets and an empty namespace, so every byte is sent again
// and no rid of the old identity can be found in the new one.
func TestOwnFleetRotationSharesNothingRealEngine(t *testing.T) {
	files := tree(200, treeBytes)
	old, fresh := newMachine(t, newIdentityKeys(t)), newMachine(t, newIdentityKeys(t))
	oldStore, newStore := blobstore.NewMemory(), blobstore.NewMemory()
	a, headA := old.chat(files)
	first := old.publish(oldStore, a, headA)
	b, headB := fresh.chat(files)
	second := fresh.publish(newStore, b, headB)
	t.Logf("scenario 5 after rotation: old identity %s; new identity %s", first, second)
	if second.Bytes < first.Bytes/2 {
		t.Fatalf("a rotated identity sent only %d bytes of %d: the keys must not converge", second.Bytes, first.Bytes)
	}
}

// assertOverheadOnly is the acceptance: the second copy sends frame overhead and
// the changed trees at most, and asks the store in batches, never per object.
func assertOverheadOnly(t *testing.T, first, second wire) {
	t.Helper()
	if limit := first.Bytes / 100; second.Bytes > limit {
		t.Errorf("second copy sent %d bytes, more than 1%% of the first (%d)", second.Bytes, first.Bytes)
	}
	if limit := maxHasRequests(first.Objects); second.Has > limit {
		t.Errorf("second copy made %d has requests, limit %d", second.Has, limit)
	}
}
