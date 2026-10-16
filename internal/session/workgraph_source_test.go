package session

// THE SOURCE READS WHAT THE APP LISTS. Every fixture here is a conversation
// that really ran its graph, wrote its checkpoint and its plan store, and was
// then read back off the disk by a source that never touched the live agent.

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/workgraph"
)

const sourceOwner = "aaaaaaaaaaaaaaaa"

// sourceFixture is a conversation with a three-level family of tasks, two
// handed-over run rows, a plan store three levels deep with a hard dependency,
// and a link to a task of another conversation.
type sourceFixture struct {
	agent  *Agent
	graph  *TaskGraph
	folder string
}

func newSourceFixture(t *testing.T) sourceFixture {
	t.Helper()
	folder := filepath.Join(t.TempDir(), sourceOwner)
	place := Place{Dir: folder}
	elsewhere := &fakeElsewhere{tasks: map[TaskRef]ExternalTask{elsewhereTask: {Exists: true, State: TaskRunning}}}
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.SessionFile = place.Transcript()
		config.Place = place
		config.ExternalTasks = elsewhere.read
	})
	seedPlanStore(t, filepath.Join(folder, planStoreFilename), sourceOwner,
		plandb.TaskSpec{ID: "a", Title: "Plan a"},
		plandb.TaskSpec{ID: "a1", Title: "Plan a one", ParentID: "a"},
		plandb.TaskSpec{ID: "a1x", Title: "Plan a one x", ParentID: "a1"},
		plandb.TaskSpec{ID: "b", Title: "Plan b", Dependencies: []plandb.Dependency{{TaskID: "a", Kind: plandb.DepBlocks}, {TaskID: "a1", Kind: plandb.DepSuggests}}},
	)
	armPlanStore(t, agent, filepath.Join(folder, planStoreFilename), sourceOwner)
	graph := agent.graph()
	hold := make(chan struct{})
	t.Cleanup(func() { close(hold) })
	graph.mu.Lock()
	graph.run = func(node *TaskNode) {
		if node.id == 1 {
			<-hold
		}
		node.finish("done", []string{"src/a.go", "/etc/passwd", "../outside"}, "task/x", mergeMerged)
		node.graph.complete(node, TaskDone)
	}
	graph.mu.Unlock()
	spec := func(parent uint64, depth int, deps ...uint64) taskSpec {
		return taskSpec{title: "task", summary: "s", brief: "do it", acceptance: "a", parent: parent, depth: depth, dependsOn: deps}
	}
	graph.admit(graph.reserve(), spec(0, 1))
	graph.admit(graph.reserve(), spec(1, 2))
	graph.admit(graph.reserve(), spec(2, 3, 1))
	two := graph.reserve()
	graph.keepRunRows(two, []TaskNotice{
		{ID: two, Run: "r1", Title: "Adaptive run", State: TaskDone},
		{ID: graph.reserve(), Run: "r1", Parent: two, Title: "Worker row", State: TaskDone},
	})
	return sourceFixture{agent: agent, graph: graph, folder: folder}
}

func factIDs(facts []workgraph.Fact, skipQuick bool) []string {
	var ids []string
	for _, f := range facts {
		if skipQuick && f.Node.Kind == workgraph.KindQuick {
			continue
		}
		ids = append(ids, f.Node.Ref.Task)
	}
	sort.Strings(ids)
	return ids
}

