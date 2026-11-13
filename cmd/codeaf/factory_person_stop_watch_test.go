package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// watchFloor is a floor of jobs, the first managers of them with a chat each.
func watchFloor(t *testing.T, jobs, managers int) (*store.Store, seamRunDoor, []string, []int) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "factory"))
	if err != nil {
		t.Fatal(err)
	}
	talks := t.TempDir()
	var chats []string
	var ids []int
	for i := range jobs {
		it := factory.Item{Repo: "acme/api", Title: fmt.Sprintf("job %d", i), State: factory.StateNew, Cap: 1}
		if i < managers {
			it.Talk = filepath.Join(talks, fmt.Sprintf("chat-%d", i), "transcript.jsonl")
			chats = append(chats, it.Talk)
		}
		made, err := st.Create(it)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, made.ID)
	}
	return st, seamRunDoor{st: st}, chats, ids
}

// settleFloor moves the floor's folder time into the past, as a floor nobody has
// written to for a while, so a look may trust the folder.
func settleFloor(t *testing.T, st *store.Store) {
	t.Helper()
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(st.Root(), old, old); err != nil {
		t.Fatal(err)
	}
}

func setStopped(t *testing.T, st *store.Store, id int, stopped bool) {
	t.Helper()
	if err := st.Annotate(id, func(it *factory.Item) error {
		if stopped {
			factory.MarkPersonStopped(it)
		} else {
			factory.ClearPersonStop(it)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// THE WATCHER RUNS TEN TIMES A SECOND FOR EVERY OPEN JOB CHAT. On a floor where
// nothing changes, it must read no job document at all, however many jobs and
// chats there are, and a chat that manages nothing is no dearer.
func TestWatchingManyJobChatsReadsNoDocumentWhileNothingChanges(t *testing.T) {
	st, door, chats, _ := watchFloor(t, 300, 20)
	stranger := filepath.Join(t.TempDir(), "transcript.jsonl")
	look := func() {
		t.Helper()
		for _, chat := range chats {
			if why := door.WatchedRefusal(chat); why != "" {
				t.Fatalf("an unstopped job refused its chat: %q", why)
			}
		}
		if why := door.WatchedRefusal(stranger); why != "" {
			t.Fatalf("a chat that manages nothing was refused: %q", why)
		}
	}
	look()
	before := st.DocumentReads()
	const ticks = 50
	for range ticks {
		look()
	}
	if reads := st.DocumentReads() - before; reads != 0 {
		t.Fatalf("%d looks over %d chats and 300 jobs read %d job documents, want none", ticks, len(chats)+1, reads)
	}

	settleFloor(t, st)
	look()
	before = st.DocumentReads()
	for range ticks {
		look()
	}
	if reads := st.DocumentReads() - before; reads != 0 {
		t.Fatalf("on a settled floor, %d looks read %d job documents, want none", ticks, reads)
	}
}

// One job changing costs that job's document once, not the floor's.
func TestOneJobChangingReadsOneDocument(t *testing.T) {
	st, door, chats, ids := watchFloor(t, 300, 5)
	door.WatchedRefusal(chats[0])
	before := st.DocumentReads()
	setStopped(t, st, ids[3], true)
	writes := st.DocumentReads() - before // what the write itself reads
	for _, chat := range chats {
		door.WatchedRefusal(chat)
	}
	if reads := st.DocumentReads() - before - writes; reads != 1 {
		t.Fatalf("one changed job cost %d document reads, want 1", reads)
	}
}

// NO STALE PASS. The very next look after any write gives the answer a full
// read of the floor gives, whether the floor was busy or long quiet, whoever
// wrote, and for every way the answer can change.
func TestTheNextLookSeesEveryChange(t *testing.T) {
	for _, quiet := range []bool{false, true} {
		t.Run(fmt.Sprintf("quiet=%v", quiet), func(t *testing.T) {
			st, door, chats, ids := watchFloor(t, 40, 3)
			// The other writer: another process's store on the same folder.
			other, err := store.Open(st.Root())
			if err != nil {
				t.Fatal(err)
			}
			prepare := func() {
				door.WatchedRefusal(chats[0])
				if quiet {
					settleFloor(t, st)
					door.WatchedRefusal(chats[0])
				}
			}
			agrees := func(label, chat, want string) {
				t.Helper()
				if got := door.WatchedRefusal(chat); got != want {
					t.Fatalf("%s: the next look said %q, want %q", label, got, want)
				}
				if fresh := door.AutomaticRefusal(chat); fresh != want {
					t.Fatalf("%s: a full read said %q, want %q", label, fresh, want)
				}
			}
			for round := range 25 {
				prepare()
				setStopped(t, st, ids[0], true)
				agrees(fmt.Sprintf("round %d stop", round), chats[0], factory.PersonStopReason)
				prepare()
				setStopped(t, other, ids[0], false)
				agrees(fmt.Sprintf("round %d start", round), chats[0], "")
			}

			// A chat is handed to a job: the same chat now watches that job.
			fresh := filepath.Join(t.TempDir(), "transcript.jsonl")
			agrees("before the handover", fresh, "")
			setStopped(t, st, ids[20], true)
			prepare()
			if err := st.Annotate(ids[20], func(it *factory.Item) error { it.Talk = fresh; return nil }); err != nil {
				t.Fatal(err)
			}
			agrees("the job gained this chat", fresh, factory.PersonStopReason)
			prepare()
			if err := st.Annotate(ids[20], func(it *factory.Item) error { it.Talk = ""; return nil }); err != nil {
				t.Fatal(err)
			}
			agrees("the job dropped this chat", fresh, "")

			// A new job is created already stopped, and a job goes away.
			prepare()
			talk := filepath.Join(t.TempDir(), "transcript.jsonl")
			stopped := true
			made, err := st.Create(factory.Item{Repo: "acme/api", Title: "new", State: factory.StateNew, Cap: 1, Talk: talk, PersonStopped: &stopped})
			if err != nil {
				t.Fatal(err)
			}
			agrees("a new stopped job", talk, factory.PersonStopReason)
			prepare()
			if err := os.Remove(st.ItemPath(made.ID)); err != nil {
				t.Fatal(err)
			}
			agrees("the job is gone", talk, "")
		})
	}
}

// The budget's own route keeps its answer: a budget changed mid-watch is the
// next look's refusal, and raising it is the next look's pass, alongside the Stop.
func TestTheNextLookSeesABudgetChange(t *testing.T) {
	st, door, chats, ids := watchFloor(t, 40, 1)
	chat := jobChatDoorOf(st).Chat(chats[0])
	if chat == nil {
		t.Fatal("the job's chat was not found")
	}
	settleFloor(t, st)
	if chat.Refusal() != "" || door.WatchedRefusal(chats[0]) != "" {
		t.Fatal("an unspent, unstopped job was refused")
	}
	if err := st.Annotate(ids[0], func(it *factory.Item) error { it.ChatSpent = 1; return nil }); err != nil {
		t.Fatal(err)
	}
	if want := fmt.Sprintf(factoryrun.ChatBudgetReached, "$1", "$1"); chat.Refusal() != want {
		t.Fatalf("after the spend the budget said %q, want %q", chat.Refusal(), want)
	}
	settleFloor(t, st)
	if err := st.Annotate(ids[0], func(it *factory.Item) error { it.Cap = 5; return nil }); err != nil {
		t.Fatal(err)
	}
	if why := chat.Refusal(); why != "" {
		t.Fatalf("after the raise the budget still said %q", why)
	}
}

// A document that can't be read is skipped, as a full read skips it, and is
// read again once it is whole, even with the folder's time unchanged.
func TestADocumentReadableAgainIsSeen(t *testing.T) {
	st, door, chats, ids := watchFloor(t, 5, 1)
	setStopped(t, st, ids[0], true)
	path := st.ItemPath(ids[0])
	kept, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{torn"), 0o600); err != nil {
		t.Fatal(err)
	}
	settleFloor(t, st)
	if got, want := door.WatchedRefusal(chats[0]), door.AutomaticRefusal(chats[0]); got != want {
		t.Fatalf("over a torn document the watcher said %q, a full read %q", got, want)
	}
	if err := os.WriteFile(path, kept, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := door.WatchedRefusal(chats[0]); got != factory.PersonStopReason {
		t.Fatalf("the whole document again said %q", got)
	}
}

// A write that keeps the document's size and time is still a new file, and the
// next look sees it through both routes: the Stop, and the budget.
func TestAWriteKeepingTheFilesTimeIsSeen(t *testing.T) {
	st, door, chats, ids := watchFloor(t, 20, 1)
	chat := jobChatDoorOf(st).Chat(chats[0])
	path := st.ItemPath(ids[0])
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// Same length, same time, new file: what a same-instant rename leaves.
	swap := func(old, next string) {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		changed := strings.Replace(string(data), old, next, 1)
		if changed == string(data) || len(changed) != len(data) {
			t.Fatalf("no same-length change from %q to %q in %s", old, next, data)
		}
		temp := path + ".swap"
		if err := os.WriteFile(temp, []byte(changed), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(temp, before.ModTime(), before.ModTime()); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(temp, path); err != nil {
			t.Fatal(err)
		}
	}
	setStopped(t, st, ids[0], false)
	if before, err = os.Stat(path); err != nil {
		t.Fatal(err)
	}
	settleFloor(t, st)
	if door.WatchedRefusal(chats[0]) != "" || chat.Refusal() != "" {
		t.Fatal("a job not stopped was refused")
	}
	swap(`"PersonStopped": false`, `"PersonStopped": true `)
	if got := door.WatchedRefusal(chats[0]); got != factory.PersonStopReason {
		t.Fatalf("a Stop written with the file's time kept: the next look said %q", got)
	}
	swap(`"PersonStopped": true `, `"PersonStopped": false`)
	if got := door.WatchedRefusal(chats[0]); got != "" {
		t.Fatalf("a Start written with the file's time kept: the next look said %q", got)
	}
	if err := st.Annotate(ids[0], func(it *factory.Item) error { it.ChatSpent = 2; it.Cap = 9; return nil }); err != nil {
		t.Fatal(err)
	}
	if before, err = os.Stat(path); err != nil {
		t.Fatal(err)
	}
	settleFloor(t, st)
	if chat.Refusal() != "" {
		t.Fatal("a job within its budget was refused")
	}
	swap(`"Cap": 9`, `"Cap": 1`)
	if chat.Refusal() == "" {
		t.Fatal("a budget lowered with the file's time kept: the next look still passed")
	}
}

// The floor going away is an answer, not a crash, and coming back is seen.
func TestAFloorThatGoesAwayIsNoManagerAndNotACrash(t *testing.T) {
	st, door, chats, ids := watchFloor(t, 3, 1)
	setStopped(t, st, ids[0], true)
	if got := door.WatchedRefusal(chats[0]); got != factory.PersonStopReason {
		t.Fatalf("before: %q", got)
	}
	aside := st.Root() + ".aside"
	if err := os.Rename(st.Root(), aside); err != nil {
		t.Fatal(err)
	}
	if got := door.WatchedRefusal(chats[0]); got != "" {
		t.Fatalf("with no floor, the watcher said %q", got)
	}
	if err := os.Rename(aside, st.Root()); err != nil {
		t.Fatal(err)
	}
	if got := door.WatchedRefusal(chats[0]); got != factory.PersonStopReason {
		t.Fatalf("with the floor back, the watcher said %q", got)
	}
}

// A write through this store is seen on the next look even if the folder's time
// comes out identical to the one the last sweep trusted.
func TestAWriteHereIsSeenWhateverTheFolderTimeSays(t *testing.T) {
	st, door, chats, ids := watchFloor(t, 3, 1)
	setStopped(t, st, ids[0], true)
	settleFloor(t, st)
	if got := door.WatchedRefusal(chats[0]); got != factory.PersonStopReason {
		t.Fatalf("before: %q", got)
	}
	settled, err := os.Stat(st.Root())
	if err != nil {
		t.Fatal(err)
	}
	setStopped(t, st, ids[0], false)
	if err := os.Chtimes(st.Root(), settled.ModTime(), settled.ModTime()); err != nil {
		t.Fatal(err)
	}
	if got := door.WatchedRefusal(chats[0]); got != "" {
		t.Fatalf("after a person's Start here, the watcher still said %q", got)
	}
}

// A link moved under a job's chat path changes no document, and a sweep sees it.
func TestALinkRetargetedUnderAChatIsSeenWithinASweep(t *testing.T) {
	st, door, _, _ := watchFloor(t, 3, 0)
	base := t.TempDir()
	here, there := filepath.Join(base, "here"), filepath.Join(base, "there")
	for _, dir := range []string{here, there} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "transcript.jsonl"), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(here, link); err != nil {
		t.Fatal(err)
	}
	stopped := true
	if _, err := st.Create(factory.Item{Repo: "acme/api", Title: "linked", State: factory.StateNew, Cap: 1, Talk: filepath.Join(link, "transcript.jsonl"), PersonStopped: &stopped}); err != nil {
		t.Fatal(err)
	}
	settleFloor(t, st)
	moved := filepath.Join(there, "transcript.jsonl")
	if got := door.WatchedRefusal(moved); got != "" {
		t.Fatalf("before the link moved, the other chat was refused: %q", got)
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(there, link); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1100 * time.Millisecond)
	if got := door.WatchedRefusal(moved); got != factory.PersonStopReason {
		t.Fatalf("after the link moved and a sweep, the watcher said %q", got)
	}
}

// Many chats look at once while a job changes; every look after the write ends
// sees it. The race detector is the other half of this test.
func TestLooksDuringAWriteEndOnTheWrite(t *testing.T) {
	st, door, chats, ids := watchFloor(t, 60, 8)
	var lookers sync.WaitGroup
	done := make(chan struct{})
	for _, chat := range chats {
		lookers.Add(1)
		go func() {
			defer lookers.Done()
			for {
				select {
				case <-done:
					return
				default:
					door.WatchedRefusal(chat)
				}
			}
		}()
	}
	for round := range 40 {
		stopped := round%2 == 0
		setStopped(t, st, ids[0], stopped)
		want := ""
		if stopped {
			want = factory.PersonStopReason
		}
		if got := door.WatchedRefusal(chats[0]); got != want {
			close(done)
			lookers.Wait()
			t.Fatalf("round %d: the next look said %q, want %q", round, got, want)
		}
	}
	close(done)
	lookers.Wait()
}
