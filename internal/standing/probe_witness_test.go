package standing

// probe_witness_test.go is the machinery's half of the readiness-witness repair:
// a check that only answers "something reached back" must never be delivered as
// the person's condition, and a check that DOES carry it must still fire.
//
// THE SENTINEL HERE IS A DETERMINISTIC DOUBLE, NOT A MODEL. It encodes the
// contract the prompt states: the person's own sentence is the criterion, a
// body that carries the condition decides, and anything weaker than what they
// named is UNKNOWN rather than yes. These tests prove the pass honours those
// three verdicts; the prompt's effect on a live model is root's native proof,
// never claimed here.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// witnessSentinel is the contract as a function: the person's words choose the
// criterion, the evidence is read literally, and a reading that cannot settle
// what they asked is unknown.
func witnessSentinel(_ context.Context, j Judgment) (SentinelReading, string, float64, error) {
	words := strings.ToLower(j.Item.Words)
	evidence := strings.TrimSpace(j.Evidence)
	switch {
	case strings.Contains(evidence, `"ready": true`):
		return VerdictYes, "the endpoint reports ready", 0, nil
	case strings.Contains(evidence, `"ready": false`):
		return VerdictNo, "the endpoint reports not ready", 0, nil
	case evidence == "" || strings.Contains(evidence, "(the command failed"):
		return VerdictUnknown, "the check showed nothing to decide on", 0, nil
	case strings.Contains(words, "http 200") || strings.Contains(words, "status code"):
		// The person named the status themselves, so it is their criterion.
		if strings.HasPrefix(evidence, "200") {
			return VerdictYes, "the endpoint answered 200", 0, nil
		}
		return VerdictNo, "the endpoint did not answer 200", 0, nil
	default:
		// A bare transport reading, where they asked about readiness.
		return VerdictUnknown, "the check saw only that something answered", 0, nil
	}
}

// readyWatch is the observed shape of the live failure: an endpoint that answers
// HTTP 200 in both states, with readiness in the body, and a command that throws
// the body away and prints only the status.
func readyWatch() Item {
	item := oneShotProbe("Notify me once when the health endpoint at http://127.0.0.1:18777/ready becomes ready.")
	item.When.Hint = "yes when the output is 200"
	item.When.Probe.Command = "curl -s -o /dev/null -w '%{http_code}' --max-time 10 http://127.0.0.1:18777/ready"
	item.Does.Say = "the health endpoint is ready"
	return item
}

// THE FALSE POSITIVE, MADE MECHANICAL. HTTP 200 while the body says not ready is
// not the person's condition: the watch stays armed, quiet, and owing nobody a
// line. Then the body turns true and the one notice is delivered.
func TestATransportReadingIsNotTheConditionAndTheBodyIs(t *testing.T) {
	now := time.Date(2026, 10, 6, 11, 50, 0, 0, time.UTC)
	store := openStore(t, now)
	// The real fixture answers 200 in both states; the status-only command
	// therefore prints exactly "200" whether the application is ready or not.
	runner := &fakeRunner{evidence: "200"}
	made, err := store.Create(readyWatch())
	if err != nil {
		t.Fatal(err)
	}

	if pass := mustTick(t, probeTicker(store, runner, witnessSentinel, now)); pass.Fired != 0 {
		t.Fatalf("a status code fired the readiness watch: %+v", pass)
	}
	if len(runner.said) != 0 {
		t.Fatalf("a status code delivered a line: %v", runner.said)
	}
	armed, err := store.Get(made.ID)
	if err != nil {
		t.Fatal(err)
	}
	if armed.Status != StatusActive || armed.Runs != 0 || len(armed.Pending) != 0 {
		t.Fatalf("the ungrounded check consumed the watch: %+v", armed)
	}
	if len(armed.Previous) != 0 {
		t.Fatalf("an undecided check wrote history: %v", armed.Previous)
	}

	// A probe that reads the body is the witness the condition needs.
	runner.evidence = `{"ready": false}`
	if pass := mustTick(t, probeTicker(store, runner, witnessSentinel, now.Add(6*time.Minute))); pass.Fired != 0 {
		t.Fatalf("a decided no fired: %+v", pass)
	}
	runner.evidence = `{"ready": true}`
	later := now.Add(11 * time.Minute)
	if pass := mustTick(t, probeTicker(store, runner, witnessSentinel, later)); pass.Fired != 1 {
		t.Fatalf("a grounded true did not fire: %+v", pass)
	}
	if len(runner.said) != 1 || !strings.Contains(runner.said[0], "ready") {
		t.Fatalf("the one notice was not delivered: %v", runner.said)
	}
	done, err := store.Get(made.ID)
	if err != nil {
		t.Fatal(err)
	}
	if done.Status != StatusRetired || done.Runs != 1 {
		t.Fatalf("a one-shot condition did not retire on its one fire: %+v", done)
	}
}

