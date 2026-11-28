package clientproto

import (
	"encoding/json"
	"strings"
	"testing"
)

// S11: an outcome is well formed exactly when it names a call, has a known status, and carries a reason
// only when the call did not run.
func TestOutcomeValidity(t *testing.T) {
	good := []Outcome{
		{Number: 1, Status: OutcomeDone, Agent: "chat-1"},
		{Number: 2, Status: OutcomeDone},
		{Number: 3, Status: OutcomeRefused, Reason: ReasonNotHolder, Agent: "chat-1"},
		{Number: 4, Status: OutcomeRefused, Reason: ReasonBadCall},
		{Number: 5, Status: OutcomeExpired, Reason: ReasonExpired},
		{Number: 6, Status: OutcomeRefused, Reason: ReasonFailed, Agent: "chat-1"},
	}
	for _, o := range good {
		if !o.Valid() {
			t.Errorf("%+v must be valid", o)
		}
	}
	bad := map[string]Outcome{
		"number zero":                 {Number: 0, Status: OutcomeDone},
		"a negative number":           {Number: -1, Status: OutcomeDone},
		"an unknown status":           {Number: 1, Status: "pending"},
		"done with a reason":          {Number: 1, Status: OutcomeDone, Reason: ReasonBadCall},
		"refused with no reason":      {Number: 1, Status: OutcomeRefused},
		"expired with another reason": {Number: 1, Status: OutcomeExpired, Reason: ReasonNotHolder},
		"expired with no reason":      {Number: 1, Status: OutcomeExpired},
	}
	for name, o := range bad {
		if o.Valid() {
			t.Errorf("an outcome with %s must be invalid: %+v", name, o)
		}
	}
}

// S11: the slot holds the last 32 outcomes, oldest first, and reads back as it was written.
func TestOutcomesKeepTheNewestThirtyTwo(t *testing.T) {
	var list []Outcome
	for n := int64(1); n <= MaxOutcomes+5; n++ {
		list = append(list, Outcome{Number: n, Status: OutcomeDone, Agent: "chat"})
	}
	raw, err := EncodeOutcomes(list)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeOutcomes(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != MaxOutcomes || got[0].Number != 6 || got[len(got)-1].Number != MaxOutcomes+5 {
		t.Fatalf("the slot must hold numbers 6..%d, got %d entries from %d to %d", MaxOutcomes+5, len(got), got[0].Number, got[len(got)-1].Number)
	}
	empty, err := EncodeOutcomes(nil)
	if err != nil || string(empty) != "[]" {
		t.Fatalf("an empty list is [] and never null: %q %v", empty, err)
	}
}

// S11: what a computer published is read strictly, so a slot a relay or a bug mangled is not half-believed.
func TestOutcomesRefuseWhatIsNotWellFormed(t *testing.T) {
	for name, raw := range map[string]string{
		"an unknown key":   `[{"number":1,"status":"done","extra":1}]`,
		"trailing data":    `[{"number":1,"status":"done"}] x`,
		"not a list":       `{"number":1,"status":"done"}`,
		"an invalid entry": `[{"number":1,"status":"refused"}]`,
		"too many entries": "[" + strings.TrimSuffix(strings.Repeat(`{"number":1,"status":"done"},`, MaxOutcomes+1), ",") + "]",
	} {
		if _, err := DecodeOutcomes([]byte(raw)); err == nil {
			t.Errorf("%s must be refused", name)
		}
	}
	if _, err := EncodeOutcomes([]Outcome{{Number: 1, Status: OutcomeRefused}}); err == nil {
		t.Error("an invalid outcome must not be encoded")
	}
}

// The wire spelling the phone parses: keys in this order, agent and reason absent when empty.
func TestOutcomeWireSpelling(t *testing.T) {
	raw, err := json.Marshal([]Outcome{{Number: 3, Status: OutcomeDone, Agent: "a"}, {Number: 4, Status: OutcomeRefused, Reason: ReasonNeedsFaceID}})
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"number":3,"status":"done","agent":"a"},{"number":4,"status":"refused","reason":"needs_face_id"}]`
	if string(raw) != want {
		t.Fatalf("got %s want %s", raw, want)
	}
}
