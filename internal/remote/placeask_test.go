package remote

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/placegraph"
	"github.com/Agent-Field/codeaf/internal/roles"
	"github.com/Agent-Field/codeaf/internal/session"
)

// placeAgent is an engine agent that answers the places ask and remembers what
// it was asked and how long it was given.
type placeAgent struct {
	*fakeAgent
	asked    placegraph.ModelRequest
	deadline bool
	left     time.Duration
	calls    int
}

func (a *placeAgent) AskPlaces(ctx context.Context, req placegraph.ModelRequest) (session.PlacesAnswer, error) {
	a.asked = req
	a.calls++
	deadline, ok := ctx.Deadline()
	a.deadline = ok
	if ok {
		a.left = time.Until(deadline)
	}
	return session.PlacesAnswer{Text: `{"place":"p2"}`, Model: "cheap/model"}, nil
}

// THE PLACES ASK CROSSES WHOLE, AND THE ENGINE SPENDS NO LONGER THAN THE ASKER
// WAITS. The welcome says the engine answers it; the question arrives with its
// role, instructions and text intact; the answer comes back with the model that
// gave it, since the desktop prices and names the ask off it.
func TestThePlacesAskCrossesTheWire(t *testing.T) {
	far := &placeAgent{fakeAgent: &fakeAgent{model: "m"}}
	loop := askLoop(t, far)
	if !loop.Client.Welcome().PlaceAsk {
		t.Fatal("an engine whose agent answers the places ask does not say so")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req := placegraph.ModelRequest{Role: roles.RolePlaceSuggest, System: "the rules", User: "c1 | the lexer"}
	got, err := loop.Client.Agent().AskPlaces(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != `{"place":"p2"}` || got.Model != "cheap/model" {
		t.Fatalf("the answer came back as %+v", got)
	}
	if far.calls != 1 || far.asked != req {
		t.Fatalf("the engine was asked %d times, about %+v", far.calls, far.asked)
	}
	if !far.deadline || far.left <= 0 || far.left > 5*time.Second {
		t.Fatalf("the engine's ask was bounded by %v (deadline %v), not the asker's five seconds", far.left, far.deadline)
	}
}

// THE ENGINE'S CEILING BOUNDS EVERY ASK, and a budget under it is kept.
func TestPlaceAskBudgetBoundsTheModelCall(t *testing.T) {
	for _, tc := range []struct {
		name   string
		budget time.Duration
		want   time.Duration
	}{
		{"zero", 0, placeAskCeiling},
		{"huge", time.Hour, placeAskCeiling},
		{"small", 250 * time.Millisecond, 250 * time.Millisecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			far := &placeAgent{fakeAgent: &fakeAgent{model: "m"}}
			loop := askLoop(t, far)
			if _, err := loop.Client.call(nil, MethodPlacesAsk, PlacesAskArgs{Budget: tc.budget}); err != nil {
				t.Fatal(err)
			}
			if far.calls != 1 || !far.deadline || far.left <= 0 || far.left > tc.want || far.left < tc.want-time.Second {
				t.Fatalf("budget %v called %d times with deadline=%v, remaining=%v; want at most %v", tc.budget, far.calls, far.deadline, far.left, tc.want)
			}
		})
	}
}

// AN EXPIRED ASK NEVER REACHES THE MODEL.
func TestExpiredPlaceAskNeverCallsTheModel(t *testing.T) {
	far := &placeAgent{fakeAgent: &fakeAgent{model: "m"}}
	loop := askLoop(t, far)
	_, err := loop.Client.call(nil, MethodPlacesAsk, PlacesAskArgs{Budget: -time.Nanosecond})
	if err == nil || err.Error() != context.DeadlineExceeded.Error() || far.calls != 0 {
		t.Fatalf("an expired ask called the model %d times and returned %v", far.calls, err)
	}
}

// AN OLDER ENGINE IS NOT ASKED. An engine whose agent has no places ask sends
// no flag, and the ask is refused at this end with nothing written onto the
// wire; an engine asked anyway refuses rather than answering.
func TestAnOlderEngineIsNotAskedAboutPlaces(t *testing.T) {
	loop := askLoop(t, &fakeAgent{model: "m"})
	if loop.Client.Welcome().PlaceAsk {
		t.Fatal("an engine whose agent cannot ask said it could")
	}
	before := loop.Client.made.Load()
	got, err := loop.Client.Agent().AskPlaces(context.Background(), placegraph.ModelRequest{Role: roles.RolePlaceFile, System: "s", User: "u"})
	if err == nil || got != (session.PlacesAnswer{}) {
		t.Fatalf("an older engine answered a places question: %+v, %v", got, err)
	}
	if after := loop.Client.made.Load(); after != before {
		t.Fatalf("%d calls went onto the wire for an ask the welcome said was not there", after-before)
	}
	if _, err := loop.Client.call(nil, MethodPlacesAsk, PlacesAskArgs{}); err == nil || !strings.Contains(err.Error(), placeAskOffWord) {
		t.Fatalf("an engine without the ask answered it: %v", err)
	}
}

// A MODEL ASK IS A READ, never queued behind a turn.
func TestThePlacesAskIsNotQueuedBehindATurn(t *testing.T) {
	if got := classify(MethodPlacesAsk); got != classGetter {
		t.Fatalf("%s is classed %v, want a getter", MethodPlacesAsk, got)
	}
}

// The actual engine, rather than only a fixture, must answer the ask, or the
// flag would be false on every real engine and nothing would ever be asked.
var _ placeAskDoor = (*session.Agent)(nil)