// EVERY ROW tasks.list SHOWS IS IN THE GRAPH: the graph's nodes, the rows of the
// conversation's runs and the sub-tasks of its plan store, at every level,
// compared with the live agent's own AppTasks and then again after a reopen.
func TestTheSourceHoldsEveryRowTheAppLists(t *testing.T) {
	f := newSourceFixture(t)
	awaitRecord(t, taskCheckpointPath(Place{Dir: f.folder}.Transcript()), 3, func(r taskRecord) bool { return r.State == TaskQueued }, "queued behind task 1")
	rows, err := f.agent.AppTasks()
	if err != nil {
		t.Fatal(err)
	}
	var live []string
	for _, row := range rows {
		live = append(live, row.ID)
	}
	sort.Strings(live)
	if len(live) < 9 {
		t.Fatalf("the fixture is not multi-level: %v", live)
	}
	source := NewWorkSource()
	owner, err := source.Owner(context.Background(), sourceOwner, f.folder)
	if err != nil {
		t.Fatal(err)
	}
	if got := factIDs(owner.Facts, true); strings.Join(got, ",") != strings.Join(live, ",") {
		t.Fatalf("the graph holds %v, the app lists %v", got, live)
	}
	if owner.Issue != "" {
		t.Fatalf("issue = %q", owner.Issue)
	}
	// Family, links, groups and the plan's own structure are the records'.
	byTask := func(id string) workgraph.GraphNode {
		fact, ok := owner.Find(id)
		if !ok {
			t.Fatalf("no fact %s", id)
		}
		return fact.Node
	}
	if n := byTask("3"); n.Parent != sourceOwner+":2" || n.Depth != 3 || n.Links != "recorded" {
		t.Fatalf("task 3 = %+v", n)
	}
	if fact, _ := owner.Find("3"); len(fact.Waits) != 1 || fact.Waits[0].On.ID() != sourceOwner+":1" || fact.Waits[0].Via != workgraph.ViaRecord {
		t.Fatalf("task 3 waits = %+v", fact.Waits)
	}
	if n := byTask("t-a1x"); n.Parent != sourceOwner+":t-a1" || n.Kind != "plan" || n.Group == nil || n.Group.ID != sourceOwner+":t-"+planRootID {
		t.Fatalf("plan row = %+v", n)
	}
	fact, _ := owner.Find("t-b")
	if len(fact.Waits) != 1 || fact.Waits[0].On.Task != "t-a" {
		t.Fatalf("plan b waits (suggests must not be a link) = %+v", fact.Waits)
	}
	if run := byTask("5"); run.Links != "unrecorded" || run.Group == nil || run.Group.Title != "Adaptive run" {
		t.Fatalf("run worker row = %+v", run)
	}

	// REOPEN: a new agent on the same folder lists the same rows, and the
	// source's answer does not depend on which of them asked.
	f.agent.Close()
	reopened, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.SessionFile = Place{Dir: f.folder}.Transcript()
		config.Place = Place{Dir: f.folder}
	})
	armPlanStore(t, reopened, filepath.Join(f.folder, planStoreFilename), sourceOwner)
	again, _ := reopened.AppTasks()
	var after []string
	for _, row := range again {
		after = append(after, row.ID)
	}
	sort.Strings(after)
	reread, err := NewWorkSource().Owner(context.Background(), sourceOwner, f.folder)
	if err != nil {
		t.Fatal(err)
	}
	if got := factIDs(reread.Facts, true); strings.Join(got, ",") != strings.Join(after, ",") {
		t.Fatalf("after a reopen the graph holds %v, the app lists %v", got, after)
	}
}

// Landed work says where it landed; paths outside the workspace are withheld
// and counted; a clock is only what the record carries.
func TestTheSourceStatesLandingFilesAndTheRecordedClock(t *testing.T) {
	f := newSourceFixture(t)
	awaitRecord(t, taskCheckpointPath(Place{Dir: f.folder}.Transcript()), 2, func(r taskRecord) bool { return r.State == TaskDone }, "done")
	owner, err := NewWorkSource().Owner(context.Background(), sourceOwner, f.folder)
	if err != nil {
		t.Fatal(err)
	}
	fact, _ := owner.Find("2")
	n := fact.Node
	if n.Landing == nil || n.Landing.Outcome != "merged" {
		t.Fatalf("landing = %+v", n.Landing)
	}
	if n.Files.Total != 1 || n.Files.Paths[0] != "src/a.go" || n.Files.Omitted != 2 || n.Files.Source != "changed" {
		t.Fatalf("files = %+v", n.Files)
	}
	if n.Clock.AdmittedAt == nil || n.Clock.StateAt == nil || n.Clock.EndedAt == nil {
		t.Fatalf("clock = %+v", n.Clock)
	}
	if n.Clock.AdmittedAt.After(time.Now()) || n.Clock.StateAt.Before(*n.Clock.AdmittedAt) {
		t.Fatalf("clock is not the events': %+v", n.Clock)
	}
	// A plan row keeps the store's own status word beside the mapped state, so
	// queued-and-waiting ("pending") is told from queued-and-free ("ready").
	if plan, _ := owner.Find("t-b"); plan.Node.StateDetail != string(plandb.StatusPending) && plan.Node.StateDetail != string(plandb.StatusReady) {
		t.Fatalf("plan b state_detail = %q", plan.Node.StateDetail)
	}
	// A plan row never carries a start it was not given.
	plan, _ := owner.Find("t-a")
	if plan.Node.Clock.StartedAt != nil || plan.Node.Clock.AdmittedAt == nil {
		t.Fatalf("plan clock = %+v", plan.Node.Clock)
	}
}

