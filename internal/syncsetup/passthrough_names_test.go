package syncsetup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/syncwords"
)

// These tests are the validation contract of SPEC-4: while blind pass-through
// is the road, the hosted relay is told neither the name of a chat nor the
// name of its folder, not even sealed, by ANY request, and a message to a chat
// on another computer is not left on it. Each is written from what the relay's
// request log shows, decoded recursively, and none reads the implementation.
// Where storage is approved the names MAY travel, and the tests that pin that
// say so, because a strip that also fired there would break the saved chat
// list of every approved account.

// namesIn is every non-empty title or folder a JSON body carries, at any
// depth, as a path: the check must not be fooled by a record nested one level
// down. A body that is not JSON is reported as a finding, never skipped.
func namesIn(raw []byte) []string {
	var body any
	if err := json.Unmarshal(raw, &body); err != nil {
		return []string{"a body that is not JSON: " + err.Error()}
	}
	var found []string
	var walk func(path string, v any)
	walk = func(path string, v any) {
		switch v := v.(type) {
		case map[string]any:
			for key, child := range v {
				if s, ok := child.(string); ok && s != "" && (strings.EqualFold(key, "title") || strings.EqualFold(key, "folder")) {
					found = append(found, path+"."+key)
				}
				walk(path+"."+key, child)
			}
		case []any:
			for i, child := range v {
				walk(fmt.Sprintf("%s[%d]", path, i), child)
			}
		}
	}
	walk("$", body)
	return found
}

// namedWrites is every directory write the relay received so far that carries
// a non-empty title or folder anywhere in its body.
func (w *wireLog) namedWrites() []string {
	var bad []string
	for _, e := range w.snapshot() {
		if len(e.Body) == 0 {
			continue
		}
		for _, at := range namesIn(e.Body) {
			bad = append(bad, fmt.Sprintf("%s %s carries %s", e.Method, e.Path, at))
		}
	}
	return bad
}

// dirWrites is how many directory writes with a body the relay has received.
func (w *wireLog) dirWrites() int {
	n := 0
	for _, e := range w.snapshot() {
		if len(e.Body) > 0 {
			n++
		}
	}
	return n
}

// noteWrites is the writes whose body holds a message outbox.
func (w *wireLog) noteWrites() []string {
	var out []string
	for _, e := range w.snapshot() {
		if strings.Contains(string(e.Body), `"outbox"`) {
			out = append(out, e.Method+" "+e.Path)
		}
	}
	return out
}

// createdBranch says the relay was asked to make a cell that names a parent:
// the kept-edits path, which is the one a take with unsealed edits runs.
func (w *wireLog) createdBranch() bool {
	for _, e := range w.snapshot() {
		if e.Method == http.MethodPost && strings.HasPrefix(e.Path, "/v1/dir/cells/") && strings.Contains(string(e.Body), `"parent_cell"`) {
			return true
		}
	}
	return false
}

// assertNoNames fails with the phase's name when the relay has been told a
// chat name or a folder name, and says how many writes it has looked at.
func assertNoNames(t *testing.T, r *liveRig, phase string) {
	t.Helper()
	if bad := r.wire.namedWrites(); len(bad) > 0 {
		t.Fatalf("after %s the relay holds a name it must never be told: %v", phase, bad)
	}
}

// aChatThatHasNames is the rig's chat on A, with its title and folder, so a
// leak has something to leak: the check is meaningless on an untitled chat.
func aChatThatHasNames(t *testing.T, r *liveRig) {
	t.Helper()
	if titleOf(r.cell.Root) == "" || chatFolder(r.cell.Root) == "" {
		t.Fatalf("the chat on A has title %q and folder %q; the check below would prove nothing", titleOf(r.cell.Root), chatFolder(r.cell.Root))
	}
}

