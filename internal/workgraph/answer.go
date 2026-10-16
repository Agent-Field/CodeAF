package workgraph

import (
	"context"
	"fmt"
	"sort"
)

// ValidateRequest refuses a request that cannot be answered as asked, before
// anything is read or built for it.
func ValidateRequest(req GraphRequest) error {
	switch req.Scope.Kind {
	case ScopeAgent, ScopeCrew, ScopeTask:
	default:
		return badf("scope.kind is agent, crew or task")
	}
	if req.Scope.ID == "" || len(req.Scope.ID) > 1024 {
		return badf("scope.id is required")
	}
	if req.Scope.Kind == ScopeTask {
		if _, ok := ParseID(req.Scope.ID); !ok {
			return badf("a task scope's id is \"<agent>:<number>\"")
		}
	}
	switch req.View {
	case "", ViewNodes, ViewClusters, ViewPeople, ViewMetrics:
	default:
		return badf("view is nodes, clusters, people or metrics")
	}
	if req.Limit < 0 {
		return badf("limit cannot be negative")
	}
	if len(req.IfRevision) > 128 || len(req.Cursor) > 1024 {
		return badf("if_revision or cursor is too long")
	}
	return validateShape(req)
}

func validateShape(req GraphRequest) error {
	view := req.View
	if view == "" {
		view = ViewNodes
	}
	if (req.Cluster != nil || req.Focus != nil) && view != ViewNodes {
		return badf("cluster and focus belong to the nodes view")
	}
	if req.Cluster != nil && req.Focus != nil {
		return badf("cluster and focus cannot be asked together")
	}
	if c := req.Cluster; c != nil && (c.ID == "" || c.Kind != ClusterCrew && c.Kind != ClusterGroup) {
		return badf("cluster needs a kind (crew or group) and an id")
	}
	if req.Focus != nil {
		return validateFocus(req.Focus, req.Cursor)
	}
	return nil
}

func validateFocus(f *FocusRef, cursor string) error {
	if _, ok := ParseID(f.Task); !ok || f.Depth < 0 {
		return badf("focus needs a task id and a depth of 0 or more")
	}
	switch f.Direction {
	case "", FocusPrerequisites, FocusDependents, FocusBoth:
	default:
		return badf("focus direction is prerequisites, dependents or both")
	}
	if cursor != "" {
		return badf("a focus has no cursor")
	}
	return nil
}

func scopeKey(s GraphScope) string { return string(s.Kind) + ":" + s.ID }

func filterKey(req GraphRequest) string {
	if req.Cluster != nil {
		return "cluster:" + clusterKey(req.Cluster.Kind, req.Cluster.ID)
	}
	return ""
}

// Answer is the snapshot's reply to one request. It is read-only, bounded and
// looks at ctx while it works; what it returns is a copy.
func (s *Snapshot) Answer(ctx context.Context, req GraphRequest) (*GraphResult, error) {
	if err := ValidateRequest(req); err != nil {
		return nil, err
	}
	if scopeKey(req.Scope) != scopeKey(s.scope) {
		return nil, badf("the request is for a different scope than this snapshot")
	}
	view := req.View
	if view == "" {
		view = ViewNodes
	}
	res := s.base(view)
	if req.IfRevision != "" && req.IfRevision == s.revision {
		return &GraphResult{Contract: Contract, View: view, Revision: s.revision, AsOf: copyTime(s.asOf), Unchanged: true, Scope: s.scope}, nil
	}
	var err error
	switch view {
	case ViewNodes:
		err = s.answerNodes(ctx, res, req)
	case ViewClusters:
		err = s.answerClusters(res, req)
	case ViewPeople:
		s.answerPeople(res)
	default:
		m := cloneMetrics(s.metrics)
		res.Metrics = &m
	}
	if err != nil {
		return nil, err
	}
	return res, nil
}

// base is the part of every answer that does not depend on the page.
func (s *Snapshot) base(view string) *GraphResult {
	about := s.about
	if about.Root != nil {
		about.Root = cloneNode(about.Root)
	}
	if about.Brief != nil {
		b := *about.Brief
		about.Brief = &b
	}
	about.ClosedAt = copyTime(about.ClosedAt)
	totals := s.totals
	totals.ByState = copyCounts(s.totals.ByState)
	cov := s.coverage
	cov.Unread = append([]OwnerIssue(nil), s.coverage.Unread...)
	cov.Excluded = append([]string(nil), s.coverage.Excluded...)
	cov.Unavailable = append([]string(nil), s.coverage.Unavailable...)
	return &GraphResult{
		Contract: Contract, View: view, Revision: s.revision, AsOf: copyTime(s.asOf), Scope: s.scope,
		About: about, Totals: totals, Coverage: cov, Diagnostics: cloneDiagnostics(s.diag),
	}
}

