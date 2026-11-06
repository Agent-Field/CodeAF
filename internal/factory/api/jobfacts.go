package api

import (
	"slices"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// JobRoute is the routing rule that decided a job ([factory.Item.Route]) and,
// when that rule asked the AI, the AI's answer about this item
// ([factory.Item.Judged] under the rule's name).
type JobRoute struct {
	Rule   string       `json:"rule"`
	Answer *RouteAnswer `json:"answer,omitempty"`
}

// RouteAnswer is the AI's yes or no about one item, its one sentence of why,
// and the model that answered. The key and the instant it was asked stay the
// factory's own ([factory.RouteAnswer]).
type RouteAnswer struct {
	Yes   bool   `json:"yes"`
	Why   string `json:"why"`
	Model string `json:"model,omitempty"`
}

// RouteDeclined is one routing rule whose question the AI answered no about
// this item, with its one sentence of why. It is how a job that no rule
// started says so: the rule read on past it, and this is the reason.
type RouteDeclined struct {
	Rule  string `json:"rule"`
	Why   string `json:"why"`
	Model string `json:"model,omitempty"`
}

// declined is every no kept on it except under the rule that decided it, in
// rule-name order so two reads of one job agree.
func declined(it factory.Item) []RouteDeclined {
	var out []RouteDeclined
	for rule, a := range it.Judged {
		if !a.Yes && rule != it.Route {
			out = append(out, RouteDeclined{Rule: rule, Why: a.Why, Model: a.Model})
		}
	}
	slices.SortFunc(out, func(a, b RouteDeclined) int { return strings.Compare(a.Rule, b.Rule) })
	return out
}

// jobFacts sets the facts a job's page says about where it came from, why it
// started and how far it is. EACH IS COMPUTED ONCE, in internal/factory, and
// only carried here; a surface reads them and never derives them again.
func jobFacts(j *Job, it factory.Item) {
	if src, ok := factory.JobSource(it); ok {
		j.Source = &src
	}
	j.RepoURL = factory.RepoURL(it)
	if rule := it.Route; rule != "" {
		j.Route = &JobRoute{Rule: rule}
		if a, ok := it.Judged[rule]; ok {
			j.Route.Answer = &RouteAnswer{Yes: a.Yes, Why: a.Why, Model: a.Model}
		}
	}
	j.RouteDeclined = declined(it)
	j.StepsDone, j.StepsTotal = factory.StepCount(it)
}

// itemFacts is jobFacts read back, for a client seam that rebuilds the item:
// the deciding rule and its answer. The source's key comes back whole from
// desk.source_ref.
func itemFacts(j Job, it *factory.Item) {
	if r := j.Route; r != nil {
		it.Route = r.Rule
		if a := r.Answer; a != nil {
			it.Judged = map[string]factory.RouteAnswer{r.Rule: {Yes: a.Yes, Why: a.Why, Model: a.Model}}
		}
	}
	for _, d := range j.RouteDeclined {
		if it.Judged == nil {
			it.Judged = map[string]factory.RouteAnswer{}
		}
		it.Judged[d.Rule] = factory.RouteAnswer{Why: d.Why, Model: d.Model}
	}
}

// ModelsIn says on a reader's job the model its next conversation step runs
// on and, on each conversation step that has not finished, the model that step
// runs on ([factory.StepModel]). d is what this factory knows below the job's
// own picks: its current default, and its runner's models when it runs its
// own jobs. A choice nothing here can name is left absent.
//
// IT IS CALLED ON READS ONLY, never by [FromItem]: a worker's report carries
// steps, and an older factory refuses a field it does not know.
func ModelsIn(j *Job, it factory.Item, d factory.ModelDefaults) {
	j.RunsOn = nil
	next, ok := factory.NextModelStage(it)
	if !ok {
		next = factory.Stage{}
	}
	if c := factory.StepModel(next, it, d); c.Model != "" {
		j.RunsOn = &c
	}
	for i := range j.Steps {
		s := &j.Steps[i]
		s.RunsOn = nil
		switch s.State {
		case StepDone, StepSkipped:
			continue
		}
		st := factory.Stage{Name: s.Name, Kind: factory.StageKind(s.Kind), Model: s.UseModel, Effort: s.Effort}
		if k := factory.StageIndex(it.Stages, s.Name); k >= 0 {
			st = it.Stages[k]
		}
		if !factory.IsChatStage(st) {
			continue
		}
		if c := factory.StepModel(st, it, d); c.Model != "" {
			s.RunsOn = &c
		}
	}
}
