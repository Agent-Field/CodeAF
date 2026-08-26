package tui3

// hostperf_test.go pins PERF.md's three --host laws AGAINST THE REAL SURFACE.
//
// The reads themselves are pinned one layer down, in internal/remote, where the
// client's own getters are counted. This file exists because that is not the
// claim: the claim is about a FRAME and a KEYSTROKE, and the only thing that
// knows what a frame reads is the code that draws one. A getter that stopped
// asking is worth nothing if something else on the render path starts.
//
// It counts and never times. A round trip here is an in-memory pipe, so a
// stopwatch would be measuring this machine's scheduler; the count is the same
// fact on a loaded laptop, on idle CI, and over an ssh pipe to another
// continent (PERF.md's doctrine).

import (
	"context"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/remote"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// farAgent is a conversation on the far side of a real connection, and the
// counter that says how many times this surface reached across it.
func farAgent(t *testing.T) (*app, *remote.Loop) {
	t.Helper()
	loop, err := remote.Loopback(remote.Hello{Version: remote.Version}, remote.Options{
		Boot: func(remote.Hello) (*remote.Engine, error) {
			return &remote.Engine{Agent: farStub{}, Workspace: "/srv/app", SessionFile: "/srv/j.jsonl"}, nil
		},
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = loop.Close() })
	a := newTestApp(loop.Client.Agent())
	a.host = "devbox"
	return a, loop
}

// A VIEW OVER --host ISSUES ZERO FAR CALLS. Not few, not cached — zero. The
// frame clock turns thirty times a second while a turn is running, and a
// terminal whose repaint rate is the round-trip time to another machine is a
// terminal that has stopped repainting.
func TestAViewOverAConnectionIssuesZeroFarCalls(t *testing.T) {
	a, loop := farAgent(t)

	// The status row is the whole reason this law exists: it names the model,
	// its reasoning level, what the conversation weighs and what it has cost.
	a.model = "openai/gpt-5"
	a.tokens, a.ctxTokens, a.cost = 4200, 8000, 0.25

	before := loop.FarCalls()
	for range 60 {
		a.touch()
		if out := frame(a); out == "" {
			t.Fatal("the frame drew nothing")
		}
	}
	if spent := loop.FarCalls() - before; spent != 0 {
		t.Fatalf("sixty frames cost %d far calls, want 0", spent)
	}
}

// A KEY OVER --host ISSUES ZERO FAR CALLS. Typing is the one thing a person
// does continuously, and a keystroke that waited on a network would make the
// composer feel broken on a link that is working perfectly.
func TestAKeyOverAConnectionIssuesZeroFarCalls(t *testing.T) {
	a, loop := farAgent(t)

	before := loop.FarCalls()
	for _, r := range "fix the roof and then the gutter" {
		a.Update(key(string(r)))
	}
	// And the keys that move around the page cost nothing either.
	for _, chord := range []string{"up", "down", "left", "right", "esc"} {
		a.Update(key(chord))
	}
	if spent := loop.FarCalls() - before; spent != 0 {
		t.Fatalf("thirty-six keystrokes cost %d far calls, want 0", spent)
	}
}

// AND A SUBMIT ISSUES EXACTLY ONE. What goes up is the intent — the sentence —
// and nothing else: a send that also asked what the model was, or what the last
// turn cost, would be three round trips on the one keystroke a person is
// actually waiting on.
func TestASubmitOverAConnectionIssuesExactlyOneFarCall(t *testing.T) {
	a, loop := farAgent(t)

	before := loop.FarCalls()
	cmd := a.submit("fix the roof")
	// The line is on the page already, and the wire has not been touched: the
	// call happens on the command, off the update loop (echo.go).
	if spent := loop.FarCalls() - before; spent != 0 {
		t.Fatalf("the update that echoed the line cost %d far calls, want 0", spent)
	}
	a.adopt(runSubmit(t, cmd))
	if spent := loop.FarCalls() - before; spent != 1 {
		t.Fatalf("a submit cost %d far calls, want exactly 1", spent)
	}
}

// farStub is a conversation that answers everything and does nothing, which is
// all these laws need one to do: what is being counted is what the SURFACE
// asks, and an engine that thought harder would not change the count.
type farStub struct{}

func (farStub) Submit(context.Context, string) (<-chan session.Event, error) {
	done := make(chan session.Event)
	close(done)
	return done, nil
}

func (farStub) SubmitStanding(context.Context, string) (<-chan session.Event, error) {
	return nil, nil
}

func (farStub) SubmitImage(context.Context, string, []session.Image) (<-chan session.Event, error) {
	return nil, nil
}
func (farStub) FollowUp(string) (<-chan session.Event, error)             { return nil, nil }
func (farStub) Interrupt()                                                {}
func (farStub) Compact(context.Context) error                             { return nil }
func (farStub) Close() error                                              { return nil }
func (farStub) Model() string                                             { return "openai/gpt-5" }
func (farStub) SetModel(string)                                           {}
func (farStub) SetContextWindow(int)                                      {}
func (farStub) ReasoningFor(string) string                                { return "high" }
func (farStub) ReasoningLevels() map[string]string                        { return map[string]string{"openai/gpt-5": "high"} }
func (farStub) SetReasoningFor(string, string)                            {}
func (farStub) ResolveConsent(uint64, bool)                               {}
func (farStub) ResolveConsentRemember(uint64, bool, session.ConsentScope) {}
func (farStub) ResolveStanding(uint64, session.StandingAnswer)            {}
func (farStub) ResolveHarness(uint64, bool, string)                       {}
func (farStub) ResolveConnect(string, bool)                               {}
func (farStub) ResolveConnectKey(string, string)                          {}
func (farStub) NoteConnected(string, string)                              {}
func (farStub) Title() string                                             { return "the roof leaks" }
func (farStub) Usage() session.Usage                                      { return session.Usage{Turns: 2, CostUSD: 0.25} }
func (farStub) ContextTokens() int                                        { return 8000 }
func (farStub) Transcript() []session.DisplayEntry                        { return nil }
func (farStub) EarlierHistory() session.EarlierHistory                    { return session.EarlierHistory{} }
func (farStub) RewindPoints() []session.RewindPoint                       { return nil }
func (farStub) RewindAt(int) ([]session.DisplayEntry, error)              { return nil, nil }
