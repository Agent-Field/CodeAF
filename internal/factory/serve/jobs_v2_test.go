package serve_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/github"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

type v2Jobs struct {
	st      *store.Store
	rec     *record.Record
	h       *serve.Handler
	seam    factory.Seam
	cfg     serve.Config
	clock   atomic.Int64
	person  string
	folders *fakeFolders
	chats   *fakeChats
}

// fakeChats stands in for this machine's conversations: a delete that may
// fail, and the jobs, as they stood, whose conversations it was asked to
// delete.
type fakeChats struct {
	mu      sync.Mutex
	refuse  error
	deleted []factory.Item
}

func (c *fakeChats) Delete(it factory.Item) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.deleted = append(c.deleted, it)
	return c.refuse
}

func (c *fakeChats) asked() []factory.Item {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]factory.Item(nil), c.deleted...)
}

// fakeFolders stands in for this machine's job folders: the work a drop
// would read there, a delete that may fail, and the jobs it deleted. during,
// when set, is called with the job each delete is asked for, before it is
// done, as a request racing the delete would arrive.
type fakeFolders struct {
	mu        sync.Mutex
	work      map[int]run.Work
	unread    error
	refuse    error
	discarded []factory.Item
	during    func(factory.Item)
}

func (f *fakeFolders) Work(it factory.Item) (run.Work, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.work[it.ID], f.unread
}

func (f *fakeFolders) Discard(it factory.Item) error {
	f.mu.Lock()
	during := f.during
	f.mu.Unlock()
	if during != nil {
		during(it)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.refuse != nil {
		return f.refuse
	}
	f.discarded = append(f.discarded, it)
	delete(f.work, it.ID)
	return nil
}

func (f *fakeFolders) deleted() []int {
	f.mu.Lock()
	defer f.mu.Unlock()
	var ids []int
	for _, it := range f.discarded {
		ids = append(ids, it.ID)
	}
	return ids
}

func newV2Jobs(t *testing.T) *v2Jobs {
	t.Helper()
	f := &v2Jobs{person: "sam", folders: &fakeFolders{work: map[int]run.Work{}}, chats: &fakeChats{}}
	f.clock.Store(now.UnixNano())
	var err error
	f.st, err = store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	clock := func() time.Time { return time.Unix(0, f.clock.Load()).UTC() }
	f.rec, err = record.Open(f.st, record.Options{Now: clock})
	if err != nil {
		t.Fatal(err)
	}
	desk, err := lease.Open(f.st, lease.Options{Now: clock, Record: f.rec})
	if err != nil {
		t.Fatal(err)
	}
	doors := lease.NewDoors(desk)
	f.seam = factory.Seam{
		Load: func() (factory.Snapshot, error) {
			items, err := f.st.List()
			return factory.Snapshot{Items: items}, err
		},
		GitHub: func(context.Context) (factory.GitHubLink, error) { return factory.GitHubLink{Login: f.person}, nil },
		Launch: doors.Launch, Stop: doors.Stop, Hold: doors.Hold, Answer: doors.Answer, SignOff: doors.SignOff,
	}
	f.cfg = serve.Config{Mode: api.FactoryTeam, Address: config.Address, Version: config.Version, Store: f.st, Record: f.rec, Now: clock, LeaseOptions: lease.Options{Now: clock, Record: f.rec},
		Pipeline: func(context.Context, string, string) (pipeline.File, error) {
			p := pipeline.Defaults()
			p.DefaultBudget = 4
			return p, nil
		},
		Folders:     f.folders,
		DeleteChats: f.chats.Delete,
	}
	f.h = serve.New(f.seam, f.cfg)
	t.Cleanup(func() { f.h.Close() })
	return f
}

func (f *v2Jobs) create(t *testing.T, it factory.Item) factory.Item {
	t.Helper()
	if it.Repo == "" {
		it.Repo = "acme/api"
	}
	if it.Title == "" {
		it.Title = "test job"
	}
	got, err := f.st.Create(it)
	if err != nil {
		t.Fatal(err)
	}
	return got
}
func (f *v2Jobs) post(t *testing.T, it factory.Item, verb, body string, status int) *httptest.ResponseRecorder {
	t.Helper()
	w := verbRequest(f.h, "POST", fmt.Sprintf("/v1/jobs/%d/%s", it.ID, verb), body)
	checkJSON(t, w, status)
	return w
}
func (f *v2Jobs) read(t *testing.T, it factory.Item) api.Job {
	t.Helper()
	w := verbRequest(f.h, "GET", fmt.Sprintf("/v1/jobs/%d", it.ID), "")
	checkJSON(t, w, 200)
	var j api.Job
	if err := json.Unmarshal(w.Body.Bytes(), &j); err != nil {
		t.Fatal(err)
	}
	return j
}

func TestV2RunStopUndoAndStartNow(t *testing.T) {
	f := newV2Jobs(t)
	it := f.create(t, factory.Item{State: factory.StateNew, Assignee: "sam"})
	f.post(t, it, "run", "", 200)
	if j := f.read(t, it); j.State != "queued" || j.Budget != 4 {
		t.Fatalf("run %+v", j)
	}
	// A QUEUED JOB HAD NO STEP TO CUT: its stop takes it out of the queue and
	// back to Backlog, with no stop mark, where Start picks it up.
	f.post(t, it, "stop", "", 200)
	if j := f.read(t, it); j.State != "backlog" || j.Failure != nil || j.NeedsYou != nil || j.StatePresentation.Title != "Backlog" || !j.Can.Run || !j.Can.Undo || j.Can.Stop {
		t.Fatalf("stop %+v", j)
	}
	f.post(t, it, "undo", "", 200)
	if j := f.read(t, it); j.State != "queued" {
		t.Fatal(j.State)
	}
	f.post(t, it, "stop", "", 200)
	f.clock.Add(int64(factory.ApprovalUndo))
	f.post(t, it, "undo", "", 409)
	f.post(t, it, "start_now", "", 200)
	cur, _ := f.st.Get(it.ID)
	if !cur.Marked || cur.State != factory.StateQueued || cur.Failure != nil {
		t.Fatalf("start now %+v", cur)
	}
}

func TestV2DismissIsPersonalAndGitHubClearsIt(t *testing.T) {
	f := newV2Jobs(t)
	it := f.create(t, factory.Item{Num: 1498, Origin: factory.OriginForge, State: factory.StateRunning, Assignee: "mei"})
	f.post(t, it, "dismiss", "", 200)
	if j := f.read(t, it); j.State != "running" || !j.Dismissed || j.Ref != "api#1498" {
		t.Fatalf("dismiss %+v", j)
	}
	f.person = "mei"
	if f.read(t, it).Dismissed {
		t.Fatal("dismiss crossed people")
	}
	fresh := it
	fresh.Labels = []string{"bug"}
	if err := github.Merge(f.st, "github", []factory.Item{fresh}); err != nil {
		t.Fatal(err)
	}
	f.person = "sam"
	if f.read(t, it).Dismissed {
		t.Fatal("GitHub activity did not clear dismissal")
	}
}

// A DROP ENDS THE JOB UNTIL A PERSON BRINGS IT BACK: a running job is
// stopped, its local branch and folder are deleted at once when they hold
// nothing to lose, a start is refused with the way back named, never as
// final, and GitHub does not revive it.
func TestV2DropEndsTheJobAndDeletesItsLocalWork(t *testing.T) {
	f := newV2Jobs(t)
	it := f.create(t, factory.Item{Num: 7, Origin: factory.OriginForge, State: factory.StateRunning, Assignee: "sam"})
	f.post(t, it, "drop", "", 200)
	if j := f.read(t, it); j.State != "done" || j.DoneHow != "dropped" {
		t.Fatalf("drop %+v", j)
	}
	if got := f.folders.deleted(); len(got) != 1 || got[0] != it.ID {
		t.Fatalf("deleted %v, want the dropped job's local work", got)
	}
	w := f.post(t, it, "run", "", 409)
	var refusal struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &refusal)
	if !strings.HasSuffix(refusal.Error, "#7 was dropped · Bring back returns it to Backlog") {
		t.Fatalf("a start of a dropped job is refused with %q", refusal.Error)
	}
	fresh := it
	fresh.Labels = []string{"bug"}
	if err := github.Merge(f.st, "github", []factory.Item{fresh}); err != nil {
		t.Fatal(err)
	}
	if f.read(t, it).DoneHow != "dropped" {
		t.Fatal("GitHub revived Drop")
	}
	events, _ := f.rec.Since(context.Background(), time.Time{})
	if len(events) != 1 || events[0].Kind != record.Dropped || events[0].Detail != "" {
		t.Fatalf("events %+v", events)
	}
	// Drop takes no words any more: a body that sends them is refused whole.
	other := f.create(t, factory.Item{State: factory.StateNew, Assignee: "sam"})
	f.post(t, other, "drop", `{"words":"not this one"}`, 422)
}

