package api

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// FromSnapshot is the floor as `GET /v1/queue` says it to me.
func FromSnapshot(s factory.Snapshot, me string, clocks ...time.Time) Queue {
	s.Items = factory.IssueJobs(s.Items)
	now := s.Now
	if len(clocks) > 0 {
		now = clocks[0]
	}
	people := map[string]factory.PersonSummary{}
	jobs := make([]Job, len(s.Items))
	// Only two running jobs can overlap ([factory.Overlaps]), so each running
	// job is compared with the running ones alone. Comparing every job with
	// every other was the queue's one quadratic step: 640,000 comparisons,
	// each copying two whole items, for a floor of 800 open issues.
	var running []int
	for i := range s.Items {
		if factory.JobState(s.Items[i]) == "running" {
			running = append(running, i)
		}
	}
	for i, it := range s.Items {
		for _, handle := range factory.MentionedHandles(it) {
			if handle = factory.NormalizeHandle(handle); handle != "" {
				people[handle] = s.People[handle]
			}
		}
		jobs[i] = FromItem(it)
		jobs[i].Display = DisplayFor(jobs[i], now)
		jobs[i].Can = CanFor(it, me, now)
		ForViewer(&jobs[i], it, me)
		if factory.JobState(it) == "running" {
			for _, k := range running {
				if factory.Overlaps(it, s.Items[k]) {
					jobs[i].Overlap = append(jobs[i].Overlap, factory.JobRef(s.Items[k]))
				}
			}
		}
	}
	return Queue{
		People:   people,
		Since:    fromShift(s.Shift),
		Jobs:     jobs,
		Repos:    fromRepos(s.Repos, s.Checkouts),
		Strip:    strip(jobs),
		Capacity: Capacity{Benches: s.Benches, Daily: s.Daily, Rail: s.Rail, Queued: s.Count(factory.StateQueued)},
		Me:       me,
		NeedsYou: groupNeedsYou(jobs),
		Desk: &QueueDesk{
			Now: s.Now, Sources: mapList(s.Sources, fromSource), Busy: s.Busy, BusyAll: s.BusyAll,
			LastReadCost: s.LastReadCost, Marked: s.Marked, Checkouts: s.Checkouts,
		},
	}
}

// groupNeedsYou buckets waiting jobs by kind, in the fixed enum order.
// A kind with no job is omitted, and the job ids stay in floor order.
func groupNeedsYou(jobs []Job) []NeedsYouGroup {
	buckets := map[string][]int{}
	for _, j := range jobs {
		if j.NeedsYou == nil || j.NeedsYou.Kind == "" {
			continue
		}
		buckets[j.NeedsYou.Kind] = append(buckets[j.NeedsYou.Kind], j.ID)
	}
	var out []NeedsYouGroup
	for _, kind := range factory.NeedsYouKinds {
		if ids := buckets[kind]; len(ids) > 0 {
			out = append(out, NeedsYouGroup{Kind: kind, Jobs: ids})
		}
	}
	return out
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
		People: q.People,
		Now:    d.Now, Repos: toRepos(q.Repos), Items: mapList(jobs, ToItem),
		Benches: q.Capacity.Benches, Daily: q.Capacity.Daily, Rail: q.Capacity.Rail,
		Shift: toShift(q.Since), Sources: mapList(d.Sources, toSource),
		Busy: d.Busy, BusyAll: d.BusyAll, LastReadCost: d.LastReadCost,
		Marked: d.Marked, Checkouts: d.Checkouts,
	}
}

// Lost names the Go fields the wire does not carry, and why. The round-trip
// test reads it, so a field is either carried or named here.
var Lost = map[string]string{
	"Snapshot.People":              "the queue projects identities onto mentioned handles only; unrelated cached profiles do not cross",
	"Item.PlannedFiles":            "the cache never crosses: the wire's planned_files is derived from the plan step's result (factory.PlanFilesOf), which does",
	"Item.OldGate":                 "read into approve steps on load and never in memory",
	"Stage.OldGate":                "read into approve steps on load and never in memory",
	"Stage.OldGateWhen":            "read into approve steps on load and never in memory",
	"Phase.Carry":                  "the runner's own inbox for a resumed step; no surface reads it",
	"Phase.Result":                 "a reader's job carries Output (step result), Spent (step cost), Tests and Model; the rest is the runner's revival state, which only the worker's pull carries (WorkerJob)",
	"Phase.Kind":                   "an empty phase kind under a stage with a kind comes back as the stage's",
	"Phase.Left":                   "crosses in whole milliseconds",
	"Item.ChatSpent":               "crosses as the job's cost while it has no run; a start carries it into the run's spend before any worker pulls the job",
	"CheckRun.AppID":               "server-side readiness matching only; every poll reads it from GitHub again before a required check is matched",
	"PullRequest.ReadinessContext": "server-side review material up to 384 KiB; a queued final review carries it to the worker as the follow-up's details",
}

