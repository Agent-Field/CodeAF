package workgraph

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

var epoch = time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)

// at is epoch plus n minutes.
func minute(n int) *time.Time {
	t := epoch.Add(time.Duration(n) * time.Minute)
	return &t
}

type opt func(*Fact)

func parent(id string) opt  { return func(f *Fact) { f.Node.Parent = id } }
func state(s string) opt    { return func(f *Fact) { f.Node.State = s } }
func kind(k string) opt     { return func(f *Fact) { f.Node.Kind = k } }
func admitted(m int) opt    { return func(f *Fact) { f.Node.Clock.AdmittedAt = minute(m) } }
func unrecorded() opt       { return func(f *Fact) { f.Node.Links = "unrecorded" } }
func spend(t TaskSpend) opt { return func(f *Fact) { f.Node.Spend = &t } }
func landing(o string) opt  { return func(f *Fact) { f.Node.Landing = &Landing{Outcome: o} } }
func checks(p, x int) opt   { return func(f *Fact) { f.Node.Checks = &TaskChecks{Passed: p, Failed: x} } }
func files(paths ...string) opt {
	return func(f *Fact) { f.Node.Files = FileSet{Paths: paths, Total: len(paths), Source: "changed"} }
}
func waits(owner, task string) opt {
	return func(f *Fact) { f.Waits = append(f.Waits, Wait{On: TaskRef{Owner: owner, Task: task}, Via: ViaRecord}) }
}
func waitsExt(owner, task string) opt {
	return func(f *Fact) {
		f.Waits = append(f.Waits, Wait{On: TaskRef{Owner: owner, Task: task}, Via: ViaExternal})
	}
}

// task is one fact: owner "a" task number n, queued unless an option says so.
func task(owner string, n int, opts ...opt) Fact {
	ref := TaskRef{Owner: owner, Task: fmt.Sprint(n)}
	f := Fact{Node: GraphNode{ID: ref.ID(), Ref: ref, Seq: uint64(n), Kind: "task", Title: "task " + ref.Task, State: StateQueued, Links: "recorded"}}
	f.Node.Clock.AdmittedAt = minute(n)
	for _, o := range opts {
		o(&f)
	}
	return f
}

func input(scope GraphScope, facts ...Fact) Input {
	return Input{Scope: scope, Facts: facts, Revision: "r1", Owners: 1}
}

var agentA = GraphScope{Kind: ScopeAgent, ID: "a"}

func mustBuild(t *testing.T, in Input) *Snapshot {
	t.Helper()
	s, err := Build(context.Background(), in)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return s
}

func answer(t *testing.T, s *Snapshot, req GraphRequest) *GraphResult {
	t.Helper()
	res, err := s.Answer(context.Background(), req)
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	return res
}

func edgeSet(res *GraphResult) map[string]GraphEdge {
	out := map[string]GraphEdge{}
	for _, e := range res.Edges {
		out[e.From+">"+e.To] = e
	}
	return out
}

func TestEdgesRunDependentToPrerequisiteAndAreNeverInferred(t *testing.T) {
	// Three tasks in a row with titles that read like a chain: ONLY the stored
	// link makes an edge. Order, titles and a shared parent make none.
	in := input(agentA,
		task("a", 1, func(f *Fact) { f.Node.Title = "step one" }),
		task("a", 2, func(f *Fact) { f.Node.Title = "step two after step one" }, waits("a", "1")),
		task("a", 3, func(f *Fact) { f.Node.Title = "step three after step two" }),
	)
	res := answer(t, mustBuild(t, in), GraphRequest{Scope: agentA})
	edges := edgeSet(res)
	if len(edges) != 1 {
		t.Fatalf("edges = %+v, want exactly the stored one", res.Edges)
	}
	e, ok := edges["a:2>a:1"]
	if !ok || e.Kind != KindWaitsOn || e.Via != ViaRecord || e.Target != TargetNode {
		t.Fatalf("edge = %+v", e)
	}
	if res.Nodes[1].WaitsOnTotal != 1 || res.Nodes[0].WaitedByTotal != 1 || res.Nodes[2].WaitsOnTotal != 0 {
		t.Fatalf("degrees = %+v", res.Nodes)
	}
}

