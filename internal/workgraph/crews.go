package workgraph

import "sort"

// maxAlsoIn is how many other in-scope crews one task's place names.
const maxAlsoIn = 8

// crewPaths is every crew's ancestry inside the scope, outermost first and the
// crew itself last. A parent the scope does not hold ends the climb there, and
// a parent chain that loops is cut where it would repeat, so a bad record
// shortens a path and never runs away.
func crewPaths(crews []CrewInput) map[string][]string {
	known := make(map[string]CrewInput, len(crews))
	for _, c := range crews {
		if _, dup := known[c.ID]; !dup {
			known[c.ID] = c
		}
	}
	paths := make(map[string][]string, len(known))
	for id := range known {
		paths[id] = climb(id, known)
	}
	return paths
}

func climb(id string, known map[string]CrewInput) []string {
	seen := map[string]bool{}
	var up []string
	for cur := id; cur != "" && !seen[cur]; {
		c, ok := known[cur]
		if !ok {
			break
		}
		seen[cur] = true
		up = append(up, cur)
		cur = c.Parent
	}
	for i, j := 0, len(up)-1; i < j; i, j = i+1, j-1 {
		up[i], up[j] = up[j], up[i]
	}
	return up
}

// place is where one conversation sits among the scope's crews.
type place struct {
	crew   string
	also   []string
	member MemberInput
}

// placeCrews gives every kept task its owner's place in the scope's crews: the
// membership that names the owner's manager if there is one inside the scope,
// otherwise the deepest crew, ties by crew id; the other memberships are
// AlsoIn.
func (b *builder) placeCrews() error {
	if len(b.in.Crews) == 0 {
		return nil
	}
	paths := crewPaths(b.in.Crews)
	places := b.choosePlaces(paths)
	for i := range b.snap.nodes {
		if err := b.tick(i); err != nil {
			return err
		}
		p, ok := places[b.snap.nodes[i].Ref.Owner]
		if !ok {
			continue
		}
		b.snap.nodes[i].Crew = &CrewPlace{Crew: p.crew, Path: append([]string(nil), paths[p.crew]...), AlsoIn: p.also}
	}
	b.snap.crews = b.crewFacts(paths)
	return nil
}

func (b *builder) choosePlaces(paths map[string][]string) map[string]place {
	byAgent := map[string][]MemberInput{}
	for _, m := range b.in.Members {
		if _, ok := paths[m.Crew]; ok {
			byAgent[m.Agent] = append(byAgent[m.Agent], m)
		}
	}
	places := make(map[string]place, len(byAgent))
	for agent, list := range byAgent {
		sort.Slice(list, func(i, j int) bool { return better(list[i], list[j], paths) })
		p := place{crew: list[0].Crew, member: list[0]}
		for _, m := range list[1:] {
			if m.Crew != p.crew && len(p.also) < maxAlsoIn {
				p.also = append(p.also, m.Crew)
			}
		}
		places[agent] = p
	}
	return places
}

// better orders two memberships of one conversation: the home one first, then
// the deeper crew, then the lower crew id.
func better(a, b MemberInput, paths map[string][]string) bool {
	if a.Home != b.Home {
		return a.Home
	}
	if da, db := len(paths[a.Crew]), len(paths[b.Crew]); da != db {
		return da > db
	}
	return a.Crew < b.Crew
}

