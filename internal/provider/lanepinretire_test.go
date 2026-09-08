package provider

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/lane/lanestub"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── THE PIN THE WIRE HAS ALREADY REFUSED ────────────────────────────────────
//
// Issue #456. What is asserted here is the whole of the policy, and it is
// asserted against a real refusal on a real wire: the stub publishes a machine
// on its endpoints page and will not serve it, answering with the router's own
// `…but your request's provider.only preference permits only: …`, which is the
// one refusal class that is certain about a pairing.
//
// The behaviour it replaces: the demand was re-sent on EVERY request, so every
// turn of a session paid a 404 round trip to be told a fact this process had
// already been told — and on the turn's own unhedged call, with nothing to walk
// to, the turn ended and the person read the router's sentence.

// retiredLanes is the scenario: one machine the sheet publishes and the wire
// refuses, and two that answer.
func retiredLanes() []lanestub.Lane {
	fast := lanestub.Profile{TTFT: 3 * time.Millisecond, Rate: 4000, Tokens: 6, Tools: true, Quant: "fp8"}
	return []lanestub.Lane{
		{Name: "Ghost", SheetOnly: true, Profile: fast},
		{Name: "Haven", Profile: fast},
		{Name: "Harbor", Profile: fast},
	}
}

// retirements is the rescue news that is about a retired pin, which is the one
// sentence this policy puts on the status line.
func retirements(news []RescueNews) []RescueNews {
	var kept []RescueNews
	for _, one := range news {
		if one.Reason == RescueRetired {
			kept = append(kept, one)
		}
	}
	return kept
}

// noticeLog is what a person's conversation is told: the notes the adapter
// emits about a call rather than from it, which is the lane the durable
// sentence travels ([StreamNotice] → internal/session's EventNotice).
type noticeLog struct {
	mu    sync.Mutex
	lines []string
}

func (l *noticeLog) observe(event StreamEvent) {
	if event.Kind != StreamNotice {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, event.Delta)
}

// retired is the notes that are about a retired pin, which is the sentence this
// test is here for.
func (l *noticeLog) retired() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var kept []string
	for _, line := range l.lines {
		if strings.Contains(line, "cannot serve this model") {
			kept = append(kept, line)
		}
	}
	return kept
}

// forgotten empties the retirement set for one test, because it is process-wide
// and a test that left one set would retire the pin of every test after it.
func forgotten(t *testing.T) {
	t.Helper()
	forgetRetiredPins()
	t.Cleanup(forgetRetiredPins)
}