// A DROP NEVER LOSES WORK UNASKED. Commits on the job's branch or changes in
// its folder refuse the drop with has_work and a sentence that names them,
// and nothing is stopped or deleted; the same drop with discard is the yes,
// and its receipt says what was thrown away. Work that could not be read is
// treated as work.
func TestV2DropWithWorkAsksFirst(t *testing.T) {
	f := newV2Jobs(t)
	it := f.create(t, factory.Item{Num: 1844, Origin: factory.OriginForge, State: factory.StateRunning, Assignee: "sam"})
	f.folders.work[it.ID] = run.Work{Commits: 3, Uncommitted: true}
	w := f.post(t, it, "drop", `{}`, 409)
	var refusal struct {
		Error    string `json:"error"`
		Reason   string `json:"reason"`
		NowState string `json:"now_state"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &refusal); err != nil {
		t.Fatal(err)
	}
	if refusal.Reason != api.ReasonHasWork || refusal.Error != "api#1844's branch has 3 commits and uncommitted changes." || refusal.NowState != "running" {
		t.Fatalf("refusal %+v", refusal)
	}
	if j := f.read(t, it); j.State != "running" || j.DoneHow != "" || len(f.folders.deleted()) != 0 {
		t.Fatalf("a refused drop changed the job: %+v deleted %v", j, f.folders.deleted())
	}
	f.post(t, it, "drop", `{"discard":true}`, 200)
	if j := f.read(t, it); j.State != "done" || j.DoneHow != "dropped" || len(f.folders.deleted()) != 1 {
		t.Fatalf("discard %+v deleted %v", j, f.folders.deleted())
	}
	events, _ := f.rec.Since(context.Background(), time.Time{})
	if len(events) != 1 || events[0].Kind != record.Dropped || events[0].Detail != "discarded: api#1844's branch has 3 commits and uncommitted changes." {
		t.Fatalf("events %+v", events)
	}

	unread := f.create(t, factory.Item{Num: 12, Origin: factory.OriginForge, State: factory.StateNew, Assignee: "sam"})
	f.folders.unread = fmt.Errorf("git: not a repository")
	w = f.post(t, unread, "drop", "", 409)
	_ = json.Unmarshal(w.Body.Bytes(), &refusal)
	if refusal.Reason != api.ReasonHasWork || refusal.Error != "api#12's branch could not be read, so what it holds is unknown." {
		t.Fatalf("unread refusal %+v", refusal)
	}
	f.post(t, unread, "drop", `{"discard":true}`, 200)
	if got := f.folders.deleted(); len(got) != 2 || got[1] != unread.ID {
		t.Fatalf("deleted %v", got)
	}
}

// A DELETE THAT FAILS IS NOT A FAILED DROP: the job is dropped all the same,
// and its activity says the local branch and folder were kept and why.
func TestV2DropWhoseDeleteFailsStillDrops(t *testing.T) {
	f := newV2Jobs(t)
	it := f.create(t, factory.Item{Num: 30, Origin: factory.OriginForge, State: factory.StateNew, Assignee: "sam"})
	f.folders.refuse = fmt.Errorf("could not delete the folder of #30: permission denied")
	f.post(t, it, "drop", "", 200)
	cur, err := f.st.Get(it.ID)
	if err != nil || cur.DoneHow != "dropped" {
		t.Fatalf("not dropped: %+v %v", cur, err)
	}
	if n := len(cur.Activity); n == 0 || cur.Activity[n-1].What != "dropped · the local branch and folder were kept: could not delete the folder of #30: permission denied" {
		t.Fatalf("activity %+v", cur.Activity)
	}
}

// A DROP THROWS THE JOB'S CONVERSATIONS AWAY WITH ITS FOLDER: once the job is
// dropped and its folder deleted, the job stops naming its agent, its thread,
// what was heard there, its crew and every step's and round's chat, and those
// are deleted by the job as it stood, so the delete reaches exactly the ones
// it recorded. Another job's are never asked for. What the run did stays on
// the dropped job: its steps, its spend, its notes and its step edits, until
// Bring back.
func TestV2DropDeletesTheJobsConversationsAndForgetsThem(t *testing.T) {
	f := newV2Jobs(t)
	stream := recordedRun()
	stream.Room = "0a1b2c3d4e5f"
	stream.Phases[0].Chat, stream.Phases[0].Handle = "plan-chat", "plan"
	stream.Phases[0].Rounds = []factory.RoundLine{{Round: 1, Summary: "first plan", Chat: "plan-round-1"}}
	it := f.create(t, factory.Item{Num: 51, Origin: factory.OriginForge, State: factory.StateRunning, Assignee: "sam",
		Talk: "/places/job-51/transcript.jsonl", Heard: now, Thread: []factory.Message{{Text: "use the ledger"}},
		Notes: []string{"use the ledger"}, Adapted: []string{"you set write: keep the ledger"}, Stream: stream})
	other := f.create(t, factory.Item{Num: 52, Origin: factory.OriginForge, State: factory.StateRunning, Assignee: "sam",
		Talk: "/places/job-52/transcript.jsonl", Stream: &factory.Stream{Room: "5f4e3d2c1b0a"}})
	f.post(t, it, "drop", "", 200)

	asked := f.chats.asked()
	if len(asked) != 1 {
		t.Fatalf("asked to delete %d jobs' conversations, want the dropped one's: %+v", len(asked), asked)
	}
	was := asked[0]
	if was.ID != it.ID || was.DoneHow != "dropped" || was.Talk != it.Talk || was.Stream == nil || was.Stream.Room != "0a1b2c3d4e5f" ||
		was.Stream.Phases[0].Chat != "plan-chat" || was.Stream.Phases[0].Rounds[0].Chat != "plan-round-1" || was.Stream.Phases[1].Chat != "old-chat" {
		t.Fatalf("the conversations deleted were not the dropped job's own as it stood: %+v", was)
	}
	if got := f.folders.deleted(); len(got) != 1 || got[0] != it.ID {
		t.Fatalf("folders deleted %v, want the dropped job's", got)
	}
	cur, err := f.st.Get(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cur.Talk != "" || len(cur.Thread) != 0 || !cur.Heard.IsZero() || cur.Stream == nil || cur.Stream.Room != "" {
		t.Fatalf("the dropped job still names a conversation: talk %q thread %+v heard %v stream %+v", cur.Talk, cur.Thread, cur.Heard, cur.Stream)
	}
	for _, ph := range cur.Stream.Phases {
		if ph.Chat != "" || ph.Handle != "" {
			t.Fatalf("step %s still names its chat: %+v", ph.Name, ph)
		}
		for _, r := range ph.Rounds {
			if r.Chat != "" {
				t.Fatalf("a round of %s still names its chat: %+v", ph.Name, r)
			}
		}
	}
	if len(cur.Stream.Phases) != 3 || cur.Stream.Spent != 1.25 || cur.Stream.Phases[0].Rounds[0].Summary != "first plan" || len(cur.Notes) != 1 || len(cur.Adapted) != 1 {
		t.Fatalf("the dropped job lost what its run did: %+v notes %v adapted %v", cur.Stream, cur.Notes, cur.Adapted)
	}
	for _, ev := range cur.Activity {
		if strings.HasPrefix(ev.What, "dropped · ") {
			t.Fatalf("a delete that worked left a note: %+v", cur.Activity)
		}
	}
	kept, err := f.st.Get(other.ID)
	if err != nil || kept.Talk != other.Talk || kept.Stream == nil || kept.Stream.Room != "5f4e3d2c1b0a" || kept.DoneHow != "" {
		t.Fatalf("another job was touched: %+v %v", kept, err)
	}
}

// A CONVERSATION DELETE THAT FAILS IS NOT A FAILED DROP: the job is dropped,
// no longer names any of its conversations, so nothing points at one half
// gone, and its activity says they were not all deleted and why.
func TestV2DropWhoseChatDeleteFailsStillDropsAndSaysSo(t *testing.T) {
	f := newV2Jobs(t)
	stream := recordedRun()
	stream.Room = "0a1b2c3d4e5f"
	it := f.create(t, factory.Item{Num: 53, Origin: factory.OriginForge, State: factory.StateNew, Assignee: "sam",
		Talk: "/places/job-53/transcript.jsonl", Stream: stream})
	f.chats.refuse = fmt.Errorf("job-53: the conversation's owner could not finish stopping")
	f.post(t, it, "drop", "", 200)
	cur, err := f.st.Get(it.ID)
	if err != nil || cur.DoneHow != "dropped" {
		t.Fatalf("not dropped: %+v %v", cur, err)
	}
	if cur.Talk != "" || cur.Stream.Room != "" || cur.Stream.Phases[1].Chat != "" {
		t.Fatalf("the dropped job still names a conversation: talk %q stream %+v", cur.Talk, cur.Stream)
	}
	if n := len(cur.Activity); n == 0 || cur.Activity[n-1].What != "dropped · its conversations were not all deleted: job-53: the conversation's owner could not finish stopping" {
		t.Fatalf("activity %+v", cur.Activity)
	}
	if j := f.read(t, it); j.State != "done" || j.DoneHow != "dropped" {
		t.Fatalf("drop %+v", j)
	}
}

// BRING BACK DELETES WHAT A DROP DID NOT: a job dropped before drops deleted
// conversations still names its agent and crew, and Bring back deletes them
// by the job as it stood. A job dropped since names none, so Bring back asks
// for none of them again.
func TestV2BringBackDeletesTheChatsOfAJobDroppedBefore(t *testing.T) {
	f := newV2Jobs(t)
	stream := recordedRun()
	stream.Room = "0a1b2c3d4e5f"
	old := f.create(t, factory.Item{Num: 61, Origin: factory.OriginForge, State: factory.StateDismissed, DoneHow: "dropped", DoneAt: now, Assignee: "sam",
		Talk: "/places/job-61/transcript.jsonl", Stream: stream})
	f.post(t, old, "restore", "", 200)
	asked := f.chats.asked()
	if len(asked) != 1 || asked[0].ID != old.ID || asked[0].Talk != old.Talk || asked[0].Stream == nil || asked[0].Stream.Room != "0a1b2c3d4e5f" || asked[0].Stream.Phases[1].Chat != "old-chat" {
		t.Fatalf("an old dropped job's conversations were not deleted by its own record: %+v", asked)
	}
	if asked[0].DoneHow != "dropped" {
		t.Fatalf("the conversations were deleted after the job was reset: %+v", asked[0])
	}
	if cur, err := f.st.Get(old.ID); err != nil || cur.Talk != "" || cur.Stream != nil || cur.State != factory.StateNew {
		t.Fatalf("not brought back fresh: %+v %v", cur, err)
	}

	fresh := f.create(t, factory.Item{Num: 62, Origin: factory.OriginForge, State: factory.StateNew, Assignee: "sam", Talk: "/places/job-62/transcript.jsonl"})
	f.post(t, fresh, "drop", "", 200)
	f.post(t, fresh, "restore", "", 200)
	asked = f.chats.asked()
	if len(asked) != 3 || asked[1].Talk != fresh.Talk || asked[2].ID != fresh.ID || asked[2].Talk != "" || asked[2].Stream != nil {
		t.Fatalf("Bring back asked again for what the drop deleted: %+v", asked)
	}
}

// BRING BACK DELETES AGAIN WHAT CAME BACK SINCE THE DROP, FIRST: a folder or
// branch an agent's write or a shell made after the drop is deleted by the
// job's own record, before the job is reset, so the change and branch the run
// recorded still name it. While that delete runs the job is still dropped,
// so a start racing the Bring back is refused and never launches a fresh run
// whose new folder the delete would take.
func TestV2BringBackDiscardsWhatCameBackFirst(t *testing.T) {
	f := newV2Jobs(t)
	it := f.create(t, factory.Item{Num: 63, Origin: factory.OriginForge, State: factory.StateNew, Assignee: "sam",
		Change: &factory.Change{Branch: "factory/63-test-job"}})
	f.post(t, it, "drop", "", 200)
	var raced []int
	f.folders.mu.Lock()
	f.folders.during = func(during factory.Item) {
		w := verbRequest(f.h, "POST", fmt.Sprintf("/v1/jobs/%d/run", during.ID), "")
		raced = append(raced, w.Code)
	}
	f.folders.mu.Unlock()
	f.post(t, it, "restore", "", 200)
	f.folders.mu.Lock()
	discarded := append([]factory.Item(nil), f.folders.discarded...)
	f.folders.mu.Unlock()
	if len(discarded) != 2 || discarded[0].ID != it.ID || discarded[1].ID != it.ID {
		t.Fatalf("discarded %+v, want the job's folder deleted by the drop and again by Bring back", discarded)
	}
	again := discarded[1]
	if again.DoneHow != "dropped" || again.Change == nil || again.Change.Branch != "factory/63-test-job" {
		t.Fatalf("Bring back deleted the folder after resetting the job, without its recorded branch: %+v", again)
	}
	if len(raced) != 1 || raced[0] != 409 {
		t.Fatalf("a start racing Bring back answered %v, want refused while the job is still dropped", raced)
	}
	cur, err := f.st.Get(it.ID)
	if err != nil || cur.State != factory.StateNew || cur.Change != nil || cur.DoneHow != "" {
		t.Fatalf("not brought back fresh: %+v %v", cur, err)
	}
	f.folders.mu.Lock()
	f.folders.during, f.folders.refuse = nil, fmt.Errorf("could not delete the folder of #63: permission denied")
	f.folders.mu.Unlock()
	f.post(t, it, "drop", "", 200)
	f.post(t, it, "restore", "", 200)
	cur, _ = f.st.Get(it.ID)
	if cur.State != factory.StateNew {
		t.Fatalf("a folder that could not be deleted stopped Bring back: %+v", cur)
	}
	notes := []string{}
	for _, ev := range cur.Activity {
		notes = append(notes, ev.What)
	}
	if !slices.Contains(notes, "brought back · the local branch and folder were kept: could not delete the folder of #63: permission denied") {
		t.Fatalf("activity %v", notes)
	}
}

// BRING BACK IS THE ONE WAY OUT OF A DROP: the job is in Backlog again with
// no drop left on it, a start works, and the poll treats it as any other item.
func TestV2RestoreBringsADroppedJobBackToBacklog(t *testing.T) {
	f := newV2Jobs(t)
	it := f.create(t, factory.Item{Num: 8, Origin: factory.OriginForge, State: factory.StateNew, Assignee: "sam"})
	if f.read(t, it).Can.Restore {
		t.Fatal("a job that was never dropped offers Bring back")
	}
	f.post(t, it, "restore", "", 409)
	f.post(t, it, "drop", "", 200)
	if j := f.read(t, it); !j.Can.Restore {
		t.Fatalf("a dropped job does not offer Bring back: %+v", j.Can)
	}
	f.person = "mei"
	if f.read(t, it).Can.Restore {
		t.Fatal("someone else's dropped job offers them Bring back")
	}
	f.post(t, it, "restore", "", 403)
	f.person = "sam"
	f.post(t, it, "restore", "", 200)
	j := f.read(t, it)
	if j.State != "backlog" || j.DoneHow != "" || !j.DoneAt.IsZero() || j.Can.Restore {
		t.Fatalf("restored %+v", j)
	}
	f.post(t, it, "restore", "", 409)
	fresh := it
	fresh.Title = "changed on GitHub"
	if err := github.Merge(f.st, "github", []factory.Item{fresh}); err != nil {
		t.Fatal(err)
	}
	if j := f.read(t, it); j.State != "backlog" || j.Title != "changed on GitHub" {
		t.Fatalf("the poll does not treat a restored job normally: %+v", j)
	}
	f.post(t, it, "run", "", 200)
	if f.read(t, it).State != "queued" {
		t.Fatal("a restored job does not start")
	}
	events, _ := f.rec.Since(context.Background(), time.Time{})
	var kinds []string
	for _, e := range events {
		kinds = append(kinds, e.Kind)
	}
	if strings.Join(kinds, " ") != "dropped restored started" {
		t.Fatalf("events %v", kinds)
	}
}

// A JOB BROUGHT BACK IS A FRESH JOB, AS IF IT HAD NEVER RUN: no steps run, $0
// spent, no failure, change, commits, runner or pull request, and nothing said
// about it before: no conversation, nothing heard, no notes, no crew, and its
// steps are the repository's recipe again. What the job is stays: its issue,
// assignee, priority, budget, model and activity. The earlier pull request is
// forgotten, never closed, and the receipt names it. The job's conversations
// are deleted by the job as it stood, so the delete reaches exactly the ones
// it recorded.
func TestV2RestoreGivesAFreshBacklogJob(t *testing.T) {
	f := newV2Jobs(t)
	stages := []factory.Stage{
		{Name: "plan", Kind: factory.StageChat, Ask: "plan it", On: true},
		{Name: "write", Kind: factory.StageChat, Ask: "write it, and keep the ledger", On: true, By: factory.ByYou},
		{Name: "proof", Kind: factory.StageChat, Ask: "prove it", On: true},
	}
	pr := "https://github.com/acme/api/pull/90"
	run := recordedRun()
	run.Room = "0a1b2c3d4e5f"
	it := f.create(t, factory.Item{
		Num: 41, Origin: factory.OriginForge, Title: "Total double-counts", Body: "the issue's words",
		State: factory.StateDismissed, DoneHow: "dropped", DoneAt: now, Assignee: "sam", Cap: 4, Model: "acme/model-1",
		Priority: &factory.JobPriority{Value: "high", SetBy: "sam"}, Notes: []string{"use the ledger"},
		Stages: stages, Adapted: []string{"you set write: keep the ledger"}, Stream: run,
		Talk: "/places/job-41/transcript.jsonl", Heard: now, Thread: []factory.Message{{Text: "use the ledger"}},
		Failure:         &factory.Failure{Code: factory.CodeStopped, Step: "write"},
		Change:          &factory.Change{Branch: "factory/41-total-double-counts"},
		ChangeBase:      "0123456789abcdef0123456789abcdef01234567",
		HeadCommit:      "89abcdef0123456789abcdef0123456789abcdef",
		Proof:           []factory.Claim{{Text: "totals once", OK: true}},
		PullRequest:     &factory.PullRequest{Number: 90, URL: pr, State: factory.PROpen},
		Activity:        []factory.Event{{At: now, What: factory.EventArrived}},
		WorktreeMachine: "mac-1",
	})
	f.post(t, it, "restore", "", 200)
	j := f.read(t, it)
	if j.State != "backlog" || j.Cost != 0 || j.Failure != nil || j.Change != nil || j.HeadCommit != "" || j.Runner != "" || j.PR != nil || len(j.Claims) != 0 || j.DoneHow != "" {
		t.Fatalf("not fresh: state %s cost %v failure %+v change %+v head %q runner %q pr %+v claims %+v", j.State, j.Cost, j.Failure, j.Change, j.HeadCommit, j.Runner, j.PR, j.Claims)
	}
	recipe := pipeline.Defaults().Recipe().For(factory.KindIssue)
	if len(j.Steps) != len(recipe) {
		t.Fatalf("steps %+v, want the recipe's %+v", j.Steps, recipe)
	}
	for i, s := range j.Steps {
		if s.InRun || (s.State != api.StepPending && s.State != api.StepSkipped) || s.Name != recipe[i].Name {
			t.Fatalf("a step of the old run survived: %+v", s)
		}
	}
	if j.Assignee != "sam" || j.Budget != 4 || j.Priority == nil || j.Priority.Value != "high" || j.Title != "Total double-counts" {
		t.Fatalf("the job's own facts were lost: %+v", j)
	}
	cur, err := f.st.Get(it.ID)
	if err != nil || cur.Stream != nil || cur.ChangeBase != "" || cur.Body != "the issue's words" || cur.Model != "acme/model-1" || len(cur.Activity) == 0 || cur.Activity[0].What != factory.EventArrived {
		t.Fatalf("stored %+v %v", cur, err)
	}
	if cur.Talk != "" || !cur.Heard.IsZero() || len(cur.Thread) != 0 || len(cur.Notes) != 0 || len(cur.Adapted) != 0 {
		t.Fatalf("something said before survived: talk %q heard %v thread %+v notes %+v adapted %+v", cur.Talk, cur.Heard, cur.Thread, cur.Notes, cur.Adapted)
	}
	for i, s := range cur.Stages {
		if s.Name != recipe[i].Name || s.Ask != recipe[i].Ask || s.By != recipe[i].By {
			t.Fatalf("stage %d is not the recipe's: %+v", i, s)
		}
	}
	asked := f.chats.asked()
	if len(asked) != 1 || asked[0].ID != it.ID || asked[0].Talk != it.Talk || asked[0].Stream == nil || asked[0].Stream.Room != "0a1b2c3d4e5f" || asked[0].Stream.Phases[1].Chat != "old-chat" {
		t.Fatalf("the conversations deleted were not the job's own: %+v", asked)
	}
	for _, ev := range cur.Activity {
		if strings.HasPrefix(ev.What, "brought back · ") {
			t.Fatalf("a delete that worked left a note: %+v", cur.Activity)
		}
	}
	events, _ := f.rec.Since(context.Background(), time.Time{})
	if len(events) != 1 || events[0].Kind != record.Restored || events[0].Detail != "earlier pr · "+pr {
		t.Fatalf("events %+v", events)
	}
}

// A DELETE THAT FAILS NEVER FAILS BRING BACK. The job is back, fresh, and no
// longer names its old conversation or crew, so neither can leak into its next
// run; its activity says what was kept. A job that was not dropped is refused
// before anything is deleted.
func TestV2RestoreWhoseChatDeleteFailsStillRestores(t *testing.T) {
	f := newV2Jobs(t)
	live := f.create(t, factory.Item{Num: 9, Origin: factory.OriginForge, State: factory.StateNew, Assignee: "sam", Talk: "/places/job-9/transcript.jsonl"})
	f.post(t, live, "restore", "", 409)
	if asked := f.chats.asked(); len(asked) != 0 {
		t.Fatalf("a refused Bring back deleted conversations: %+v", asked)
	}
	run := recordedRun()
	run.Room = "0a1b2c3d4e5f"
	it := f.create(t, factory.Item{
		Num: 44, Origin: factory.OriginForge, State: factory.StateDismissed, DoneHow: "dropped", DoneAt: now, Assignee: "sam",
		Talk: "/places/job-44/transcript.jsonl", Notes: []string{"use the ledger"}, Stream: run,
	})
	f.chats.refuse = fmt.Errorf("job-44: the conversation's owner could not finish stopping")
	f.post(t, it, "restore", "", 200)
	cur, err := f.st.Get(it.ID)
	if err != nil || cur.DoneHow != "" || cur.State != factory.StateNew {
		t.Fatalf("not brought back: %+v %v", cur, err)
	}
	if cur.Stream != nil || cur.Talk != "" || len(cur.Notes) != 0 {
		t.Fatalf("the old room or conversation is still linked: stream %+v talk %q notes %+v", cur.Stream, cur.Talk, cur.Notes)
	}
	warning := false
	for _, event := range cur.Activity {
		if event.What == "brought back · its conversations were not all deleted: job-44: the conversation's owner could not finish stopping" {
			warning = true
		}
	}
	if !warning {
		t.Fatalf("activity %+v", cur.Activity)
	}
	if j := f.read(t, it); j.State != "backlog" {
		t.Fatalf("restored %+v", j)
	}
}

// A PERSON'S START READS THE ONE ITEM AGAIN FIRST. An issue closed on GitHub
// is finished instead of started and the person is told in one sentence; an
// open one starts; a GitHub that cannot be reached never stops a start; and an
// item that is not on GitHub is not read at all.
func TestV2StartRechecksTheItemOnGitHub(t *testing.T) {
	f := newV2Jobs(t)
	f.h.Close()
	standing, reads := map[int]string{}, map[int]int{}
	f.cfg.Recheck = func(_ context.Context, it factory.Item) (string, error) {
		reads[it.Num]++
		if it.Num == 404 {
			return "", fmt.Errorf("dial tcp: no route")
		}
		return standing[it.Num], nil
	}
	f.h = serve.New(f.seam, f.cfg)

	closed := f.create(t, factory.Item{Num: 12, Origin: factory.OriginForge, State: factory.StateNew, Assignee: "sam"})
	standing[12] = github.StandingClosed
	w := f.post(t, closed, "start_now", "", 409)
	var refusal struct {
		Error    string `json:"error"`
		NowState string `json:"now_state"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &refusal)
	if refusal.Error != "#12 was closed on GitHub, so it wasn't started." || refusal.NowState != "done" {
		t.Fatalf("refusal %+v", refusal)
	}
	if j := f.read(t, closed); j.State != "done" || j.DoneHow != "closed" || j.DoneAt.IsZero() {
		t.Fatalf("closed job %+v", j)
	}
	if cur, _ := f.st.Get(closed.ID); cur.Marked {
		t.Fatal("a job that was not started is marked to start")
	}

	legacy := f.create(t, factory.Item{Num: 13, Kind: factory.KindPR, State: factory.StateNew, Assignee: "sam"})
	f.post(t, legacy, "run", "", 404)
	if cur, _ := f.st.Get(legacy.ID); cur.State != factory.StateNew {
		t.Fatal("unsupported PR job changed")
	}

	open := f.create(t, factory.Item{Num: 14, Origin: factory.OriginForge, State: factory.StateNew, Assignee: "sam"})
	f.post(t, open, "run", "", 200)
	if f.read(t, open).State != "queued" {
		t.Fatal("an open issue did not start")
	}

	away := f.create(t, factory.Item{Num: 404, Origin: factory.OriginForge, State: factory.StateNew, Assignee: "sam"})
	f.post(t, away, "run", "", 200)
	if f.read(t, away).State != "queued" {
		t.Fatal("a GitHub that cannot be reached stopped a start")
	}

	typed := f.create(t, factory.Item{Origin: factory.OriginTerminal, State: factory.StateNew, Assignee: "sam"})
	f.post(t, typed, "run", "", 200)
	if reads[0] != 0 || reads[12] != 1 || reads[13] != 0 || reads[14] != 1 || reads[404] != 1 {
		t.Fatalf("reads %v", reads)
	}
	events, _ := f.rec.Since(context.Background(), time.Time{})
	var got []string
	for _, e := range events {
		if e.Kind == record.SourceClosed {
			got = append(got, fmt.Sprintf("%d %s %s", e.Job, e.Verb, e.Actor.Kind))
		}
	}
	if want := []string{fmt.Sprintf("%d closed factory", closed.ID)}; strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("recorded %v, want %v", got, want)
	}
}

