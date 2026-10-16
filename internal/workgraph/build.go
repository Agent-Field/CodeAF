package workgraph

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"
)

// ErrNotFound is a request about a task, crew or agent the snapshot does not
// hold; ErrBadRequest is one that cannot be answered as asked. The app door
// maps them to not_found and bad_params.
var (
	ErrNotFound   = errors.New("workgraph: not found")
	ErrBadRequest = errors.New("workgraph: bad request")
)

func badf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrBadRequest, fmt.Sprintf(format, args...))
}

// tickEvery is how many items a loop handles between looks at its context.
const tickEvery = 1024

// Snapshot is the whole of one scope's facts, built once from one [Input] and
// never changed afterwards. Every answer is a copy out of it, so two callers —
// or one caller twice — never share a slice, and a cache may hold it freely.
type Snapshot struct {
	scope    GraphScope
	about    ScopeFacts
	revision string
	asOf     *time.Time

	nodes []GraphNode
	keys  []orderKey
	idx   map[string]int32

	// edgeStart is a CSR index: node i's edges are edges[edgeStart[i]:edgeStart[i+1]].
	edgeStart []int32
	edges     []GraphEdge
	edgeTo    []int32
	// revStart and revList are the same index backwards: the nodes that wait on i.
	revStart []int32
	revList  []int32

	clusters []GraphCluster
	members  map[string][]int32
	crews    []CrewFact
	people   []MemberFact

	metrics GraphMetrics
	// bytes is the conservative size the cache budgets this snapshot at.
	bytes    int64
	totals   GraphTotals
	coverage Coverage
	diag     Diagnostics
}

// Revision is the revision the snapshot was built for.
func (s *Snapshot) Revision() string { return s.revision }

// Len is how many nodes the snapshot holds.
func (s *Snapshot) Len() int { return len(s.nodes) }

// builder carries one build's working state.
type builder struct {
	ctx   context.Context
	in    Input
	snap  *Snapshot
	all   []Fact
	allIx map[string]int
	// kept are the indexes into all that are in scope, in order.
	kept   []int
	owners map[string]bool
	diag   Diagnostics
}

func (b *builder) tick(i int) error {
	if i%tickEvery == tickEvery-1 {
		return b.ctx.Err()
	}
	return nil
}

// Build makes one snapshot, in O(N+E) over the facts, looking at ctx every
// [tickEvery] items. It reports a cancelled context and a task scope whose
// root is not among the facts; it never reports a fact it finds odd — odd facts
// are classified and counted in the snapshot's diagnostics.
func Build(ctx context.Context, in Input) (*Snapshot, error) {
	b := &builder{ctx: ctx, in: in, snap: &Snapshot{scope: in.Scope, about: in.About, revision: in.Revision, asOf: stamp(in.AsOf)}}
	steps := []func() error{b.prepare, b.selectScope, b.makeNodes, b.makeEdges, b.findCycles, b.placeCrews, b.makeClusters, b.makePeople, b.measure, b.finish}
	for _, step := range steps {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := step(); err != nil {
			return nil, err
		}
	}
	return b.snap, nil
}

// prepare orders the facts and indexes them, dropping a task listed twice.
func (b *builder) prepare() error {
	facts := make([]Fact, 0, len(b.in.Facts))
	for i, f := range b.in.Facts {
		if err := b.tick(i); err != nil {
			return err
		}
		if f.Node.Ref.Valid() {
			facts = append(facts, f)
		}
	}
	sortFacts(facts)
	b.allIx = make(map[string]int, len(facts))
	b.owners = map[string]bool{}
	for _, f := range facts {
		id := f.Node.Ref.ID()
		if _, dup := b.allIx[id]; dup {
			continue
		}
		b.allIx[id] = len(b.all)
		b.all = append(b.all, f)
		b.owners[f.Node.Ref.Owner] = true
	}
	return nil
}

// selectScope keeps the scope's tasks: all of them, or for a task scope the
// root's sub-tasks at every depth, reached through the family links with a
// visited set so a family that loops cannot run away.
func (b *builder) selectScope() error {
	loops := b.parentLoops()
	b.diag.ParentCycles = loops
	if b.in.Scope.Kind != ScopeTask {
		b.kept = make([]int, len(b.all))
		for i := range b.all {
			b.kept[i] = i
		}
		return nil
	}
	root, ok := b.allIx[b.in.Root]
	if !ok {
		return fmt.Errorf("%w: task %s", ErrNotFound, b.in.Root)
	}
	return b.descend(root)
}

// parentOf is the index of a fact's parent among all facts, or -1.
func (b *builder) parentOf(i int) int {
	p := b.all[i].Node.Parent
	if p == "" {
		return -1
	}
	if j, ok := b.allIx[p]; ok {
		return j
	}
	return -1
}

