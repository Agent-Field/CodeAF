package provider

import (
	"strings"
	"testing"

	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── THE CHIP AND THE WIRE READ ONE FACT ─────────────────────────────────────
//
// THE MEASURED FAILURE (issue #1022, dev 649392ad7). Morph was pinned from the
// settings sheet; the `lane` row read `pinned: Morph`, the status chip read
// `moonshotai/kimi-k3@morph`, and three turns were answered by Sail Research. A
// body carrying `{"only":["Morph"],"allow_fallbacks":false}` cannot be answered
// by another machine, so for those three turns the pin was not on the wire while
// the chrome said it was.
//
// The surface names the machine through one door ([PinNow]) and the wire draws
// its demand through another ([Client.drawLaneChoice]). This is the law that the
// two answer the same question: for every state the row and the run can be in,
// the machine a surface may name is exactly the machine the next request a
// person is reading would demand.

// demandedFor is what the wire would ask for on a call this person is waiting
// on — the `only` of the draw, or nothing where the draw makes no choice.
func demandedFor(t *testing.T, rig *laneRig, model string) string {
	t.Helper()
	request := &ai.Request{Model: model, Messages: userMessages("hello")}
	choice, made := rig.client.drawLaneChoice(turnKnobs(), model, request)
	if !made || len(choice.Only) == 0 {
		return ""
	}
	if len(choice.Only) != 1 {
		t.Fatalf("the draw demanded %v, and a pin is one machine", choice.Only)
	}
	return choice.Only[0]
}

// THE LAW, WALKED OVER EVERY STATE OF THE ROW. Each step moves one fact — the
// row, the wire's answer to it, the base — and after each one the chip's fact
// and the wire's demand are compared. They are compared rather than each
// asserted against a literal on purpose: what this test is about is that they
// cannot disagree, and a literal in two places is how they came to.
func TestTheChipNamesAMachineOnlyWhileTheWireWouldDemandIt(t *testing.T) {
	rig := newLaneRig(t, "chip/one-fact", retiredLanes()...)
	forgotten(t)
	installedRow(t, RoutingSimple)
	rig.client.config.Routing = nil
	model := rig.model

	agree := func(state string) string {
		t.Helper()
		named, stood := PinNow(model)
		demanded := demandedFor(t, rig, model)
		if !strings.EqualFold(named, demanded) {
			t.Fatalf("%s: the chip would say %q and the wire demands %q", state, named, demanded)
		}
		if named != "" && stood != "" {
			t.Fatalf("%s: the pin is on the wire as %q and stood down as %q at once", state, named, stood)
		}
		return stood
	}

	// 1. NOBODY HAS CHOSEN. No name on the chrome, no provider object on the
	//    request.
	pinned(t, LanePin{})
	agree("auto")

	// 2. THE ROW ASKS FOR NO LANE AT ALL. `openrouter` is a person declining to
	//    name a machine, so there is still no machine to name.
	SetLanePin(LanePin{OpenRouter: true})
	agree("openrouter")

	// 3. A PIN. The one state where the chrome may write `@machine`.
	SetLanePin(LanePin{Lane: "Ghost"})
	if named, _ := PinNow(model); !strings.EqualFold(named, "Ghost") {
		t.Fatalf("a live pin reads %q, want Ghost", named)
	}
	agree("pinned")

	// 4. THE WIRE HAS RETIRED THE PAIRING. This is the state the defect was: the
	//    row still says Ghost, every request since goes out on auto, and the chip
	//    must come off the model word in the same breath — with the OTHER half of
	//    the door naming whose pin it was, so a row can say `auto (…)` rather
	//    than falling silent about a machine somebody wrote down.
	if !retirePin("Ghost", model) {
		t.Fatal("the refusal did not retire the pin")
	}
	if stood := agree("retired"); !strings.EqualFold(stood, "Ghost") {
		t.Fatalf("a retired pin stood down %q, want Ghost — a surface has nothing to name it by", stood)
	}

	// 5. AND A PIN RETIRED FOR ONE MODEL IS UNTOUCHED FOR ANOTHER, because the
	//    refusal was about a pairing.
	other := model + "-elsewhere"
	if named, _ := PinNow(other); !strings.EqualFold(named, "Ghost") {
		t.Fatalf("a pin retired for one model reads %q for another", named)
	}

	// 6. PINNING AGAIN PUTS IT STRAIGHT BACK, which is what the manual promises
	//    and what makes `enter` on a stood-down machine a re-pin rather than the
	//    unpin the same key is everywhere else in that fold.
	RepinLane(LanePin{Lane: "Ghost"})
	if named, stood := PinNow(model); !strings.EqualFold(named, "Ghost") || stood != "" {
		t.Fatalf("re-pinning left the pin at %q stood down as %q", named, stood)
	}
	agree("re-pinned")

	// 7. A BASE THAT WILL NOT CARRY A LANE CHOICE AT ALL. Nothing is demanded, so
	//    nothing may be named — and it is not a stand-down either: the row was
	//    never refused, the base simply does not take it, and that has its own
	//    sentence ([UncarriedPinLine]).
	const base = "https://proxy.example/v1"
	lanes.WireSheet(base, "", nil, false)
	t.Cleanup(func() { lanes.WireSheet("", "", nil, false) })
	if !lanes.HeardPrefsSilent(base) {
		t.Fatal("the base's answer was not filed")
	}
	if named, stood := PinNow(model); named != "" || stood != "" {
		t.Fatalf("on a base that takes no lane choice the chip reads %q / %q", named, stood)
	}
}

// AND THE OLD DOOR IS THE NEW ONE'S FIRST HALF. [PinnedFor] is what the surface
// called before this pair existed and what several callers still call; a build
// where it answered anything else would be two readings of one fact again.
func TestPinnedForIsTheFirstHalfOfPinNow(t *testing.T) {
	rig := newLaneRig(t, "chip/one-door", retiredLanes()...)
	forgotten(t)
	pinned(t, LanePin{Lane: "Harbor"})
	for _, model := range []string{rig.model, rig.model + "-elsewhere"} {
		named, _ := PinNow(model)
		if PinnedFor(model) != named {
			t.Fatalf("PinnedFor(%q) is %q and PinNow is %q", model, PinnedFor(model), named)
		}
	}
	if !retirePin("Harbor", rig.model) {
		t.Fatal("the refusal did not retire the pin")
	}
	named, stood := PinNow(rig.model)
	if PinnedFor(rig.model) != named || named != "" {
		t.Fatalf("after the retirement PinnedFor is %q and PinNow is %q", PinnedFor(rig.model), named)
	}
	// THE SENTENCE AND THE ROW'S OWN TAIL ARE SPELLED FROM THE SAME NAME, so a
	// surface drawing the short one cannot name a different machine from the one
	// the conversation was told about.
	if !strings.Contains(RetiredPinLine(stood), stood) || !strings.Contains(RetiredPinTail(stood), stood) {
		t.Fatalf("the retirement's two spellings do not both name %q", stood)
	}
}