// editedOnAThenTakenBackOnA is the scene the audit reproduced: A's chat has a
// title, B takes it and saves a turn, then A edits a file of its own folder by
// hand and takes the chat back. A's edit was never sealed, so the take keeps it
// as a cell of its own that names the chat as its parent.
func editedOnAThenTakenBackOnA(t *testing.T, r *liveRig, a *openChat) Continued {
	t.Helper()
	ctx := context.Background()
	mustNil(t, a.drive.Close(ctx))
	took := liveTake(t, r)
	workB := workspaceOf(took.Taken.Cell.Root)
	b := r.openSeated(r.b, r.engB, took.Taken.Cell, workB, nameB, nil, cellstore.WithPipeline(), cellstore.WithWarmCapture())
	b.mustSay("a turn on B")
	mustNil(t, executor.Drain(ctx, b.seat))
	waitFor(t, "B's turn to reach the directory", func() bool {
		head, err := cellstore.Head(took.Taken.Cell)
		return err == nil && head != nil && r.directoryHead(r.cell.ID) == head.Turn.ID
	})
	f, err := os.OpenFile(filepath.Join(r.work, "README.md"), os.O_WRONLY|os.O_APPEND, 0)
	mustNil(t, err)
	_, err = f.WriteString("edited by hand on A\n")
	mustNil(t, err)
	mustNil(t, f.Close())

	back, err := r.continuerA().Take(ctx, r.cell.ID)
	if err != nil {
		t.Fatalf("A could not take the chat back: %v", err)
	}
	if back.Wait != nil {
		mustNil(t, back.Wait(ctx))
	}
	return back
}

// Names-1 (the audit's finding A): a computer that takes its own chat back
// with an edit nobody saved keeps the edit as a cell of its own. That cell's
// record must carry no title and no folder either, though the chat has both on
// A when the take begins.
func TestPassThroughNamesBranchTakeBackCarriesNoTitle(t *testing.T) {
	r := newLiveRig(t)
	a := liveChat(t, r)
	aChatThatHasNames(t, r)

	editedOnAThenTakenBackOnA(t, r, a)

	if !r.wire.createdBranch() {
		t.Fatal("the relay was never asked for a kept-edits cell, so this scene did not run the branch road")
	}
	assertNoNames(t, r, "A took its chat back with an edit nobody saved")
}

// Names-2: across every request a chat makes while blind pass-through carries
// it (create, publish, acquire, a failed take, a take, a publish from the
// taking computer, a kept-edits branch), no body the relay receives holds a
// non-empty title or folder at any depth.
func TestPassThroughNamesNoRequestBodyCarriesAnyAcrossTheRoad(t *testing.T) {
	r := newLiveRig(t)
	a := liveChat(t, r)
	aChatThatHasNames(t, r)
	assertNoNames(t, r, "create and publish")
	if r.wire.dirWrites() == 0 {
		t.Fatal("the relay saw no directory write, so the check proves nothing")
	}

	// A failed take: the computer that has the chat is off.
	mustNil(t, a.drive.Close(context.Background()))
	stopFilesDoor(r.homeA)
	failedTake(t, r)
	assertNoNames(t, r, "a failed take")
	r.a.ensureFilesDoor()

	// The rest of the scene, from a chat that is back open on A.
	a = r.openA()
	a.mustSay("a turn after the failed take")
	r.durable(r.cell.ID, r.cell)
	editedOnAThenTakenBackOnA(t, r, a)
	assertNoNames(t, r, "a take, a publish from B and a kept-edits branch")
	if !r.wire.createdBranch() {
		t.Fatal("the scene never reached the kept-edits road")
	}
}

// Names-3: the strip is one choke point at the directory client, so a caller
// that forgot to blank a name is still kept from sending it. A create, a
// publish and a branch built by hand with names in them reach the relay
// without them.
func TestPassThroughNamesTheDirectoryClientStripsWhatACallerSends(t *testing.T) {
	r := newLiveRig(t)
	ctx := context.Background()
	s := r.a
	title, err := directory.SealTitle(directory.MetadataKey(s.Identity.CellKey()), "a secret title")
	mustNil(t, err)
	folder, err := directory.SealTitle(directory.MetadataKey(s.Identity.CellKey()), "a secret folder")
	mustNil(t, err)
	keys := map[string]map[string]string{"k": {s.Identity.ID(): ""}}

	id, err := cell.NewID(time.Now())
	mustNil(t, err)
	head := strings.Repeat("a", 64)
	view, err := s.Dir.Create(ctx, id, directory.CellInit{Head: head, Class: "chat", Title: title, Folder: folder, Size: 1, Keys: keys})
	mustNil(t, err)
	next := strings.Repeat("b", 64)
	_, err = s.Dir.Publish(ctx, id, directory.Publish{Fence: view.Cell.Lease.Fence, OldHead: head, Head: next, Size: 2, Class: "chat", Title: title, Folder: folder})
	mustNil(t, err)
	child, err := cell.NewID(time.Now())
	mustNil(t, err)
	_, err = s.Dir.Create(ctx, child, directory.CellInit{Head: next, Class: "chat", Title: title, ParentCell: id, Size: 2, Keys: keys})
	mustNil(t, err)

	if got := r.wire.count(http.MethodPost, "/v1/dir/cells/"+id); got < 2 {
		t.Fatalf("the relay saw %d writes of the chat; the create and the publish were not both sent", got)
	}
	assertNoNames(t, r, "a create, a publish and a branch built with names in them")
	view, err = s.Dir.Cell(ctx, id)
	mustNil(t, err)
	if view.Cell.Title != "" || view.Cell.Folder != "" {
		t.Fatalf("the relay's record holds title %q and folder %q", view.Cell.Title, view.Cell.Folder)
	}
}