// FromItem is one item as a job.
func FromItem(it factory.Item) Job {
	j := Job{
		Recovery: it.Recovery, Autonomy: it.Autonomy, AutonomyWhy: factory.AutonomyBlockReason(it), FollowUp: it.FollowUp, Manual: it.Manual, Effort: it.Effort,
		SetupHeld: it.SetupHeld,
		Owners:    append([]string(nil), it.Owners...),
		ID:        it.ID, Repo: it.Repo, Num: it.Num, Kind: string(it.Kind), Title: it.Title, URL: it.URL,
		HeadCommit: it.HeadCommit, Publishing: it.PublishStatus, Change: it.Change, Publish: it.Publish, CommentURL: it.CommentURL,
		Runner: it.WorktreeMachine, JobID: factory.JobRef(it), Ref: factory.JobRef(it), Priority: factory.PriorityFor(it), Size: factory.JobSize(it), Budget: it.Cap, Model: it.Model,
		Reason: factory.JobReason(it), DoneHow: factory.JobDoneHow(it), DoneAt: it.DoneAt,
		DismissedBy: append([]string(nil), it.DismissedBy...), Approval: factory.JobApproval(it),
		State: factory.JobState(it), Ready: it.State == factory.StateLanded, Gate: gate(it),
		AssignedTo: it.Person(), Assignee: it.Person(),
		AskedFirstReason: it.AskedFirstReason,
		Steps:            steps(it),
		Claims:           mapList(it.Proof, fromClaim),
		PlannedFiles:     factory.PlanFilesOf(it),
		Questions:        questions(it),
		CreatedAt:        it.Created,
		Desk:             fromDesk(it),
	}
	// Preserve a stored true flag for future job kinds as well. A false flag
	// is known only for a PR; other jobs leave the field absent.
	if it.Kind == factory.KindPR || it.Draft {
		draft := it.Draft
		j.Draft = &draft
	}
	j.ReadSize = readSize(it)
	j.PR = fromPR(it.PullRequest)
	j.Merge = jobMerge(it)
	if j.State == "" {
		j.State = string(it.State)
	}
	if a := it.Asking; a != nil && a.With == factory.AskedYou {
		at := a.At
		j.WaitingSince = &at
	}
	// THE JOB'S COST IS EVERYTHING DONE FOR IT: its run's, or before any run
	// what its chat spent ([factory.JobSpent]).
	j.Cost = factory.JobSpent(it)
	if st := it.Stream; st != nil {
		j.Step = &StepAt{Index: st.Cur, Count: len(st.Phases)}
		if st.Cur >= 0 && st.Cur < len(st.Phases) {
			j.Step.Name = st.Phases[st.Cur].Name
		}
	}
	j.NeedsYou = needsYou(it)
	j.SpendingLimit = spendingLimit(it)
	j.Failure = failureOf(it)
	j.Ask = jobAsk(it)
	j.PublishError = it.PublishError
	PresentJob(&j)
	if it.State == factory.StateLanded {
		j.PublishPreview = factory.PublishBody(it, "")
	}

	j.FollowPR = factory.IsIssueJob(it) && it.DoneHow != "dropped"

	return j
}

func needsYou(it factory.Item) *NeedsYou {
	kind := factory.NeedsKind(it)
	if kind == "" {
		return nil
	}
	ny := &NeedsYou{Kind: kind, Since: factory.NeedsSince(it), WhoCanAnswer: factory.WhoCanAnswer(kind)}
	if kind == factory.NeedsQuestion || kind == factory.NeedsOutOfAttempts || kind == factory.NeedsRunnerOffline {
		ny.Question = needsQuestion(it)
	}
	switch kind {
	case factory.NeedsApprovePlan:
		ny.BackTo, _ = factory.PlanSendsBack(it)
	case factory.NeedsOutOfAttempts:
		ny.BackTo = stuckBackTo(it)
	}
	return ny
}

