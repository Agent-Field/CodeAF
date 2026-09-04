package provider

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/lane/lanestub"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// rescuedMojibake is the F20 shape: a stream that closed after a 429
// rescue and is not language. The replacement runes are a failed decode;
// the NULs drag the printable ratio under the floor. Either reading
// alone is enough to refuse the arm.
const rescuedMojibake = "\uFFFD\x00\x01intentionally …..25 B4 third¹ .25\". as it would: structuresheetrotation Oazu\""

// TestACorruptedRescuedStreamIsRejectedAndNeverPersisted is F20, staged.
//
// A refuses, the walk lands on B, B serves mojibake with a tidy finish,
// and C has a real answer. The garbage must not be the turn: C wins, and
// the distinctive bytes never come back on the response the transcript
// would keep.
func TestACorruptedRescuedStreamIsRejectedAndNeverPersisted(t *testing.T) {
	rig := newLaneRig(t, "rescue/mojibake",
		lanestub.Lane{Name: "A", Profile: lanestub.Profile{FailWith: 400}},
		lanestub.Lane{Name: "B", Profile: lanestub.Profile{
			TTFT: 3 * time.Millisecond, Rate: 4000, Tokens: 8, Answer: rescuedMojibake,
		}},
		lanestub.Lane{Name: "C", Profile: lanestub.Profile{
			TTFT: 3 * time.Millisecond, Rate: 4000, Tokens: 24,
		}},
	)

	report := &HedgeReport{}
	ctx := WithHedgeReport(talking(), report)
	ctx = WithLaneChoice(ctx, ladderChoice(rig.model, 12*time.Millisecond))

	response, err := rig.client.CompleteWithMessages(ctx, userMessages("hello"))
	if err != nil {
		t.Fatalf("the walk died on a corrupt rescue it was supposed to refuse: %v", err)
	}
	if response == nil {
		t.Fatal("no answer came back from the machine past the corrupt rescue")
	}
	text := response.Text()
	if strings.ContainsRune(text, utf8.RuneError) || strings.Contains(text, "intentionally") ||
		strings.Contains(text, "structuresheetrotation") {
		t.Fatalf("the corrupt rescue was persisted as the turn: %q", text)
	}
	if got := answerTokens(response); got != 24 {
		t.Fatalf("the answer is %d tokens, want C's 24 — the corrupt rescue must not have won", got)
	}
	if winner, _ := report.Lanes(); winner != "C" {
		t.Fatalf("winner = %q, want the clean lane past the refuse", winner)
	}
	for _, lane := range []string{"A", "B", "C"} {
		if got := rig.server.Requests(lane); got != 1 {
			t.Fatalf("Requests(%s) = %d, want one — A failed, B was refused, C answered", lane, got)
		}
	}
}

// TestACorruptedRescuedStreamWithNowhereToGoIsAnError is the other half
// of the same law: when the only rescue is garbage, the turn ends in an
// error and the garbage is still not the response.
func TestACorruptedRescuedStreamWithNowhereToGoIsAnError(t *testing.T) {
	rig := newLaneRig(t, "rescue/mojibake-alone",
		lanestub.Lane{Name: "A", Profile: lanestub.Profile{FailWith: 400}},
		lanestub.Lane{Name: "B", Profile: lanestub.Profile{
			TTFT: 3 * time.Millisecond, Rate: 4000, Tokens: 8, Answer: rescuedMojibake,
		}},
	)

	ctx := WithLaneChoice(talking(), lanes.Choice{
		Order: []string{"A", "B"},
		Frontier: []lanes.Scored{
			{ID: lanes.ID{Model: rig.model, Lane: "A"}, TTFT: 2, Rate: 2000, Price: 0.01},
			{ID: lanes.ID{Model: rig.model, Lane: "B"}, TTFT: 5, Rate: 2000, Price: 0.01},
		},
	})
	response, err := rig.client.CompleteWithMessages(ctx, userMessages("hello"))
	if err == nil {
		t.Fatal("a corrupt rescue with nowhere to walk produced an answer")
	}
	if response != nil {
		if text := response.Text(); strings.ContainsRune(text, utf8.RuneError) ||
			strings.Contains(text, "intentionally") {
			t.Fatalf("the corrupt rescue was persisted as the turn: %q", text)
		}
	}
}

func TestRescuedStreamSanityReadsDecodePrintableAndFinish(t *testing.T) {
	reply := func(text, finish string) *ai.Response {
		return &ai.Response{Choices: []ai.Choice{{
			Message:      ai.Message{Content: []ai.ContentPart{{Type: "text", Text: text}}},
			FinishReason: finish,
		}}}
	}
	if err := rescuedStreamError(reply("the answer is four", "stop")); err != nil {
		t.Fatalf("a clean rescued reply was refused: %v", err)
	}
	if err := rescuedStreamError(reply(rescuedMojibake, "stop")); err == nil {
		t.Fatal("mojibake with a tidy finish_reason was accepted")
	}
	if err := rescuedStreamError(reply("the answer is four", "error")); err == nil {
		t.Fatal("finish_reason=error was accepted")
	}
	if err := rescuedStreamError(reply(strings.Repeat("\x00\x01", 40), "stop")); err == nil {
		t.Fatal("a non-printable stream was accepted")
	}
}