// CHECK GITHUB NOW WAKES THE POLL AND DOES NOT WAIT FOR IT: the answer is the
// factory alone, and the store's poll-now mark, which a waiting poll in any
// process on this machine looks at, has moved.
func TestV2GitHubPollNudgesThePollAndReturns(t *testing.T) {
	f := newV2Jobs(t)
	if !f.st.PollNudged().IsZero() {
		t.Fatal("a new store was already nudged")
	}
	w := verbRequest(f.h, "POST", "/v1/github/poll", "")
	checkJSON(t, w, 200)
	if f.st.PollNudged().IsZero() {
		t.Fatal("Check GitHub now did not nudge the poll")
	}
	checkJSON(t, verbRequest(f.h, "GET", "/v1/github/poll", ""), 405)
}

func TestV2PriorityBudgetAndAssignmentTransitions(t *testing.T) {
	f := newV2Jobs(t)
	it := f.create(t, factory.Item{State: factory.StateNew, Labels: []string{"bug", "size:XL"}})
	f.post(t, it, "assign_to_me", "", 200)
	f.post(t, it, "priority", `{"priority":"urgent"}`, 200)
	f.post(t, it, "raise_budget", `{"budget":10}`, 200)
	if j := f.read(t, it); j.Priority.Value != "urgent" || j.Priority.SetBy != "sam" || j.Size != "XL" || j.Budget != 10 || j.State != "backlog" {
		t.Fatalf("metadata %+v", j)
	}
	f.post(t, it, "run", "", 200)
	f.post(t, it, "unassign", "", 200)
	if j := f.read(t, it); j.State != "backlog" || j.AssignedTo != "" {
		t.Fatalf("unassign %+v", j)
	}
	f.post(t, it, "change_assignee", `{"assignee":"mei"}`, 403)
	f.post(t, it, "assign_to_me", "", 200)
	f.post(t, it, "change_assignee", `{"assignee":"mei"}`, 200)
	f.post(t, it, "nudge", `{"words":"please look"}`, 200)
	events, _ := f.rec.Since(context.Background(), time.Time{})
	last := events[len(events)-1]
	if last.Kind != record.Nudged || last.Detail != "@mei please look" {
		t.Fatalf("nudge %+v", last)
	}
	f.post(t, it, "raise_budget", `{"budget":20}`, 403)
}

