package head

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// "We don't target enterprise any more" used to land BESIDE the belief it
// contradicted. The head had exactly two verbs — remember, which only adds, and
// retract, which is for a line the person is throwing away — so the one thing a
// correction actually is had no spelling, and both beliefs stayed active and
// selectable into every later job.
func TestCorrectingABeliefRetiresItInsteadOfAccumulatingBesideIt(t *testing.T) {
	graph := openHeadStore(t)
	stale, err := graph.RecordFact("", "domain:positioning", store.FactPlain,
		"enterprise is the primary segment")
	if err != nil {
		t.Fatal(err)
	}

	client := &fakeClient{responses: []string{
		`{"reply":"Got it — small teams from here.","command":null,"remember":{"scope":"domain:positioning","kind":"fact","body":"small teams are the primary segment, not enterprise","replaces":` +
			itoa(stale.Seq) + `}}`,
	}}
	user, err := graph.PostMessage(store.Message{
		SessionID: "chat", Role: store.RoleUser,
		Body: "we don't target enterprise anymore — stop framing it that way",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := New(client, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}

	active, err := graph.ActiveFacts("domain:positioning", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 {
		t.Fatalf("positioning shelf holds %d live beliefs, want one: %+v", len(active), active)
	}
	if !strings.Contains(active[0].Body, "small teams") {
		t.Fatalf("the wrong belief survived: %+v", active[0])
	}
	retired, found, err := graph.FactBySeq(stale.Seq)
	if err != nil || !found {
		t.Fatalf("stale belief vanished entirely: found=%t err=%v", found, err)
	}
	if retired.Status != store.FactSuperseded {
		t.Fatalf("stale belief status = %q, want superseded", retired.Status)
	}
	// Superseded, not retracted: the person stated a new version, they did not
	// veto the subject, so nothing here may look like a human refusal.
	if retired.StatusOrigin == store.FactOriginUser {
		t.Fatalf("a correction was recorded as a user veto: %+v", retired)
	}
}

// The judgment is the model's; the checkable part is not. A number pointing at
// nothing, at a line that is already gone, or at the row just written must cost
// the notebook nothing.
func TestASupersessionPointingAtNothingChangesNothing(t *testing.T) {
	graph := openHeadStore(t)
	standing, err := graph.RecordFact("", "user", store.FactPreference, "they like short answers")
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.QuarantineFact(standing.Seq, standing.Seq, store.FactOriginUser); err != nil {
		t.Fatal(err)
	}

	client := &fakeClient{responses: []string{
		`{"reply":"noted","command":null,"remember":{"scope":"user","kind":"preference","body":"they want figures in dollars","replaces":` +
			itoa(standing.Seq) + `}}`,
	}}
	user, err := graph.PostMessage(store.Message{
		SessionID: "chat", Role: store.RoleUser, Body: "always give me figures in dollars",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := New(client, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}

	// The new belief still lands; the mis-aimed supersession is simply ignored,
	// and the user's own retraction keeps the status it was given.
	active, err := graph.ActiveFacts("user", 10)
	if err != nil || len(active) != 1 || !strings.Contains(active[0].Body, "dollars") {
		t.Fatalf("active user shelf = %+v err=%v", active, err)
	}
	after, found, err := graph.FactBySeq(standing.Seq)
	if err != nil || !found || after.Status != store.FactQuarantined ||
		after.StatusOrigin != store.FactOriginUser {
		t.Fatalf("the retraction was overwritten: %+v found=%t err=%v", after, found, err)
	}
}

func itoa(value int64) string {
	digits := ""
	if value == 0 {
		return "0"
	}
	for value > 0 {
		digits = string(rune('0'+value%10)) + digits
		value /= 10
	}
	return digits
}