func TestDependencyDepthAndFocusWalkNearestFirst(t *testing.T) {
	var facts []Fact
	facts = append(facts, task("a", 1))
	for n := 2; n <= 8; n++ {
		facts = append(facts, task("a", n, waits("a", fmt.Sprint(n-1))))
	}
	s := mustBuild(t, input(agentA, facts...))
	res := answer(t, s, GraphRequest{Scope: agentA, Focus: &FocusRef{Task: "a:8", Direction: FocusPrerequisites, Depth: 3}})
	var got []string
	for _, n := range res.Nodes {
		got = append(got, n.ID)
	}
	if strings.Join(got, ",") != "a:8,a:7,a:6,a:5" || !res.Cut {
		t.Fatalf("focus = %v cut=%v", got, res.Cut)
	}
	res = answer(t, s, GraphRequest{Scope: agentA, Focus: &FocusRef{Task: "a:1", Direction: FocusDependents, Depth: 6}})
	// Six steps from the first task reach the seventh and stop short of the eighth.
	if len(res.Nodes) != 7 || !res.Cut {
		t.Fatalf("dependents = %d cut=%v", len(res.Nodes), res.Cut)
	}
	res = answer(t, s, GraphRequest{Scope: agentA, Focus: &FocusRef{Task: "a:4", Direction: FocusBoth, Depth: 6}})
	if len(res.Nodes) != 8 || res.Cut {
		t.Fatalf("both = %d cut=%v", len(res.Nodes), res.Cut)
	}
}

func TestCyclesAreReportedAndLeftInPlace(t *testing.T) {
	s := mustBuild(t, input(agentA,
		task("a", 1, waits("a", "3")),
		task("a", 2, waits("a", "1")),
		task("a", 3, waits("a", "2")),
		task("a", 4, waits("a", "4")),
		task("a", 5),
	))
	res := answer(t, s, GraphRequest{Scope: agentA})
	if res.Diagnostics.CyclesTotal != 2 || len(res.Diagnostics.Cycles) != 2 {
		t.Fatalf("diagnostics = %+v", res.Diagnostics)
	}
	for _, n := range res.Nodes {
		want := n.Ref.Task != "5"
		if n.InCycle != want {
			t.Fatalf("%s in_cycle = %v", n.ID, n.InCycle)
		}
	}
	for _, e := range res.Edges {
		if !e.InCycle {
			t.Fatalf("edge %s>%s is on a cycle", e.From, e.To)
		}
	}
}

func TestExternalAndMissingTargetsAreExplicitAndNeverFabricated(t *testing.T) {
	in := input(agentA,
		task("a", 1, waitsExt("b", "7"), waitsExt("b", "9"), waitsExt("c", "1"), waits("a", "99")),
	)
	in.Resolve = func(r TaskRef) Resolution {
		switch r.ID() {
		case "b:7":
			return Resolution{Standing: StandingFound, State: StateRunning}
		case "b:9":
			return Resolution{Standing: StandingAbsent}
		}
		return Resolution{Standing: StandingUnknown}
	}
	res := answer(t, mustBuild(t, in), GraphRequest{Scope: agentA})
	e := edgeSet(res)
	if x := e["a:1>b:7"]; x.Target != TargetExternal || x.ToState != StateRunning || x.Via != ViaExternal {
		t.Fatalf("b:7 = %+v", x)
	}
	if x := e["a:1>b:9"]; x.Target != TargetMissing {
		t.Fatalf("b:9 = %+v", x)
	}
	// An owner nobody could read says nothing about the task: it is external with
	// no state, not "missing".
	if x := e["a:1>c:1"]; x.Target != TargetExternal || x.ToState != "" {
		t.Fatalf("c:1 = %+v", x)
	}
	if x := e["a:1>a:99"]; x.Target != TargetMissing {
		t.Fatalf("a:99 = %+v", x)
	}
	if d := res.Diagnostics; d.MissingTargets != 2 || d.ExternalTargets != 2 {
		t.Fatalf("diagnostics = %+v", d)
	}
	for _, n := range res.Nodes {
		if n.ID == "b:7" || n.ID == "b:9" {
			t.Fatalf("a target outside the scope became a node: %+v", n)
		}
	}
}

