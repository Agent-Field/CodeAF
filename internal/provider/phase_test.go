package provider

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/lane/lanestub"
)

// ── ONE REQUEST, ONE STORY ──────────────────────────────────────────────────
//
// The phase clock's whole claim is that a person watching a slow answer is
// never told two things at once and never told nothing at all. These tests are
// the claim, scenario by scenario, against the fake router: a healthy stream
// walks connecting → first word → thinking → writing and stops; a stream that
// stalls inside its thinking shows the countdown and then the switch, on ONE
// request's worth of extra spending; and a stream with nowhere to go shows the
// phase and the clock and promises nothing.

// heard collects the phase story in the order it was told.
type heard struct {
	mu   sync.Mutex
	news []PhaseNews
}

func (h *heard) take(news PhaseNews) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.news = append(h.news, news)
}

func (h *heard) all() []PhaseNews {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]PhaseNews(nil), h.news...)
}

// story is the phases in order with the repeats collapsed, which is what a
// person actually watched: a phase that says itself again every second to
// carry a rate is one phase and not sixty.
func (h *heard) story() []Phase {
	var out []Phase
	for _, news := range h.all() {
		if len(out) > 0 && out[len(out)-1] == news.Phase {
			continue
		}
		out = append(out, news.Phase)
	}
	return out
}

// find is the first news in a phase, and whether there was one.
func (h *heard) find(phase Phase) (PhaseNews, bool) {
	for _, news := range h.all() {
		if news.Phase == phase {
			return news, true
		}
	}
	return PhaseNews{}, false
}

// listen registers a reader for the length of the test.
func listen(t *testing.T) *heard {
	t.Helper()
	told := &heard{}
	previous := OnPhase(told.take)
	t.Cleanup(func() { OnPhase(previous) })
	return told
}