// stuckBackTo is, on a step that stopped short, the finished step before it
// that could take the work back ([factory.PreviousWork]), "" when there is none:
// the step a person's "send it back" reruns.
func stuckBackTo(it factory.Item) string {
	if it.Stream == nil || it.Stream.Cur < 0 || it.Stream.Cur >= len(it.Stream.Phases) {
		return ""
	}
	if prev := factory.PreviousWork(it.Stream.Phases, it.Stream.Cur); prev >= 0 {
		return it.Stream.Phases[prev].Name
	}
	return ""
}

func needsQuestion(it factory.Item) *NeedsQuestion {
	form, options, who := factory.QuestionForm(it)
	q := &NeedsQuestion{
		ID: itQuestionID(it.ID), Kind: form, Text: it.Question, Options: options, WhoCanAnswer: who,
	}
	if it.Question == "" && it.Asking != nil {
		q.Text = it.Asking.Question
	}
	if it.Asking != nil && it.Asking.Stage != "" {
		q.FromStep = it.Asking.Stage
	} else if st := it.Stream; st != nil && st.Cur >= 0 && st.Cur < len(st.Phases) {
		q.FromStep = st.Phases[st.Cur].Name
	}
	if asks := it.Person(); asks != "" {
		q.Asks = asks
	}
	return q
}

func itQuestionID(id int) string { return fmt.Sprintf("q%d", id) }

func spendingLimit(it factory.Item) *SpendingLimit {
	if factory.NeedsKind(it) != factory.NeedsSpendingLimit {
		return nil
	}
	spent := 0.0
	if it.Stream != nil {
		spent = it.Stream.Spent
	}
	return &SpendingLimit{Limit: it.Cap, Spent: spent, NextLimit: factory.NextJobCap(it.Cap), Currency: factory.CurrencyUSD}
}

func failureOf(it factory.Item) *Failure {
	if it.Failure == nil {
		return nil
	}
	return failureWire(*it.Failure)
}

func failureWire(f factory.Failure) *Failure {
	return &Failure{Rounds: f.Attempts, Runner: f.Machine, Step: f.Step, Code: f.Code, Detail: f.Detail, Attempts: f.Attempts, Machine: f.Machine, Next: f.Next, At: f.At}
}

func failureItem(f *Failure) *factory.Failure {
	if f == nil {
		return nil
	}
	return &factory.Failure{Step: f.Step, Code: f.Code, Detail: f.Detail, Attempts: f.Attempts, Machine: f.Machine, Next: f.Next, At: f.At}
}

// CanFor is MayAct in the wire's field names.
func CanFor(it factory.Item, caller string, clocks ...time.Time) Can {
	now := time.Now()
	if len(clocks) > 0 && !clocks[0].IsZero() {
		now = clocks[0]
	}
	a := factory.MayAct(it, caller)
	person := it.Person()
	owner := person == "" || person == factory.NormalizeHandle(caller)
	approval := factory.JobApproval(it)
	approve := approval != nil && approval.State == "waiting" && person != "" && person == factory.NormalizeHandle(caller)
	state := factory.JobState(it)
	// A RUN THAT ALREADY ENDED HAS NOTHING TO STOP, so a failed or stopped
	// job offers no Stop beside its Run again ([factory.UnderWay]).
	active := factory.UnderWay(it)
	return Can{
		Run: owner && state == StateBacklog, StartNow: owner && state == StateBacklog,
		Stop: owner && active, Dismiss: factory.NormalizeHandle(caller) != "", Drop: owner && state != StateDone,
		ChangeAssignee: person != "" && factory.NormalizeHandle(caller) == person, Unassign: person != "" && factory.NormalizeHandle(caller) == person, Nudge: person != "", RaiseBudget: owner,
		Undo:    (it.Approval != nil && it.Approval.State == "approving" && now.Before(it.Approval.UndoUntil) && it.Approval.DecidedBy == factory.NormalizeHandle(caller)) || (it.StopUndo != nil && now.Before(it.StopUndo.Until) && it.StopUndo.By == factory.NormalizeHandle(caller)) || (it.Merging != nil && it.Merging.State == factory.MergeAsked && now.Before(it.Merging.UndoUntil) && it.Merging.By == factory.NormalizeHandle(caller)),
		Approve: approve, RunAgain: a.RunAgain, CancelJob: a.CancelJob,
		RetryPublish: a.RetryPublish, Answer: a.Answer, AssignToMe: a.AssignToMe, Reply: a.Reply,
		RejectPlan: a.Answer && planSendsBack(it),
		Merge:      owner && factory.NormalizeHandle(caller) != "" && factory.MergeGate(it, factory.MergeAsk{By: caller}).OK,
		Restore:    owner && factory.JobDoneHow(it) == "dropped",
	}
}