func TestSameTaskNumberUnderDifferentOwnersNeverCollides(t *testing.T) {
	scope := GraphScope{Kind: ScopeCrew, ID: "crew"}
	in := input(scope,
		task("a", 1), task("b", 1, waits("a", "1")), task("c", 1, waitsExt("b", "1")),
	)
	in.Owners = 3
	s := mustBuild(t, in)
	res := answer(t, s, GraphRequest{Scope: scope})
	if len(res.Nodes) != 3 {
		t.Fatalf("nodes = %d", len(res.Nodes))
	}
	e := edgeSet(res)
	if e["b:1>a:1"].Target != TargetNode || e["c:1>b:1"].Target != TargetNode {
		t.Fatalf("edges = %+v", res.Edges)
	}
}

func TestADuplicateFactIsDroppedAndAnInvalidRefIsNeverAdopted(t *testing.T) {
	bad := task("a", 3)
	bad.Node.Ref.Owner = "../etc"
	res := answer(t, mustBuild(t, input(agentA, task("a", 1), task("a", 1), bad)), GraphRequest{Scope: agentA})
	if len(res.Nodes) != 1 {
		t.Fatalf("nodes = %+v", res.Nodes)
	}
}

func TestNestedFamilyClosedMissingAndLoopingParentsAreClassified(t *testing.T) {
	task1 := task("a", 1)
	in := input(agentA,
		task1,
		task("a", 2, parent("a:1")),
		task("a", 3, parent("a:2")),
		task("a", 4, parent("a:77")), // a parent nobody holds
		task("a", 5, parent("a:6")),  // a family loop
		task("a", 6, parent("a:5")),
	)
	res := answer(t, mustBuild(t, in), GraphRequest{Scope: agentA})
	by := map[string]GraphNode{}
	for _, n := range res.Nodes {
		by[n.ID] = n
	}
	if by["a:1"].ParentState != "root" || by["a:2"].ParentState != "node" || by["a:4"].ParentState != "missing" {
		t.Fatalf("parent states = %+v", by)
	}
	if d := res.Diagnostics; d.MissingParents != 1 || d.ParentCycles != 2 {
		t.Fatalf("diagnostics = %+v", d)
	}
	// A task scope keeps the root's subtree at every depth and nothing else, and
	// does not run away on the loop.
	scope := GraphScope{Kind: ScopeTask, ID: "a:1"}
	in.Scope, in.Root = scope, "a:1"
	got := answer(t, mustBuild(t, in), GraphRequest{Scope: scope})
	// The root is what the scope is about (About.Root); the page holds its
	// sub-tasks at every depth and nothing else.
	if len(got.Nodes) != 2 || got.About.Root == nil || got.About.Root.ID != "a:1" {
		t.Fatalf("task scope nodes = %d root=%v", len(got.Nodes), got.About.Root)
	}
	in.Scope, in.Root = GraphScope{Kind: ScopeTask, ID: "a:5"}, "a:5"
	if got := answer(t, mustBuild(t, in), GraphRequest{Scope: in.Scope}); len(got.Nodes) != 1 {
		t.Fatalf("looping family nodes = %d", len(got.Nodes))
	}
	in.Scope, in.Root = GraphScope{Kind: ScopeTask, ID: "a:50"}, "a:50"
	if _, err := Build(context.Background(), in); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing root err = %v", err)
	}
}

