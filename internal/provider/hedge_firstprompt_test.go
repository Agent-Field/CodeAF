package provider

import (
	"strings"
	"testing"
	"time"

	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/lane/lanestub"
)

// TestAStalledFirstPromptLaneShowsARescueSwitch is F42, staged.
//
// A fresh profile's first prompt has no saved talk model and no text on the
// screen yet. The ordinary rescue notice waits for text to replace, so a stall
// sat through the ninety-second first-token cut with nothing said and `/model`
// left to be discovered. The first-prompt mark makes that stall name the
// switch path while the second arm is already out.
func TestAStalledFirstPromptLaneShowsARescueSwitch(t *testing.T) {
	told := listen(t)
	rig := newLaneRig(t, "first-prompt/stall",
		lanestub.Lane{Name: "A", Profile: lanestub.Profile{
			TTFT: 300 * time.Millisecond, Rate: 2000, Tokens: 24, Heartbeats: true,
		}},
		lanestub.Lane{Name: "B", Profile: lanestub.Profile{TTFT: 5 * time.Millisecond, Rate: 2000, Tokens: 24}},
	)
	// THE LEDGER IS LEFT EMPTY. That is a first prompt: nothing believed, the
	// shipped default still on the talk slot.
	SetHedgeBudget(lanes.NewBudget(1, 0))
	rig.patience(t, 150*time.Millisecond)

	watched := &notices{}
	report := &HedgeReport{}
	ctx := WithFirstPrompt(WithHedgeReport(talking(), report))
	ctx = WithLaneChoice(ctx, choiceFor(rig.model, 0))
	ctx = WithStreamObserver(ctx, watched.observe)

	response, err := rig.client.CompleteWithMessages(ctx, userMessages("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if !report.Hedged() {
		t.Fatal("a stalled first prompt was not rescued")
	}
	if winner, _ := report.Lanes(); winner != "B" {
		t.Fatalf("winner = %q, want the lane the first-prompt stall armed", winner)
	}
	if tokens := answerTokens(response); tokens != 24 {
		t.Fatalf("the answer is %d tokens, want B's 24", tokens)
	}
	switching, ok := told.find(PhaseSwitching)
	if !ok {
		t.Fatalf("the rescue was never said out loud: %v", told.story())
	}
	if !strings.EqualFold(switching.Then, "B") {
		t.Fatalf("switching to %q, want B", switching.Then)
	}
	said := watched.kinds(StreamNotice)
	if len(said) != 1 || said[0].Delta != firstPromptNotice {
		t.Fatalf("notices = %+v, want the first-prompt /model switch path", said)
	}
	if !strings.Contains(said[0].Delta, "/model") {
		t.Fatalf("the first-prompt notice %q does not name /model", said[0].Delta)
	}
	waitFor(t, func() bool { return rig.server.Cancels("A") == 1 })
}
