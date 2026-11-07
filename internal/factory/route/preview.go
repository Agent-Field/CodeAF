package route

import (
	"sort"
	"strings"

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
	// Outcome is "matched", "pending", "refused" or "none".
	Outcome string `json:"outcome"`
	// Rule names the rule that matched, the rule whose question has no
	// fresh answer yet when the outcome is pending, or the rule that matched
	// and could not be applied when it is refused.
	Rule string `json:"rule,omitempty"`
	// Why is the kept answer's one sentence when a question decided it.
	Why string `json:"why,omitempty"`
	// Reason is why a refused rule cannot be applied
	// ([PipelineRefusal]), the same sentence the job's activity says.
	Reason string `json:"reason,omitempty"`
}

// Outcomes of [Tried].
const (
	TriedMatched = "matched"
	TriedPending = "pending"
	TriedRefused = "refused"
	TriedNone    = "none"
)

// Listed says whether repo's pipeline file lists a pipeline named name, and
// known false when that file cannot be read (then nothing is refused).
type Listed func(repo, name string) (listed, known bool)

// PipelineRefusal is the one sentence of a rule that sends a job to a
// repository whose file lists no pipeline of the name it gives: the job's
// activity row, `factory.routes_test`'s reason and `factory.routes_set`'s
// refusal all say it.
func PipelineRefusal(rule, repo, pipeline string) string {
	return "routing rule " + rule + ": " + repo + " has no pipeline " + pipeline
}

// PipelineFor is the repository and pipeline a matched rule sends it to:
// the rule's repository when it names one, else the job's own; "" pipeline
// when the rule names none, which nothing can refuse. A person's repository
// ([factory.Item.RepoSetBy]) wins over the rule's, as intake applies it.
func PipelineFor(r Rule, it factory.Item) (repo, pipeline string) {
	repo = r.Then.RepoFor(it)
	if it.RepoSetBy != "" && strings.TrimSpace(it.Repo) != "" {
		repo = it.Repo
	}
	return repo, r.Then.Pipeline
}

// Preview reads rules over the limit most recently made jobs of items, newest
// first, and says per job what [Decide] would do. PURE, and it ASKS NO
// MODEL: a question with no fresh answer on the job is reported pending,
// which is the honest answer to "what would these rules do right now".
func Preview(rules []Rule, items []factory.Item, limit int) []Tried {
	return PreviewListed(rules, items, limit, nil)
}

// PreviewListed is [Preview] that also says a matched rule is refused when
// listed knows the repository it sends a job to lists no pipeline of the
// name it gives. listed is the caller's read; nil knows no file, and then
// nothing is refused.
func PreviewListed(rules []Rule, items []factory.Item, limit int, listed Listed) []Tried {
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
		t := Tried{Job: it.ID, Ref: it.Ref(), Title: it.Title, Outcome: TriedNone}
		switch o := Decide(rules, it, HeldAnswers(it)); {
		case o.Pending != "":
			t.Outcome, t.Rule = TriedPending, o.Pending
		case o.Matched:
			t.Outcome, t.Rule = TriedMatched, o.Rule.Name
			if o.Answer != nil {
				t.Why = o.Answer.Why
			}
			if repo, name := PipelineFor(o.Rule, it); name != "" && strings.TrimSpace(repo) != "" && listed != nil {
				if ok, known := listed(repo, name); known && !ok {
					t.Outcome, t.Reason = TriedRefused, PipelineRefusal(o.Rule.Name, repo, name)
				}
			}
		}
		out = append(out, t)
	}
	return out
}
