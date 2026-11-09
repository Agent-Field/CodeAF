package intake

import (
	"context"
	"errors"
	"fmt"
	"strings"
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
// names one, and on its AgentField reasoner when the rule runs it on one. It
// changes nothing stored; it is what the pass reads the repository's switch
// and file for, and a job a rule runs on a reasoner names a repository it
// does not need ([factory.NamesNoRepo]).
func routedTo(it factory.Item, r *routing) factory.Item {
	if r != nil {
		it.Repo = repoFor(r.rule, it)
		it.RunOn = r.rule.Then.RunOnFor(it)
	}
	return it
}

// repoFor is the repository rule sends it to: A PERSON'S REPOSITORY WINS
// ([factory.Item.RepoSetBy]), else the rule's when it names one, else the
// one it already has.
func repoFor(rule route.Rule, it factory.Item) string {
	repo, _ := route.PipelineFor(rule, it)
	return repo
}

// refusedBy is why rule cannot be applied to it under f, the file of the
// repository it sends it to, "" when it can: a pipeline that file does not
// list. It is the activity row's sentence, and the one `factory.routes_test`
// answers for the same rule ([route.PipelineRefusal]).
func refusedBy(rule route.Rule, it factory.Item, f pipeline.File) string {
	if rule.Then.Pipeline == "" || len(f.Recipe().Named(rule.Then.Pipeline)) > 0 {
		return ""
	}
	return route.PipelineRefusal(rule.Name, it.Repo, rule.Then.Pipeline)
}

// replacedWords is the activity sentence of a job rule r now routes in place
// of the rule that routed it before, with the steps it has now.
func replacedWords(it factory.Item, r *routing, staged []factory.Stage) string {
	names := make([]string, 0, len(staged))
	for _, st := range staged {
		names = append(names, st.Name)
	}
	out := r.reason() + " · in place of routing rule " + it.Route
	if len(names) > 0 {
		out += " · steps " + strings.Join(names, ", ")
	}
	return out
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
// the steps of the pipeline it names, or the AgentField reasoner it runs on.
// An empty field keeps what cur has.
//
// A PERSON'S PRIORITY MARK WINS over a rule's, as it wins over a label
// ([factory.PriorityFor]); the priority a source sent does not, because the
// rule is the factory's own decision about where that source's work stands.
// A pipeline the file does not list is refused, and nothing about the job is
// changed by a refused rule.
func applyRoute(cur *factory.Item, rule route.Rule, f pipeline.File) error {
	var steps []factory.Stage
	if rule.Then.Pipeline != "" {
		if steps = f.Named(rule.Then.Pipeline); len(steps) == 0 {
			return errors.New(route.PipelineRefusal(rule.Name, repoFor(rule, *cur), rule.Then.Pipeline))
		}
	}
	cur.Route, cur.RouteRefused = rule.Name, ""
	if repo := repoFor(rule, *cur); repo != cur.Repo {
		cur.Repo, cur.Places = repo, []string{repo}
	}
	// The source's own priority is not a person's mark (inbox.SetBySource):
	// the rule's word replaces it, as it would replace none.
	if value, ok := factory.PriorityFromShort(rule.Then.Priority); ok && (cur.Priority == nil || cur.Priority.SetBy == inbox.SetBySource) {
		cur.Priority = &factory.JobPriority{Value: value}
	}
	if steps != nil {
		cur.Stages = factory.CopyStages(steps)
	}
	// A JOB A RULE RUNS ON AN AGENTFIELD REASONER HAS THE RUN'S STEPS, NOT A
	// PIPELINE'S: it waits with none, as a job filed with run_on does, and
	// its steps are the run's calls once it starts.
	if rule.Then.RunOn != "" {
		cur.RunOn, cur.Stages = rule.Then.RunOn, nil
	}
	withdrawWaiting(cur)
	return nil
}

// withdrawWaiting takes the inbox's "waiting for a repository" line off cur
// once routing gave it somewhere to run: a repository, or an AgentField
// reasoner. THE INBOX FILES BEFORE THE TABLE IS READ, so it writes that line
// on every job that arrives with neither; a rule that then routes the job in
// the same instant left a row saying the opposite of what happened, and a
// job run on a reasoner never waits for a repository at all.
func withdrawWaiting(cur *factory.Item) {
	if strings.TrimSpace(cur.Repo) == "" && strings.TrimSpace(cur.RunOn) == "" {
		return
	}
	kept := cur.Activity[:0:0]
	for _, ev := range cur.Activity {
		if ev.What != inbox.WaitingWords {
			kept = append(kept, ev)
		}
	}
	if len(kept) != len(cur.Activity) {
		cur.Activity = kept
	}
}