func TestV2ApprovalsWaitUndoAndRecoverOnRestart(t *testing.T) {
	for _, kind := range []string{"plan", "proof", "start"} {
		t.Run(kind, func(t *testing.T) {
			f := newV2Jobs(t)
			it := factory.Item{State: factory.StateNeedsYou, Assignee: "sam", Question: "approve?", QKind: factory.QKindApprove}
			if kind == "start" {
				it.QKind = factory.QKindAskFirst
				it.AskedFirstReason = "touches auth/"
			}
			if kind == "proof" {
				it.State = factory.StateLanded
				it.Question = ""
				it.QKind = ""
				it.HeadCommit = approvedHead
				it.Proof = []factory.Claim{{Text: "works", OK: true}}
			}
			it = f.create(t, it)
			body := "{}"
			if kind == "proof" {
				body = `{"commit":"` + approvedHead + `"}`
			}
			f.post(t, it, "approve", body, 200)
			j := f.read(t, it)
			if j.Approval.Kind != kind || j.Approval.State != "approving" || j.Approval.UndoUntil.Sub(j.Approval.At) != 5*time.Second {
				t.Fatalf("approval %+v", j.Approval)
			}
			if events, _ := f.rec.Since(context.Background(), time.Time{}); len(events) != 0 {
				t.Fatal("approval ran before Undo")
			}
			f.post(t, it, "undo", "", 200)
			f.clock.Add(int64(factory.ApprovalUndo))
			f.read(t, it)
			if events, _ := f.rec.Since(context.Background(), time.Time{}); len(events) != 1 || events[0].Kind != record.Undone {
				t.Fatal("Undo let approval through")
			}
			f.post(t, it, "approve", body, 200)
			f.h.Close()
			f.clock.Add(int64(factory.ApprovalUndo))
			f.h = serve.New(f.seam, f.cfg)
			j = f.read(t, it)
			if j.Approval == nil || j.Approval.State != "approved" {
				t.Fatalf("restart approval %+v", j.Approval)
			}
			events, _ := f.rec.Since(context.Background(), time.Time{})
			if len(events) != 2 || events[1].Kind != record.Approved || events[1].Handle != "sam" {
				t.Fatalf("receipts %+v", events)
			}
			f.read(t, it)
			events, _ = f.rec.Since(context.Background(), time.Time{})
			if len(events) != 2 {
				t.Fatal("approval repeated")
			}
		})
	}
}

