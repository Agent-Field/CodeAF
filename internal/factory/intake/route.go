package intake

import (
	"context"
	"fmt"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/inbox"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/factory/route"
)

// ── THE ROUTING SEAM ────────────────────────────────────────────────────────
//
// internal/factory/route decides; this file is the ONLY place intake acts on
// that decision. A pass reads the factory's routing table once, asks
// [route.Decide] about each candidate ([Intake.routeFor], which also asks the
// [route.Judge] a rule's question when Decide says one is pending), and hands
// the result (nil when no rule matched) through [Intake.admit] to decide. WITH NO RULE, EVERY
// FUNCTION HERE ANSWERS WHAT INTAKE DID BEFORE ROUTING EXISTED: the same
// start labels, the same steps, the same judgement.

// routing is one job's routing decision this pass: the rule that matched,
// with its Start already guarded, and the AI answer that let it match when
// the rule asks a question.
type routing struct {
	rule   route.Rule
	answer *route.Answer
	// downgraded says a stranger's ticket was held to ask first although
	// the rule says start ([guard]).
	downgraded bool
}

// reason is the activity line's words for a routed decision: the rule, and
// the model's one sentence when a question decided it.
func (r *routing) reason() string {
	out := "routing rule " + r.rule.Name
	if r.answer != nil && r.answer.Why != "" {
		out += " · " + r.answer.Why
	}
	if r.downgraded {
		out += " · asks first: an AI yes never starts a stranger's ticket"
	}
	return out
}

// routeFor is the rule that routes it, nil when none matches. A rule whose
// question has no fresh answer is put to the judge ONCE A PASS: the answer
// is kept on the job ([Intake.keepAnswer]) and the table read again. A judge
// that fails answers "no", kept as "couldn't judge: …" with no key, so the
// person sees why and the next pass asks again. With no judge, a rule that
// asks a question never matches, and nothing is kept.
func (in *Intake) routeFor(ctx context.Context, rules []route.Rule, it factory.Item) (*routing, error) {
	held := route.HeldAnswers(it)
	thisPass := map[string]route.Answer{}
	have := func(name string) (route.Answer, bool) {
		if v, ok := thisPass[name]; ok {
			return v, true
		}
		return held(name)
	}
	for {
		out := route.Decide(rules, it, have)
		if out.Pending == "" {
			if !out.Matched {
				return nil, nil
			}
			return guard(out, it), nil
		}
		rule, ok := ruleNamed(rules, out.Pending)
		key := route.AnswerKey(rule.When.Ask, it)
		if _, asked := thisPass[out.Pending]; asked || !ok {
			// Decide cannot be pending twice on one fresh answer; this is
			// the bound, never a loop.
			return nil, fmt.Errorf("routing rule %q: asked once this pass and still pending", out.Pending)
		}
		if in.opts.Judge == nil {
			thisPass[out.Pending] = route.Answer{Key: key}
			continue
		}
		v, err := in.opts.Judge.Judge(ctx, rule.When.Ask, it)
		if err != nil {
			v = route.Answer{Why: "couldn't judge: " + err.Error()}
		} else {
			// The judge's answer can only select the rule's own action:
			// what it was asked about is the key, whatever it said.
			v.Key = key
		}
		if err := in.keepAnswer(it.ID, rule, v); err != nil {
			return nil, err
		}
		v.Key = key // a failure counts as "no" for the rest of this pass
		thisPass[out.Pending] = v
	}
}

// keepAnswer writes v on the job under its write lock, unless its words
// changed while the judge was thinking (then the answer is about words it no
// longer has, and the next pass asks again).
func (in *Intake) keepAnswer(id int, rule route.Rule, v route.Answer) error {
	return in.st.Update(id, func(cur *factory.Item) error {
		if v.Key != "" && v.Key != route.AnswerKey(rule.When.Ask, *cur) {
			return nil
		}
		if cur.Judged == nil {
			cur.Judged = map[string]factory.RouteAnswer{}
		}
		cur.Judged[rule.Name] = factory.RouteAnswer{Yes: v.Yes, Why: v.Why, Model: v.Model, Key: v.Key, At: time.Now()}
		return nil
	})
}