// planSendsBack is factory.PlanSendsBack without the step's name.
func planSendsBack(it factory.Item) bool {
	_, ok := factory.PlanSendsBack(it)
	return ok
}

// jobMerge is the job's merging on the wire, absent until the job has a pull
// request of its own or a pinned policy to show.
func jobMerge(it factory.Item) *JobMerge {
	if it.Kind == factory.KindPR || (it.PullRequest == nil && it.MergePolicy == nil && it.Merging == nil) {
		return nil
	}
	out := &JobMerge{}
	if p := it.MergePolicy; p != nil {
		out.Auto, out.Method = p.Auto, p.Method
	}
	if m := it.Merging; m != nil {
		run := &JobMergeRun{State: m.State, By: m.By, Auto: m.Auto, Rule: m.Rule, Why: m.Why, Commit: m.Commit, Method: m.Method}
		if !m.At.IsZero() {
			at := m.At
			run.At = &at
		}
		if !m.UndoUntil.IsZero() {
			until := m.UndoUntil
			run.UndoUntil = &until
		}
		out.Run = run
	}
	if m := it.Merging; m == nil || m.State == factory.MergeFailed {
		// Blocked is the gate's answer for a person: the same rule the Merge
		// verb applies, so a disabled button and a refused verb agree.
		if v := factory.MergeGate(it, factory.MergeAsk{By: "you"}); !v.OK {
			out.Blocked = v.Why
		}
	}
	return out
}

// unmerge reads a job's merging back: the pinned policy and the merge on
// its way, which a worker needs to carry the merge out.
func unmerge(m *JobMerge) (*factory.MergePolicy, *factory.MergeRun) {
	if m == nil {
		return nil, nil
	}
	var policy *factory.MergePolicy
	if m.Auto || m.Method != "" {
		policy = &factory.MergePolicy{Auto: m.Auto, Method: m.Method}
	}
	r := m.Run
	if r == nil {
		return policy, nil
	}
	run := &factory.MergeRun{State: r.State, By: r.By, Auto: r.Auto, Rule: r.Rule, Why: r.Why, Commit: r.Commit, Method: r.Method}
	if r.At != nil {
		run.At = *r.At
	}
	if r.UndoUntil != nil {
		run.UndoUntil = *r.UndoUntil
	}
	return policy, run
}

// jobAsk prefers the stored ask. A GitHub issue with no stored ask is derived
// and is not written back, so a round trip cannot invent a second copy.
func jobAsk(it factory.Item) *JobAsk {
	if it.Ask != nil {
		a := &JobAsk{Text: it.Ask.Text, By: it.Ask.By, Source: it.Ask.Source, FromRef: it.Ask.FromRef}
		if !it.Ask.At.IsZero() {
			at := it.Ask.At
			a.At = &at
		}
		return a
	}
	if !factory.GitHubIssue(it) {
		return nil
	}
	return &JobAsk{Text: it.Body, Issue: &AskIssue{Number: it.Num, URL: it.URL, Title: it.Title}}
}

// readSize is the size to show when no label sets one. A label wins and leaves
// this absent, so job.size and job.read_size are never both present.
func readSize(it factory.Item) *JobReadSize {
	if factory.JobSize(it) != "" {
		return nil
	}
	letter, from := factory.ShownSize(it)
	if letter == "" || (from != factory.ShownRead && from != factory.ShownDiff) {
		return nil
	}
	return &JobReadSize{Value: letter, From: from}
}