func cloneDiagnostics(d Diagnostics) Diagnostics {
	out := d
	out.Cycles = make([]Cycle, len(d.Cycles))
	for i, c := range d.Cycles {
		out.Cycles[i] = Cycle{Total: c.Total, Tasks: append([]string(nil), c.Tasks...)}
	}
	if len(out.Cycles) == 0 {
		out.Cycles = nil
	}
	return out
}

func cloneMetrics(m GraphMetrics) GraphMetrics {
	out := m
	if m.Members != nil {
		f := *m.Members
		out.Members = &f
	}
	out.From, out.To = copyTime(m.From), copyTime(m.To)
	return out
}

func (s *Snapshot) answerNodes(ctx context.Context, res *GraphResult, req GraphRequest) error {
	limit := req.Limit
	if limit == 0 {
		limit = DefaultNodes
	}
	limit = min(limit, MaxNodes)
	if req.Focus != nil {
		return s.answerFocus(ctx, res, req, limit)
	}
	var list []int32
	if c := req.Cluster; c != nil {
		if !s.hasCluster(c.Kind, c.ID) {
			return fmt.Errorf("%w: %s cluster %s", ErrNotFound, c.Kind, c.ID)
		}
		list = s.members[clusterKey(c.Kind, c.ID)]
		if list == nil {
			list = []int32{}
		}
	}
	want := cursorBody{Scope: scopeKey(s.scope), View: ViewNodes, Filter: filterKey(req)}
	start, err := s.startAt(req.Cursor, want, list)
	if err != nil {
		return err
	}
	s.fill(ctx, res, list, start, limit, want)
	return ctx.Err()
}

func (s *Snapshot) hasCluster(kind, id string) bool {
	i := sort.Search(len(s.clusters), func(i int) bool {
		c := s.clusters[i]
		return c.Kind > kind || c.Kind == kind && c.ID >= id
	})
	return i < len(s.clusters) && s.clusters[i].Kind == kind && s.clusters[i].ID == id
}

// at is the node at a position of a list, where a nil list is every node.
func at(list []int32, pos int) int {
	if list == nil {
		return pos
	}
	return int(list[pos])
}

func size(s *Snapshot, list []int32) int {
	if list == nil {
		return len(s.nodes)
	}
	return len(list)
}

// startAt is the position a cursor names within a list: the first node after
// the one the cursor ended on, found by binary search over creation keys.
func (s *Snapshot) startAt(raw string, want cursorBody, list []int32) (int, error) {
	if raw == "" {
		return 0, nil
	}
	got, err := decodeCursor(raw, want)
	if err != nil {
		return 0, err
	}
	after := orderKey{at: got.At, owner: got.Owner, seq: got.Seq, task: got.Task}
	n := size(s, list)
	return sort.Search(n, func(i int) bool { return after.less(s.keys[at(list, i)]) }), nil
}

// fill puts nodes from a position onward on a page, with the edges of each, and
// stops at the node limit or before the page would pass [MaxEdges] — always
// taking at least one node, so a page can never be empty with more to come.
func (s *Snapshot) fill(ctx context.Context, res *GraphResult, list []int32, start, limit int, want cursorBody) {
	n := size(s, list)
	pos := start
	for ; pos < n && len(res.Nodes) < limit; pos++ {
		if ctx.Err() != nil {
			return
		}
		i := at(list, pos)
		cut := min(s.nodes[i].WaitsOnTotal, MaxEdgesPerNode)
		if len(res.Edges)+cut > MaxEdges && len(res.Nodes) > 0 {
			break
		}
		s.take(res, i)
	}
	res.Returned = len(res.Nodes)
	if pos < n && len(res.Nodes) > 0 {
		k := s.keys[at(list, pos-1)]
		want.At, want.Owner, want.Seq, want.Task = k.at, k.owner, k.seq, k.task
		res.Next = encodeCursor(want)
	}
}