func TestSubcrewOwnershipAncestryAndClusterExpansion(t *testing.T) {
	scope := GraphScope{Kind: ScopeCrew, ID: "top"}
	in := input(scope, task("a", 1), task("b", 1), task("c", 1), task("d", 1))
	in.Owners = 4
	in.Crews = []CrewInput{
		{CrewFact{ID: "top", Name: "Top"}},
		{CrewFact{ID: "mid", Name: "Mid", Parent: "top"}},
		{CrewFact{ID: "leaf", Name: "Leaf", Parent: "mid"}},
		{CrewFact{ID: "loop1", Name: "Loop", Parent: "loop2"}},
		{CrewFact{ID: "loop2", Name: "Loop", Parent: "loop1"}},
	}
	in.Members = []MemberInput{
		{MemberFact: MemberFact{Agent: "a", Crew: "top", Manager: true}, Home: true},
		{MemberFact: MemberFact{Agent: "b", Crew: "mid"}},
		{MemberFact: MemberFact{Agent: "c", Crew: "leaf"}},
		{MemberFact: MemberFact{Agent: "c", Crew: "mid"}},
		{MemberFact: MemberFact{Agent: "d", Crew: "loop1"}},
	}
	s := mustBuild(t, in)
	res := answer(t, s, GraphRequest{Scope: scope})
	by := map[string]GraphNode{}
	for _, n := range res.Nodes {
		by[n.Ref.Owner] = n
	}
	if p := by["c"].Crew; p == nil || p.Crew != "leaf" || strings.Join(p.Path, ">") != "top>mid>leaf" || len(p.AlsoIn) != 1 || p.AlsoIn[0] != "mid" {
		t.Fatalf("c place = %+v", by["c"].Crew)
	}
	if p := by["d"].Crew; p == nil || strings.Join(p.Path, ">") != "loop2>loop1" {
		t.Fatalf("looping ancestry must be cut where it repeats: %+v", p)
	}
	// Expanding the mid crew yields its members and every crew under it.
	got := answer(t, s, GraphRequest{Scope: scope, Cluster: &ClusterRef{Kind: ClusterCrew, ID: "mid"}})
	var owners []string
	for _, n := range got.Nodes {
		owners = append(owners, n.Ref.Owner)
	}
	if strings.Join(owners, ",") != "b,c" {
		t.Fatalf("mid cluster = %v", owners)
	}
	if _, err := s.Answer(context.Background(), GraphRequest{Scope: scope, Cluster: &ClusterRef{Kind: ClusterCrew, ID: "nope"}}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown cluster err = %v", err)
	}
	people := answer(t, s, GraphRequest{Scope: scope, View: ViewPeople})
	if len(people.Crews) < 3 || len(people.Members) != 5 {
		t.Fatalf("people = %d crews %d members", len(people.Crews), len(people.Members))
	}
}

func TestClosedCrewKeepsItsActualState(t *testing.T) {
	scope := GraphScope{Kind: ScopeCrew, ID: "top"}
	in := input(scope, task("a", 1, state(StateDone)))
	in.Crews = []CrewInput{{CrewFact{ID: "top", Name: "Top", State: "closed", ClosedAt: minute(30)}}}
	in.Members = []MemberInput{{MemberFact: MemberFact{Agent: "a", Crew: "top"}, Home: true}}
	res := answer(t, mustBuild(t, in), GraphRequest{Scope: scope, View: ViewPeople})
	if len(res.Crews) != 1 || res.Crews[0].State != "closed" || res.Crews[0].ClosedAt == nil || !res.Crews[0].ClosedAt.Equal(*minute(30)) {
		t.Fatalf("crews = %+v", res.Crews)
	}
}

