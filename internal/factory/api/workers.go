package api

import (
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// WorkerAttach names the machine and its person; consent to unassigned work
// is explicit and applies only to each named repository.
type WorkerAttach struct {
	Machine         string          `json:"machine,omitempty"`
	Name            string          `json:"name"`
	Handle          string          `json:"handle"`
	TakesUnassigned map[string]bool `json:"takes_unassigned,omitempty"`
}

// WorkerAttached returns the credential only at registration, so reads of
// the floor can never expose another machine's bearer secret.
type WorkerAttached struct {
	Factory    Factory `json:"factory"`
	WorkerID   string  `json:"worker_id"`
	Credential string  `json:"credential"`
}

// WorkerReport carries incremental results. Cost is newly incurred dollars,
// so a result's own cost is descriptive and is not added a second time.
type WorkerReport struct {
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
}

// StepReport carries the last round's full result without exposing Go names.
type StepReport struct {
	Name   string      `json:"name"`
	State  string      `json:"state,omitempty"`
	Round  int         `json:"round,omitempty"`
	Result *StepResult `json:"result,omitempty"`
}

// StepResult preserves the evidence that later steps need after a restart.
type StepResult struct {
	Done     bool     `json:"done"`
	Findings int      `json:"findings,omitempty"`
	Claims   []Claim  `json:"claims,omitempty"`
	Output   string   `json:"output,omitempty"`
	Exit     int      `json:"exit,omitempty"`
	Notes    []string `json:"notes,omitempty"`
	Cost     float64  `json:"cost,omitempty"`
	Chat     string   `json:"chat,omitempty"`
}

// WorkerDone names the final product state; ready distinguishes proof that
// still needs a person from another open question.
type WorkerDone struct {
	State string `json:"state,omitempty"`
	Ready bool   `json:"ready,omitempty"`
}

// ToWorkerReport is the single vocabulary boundary for worker writes.
func ToWorkerReport(r WorkerReport) factory.RunReport {
	out := factory.RunReport{Cost: r.Cost, Claims: mapList(r.Claims, toClaim), Talk: r.Talk, Notes: r.Notes, Publish: r.Publish, CommentURL: r.CommentURL}
	if r.Pipeline != nil {
		stages, stream := unsteps(Job{Steps: r.Pipeline, Desk: &JobDesk{Run: &Run{}}})
		out.Pipeline = &factory.PipelineChange{Stages: stages, Phases: stream.Phases}
	}
	for _, s := range r.Steps {
		stage := factory.StageReport{Name: s.Name, State: factory.PhaseState(s.State), Round: s.Round}
		if res := s.Result; res != nil {
			stage.Result = &factory.StageResult{Done: res.Done, Findings: res.Findings, Claims: mapList(res.Claims, toClaim), Output: res.Output, Exit: res.Exit, Notes: res.Notes, Spent: res.Cost, Chat: res.Chat}
		}
		out.Stages = append(out.Stages, stage)
	}
	for _, l := range r.Log {
		out.Log = append(out.Log, factory.LogLine{At: l.At, Tone: l.Tone, Text: l.Text})
	}
	if q := r.Question; q != nil {
		out.Question = &factory.Asked{Stage: q.FromStep, Question: q.Text, Options: q.Options, Pick: q.Pick, Why: q.Why}
		out.QuestionKind = q.Kind
	}
	if pr := r.PR; pr != nil {
		out.PR = &factory.PullRequest{Number: pr.Number, URL: pr.URL, CI: pr.CI}
	}
	return out
}

// FromRunReport is the worker's half of the same boundary: what its runner
// wrote, in the wire's words. [ToWorkerReport] reads it back.
func FromRunReport(r factory.RunReport) WorkerReport {
	out := WorkerReport{Cost: r.Cost, Claims: mapList(r.Claims, fromClaim), Talk: r.Talk, Notes: r.Notes,
		Log: mapList(r.Log, fromLog), Publish: r.Publish, CommentURL: r.CommentURL}
	if p := r.Pipeline; p != nil {
		out.Pipeline = steps(factory.Item{Stages: p.Stages, Stream: &factory.Stream{Phases: p.Phases}})
	}
	for _, s := range r.Stages {
		step := StepReport{Name: s.Name, State: string(s.State), Round: s.Round}
		if res := s.Result; res != nil {
			step.Result = &StepResult{Done: res.Done, Findings: res.Findings, Claims: mapList(res.Claims, fromClaim),
				Output: res.Output, Exit: res.Exit, Notes: res.Notes, Cost: res.Spent, Chat: res.Chat}
		}
		out.Steps = append(out.Steps, step)
	}
	if q := r.Question; q != nil {
		out.Question = &Question{FromStep: q.Stage, Text: q.Question, Options: q.Options, Pick: q.Pick, Why: q.Why, Kind: r.QuestionKind}
	}
	if pr := r.PR; pr != nil {
		out.PR = &PR{Number: pr.Number, URL: pr.URL, CI: pr.CI}
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
// (yes, words), steer (words), recheck, or shape.
type WorkerSignal struct {
	Verb  string `json:"verb"`
	Yes   bool   `json:"yes,omitempty"`
	Words string `json:"words,omitempty"`
}

type WorkerLease struct {
	ID        string    `json:"id"`
	Job       int       `json:"job"`
	Worker    string    `json:"worker"`
	Machine   string    `json:"machine,omitempty"`
	Handle    string    `json:"handle"`
	ExpiresAt time.Time `json:"expires_at"`
}