func TestV2PlainQuestionFirstAnswerWinsExactCopy(t *testing.T) {
	f := newV2Jobs(t)
	f.seam.Answer = func(id int, yes bool, words string) error {
		return f.st.Update(id, func(it *factory.Item) error {
			it.State = factory.StateQueued
			it.Question = ""
			it.QKind = ""
			return nil
		})
	}
	f.h.Close()
	f.h = serve.New(f.seam, f.cfg)
	it := f.create(t, factory.Item{State: factory.StateNeedsYou, Assignee: "raj", Question: "which API?", QKind: "scope"})
	if j := f.read(t, it); j.WaitingFor != "@raj" {
		t.Fatalf("waiting %+v", j)
	}
	f.person = "jm"
	f.post(t, it, "answer", `{"words":"v2"}`, 200)
	f.clock.Add(int64(10 * time.Second))
	f.person = "sam"
	w := f.post(t, it, "answer", `{"words":"v1"}`, 409)
	var got struct{ Error string }
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if got.Error != "@jm answered this 10s ago. Your answer wasn't sent." {
		t.Fatal(got.Error)
	}
}

func TestV2ClosedPRRunAgainAndMergedRefusal(t *testing.T) {
	f := newV2Jobs(t)
	it := f.create(t, factory.Item{State: factory.StateShipped, Assignee: "sam", PullRequest: &factory.PullRequest{State: factory.PRClosed}})
	if j := f.read(t, it); j.DoneHow != "closed" || j.State != "done" {
		t.Fatalf("closed %+v", j)
	}
	f.post(t, it, "run_again", "", 200)
	if f.read(t, it).State != "queued" {
		t.Fatal("Run again did not queue")
	}
	merged := f.create(t, factory.Item{State: factory.StateShipped, Assignee: "sam", PullRequest: &factory.PullRequest{State: factory.PRMerged}})
	f.post(t, merged, "run_again", "", 409)
}