// ToItem is one job as an item.
func ToItem(j Job) factory.Item {
	d := j.Desk
	if d == nil {
		d = &JobDesk{}
	}
	it := factory.Item{
		Recovery: j.Recovery, Autonomy: j.Autonomy, FollowUp: j.FollowUp, Manual: j.Manual, Effort: j.Effort,
		SetupHeld: j.SetupHeld,
		Owners:    append([]string(nil), j.Owners...),
		ID:        j.ID, Repo: j.Repo, Num: j.Num, Kind: factory.Kind(j.Kind), Title: j.Title, URL: j.URL, Model: j.Model,
		HeadCommit: j.HeadCommit, PublishStatus: j.Publishing, Change: j.Change, ChangeBase: d.ChangeBase, Publish: j.Publish, CommentURL: j.CommentURL,
		WorktreeMachine: d.WorktreeMachine, Publishing: d.Publishing,
		Priority: d.Priority, DoneHow: d.DoneHow, Approval: d.Approval, StopUndo: d.StopUndo,
		DismissedBy: j.DismissedBy, DoneAt: j.DoneAt,
		State: stateIn(j), Created: j.CreatedAt,
		Product: d.Product, Places: d.Places, Body: d.Body, Author: d.Author,
		Assignee: d.Assignee, Starter: d.Starter, Taker: d.Taker,
		SourceAssignees: slices.Clone(d.Assignees),
		Tier:            factory.Tier(d.Tier), Origin: factory.Origin(d.Origin), Synced: d.Synced, Changed: d.ChangedAt,
		Triage: toTriage(d.Triage), Cap: d.Cap,
		Proof: mapList(j.Claims, toClaim), Policy: mapList(d.Policy, toClaim),
		Marked: d.Marked, Labels: d.Labels, Checks: d.Checks, Diff: d.Diff,
		Adapted: d.Adapted, Notes: d.Notes, Talk: d.Talk, Heard: d.Heard, Thread: d.Thread,
		Comments: mapList(d.Comments, toComment), Files: mapList(d.Files, toFile),
		CheckRuns: mapList(d.CheckRuns, toCheckRun), Base: d.Base, Head: d.Head, HeadSHA: d.HeadSHA,
		Activity: mapList(d.Activity, toActivity),
	}
	it.PullRequest = toPR(j.PR)
	it.MergePolicy, it.Merging = unmerge(j.Merge)
	if j.Draft != nil {
		it.Draft = *j.Draft
	}
	it.AskedFirstReason = j.AskedFirstReason
	it.PublishError = j.PublishError
	if a := j.Ask; a != nil && a.Issue == nil {
		it.Ask = &factory.JobAsk{Text: a.Text, By: a.By, Source: a.Source, FromRef: a.FromRef}
		if a.At != nil {
			it.Ask.At = *a.At
		}
	}
	if f := j.Failure; f != nil {
		it.Failure = failureItem(f)
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
	if j.Desk != nil && j.Desk.EngineState != "" {
		return factory.State(j.Desk.EngineState)
	}
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
	if it.State == factory.StateLanded && it.PublishStatus != nil && it.PublishStatus.State == "failed" {
		return "publish_failed"
	}
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
	return Since{At: s.Since, Shipped: s.Shipped, Arrived: s.Arrived, Asked: s.Asked, Handled: s.Handled, Spent: s.Spent, SpentToday: s.SpentToday, MergedToday: s.MergedToday, MergedWeek: s.MergedWeek, Hours: s.Hours, Shipping: s.Shipping}
}

func toShift(s Since) factory.Shift {
	return factory.Shift{Since: s.At, Shipped: s.Shipped, Arrived: s.Arrived, Asked: s.Asked, Handled: s.Handled, Spent: s.Spent, SpentToday: s.SpentToday, MergedToday: s.MergedToday, MergedWeek: s.MergedWeek, Hours: s.Hours, Shipping: s.Shipping}
}

// fromPR is a published pull request on the wire. Nil stays nil so a comment
// receipt is JSON null, which is the field a client already reads.
func fromPR(pr *factory.PullRequest) *PR {
	if pr == nil {
		return nil
	}
	out := &PR{
		Problems: append([]factory.PRReadProblem(nil), pr.Problems...), BaseSHA: pr.BaseSHA, Feedback: pr.Feedback, CheckRuns: mapList(pr.CheckRuns, fromCheckRun),
		ReadinessKey: pr.ReadinessKey, Conflict: pr.Conflict, Draft: pr.Draft, Number: pr.Number, URL: pr.URL, CI: pr.CI, State: pr.State,
		MergedBy: pr.MergedBy, ReviewDecision: pr.ReviewDecision, RequiredReviews: pr.Required,
		Reviews: mapList(pr.Reviews, func(r factory.PRReview) PRReview {
			return PRReview{By: r.By, State: r.State, At: r.At, Body: r.Body, URL: r.URL}
		}),
		HeadSHA: pr.HeadSHA, Mergeable: pr.Mergeable, ChecksRead: pr.ChecksRead, ReviewsRead: pr.ReviewsRead,
	}
	if !pr.MergedAt.IsZero() {
		at := pr.MergedAt
		out.MergedAt = &at
	}
	if pr.Checks != (factory.PRChecks{}) {
		c := pr.Checks
		out.Checks = &PRChecks{Passed: c.Passed, Failed: c.Failed, Pending: c.Pending}
	}
	return out
}

func toPR(pr *PR) *factory.PullRequest {
	if pr == nil {
		return nil
	}
	out := &factory.PullRequest{
		Problems: append([]factory.PRReadProblem(nil), pr.Problems...), BaseSHA: pr.BaseSHA, Feedback: pr.Feedback, CheckRuns: mapList(pr.CheckRuns, toCheckRun),
		ReadinessKey: pr.ReadinessKey, Conflict: pr.Conflict, Draft: pr.Draft, Number: pr.Number, URL: pr.URL, CI: pr.CI, State: pr.State,
		MergedBy: pr.MergedBy, ReviewDecision: pr.ReviewDecision, Required: pr.RequiredReviews,
		Reviews: mapList(pr.Reviews, func(r PRReview) factory.PRReview {
			return factory.PRReview{By: r.By, State: r.State, At: r.At, Body: r.Body, URL: r.URL}
		}),
		HeadSHA: pr.HeadSHA, Mergeable: pr.Mergeable, ChecksRead: pr.ChecksRead, ReviewsRead: pr.ReviewsRead,
	}
	if pr.MergedAt != nil {
		out.MergedAt = *pr.MergedAt
	}
	if pr.Checks != nil {
		out.Checks = factory.PRChecks{Passed: pr.Checks.Passed, Failed: pr.Checks.Failed, Pending: pr.Checks.Pending}
	}
	return out
}

func fromSource(s factory.SourceInfo) Source {
	return Source{Name: s.Name, Writes: s.Writes, Repos: s.Repos, Polled: s.Polled, Trouble: s.Trouble, Polling: s.Polling, Reading: s.Reading, Read: s.Read, Of: s.Of, Items: s.Items}
}

func toSource(s Source) factory.SourceInfo {
	return factory.SourceInfo{Name: s.Name, Writes: s.Writes, Repos: s.Repos, Polled: s.Polled, Trouble: s.Trouble, Polling: s.Polling, Reading: s.Reading, Read: s.Read, Of: s.Of, Items: s.Items}
}

// fromRepos carries each repository's watched fact as the floor stamped it
// ([factory.Repo.Watched]), never a constant: a queue lists only the
// repositories that are on, and a floor that keeps no list watches none.
func fromRepos(rs []factory.Repo, checkouts map[string]string) []Repo {
	return mapList(rs, func(r factory.Repo) Repo {
		p := FromRecipe(r.Recipe)
		return Repo{Name: r.Name, Watched: r.Watched, Checkout: checkouts[r.Name], CheckedOut: checkouts[r.Name] != "", Pipeline: &p,
			Team: r.Team, Areas: r.Areas, Habits: r.Habits, Hue: r.Hue, CIRed: r.CIRed}
	})
}

// toRepos reads the repositories back; their checkouts come back through the
// desk's one map, never per repository, so there is one source for them.
func toRepos(rs []Repo) []factory.Repo {
	return mapList(rs, func(r Repo) factory.Repo {
		out := factory.Repo{Name: r.Name, Watched: r.Watched, Team: r.Team, Areas: r.Areas, Habits: r.Habits, Hue: r.Hue, CIRed: r.CIRed}
		if r.Pipeline != nil {
			out.Recipe = toRecipe(*r.Pipeline)
		}
		return out
	})
}

// FromRecipe is a recipe as a pipeline, for `GET .../pipeline` as much as for
// the repositories on the queue.
func FromRecipe(r factory.Recipe) Pipeline {
	p := Pipeline{DefaultBudget: r.DefaultBudget, Source: r.Source, Steps: mapList(r.Stages, fromStage), Policy: r.Policy, Habits: r.Habits}
	for _, problem := range r.Problems {
		p.Problems = append(p.Problems, problem)
	}
	if r.ByKind != nil {
		p.ByKind = make(map[string][]StepDef, len(r.ByKind))
		for k, st := range r.ByKind {
			if k == factory.KindPR || k == factory.KindCI {
				continue
			}
			p.ByKind[string(k)] = mapList(st, fromStage)
		}
	}
	if r.Adapt != nil {
		p.Adapt = make(map[string]string, len(r.Adapt))
		for k, m := range r.Adapt {
			p.Adapt[string(k)] = string(m)
		}
	}
	if r.Merge != (factory.MergePolicy{}) {
		p.Merge = &MergeBlock{Auto: r.Merge.Auto, Method: r.Merge.Method}
	}
	return p
}

func toRecipe(p Pipeline) factory.Recipe {
	r := factory.Recipe{DefaultBudget: p.DefaultBudget, Source: p.Source, Stages: mapList(p.Steps, toStage), Policy: p.Policy, Habits: p.Habits}
	for _, problem := range p.Problems {
		r.Problems = append(r.Problems, problem)
	}
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
	if m := p.Merge; m != nil {
		r.Merge = factory.MergePolicy{Auto: m.Auto, Method: m.Method}
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
		EngineState: string(it.State), Priority: it.Priority, DoneHow: it.DoneHow, Approval: it.Approval, StopUndo: it.StopUndo,
		ChangeBase: it.ChangeBase, WorktreeMachine: it.WorktreeMachine, Publishing: it.Publishing,
		Product: it.Product, Places: it.Places, Body: it.Body, Author: it.Author,
		Assignee: it.Assignee, Starter: it.Starter, Taker: it.Taker,
		Assignees: slices.Clone(it.SourceAssignees),
		Tier:      string(it.Tier), Origin: string(it.Origin), Synced: it.Synced, ChangedAt: it.Changed,
		Triage: fromTriage(it.Triage), Cap: it.Cap, Policy: mapList(it.Policy, fromClaim),
		Marked: it.Marked, Labels: it.Labels, Checks: it.Checks, Diff: it.Diff,
		Adapted: it.Adapted, Notes: it.Notes, Talk: it.Talk, Thread: it.Thread, Heard: it.Heard,
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
	return Triage{Type: t.Type, Size: t.Size, SizeFrom: t.SizeFrom, Area: t.Area, Readiness: t.Readiness, Est: t.Est, DupOf: t.DupOf,
		Risk: t.Risk, Read: t.Read, Questions: t.Questions, Dup: t.Dup, Priority: t.Priority, Reason: t.Reason, TriagedAt: t.TriagedAt, StartJudged: t.StartJudged, StartMatched: t.StartMatched, StartPaths: t.StartPaths, StartRules: t.StartRules}
}

func toTriage(t Triage) factory.Triage {
	return factory.Triage{Type: t.Type, Size: t.Size, SizeFrom: t.SizeFrom, Area: t.Area, Readiness: t.Readiness, Est: t.Est, DupOf: t.DupOf,
		Risk: t.Risk, Read: t.Read, Questions: t.Questions, Dup: t.Dup, Priority: t.Priority, Reason: t.Reason, TriagedAt: t.TriagedAt, StartJudged: t.StartJudged, StartMatched: t.StartMatched, StartPaths: t.StartPaths, StartRules: t.StartRules}
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
		q := Question{Text: it.Question, State: "open", Kind: questionKindOut(it.QKind, it.Question)}
		if st := it.Stream; st != nil && st.Cur >= 0 && st.Cur < len(st.Phases) {
			q.FromStep = st.Phases[st.Cur].Name
		}
		annotateQuestion(&q, it)
		out = append(out, q)
	}
	if a := it.Asking; a != nil {
		at := a.At
		q := Question{Text: a.Question, State: "open", Kind: QuestionAsked, FromStep: a.Stage,
			With: a.With, Options: append([]string(nil), a.Options...), Pick: a.Pick, Why: a.Why, At: &at}
		// A reported ask holds the same words in Question and Asking. Keep
		// its gate kind and its answer details together on one wire entry.
		if a.With == factory.AskedYou && it.Question != "" && it.Question == a.Question {
			q.Kind = questionKindOut(it.QKind, it.Question)
			annotateQuestion(&q, it)
			out[0] = q
		} else {
			out = append(out, q)
		}
	}
	return append(out, it.AnsweredQuestions...)
}

// questionKindOut is the kind a person reads. The store keeps the runner's
// words (approve, scope); the wire says plan and max_rounds. Anything else,
// including proof and step, is already the wire's word.
func questionKindOut(kind, text string) string {
	switch kind {
	case factory.QKindApprove:
		return "plan"
	case "scope":
		if strings.Contains(text, "one more round") {
			return "max_rounds"
		}
	}
	return kind
}

// questionKindIn is the store's word for a kind the wire showed, so a job
// that left the factory and came back still has the runner's kind.
func questionKindIn(kind string) string {
	switch kind {
	case "plan":
		return factory.QKindApprove
	case "max_rounds":
		return "scope"
	default:
		return kind
	}
}

// annotateQuestion adds the numbers and choices a cap or rounds question
// carries. Choices the step did not offer are filled only when nothing is
// stored beside the question, so a round trip does not invent an answer list.
func annotateQuestion(q *Question, it factory.Item) {
	if q.Kind == "cap" {
		q.Limit = it.Cap
		if it.Stream != nil {
			q.Spent = it.Stream.Spent
		}
		if it.Cap > 0 {
			q.NextLimit = factory.NextJobCap(it.Cap)
		}
	}
	if it.QKind == factory.QKindOutOfAttempts && len(q.Options) == 0 {
		q.Options = append([]string(nil), factory.OutOfAttemptOptions...)
	}
	if len(q.Options) == 0 && it.Asking == nil && (q.Kind == "cap" || q.Kind == "max_rounds") {
		q.Options = []string{"yes", "no"}
	}
}

func unquestions(it *factory.Item, qs []Question) {
	for _, q := range qs {
		if q.Answer != nil {
			it.AnsweredQuestions = append(it.AnsweredQuestions, q)
			continue
		}
		if q.Kind != QuestionAsked {
			it.Question, it.QKind = q.Text, questionKindIn(q.Kind)
			if q.With != factory.AskedYou {
				continue
			}
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
		s.Result, s.Cost, s.Tests, s.Model = r.Output, r.Spent, r.Tests, r.Model
	}
	s.At, s.StartedAt = timeOrNil(p.At), timeOrNil(p.Started)
	s.Failure = failureWireOrNil(p.Failure)
	s.Rounds = roundsWire(p.Rounds)
	return s
}

// timeOrNil is an instant on the wire: absent when it is unknown.
func timeOrNil(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// roundsWire is a step's finished tries in the wire's words, nil for none.
// [roundsItem] reads them back.
func roundsWire(lines []factory.RoundLine) []RoundLine {
	if len(lines) == 0 {
		return nil
	}
	out := make([]RoundLine, len(lines))
	for i, line := range lines {
		out[i] = RoundLine{Rejected: line.Rejected, Round: line.Round, Summary: line.Summary, At: line.At, StartedAt: line.Started, Chat: line.Chat}
	}
	return out
}

// roundsItem is the wire's finished tries as the item keeps them.
func roundsItem(lines []RoundLine) []factory.RoundLine {
	if len(lines) == 0 {
		return nil
	}
	out := make([]factory.RoundLine, len(lines))
	for i, line := range lines {
		out[i] = factory.RoundLine{Rejected: line.Rejected, Round: line.Round, Summary: line.Summary, At: line.At, Started: line.StartedAt, Chat: line.Chat}
	}
	return out
}

func failureWireOrNil(f *factory.Failure) *Failure {
	if f == nil {
		return nil
	}
	return failureWire(*f)
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
	// THE WHOLE REPORT WINS WHEN IT CAME. A worker's pull carries it; a
	// reader's job, and a pull from an older factory, carry only the step's
	// result line, which is read as it always was (no done, no notes, no claims).
	if r := s.Report; r != nil {
		res := stageResultItem(*r)
		res.Model = s.Model
		p.Result = &res
	} else if s.Result != "" || s.Cost != 0 || s.Tests != nil || s.Model != "" {
		p.Result = &factory.StageResult{Output: s.Result, Spent: s.Cost, Tests: s.Tests, Model: s.Model}
	}
	if s.At != nil {
		p.At = *s.At
	}
	if s.StartedAt != nil {
		p.Started = *s.StartedAt
	}
	p.Failure = failureItem(s.Failure)
	p.Rounds = roundsItem(s.Rounds)
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