func TestOldRecordsWithoutLinksAreUnknownNotEmpty(t *testing.T) {
	in := input(agentA, task("a", 1), task("a", 2, unrecorded()))
	res := answer(t, mustBuild(t, in), GraphRequest{Scope: agentA})
	if res.Coverage.Links.Recorded != 1 || res.Coverage.Links.Unrecorded != 1 {
		t.Fatalf("link coverage = %+v", res.Coverage.Links)
	}
	if res.Nodes[0].Links != "recorded" || res.Nodes[1].Links != "unrecorded" {
		t.Fatalf("links = %q %q", res.Nodes[0].Links, res.Nodes[1].Links)
	}
	// A task with a link-less record that nonetheless is waited on stays
	// honest: the in-edges are real, the out-edges are unknown.
	in = input(agentA, task("a", 1, unrecorded()), task("a", 2, waits("a", "1")))
	res = answer(t, mustBuild(t, in), GraphRequest{Scope: agentA})
	if res.Nodes[0].WaitedByTotal != 1 || res.Nodes[0].Links != "unrecorded" {
		t.Fatalf("node = %+v", res.Nodes[0])
	}
}

func TestClockCoverageCountsOnlyRecordedInstants(t *testing.T) {
	in := input(agentA,
		task("a", 1, func(f *Fact) { f.Node.Clock = Clock{} }),
		task("a", 2, func(f *Fact) { f.Node.Clock.StartedAt = minute(3); f.Node.Clock.StateAt = minute(4) }),
	)
	res := answer(t, mustBuild(t, in), GraphRequest{Scope: agentA})
	if c := res.Coverage.Clock; c.Admitted != 1 || c.Started != 1 || c.State != 1 || c.Ended != 0 {
		t.Fatalf("clock coverage = %+v", c)
	}
	// An unknown admission time sorts first, so creation order stays stable.
	if res.Nodes[0].Ref.Task != "1" {
		t.Fatalf("order = %s", res.Nodes[0].Ref.Task)
	}
}

func TestMetricsKnownZeroFollowsSourceCoverage(t *testing.T) {
	// An empty scope that was read whole has exactly zero tasks and zero
	// landed work: those are KNOWN zeros.
	res := answer(t, mustBuild(t, input(agentA)), GraphRequest{Scope: agentA, View: ViewMetrics})
	m := res.Metrics
	if !m.Tasks.Known || m.Tasks.Value != 0 || m.Tasks.Partial || !m.LandedTasks.Known || !m.ChecksPassed.Known {
		t.Fatalf("empty known scope = %+v", m)
	}
	if m.Spend.Known {
		t.Fatalf("no task carries a bill, so no spend is known: %+v", m.Spend)
	}
	// The same empty scope with an unread owner knows nothing.
	in := input(agentA)
	in.Unread = []OwnerIssue{{Owner: "a", Reason: "unreadable"}}
	m = answer(t, mustBuild(t, in), GraphRequest{Scope: agentA, View: ViewMetrics}).Metrics
	if m.Tasks.Known || !m.Tasks.Partial || m.LandedTasks.Known || m.ChecksPassed.Known || m.Spend.Known {
		t.Fatalf("unread scope claimed knowledge: %+v", m)
	}
	// A truncated scope is a lower bound, not a zero.
	in = input(agentA)
	in.Truncated = true
	if m = answer(t, mustBuild(t, in), GraphRequest{Scope: agentA, View: ViewMetrics}).Metrics; m.Tasks.Known {
		t.Fatalf("truncated empty scope claimed zero: %+v", m.Tasks)
	}
}

func TestMetricsCountOnlyWhatRecordsEstablish(t *testing.T) {
	in := input(agentA,
		task("a", 1, state(StateDone), landing("merged"), checks(2, 1), files("x.go", "y.go"), spend(TaskSpend{USD: 0.5, Basis: "priced"})),
		task("a", 2, state(StateDone), landing("discarded"), files("z.go")),
		task("a", 3, state(StateDone)),
		task("a", 4, state(StateDone), landing("inplace"), checks(1, 0), files("x.go"), spend(TaskSpend{Tokens: 90, Basis: "unpriced"})),
		task("a", 5, state(StateRunning), parent("a:1")),
		task("a", 6, kind(KindQuick), state(StateDone)),
	)
	m := answer(t, mustBuild(t, in), GraphRequest{Scope: agentA, View: ViewMetrics}).Metrics
	// A quick lookup is not a task and does not enter the landing or check counts.
	figure(t, "tasks", m.Tasks, 5, 5, 5, true, false)
	figure(t, "subtasks", m.Subtasks, 1, 1, 1, true, false)
	// Of four done tasks, three carry a landing outcome; two of those landed.
	figure(t, "landed", m.LandedTasks, 2, 3, 4, true, true)
	figure(t, "landed files", m.LandedFiles, 2, 2, 2, true, false)
	figure(t, "checks", m.ChecksPassed, 3, 2, 4, true, true)
	// Only the priced bill adds to spend; the unpriced one is counted apart and
	// is never a zero.
	figure(t, "spend", m.Spend, 0.5, 1, 6, true, true)
	if m.Unpriced != 1 {
		t.Fatalf("unpriced = %d", m.Unpriced)
	}
	if m.From == nil || m.To != nil && m.To.Before(*m.From) {
		t.Fatalf("span = %v..%v", m.From, m.To)
	}
}