func TestSoloUnassignedPlanApprovalKeepsUndoAndNoAssignment(t *testing.T) {
	f := newV2Jobs(t)
	f.h.Close()
	f.cfg.Mode = api.FactorySolo
	f.h = serve.New(f.seam, f.cfg)
	it := f.create(t, factory.Item{State: factory.StateNeedsYou, Question: "plan ready?", QKind: factory.QKindApprove})
	j := f.read(t, it)
	w := verbRequest(f.h, "GET", "/v1/queue", "")
	checkJSON(t, w, 200)
	var q api.Queue
	if err := json.Unmarshal(w.Body.Bytes(), &q); err != nil || q.Mode != api.FactorySolo || len(q.Jobs) != 1 || !q.Jobs[0].Can.Approve || q.Jobs[0].AssignedTo != "" {
		t.Fatalf("solo queue approval: %+v %v", q, err)
	}
	if !j.Can.Approve || j.AssignedTo != "" {
		t.Fatalf("solo plan cannot be approved privately: %+v", j.Can)
	}
	f.post(t, it, "approve", "{}", 200)
	j = f.read(t, it)
	if !j.Can.Undo || j.Can.Approve || j.Approval.State != "approving" {
		t.Fatalf("missing Undo: %+v %+v", j.Can, j.Approval)
	}
	f.post(t, it, "undo", "", 200)
	if !f.read(t, it).Can.Approve {
		t.Fatal("Undo did not restore approval")
	}
	f.post(t, it, "approve", "{}", 200)
	f.clock.Add(int64(factory.ApprovalUndo))
	if j = f.read(t, it); j.Approval == nil || j.Approval.State != "approved" {
		t.Fatalf("approval did not settle: %+v", j.Approval)
	}
	stored, err := f.st.Get(it.ID)
	if err != nil || stored.Person() != "" {
		t.Fatalf("solo approval invented an owner: %+v %v", stored, err)
	}
}