// A TERMINAL REFUSAL OF THE PINNED MACHINE RETIRES THE PIN FOR THAT MODEL, and
// the person is told once, in one sentence, at the moment it happens.
//
// Four laws in one run, because they are one behaviour: the demand goes out
// once, the next request for that model carries none, a request for a DIFFERENT
// model still carries the pin, and pinning somewhere else puts everything back.
func TestARefusedPinIsRetiredForThatModelAndSaidOnce(t *testing.T) {
	rig := newLaneRig(t, "refusal/pin-retired", retiredLanes()...)
	forgotten(t)
	pinned(t, LanePin{Lane: "Ghost"})
	log := &rescueLog{}
	report := &HedgeReport{}
	log.watch(report)
	ctx := WithHedgeReport(talking(), report)

	// 1. THE FIRST REQUEST DEMANDS THE MACHINE THE PERSON PINNED, is refused,
	//    and the turn still lands — on the one retry the retirement earns.
	if _, err := rig.client.CompleteWithMessages(ctx, userMessages("hello")); err != nil {
		t.Fatalf("the turn died on a refusal the widened retry was supposed to absorb: %v", err)
	}
	asks := rig.server.Asks()
	if len(asks) == 0 {
		t.Fatal("nothing reached the router")
	}
	if !demandedOnly(asks[0], "Ghost") {
		t.Fatalf("the first ask demanded %v, want the machine that was pinned", asks[0].Only)
	}
	before := len(asks)

	// 2. AND THE NEXT REQUEST FOR THIS MODEL CARRIES NO DEMAND AT ALL. This is
	//    the round trip the policy exists to stop paying: the pin used to be
	//    re-demanded here and answered with the identical 404.
	if _, err := rig.client.CompleteWithMessages(ctx, userMessages("again")); err != nil {
		t.Fatalf("the second turn failed: %v", err)
	}
	asks = rig.server.Asks()
	if len(asks) <= before {
		t.Fatal("the second turn sent nothing")
	}
	for _, later := range asks[before:] {
		if len(later.Only) != 0 {
			t.Fatalf("a request after the refusal still demanded %v — the pin was not retired", later.Only)
		}
	}
	if asked := rig.server.Requests("Ghost"); asked != 1 {
		t.Fatalf("Ghost was asked %d times, want the one demand that earned the refusal", asked)
	}

	// 3. AND THE MACHINE IS STRUCK, which is the fact the strike used to miss
	//    entirely when the refusal was absorbed below this seam rather than
	//    handed to a caller: a pin the frontier can still choose is a pin that
	//    comes back on the next turn.
	if lanes.Serves(rig.model, "Ghost") {
		t.Fatal("the machine that said it cannot serve this model is still in the serving set")
	}

	// 4. AND THE PIN STILL APPLIES TO EVERY OTHER MODEL. The refusal was about
	//    a PAIRING — "this machine cannot serve this model" — and retiring the
	//    row wholesale would be this build answering a question the wire never
	//    asked.
	other := "openrouter/another-model"
	choice, made := rig.client.laneChoiceFor(callKnobs{}, other,
		&ai.Request{Model: other, Messages: userMessages("hello")})
	if !made || len(choice.Only) != 1 || !strings.EqualFold(choice.Only[0], "Ghost") {
		t.Fatalf("a request for another model went out on %+v, want the pin the person wrote", choice)
	}

	// 5. AND THE PERSON WAS TOLD ONCE. Two turns, two turns' worth of
	//    machinery, and exactly one sentence about their row.
	retired := retirements(log.all())
	if len(retired) != 1 {
		t.Fatalf("the surface heard %d retirements, want exactly one: %+v", len(retired), log.all())
	}
	if retired[0].Alt != "Ghost" || !retired[0].Failed {
		t.Fatalf("the retirement reads %+v, want the pinned machine, failed", retired[0])
	}

	// 6. AND PINNING AGAIN CLEARS IT — the SAME machine, which is the keystroke
	//    the sentence a person just read promises works. A person naming a
	//    machine is them stating the instruction afresh, and the demand goes
	//    back out.
	RepinLane(LanePin{Lane: "Ghost"})
	choice, made = rig.client.laneChoiceFor(callKnobs{}, rig.model,
		&ai.Request{Model: rig.model, Messages: userMessages("hello")})
	if !made || len(choice.Only) != 1 || !strings.EqualFold(choice.Only[0], "Ghost") {
		t.Fatalf("after pinning again the request goes out on %+v, want the pin demanded again", choice)
	}
}

