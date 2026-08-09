package plan

import (
	"fmt"
	"strings"
)

// Bundle lays several requests that do not feed each other side by side: every
// part in stage one, a synthesis that reads them all in stage two, no planner
// call anywhere.
//
// It exists because the prompt route was measured to fail. The spine was told,
// in its own words, that a bundle's listed order is never a gate; on the live
// run it restated that rule — "none of them depends on the others" — and then
// emitted one stage per request with invented cross-part needs, and three
// independent asks ran end to end. The judgment of WHETHER the requests are
// independent stays with the model that read the whole ask (the intent
// compiler names the parts); what stops being a judgment is the layout, which
// is geometry once independence is declared: parts share a stage, and a stage
// is never manufactured from the order words arrived in.
//
// The shape is the ensemble's — N workers, one merge — because that is the one
// graph shape in the system whose parallelism is load-bearing and proven.
func Bundle(goal string, parts []string) *Graph {
	graph := &Graph{Goal: goal}
	ids := make([]int, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		ids = append(ids, graph.Add(Node{
			Stage: 1,
			// The name is the person's own first words for the part, not an
			// invention: recognisable in a rail beside its siblings.
			Title:   clipTitle(part),
			Summary: part,
			Brief:   part,
			Size:    SizeAtomic,
			Kind:    KindWork,
		}))
	}
	graph.Add(Node{
		Stage:   2,
		Title:   "Deliver together",
		Summary: fmt.Sprintf("Assemble the %d finished results into one delivery, in the order they were asked.", len(ids)),
		Needs:   ids,
		Kind:    KindSynthesis,
		Brief: fmt.Sprintf("The %d results above were asked for in one breath and finished independently. "+
			"Your final message is the whole delivery: every result in full, in the order the person asked, "+
			"each under its own words from the request. Nothing is summarised away and nothing is added between them.", len(ids)),
	})
	return graph
}

// clipTitle bounds a part's display name to rail width on a word boundary.
func clipTitle(part string) string {
	const width = 48
	title := strings.Join(strings.Fields(part), " ")
	if len(title) <= width {
		return title
	}
	clipped := title[:width]
	if cut := strings.LastIndexByte(clipped, ' '); cut > width/2 {
		clipped = clipped[:cut]
	}
	return clipped
}
