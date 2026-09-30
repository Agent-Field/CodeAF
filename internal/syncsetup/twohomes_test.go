package syncsetup

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/keys"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/vaultsync"
)

// The two-home rig: one identity on two machines that share nothing but a relay
// with a store, in this process, with the real engine over spawn and a scripted
// model. Machine A is "spark", machine B is "blackmac". B never sees A's disk,
// git remote or credentials: everything it gets, it gets from the relay.

const (
	nameA = "spark"
	nameB = "blackmac"
)

// twoHomes is a driveRig with what the take side needs on top of it.
type twoHomes struct {
	*driveRig
	engB   cellstore.Engine // B's engine: its own data root, no workspace of its own
	rootsB string           // where B keeps chats it takes
	block  *blocker         // A's store, which can be made to fail
	quiet  atomic.Bool      // A's heartbeats alone are refused; its uploads still reach the relay
	wall   *noticeLog
	link   string // the project path A's session record names; a link to A's folder, removed while B has the chat
}

func newTwoHomes(t *testing.T) *twoHomes {
	t.Helper()
	r := newDriveRig(t)
	h := &twoHomes{driveRig: r, rootsB: t.TempDir(), wall: &noticeLog{}}
	h.block = &blocker{Store: r.a.Store}
	r.a.Store = h.block
	r.a.Dir = offlineDir{Client: r.a.Dir, down: func() bool { return h.block.down.Load() || h.quiet.Load() }}
	h.engB = cellstore.EngineFor("")
	h.engB.DataRoot, h.engB.Binary, h.engB.Transport = t.TempDir(), r.bin, cellstore.Spawn{Binary: r.bin}
	r.sealMeta()
	h.link = h.work + ".link"
	h.pointRecordAtLink()
	h.backToA()
	return h
}

// pointRecordAtLink makes A's session record name the project by the link that
// awayFromA can remove, so A's own path stays where it is.
func (h *twoHomes) pointRecordAtLink() {
	h.t.Helper()
	m, err := session.LoadMeta(h.cell.Root)
	if err != nil {
		h.t.Fatal(err)
	}
	m.Workspace = h.link
	if err := session.SaveMeta(h.cell.Root, m); err != nil {
		h.t.Fatal(err)
	}
}

// sealMeta gives A's chat the session record a real chat always has, so the
// take side finds the project folder A works in.
func (r *driveRig) sealMeta() {
	r.t.Helper()
	err := session.SaveMeta(r.cell.Root, session.Meta{ID: r.cell.ID, Title: "two homes", Workspace: r.work, Created: time.Now()})
	if err != nil {
		r.t.Fatal(err)
	}
	header := fmt.Sprintf(`{"type":"session","version":3,"id":%q,"cwd":%q,"timestamp":"2026-09-29T10:00:00Z"}`+"\n", r.cell.ID, r.work)
	appendTo(r.t, transcriptOf(r.cell), header)
}

func transcriptOf(c cell.Cell) string { return session.Place{Dir: c.Root}.Transcript() }