// THE PERSON MAY NAME THE STATUS THEMSELVES. "notify me when it answers 200" is
// their own criterion, so a 200 witness is a grounded yes and fires.
func TestAStatusThePersonNamedIsTheirCriterion(t *testing.T) {
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	runner := &fakeRunner{evidence: "200"}
	item := readyWatch()
	item.Words = "Notify me once when the endpoint answers HTTP 200."
	item.When.Words = item.Words
	item.When.Hint = "yes when the output is 200"
	if _, err := store.Create(item); err != nil {
		t.Fatal(err)
	}
	if pass := mustTick(t, probeTicker(store, runner, witnessSentinel, now)); pass.Fired != 1 {
		t.Fatalf("the person's own status criterion did not fire: %+v", pass)
	}
}

// A CONDITION ALREADY TRUE AT THE FIRST LOOK FIRES. The repair must not require
// the world to be false first: their sentence was "when it becomes ready", and
// a watch told once is owed the notice whether the edge is crossed now or later.
func TestACurrentlyReadyFirstLookStillFires(t *testing.T) {
	now := time.Date(2026, 10, 6, 12, 30, 0, 0, time.UTC)
	store := openStore(t, now)
	runner := &fakeRunner{evidence: `{"ready": true}`}
	made, err := store.Create(readyWatch())
	if err != nil {
		t.Fatal(err)
	}
	if pass := mustTick(t, probeTicker(store, runner, witnessSentinel, now)); pass.Fired != 1 {
		t.Fatalf("an already-true condition did not fire: %+v", pass)
	}
	after, err := store.Get(made.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != StatusRetired {
		t.Fatalf("the first true did not retire the one-shot: %+v", after)
	}
}

// NO BODY, NO DECISION. An empty reading and a failed command are UNKNOWN: the
// watch keeps its opportunity, writes no negative, and delivers nothing.
func TestAMissingBodyOrAFailedCommandIsNotAWitness(t *testing.T) {
	now := time.Date(2026, 10, 6, 13, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	runner := &fakeRunner{}
	made, err := store.Create(readyWatch())
	if err != nil {
		t.Fatal(err)
	}
	before, err := store.Get(made.ID)
	if err != nil {
		t.Fatal(err)
	}
	if pass := mustTick(t, probeTicker(store, runner, witnessSentinel, now)); pass.Fired != 0 || pass.Errors != 0 {
		t.Fatalf("an empty reading was treated as an outcome: %+v", pass)
	}
	mid, err := store.Get(made.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !mid.NextDue.Equal(before.NextDue) || !mid.LastChecked.Equal(before.LastChecked) {
		t.Fatalf("an unknown consumed the opportunity: %+v", mid)
	}
	if len(mid.Previous) != 0 {
		t.Fatalf("an unknown wrote a negative into the history: %v", mid.Previous)
	}

	// The command itself failed: the runner answers an error, which is not the
	// world saying no.
	runner.probeErr = errors.New("the shell could not start it")
	runner.evidence = ""
	if pass := mustTick(t, probeTicker(store, runner, witnessSentinel, now.Add(6*time.Minute))); pass.Fired != 0 {
		t.Fatalf("a failed look fired: %+v", pass)
	}
	if len(runner.said) != 0 {
		t.Fatalf("a failed look delivered: %v", runner.said)
	}
	still, err := store.Get(made.ID)
	if err != nil {
		t.Fatal(err)
	}
	if still.Status != StatusActive || len(still.Previous) != 0 {
		t.Fatalf("a failed look was written as a decided no: %+v", still)
	}
}

// CLIPPING TRAVELS WITH THE READING. A look longer than the bound is marked cut,
// and the marker survives into what the sentinel is asked, so no partial view
// can certify an unchanged state.
func TestAClippedReadingReachesTheJudgmentClipped(t *testing.T) {
	now := time.Date(2026, 10, 6, 13, 30, 0, 0, time.UTC)
	store := openStore(t, now)
	runner := &fakeRunner{evidence: strings.Repeat("x", ProbeClip) + "tail"}
	made, err := store.Create(oneShotProbe("notify me when the condition holds"))
	if err != nil {
		t.Fatal(err)
	}
	seen := &[]Judgment{}
	sentinel := func(_ context.Context, j Judgment) (SentinelReading, string, float64, error) {
		*seen = append(*seen, j)
		return VerdictYes, "it holds", 0, nil
	}
	if pass := mustTick(t, probeTicker(store, runner, sentinel, now)); pass.Fired != 1 {
		t.Fatalf("the clipped look did not fire: %+v", pass)
	}
	if len(*seen) != 1 {
		t.Fatalf("the sentinel was asked %d times, wanted one", len(*seen))
	}
	if !strings.Contains((*seen)[0].Evidence, "tail") {
		t.Fatalf("the clipped tail did not reach the judgment: %q", (*seen)[0].Evidence)
	}
	after, err := store.Get(made.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Positive != "" {
		t.Fatalf("a clipped reading certified an identity: %q", after.Positive)
	}
}