func TestAHealthyAnswerTellsItsPhasesInOrderAndThenStops(t *testing.T) {
	told := listen(t)
	rig := newLaneRig(t, "phase/healthy",
		lanestub.Lane{Name: "A", Profile: lanestub.Profile{
			TTFT: 5 * time.Millisecond, Rate: 2000, Reasoning: 8, Tokens: 24,
		}},
		lanestub.Lane{Name: "B", Profile: lanestub.Profile{TTFT: 5 * time.Millisecond, Rate: 2000, Tokens: 24}},
	)
	rig.believes("A", 5, 200)

	ctx := WithLaneChoice(context.Background(), choiceFor(rig.model, 500*time.Millisecond))
	if _, err := rig.client.CompleteWithMessages(ctx, userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	want := []Phase{PhaseConnecting, PhaseFirstWord, PhaseThinking, PhaseWriting, ""}
	got := told.story()
	if len(got) != len(want) {
		t.Fatalf("story = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("story = %v, want %v", got, want)
		}
	}
	// THE LANE IS NAMED ON THE PHASES THAT KNOW IT, and never before the
	// stream said who was answering.
	writing, _ := told.find(PhaseWriting)
	if writing.Lane != "A" {
		t.Fatalf("the writing phase named lane %q, want A", writing.Lane)
	}
	if connecting, _ := told.find(PhaseConnecting); connecting.Lane != "" {
		t.Fatalf("named a lane %q before the stream had said one", connecting.Lane)
	}
}

func TestAStallInsideTheThinkingShowsTheCountdownAndThenTheSwitch(t *testing.T) {
	told := listen(t)
	rig := newLaneRig(t, "phase/stall",
		lanestub.Lane{Name: "A", Profile: lanestub.Profile{
			TTFT: 2 * time.Millisecond, Rate: 1000,
			Reasoning: 100, Tokens: 40,
			StallAfter: 100, StallFor: 600 * time.Millisecond,
		}},
		lanestub.Lane{Name: "B", Profile: lanestub.Profile{TTFT: 5 * time.Millisecond, Rate: 2000, Tokens: 24}},
	)
	rig.believes("A", 2, 250)

	report := &HedgeReport{}
	ctx := WithHedgeReport(context.Background(), report)
	ctx = WithLaneChoice(ctx, choiceFor(rig.model, 12*time.Millisecond))
	response, err := rig.client.CompleteWithMessages(ctx, userMessages("hello"))
	if err != nil {
		// A HEDGE THAT LANDS IS NOT A CUT. The whole point of the rescue is
		// that the person never sees the transport's own last resort.
		t.Fatalf("the caller was handed an error for a rescued answer: %v", err)
	}
	if answerTokens(response) != 24 {
		t.Fatalf("the answer is %d tokens, want the rescuer's 24", answerTokens(response))
	}
	// EXACTLY ONE EXTRA REQUEST. Two mechanisms used to be able to answer one
	// silence — the transport's cut and re-ask, and the lane watch's hedge —
	// and a person paid for the prompt twice to be told about it once.
	if got := rig.server.Requests("A") + rig.server.Requests("B"); got != 2 {
		t.Fatalf("requests on the wire = %d, want the original and one rescue", got)
	}
	// The countdown was real: a moment, and a lane to go to at it.
	first, ok := told.find(PhaseFirstWord)
	if !ok {
		t.Fatalf("nothing was said about the wait for the first word: %v", told.story())
	}
	if first.Deadline.IsZero() || first.Then == "" {
		t.Fatalf("first word carried deadline %v then %q; want both, because there was a lane to go to", first.Deadline, first.Then)
	}
	// And the switch named where it was going.
	switching, ok := told.find(PhaseSwitching)
	if !ok {
		t.Fatalf("the rescue was never said out loud: %v", told.story())
	}
	if !strings.EqualFold(switching.Then, "B") {
		t.Fatalf("switching to %q, want B", switching.Then)
	}
	// ONE STORY AND NOT TWO. The arm nobody is hearing must not narrate.
	for _, news := range told.all() {
		if news.Phase == PhaseFirstWord && news.Lane == "B" {
			t.Fatalf("the rescuing arm told its own story while it was still silent")
		}
	}
}

func TestWithNowhereToGoTheClockPromisesNothing(t *testing.T) {
	told := listen(t)
	rig := newLaneRig(t, "phase/alone",
		lanestub.Lane{Name: "A", Profile: lanestub.Profile{TTFT: 20 * time.Millisecond, Rate: 2000, Tokens: 24}},
	)
	rig.believes("A", 20, 2000)

	// A choice with no alternative is what `routing off`, a strict pin and a
	// ledger that has heard of one lane all look like from here.
	choice := choiceFor(rig.model, 12*time.Millisecond)
	choice.Alt = ""
	ctx := WithLaneChoice(context.Background(), choice)
	if _, err := rig.client.CompleteWithMessages(ctx, userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	if len(told.story()) == 0 {
		t.Fatalf("a request with no alternative told the person nothing at all")
	}
	for _, news := range told.all() {
		if !news.Deadline.IsZero() || news.Then != "" {
			t.Fatalf("promised %q at %v with no lane to go to: a countdown that expires and does nothing is the surface lying", news.Then, news.Deadline)
		}
	}
}

func TestTheGuardStillProtectsWhenNoRescueIsPossible(t *testing.T) {
	defer shortenStallBoundsCapped(t, 40*time.Millisecond, 40*time.Millisecond, 60*time.Millisecond)()
	rig := newLaneRig(t, "phase/lastresort",
		lanestub.Lane{Name: "A", Profile: lanestub.Profile{
			TTFT: 2 * time.Millisecond, Rate: 2000, Tokens: 40,
			StallAfter: 4, StallFor: 5 * time.Second,
		}},
	)
	rig.believes("A", 2, 2000)

	choice := choiceFor(rig.model, 12*time.Millisecond)
	choice.Alt = ""
	ctx := WithLaneChoice(context.Background(), choice)
	_, err := rig.client.CompleteWithMessages(ctx, userMessages("hello"))
	if _, cut := CutFrom(err); !cut {
		t.Fatalf("err = %v, want the stall guard's cut: with no rescue possible it is the last thing standing between a person and forever", err)
	}
}

func TestAHiddenErrandNamesItselfSoTheSurfaceCanIgnoreIt(t *testing.T) {
	told := listen(t)
	rig := newLaneRig(t, "phase/hidden",
		lanestub.Lane{Name: "A", Profile: lanestub.Profile{TTFT: 2 * time.Millisecond, Rate: 2000, Tokens: 8}},
	)
	rig.believes("A", 2, 2000)

	ctx := WithRole(context.Background(), lanes.RoleAuxiliary)
	ctx = WithLaneChoice(ctx, choiceFor(rig.model, 500*time.Millisecond))
	if _, err := rig.client.CompleteWithMessages(ctx, userMessages("name this")); err != nil {
		t.Fatal(err)
	}
	news := told.all()
	if len(news) == 0 {
		t.Fatalf("a side errand said nothing at all")
	}
	for _, one := range news {
		if one.Role != lanes.RoleAuxiliary {
			t.Fatalf("phase %q carried role %q, want the errand to name itself so a status line can decline to draw it", one.Phase, one.Role)
		}
		if one.Role.Visible() {
			t.Fatalf("a naming errand claimed the person was reading it")
		}
	}
}

// TestNoCountdownIsDrawnOverARescueNobodyCanAfford is the other half of "a
// deadline is never invented": there IS an alternative lane and there IS a
// moment, and the budget was always going to refuse the second request. A
// countdown drawn over that is a promise the machinery cannot keep.
func TestNoCountdownIsDrawnOverARescueNobodyCanAfford(t *testing.T) {
	told := listen(t)
	rig := newLaneRig(t, "phase/unaffordable",
		lanestub.Lane{Name: "A", Profile: lanestub.Profile{TTFT: 20 * time.Millisecond, Rate: 2000, Tokens: 24}},
		lanestub.Lane{Name: "B", Profile: lanestub.Profile{TTFT: 5 * time.Millisecond, Rate: 2000, Tokens: 24}},
	)
	rig.believes("A", 20, 2000)
	// A budget with no bucket is how the speed guard is switched off.
	SetHedgeBudget(lanes.NewBudget(0, 0))

	ctx := WithLaneChoice(context.Background(), choiceFor(rig.model, 12*time.Millisecond))
	if _, err := rig.client.CompleteWithMessages(ctx, userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	if len(told.story()) == 0 {
		t.Fatalf("a request with the guard off told the person nothing at all")
	}
	for _, news := range told.all() {
		if !news.Deadline.IsZero() || news.Then != "" {
			t.Fatalf("promised %q at %v on a budget that refuses every rescue", news.Then, news.Deadline)
		}
	}
}
