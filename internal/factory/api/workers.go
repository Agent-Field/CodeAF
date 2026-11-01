package api

import (
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// LiveCapacityHeader carries the live-capacity attestation on attach. A factory
// that predates it decodes the body strictly and would refuse an unknown JSON
// field with 422, but it never looks at headers, so the attestation travels
// here and an older factory simply does not hear it.
const LiveCapacityHeader = "X-Codeaf-Live-Capacity"

// LiveCapacityHeaderValue is the only value that attests; anything else is silence.
const LiveCapacityHeaderValue = "1"

// StepModelsHeader carries execution models beside a report, so older strict
// factories still receive its results and costs without an unknown JSON field.
const StepModelsHeader = "X-Codeaf-Step-Models"

// StepChatsHeader publishes live chat identities without breaking older strict servers.
const StepChatsHeader = "X-Codeaf-Step-Chats"

// WorkerAttach names the machine and its person; consent to unassigned work
// is explicit and applies only to each named repository.
type WorkerAttach struct {
	Runner string `json:"runner,omitempty"`
	Owner  string `json:"owner,omitempty"`
	Kind   string `json:"kind,omitempty"`
	Slots  int    `json:"slots,omitempty"`
	// LiveCapacity attests that this worker re-reads its saved capacity and
	// changes its running slots to match. WorkerClient.Attach never writes it
	// into the body (see LiveCapacityHeader); the server still reads it from
	// the body for direct callers. Older workers never attest.
	LiveCapacity    bool            `json:"live_capacity,omitempty"`
	Checkouts       []Checkout      `json:"checkouts,omitempty"`
	Machine         string          `json:"machine,omitempty"`
	Name            string          `json:"name"`
	Handle          string          `json:"handle"`
	OS              string          `json:"os,omitempty"`
	CodeafVersion   string          `json:"codeaf_version,omitempty"`
	TakesUnassigned map[string]bool `json:"takes_unassigned,omitempty"`
}

// WorkerAttached returns the credential only at registration, so reads of
// the floor can never expose another machine's bearer secret.
type WorkerAttached struct {
	Factory    Factory `json:"factory"`
	WorkerID   string  `json:"worker_id"`
	RunnerID   string  `json:"runner_id"`
	Credential string  `json:"credential"`
	// Slots is the capacity the owner saved for this runner, which wins over
	// what the worker asked for. Older servers omit it.
	Slots int `json:"slots,omitempty"`
}

// WorkerReport carries incremental results. Cost is newly incurred dollars,
// so a result's own cost is descriptive and is not added a second time.
type WorkerReport struct {
	FollowUp   *factory.PRFollowUp `json:"follow_up,omitempty"`
	Change     *factory.Change     `json:"change,omitempty"`
	ChangeBase string              `json:"change_base,omitempty"`
	HeadCommit string              `json:"head_commit,omitempty"`
	Publishing *factory.Publishing `json:"publishing,omitempty"`
	Thread     []Message           `json:"thread,omitempty"`
	Delivered  []string            `json:"delivered,omitempty"`
	// Pipeline is the job's steps after the worker's runner reshaped them;
	// absent is no change. Only an approve step's presence is checked.
	Pipeline   []Step       `json:"pipeline,omitempty"`
	Steps      []StepReport `json:"steps,omitempty"`
	Log        []LogLine    `json:"log,omitempty"`
	Cost       float64      `json:"cost,omitempty"`
	Claims     []Claim      `json:"claims,omitempty"`
	Question   *Question    `json:"question,omitempty"`
	PR         *PR          `json:"pr,omitempty"`
	Publish    string       `json:"publish,omitempty"`
	CommentURL string       `json:"comment,omitempty"`
	Talk       string       `json:"talk,omitempty"`  // desk: the manager conversation the worker made
	Notes      []string     `json:"notes,omitempty"` // desk: sentences the run added to the job's notes
	Room       string       `json:"room,omitempty"`  // desk: the item team's id (Stream.Room), write-once
	// Merge is what the runner's merge did: merging, merged or failed.
	Merge *WorkerMerge `json:"merge,omitempty"`
}

// WorkerMerge is [factory.MergeReport] on the wire.
type WorkerMerge struct {
	State string `json:"state"`
	SHA   string `json:"sha,omitempty"`
	Why   string `json:"why,omitempty"`
	PR    *PR    `json:"pr,omitempty"`
}

// StepReport carries the last round's full result without exposing Go names.
// rounds is every finished try. failure is set when this step errored.
type StepReport struct {
	RoundLimit   int                       `json:"round_limit,omitempty"`
	Verification *factory.StepVerification `json:"verification,omitempty"`
	Chat         string                    `json:"chat,omitempty"`
	Name         string                    `json:"name"`
	Model        string                    `json:"model,omitempty"`
	State        string                    `json:"state,omitempty"`
	Round        int                       `json:"round,omitempty"`
	// StartedAt is when the step's latest round began on the worker; absent
	// leaves the factory's as it is.
	StartedAt *time.Time  `json:"started_at,omitempty"`
	Result    *StepResult `json:"result,omitempty"`
	Rounds    []RoundLine `json:"rounds,omitempty"`
	Failure   *Failure    `json:"failure,omitempty"`
}

// StepResult preserves the evidence that later steps need after a restart.
type StepResult struct {
	PublishSummary []string                `json:"publish_summary,omitempty"`
	Repair         *factory.CIRepairResult `json:"repair,omitempty"`
	Tests          *factory.TestCounts     `json:"tests,omitempty"`
	Done           bool                    `json:"done"`
	Findings       int                     `json:"findings,omitempty"`
	Claims         []Claim                 `json:"claims,omitempty"`
	Output         string                  `json:"output,omitempty"`
	Exit           int                     `json:"exit,omitempty"`
	Notes          []string                `json:"notes,omitempty"`
	Cost           float64                 `json:"cost,omitempty"`
	Chat           string                  `json:"chat,omitempty"`
}

// stepResultWire is one step's report in the wire's words, both ways a
// report crosses: a worker's report of a round ([FromRunReport]) and the
// pull that hands a job to a worker ([WorkerJob]). [stageResultItem] reads it
// back. The model rides on the step itself (Step.Model on a pull,
// StepReport.Model on a worker report). A plan's edit never crosses (the
// runner applies it as the round ends, and the kept result drops it), nor do
// Reports and Inferred: they are folded into the round's summary line where
// the round ran, and that line crosses (steps[].rounds). A field added here is
// one a factory older than it refuses a worker's whole report for.
func stepResultWire(r factory.StageResult) *StepResult {
	return &StepResult{Repair: r.Repair, PublishSummary: r.PublishSummary, Tests: r.Tests, Done: r.Done, Findings: r.Findings, Claims: mapList(r.Claims, fromClaim),
		Output: r.Output, Exit: r.Exit, Notes: r.Notes, Cost: r.Spent, Chat: r.Chat}
}

// stageResultItem is [stepResultWire] read back.
func stageResultItem(r StepResult) factory.StageResult {
	return factory.StageResult{Repair: r.Repair, PublishSummary: r.PublishSummary, Tests: r.Tests, Done: r.Done, Findings: r.Findings, Claims: mapList(r.Claims, toClaim),
		Output: r.Output, Exit: r.Exit, Notes: r.Notes, Spent: r.Cost, Chat: r.Chat}
}

// WorkerJob is the job as the worker holding its lease pulls it: [FromItem],
// with each step that ran carrying its whole last report, and the factory's
// copy of the pipeline file it runs whole ([Job.PipelineCopy]).
//
// A RUNNER THAT TAKES A JOB ON MUST KNOW WHAT CAME BEFORE. A job moves to
// another machine when its worker goes away, and the runner there rebuilds
// every earlier step from the pulled job (internal/factory/run's revive). With only
// a step's result line it rebuilt a plan that said nothing of being done, no
// notes and no claims, so the steps after it were briefed that every earlier
// step was incomplete. The report is the pull's alone: a reader's job stays as
// it was, and the report's chat names a path on the runner's machine, which
// only the lease holder may read (serve's withholdChats).
func WorkerJob(it factory.Item) Job {
	j := FromItem(it)
	// The runner runs the file the job carries, so it is sent whole here.
	j.PipelineCopy = it.PipelineCopy.Clone()
	if it.Stream == nil {
		return j
	}
	// The run's steps are its phases in order ([steps]), so the k-th step
	// in the run is the k-th phase; [unsteps] reads them back the same way.
	k := 0
	for i := range j.Steps {
		if !j.Steps[i].InRun {
			continue
		}
		if k < len(it.Stream.Phases) {
			if r := it.Stream.Phases[k].Result; r != nil {
				j.Steps[i].Report = stepResultWire(*r)
			}
		}
		k++
	}
	return j
}

// WorkerDone names the final product state; ready distinguishes proof that
// still needs a person from another open question.
type WorkerDone struct {
	State string `json:"state,omitempty"`
	Ready bool   `json:"ready,omitempty"`
}

// ToWorkerReport is the single vocabulary boundary for worker writes.
func ToWorkerReport(r WorkerReport) factory.RunReport {
	out := factory.RunReport{FollowUp: r.FollowUp, HeadCommit: r.HeadCommit, Publishing: r.Publishing, Change: r.Change, ChangeBase: r.ChangeBase, Thread: r.Thread, Delivered: r.Delivered, Cost: r.Cost, Claims: mapList(r.Claims, toClaim), Talk: r.Talk, Notes: r.Notes, Publish: r.Publish, CommentURL: r.CommentURL, Room: r.Room}
	if r.Pipeline != nil {
		stages, stream := unsteps(Job{Steps: r.Pipeline, Desk: &JobDesk{Run: &Run{}}})
		out.Pipeline = &factory.PipelineChange{Stages: stages, Phases: stream.Phases}
	}
	for _, s := range r.Steps {
		stage := factory.StageReport{Name: s.Name, Chat: s.Chat, State: factory.PhaseState(s.State), Round: s.Round, RoundLimit: s.RoundLimit, Verification: s.Verification, Failure: failureItem(s.Failure), Rounds: roundsItem(s.Rounds)}
		if s.StartedAt != nil {
			stage.Started = *s.StartedAt
		}
		if res := s.Result; res != nil {
			item := stageResultItem(*res)
			item.Model = s.Model
			stage.Result = &item
		}
		out.Stages = append(out.Stages, stage)
	}
	for _, l := range r.Log {
		out.Log = append(out.Log, factory.LogLine{At: l.At, Tone: l.Tone, Text: l.Text})
	}
	if q := r.Question; q != nil {
		out.Question = &factory.Asked{Stage: q.FromStep, Question: q.Text, Options: q.Options, Pick: q.Pick, Why: q.Why}
		if q.At != nil {
			out.Question.At = *q.At
		}
		out.QuestionKind = q.Kind
	}
	out.PR = toPR(r.PR)
	if m := r.Merge; m != nil {
		out.Merge = &factory.MergeReport{State: m.State, SHA: m.SHA, Why: m.Why, PR: toPR(m.PR)}
	}
	return out
}

// FromRunReport is the worker's half of the same boundary: what its runner
// wrote, in the wire's words. [ToWorkerReport] reads it back.
func FromRunReport(r factory.RunReport) WorkerReport {
	out := WorkerReport{FollowUp: r.FollowUp, HeadCommit: r.HeadCommit, Publishing: r.Publishing, Change: r.Change, ChangeBase: r.ChangeBase, Thread: r.Thread, Delivered: r.Delivered, Cost: r.Cost, Claims: mapList(r.Claims, fromClaim), Talk: r.Talk, Notes: r.Notes,
		Log: mapList(r.Log, fromLog), Publish: r.Publish, CommentURL: r.CommentURL, Room: r.Room}
	if p := r.Pipeline; p != nil {
		out.Pipeline = steps(factory.Item{Stages: p.Stages, Stream: &factory.Stream{Phases: p.Phases}})
	}
	for _, s := range r.Stages {
		step := StepReport{Name: s.Name, Chat: s.Chat, State: string(s.State), Round: s.Round, RoundLimit: s.RoundLimit, Verification: s.Verification, StartedAt: timeOrNil(s.Started), Failure: failureWireOrNil(s.Failure), Rounds: reportedRounds(s.Rounds)}
		if res := s.Result; res != nil {
			step.Result = stepResultWire(*res)
			step.Model = res.Model
		}
		out.Steps = append(out.Steps, step)
	}
	if q := r.Question; q != nil {
		out.Question = &Question{FromStep: q.Stage, Text: q.Question, Options: q.Options, Pick: q.Pick, Why: q.Why, Kind: r.QuestionKind, At: timeOrNil(q.At)}
	}
	out.PR = fromPR(r.PR)
	if m := r.Merge; m != nil {
		out.Merge = &WorkerMerge{State: m.State, SHA: m.SHA, Why: m.Why, PR: fromPR(m.PR)}
	}
	return out
}

// ToWorkerState recognizes only final states; unknown input must not fall
// through to the codec's forward-compatible read behavior.
func ToWorkerState(d WorkerDone) (factory.State, bool) {
	if d.State == "" {
		return factory.StateLanded, !d.Ready
	}
	if d.Ready {
		return factory.StateLanded, d.State == StateNeedsYou
	}
	switch d.State {
	case StateBacklog, StateNeedsYou, StateDone, StateDropped:
		return stateBack[d.State], true
	default:
		return "", false
	}
}

// WorkerPull is the worker's only execution door; the lease identifies a
// particular hold, distinct from the job that may run again later.
type WorkerPull struct {
	Factory Factory     `json:"factory"`
	Lease   WorkerLease `json:"lease"`
	Job     Job         `json:"job"`
	// Signals are verbs left for the job before it was pulled (a recheck).
	Signals []WorkerSignal `json:"signals,omitempty"`
}

// WorkerHeartbeat answers a heartbeat: the renewed lease, and every verb a
// person asked of the job since the last one. This is the whole of the
// server-to-worker channel (ARCHITECTURE §4).
type WorkerHeartbeat struct {
	Factory Factory        `json:"factory"`
	Lease   WorkerLease    `json:"lease"`
	Signals []WorkerSignal `json:"signals,omitempty"`
}

// WorkerSignal is one verb for the worker's runner: hold (yes is on), answer
// (yes, words), steer (words), recheck, budget, model, reply, publish, merge
// or stop (internal/factory/lease's Signal* names).
type WorkerSignal struct {
	Automatic *bool   `json:"automatic,omitempty"`
	Budget    float64 `json:"budget,omitempty"`
	ID        string  `json:"id,omitempty"`
	Verb      string  `json:"verb"`
	Yes       bool    `json:"yes,omitempty"`
	Words     string  `json:"words,omitempty"`
}

type WorkerLease struct {
	Runner     string    `json:"runner,omitempty"`
	Owner      string    `json:"owner,omitempty"`
	ThreadOnly bool      `json:"thread_only,omitempty"`
	ID         string    `json:"id"`
	Job        int       `json:"job"`
	Worker     string    `json:"worker"`
	Machine    string    `json:"machine,omitempty"`
	Handle     string    `json:"handle"`
	ExpiresAt  time.Time `json:"expires_at"`
}