func figure(t *testing.T, name string, f GraphFigure, value float64, counted, of int, known, partial bool) {
	t.Helper()
	if f.Value != value || f.Counted != counted || f.Of != of || f.Known != known || f.Partial != partial {
		t.Errorf("%s = %+v, want value %v counted %d of %d known %v partial %v", name, f, value, counted, of, known, partial)
	}
}

func TestAFreeZeroBillIsNotKnownPricedUnlessTheSourceSaysSo(t *testing.T) {
	// A producer that cannot establish the price marks the bill unpriced, and a
	// recorded 0.00 under that basis stays unknown rather than becoming a free
	// $0.00.
	in := input(agentA, task("a", 1, state(StateDone), spend(TaskSpend{USD: 0, Basis: "unpriced"})))
	m := answer(t, mustBuild(t, in), GraphRequest{Scope: agentA, View: ViewMetrics}).Metrics
	if m.Spend.Known || m.Spend.Value != 0 || m.Unpriced != 1 {
		t.Fatalf("spend = %+v unpriced=%d", m.Spend, m.Unpriced)
	}
	in = input(agentA, task("a", 1, state(StateDone), spend(TaskSpend{USD: 0, Basis: "priced"})))
	if m = answer(t, mustBuild(t, in), GraphRequest{Scope: agentA, View: ViewMetrics}).Metrics; !m.Spend.Known {
		t.Fatalf("a priced zero is a known zero: %+v", m.Spend)
	}
}

func TestPagingIsKeysetBoundedAndSurvivesAppends(t *testing.T) {
	var facts []Fact
	for n := 1; n <= 25; n++ {
		facts = append(facts, task("a", n))
	}
	s := mustBuild(t, input(agentA, facts...))
	req := GraphRequest{Scope: agentA, Limit: 10}
	var seen []string
	var cursor string
	for pages := 0; ; pages++ {
		req.Cursor = cursor
		res := answer(t, s, req)
		if len(res.Nodes) > 10 || res.Totals.Nodes != 25 {
			t.Fatalf("page %d: %d nodes, total %d", pages, len(res.Nodes), res.Totals.Nodes)
		}
		for _, n := range res.Nodes {
			seen = append(seen, n.ID)
		}
		if cursor = res.Next; cursor == "" {
			break
		}
		if pages > 5 {
			t.Fatal("paging does not end")
		}
	}
	if len(seen) != 25 || seen[0] != "a:1" || seen[24] != "a:25" {
		t.Fatalf("seen = %v", seen)
	}
	// The cursor from the first page keeps meaning after tasks are appended: a
	// new snapshot continues from the same position, and the appended tasks are
	// reached later, not skipped or repeated.
	first := answer(t, s, GraphRequest{Scope: agentA, Limit: 10})
	more := append(facts, task("a", 26), task("a", 27))
	s2 := mustBuild(t, Input{Scope: agentA, Facts: more, Revision: "r2", Owners: 1})
	next := answer(t, s2, GraphRequest{Scope: agentA, Limit: 10, Cursor: first.Next})
	if next.Nodes[0].ID != "a:11" {
		t.Fatalf("after an append the next page starts at %s", next.Nodes[0].ID)
	}
}