// An unchanged conversation is never reparsed; a changed one is read once.
func TestTheSourceNeverReparsesAnUnchangedConversation(t *testing.T) {
	f := newSourceFixture(t)
	source := NewWorkSource()
	ctx := context.Background()
	// The fixture's own tasks are still landing; wait until the files hold still.
	for last, still := "", 0; still < 5; time.Sleep(20 * time.Millisecond) {
		if now := source.Stamp(f.folder); now == last {
			still++
		} else {
			last, still = now, 0
		}
	}
	// The first read may read twice (opening a WAL store creates its side files);
	// after it, an unchanged folder costs nothing more.
	if _, err := source.Owner(ctx, sourceOwner, f.folder); err != nil {
		t.Fatal(err)
	}
	base := source.Parses()
	for i := 0; i < 5; i++ {
		if _, err := source.Owner(ctx, sourceOwner, f.folder); err != nil {
			t.Fatal(err)
		}
	}
	if source.Parses() != base {
		t.Fatalf("parses went %d -> %d across five reads of an unchanged folder", base, source.Parses())
	}
	before := source.Stamp(f.folder)
	f.graph.admit(f.graph.reserve(), taskSpec{title: "later", summary: "s", brief: "b", acceptance: "a", depth: 1})
	if source.Stamp(f.folder) == before {
		t.Fatal("an admission left the stamp as it was")
	}
	if _, err := source.Owner(ctx, sourceOwner, f.folder); err != nil {
		t.Fatal(err)
	}
	if source.Parses() <= base {
		t.Fatalf("a change was not read: parses = %d", source.Parses())
	}
}