// A HELD START A PERSON APPROVES IS THEIRS: on a solo factory the job a rule
// held (nobody on it) launches with the approver as its starter, so Auto has
// an owner at the plan.
func TestSoloApprovedHeldStartBelongsToTheApprover(t *testing.T) {
	f := newV2Jobs(t)
	f.h.Close()
	f.cfg.Mode = api.FactorySolo
	f.h = serve.New(f.seam, f.cfg)
	it := f.create(t, factory.Item{State: factory.StateNeedsYou, Question: "Approval needed: start · ask first · touch auth/", QKind: factory.QKindAskFirst, AskedFirstReason: "ask first · touch auth/", Autonomy: factory.AutonomyAuto, Cap: 3})
	f.post(t, it, "approve", "{}", 200)
	f.clock.Add(int64(factory.ApprovalUndo))
	if j := f.read(t, it); j.Approval == nil || j.Approval.State != "approved" {
		t.Fatalf("approval did not settle: %+v", j.Approval)
	}
	stored, err := f.st.Get(it.ID)
	if err != nil || stored.Person() != "sam" {
		t.Fatalf("an approved held start has person %q: %v", stored.Person(), err)
	}
	stored.State, stored.QKind = factory.StateNeedsYou, factory.QKindApprove
	if why := factory.AutonomyBlockReason(stored); strings.Contains(why, "owner is required") {
		t.Fatalf("Auto still wants an owner: %q", why)
	}
}

func TestTeamUnassignedPlanCannotBeApproved(t *testing.T) {
	f := newV2Jobs(t)
	it := f.create(t, factory.Item{State: factory.StateNeedsYou, Question: "plan ready?", QKind: factory.QKindApprove})
	if f.read(t, it).Can.Approve {
		t.Fatal("team unassigned job gained approval")
	}
	f.post(t, it, "approve", "{}", 422)
	stored, _ := f.st.Get(it.ID)
	if stored.Approval != nil {
		t.Fatal("refused approval left a decision")
	}
}

func TestSoloCallerKeepsPersonalDismissAndStopUndo(t *testing.T) {
	f := newV2Jobs(t)
	f.h.Close()
	f.cfg.Mode = api.FactorySolo
	f.h = serve.New(f.seam, f.cfg)
	it := f.create(t, factory.Item{State: factory.StateNew})
	if !f.read(t, it).Can.Dismiss {
		t.Fatal("signed-in solo caller cannot dismiss")
	}
	f.post(t, it, "dismiss", "", 200)
	if !f.read(t, it).Dismissed {
		t.Fatal("personal dismissal was lost")
	}
	active := f.create(t, factory.Item{State: factory.StateQueued})
	f.post(t, active, "stop", "", 200)
	if !f.read(t, active).Can.Undo {
		t.Fatal("solo caller cannot undo their stop")
	}
	f.post(t, active, "undo", "", 200)
	if f.read(t, active).State != api.StateQueued {
		t.Fatal("stop Undo failed")
	}
}

func TestSoloApprovalRequiresGitHubIdentityWithoutInventingAnAssignee(t *testing.T) {
	f := newV2Jobs(t)
	f.h.Close()
	f.cfg.Mode = api.FactorySolo
	f.person = ""
	f.h = serve.New(f.seam, f.cfg)
	it := f.create(t, factory.Item{State: factory.StateNeedsYou, Question: "plan ready?", QKind: factory.QKindApprove})
	if f.read(t, it).Can.Approve {
		t.Fatal("unknown caller gained approval")
	}
	w := f.post(t, it, "approve", "{}", 422)
	if !strings.Contains(w.Body.String(), "Sign in to GitHub") {
		t.Fatalf("not actionable: %s", w.Body.String())
	}
}

// A JOB'S MODEL: its person chooses it, the job reads it back, a model id with
// spaces is refused, going back to the default clears it, another person may
// not, and a worker holding the job is told.
func TestV2AJobsModelIsItsPersonsToChoose(t *testing.T) {
	f := newV2Jobs(t)
	it := f.create(t, factory.Item{State: factory.StateNew, Assignee: "sam"})
	f.post(t, it, "model", `{"model":"  vendor/model-a  "}`, 200)
	if j := f.read(t, it); j.Model != "vendor/model-a" {
		t.Fatalf("model = %q", j.Model)
	}
	f.post(t, it, "model", `{"model":"two words"}`, 422)
	f.post(t, it, "model", `{"model":""}`, 200)
	if j := f.read(t, it); j.Model != "" {
		t.Fatalf("default = %q", j.Model)
	}
	f.person = "mei"
	f.post(t, it, "model", `{"model":"vendor/model-b"}`, 403)
	f.person = "sam"

	desk, err := lease.Open(f.st, f.cfg.LeaseOptions)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.st.Update(it.ID, func(cur *factory.Item) error { cur.State = factory.StateQueued; return nil }); err != nil {
		t.Fatal(err)
	}
	worker := lease.Worker{ID: "worker", Machine: "laptop", Handle: "sam", Team: true, Slots: 1}
	held, err := desk.Pull(worker)
	if err != nil {
		t.Fatal(err)
	}
	f.post(t, it, "model", `{"model":"vendor/model-c","automatic":false}`, 200)
	signals, err := desk.DrainLive(held.ID, held.Worker)
	if err != nil {
		t.Fatal(err)
	}
	if len(signals) != 1 || signals[0].Verb != lease.SignalModel || signals[0].Words != "vendor/model-c" || signals[0].Automatic == nil || *signals[0].Automatic {
		t.Fatalf("signals = %+v", signals)
	}
}

