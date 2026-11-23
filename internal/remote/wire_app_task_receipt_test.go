package remote

// A NOTE'S TYPED OUTCOME CROSSES THE SOCKET INTACT, AND WHAT THIS END CANNOT
// KNOW IS UNKNOWN. Every law here is about one of the three ways that fails:
// an old client reading a typed unknown as success, a silence read as a
// refusal, and an old engine's flattened text read as a refusal.

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

// receiptFake is an engine with the receipt door, answering what it is told.
type receiptFake struct {
	appTasksFake
	receipt session.AppTaskReceipt
	asked   []session.AppTaskIntent
}

func (f *receiptFake) AppTaskActionReceipt(intent session.AppTaskIntent) session.AppTaskReceipt {
	f.asked = append(f.asked, intent)
	return f.receipt
}

// The frame is the intent at the top level plus one flag, so an engine that
// predates the flag decodes the same intent; and the typed answer is read back.
func TestANoteReceiptCrossesTypedAndTheIntentStaysReadableToAnOldEngine(t *testing.T) {
	client, e := newEngineStating(t, func(w *Welcome) { w.AppTaskReceipt = true })
	held := session.AppTaskReceipt{Outcome: session.AppTaskHeld, ClientID: "c-1"}
	e.answers[MethodAppTaskAction] = appTaskActionResult{Receipt: &held}
	intent := session.AppTaskIntent{ID: "7", Action: "note", Text: "same words", ClientID: "c-1"}
	if got := client.Agent().AppTaskActionReceipt(intent); !reflect.DeepEqual(got, held) {
		t.Fatalf("receipt = %+v", got)
	}
	call := e.calls(MethodAppTaskAction)[0]
	var old session.AppTaskIntent
	if err := json.Unmarshal(call.Payload, &old); err != nil || !reflect.DeepEqual(old, intent) {
		t.Fatalf("an old engine would read %+v %v", old, err)
	}
	var args appTaskActionArgs
	if err := json.Unmarshal(call.Payload, &args); err != nil || !args.Receipt {
		t.Fatalf("the receipt flag did not cross: %+v %v", args, err)
	}
}

// AN ENGINE THAT PREDATES THE RECEIPT IS UNKNOWN BOTH WAYS. Its success carries
// no outcome, and its failure may be a delivered note it flattened into text.
func TestAnOldEnginesAnswersAreUnknownNeverRefusedOrDelivered(t *testing.T) {
	client, e := newEngine(t)
	intent := session.AppTaskIntent{ID: "7", Action: "note", Text: "words", ClientID: "c-1"}
	if got := client.Agent().AppTaskActionReceipt(intent); got.Outcome != session.AppTaskUnknown || got.ClientID != "c-1" {
		t.Fatalf("an old engine's success = %+v, want unknown", got)
	}
	e.fails[MethodAppTaskAction] = "no answer came back, so it is not known whether these words arrived"
	if got := client.Agent().AppTaskActionReceipt(intent); got.Outcome != session.AppTaskUnknown {
		t.Fatalf("an old engine's failure = %+v, want unknown", got)
	}
	if calls := e.calls(MethodAppTaskAction); len(calls) != 2 {
		t.Fatalf("%d calls for two sends; nothing may be sent again", len(calls))
	}
	// An outcome word this build does not know is unknown, not guessed at.
	client2, e2 := newEngineStating(t, func(w *Welcome) { w.AppTaskReceipt = true })
	e2.answers[MethodAppTaskAction] = json.RawMessage(`{"receipt":{"outcome":"teleported"}}`)
	if got := client2.Agent().AppTaskActionReceipt(intent); got.Outcome != session.AppTaskUnknown {
		t.Fatalf("an unknown word = %+v", got)
	}
}

// An untyped error carries no occurrence-bound outcome, regardless of the
// current welcome. Capability changes must never turn unknown into refused.
func TestANewEnginesUntypedErrorOnTheReceiptCallStaysUnknown(t *testing.T) {
	client, e := newEngineStating(t, func(w *Welcome) { w.AppTaskReceipt = true })
	e.fails[MethodAppTaskAction] = "a watcher cannot do that"
	got := client.Agent().AppTaskActionReceipt(session.AppTaskIntent{ID: "7", Action: "note", Text: "w", ClientID: "c-1"})
	if got.Outcome != session.AppTaskUnknown || got.Reason != "a watcher cannot do that" || got.ClientID != "c-1" {
		t.Fatalf("receipt = %+v", got)
	}
}