func appendTo(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(text); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// blocker is a store whose uploads can be refused, as a network that is down
// for this machine only.
type blocker struct {
	blobstore.Store
	down atomic.Bool
}

func (b *blocker) set(down bool) { b.down.Store(down) }

func (b *blocker) PutFrame(ctx context.Context, frame []byte) (blobstore.FrameID, error) {
	if b.down.Load() {
		return "", blobstore.ErrUnreachable
	}
	return b.Store.PutFrame(ctx, frame)
}

// offlineDir is A's directory client while A is cut off: its heartbeats go
// nowhere, as they do on a machine whose lid is closed. Without this a lease the
// test lets lapse would be renewed by A's own prompt heartbeat before B took it.
type offlineDir struct {
	directory.Client
	down func() bool
}

func (d offlineDir) Heartbeat(ctx context.Context, id string, b directory.Beat) (directory.CellView, error) {
	if d.down() {
		return directory.CellView{}, blobstore.ErrUnreachable
	}
	return d.Client.Heartbeat(ctx, id, b)
}

// continuerA and continuerB are each machine's take side. A keeps the chat it
// started where it started it; anything else, and everything of B's, goes under
// a folder of that machine's own.
func (h *twoHomes) continuerA() *Continuer {
	h.backToA()
	base := h.engine
	base.Workspace = ""
	return h.a.Continuer(base, TakeOptions{DeviceName: nameA, Notify: h.wall.add, RootFor: func(id string) string {
		if id == h.cell.ID {
			return h.cell.Root
		}
		return filepath.Join(h.t.TempDir(), id)
	}})
}

func (h *twoHomes) continuerB() *Continuer {
	h.awayFromA()
	return h.b.Continuer(h.engB, TakeOptions{DeviceName: nameB, Notify: h.wall.add, RootFor: func(id string) string {
		return filepath.Join(h.rootsB, id)
	}})
}

// openChat is one chat open on one machine: its drive side, and a seat that
// seals every scripted call through it the way the door does.
type openChat struct {
	t     *testing.T
	drive *Drive
	seat  executor.Seat
	cell  cell.Cell
	work  string
	calls int
}

func (h *twoHomes) openOn(s *Sync, eng cellstore.Engine, c cell.Cell, work, device string) *openChat {
	return h.openObserved(s, eng, c, work, device, nil)
}

// openObserved is openOn with an observer on the seat, the way the door gives a
// chat the machine that keeps the record of what it ran.
func (h *twoHomes) openObserved(s *Sync, eng cellstore.Engine, c cell.Cell, work, device string, obs executor.Observer) *openChat {
	h.t.Helper()
	eng.Workspace = work
	title := func() string { m, _ := session.LoadMeta(c.Root); return m.Title }
	drive, err := s.Drive(context.Background(), eng, c, DriveOptions{DeviceName: device, Title: title, OnNotice: h.wall.add})
	if err != nil {
		h.t.Fatal(err)
	}
	h.t.Cleanup(func() { _ = drive.Close(context.Background()) })
	seat, err := cellstore.SeatOver(executor.HostBound, c, work, obs, nil,
		func(cellstore.Engine) cellstore.Store { return drive.Store(eng) })
	if err != nil {
		h.t.Fatal(err)
	}
	return &openChat{t: h.t, drive: drive, seat: executor.Gated(seat, drive.Gate), cell: c, work: work}
}

func (h *twoHomes) openA() *openChat {
	h.backToA()
	return h.openOn(h.a, h.engine, h.cell, h.work, nameA)
}

// say is one scripted model turn: the transcript grows by a line, a file is
// written, and the seat seals the call.
func (o *openChat) say(text string) error {
	o.calls++
	note := filepath.Join(o.work, fmt.Sprintf("turn-%s-%d.txt", o.cell.ID[len(o.cell.ID)-4:], o.calls))
	appendTo(o.t, transcriptOf(o.cell), fmt.Sprintf(`{"type":"message","role":"assistant","content":%q}`+"\n", text))
	call := executor.Call{Tool: "bash", Args: []byte(`{"command":"echo"}`)}
	return o.seat.Around(context.Background(), call, func() ([]byte, bool) {
		return nil, os.WriteFile(note, []byte(text), 0o600) != nil
	})
}

func (o *openChat) mustSay(text string) {
	o.t.Helper()
	if err := o.say(text); err != nil {
		o.t.Fatal(err)
	}
}

func headOf(t *testing.T, c cell.Cell) string {
	t.Helper()
	h, err := cellstore.Head(c)
	if err != nil || h == nil {
		t.Fatalf("no head at %s: %v", c.Root, err)
	}
	return h.Turn.ID
}

// durable waits until the directory holds the chat's own head.
func (h *twoHomes) durable(id string, c cell.Cell) {
	h.t.Helper()
	want := headOf(h.t, c)
	waitFor(h.t, "the directory to hold the chat's head", func() bool { return h.directoryHead(id) == want })
}

// seedTree writes what a real project holds: text, a binary file, an
// executable, a link, an untracked folder and a .env whose secrets are also in
// the vault.
func seedTree(t *testing.T, work string) {
	t.Helper()
	blob := make([]byte, 0, 9000)
	for i := 0; i < 9000; i++ {
		blob = append(blob, byte(i*31))
	}
	files := []struct {
		path string
		body []byte
		mode fs.FileMode
	}{
		{"README.md", []byte("# project\n"), 0o644},
		{"src/main.go", []byte("package main\n\nfunc main() {}\n"), 0o644},
		{"bin/run.sh", []byte("#!/bin/sh\necho hi\n"), 0o755},
		{"data/blob.bin", blob, 0o600},
		{"untracked/notes.txt", []byte("not in any repository\n"), 0o644},
		{".env", []byte(envBody), 0o644},
		{"web/client/app/.env.production", []byte("# prod\nAPI_URL='https://x'\n"), 0o640},
	}
	for _, f := range files {
		path := filepath.Join(work, f.path)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, f.body, f.mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, f.mode); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("README.md", filepath.Join(work, "readme-link")); err != nil {
		t.Fatal(err)
	}
}

// vaultTheEnv puts the .env of A's workspace in A's vault under the chat's scope.
func (h *twoHomes) vaultTheEnv() {
	h.t.Helper()
	v, err := keys.Open(h.homeA)
	if err != nil {
		h.t.Fatal(err)
	}
	if n, err := v.ImportDotenv(filepath.Join(h.work, ".env"), keys.ScopeOf(h.cell)); err != nil || n != 2 {
		h.t.Fatalf("ImportDotenv = %d, %v", n, err)
	}
}

// entry is one path of a tree as a byte-for-byte comparison sees it.
type entry struct {
	mode fs.FileMode
	body string // the bytes of a file, the target of a link
}

// readTree reads every path under dir except the engine's own marker.
func readTree(t *testing.T, dir string) map[string]entry {
	t.Helper()
	out := map[string]entry{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		rel, _ := filepath.Rel(dir, path)
		if err != nil || rel == "." || rel == ".furrow" || strings.HasPrefix(rel, ".furrow"+string(filepath.Separator)) || d.IsDir() {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		body, err := bodyOf(path, info)
		out[rel] = entry{mode: info.Mode(), body: string(body)}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func bodyOf(path string, info fs.FileInfo) ([]byte, error) {
	if info.Mode()&fs.ModeSymlink != 0 {
		target, err := os.Readlink(path)
		return []byte(target), err
	}
	return os.ReadFile(path)
}

func sameTree(t *testing.T, what string, want, got map[string]entry) {
	t.Helper()
	if len(want) == 0 {
		t.Fatalf("%s: nothing to compare", what)
	}
	for path, w := range want {
		g, ok := got[path]
		switch {
		case !ok:
			t.Errorf("%s: %s is missing", what, path)
		case g.mode != w.mode:
			t.Errorf("%s: %s has mode %v, want %v", what, path, g.mode, w.mode)
		case g.body != w.body:
			t.Errorf("%s: %s differs (%d bytes, want %d)", what, path, len(g.body), len(w.body))
		}
	}
	for path := range got {
		if _, ok := want[path]; !ok {
			t.Errorf("%s: %s should not be there", what, path)
		}
	}
}

func rowOf(t *testing.T, s *Sync, id string) (chatlist.Row, bool) {
	t.Helper()
	rows, err := s.Rows(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.Cell == id {
			return row, true
		}
	}
	return chatlist.Row{}, false
}

func branchRowOf(t *testing.T, s *Sync, parent string) (chatlist.Row, bool) {
	t.Helper()
	rows, err := s.Rows(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.Status == chatlist.Branch && row.Parent == parent {
			return row, true
		}
	}
	return chatlist.Row{}, false
}

// lapse lets every lease run out, as a lid closed for longer than the TTL.
func (h *twoHomes) lapse() { h.clock.advance(directory.LeaseTTL + time.Second) }

// fetchInto materializes head on a scratch machine that has never seen it and
// reads the tree back: what anyone with the identity would get from the relay.
func (h *twoHomes) fetchInto(head string) map[string]entry {
	h.t.Helper()
	h.awayFromA()
	eng := h.engB
	eng.DataRoot = h.t.TempDir()
	root := filepath.Join(h.t.TempDir(), h.cell.ID)
	fetcher := h.b.Continuer(eng, TakeOptions{RootFor: func(string) string { return root }})
	stage := cell.Cell{ID: h.cell.ID, Root: root}
	if err := os.MkdirAll(root, 0o700); err != nil {
		h.t.Fatal(err)
	}
	if err := fetcher.taker(h.b.scope(h.cell.ID)).Fetch.Fetch(context.Background(), stage, head); err != nil {
		h.t.Fatalf("the head is not in the store: %v", err)
	}
	return readTree(h.t, workspaceOf(root))
}

// TestTwoHomesL8ByteIdentical is L8 end to end: A seals several turns of a real
// tree, B has nothing but the relay, and what B takes is A's tree byte for byte,
// with the modes, the links, the transcript, and the .env put back from the
// vault.
func TestTwoHomesL8ByteIdentical(t *testing.T) {
	h := newTwoHomes(t)
	ctx := context.Background()
	seedTree(t, h.work)
	h.vaultTheEnv()

	a := h.openA()
	a.mustSay("first")
	a.mustSay("second")
	a.mustSay("third")
	h.durable(h.cell.ID, h.cell)
	if err := a.drive.Close(ctx); err != nil {
		t.Fatal(err)
	}

	row, ok := rowOf(t, h.b, h.cell.ID)
	if !ok || row.Status != chatlist.Idle || row.Title != "two homes" {
		t.Fatalf("B lists %+v, %v; want A's released chat under the title it was sealed with", row, ok)
	}

	got, err := h.continuerB().Take(ctx, h.cell.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Taken.Kept != "" || got.KeptTurns != 0 || got.Device != nameB {
		t.Fatalf("Continued = %+v; nothing was kept and this machine is %s", got, nameB)
	}
	rootB := got.Taken.Cell.Root
	sameTree(t, "the tree", readTree(t, h.work), readTree(t, workspaceOf(rootB)))

	wantLog, _ := os.ReadFile(transcriptOf(h.cell))
	gotLog, err := os.ReadFile(transcriptOf(got.Taken.Cell))
	if err != nil || !bytes.Equal(gotLog, wantLog) || len(wantLog) == 0 {
		t.Fatalf("transcript on B = %d bytes (%v), want A's %d bytes", len(gotLog), err, len(wantLog))
	}
	assertAdoptedHead(t, h, got.Taken.Cell)
	assertEnvInjected(t, workspaceOf(rootB))
	if m, _ := session.LoadMeta(rootB); m.ID != h.cell.ID {
		t.Fatalf("B lists no session row for the chat it took: %+v", m)
	}
}

func assertEnvInjected(t *testing.T, work string) {
	t.Helper()
	path := filepath.Join(work, ".env")
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o644 {
		t.Fatalf(".env on B = %v, %v; want A's file at mode 0644", info, err)
	}
	if got, _ := os.ReadFile(path); string(got) != envBody {
		t.Fatalf(".env on B = %q", got)
	}
	deep := filepath.Join(work, "web/client/app/.env.production")
	if info, err := os.Stat(deep); err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("the nested dotenv on B = %v, %v; want it back at mode 0640", info, err)
	}
	if got, _ := os.ReadFile(deep); string(got) != "# prod\nAPI_URL='https://x'\n" {
		t.Fatalf("the nested dotenv on B = %q", got)
	}
}

// TestTwoHomesL12DeathBranches is L12 end to end: three turns are sealed while
// A's upload is blocked, B takes the chat over and seals two more, and when A
// returns its three turns become a child cell of the chat. Nothing is
// overwritten, both machines list the branch, and discarding it archives it.
func TestTwoHomesL12DeathBranches(t *testing.T) {
	h := newTwoHomes(t)
	ctx := context.Background()
	seedTree(t, h.work)
	a := h.openA()
	a.mustSay("durable")
	h.durable(h.cell.ID, h.cell)

	h.block.set(true)
	a.mustSay("lost one")
	a.mustSay("lost two")
	a.mustSay("lost three")
	orphanHead := headOf(t, h.cell)
	h.lapse()

	took, err := h.continuerB().Take(ctx, h.cell.ID)
	if err != nil {
		t.Fatal(err)
	}
	b := h.openOn(h.b, h.engB, took.Taken.Cell, workspaceOf(took.Taken.Cell.Root), nameB)
	b.mustSay("b one")
	b.mustSay("b two")
	h.durable(h.cell.ID, took.Taken.Cell)
	parentHead := headOf(t, took.Taken.Cell)

	h.block.set(false)
	waitFor(t, "A to learn it was superseded", func() bool { _, viewer := a.drive.Viewer(); return viewer })

	v, err := h.a.Dir.Cell(ctx, h.cell.ID)
	if err != nil || v.Cell.Head != parentHead || v.Cell.Lease.Device != h.b.Device.ID() {
		t.Fatalf("the chat = %+v (%v); want B's head %s and B holding it", v.Cell, err, parentHead)
	}
	branch, ok := branchRowOf(t, h.a, h.cell.ID)
	if !ok || branch.OrphanTurns != 3 || branch.Device != nameA {
		t.Fatalf("A's branch row = %+v, %v; want 3 turns from %s", branch, ok, nameA)
	}
	rec, err := h.a.Dir.Cell(ctx, branch.Cell)
	if err != nil || rec.Cell.ParentCell != h.cell.ID || rec.Cell.Head != orphanHead || rec.Cell.OrphanTurns != 3 {
		t.Fatalf("branch record = %+v (%v); want parent %s, head %s, 3 turns", rec.Cell, err, h.cell.ID, orphanHead)
	}
	if got, ok := branchRowOf(t, h.b, h.cell.ID); !ok || got.Cell != branch.Cell || got.Device != nameA {
		t.Fatalf("B's branch row = %+v, %v; want the same branch from %s", got, ok, nameA)
	}
	if _, err := os.Stat(filepath.Join(h.work, "turn-"+h.cell.ID[len(h.cell.ID)-4:]+"-4.txt")); err != nil {
		t.Fatalf("A's own tree lost its last turn: %v", err)
	}
	if kept := h.fetchInto(orphanHead); kept["turn-"+h.cell.ID[len(h.cell.ID)-4:]+"-4.txt"].body != "lost three" {
		t.Fatalf("the branch head does not hold A's third orphan turn: %v", kept)
	}
	assertNotOverwritten(t, h, took.Taken.Cell)

	if err := h.b.Discard(ctx, h.cell.ID); err == nil {
		t.Fatal("the chat itself was discarded as if it were a branch")
	}
	if err := h.b.Discard(ctx, branch.Cell); err != nil {
		t.Fatal(err)
	}
	for _, s := range []*Sync{h.a, h.b} {
		if _, ok := branchRowOf(t, s, h.cell.ID); ok {
			t.Fatal("a discarded branch is still listed")
		}
	}
}

// assertNotOverwritten checks B's copy still says what B sealed.
func assertNotOverwritten(t *testing.T, h *twoHomes, c cell.Cell) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(workspaceOf(c.Root), "turn-"+c.ID[len(c.ID)-4:]+"-2.txt")); err != nil {
		t.Fatalf("B's own turn is gone: %v", err)
	}
}

// bContinued is the scene both take-back tests start from: A sealed two turns
// and let go, B took the chat, sealed one turn of its own and let go.
func bContinued(t *testing.T) *twoHomes {
	t.Helper()
	h := newTwoHomes(t)
	ctx := context.Background()
	seedTree(t, h.work)
	a := h.openA()
	a.mustSay("a one")
	a.mustSay("a two")
	h.durable(h.cell.ID, h.cell)
	if err := a.drive.Close(ctx); err != nil {
		t.Fatal(err)
	}
	took, err := h.continuerB().Take(ctx, h.cell.ID)
	if err != nil {
		t.Fatal(err)
	}
	b := h.openOn(h.b, h.engB, took.Taken.Cell, workspaceOf(took.Taken.Cell.Root), nameB)
	b.mustSay("b one")
	h.durable(h.cell.ID, took.Taken.Cell)
	if err := b.drive.Close(ctx); err != nil {
		t.Fatal(err)
	}
	return h
}

// sameFolder asserts a path is still the very directory it was: editors and git
// hold a project folder open, so it is never renamed or replaced.
func sameFolder(t *testing.T, path string, before os.FileInfo) {
	t.Helper()
	after, err := os.Stat(path)
	if err != nil || !os.SameFile(before, after) {
		t.Fatalf("%s is not the folder it was (%v)", path, err)
	}
}

func inodeOf(t *testing.T, path string) os.FileInfo {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info
}

// TestTwoHomesTakeBackIntoTheProjectFolder: A's chat works in the person's own
// project folder, so when A takes the chat back B's turn lands in that folder,
// where the person is looking, and neither it nor the chat's folder is replaced.
func TestTwoHomesTakeBackIntoTheProjectFolder(t *testing.T) {
	h := bContinued(t)
	project, root := inodeOf(t, h.work), inodeOf(t, h.cell.Root)

	back, err := h.continuerA().Take(context.Background(), h.cell.ID)
	if err != nil {
		t.Fatal(err)
	}
	if back.Taken.Kept != "" {
		t.Fatalf("a clean folder kept %s", back.Taken.Kept)
	}
	sameFolder(t, h.work, project)
	sameFolder(t, h.cell.Root, root)
	if back.Taken.Cell.Root != h.cell.Root || workspaceOf(h.cell.Root) != h.link {
		t.Fatalf("the chat moved to %s / %s; it works in %s", back.Taken.Cell.Root, workspaceOf(h.cell.Root), h.work)
	}
	if _, err := os.Stat(filepath.Join(h.work, "turn-"+h.cell.ID[len(h.cell.ID)-4:]+"-1.txt")); err != nil {
		t.Fatalf("B's turn did not reach the project folder: %v", err)
	}
	if _, err := os.Stat(filepath.Join(h.cell.Root, "work")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a hidden copy was made (%v)", err)
	}
	if _, err := os.Stat(h.cell.Root + ".taking"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the staging folder is still there (%v)", err)
	}
	if m, _ := session.LoadMeta(h.cell.Root); m.ID != h.cell.ID || m.Workspace != h.link {
		t.Fatalf("the session record lost its project: %+v", m)
	}
}

// TestTwoHomesTakeBackKeepsHandEdit is the no-overwrite law at the take-back: A
// edits a file by hand after B continued the chat, takes the chat back, and the
// edit is sealed and kept as a branch before B's head is restored into the
// folder, in place.
func TestTwoHomesTakeBackKeepsHandEdit(t *testing.T) {
	h := bContinued(t)
	ctx := context.Background()
	project := inodeOf(t, h.work)
	if err := os.WriteFile(filepath.Join(h.work, "README.md"), []byte("# hand edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	back, err := h.continuerA().Take(ctx, h.cell.ID)
	if err != nil {
		t.Fatal(err)
	}
	if back.Taken.Kept == "" || back.KeptTurns != 1 || back.Device != nameA {
		t.Fatalf("Continued = %+v; want the hand edit kept as 1 turn from %s", back, nameA)
	}
	rec, err := h.a.Dir.Cell(ctx, back.Taken.Kept)
	if err != nil || rec.Cell.ParentCell != h.cell.ID || rec.Cell.OrphanTurns != 1 {
		t.Fatalf("kept branch = %+v (%v); want a child of the chat with 1 turn", rec.Cell, err)
	}
	if kept := h.fetchInto(rec.Cell.Head); kept["README.md"].body != "# hand edit\n" {
		t.Fatalf("the branch holds README.md = %q; want the hand edit", kept["README.md"].body)
	}
	sameFolder(t, h.work, project)
	if got, _ := os.ReadFile(filepath.Join(h.work, "README.md")); string(got) != "# project\n" {
		t.Fatalf("the project folder holds README.md = %q; want B's head, the edit being in the branch", got)
	}
	if _, err := os.Stat(filepath.Join(h.work, "turn-"+h.cell.ID[len(h.cell.ID)-4:]+"-1.txt")); err != nil {
		t.Fatalf("B's turn is not in the project folder: %v", err)
	}
}

// TestTwoHomesTakeARunningChatAtOnce: B continues a chat A is still running. B
// does not wait for A's lease to run out; it takes the chat at once, A's next
// turn is refused as superseded, and what A had not yet sent becomes a branch.
func TestTwoHomesTakeARunningChatAtOnce(t *testing.T) {
	h := newTwoHomes(t)
	ctx := context.Background()
	seedTree(t, h.work)
	a := h.openA()
	a.mustSay("a one")
	h.durable(h.cell.ID, h.cell)

	row, ok := rowOf(t, h.b, h.cell.ID)
	if !ok || row.Status != chatlist.Running || chatlist.OfferFor(row).Kind != chatlist.ContinueHere {
		t.Fatalf("B's row = %+v, %v; want a running chat that offers continue here", row, ok)
	}
	if _, err := h.continuerB().Take(ctx, h.cell.ID); err != nil {
		t.Fatalf("Take of a running chat = %v; want it taken at once", err)
	}
	if v, _ := h.a.Dir.Cell(ctx, h.cell.ID); v.Cell.Lease.Device != h.b.Device.ID() {
		t.Fatalf("the lease is with %s, want B's", v.Cell.Lease.Device)
	}
	a.mustSay("orphaned") // A does not know it lost the chat; its publish is refused and becomes a branch
	waitFor(t, "A to learn it was superseded", func() bool { _, viewer := a.drive.Viewer(); return viewer })
	if _, ok := branchRowOf(t, h.a, h.cell.ID); !ok {
		t.Fatal("A's refused turns did not become a branch")
	}
	if err := a.drive.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

// TestTakeSideContinueHere is the whole of `continue here` from the home's
// side: A goes quiet, its row on B offers `continue here`, B takes the chat and
// drives it, and A's row says where the chat is now.
func TestTakeSideContinueHere(t *testing.T) {
	h := newTwoHomes(t)
	ctx := context.Background()
	seedTree(t, h.work)
	a := h.openA()
	a.mustSay("a one")
	h.durable(h.cell.ID, h.cell)
	h.lapse()

	row, ok := rowOf(t, h.b, h.cell.ID)
	if !ok || row.Status != chatlist.Off || chatlist.OfferFor(row).Kind != chatlist.ContinueHere {
		t.Fatalf("B's row = %+v, %v; want %s off, offering continue here", row, ok, nameA)
	}
	took, err := h.continuerB().Take(ctx, h.cell.ID)
	if err != nil {
		t.Fatal(err)
	}
	b := h.openOn(h.b, h.engB, took.Taken.Cell, workspaceOf(took.Taken.Cell.Root), nameB)
	b.mustSay("b one")
	h.durable(h.cell.ID, took.Taken.Cell)
	if row, _ := rowOf(t, h.a, h.cell.ID); row.Status != chatlist.Running || row.Device != nameB {
		t.Fatalf("A's row = %+v; want running on %s", row, nameB)
	}
	turns, _ := cellstore.Turns(took.Taken.Cell)
	if len(turns) == 0 || turns[len(turns)-1].Device != h.b.Device.Cert.Device {
		t.Fatalf("B's chain = %+v; want B's turn last, under B's key", turns)
	}
}

// assertAdoptedHead: the log of the chat B took shows A's head as one entry
// sealed on A, with no receipt, and B's next turn descends from it.
func assertAdoptedHead(t *testing.T, h *twoHomes, c cell.Cell) {
	t.Helper()
	aHead := headOf(t, h.cell)
	entries, err := cellstore.Log(c)
	if err != nil || len(entries) == 0 || !entries[0].IsAdopted || entries[0].Adopted != nameA || entries[0].Turn.ID != aHead {
		t.Fatalf("B's log = %+v, %v; want A's head %s first, sealed on %s", entries, err, aHead, nameA)
	}
	b := h.openOn(h.b, h.engB, c, workspaceOf(c.Root), nameB)
	b.mustSay("b next")
	turns, _ := cellstore.Turns(c)
	if got := turns[len(turns)-1].Parent; got != aHead {
		t.Fatalf("B's next turn has parent %s; want A's head %s", got, aHead)
	}
}

// envBody is the .env the vault tests put in A's project folder.
const envBody = "API_KEY=sk-test-123\nDB_URL=postgres://u:p@h/db\n"

// writeEnvAndVault makes the .env in A's project folder and imports it into
// A's vault, as the guard does at the first seal that finds one.
func (h *twoHomes) writeEnvAndVault() {
	h.t.Helper()
	if err := os.WriteFile(filepath.Join(h.work, ".env"), []byte(envBody), 0o644); err != nil {
		h.t.Fatal(err)
	}
	h.vaultTheEnv()
}

// takeOnB releases A's chat once it is durable and continues it on B.
func (h *twoHomes) takeOnB(a *openChat) Continued {
	h.t.Helper()
	h.durable(h.cell.ID, h.cell)
	if err := a.drive.Close(context.Background()); err != nil {
		h.t.Fatal(err)
	}
	got, err := h.continuerB().Take(context.Background(), h.cell.ID)
	if err != nil {
		h.t.Fatal(err)
	}
	return got
}

// A .env that appears after the chat has started reaches the other machine: the
// vault is sent again when a later turn is published, not only at chat start.
func TestTwoHomesEnvCreatedMidChatReachesB(t *testing.T) {
	h := newTwoHomes(t)
	seedTree(t, h.work)
	if err := os.Remove(filepath.Join(h.work, ".env")); err != nil {
		t.Fatal(err)
	}
	a := h.openA()
	a.mustSay("before any secret")
	h.durable(h.cell.ID, h.cell)

	h.writeEnvAndVault()
	a.mustSay("after the .env appeared")

	got := h.takeOnB(a)
	assertEnvInjected(t, workspaceOf(got.Taken.Cell.Root))
}

// A .env that was there before the chat started is sent as it always was.
func TestTwoHomesEnvBeforeChatReachesB(t *testing.T) {
	h := newTwoHomes(t)
	seedTree(t, h.work)
	h.vaultTheEnv()
	a := h.openA()
	a.mustSay("first")

	got := h.takeOnB(a)
	assertEnvInjected(t, workspaceOf(got.Taken.Cell.Root))
}

// A .env the person edited by hand in A's own folder after it was first vaulted
// reaches B as the edited file, and comes back to A the same, byte for byte.
func TestTwoHomesMidChatEnvEditTravelsWholeAndComesBack(t *testing.T) {
	h := newTwoHomes(t)
	seedTree(t, h.work)
	if err := os.Remove(filepath.Join(h.work, ".env")); err != nil {
		t.Fatal(err)
	}
	a := h.openA()
	a.mustSay("before any secret")
	h.writeEnvAndVault()
	mine := "API_KEY=my-own-value\nDB_URL=postgres://u:p@h/db\n"
	if err := os.WriteFile(filepath.Join(h.work, ".env"), []byte(mine), 0o644); err != nil {
		t.Fatal(err)
	}
	a.mustSay("after the .env appeared")

	took := h.takeOnB(a)
	if got, _ := os.ReadFile(filepath.Join(workspaceOf(took.Taken.Cell.Root), ".env")); string(got) != mine {
		t.Fatalf(".env on B = %q; want the edited file", got)
	}
	b := h.openOn(h.b, h.engB, took.Taken.Cell, workspaceOf(took.Taken.Cell.Root), nameB)
	b.mustSay("b one")
	h.durable(h.cell.ID, took.Taken.Cell)
	if err := b.drive.Close(context.Background()); err != nil {
		t.Fatal(err)
	}

	if _, err := h.continuerA().Take(context.Background(), h.cell.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(h.work, ".env")); string(got) != mine {
		t.Fatalf(".env on A = %q; want the same bytes back", got)
	}
}

// awayFromA takes A's project out of B's sight, as it is when B is another
// machine: the path the chat's session record names is not on B, so B keeps the
// chat in a work/ folder of its own, exactly as on a second machine. Without this
// the two homes share one disk, a take whose project path is still there lands in
// it in place (take.go InPlace, projectOf), and a take that writes nothing still
// compares equal. A's session record names the project by a link to its folder,
// and B's view is cut by removing the link, so A keeps working in the folder
// itself while B cannot resolve the path. Every take on B goes through it (see
// continuerB); backToA restores the link for A's own use.
func (h *twoHomes) awayFromA() {
	h.t.Helper()
	if err := os.Remove(h.link); err != nil && !errors.Is(err, os.ErrNotExist) {
		h.t.Fatal(err)
	}
}

// backToA makes the path A's session record names resolve again.
func (h *twoHomes) backToA() {
	h.t.Helper()
	if _, err := os.Lstat(h.link); err == nil {
		return
	}
	if err := os.Symlink(h.work, h.link); err != nil {
		h.t.Fatal(err)
	}
}

// A window on B that only started up has already merged A's vault, which holds
// the slots of every withheld file but writes none of them: the chat is not
// there yet. The take the surface offers (Continuer.Take is the one entry the
// surface, the corpus and these tests all call) must still write every withheld
// file at its path and mode, and B's next push must not delete them for A.
func TestTakeWritesWithheldFilesAfterTheTakersWindowSyncedTheVault(t *testing.T) {
	h := newTwoHomes(t)
	ctx := context.Background()
	seedTree(t, h.work)
	want := readTree(t, h.work)

	a := h.openA()
	a.mustSay("first")
	a.mustSay("second")
	h.durable(h.cell.ID, h.cell)
	if err := a.drive.Close(ctx); err != nil {
		t.Fatal(err)
	}
	h.awayFromA()
	if err := h.b.withVault(func(v vaultsync.Syncer) error { return v.Push(ctx) }); err != nil {
		t.Fatal(err)
	}

	got, err := h.continuerB().Take(ctx, h.cell.ID)
	if err != nil {
		t.Fatal(err)
	}
	work := workspaceOf(got.Taken.Cell.Root)
	assertEnvInjected(t, work)
	for _, rel := range []string{".env", "web/client/app/.env.production"} {
		if have := readTree(t, work)[rel]; have != want[rel] {
			t.Fatalf("%s on B = %+v, want A's %+v", rel, have, want[rel])
		}
	}
	b := h.openOn(h.b, h.engB, got.Taken.Cell, work, nameB)
	b.mustSay("b1")
	assertEnvInjected(t, work)
}

// A chat that was taken and never opened is taken again: the person continued
// it here, closed the window before any tool call, and continued it once more.
// Nothing was sealed in between, so the take must still find the tree it put in
// place and see that it holds no work of its own.
func TestTwoHomesTakeOfAnUnopenedTakenChat(t *testing.T) {
	h := newTwoHomes(t)
	ctx := context.Background()
	if err := os.WriteFile(filepath.Join(h.work, "README.md"), []byte("# project\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := h.openA()
	a.mustSay("first")
	h.durable(h.cell.ID, h.cell)
	if err := a.drive.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := h.continuerB().Take(ctx, h.cell.ID); err != nil {
		t.Fatal(err)
	}
	again, err := h.continuerB().Take(ctx, h.cell.ID)
	if err != nil {
		t.Fatalf("a warm take of a chat never opened here failed: %v", err)
	}
	if again.Taken.Kept != "" {
		t.Fatalf("a tree nobody touched kept %s as edits", again.Taken.Kept)
	}
}

// bigFile writes n bytes that do not compress or repeat, so what a publish
// sends can be told from what the chat merely holds.
func bigFile(t *testing.T, path string, n int) {
	t.Helper()
	body := make([]byte, n)
	for i := range body {
		body[i] = byte(i*i + i>>8*7 + i>>16*13)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestTwoHomesPublishAfterTakeSendsOnlyTheEdit: B takes a chat whose tree holds
// a large file, then makes one small turn. Everything the take brought is
// already on the relay, so B's publish sends the turn and not the file again.
func TestTwoHomesPublishAfterTakeSendsOnlyTheEdit(t *testing.T) {
	const size = 1 << 20
	h := newTwoHomes(t)
	ctx := context.Background()
	bigFile(t, filepath.Join(h.work, "big.bin"), size)
	a := h.openA()
	a.mustSay("first")
	h.durable(h.cell.ID, h.cell)
	if err := a.drive.Close(ctx); err != nil {
		t.Fatal(err)
	}
	took, err := h.continuerB().Take(ctx, h.cell.ID)
	if err != nil {
		t.Fatal(err)
	}
	before := relayCounts(t, h.b).BytesIn

	b := h.openOn(h.b, h.engB, took.Taken.Cell, workspaceOf(took.Taken.Cell.Root), nameB)
	b.mustSay("b one")
	h.durable(h.cell.ID, took.Taken.Cell)
	if err := b.drive.Close(ctx); err != nil {
		t.Fatal(err)
	}
	sent := relayCounts(t, h.b).BytesIn - before
	t.Logf("B's publish after the take sent %d bytes; the large file is %d", sent, size)
	if sent >= size {
		t.Fatalf("B's publish sent %d bytes, as much as the %d-byte file it had just been sent", sent, size)
	}
}
