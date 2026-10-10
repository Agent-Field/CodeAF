package decide

import "github.com/Agent-Field/codeaf/internal/placegraph"

// EscalationDepth limits how far a question may travel from its chat's places.
// This is separate from context inheritance and settings lookup depth.
const EscalationDepth = 3

// EscalationGraph is the read-only graph needed to route one question. A
// placegraph.Snapshot satisfies it, keeping routing on one graph revision.
type EscalationGraph interface {
	Place(string) (placegraph.Place, bool)
	PlacesOf(string) []placegraph.Membership
	EffectiveDecide(string) (placegraph.Decide, string)
}

// Assessment is the question-specific confidence and the place's mode for its
// kind. Callers supply real evidence; routing never invents confidence.
type Assessment struct {
	Mode    Mode
	Percent int
}

// Escalate returns the place that can decide, or an empty id for the person.
// It visits nearer parents first, preserving parent order for ties. Multiple
// memberships start at their nearest common ancestor, never at just one member.
// Assessment errors are returned so a failed lookup cannot authorise a decision.
// This selects an owner only; the caller still owns answering and recording it.
func Escalate(graph EscalationGraph, chatID string, assess func(string) (Assessment, error)) (string, error) {
	if graph == nil || assess == nil {
		return "", nil
	}
	var roots []string
	seen := map[string]bool{}
	for _, m := range graph.PlacesOf(chatID) {
		p, ok := graph.Place(m.PlaceID)
		if !ok || p.Archived || seen[p.ID] {
			continue
		}
		// A person's explicit opt-out applies even when the chat is filed
		// in several places and would otherwise start at a shared parent.
		if settings, _ := graph.EffectiveDecide(p.ID); settings.AlwaysAsk {
			return "", nil
		}
		seen[p.ID] = true
		roots = append(roots, p.ID)
	}
	if len(roots) == 0 {
		return "", nil
	}
	start, depth := roots[0], 0
	if len(roots) > 1 {
		start, depth = commonDecider(graph, roots)
		if start == "" {
			return "", nil
		}
	}
	for _, node := range escalationWalk(graph, start, EscalationDepth-depth) {
		p, _ := graph.Place(node.id)
		if p.Archived {
			continue
		}
		settings, _ := graph.EffectiveDecide(p.ID)
		if settings.AlwaysAsk {
			return "", nil
		}
		a, err := assess(p.ID)
		if err != nil {
			return "", err
		}
		if a.Mode == ModeAsk {
			return "", nil
		}
		if a.Mode == ModeDeciding && a.Percent >= settings.Threshold && a.Percent <= 100 {
			return p.ID, nil
		}
	}
	return "", nil
}

type escalationNode struct {
	id    string
	depth int
}

// escalationWalk records shortest distances once, so a diamond does not ask a
// shared parent twice and even a damaged cyclic graph terminates.
func escalationWalk(graph EscalationGraph, start string, limit int) []escalationNode {
	queue := []escalationNode{{id: start}}
	seen := map[string]bool{start: true}
	var out []escalationNode
	for i := 0; i < len(queue); i++ {
		n := queue[i]
		p, ok := graph.Place(n.id)
		if !ok {
			continue
		}
		out = append(out, n)
		if n.depth >= limit {
			continue
		}
		for _, parent := range p.Parents {
			if !seen[parent] {
				seen[parent] = true
				queue = append(queue, escalationNode{id: parent, depth: n.depth + 1})
			}
		}
	}
	return out
}

// commonDecider minimises the furthest membership distance, then total
// distance. Exact ties keep the first membership's parent traversal order.
// Its returned depth keeps the later escalation inside the original hop cap.
func commonDecider(graph EscalationGraph, roots []string) (string, int) {
	distances := make([]map[string]int, len(roots))
	var order []escalationNode
	for i, root := range roots {
		walk := escalationWalk(graph, root, EscalationDepth)
		if i == 0 {
			order = walk
		}
		distances[i] = map[string]int{}
		for _, n := range walk {
			distances[i][n.id] = n.depth
		}
	}
	best, bestMax, bestSum := "", EscalationDepth+1, len(roots)*(EscalationDepth+1)
	for _, n := range order {
		p, _ := graph.Place(n.id)
		if p.Archived {
			continue
		}
		maxDepth, sum, common := 0, 0, true
		for _, dist := range distances {
			d, ok := dist[n.id]
			if !ok {
				common = false
				break
			}
			if d > maxDepth {
				maxDepth = d
			}
			sum += d
		}
		if common && (maxDepth < bestMax || maxDepth == bestMax && sum < bestSum) {
			best, bestMax, bestSum = n.id, maxDepth, sum
		}
	}
	return best, bestMax
}