// Names-3b: the record every caller builds, not only the one that reaches the
// wire, carries no title and no folder while pass-through is the road, and both
// once storage is approved. The drive, the kept-edits branch and the background
// pass all build their record through the same call.
func TestPassThroughNamesTheRecordEveryBuilderMakesCarriesNone(t *testing.T) {
	r := newLiveRig(t)
	aChatThatHasNames(t, r)
	info := r.a.publishInfo(r.cell, titleOf(r.cell.Root))
	if info.Title != "" || info.Folder != "" {
		t.Fatalf("the record built while pass-through is the road holds title %q and folder %q", info.Title, info.Folder)
	}
	if info.Class == "" || len(info.Keys) == 0 {
		t.Fatalf("the record lost what it must keep: class %q, keys %v", info.Class, info.Keys)
	}

	r.approveStorage()
	info = r.a.publishInfo(r.cell, titleOf(r.cell.Root))
	if info.Title == "" || info.Folder == "" {
		t.Fatalf("the record built with storage approved holds title %q and folder %q; it must hold both", info.Title, info.Folder)
	}
}

// Names-4: where storage is approved the names travel, as they always have.
// The strip must not fire there: the chat list of an approved account shows
// titles because the record carries them. A hand-built create and publish, a
// chat opened on A, and the kept-edits cell all carry the sealed title.
func TestPassThroughNamesApprovedStorageStillCarriesThem(t *testing.T) {
	r := newLiveRig(t)
	r.approveStorage()
	a := liveChat(t, r)
	aChatThatHasNames(t, r)
	if bad := r.wire.namedWrites(); len(bad) == 0 {
		t.Fatal("with storage approved the chat's record carried no title; the strip fired where it must not")
	}

	editedOnAThenTakenBackOnA(t, r, a)
	branchNamed := false
	for _, e := range r.wire.snapshot() {
		if e.Method == http.MethodPost && strings.Contains(string(e.Body), `"parent_cell"`) && len(namesIn(e.Body)) > 0 {
			branchNamed = true
		}
	}
	if !branchNamed {
		t.Fatal("with storage approved the kept-edits cell carried no title; the strip fired where it must not")
	}

	ctx := context.Background()
	title, err := directory.SealTitle(directory.MetadataKey(r.a.Identity.CellKey()), "a title on an approved account")
	mustNil(t, err)
	id, err := cell.NewID(time.Now())
	mustNil(t, err)
	_, err = r.a.Dir.Create(ctx, id, directory.CellInit{Head: strings.Repeat("c", 64), Class: "chat", Title: title, Size: 1, Keys: map[string]map[string]string{"k": {r.a.Identity.ID(): ""}}})
	mustNil(t, err)
	view, err := r.a.Dir.Cell(ctx, id)
	mustNil(t, err)
	if view.Cell.Title != title {
		t.Fatalf("the relay's record of a chat on an approved account holds title %q; want what was sent", view.Cell.Title)
	}
}