// AND THE SENTENCE REACHES THE CONVERSATION, WHICH IS WHERE IT IS READ.
//
// THE MEASURED FAILURE: an experiment build posted this news on the rescue slot
// alone, and in eight cells on the live router the sentence was never once
// observed on the screen. Two reasons, and both are about the slot rather than
// about the words. The refusal that matters is collected by the errand that
// runs at launch, whose context carries no slot and no observer at all
// (internal/session's auxiliary.go) — so the news went nowhere and every later
// request was already retired and had nothing to say. And the status row it
// would have been drawn on is outranked by the phase clock while a request is
// in flight and holds only the newest news about a model, which the answer's
// own arrival overwrites (internal/tui3's servedRiderAt).
//
// So the sentence travels as a NOTE as well — the lane the ladder's own rungs
// travel, which lands in the transcript and stays — and it waits for a call
// somebody is actually reading.
func TestTheRetiredPinIsSaidInTheConversationOnceEvenWhenNobodyWasReadingTheCallThatLearnedIt(t *testing.T) {
	rig := newLaneRig(t, "refusal/pin-said", retiredLanes()...)
	forgotten(t)
	pinned(t, LanePin{Lane: "Ghost"})
	notes := &noticeLog{}

	// THE ERRAND FIRST, and it is the shape of the real one: a role nobody is
	// reading and no observer on the context, which is where the run's first
	// `permits only:` refusal is really collected.
	errand := WithRole(WithoutStream(context.Background()), lanes.RoleAuxiliary)
	if _, err := rig.client.CompleteWithMessages(errand, userMessages("what shall we call this")); err != nil {
		t.Fatalf("the errand died on a refusal it should have widened past: %v", err)
	}
	if said := notes.retired(); len(said) != 0 {
		t.Fatalf("a call nobody is reading said %q into a stream that does not exist", said)
	}

	// AND THEN THE PERSON'S OWN TURN, which is the first call anybody is
	// reading and therefore the one that owes them the sentence.
	turn := WithStreamObserver(talking(), notes.observe)
	if _, err := rig.client.CompleteWithMessages(turn, userMessages("hello")); err != nil {
		t.Fatalf("the turn failed: %v", err)
	}
	said := notes.retired()
	if len(said) != 1 {
		t.Fatalf("the conversation was told %d times, want exactly once: %q", len(said), said)
	}
	if want := "Ghost cannot serve this model; routing on auto for this model until you pin again"; said[0] != want {
		t.Fatalf("the conversation reads %q, want %q", said[0], want)
	}

	// AND NEVER AGAIN. The fact is one fact, and a run that collects the same
	// refusal on ten turns says it on the first one only.
	if _, err := rig.client.CompleteWithMessages(turn, userMessages("again")); err != nil {
		t.Fatalf("the second turn failed: %v", err)
	}
	if said := notes.retired(); len(said) != 1 {
		t.Fatalf("the conversation was told %d times over two turns: %q", len(said), said)
	}
}

// AND A REFUSAL OF A MACHINE NOBODY PINNED RETIRES NOTHING. A rescue arm
// demands a lane of its own and a refusal of that is the race's business; only
// the row's own machine is the row's business.
func TestARefusalOfAMachineNobodyPinnedRetiresNothing(t *testing.T) {
	rig := newLaneRig(t, "refusal/pin-innocent", retiredLanes()...)
	forgotten(t)
	pinned(t, LanePin{Lane: "Harbor"})
	log := &rescueLog{}
	report := &HedgeReport{}
	log.watch(report)
	// The request demands Ghost the way a rescue does — the walk's own demand,
	// not the row's — while the person's pin names Harbor.
	ctx := WithHedgeReport(talking(), report)
	ctx = WithLaneChoice(ctx, demanding(rig.model, "Ghost", "Ghost", "Haven"))

	if _, err := rig.client.CompleteWithMessages(ctx, userMessages("hello")); err != nil {
		t.Fatalf("the walk did not absorb the refusal: %v", err)
	}
	if pinRetired("Harbor", rig.model) {
		t.Fatal("a refusal of a machine the person never pinned retired their pin")
	}
	if retired := retirements(log.all()); len(retired) != 0 {
		t.Fatalf("the surface was told %+v about a row that was not involved", retired)
	}
}