// A CALL NOBODY ANSWERED IS UNKNOWN, NEVER REFUSED — even on an engine whose
// welcome says it types the outcome — and it is not sent again.
func TestANoteNobodyAnsweredIsUnknown(t *testing.T) {
	client, e := newEngineStating(t, func(w *Welcome) { w.AppTaskReceipt = true })
	e.silent[MethodAppTaskAction] = true
	waited := make(chan session.AppTaskReceipt, 1)
	go func() {
		waited <- client.Agent().AppTaskActionReceipt(session.AppTaskIntent{ID: "7", Action: "note", Text: "w", ClientID: "c-1"})
	}()
	time.Sleep(20 * time.Millisecond)
	_ = e.conn.Close()
	select {
	case got := <-waited:
		if got.Outcome != session.AppTaskUnknown || got.ClientID != "c-1" {
			t.Fatalf("an unanswered note = %+v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a note on a dead pipe hung")
	}
	if calls := e.calls(MethodAppTaskAction); len(calls) != 1 {
		t.Fatalf("%d calls; an unanswered note was sent again", len(calls))
	}
}

// THE ENGINE'S HALF ANSWERS A RECEIPT ONLY TO A CALL THAT ASKED FOR ONE. An old
// client reads a nil error as success, so an unknown must reach it as an error.
func TestTheEngineTypesTheOutcomeOnlyForACallThatAskedForIt(t *testing.T) {
	unknown := session.AppTaskReceipt{Outcome: session.AppTaskUnknown, Reason: "not known", ClientID: "c-1"}
	fake := &receiptFake{appTasksFake: appTasksFake{failure: errors.New("not known")}, receipt: unknown}
	intent := session.AppTaskIntent{ID: "7", Action: "note", Text: "w", ClientID: "c-1"}

	old, _ := json.Marshal(intent)
	if payload, err := dispatchAppTask(fake, Frame{Method: MethodAppTaskAction, Payload: old}); err == nil || payload != nil {
		t.Fatalf("an old client's call = %s %v, want the error it always got", payload, err)
	}
	if len(fake.asked) != 0 || fake.intent.ClientID != "c-1" {
		t.Fatalf("an old client's call took the receipt door: %+v", fake.asked)
	}

	asked, _ := json.Marshal(appTaskActionArgs{AppTaskIntent: intent, Receipt: true})
	payload, err := dispatchAppTask(fake, Frame{Method: MethodAppTaskAction, Payload: asked})
	if err != nil {
		t.Fatal(err)
	}
	var result appTaskActionResult
	if err := json.Unmarshal(payload, &result); err != nil || result.Receipt == nil || !reflect.DeepEqual(*result.Receipt, unknown) {
		t.Fatalf("the typed answer = %s %v", payload, err)
	}
	if len(fake.asked) != 1 || !reflect.DeepEqual(fake.asked[0], intent) {
		t.Fatalf("the receipt door was asked %+v", fake.asked)
	}

	// An engine without the door REFUSES the flagged call, and its welcome does
	// not claim the receipt. The plain door is not called (see
	// TestAReceiptCallWithoutTheDoorIsRefusedBeforeTheEffect).
	plain := &appTasksFake{}
	if payload, err := dispatchAppTask(plain, Frame{Method: MethodAppTaskAction, Payload: asked}); !errors.Is(err, errAppTaskNoReceiptDoor) || payload != nil {
		t.Fatalf("an engine without the door = %s %v", payload, err)
	}
	if appTaskReceiptKnown(plain) || !appTaskReceiptKnown(fake) {
		t.Fatal("the welcome's claim does not follow the door")
	}
}

// panickingReceiptFake panics inside the receipt door AFTER it has counted the
// call, the way a failure after the direction was written would.
type panickingReceiptFake struct {
	appTasksFake
	calls int
}

func (f *panickingReceiptFake) AppTaskActionReceipt(session.AppTaskIntent) session.AppTaskReceipt {
	f.calls++
	panic("the announcement failed after the direction was written")
}

type panickingReceiptAgent struct {
	*fakeAgent
	*panickingReceiptFake
}

// A PANIC INSIDE THE ENGINE'S DOOR IS NOT PRE-DISPATCH. The note may already be
// written down or handed over, so the client, whose welcome says its errors are
// refusals, must be told UNKNOWN by a typed answer, once.
func TestAPanicAfterTheNoteIsUnknownNeverRefused(t *testing.T) {
	far := &panickingReceiptAgent{fakeAgent: &fakeAgent{}, panickingReceiptFake: &panickingReceiptFake{}}
	loop := laneLoop(t, far)
	if !loop.Client.Welcome().AppTaskReceipt {
		t.Fatal("the engine does not claim the typed receipt, so this test proves nothing")
	}
	got := loop.Client.Agent().AppTaskActionReceipt(session.AppTaskIntent{ID: "7", Action: "note", Text: "w", ClientID: "c-1"})
	if got.Outcome != session.AppTaskUnknown || got.ClientID != "c-1" || got.Reason != appTaskReceiptWentWrongWord {
		t.Fatalf("a panic after the note = %+v, want unknown", got)
	}
	if far.calls != 1 {
		t.Fatalf("the receipt door was entered %d times; nothing may be sent again", far.calls)
	}
}

// AN AGENT WITH NO RECEIPT DOOR IS NEVER ASKED THE PLAIN WAY for a call that
// asked for a receipt: the plain door's error is flattened text that may follow
// the effect, and the client reads an error under the welcome as a refusal.
func TestAReceiptCallWithoutTheDoorIsRefusedBeforeTheEffect(t *testing.T) {
	plain := &appTasksFake{}
	asked, _ := json.Marshal(appTaskActionArgs{AppTaskIntent: session.AppTaskIntent{ID: "7", Action: "note", Text: "w", ClientID: "c-1"}, Receipt: true})
	if _, err := dispatchAppTask(plain, Frame{Method: MethodAppTaskAction, Payload: asked}); !errors.Is(err, errAppTaskNoReceiptDoor) {
		t.Fatalf("err = %v, want the typed refusal", err)
	}
	if plain.intent.ID != "" || plain.intent.Action != "" {
		t.Fatalf("the plain door was called: %+v", plain.intent)
	}
}