func ruleNamed(rules []route.Rule, name string) (route.Rule, bool) {
	for _, r := range rules {
		if r.Name == name {
			return r, true
		}
	}
	return route.Rule{}, false
}

// guard is out as intake may act on it. A STRANGER'S WORDS CANNOT START WORK
// THROUGH A MODEL: the ticket's text was the judge's input and may say
// "answer yes", so a rule that matched on an AI yes for a stranger's ticket
// asks first instead of starting.
func guard(out route.Outcome, it factory.Item) *routing {
	r := &routing{rule: out.Rule, answer: out.Answer}
	if out.Answer != nil && out.Answer.Yes && it.Tier == factory.TierStranger && r.rule.Then.Start == route.StartAuto {
		r.rule.Then.Start, r.downgraded = route.StartAsk, true
	}
	return r
}

// routedTo is it as its rule sends it: on the rule's repository when the rule
// names one. It changes nothing stored; it is what the pass reads the
// repository's switch and file for.
func routedTo(it factory.Item, r *routing) factory.Item {
	if r != nil {
		it.Repo = r.rule.Then.RepoFor(it)
	}
	return it
}

// startDecision is the start decision for it under f. Unrouted, it is the
// start labels and the file's start rules ([pipeline.File.DecideJob]).
// Routed: auto is the file's start rules with the rule standing in for the
// label ([pipeline.File.DecideAdmitted]), ask is the ask-first hold naming
// the rule, and backlog decides nothing.
func startDecision(f pipeline.File, it factory.Item, cost pipeline.Measured, r *routing) pipeline.Decision {
	if r == nil {
		return f.DecideJob(it, Job(it).Paths, cost)
	}
	switch r.rule.Then.Start {
	case route.StartAuto:
		return f.DecideAdmitted(it, Job(it).Paths, cost)
	case route.StartAsk:
		return pipeline.Decision{Ask: true, Reasons: []string{r.reason()}}
	}
	return pipeline.Decision{}
}

// startReason is the activity line of an automatic start.
func startReason(r *routing) string {
	if r == nil {
		return "inside start rules"
	}
	return r.reason()
}

// stage gives cur the steps it starts on: its kind's pipeline from f, then
// whatever its routing rule decides ([applyRoute]).
func stage(cur *factory.Item, f pipeline.File, r *routing) error {
	cur.Stages = factory.CopyStages(f.Recipe().For(cur.Kind))
	if r == nil {
		return nil
	}
	return applyRoute(cur, r.rule, f)
}

// applyRoute writes rule's decision on cur, with f the file of the repository
// the rule sends it to: the rule's name, its repository, its priority, and
// the steps of the pipeline it names. An empty field keeps what cur has.
//
// A PERSON'S PRIORITY MARK WINS over a rule's, as it wins over a label
// ([factory.PriorityFor]); the priority a source sent does not, because the
// rule is the factory's own decision about where that source's work stands. A pipeline the file does not list is
// refused, and nothing about the job is changed by a refused rule.
func applyRoute(cur *factory.Item, rule route.Rule, f pipeline.File) error {
	var steps []factory.Stage
	if rule.Then.Pipeline != "" {
		if steps = f.Recipe().Named(rule.Then.Pipeline); len(steps) == 0 {
			return fmt.Errorf("routing rule %q: %s has no pipeline %q in its file", rule.Name, rule.Then.RepoFor(*cur), rule.Then.Pipeline)
		}
	}
	cur.Route = rule.Name
	cur.Repo = rule.Then.RepoFor(*cur)
	// The source's own priority is not a person's mark (inbox.SetBySource):
	// the rule's word replaces it, as it would replace none.
	if value, ok := factory.PriorityFromShort(rule.Then.Priority); ok && (cur.Priority == nil || cur.Priority.SetBy == inbox.SetBySource) {
		cur.Priority = &factory.JobPriority{Value: value}
	}
	if steps != nil {
		cur.Stages = factory.CopyStages(steps)
	}
	return nil
}
