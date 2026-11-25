package runs

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
)

// Call names one step invocation. Key is stable across retries of that same
// invocation and can serve as an idempotency key at the destination.
type Call struct {
	approved bool
	App      string         `json:"app"`
	Run      string         `json:"run"`
	ID       string         `json:"id"`
	Step     string         `json:"step"`
	Key      string         `json:"key"`
	Name     string         `json:"name"`
	As       string         `json:"as"`
	Args     map[string]any `json:"args"`
}

type Reply struct {
	Output any
	Cost   float64
}

// ErrOutcomeUnknown is for operations whose transport failed after dispatch.
// Retrying such a call could send or pay twice.
var ErrOutcomeUnknown = errors.New("the connector could not confirm this step's result")

type Connector interface {
	Call(context.Context, Call) (Reply, error)
}

type RecordCall struct {
	Call
	Type       string
	IDs        []string
	Set        map[string]any
	causeChain []string
}

type AgentCall struct {
	Call
	Prompt    string
	Steps     map[string]any
	Remaining float64
}

type Approval struct {
	ID        string         `json:"id"`
	Kind      string         `json:"kind"`
	Who       string         `json:"who"`
	Tool      string         `json:"tool,omitempty"`
	Args      map[string]any `json:"args,omitempty"`
	MinBudget float64        `json:"min_budget,omitempty"`
}

type FakeConnector struct {
	mu      sync.Mutex
	replies map[string]Reply
	calls   []Call
}

// NewFakeConnector answers with canned replies and rejects calls with no
// fixture, making an accidental new tool visible during Check and Preview.
func NewFakeConnector(replies map[string]Reply) *FakeConnector {
	f := &FakeConnector{replies: make(map[string]Reply, len(replies))}
	for key, reply := range replies {
		f.replies[key] = reply
	}
	return f
}

func (f *FakeConnector) Call(_ context.Context, call Call) (Reply, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, copyCall(call))
	reply, ok := f.replies[call.Name]
	if !ok {
		return Reply{}, errors.New("no sample reply for " + call.Name)
	}
	return reply, nil
}

func (f *FakeConnector) Calls() []Call {
	f.mu.Lock()
	defer f.mu.Unlock()
	calls := make([]Call, len(f.calls))
	for i, call := range f.calls {
		calls[i] = copyCall(call)
	}
	return calls
}

func copyCall(call Call) Call {
	encoded, _ := json.Marshal(call.Args)
	call.Args = nil
	_ = json.Unmarshal(encoded, &call.Args)
	return call
}
