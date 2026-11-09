package remote

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/chatlist"
)

type finalNoticeAgent struct {
	*fakeAgent
	mu    sync.Mutex
	lines []string
}

func (a *finalNoticeAgent) TakeSealNotice() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.lines) == 0 {
		return ""
	}
	line := a.lines[0]
	a.lines = a.lines[1:]
	return line
}

func TestFinalSavingReturnsItsNoticeToTheClosingWindowOnce(t *testing.T) {
	for _, displaced := range []bool{true, false} {
		t.Run(map[bool]string{true: "displaced", false: "clean"}[displaced], func(t *testing.T) {
			agent := &finalNoticeAgent{fakeAgent: &fakeAgent{}}
			loop, err := Loopback(Hello{}, Options{Boot: func(Hello) (*Engine, error) {
				return &Engine{Agent: agent, FinishSaving: func(WrappedAgent) error {
					if displaced {
						agent.note(chatlist.Superseded("desk"))
					}
					return nil
				}}, nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			defer loop.Close()
			if err := loop.Client.Agent().Close(); err != nil {
				t.Fatal(err)
			}
			// Both generations expose notices as an optional capability at the window boundary.
			source, ok := any(loop.Client.Agent()).(interface{ TakeSavingNotices() []string })
			var got []string
			if ok {
				got = source.TakeSavingNotices()
			}
			if displaced && (len(got) != 1 || got[0] != chatlist.Superseded("desk")) || !displaced && len(got) != 0 {
				t.Fatalf("closing receipt notices: %q", got)
			}
			if ok && len(source.TakeSavingNotices()) != 0 {
				t.Fatal("closing receipt repeated its notice")
			}
			if got := loop.Client.TakeNotice(); got != "" {
				t.Fatalf("final receipt also used the live notice queue: %s", got)
			}
			if err := loop.Client.Agent().Close(); err != nil {
				t.Fatal(err)
			}
			if ok && len(source.TakeSavingNotices()) != 0 {
				t.Fatal("repeated close repeated its notice")
			}
		})
	}
}

func TestSavingNoticeFieldPreservesBothWireGenerations(t *testing.T) {
	raw := []byte(`{"kind":"result","id":1,"localSaved":true,"savingNotices":["saved on a branch"]}`)
	var current Frame
	if err := json.Unmarshal(raw, &current); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(current)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"savingNotices":["saved on a branch"]`) {
		t.Fatalf("new peer lost the optional field: %s", encoded)
	}
	var old struct {
		Kind       string `json:"kind"`
		ID         uint64 `json:"id"`
		LocalSaved bool   `json:"localSaved"`
	}
	if err := json.Unmarshal(encoded, &old); err != nil || !old.LocalSaved {
		t.Fatalf("old peer rejected the new receipt: %+v %v", old, err)
	}
	if err := json.Unmarshal([]byte(`{"kind":"result","id":1,"localSaved":true}`), &current); err != nil {
		t.Fatal(err)
	}
	// Decode into a fresh frame, as the wire reader does for every message.
	current = Frame{}
	if err := json.Unmarshal([]byte(`{"kind":"result","id":1,"localSaved":true}`), &current); err != nil {
		t.Fatal(err)
	}
	encoded, err = json.Marshal(current)
	if err != nil || strings.Contains(string(encoded), "savingNotices") {
		t.Fatalf("old engine acquired a new notice: %s %v", encoded, err)
	}
}

func (a *finalNoticeAgent) note(line string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.lines = append(a.lines, line)
}
