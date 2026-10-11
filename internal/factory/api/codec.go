package api

import (
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// FromSnapshot is the floor as `GET /v1/queue` says it to me.
func FromSnapshot(s factory.Snapshot, me string) Queue {
	jobs := make([]Job, len(s.Items))
	for i, it := range s.Items {
		jobs[i] = FromItem(it)
	}
	return Queue{
		Since:    fromShift(s.Shift),
		Jobs:     jobs,
		Repos:    fromRepos(s.Repos, s.Checkouts),
		Strip:    strip(jobs),
		Capacity: Capacity{Benches: s.Benches, Daily: s.Daily, Rail: s.Rail, Queued: s.Count(factory.StateQueued)},
		Me:       me,
		Desk: &QueueDesk{
			Now: s.Now, Sources: mapList(s.Sources, fromSource), Busy: s.Busy, BusyAll: s.BusyAll,
			LastReadCost: s.LastReadCost, Marked: s.Marked, Checkouts: s.Checkouts,
		},
	}
}

// ToSnapshot is the inverse a client seam's Load answers: the floor rebuilt
// from the queue and the jobs it lists. jobs nil reads q.Jobs. Everything the
// TUI page reads comes back; what does not is named in [Lost].
func ToSnapshot(q Queue, jobs []Job) factory.Snapshot {
	if jobs == nil {
		jobs = q.Jobs
	}
	d := q.Desk
	if d == nil {
		d = &QueueDesk{}
	}
	return factory.Snapshot{
		Now: d.Now, Repos: toRepos(q.Repos), Items: mapList(jobs, ToItem),
		Benches: q.Capacity.Benches, Daily: q.Capacity.Daily, Rail: q.Capacity.Rail,
		Shift: toShift(q.Since), Sources: mapList(d.Sources, toSource),
		Busy: d.Busy, BusyAll: d.BusyAll, LastReadCost: d.LastReadCost,
		Marked: d.Marked, Checkouts: d.Checkouts,
	}
}

// Lost names the Go fields the wire does not carry, and why. The round-trip
// test reads it, so a field is either carried or named here.
var Lost = map[string]string{
	"Item.OldGate":      "read into approve steps on load and never in memory",
	"Stage.OldGate":     "read into approve steps on load and never in memory",
	"Stage.OldGateWhen": "read into approve steps on load and never in memory",
	"Phase.Carry":       "the runner's own inbox for a resumed step; no surface reads it",
	"Phase.Result":      "only Output (step result) and Spent (step cost) cross; the rest is the runner's revival state",
	"Phase.Kind":        "an empty phase kind under a stage with a kind comes back as the stage's",
	"Phase.Left":        "crosses in whole milliseconds",
}

// FromItem is one item as a job.
func FromItem(it factory.Item) Job {
	j := Job{
		ID: it.ID, Repo: it.Repo, Num: it.Num, Kind: string(it.Kind), Title: it.Title, URL: it.URL,
		State: stateOut[it.State], Ready: it.State == factory.StateLanded, Gate: gate(it),
		AssignedTo: it.Person(),
		Steps:      steps(it),
		Claims:     mapList(it.Proof, fromClaim),
		Questions:  questions(it),
		CreatedAt:  it.Created,
		Desk:       fromDesk(it),
	}
	if pr := it.PullRequest; pr != nil {
		j.PR = &PR{Number: pr.Number, URL: pr.URL, CI: pr.CI}
	}
	if j.State == "" {
		j.State = string(it.State)
	}
	if a := it.Asking; a != nil && a.With == factory.AskedYou {
		at := a.At
		j.WaitingSince = &at
	}
	if st := it.Stream; st != nil {
		j.Cost = st.Spent
		j.Step = &StepAt{Index: st.Cur, Count: len(st.Phases)}
		if st.Cur >= 0 && st.Cur < len(st.Phases) {
			j.Step.Name = st.Phases[st.Cur].Name
		}
	}
	return j
}

// ToItem is one job as an item.
func ToItem(j Job) factory.Item {
	d := j.Desk
	if d == nil {
		d = &JobDesk{}
	}
	it := factory.Item{
		ID: j.ID, Repo: j.Repo, Num: j.Num, Kind: factory.Kind(j.Kind), Title: j.Title, URL: j.URL,
		State: stateIn(j), Created: j.CreatedAt,
		Product: d.Product, Places: d.Places, Body: d.Body, Author: d.Author,
		Assignee: d.Assignee, Starter: d.Starter, Taker: d.Taker,
		Tier: factory.Tier(d.Tier), Origin: factory.Origin(d.Origin), Synced: d.Synced, Changed: d.ChangedAt,
		Triage: toTriage(d.Triage), Cap: d.Cap,
		Proof: mapList(j.Claims, toClaim), Policy: mapList(d.Policy, toClaim),
		Marked: d.Marked, Labels: d.Labels, Checks: d.Checks, Diff: d.Diff,
		Adapted: d.Adapted, Notes: d.Notes, Talk: d.Talk, Heard: d.Heard,
		Comments: mapList(d.Comments, toComment), Files: mapList(d.Files, toFile),
		CheckRuns: mapList(d.CheckRuns, toCheckRun), Base: d.Base, Head: d.Head, HeadSHA: d.HeadSHA,
		Activity: mapList(d.Activity, toActivity),
	}
	if pr := j.PR; pr != nil {
		it.PullRequest = &factory.PullRequest{Number: pr.Number, URL: pr.URL, CI: pr.CI}
	}
	it.Stages, it.Stream = unsteps(j)
	unquestions(&it, j.Questions)
	return it
}

var stateOut = map[factory.State]string{
	factory.StateNew:       StateBacklog,
	factory.StateQueued:    StateQueued,
	factory.StateRunning:   StateRunning,
	factory.StateNeedsYou:  StateNeedsYou,
	factory.StateLanded:    StateNeedsYou,
	factory.StateShipped:   StateDone,
	factory.StateDismissed: StateDropped,
}

var stateBack = map[string]factory.State{
	StateBacklog:  factory.StateNew,
	StateQueued:   factory.StateQueued,
	StateRunning:  factory.StateRunning,
	StateNeedsYou: factory.StateNeedsYou,
	StateDone:     factory.StateShipped,
	StateDropped:  factory.StateDismissed,
}

// stateIn reads a job's state back. A state the API does not name passes
// through as it came, so a state the floor grows later is not lost on the
// wire before the codec learns its name.
func stateIn(j Job) factory.State {
	if j.Ready {
		return factory.StateLanded
	}
	if s, ok := stateBack[j.State]; ok {
		return s
	}
	return factory.State(j.State)
}

// gate is what Approve does on the item now: sign off a landed proof, say yes
// at an approve step after the plan, raise a cap, or answer a question.
func gate(it factory.Item) string {
	switch {
	case it.State == factory.StateLanded:
		return GateProof
	case it.Question == "":
		return ""
	case it.QKind == factory.QKindApprove:
		return GatePlan
	case it.QKind == "cap":
		return GateCap
	}
	return GateQuestion
}

// mapList maps a list, keeping nil as nil and empty as empty, because the
// floor reads some nils as "not read yet".
func mapList[A, B any](in []A, f func(A) B) []B {
	if in == nil {
		return nil
	}
	out := make([]B, len(in))
	for i, a := range in {
		out[i] = f(a)
	}
	return out
}

func fromShift(s factory.Shift) Since {
	return Since{At: s.Since, Shipped: s.Shipped, Arrived: s.Arrived, Asked: s.Asked, Handled: s.Handled, Spent: s.Spent, Hours: s.Hours, Shipping: s.Shipping}
}

func toShift(s Since) factory.Shift {
	return factory.Shift{Since: s.At, Shipped: s.Shipped, Arrived: s.Arrived, Asked: s.Asked, Handled: s.Handled, Spent: s.Spent, Hours: s.Hours, Shipping: s.Shipping}
}

func fromSource(s factory.SourceInfo) Source {
	return Source{Name: s.Name, Writes: s.Writes, Repos: s.Repos, Polled: s.Polled, Trouble: s.Trouble, Polling: s.Polling, Reading: s.Reading, Read: s.Read, Of: s.Of, Items: s.Items}
}

func toSource(s Source) factory.SourceInfo {
	return factory.SourceInfo{Name: s.Name, Writes: s.Writes, Repos: s.Repos, Polled: s.Polled, Trouble: s.Trouble, Polling: s.Polling, Reading: s.Reading, Read: s.Read, Of: s.Of, Items: s.Items}
}

func fromRepos(rs []factory.Repo, checkouts map[string]string) []Repo {
	return mapList(rs, func(r factory.Repo) Repo {
		p := FromRecipe(r.Recipe)
		return Repo{Name: r.Name, Watched: true, Checkout: checkouts[r.Name], Pipeline: &p,
			Team: r.Team, Areas: r.Areas, Habits: r.Habits, Hue: r.Hue, CIRed: r.CIRed}
	})
}

// toRepos reads the repositories back; their checkouts come back through the
// desk's one map, never per repository, so there is one source for them.
func toRepos(rs []Repo) []factory.Repo {
	return mapList(rs, func(r Repo) factory.Repo {
		out := factory.Repo{Name: r.Name, Team: r.Team, Areas: r.Areas, Habits: r.Habits, Hue: r.Hue, CIRed: r.CIRed}
		if r.Pipeline != nil {
			out.Recipe = toRecipe(*r.Pipeline)
		}
		return out
	})
}

// FromRecipe is a recipe as a pipeline, for `GET .../pipeline` as much as for
// the repositories on the queue.
func FromRecipe(r factory.Recipe) Pipeline {
	p := Pipeline{Steps: mapList(r.Stages, fromStage), Policy: r.Policy, Habits: r.Habits}
	if r.ByKind != nil {
		p.ByKind = make(map[string][]StepDef, len(r.ByKind))
		for k, st := range r.ByKind {
			p.ByKind[string(k)] = mapList(st, fromStage)
		}
	}
	if r.Adapt != nil {
		p.Adapt = make(map[string]string, len(r.Adapt))
		for k, m := range r.Adapt {
			p.Adapt[string(k)] = string(m)
		}
	}
	return p
}

func toRecipe(p Pipeline) factory.Recipe {
	r := factory.Recipe{Stages: mapList(p.Steps, toStage), Policy: p.Policy, Habits: p.Habits}
	if p.ByKind != nil {
		r.ByKind = make(map[factory.Kind][]factory.Stage, len(p.ByKind))
		for k, st := range p.ByKind {
			r.ByKind[factory.Kind(k)] = mapList(st, toStage)
		}
	}
	if p.Adapt != nil {
		r.Adapt = make(map[factory.Kind]factory.AdaptMode, len(p.Adapt))
		for k, m := range p.Adapt {
			r.Adapt[factory.Kind(k)] = factory.AdaptMode(m)
		}
	}
	return r
}

func fromStage(s factory.Stage) StepDef {
	return StepDef{Name: s.Name, Kind: string(s.Kind), Ask: s.Ask, Until: s.Until, Max: s.Max, Fixed: s.Fixed,
		Custom: custom(s), Off: !s.On, When: s.When, Effort: s.Effort, Fanout: s.Fanout, Proof: s.Proof, Why: s.Why, By: s.By}
}

func toStage(d StepDef) factory.Stage {
	return factory.Stage{Name: d.Name, Kind: factory.StageKind(d.Kind), Ask: d.Ask, Until: d.Until, Max: d.Max, Fixed: d.Fixed,
		On: !d.Off, When: d.When, Effort: d.Effort, Fanout: d.Fanout, Proof: d.Proof, Why: d.Why, By: d.By}
}

// stockNames are the step names the default pipeline spells for any kind; a
// step named otherwise is a team's own, which the dashboard badges `custom`.
var stockNames = func() map[string]bool {
	r := factory.DefaultRecipe()
	names := map[string]bool{}
	for _, s := range r.Stages {
		names[s.Name] = true
	}
	for _, st := range r.ByKind {
		for _, s := range st {
			names[s.Name] = true
		}
	}
	return names
}()

func custom(s factory.Stage) bool { return !factory.IsApprove(s) && !stockNames[s.Name] }

func fromClaim(c factory.Claim) Claim {
	out := Claim{Text: c.Text, Proven: c.OK}
	if c.Evidence != "" || c.Medium != "" {
		out.Evidence = &Evidence{Kind: c.Medium, Ref: c.Evidence}
	}
	return out
}

func toClaim(c Claim) factory.Claim {
	out := factory.Claim{Text: c.Text, OK: c.Proven}
	if c.Evidence != nil {
		out.Medium, out.Evidence = c.Evidence.Kind, c.Evidence.Ref
	}
	return out
}

func fromDesk(it factory.Item) *JobDesk {
	d := &JobDesk{
		Product: it.Product, Places: it.Places, Body: it.Body, Author: it.Author,
		Assignee: it.Assignee, Starter: it.Starter, Taker: it.Taker,
		Tier: string(it.Tier), Origin: string(it.Origin), Synced: it.Synced, ChangedAt: it.Changed,
		Triage: fromTriage(it.Triage), Cap: it.Cap, Policy: mapList(it.Policy, fromClaim),
		Marked: it.Marked, Labels: it.Labels, Checks: it.Checks, Diff: it.Diff,
		Adapted: it.Adapted, Notes: it.Notes, Talk: it.Talk, Heard: it.Heard,
		Comments: mapList(it.Comments, fromComment), Files: mapList(it.Files, fromFile),
		CheckRuns: mapList(it.CheckRuns, fromCheckRun), Base: it.Base, Head: it.Head, HeadSHA: it.HeadSHA,
		Activity: mapList(it.Activity, fromActivity),
	}
	if st := it.Stream; st != nil {
		d.Run = &Run{Started: st.Started, Ended: st.Ended, Activity: st.Activity, Log: mapList(st.Log, fromLog),
			Paused: st.Paused, Findings: st.Findings, Bench: st.Bench, Room: st.Room}
	}
	return d
}

func fromTriage(t factory.Triage) Triage {
	return Triage{Type: t.Type, Size: t.Size, Area: t.Area, Readiness: t.Readiness, Est: t.Est, DupOf: t.DupOf,
		Risk: t.Risk, Read: t.Read, Questions: t.Questions, Dup: t.Dup, Priority: t.Priority, Reason: t.Reason, TriagedAt: t.TriagedAt}
}

func toTriage(t Triage) factory.Triage {
	return factory.Triage{Type: t.Type, Size: t.Size, Area: t.Area, Readiness: t.Readiness, Est: t.Est, DupOf: t.DupOf,
		Risk: t.Risk, Read: t.Read, Questions: t.Questions, Dup: t.Dup, Priority: t.Priority, Reason: t.Reason, TriagedAt: t.TriagedAt}
}

func fromLog(l factory.LogLine) LogLine {
	return LogLine{At: l.At, Glyph: l.Glyph, Tone: l.Tone, Text: l.Text}
}

func toLog(l LogLine) factory.LogLine {
	return factory.LogLine{At: l.At, Glyph: l.Glyph, Tone: l.Tone, Text: l.Text}
}

func fromComment(c factory.Comment) Comment { return Comment{Author: c.Author, Body: c.Body, At: c.At} }
func toComment(c Comment) factory.Comment {
	return factory.Comment{Author: c.Author, Body: c.Body, At: c.At}
}

func fromFile(f factory.FileChange) File {
	return File{Path: f.Path, Added: f.Added, Removed: f.Removed}
}
func toFile(f File) factory.FileChange {
	return factory.FileChange{Path: f.Path, Added: f.Added, Removed: f.Removed}
}

func fromCheckRun(c factory.CheckRun) CheckRun {
	return CheckRun{Name: c.Name, State: c.State, URL: c.URL}
}
func toCheckRun(c CheckRun) factory.CheckRun {
	return factory.CheckRun{Name: c.Name, State: c.State, URL: c.URL}
}

func fromActivity(e factory.Event) Activity { return Activity{At: e.At, What: e.What} }
func toActivity(a Activity) factory.Event   { return factory.Event{At: a.At, What: a.What} }

// questions are what the job waits on: the floor's own open question, and
// the question a step put to the manager, which the person sees once the
// manager sent it on.
func questions(it factory.Item) []Question {
	var out []Question
	if it.Question != "" {
		q := Question{Text: it.Question, State: "open", Kind: it.QKind}
		if st := it.Stream; st != nil && st.Cur >= 0 && st.Cur < len(st.Phases) {
			q.FromStep = st.Phases[st.Cur].Name
		}
		out = append(out, q)
	}
	if a := it.Asking; a != nil {
		at := a.At
		out = append(out, Question{Text: a.Question, State: "open", Kind: QuestionAsked, FromStep: a.Stage,
			With: a.With, Options: a.Options, Pick: a.Pick, Why: a.Why, At: &at})
	}
	return out
}

func unquestions(it *factory.Item, qs []Question) {
	for _, q := range qs {
		if q.Kind != QuestionAsked {
			it.Question, it.QKind = q.Text, q.Kind
			continue
		}
		a := &factory.Asked{Stage: q.FromStep, Question: q.Text, Options: q.Options, Pick: q.Pick, With: q.With, Why: q.Why}
		if q.At != nil {
			a.At = *q.At
		}
		it.Asking = a
	}
}

// steps lines the item's pipeline up with its run. The two lists are the
// same names in the same order on every item the runner compiled, but an
// edit may add a stage the run has not reached or a run may hold a phase the
// stages since dropped; a phase named nowhere ahead in the pipeline is its own
// step, so neither list loses a row or its order.
func steps(it factory.Item) []Step {
	var phases []factory.Phase
	if it.Stream != nil {
		phases = it.Stream.Phases
	}
	out := make([]Step, 0, len(it.Stages)+len(phases))
	j := 0
	for i, st := range it.Stages {
		for j < len(phases) && phases[j].Name != st.Name && factory.StageIndex(it.Stages[i:], phases[j].Name) < 0 {
			out = append(out, runStep(Step{StepDef: StepDef{Name: phases[j].Name, Kind: string(phases[j].Kind)}}, phases[j]))
			j++
		}
		s := Step{StepDef: fromStage(st), State: StepPending, InPipeline: true}
		if !st.On {
			s.State = StepSkipped
		}
		if j < len(phases) && phases[j].Name == st.Name {
			s = runStep(s, phases[j])
			j++
		}
		out = append(out, s)
	}
	for ; j < len(phases); j++ {
		out = append(out, runStep(Step{StepDef: StepDef{Name: phases[j].Name, Kind: string(phases[j].Kind)}}, phases[j]))
	}
	return out
}

func runStep(s Step, p factory.Phase) Step {
	s.InRun, s.State, s.Round, s.Note, s.Tasks = true, string(p.State), p.Round, p.Note, p.Tasks
	s.LeftMS, s.Chat, s.Handle = p.Left.Milliseconds(), p.Chat, p.Handle
	if string(p.Kind) != s.Kind {
		s.RunKind = string(p.Kind)
	}
	if r := p.Result; r != nil {
		s.Result, s.Cost = r.Output, r.Spent
	}
	return s
}

// unsteps gives each step back to its own list: the pipeline's stages, and
// the run's phases on a stream rebuilt from the job's run block.
func unsteps(j Job) ([]factory.Stage, *factory.Stream) {
	var stages []factory.Stage
	var phases []factory.Phase
	for _, s := range j.Steps {
		if s.InPipeline {
			stages = append(stages, toStage(s.StepDef))
		}
		if s.InRun {
			phases = append(phases, toPhase(s))
		}
	}
	if j.Desk == nil || j.Desk.Run == nil {
		return stages, nil
	}
	r := j.Desk.Run
	st := &factory.Stream{Phases: phases, Spent: j.Cost, Started: r.Started, Ended: r.Ended, Activity: r.Activity,
		Log: mapList(r.Log, toLog), Paused: r.Paused, Findings: r.Findings, Bench: r.Bench, Room: r.Room}
	if j.Step != nil {
		st.Cur = j.Step.Index
	}
	return stages, st
}

func toPhase(s Step) factory.Phase {
	kind := s.Kind
	if s.RunKind != "" {
		kind = s.RunKind
	}
	p := factory.Phase{Name: s.Name, Kind: factory.StageKind(kind), State: factory.PhaseState(s.State), Note: s.Note,
		Round: s.Round, Tasks: s.Tasks, Left: time.Duration(s.LeftMS) * time.Millisecond, Chat: s.Chat, Handle: s.Handle}
	if s.Result != "" || s.Cost != 0 {
		p.Result = &factory.StageResult{Output: s.Result, Spent: s.Cost}
	}
	return p
}

// strip counts the jobs standing in each step, in the order the steps first
// appear, and marks the steps where an approve waits.
func strip(jobs []Job) []Cell {
	var out []Cell
	at := map[string]int{}
	for _, j := range jobs {
		if j.Step == nil || j.Step.Name == "" || (j.State != StateRunning && j.State != StateNeedsYou) {
			continue
		}
		i, ok := at[j.Step.Name]
		if !ok {
			i = len(out)
			at[j.Step.Name] = i
			out = append(out, Cell{Step: j.Step.Name})
		}
		out[i].Jobs++
		out[i].Waiting = out[i].Waiting || j.Gate == GatePlan || j.Gate == GateProof
	}
	return out
}