func TestLimitsAreCappedAndRefused(t *testing.T) {
	var facts []Fact
	for n := 1; n <= MaxNodes+50; n++ {
		facts = append(facts, task("a", n))
	}
	s := mustBuild(t, input(agentA, facts...))
	if res := answer(t, s, GraphRequest{Scope: agentA, Limit: 100000}); len(res.Nodes) != MaxNodes || res.Next == "" {
		t.Fatalf("limit not capped: %d", len(res.Nodes))
	}
	for _, bad := range []GraphRequest{
		{Scope: GraphScope{Kind: "planet", ID: "a"}},
		{Scope: agentA, View: "canvas"},
		{Scope: agentA, Limit: -1},
		{Scope: GraphScope{Kind: ScopeTask, ID: "no-number"}},
		{Scope: agentA, View: ViewPeople, Focus: &FocusRef{Task: "a:1"}},
		{Scope: agentA, IfRevision: strings.Repeat("x", 200)},
	} {
		if _, err := s.Answer(context.Background(), bad); !errors.Is(err, ErrBadRequest) {
			t.Fatalf("%+v err = %v", bad, err)
		}
	}
}

func TestACursorThatWasEditedForeignOrForAnotherRequestIsRefused(t *testing.T) {
	var facts []Fact
	for n := 1; n <= 30; n++ {
		facts = append(facts, task("a", n))
	}
	s := mustBuild(t, input(agentA, facts...))
	page := answer(t, s, GraphRequest{Scope: agentA, Limit: 10})
	tampered := page.Next[:len(page.Next)-2] + "AA"
	other := GraphScope{Kind: ScopeAgent, ID: "z"}
	for name, req := range map[string]GraphRequest{
		"tampered":     {Scope: agentA, Limit: 10, Cursor: tampered},
		"garbage":      {Scope: agentA, Cursor: "not-a-cursor"},
		"other view":   {Scope: agentA, View: ViewClusters, Cursor: page.Next},
		"other filter": {Scope: agentA, Cluster: &ClusterRef{Kind: ClusterGroup, ID: "g"}, Cursor: page.Next},
	} {
		if _, err := s.Answer(context.Background(), req); err == nil {
			t.Fatalf("%s: cursor accepted", name)
		}
	}
	s2 := mustBuild(t, Input{Scope: other, Facts: nil, Revision: "r", Owners: 1})
	if _, err := s2.Answer(context.Background(), GraphRequest{Scope: other, Cursor: page.Next}); !errors.Is(err, ErrBadRequest) {
		t.Fatalf("cursor for another scope: %v", err)
	}
}

func TestIfRevisionAnswersUnchangedWithoutAPage(t *testing.T) {
	s := mustBuild(t, input(agentA, task("a", 1)))
	res := answer(t, s, GraphRequest{Scope: agentA, IfRevision: "r1"})
	if !res.Unchanged || len(res.Nodes) != 0 || res.Revision != "r1" {
		t.Fatalf("res = %+v", res)
	}
	if res = answer(t, s, GraphRequest{Scope: agentA, IfRevision: "stale"}); res.Unchanged || len(res.Nodes) != 1 {
		t.Fatalf("stale revision must answer in full: %+v", res)
	}
}