// Names-5: the background pass over older chats. While pass-through is the
// road it sends no record at all; once storage is approved it creates the
// record with the chat's sealed title, exactly as it did before.
func TestPassThroughNamesBackgroundPublishCarriesThemOnlyOnceStorageIsApproved(t *testing.T) {
	r := newLiveRig(t)
	ctx := context.Background()
	older := savedHomeChat(t, r.driveRig, projectsOf(r.a), "an older chat nobody opened", 1<<10)

	before := r.wire.dirWrites()
	mustNil(t, r.a.PublishLocalChats(ctx, time.Now, nil))
	if n := r.wire.dirWrites() - before; n != 0 {
		t.Fatalf("the background pass sent %d directory writes while storage is off", n)
	}
	assertNoNames(t, r, "the background pass with storage off")

	r.approveStorage()
	mustNil(t, r.a.PublishLocalChats(ctx, time.Now, nil))
	titled := false
	for _, e := range r.wire.snapshot() {
		if e.Method == http.MethodPost && e.Path == "/v1/dir/cells/"+older.ID && len(namesIn(e.Body)) > 0 {
			titled = true
		}
	}
	if !titled {
		t.Fatalf("the record of %s, made after storage was approved, carried no title", older.ID)
	}
}

// Notes-1 (the audit's finding B): with sync storage off, a message to a chat
// another computer holds is not left on the relay. Nothing is written to B's
// record, the verb is not on offer, and the refusal is a plain sentence.
func TestPassThroughNoteToAChatElsewhereIsNotLeftOnTheRelayWhileStorageIsOff(t *testing.T) {
	r := newLiveRig(t)
	liveChat(t, r)
	if r.b.Notes() {
		t.Fatal("Notes() says messages to another computer are on offer while relay storage is off")
	}

	id, refused := r.b.SendNote(context.Background(), r.cell.ID, "PLAINTEXT-NOTE-MARKER-4c1e hello from B")
	if !errors.Is(refused, syncwords.ErrNotesWaitForStorage) || id != "" {
		t.Fatalf("SendNote = %q, %v; want the sentence that messages wait for relay storage", id, refused)
	}
	if bad := r.wire.noteWrites(); len(bad) > 0 {
		t.Fatalf("the relay was sent a message outbox: %v", bad)
	}
	l, err := r.a.Dir.List(context.Background())
	mustNil(t, err)
	if n := len(l.Devices[r.b.Device.ID()].Outbox); n != 0 {
		t.Fatalf("the relay holds %d sealed messages from B", n)
	}
	if !strings.Contains(refused.Error(), "relay storage") {
		t.Fatalf("the refusal %q does not say what the message is waiting for", refused)
	}
}

// Notes-2: with storage approved the same message goes, is listed as waiting,
// and the capability is on offer: nothing about that road changed.
func TestPassThroughNoteStillTravelsWhereStorageIsApproved(t *testing.T) {
	r := newLiveRig(t)
	liveChat(t, r)
	r.approveStorage()
	if !r.b.Notes() {
		t.Fatal("Notes() says messages are not on offer with storage approved")
	}
	id, err := r.b.SendNote(context.Background(), r.cell.ID, "a message that may wait on the relay")
	if err != nil || id == "" {
		t.Fatalf("SendNote with storage approved = %q, %v; want the message sent", id, err)
	}
	l, err := r.a.Dir.List(context.Background())
	mustNil(t, err)
	if n := len(l.Devices[r.b.Device.ID()].Outbox); n != 1 {
		t.Fatalf("the relay holds %d messages from B; want the one sent", n)
	}
}

// Notes-3: a message left while storage was approved can still be taken off
// the relay after storage is switched off again. Refusing to send must not
// leave an outbox that nothing is allowed to empty.
func TestPassThroughNoteAnOutboxLeftEarlierCanStillBeEmptiedWhileStorageIsOff(t *testing.T) {
	r := newLiveRig(t)
	liveChat(t, r)
	r.approveStorage()
	ctx := context.Background()
	id, err := r.b.SendNote(ctx, r.cell.ID, "left while storage was on")
	mustNil(t, err)

	for _, one := range []*Sync{r.a, r.b} {
		mustNil(t, SaveStorage(one.Home, one.Relay, one.Identity.ID(), "off"))
	}
	if r.b.Notes() {
		t.Fatal("Notes() is on offer after storage was switched off")
	}
	mustNil(t, r.b.DropNotes(ctx, []string{id}))
	l, err := r.a.Dir.List(ctx)
	mustNil(t, err)
	if n := len(l.Devices[r.b.Device.ID()].Outbox); n != 0 {
		t.Fatalf("the relay still holds %d messages from B that were dropped", n)
	}
}