// parentLoops counts the tasks on a family loop. Every task has at most one
// parent, so the family is a functional graph and one coloured walk finds every
// loop in O(N).
func (b *builder) parentLoops() int {
	color := make([]uint8, len(b.all))
	loops := 0
	for i := range b.all {
		if color[i] != 0 {
			continue
		}
		var path []int
		cur := i
		for cur != -1 && color[cur] == 0 {
			color[cur] = 1
			path = append(path, cur)
			cur = b.parentOf(cur)
		}
		if cur != -1 && color[cur] == 1 {
			loops += loopLength(path, cur)
		}
		for _, p := range path {
			color[p] = 2
		}
	}
	return loops
}

// loopLength is how many tasks of path lie on the loop that closes at cur.
func loopLength(path []int, cur int) int {
	for at, p := range path {
		if p == cur {
			return len(path) - at
		}
	}
	return 0
}

// descend keeps root's descendants in creation order.
func (b *builder) descend(root int) error {
	children := make(map[int][]int)
	for i := range b.all {
		if err := b.tick(i); err != nil {
			return err
		}
		if p := b.parentOf(i); p >= 0 {
			children[p] = append(children[p], i)
		}
	}
	seen := map[int]bool{root: true}
	queue := []int{root}
	var keep []int
	for head := 0; head < len(queue); head++ {
		if err := b.tick(head); err != nil {
			return err
		}
		for _, c := range children[queue[head]] {
			if !seen[c] {
				seen[c] = true
				queue = append(queue, c)
				keep = append(keep, c)
			}
		}
	}
	sort.Ints(keep)
	b.kept = keep
	return b.rootFacts(root)
}

// rootFacts puts the task scope's own task in About.Root.
func (b *builder) rootFacts(root int) error {
	n := b.all[root].Node
	n.ParentState = b.parentState(root)
	n.WaitsOnTotal = len(dedupeWaits(b.all[root].Waits))
	id := n.ID
	for i, f := range b.all {
		if err := b.tick(i); err != nil {
			return err
		}
		for _, w := range dedupeWaits(f.Waits) {
			if w.On.ID() == id {
				n.WaitedByTotal++
			}
		}
	}
	n.Crew = nil
	b.snap.about.Root = cloneNode(&n)
	return nil
}

// parentState classifies a fact's recorded parent against the facts.
func (b *builder) parentState(i int) string {
	switch {
	case b.all[i].Node.Parent == "":
		return "root"
	case b.parentOf(i) >= 0:
		return "node"
	}
	return "missing"
}

// dedupeWaits drops a link recorded twice to one task, keeping the first.
func dedupeWaits(in []Wait) []Wait {
	if len(in) < 2 {
		return in
	}
	seen := make(map[string]bool, len(in))
	out := make([]Wait, 0, len(in))
	for _, w := range in {
		id := w.On.ID()
		if !w.On.Valid() || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, w)
	}
	return out
}

// makeNodes copies the kept facts into the snapshot's own nodes.
func (b *builder) makeNodes() error {
	s := b.snap
	s.nodes = make([]GraphNode, len(b.kept))
	s.keys = make([]orderKey, len(b.kept))
	s.idx = make(map[string]int32, len(b.kept))
	for at, i := range b.kept {
		if err := b.tick(at); err != nil {
			return err
		}
		n := *cloneNode(&b.all[i].Node)
		n.ParentState = b.parentState(i)
		if n.ParentState == "missing" {
			b.diag.MissingParents++
		}
		if n.Links == "" {
			n.Links = "recorded"
		}
		s.nodes[at], s.keys[at] = n, keyOf(&n)
		s.idx[n.ID] = int32(at)
	}
	return nil
}

// target classifies one link's target against the scope and the facts.
func (b *builder) target(w Wait) (kind, state string, node int32) {
	id := w.On.ID()
	if j, ok := b.snap.idx[id]; ok {
		return TargetNode, b.snap.nodes[j].State, j
	}
	if k, ok := b.allIx[id]; ok {
		return TargetExternal, b.all[k].Node.State, -1
	}
	if b.owners[w.On.Owner] {
		return TargetMissing, "", -1
	}
	if b.in.Resolve == nil {
		return TargetExternal, "", -1
	}
	res := b.in.Resolve(w.On)
	switch res.Standing {
	case StandingFound:
		return TargetExternal, res.State, -1
	case StandingAbsent:
		return TargetMissing, "", -1
	}
	return TargetExternal, "", -1
}

// makeEdges turns every stored link of a kept task into an edge, keeping the
// whole list (the page cuts it) and the in/out degrees.
func (b *builder) makeEdges() error {
	s := b.snap
	s.edgeStart = make([]int32, len(s.nodes)+1)
	waitedBy := make([]int32, len(s.nodes))
	for at, i := range b.kept {
		if err := b.tick(at); err != nil {
			return err
		}
		s.edgeStart[at] = int32(len(s.edges))
		for _, w := range dedupeWaits(b.all[i].Waits) {
			e, to := b.edge(s.nodes[at].ID, w)
			s.edges = append(s.edges, e)
			s.edgeTo = append(s.edgeTo, to)
			if to >= 0 {
				waitedBy[to]++
			}
		}
		s.nodes[at].WaitsOnTotal = len(s.edges) - int(s.edgeStart[at])
		s.nodes[at].WaitsOnCut = s.nodes[at].WaitsOnTotal > MaxEdgesPerNode
	}
	s.edgeStart[len(s.nodes)] = int32(len(s.edges))
	for at := range s.nodes {
		s.nodes[at].WaitedByTotal = int(waitedBy[at])
	}
	b.reverse(waitedBy)
	return nil
}