// crewFacts is the scope's crews with their direct member counts, outermost
// first.
func (b *builder) crewFacts(paths map[string][]string) []CrewFact {
	direct := map[string]int{}
	seen := map[[2]string]bool{}
	for _, m := range b.in.Members {
		key := [2]string{m.Crew, m.Agent}
		if _, ok := paths[m.Crew]; ok && !seen[key] {
			seen[key] = true
			direct[m.Crew]++
		}
	}
	var out []CrewFact
	done := map[string]bool{}
	for _, c := range b.in.Crews {
		if done[c.ID] {
			continue
		}
		done[c.ID] = true
		f := c.CrewFact
		f.Members = direct[c.ID]
		out = append(out, f)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if di, dj := len(paths[out[i].ID]), len(paths[out[j].ID]); di != dj {
			return di < dj
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func clusterKey(kind, id string) string { return kind + "\x00" + id }

// makeClusters groups the kept tasks by sub-crew and by task group and keeps,
// for each cluster, its members' indexes in creation order, so a cluster's
// members are a page away rather than a scan.
func (b *builder) makeClusters() error {
	s := b.snap
	s.members = map[string][]int32{}
	index := map[string]*GraphCluster{}
	for _, c := range s.crews {
		g := &GraphCluster{Kind: ClusterCrew, ID: c.ID, Title: c.Name, Parent: c.Parent, ByState: map[string]int{}}
		index[clusterKey(ClusterCrew, c.ID)] = g
	}
	for i := range s.nodes {
		if err := b.tick(i); err != nil {
			return err
		}
		n := &s.nodes[i]
		if n.Crew != nil {
			addToCrews(s, index, n, int32(i))
		}
		if n.Group != nil {
			g := groupCluster(index, n.Group)
			count(g, n.State, true)
			s.members[clusterKey(ClusterGroup, g.ID)] = append(s.members[clusterKey(ClusterGroup, g.ID)], int32(i))
		}
	}
	s.clusters = collect(index)
	return nil
}

func addToCrews(s *Snapshot, index map[string]*GraphCluster, n *GraphNode, at int32) {
	for depth, id := range n.Crew.Path {
		g := index[clusterKey(ClusterCrew, id)]
		if g == nil {
			continue
		}
		count(g, n.State, depth == len(n.Crew.Path)-1)
		s.members[clusterKey(ClusterCrew, id)] = append(s.members[clusterKey(ClusterCrew, id)], at)
	}
}

func groupCluster(index map[string]*GraphCluster, ref *GroupRef) *GraphCluster {
	key := clusterKey(ClusterGroup, ref.ID)
	g := index[key]
	if g == nil {
		g = &GraphCluster{Kind: ClusterGroup, ID: ref.ID, ByState: map[string]int{}}
		index[key] = g
	}
	if g.Title == "" {
		g.Title = ref.Title
	}
	return g
}

func count(g *GraphCluster, state string, direct bool) {
	g.Total++
	g.ByState[state]++
	if direct {
		g.Direct++
	}
}

func collect(index map[string]*GraphCluster) []GraphCluster {
	out := make([]GraphCluster, 0, len(index))
	for _, g := range index {
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// makePeople answers each membership with how its owner's work in the scope
// stands. A conversation in two crews appears once per crew, with the same
// counts: the work is the conversation's, not the crew's.
func (b *builder) makePeople() error {
	if len(b.in.Members) == 0 {
		return nil
	}
	tasks := map[string]*MemberTasks{}
	for i := range b.snap.nodes {
		if err := b.tick(i); err != nil {
			return err
		}
		n := &b.snap.nodes[i]
		t := tasks[n.Ref.Owner]
		if t == nil {
			t = &MemberTasks{ByState: map[string]int{}}
			tasks[n.Ref.Owner] = t
		}
		t.Total++
		t.ByState[n.State]++
	}
	seen := map[[2]string]bool{}
	var people []MemberFact
	for _, m := range b.in.Members {
		key := [2]string{m.Crew, m.Agent}
		if seen[key] {
			continue
		}
		seen[key] = true
		f := m.MemberFact
		if t := tasks[m.Agent]; t != nil {
			f.Tasks = MemberTasks{Total: t.Total, ByState: copyCounts(t.ByState)}
		}
		people = append(people, f)
	}
	sort.SliceStable(people, func(i, j int) bool {
		if people[i].Crew != people[j].Crew {
			return people[i].Crew < people[j].Crew
		}
		return people[i].Agent < people[j].Agent
	})
	b.snap.people = people
	b.snap.totals.Members = len(people)
	return nil
}

func copyCounts(in map[string]int) map[string]int {
	if in == nil {
		return nil
	}
	out := make(map[string]int, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