func TestSoloStartRecordsBranchOwnerWithoutAssignment(t *testing.T) {
	f := newV2Jobs(t)
	it := f.create(t, factory.Item{State: factory.StateNew})
	f.post(t, it, "start_now", "", 200)
	saved, err := f.st.Get(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Starter != "sam" || saved.Person() != "sam" {
		t.Fatalf("solo start lost caller: starter=%q person=%q", saved.Starter, saved.Person())
	}
}

func TestStandalonePRAndCIJobCreationIsRefusedBeforeOpeningAnIssue(t *testing.T) {
	f := newV2Jobs(t)
	f.seam.New = func(string, string) (int, error) { t.Fatal("unsupported job reached creation"); return 0, nil }
	f.h.Close()
	f.h = serve.New(f.seam, f.cfg)
	before, err := f.st.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"pr", "ci"} {
		body := fmt.Sprintf(`{"repo":"acme/api","title":"unsupported","pipeline":%q,"issue":true}`, name)
		w := verbRequest(f.h, "POST", "/v1/jobs", body)
		if w.Code != 422 || !strings.Contains(w.Body.String(), "issue pipeline only") {
			t.Fatalf("%s job creation: %d %s", name, w.Code, w.Body)
		}
	}
	after, err := f.st.List()
	if err != nil || len(after) != len(before) {
		t.Fatalf("unsupported job was saved: %v %v", after, err)
	}
}

func TestJobBudgetCanBeLoweredButNotBelowRecordedSpend(t *testing.T) {
	f := newV2Jobs(t)
	it := f.create(t, factory.Item{State: factory.StateNew, Cap: 5, ChatSpent: 4})
	f.post(t, it, "raise_budget", `{"budget":10}`, 200)
	f.post(t, it, "raise_budget", `{"budget":5}`, 200)
	if j := f.read(t, it); j.Budget != 5 {
		t.Fatalf("budget %v", j.Budget)
	}
	_ = f.st.Annotate(it.ID, func(cur *factory.Item) error { cur.ChatSpent = 5.01; return nil })
	f.post(t, it, "raise_budget", `{"budget":5}`, 422)
	f.post(t, it, "raise_budget", `{"budget":10}`, 200)
	_ = f.st.Annotate(it.ID, func(cur *factory.Item) error { cur.ChatSpent = 5; return nil })
	f.post(t, it, "raise_budget", `{"budget":5}`, 200)
}

func TestV2BudgetChangePreservesManualDecisionAndNudgesObservation(t *testing.T) {
	for _, mode := range []string{"auto", "manual", "takeover", "critical"} {
		t.Run(mode, func(t *testing.T) {
			f := newV2Jobs(t)
			it := f.create(t, factory.Item{State: factory.StateNeedsYou, Cap: 5, Autonomy: factory.AutonomyAuto, Assignee: "sam", QKind: "cap", Question: "Budget reached", Stream: &factory.Stream{Spent: 5}})
			_ = f.st.Update(it.ID, func(cur *factory.Item) error {
				switch mode {
				case "manual":
					cur.Autonomy = factory.AutonomyManual
				case "takeover":
					cur.Manual = true
				case "critical":
					cur.QKind = "scope"
					cur.Question = "Choose the public API"
				}
				return nil
			})
			f.post(t, it, "raise_budget", `{"budget":10}`, 200)
			got, _ := f.st.Get(it.ID)
			if got.Cap != 10 || got.Stream.Spent != 5 || f.st.PollNudged().IsZero() {
				t.Fatal("cap did not persist or wake observation")
			}
			if mode == "auto" {
				if got.State != factory.StateQueued || got.Question != "" {
					t.Fatal("automatic cap recovery stranded")
				}
			} else if got.State != factory.StateNeedsYou || got.Question == "" {
				t.Fatal("budget edit erased a separate user decision")
			}
		})
	}
}

// START NOW ON A QUEUED JOB IS ONE SLOT ABOVE JOBS AT ONCE and launches
// nothing, while a start from Backlog waits its turn; Hold keeps a queued job
// from starting until Resume, and each is a person's recorded decision.
func TestV2StartNowPassesTheLimitOnceAndHoldWaitsForResume(t *testing.T) {
	f := newV2Jobs(t)
	backlog := f.create(t, factory.Item{State: factory.StateNew, Assignee: "sam"})
	f.post(t, backlog, "start_now", "", 200)
	if cur, _ := f.st.Get(backlog.ID); cur.OverLimit || cur.State != factory.StateQueued {
		t.Fatalf("a start from backlog passed the limit %+v", cur)
	}
	f.post(t, backlog, "start_now", "", 200)
	if j := f.read(t, backlog); !j.OverLimit || j.Can.StartNow || j.StatePresentation.Title != "Starting now" {
		t.Fatalf("started-now job reads %+v", j)
	}

	queued := f.create(t, factory.Item{State: factory.StateNew, Assignee: "sam"})
	f.post(t, queued, "run", "", 200)
	j := f.read(t, queued)
	if j.OverLimit || !j.Can.StartNow || !j.Can.Hold || j.Can.Resume {
		t.Fatalf("queued job reads %+v", j)
	}
	f.post(t, queued, "pause", "", 200)
	if j := f.read(t, queued); !j.Held || j.Can.Hold || !j.Can.Resume || j.StatePresentation.Title != "Held" {
		t.Fatalf("held job reads %+v", j)
	}
	f.post(t, queued, "resume", "", 200)
	if j := f.read(t, queued); j.Held || !j.Can.Hold || j.State != "queued" {
		t.Fatalf("resumed job reads %+v", j)
	}
	f.post(t, queued, "pause", "", 200)
	f.post(t, queued, "start_now", "", 200)
	cur, _ := f.st.Get(queued.ID)
	if !cur.OverLimit || cur.State != factory.StateQueued || cur.Stream == nil || cur.Stream.Paused {
		t.Fatalf("start now on a held queued job %+v", cur)
	}
	events, _ := f.rec.Since(context.Background(), time.Time{})
	var kinds []string
	for _, e := range events {
		if e.Job == queued.ID {
			kinds = append(kinds, e.Kind+":"+e.Detail)
		}
	}
	want := []string{"started:", "held:", "resumed:", "held:", "started:above Jobs at once"}
	if strings.Join(kinds, ",") != strings.Join(want, ",") {
		t.Fatalf("recorded %v, want %v", kinds, want)
	}
}