// take copies node i and its first [MaxEdgesPerNode] edges onto the page.
func (s *Snapshot) take(res *GraphResult, i int) {
	res.Nodes = append(res.Nodes, *cloneNode(&s.nodes[i]))
	lo, hi := s.edgeStart[i], s.edgeStart[i+1]
	if hi-lo > MaxEdgesPerNode {
		hi = lo + MaxEdgesPerNode
	}
	res.Edges = append(res.Edges, s.edges[lo:hi]...)
}

// answerFocus walks outward from one task along the waits-on links, nearest
// first, within a depth and a node bound, and answers what it met.
func (s *Snapshot) answerFocus(ctx context.Context, res *GraphResult, req GraphRequest, limit int) error {
	root, ok := s.idx[req.Focus.Task]
	if !ok {
		return fmt.Errorf("%w: task %s", ErrNotFound, req.Focus.Task)
	}
	depth := req.Focus.Depth
	if depth == 0 {
		depth = 3
	}
	depth = min(depth, MaxFocusDepth)
	limit = min(limit, MaxFocusNodes)
	order, cut, err := s.walk(ctx, root, req.Focus.Direction, depth, limit)
	if err != nil {
		return err
	}
	for _, i := range order {
		s.take(res, int(i))
		if len(res.Edges) > MaxEdges {
			res.Edges, cut = res.Edges[:MaxEdges], true
		}
	}
	res.Returned, res.Cut = len(res.Nodes), cut
	return nil
}

// walk is a breadth-first search over the waits-on links in the asked
// direction, bounded by depth and node count. cut says it stopped with more to
// find.
func (s *Snapshot) walk(ctx context.Context, root int32, direction string, depth, limit int) ([]int32, bool, error) {
	seen := map[int32]bool{root: true}
	order := []int32{root}
	level := []int32{root}
	for d := 0; d < depth && len(level) > 0; d++ {
		var next []int32
		for _, v := range level {
			if err := ctx.Err(); err != nil {
				return nil, false, err
			}
			for _, w := range s.neighbours(v, direction) {
				if seen[w] {
					continue
				}
				if len(order) >= limit {
					return order, true, nil
				}
				seen[w] = true
				order = append(order, w)
				next = append(next, w)
			}
		}
		level = next
	}
	return order, len(level) > 0, nil
}

func (s *Snapshot) neighbours(v int32, direction string) []int32 {
	var out []int32
	if direction != FocusDependents {
		for e := s.edgeStart[v]; e < s.edgeStart[v+1]; e++ {
			if to := s.edgeTo[e]; to >= 0 {
				out = append(out, to)
			}
		}
	}
	if direction == FocusDependents || direction == FocusBoth {
		out = append(out, s.revList[s.revStart[v]:s.revStart[v+1]]...)
	}
	return out
}

func (s *Snapshot) answerClusters(res *GraphResult, req GraphRequest) error {
	limit := req.Limit
	if limit == 0 {
		limit = min(DefaultNodes, MaxClusters)
	}
	limit = min(limit, MaxClusters)
	want := cursorBody{Scope: scopeKey(s.scope), View: ViewClusters}
	start := 0
	if req.Cursor != "" {
		got, err := decodeCursor(req.Cursor, want)
		if err != nil {
			return err
		}
		start = sort.Search(len(s.clusters), func(i int) bool { return got.Key < clusterKey(s.clusters[i].Kind, s.clusters[i].ID) })
	}
	end := min(start+limit, len(s.clusters))
	for _, c := range s.clusters[start:end] {
		c.ByState = copyCounts(c.ByState)
		res.Clusters = append(res.Clusters, c)
	}
	res.Returned = len(res.Clusters)
	if end < len(s.clusters) && end > start {
		last := s.clusters[end-1]
		want.Key = clusterKey(last.Kind, last.ID)
		res.Next = encodeCursor(want)
	}
	return nil
}

func (s *Snapshot) answerPeople(res *GraphResult) {
	res.Crews = append([]CrewFact(nil), s.crews...)
	for i := range res.Crews {
		res.Crews[i].ClosedAt = copyTime(res.Crews[i].ClosedAt)
		if h := res.Crews[i].Hue; h != nil {
			hue := *h
			res.Crews[i].Hue = &hue
		}
	}
	n := min(len(s.people), MaxMembers)
	res.Members = make([]MemberFact, n)
	for i := 0; i < n; i++ {
		m := s.people[i]
		m.JoinedAt = copyTime(m.JoinedAt)
		m.Tasks.ByState = copyCounts(m.Tasks.ByState)
		res.Members[i] = m
	}
	res.Returned = n
	res.Cut = len(s.people) > n
}