func (b *builder) edge(from string, w Wait) (GraphEdge, int32) {
	kind, state, to := b.target(w)
	switch kind {
	case TargetMissing:
		b.diag.MissingTargets++
	case TargetExternal:
		b.diag.ExternalTargets++
	}
	via := w.Via
	if via == "" {
		via = ViaRecord
	}
	return GraphEdge{From: from, To: w.On.ID(), ToRef: w.On, Kind: KindWaitsOn, Via: via, Target: kind, ToState: state}, to
}

// reverse builds the dependents index from the edges.
func (b *builder) reverse(waitedBy []int32) {
	s := b.snap
	s.revStart = make([]int32, len(s.nodes)+1)
	for i := range s.nodes {
		s.revStart[i+1] = s.revStart[i] + waitedBy[i]
	}
	s.revList = make([]int32, s.revStart[len(s.nodes)])
	fill := make([]int32, len(s.nodes))
	for from := range s.nodes {
		for e := s.edgeStart[from]; e < s.edgeStart[from+1]; e++ {
			if to := s.edgeTo[e]; to >= 0 {
				s.revList[s.revStart[to]+fill[to]] = int32(from)
				fill[to]++
			}
		}
	}
}

func (b *builder) finish() error {
	s := b.snap
	s.diag = b.diag
	s.totals.Nodes = len(s.nodes)
	s.totals.Edges = len(s.edges)
	s.totals.Clusters = len(s.clusters)
	s.totals.Crews = len(s.crews)
	s.totals.ByState = map[string]int{}
	for i := range s.nodes {
		s.totals.ByState[s.nodes[i].State]++
		if s.nodes[i].Dismissed {
			s.totals.Dismissed++
		}
	}
	s.coverage = b.coverage()
	s.bytes = s.estimateBytes()
	return nil
}

func (b *builder) coverage() Coverage {
	s := b.snap
	c := Coverage{
		Complete:    len(b.in.Unread) == 0 && !b.in.Truncated,
		Truncated:   b.in.Truncated,
		Owners:      b.in.Owners,
		Unread:      append([]OwnerIssue(nil), b.in.Unread...),
		Excluded:    append([]string(nil), b.in.Excluded...),
		Unavailable: append([]string(nil), b.in.Unavailable...),
	}
	if len(c.Unread) > 16 {
		c.Unread = c.Unread[:16]
	}
	for i := range s.nodes {
		n := &s.nodes[i]
		if n.Links == "unrecorded" {
			c.Links.Unrecorded++
		} else {
			c.Links.Recorded++
		}
		c.Clock = addClock(c.Clock, n.Clock)
	}
	return c
}

func addClock(c ClockCoverage, k Clock) ClockCoverage {
	if k.AdmittedAt != nil {
		c.Admitted++
	}
	if k.StartedAt != nil {
		c.Started++
	}
	if k.StateAt != nil {
		c.State++
	}
	if k.EndedAt != nil {
		c.Ended++
	}
	return c
}

// cloneNode is a deep copy of a node, so no answer aliases the snapshot.
func cloneNode(n *GraphNode) *GraphNode {
	c := *n
	if n.Group != nil {
		g := *n.Group
		c.Group = &g
	}
	if n.Crew != nil {
		p := *n.Crew
		p.Path = append([]string(nil), n.Crew.Path...)
		p.AlsoIn = append([]string(nil), n.Crew.AlsoIn...)
		c.Crew = &p
	}
	c.Clock = Clock{AdmittedAt: copyTime(n.Clock.AdmittedAt), StartedAt: copyTime(n.Clock.StartedAt), StateAt: copyTime(n.Clock.StateAt), EndedAt: copyTime(n.Clock.EndedAt)}
	if n.Instructions != nil {
		t := *n.Instructions
		if n.Instructions.Anchor != nil {
			a := *n.Instructions.Anchor
			t.Anchor = &a
		}
		c.Instructions = &t
	}
	if n.ElapsedMS != nil {
		ms := *n.ElapsedMS
		c.ElapsedMS = &ms
	}
	if n.Source != nil {
		src := *n.Source
		src.At = copyTime(n.Source.At)
		c.Source = &src
	}
	c.Route = append([]RouteSeat(nil), n.Route...)
	for i := range c.Route {
		if p := n.Route[i].Price; p != nil {
			price := *p
			c.Route[i].Price = &price
		}
	}
	if n.Landing != nil {
		l := *n.Landing
		l.At = copyTime(n.Landing.At)
		c.Landing = &l
	}
	c.Files.Paths = append([]string(nil), n.Files.Paths...)
	if n.Spend != nil {
		sp := *n.Spend
		c.Spend = &sp
	}
	if n.Checks != nil {
		ck := *n.Checks
		c.Checks = &ck
	}
	return &c
}