// A folder that is not there is missing, a name that could walk out of the
// places root is refused, and a corrupt checkpoint is an issue and not a
// guess.
func TestTheSourceReportsWhatItCannotRead(t *testing.T) {
	source := NewWorkSource()
	ctx := context.Background()
	if _, err := source.Owner(ctx, sourceOwner, filepath.Join(t.TempDir(), "absent")); err != ErrWorkOwnerMissing {
		t.Fatalf("absent folder: %v", err)
	}
	if _, err := source.Owner(ctx, "../etc", t.TempDir()); err != ErrWorkOwnerMissing {
		t.Fatalf("unsafe owner: %v", err)
	}
	folder := filepath.Join(t.TempDir(), sourceOwner)
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	empty, err := source.Owner(ctx, sourceOwner, folder)
	if err != nil || len(empty.Facts) != 0 || empty.Issue != "" {
		t.Fatalf("a conversation with no checkpoint has no tasks and no issue: %+v %v", empty, err)
	}
	writeFile(t, Place{Dir: folder}.Tasks(), "{not json")
	bad, err := source.Owner(ctx, sourceOwner, folder)
	if err != nil || bad.Issue != WorkIssueUnreadable || len(bad.Facts) != 0 {
		t.Fatalf("corrupt checkpoint: %+v %v", bad, err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	folder2 := filepath.Join(t.TempDir(), sourceOwner)
	_ = os.MkdirAll(folder2, 0o755)
	if _, err := source.Owner(cancelled, sourceOwner, folder2); err == nil {
		t.Fatal("a cancelled read answered")
	}
}

// The external reader answers from the same projections: a task's state and its
// stored waits, "does not exist" for a conversation that was read and lacks the
// task, and an error for a conversation that cannot be read.
func TestTheExternalReaderAnswersFromTheSource(t *testing.T) {
	f := newSourceFixture(t)
	source := NewWorkSource()
	read := source.ExternalReader(func(owner string) (string, bool) {
		if owner == sourceOwner {
			return f.folder, true
		}
		return "", false
	})
	ctx := context.Background()
	got, err := read(ctx, TaskRef{Session: sourceOwner, ID: 3})
	if err != nil || !got.Exists || got.State != TaskQueued || len(got.Waits) != 1 || got.Waits[0] != (TaskRef{Session: sourceOwner, ID: 1}) {
		t.Fatalf("task 3 = %+v %v", got, err)
	}
	if got, err = read(ctx, TaskRef{Session: sourceOwner, ID: 999}); err != nil || got.Exists {
		t.Fatalf("task 999 = %+v %v", got, err)
	}
	if _, err = read(ctx, TaskRef{Session: "bbbbbbbbbbbbbbbb", ID: 1}); err == nil {
		t.Fatal("an unknown conversation was answered")
	}
}

func TestRelativePathsOnly(t *testing.T) {
	for raw, want := range map[string]string{"a/b.go": "a/b.go", "./a//b.go": "a/b.go", "a\\b.go": "a/b.go"} {
		if got, ok := wgRelPath(raw); !ok || got != want {
			t.Errorf("%q = %q %v", raw, got, ok)
		}
	}
	for _, raw := range []string{"", "/etc/x", "../x", "a/../../x", "C:/x", "x\x00y", ".", ".."} {
		if _, ok := wgRelPath(raw); ok {
			t.Errorf("%q was accepted", raw)
		}
	}
}

// The anchor to the person's words and the accepted route are what the record
// carries and nothing more: no pointer, no anchor; no crew, no route.
func TestTheProjectionCarriesTheRecordedAnchorAndRouteOnly(t *testing.T) {
	journal := filepath.Join("/x", "places", sourceOwner, placeTranscript)
	with := nodeFact(sourceOwner, taskRecord{ID: 1, Title: "t", Brief: "do it", State: TaskQueued, OriginJournal: journal, OriginLine: 7})
	if a := with.Node.Instructions.Anchor; a == nil || a.Agent != sourceOwner || a.Line != 7 {
		t.Fatalf("anchor = %+v", with.Node.Instructions)
	}
	for name, rec := range map[string]taskRecord{
		"no line":          {ID: 2, Title: "t", Brief: "b", OriginJournal: journal},
		"not a transcript": {ID: 3, Title: "t", Brief: "b", OriginJournal: "/x/notes.txt", OriginLine: 4},
		"unsafe agent":     {ID: 4, Title: "t", Brief: "b", OriginJournal: "/x/../" + placeTranscript, OriginLine: 4},
	} {
		if a := nodeFact(sourceOwner, rec).Node.Instructions.Anchor; a != nil {
			t.Errorf("%s: anchor = %+v", name, a)
		}
	}
	crew := &TaskCrewRecord{Routed: true, TaskPins: map[crewroute.Seat]bool{crewroute.Checker: true}}
	crew.Decision.Crew = []crewroute.Pick{
		{Seat: crewroute.Worker, Model: "a/worker", Price: &crewroute.RoutePrice{Prompt: 1e-6, Completion: 4e-6, CacheRead: 1e-7}},
		{Seat: crewroute.Checker, Model: "a/checker"},
	}
	run := runFact(sourceOwner, runRecord{ID: 5, Title: "r", State: TaskDone, CrewState: crew}, nil)
	if len(run.Node.Route) != 2 || run.Node.Route[0].Model != "a/worker" || run.Node.Route[0].Pinned || !run.Node.Route[1].Pinned {
		t.Fatalf("route = %+v", run.Node.Route)
	}
	// The price is the accepted route's own tariff; a pick that stored none is unknown, not free.
	if p := run.Node.Route[0].Price; p == nil || p.Prompt != 1e-6 || p.Completion != 4e-6 || p.CacheRead != 1e-7 {
		t.Fatalf("worker price = %+v", p)
	}
	if run.Node.Route[1].Price != nil {
		t.Fatalf("an unpriced pick carries a price: %+v", run.Node.Route[1].Price)
	}
	if got := runFact(sourceOwner, runRecord{ID: 6, Title: "r", State: TaskDone}, nil); got.Node.Route != nil {
		t.Fatalf("a row with no crew has a route: %+v", got.Node.Route)
	}
}
