package route

import (
	"sort"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// MaxPreview is the most jobs one preview reads, and DefaultPreview the
// number it reads when asked for none.
const (
	MaxPreview     = 200
	DefaultPreview = 50
)

// Tried is what a table would do with one job, read with only the answers
// already kept on it.
type Tried struct {
	Job   int    `json:"job"`
	Ref   string `json:"ref"`
	Title string `json:"title"`
	// Outcome is "matched", "pending" or "none".
	Outcome string `json:"outcome"`
	// Rule names the rule that matched, or the rule whose question has no
	// fresh answer yet when the outcome is pending.
	Rule string `json:"rule,omitempty"`
	// Why is the kept answer's one sentence when a question decided it.
	Why string `json:"why,omitempty"`
}

// Preview reads rules over the limit most recently made jobs of items, newest
// first, and says per job what [Decide] would do. PURE, and it ASKS NO
// MODEL: a question with no fresh answer on the job is reported pending,
// which is the honest answer to "what would these rules do right now".
func Preview(rules []Rule, items []factory.Item, limit int) []Tried {
	if limit <= 0 {
		limit = DefaultPreview
	}
	limit = min(limit, MaxPreview)
	recent := append([]factory.Item(nil), items...)
	sort.SliceStable(recent, func(i, j int) bool {
		if !recent[i].Created.Equal(recent[j].Created) {
			return recent[i].Created.After(recent[j].Created)
		}
		return recent[i].ID > recent[j].ID
	})
	if len(recent) > limit {
		recent = recent[:limit]
	}
	out := make([]Tried, 0, len(recent))
	for _, it := range recent {
		t := Tried{Job: it.ID, Ref: it.Ref(), Title: it.Title, Outcome: "none"}
		switch o := Decide(rules, it, HeldAnswers(it)); {
		case o.Pending != "":
			t.Outcome, t.Rule = "pending", o.Pending
		case o.Matched:
			t.Outcome, t.Rule = "matched", o.Rule.Name
			if o.Answer != nil {
				t.Why = o.Answer.Why
			}
		}
		out = append(out, t)
	}
	return out
}
