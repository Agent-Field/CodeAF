package remote

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

func wireRoundTrip(t *testing.T, sent session.Event) (session.Event, string) {
	t.Helper()
	payload, err := json.Marshal(WireEvent(sent))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var wire EventWire
	if err := json.Unmarshal(payload, &wire); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return wire.Unwire(), string(payload)
}

// A surface decides that a turn stopped at a cap with errors.Is, so the type has
// to cross the link beside the sentence — for every kind the engine names.
func TestATypedTurnStopSurvivesTheLink(t *testing.T) {
	for _, kind := range session.TurnStops() {
		sent := session.TurnStopError(kind, "stopped · and why")
		got, _ := wireRoundTrip(t, session.Event{Kind: session.EventError, Err: sent})
		if session.TurnStopOf(got.Err) != kind || got.Err.Error() != sent.Error() {
			t.Fatalf("%q arrived as %q (%v)", kind, session.TurnStopOf(got.Err), got.Err)
		}
	}
}

// AND AN ORDINARY FAILURE WRITES THE BYTES IT ALWAYS DID, which is what keeps a
// new engine and an older peer talking. A kind this build has never heard of —
// a newer peer's — keeps its sentence and reads as the failure it used to be.
func TestAnUntypedErrorIsUnchangedOnTheLink(t *testing.T) {
	got, payload := wireRoundTrip(t, session.Event{Kind: session.EventError, Err: errors.New("the connection to the model dropped")})
	if strings.Contains(payload, "ErrStop") || session.TurnStopOf(got.Err) != "" || got.Err.Error() != "the connection to the model dropped" {
		t.Fatalf("payload %s err %v", payload, got.Err)
	}
	var wire EventWire
	if err := json.Unmarshal([]byte(`{"Kind":0,"Err":"paused","ErrStop":"a_kind_from_the_future"}`), &wire); err != nil {
		t.Fatal(err)
	}
	if err := wire.Unwire().Err; err == nil || err.Error() != "paused" || session.TurnStopOf(err) != "" {
		t.Fatalf("unknown kind: %v", err)
	}
}