// AND A ROW RESTATED IS NOT A ROW CHANGED, which is what makes the retirement
// last a RUN rather than five minutes.
//
// THE MEASURED FAILURE: [SetLanePin] is not only the picker. It is every place
// that resolves the row, and one of them is the standing ticker, which rebuilds
// a whole posture every five minutes for as long as a window lives
// (cmd/aforge's v3StandingTicker). An experiment build that forgot every
// retirement here therefore paid the identical 404 again at 16:30:02 and again
// at 16:35:00, in one process, in one measured run, with nobody having touched
// the row. A person pinning is a person CHANGING their answer.
func TestTheRowRestatedKeepsTheRetirementAndTheRowChangedForgetsIt(t *testing.T) {
	forgotten(t)
	pinned(t, LanePin{Lane: "Ghost"})
	model := "openrouter/steady"
	if !retirePin("Ghost", model) {
		t.Fatal("the refusal did not retire the pin")
	}

	// The ticker, five minutes later, handing down the same row it read at
	// launch.
	SetLanePin(LanePin{Lane: "Ghost"})
	if !pinRetired("Ghost", model) {
		t.Fatal("restating the row somebody never touched forgot what the wire said")
	}

	// And a resolver handing down a row that really did move.
	SetLanePin(LanePin{Lane: "Harbor"})
	if pinRetired("Ghost", model) {
		t.Fatal("a row that changed did not clear the refusal the old one collected")
	}
}

// AND A PERSON PINNING THE MACHINE THEY ALREADY PINNED PUTS IT BACK, which is
// the exact keystroke the sentence they have just read promises works: "until
// you pin again".
//
// IT IS THE ONE CASE THE ROW CANNOT ANSWER. Re-choosing coreweave in the picker
// writes a row identical to the one already in force, so a rule that compared
// rows would answer a person who had just re-pinned with silence and go on
// routing their model on auto — the sentence on their screen made into a lie.
// The two entrances are the difference: [SetLanePin] is a resolver reading the
// row (the door, the standing ticker), [RepinLane] is somebody's own act.
func TestAPersonPinningTheSameLaneAgainPutsItBack(t *testing.T) {
	forgotten(t)
	pinned(t, LanePin{Lane: "Ghost"})
	model := "openrouter/repinned"
	if !retirePin("Ghost", model) {
		t.Fatal("the refusal did not retire the pin")
	}

	// The resolver, first, so that the two are told apart on the SAME row.
	SetLanePin(LanePin{Lane: "Ghost"})
	if !pinRetired("Ghost", model) {
		t.Fatal("a resolver restating the row forgot what the wire said")
	}

	// And then the person, choosing the machine they already had.
	RepinLane(LanePin{Lane: "Ghost"})
	if pinRetired("Ghost", model) {
		t.Fatal("a person pinning the same lane again did not put it back")
	}
	if pin := CurrentLanePin(); pin.Lane != "Ghost" {
		t.Fatalf("the row now reads %+v, want the machine they named", pin)
	}
}

// AND EVERY SESSION IN THE RUN INHERITS IT — the conversation, the errands
// beside it, and the task nodes it starts.
//
// A NODE IS A SESSION AND NOT A PROCESS (internal/session's task_run.go runs
// one on a goroutine; nothing in this build re-executes the binary for one), so
// the seam a child inherits the pin through is the seam it inherits the
// retirement through: this file's process-wide knob, read by every client in
// the run. A retirement that lived on a client would be re-paid once per node —
// which is what an eight-cell run of the experiment build measured, at roughly
// three 404s a cell instead of one.
func TestASecondSessionInTheSameRunInheritsTheRetirement(t *testing.T) {
	rig := newLaneRig(t, "refusal/pin-inherited", retiredLanes()...)
	forgotten(t)
	pinned(t, LanePin{Lane: "Ghost"})

	if _, err := rig.client.CompleteWithMessages(talking(), userMessages("hello")); err != nil {
		t.Fatalf("the conversation's own turn failed: %v", err)
	}
	before := rig.server.Requests("Ghost")

	// The node's own client, built the way a session builds one, against the
	// same router and in the same run.
	node, err := NewClient(Config{APIKey: "test-key", BaseURL: rig.server.URL(), Model: rig.model})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := node.CompleteWithMessages(WithRole(context.Background(), lanes.RoleLeafAttached),
		userMessages("run the tests")); err != nil {
		t.Fatalf("the node's turn failed: %v", err)
	}
	if asked := rig.server.Requests("Ghost"); asked != before {
		t.Fatalf("the node demanded the retired machine %d more times, want none", asked-before)
	}
}