func TestCancellationStopsBuildAndAnswer(t *testing.T) {
	var facts []Fact
	for n := 1; n <= 5000; n++ {
		facts = append(facts, task("a", n))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Build(ctx, input(agentA, facts...)); !errors.Is(err, context.Canceled) {
		t.Fatalf("Build err = %v", err)
	}
	s := mustBuild(t, input(agentA, facts...))
	if _, err := s.Answer(ctx, GraphRequest{Scope: agentA, Limit: 500}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Answer err = %v", err)
	}
}

func TestAnswersAreCopiesAndNeverAliasTheSnapshot(t *testing.T) {
	in := input(agentA,
		task("a", 1, files("x.go"), spend(TaskSpend{USD: 1, Basis: "priced"}), landing("merged"), checks(1, 0)),
		task("a", 2, waits("a", "1")),
	)
	in.Facts[0].Node.Instructions = &GraphText{Text: "do it"}
	s := mustBuild(t, in)
	first := answer(t, s, GraphRequest{Scope: agentA})
	first.Nodes[0].Title = "mutated"
	first.Nodes[0].Files.Paths[0] = "mutated"
	first.Nodes[0].Spend.USD = 99
	first.Nodes[0].Landing.Outcome = "mutated"
	first.Nodes[0].Instructions.Text = "mutated"
	first.Nodes[0].Clock.AdmittedAt = minute(999)
	first.Edges[0].To = "mutated"
	first.Totals.ByState["queued"] = 99
	// The input the producer still holds is no alias either.
	in.Facts[0].Node.Title = "producer-mutated"
	second := answer(t, s, GraphRequest{Scope: agentA})
	n := second.Nodes[0]
	if n.Title != "task 1" || n.Files.Paths[0] != "x.go" || n.Spend.USD != 1 || n.Landing.Outcome != "merged" || n.Instructions.Text != "do it" || !n.Clock.AdmittedAt.Equal(*minute(1)) {
		t.Fatalf("snapshot was reached through an answer: %+v", n)
	}
	if second.Edges[0].To != "a:1" || second.Totals.ByState["queued"] == 99 {
		t.Fatalf("edges or totals aliased: %+v %+v", second.Edges, second.Totals)
	}
}

func TestBuildIsDeterministicWhateverOrderTheFactsArriveIn(t *testing.T) {
	var facts []Fact
	for n := 1; n <= 40; n++ {
		facts = append(facts, task("a", n, waits("a", fmt.Sprint(max(n-1, 1)))), task("b", n))
	}
	reversed := make([]Fact, len(facts))
	for i, f := range facts {
		reversed[len(facts)-1-i] = f
	}
	scope := GraphScope{Kind: ScopeCrew, ID: "c"}
	a := answer(t, mustBuild(t, input(scope, facts...)), GraphRequest{Scope: scope, Limit: 500})
	b := answer(t, mustBuild(t, input(scope, reversed...)), GraphRequest{Scope: scope, Limit: 500})
	for i := range a.Nodes {
		if a.Nodes[i].ID != b.Nodes[i].ID {
			t.Fatalf("order differs at %d: %s vs %s", i, a.Nodes[i].ID, b.Nodes[i].ID)
		}
	}
}

func TestPlanRowsKeepTheirOwnIdentityAndOrderDeterministically(t *testing.T) {
	plan := func(id string, opts ...opt) Fact {
		f := task("a", 1, opts...)
		f.Node.Ref.Task, f.Node.ID, f.Node.Kind, f.Node.Seq = id, "a:"+id, "plan", 0
		f.Node.Clock.AdmittedAt = minute(5)
		return f
	}
	in := input(agentA, task("a", 1), plan("t-bb", parent("a:1")), plan("t-aa", parent("a:1"), waits("a", "t-bb")))
	res := answer(t, mustBuild(t, in), GraphRequest{Scope: agentA})
	if len(res.Nodes) != 3 || res.Nodes[1].Ref.Task != "t-aa" || res.Nodes[2].Ref.Task != "t-bb" {
		t.Fatalf("nodes = %+v", res.Nodes)
	}
	if e := edgeSet(res)["a:t-aa>a:t-bb"]; e.Target != TargetNode {
		t.Fatalf("plan edge = %+v", e)
	}
	page := answer(t, mustBuild(t, in), GraphRequest{Scope: agentA, Limit: 2})
	rest := answer(t, mustBuild(t, in), GraphRequest{Scope: agentA, Limit: 2, Cursor: page.Next})
	if len(rest.Nodes) != 1 || rest.Nodes[0].Ref.Task != "t-bb" {
		t.Fatalf("second page = %+v", rest.Nodes)
	}
}
