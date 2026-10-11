package api

import (
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// WorkerAttach names the machine and its person; consent to unassigned work
// is explicit and applies only to each named repository.
type WorkerAttach struct {
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
	Steps    []StepReport `json:"steps,omitempty"`
	Log      []LogLine    `json:"log,omitempty"`
	Cost     float64      `json:"cost,omitempty"`
	Claims   []Claim      `json:"claims,omitempty"`
	Question *Question    `json:"question,omitempty"`
	PR       *PR          `json:"pr,omitempty"`
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
	out := factory.RunReport{Cost: r.Cost, Claims: mapList(r.Claims, toClaim)}
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
}

type WorkerLease struct {
	ID        string    `json:"id"`
	Job       int       `json:"job"`
	Worker    string    `json:"worker"`
	Handle    string    `json:"handle"`
	ExpiresAt time.Time `json:"expires_at"`
}
