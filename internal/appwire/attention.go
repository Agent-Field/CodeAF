package appwire

// attention.go — what a surface needs to interrupt the person less: a clock
// that waits while it holds a question nothing waits on, and a count of what
// came after the place the person last read (cmd/codeaf's appattention.go).

import (
	"strings"
	"time"
)

// Since is what an agent's record holds after one of its entries. Found false
// is an entry the record does not hold, and says nothing else. Files is the
// distinct paths the tool calls after it changed, nil when there were more
// calls than the engine reads for one answer: a count it did not make.
type Since struct {
	Found bool `json:"found"`
	// At is the moment of the entry the count starts after.
	At      *time.Time `json:"at,omitempty"`
	Entries int        `json:"entries"`
	// Turns counts the turns after the entry by the record's own marks: a
	// person's message, a line the session wrote to start one or a crew's
	// delivery opens one, and a completed or stopped reply ends one. It is nil
	// where the marks cannot tell turns apart, never a guess.
	Turns *int `json:"turns,omitempty"`
	Files *int `json:"files,omitempty"`
	// First is the id of the first entry after it, and LastAt the moment of
	// the newest.
	First  string     `json:"first,omitempty"`
	LastAt *time.Time `json:"last_at,omitempty"`
}

// AskResult is one ask as it now stands, absent when it is no longer open.
type AskResult struct {
	Ask *Ask `json:"ask,omitempty"`
}

// Attender is the optional half of attention. An [Engine] that is not one
// refuses these methods as unknown.
type Attender interface {
	// DeferAsk keeps an open ask's clock from starting; it refuses an ask the
	// engine does not know holds nothing up (Ask.Blocking false).
	DeferAsk(agent, id string) (*Ask, error)
	// ShowAsk starts a deferred clock from now, for the policy's whole length.
	ShowAsk(agent, id string) (*Ask, error)
	Since(agent, after string) (Since, error)
}

// TaskSincer is [Attender.Since] over one task's own record (tasks.since). An
// [Engine] that is not one refuses tasks.since as unknown.
type TaskSincer interface {
	// TaskSince counts what the task's record holds after one of its entries
	// (an id tasks.entries served). The count is the record's own entries:
	// the notes and sub-task rows tasks.entries lists after them are not in
	// it, and an entry that is not on the record is Found false.
	TaskSince(task, after string) (Since, error)
}

func attender(e Engine, name string) (Attender, error) {
	a, ok := e.(Attender)
	if !ok {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", name)
	}
	return a, nil
}

// askAt is asks.defer and asks.shown's params.
type askAt struct {
	Agent string `json:"agent"`
	ID    string `json:"id"`
}

func askClock(name string, call func(Attender, string, string) (*Ask, error)) method {
	return typed(func(e Engine, p askAt) (*AskResult, error) {
		a, err := attender(e, name)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(p.Agent) == "" || strings.TrimSpace(p.ID) == "" {
			return nil, Errorf(CodeBadParams, "%s needs an agent and an id", name)
		}
		ask, err := call(a, p.Agent, p.ID)
		return &AskResult{ask}, err
	})
}

var attentionMethods = map[string]method{
	"asks.defer": askClock("asks.defer", Attender.DeferAsk),
	"asks.shown": askClock("asks.shown", Attender.ShowAsk),
	"agents.since": typed(func(e Engine, p struct {
		ID    string `json:"id"`
		After string `json:"after"`
	}) (*Since, error) {
		a, err := attender(e, "agents.since")
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(p.ID) == "" || strings.TrimSpace(p.After) == "" {
			return nil, Errorf(CodeBadParams, "agents.since needs an id and the entry to count after")
		}
		since, err := a.Since(p.ID, p.After)
		return &since, err
	}),
	"tasks.since": typed(func(e Engine, p struct {
		ID    string `json:"id"`
		After string `json:"after"`
	}) (*Since, error) {
		t, ok := e.(TaskSincer)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve tasks.since")
		}
		if strings.TrimSpace(p.ID) == "" || strings.TrimSpace(p.After) == "" {
			return nil, Errorf(CodeBadParams, "tasks.since needs a task id and the entry to count after")
		}
		since, err := t.TaskSince(p.ID, p.After)
		return &since, err
	}),
}

func init() {
	for name, m := range attentionMethods {
		methods[name] = m
	}
}
