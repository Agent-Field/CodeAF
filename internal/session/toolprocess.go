package session

import (
	"context"
	"sync/atomic"
	"time"
)

// ToolProcess identifies the process THIS call became, from the registry that
// took it. Job and Started are the job's own identity; Service is instead the
// supervisor's durable service id. Neither is ever read from a tool's output.
// Nil means the call did not become a process, or its producer did not say.
type ToolProcess struct {
	Job     int       `json:"job,omitempty"`
	Started time.Time `json:"started,omitempty"`
	Service string    `json:"service,omitempty"`
}

type toolProcessKey struct{}

// withToolProcess gives one dispatch its own immutable result, so reused
// provider call ids and parallel calls cannot lend each other a process.
func withToolProcess(ctx context.Context) (context.Context, *atomic.Pointer[ToolProcess]) {
	mark := &atomic.Pointer[ToolProcess]{}
	return context.WithValue(ctx, toolProcessKey{}, mark), mark
}

func toolProcessFrom(ctx context.Context) *atomic.Pointer[ToolProcess] {
	mark, _ := ctx.Value(toolProcessKey{}).(*atomic.Pointer[ToolProcess])
	return mark
}

// markToolJob runs only after the registry owns the command. The snapshot is
// written before adoption wakes Execute, including on the steer's goroutine.
func markToolJob(mark *atomic.Pointer[ToolProcess], started *job) {
	if mark != nil {
		mark.Store(&ToolProcess{Job: started.id, Started: started.started})
	}
}

func markToolService(ctx context.Context, id string, failed bool) {
	if mark := toolProcessFrom(ctx); mark != nil && id != "" && !failed {
		mark.Store(&ToolProcess{Service: id})
	}
}
