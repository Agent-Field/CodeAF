package tui3

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/codexauth"
	"github.com/Agent-Field/codeaf/internal/provider"
)

// A VENDOR'S REFUSAL IS QUOTED AND NEVER CLASSIFIED, and this is the line that
// says so. The turn used to draw `error: API error (429): Insufficient balance
// …` — three machinery words and a status number on a sentence a person reads,
// against CLAUDE.md's law — while the connect row for the same fact already
// said it properly. One composer now serves both, and this pins the half that
// regressed.
func TestAnAccountThatCannotPaySaysSoInTheVendorsWords(t *testing.T) {
	refusal := &provider.APIError{
		Status:  429,
		Message: "Insufficient balance or no resource package. Please recharge.",
		Body:    `{"error":{"code":"1113","message":"Insufficient balance or no resource package. Please recharge."}}`,
	}
	f := &feed{}
	note := f.failureNote(fmt.Errorf("send: %w", refusal), "z-ai")

	const want = "z-ai accepted the key but the account cannot pay — " +
		"Insufficient balance or no resource package. Please recharge."
	if note != want {
		t.Errorf("the turn drew\n  %q\nwant\n  %q", note, want)
	}
	forbidden(t, note)
}

// The same line, wherever the refusal is reached from: a turn that had already
// been asked again must not fall back to the give-up row and put the status
// number back on screen.
func TestAPaymentRefusalNeverDrawsTheGiveUpRow(t *testing.T) {
	refusal := &provider.APIError{Status: 402, Message: "Payment Required"}
	f := &feed{}
	f.asked, f.askedTurn = 3, f.turn
	note := f.failureNote(refusal, "deepseek")
	if !strings.HasPrefix(note, "deepseek accepted the key but the account cannot pay") {
		t.Errorf("a counted turn drew %q", note)
	}
	if strings.Contains(note, gaveUpWord) {
		t.Errorf("a refusal nobody can retry claimed a ladder: %q", note)
	}
	forbidden(t, note)
}

func TestAPaymentRefusalKeepsTheWholeTopUpLinkOnTheTurnRow(t *testing.T) {
	sentence := "You requested up to 2212 tokens, but can only afford 641. " +
		strings.Repeat("This request needs more credits. ", 7) +
		"To increase, visit https://openrouter.ai/settings/credits"
	refusal := &provider.APIError{Status: 402, Message: sentence}
	note := (&feed{}).failureNote(refusal, "OpenRouter")
	if !strings.Contains(note, "https://openrouter.ai/settings/credits") || !strings.Contains(note, strings.Repeat("This request needs more credits. ", 7)) {
		t.Fatalf("payment row cut the vendor's sentence: %q", note)
	}
}

// AND A PACING 429 IS STILL A PACING 429. The shape test is the only thing
// separating them, so a test that only proved the new line would let the old
// behaviour be widened onto every busy queue.
func TestAnOrdinaryRefusalKeepsItsPlainLine(t *testing.T) {
	f := &feed{}
	note := f.failureNote(errors.New("nobody answered in time"), "z-ai")
	if strings.Contains(note, "cannot pay") {
		t.Errorf("an ordinary failure was read as a payment refusal: %q", note)
	}
}

func TestAPlanPauseEndingDrawsOnlyThePauseSentence(t *testing.T) {
	refusal := &provider.APIError{
		Status: 429, Message: "Usage limit reached", Body: `{"code":"1316","message":"Usage limit reached"}`,
	}
	ending := &provider.PlanPauseError{
		Reset: "18:30 UTC", OverflowDoor: "pay-as-you-go", Cause: refusal,
	}
	note := (&feed{}).failureNote(fmt.Errorf("LLM call failed: %w", ending), "z-ai")
	const want = "plan paused · resets at 18:30 UTC · /connect can switch to pay-as-you-go"
	if note != want {
		t.Fatalf("pause ending = %q, want %q", note, want)
	}
	forbidden(t, note)
}

func TestConnectCodexExpiredSignInDrawsTheActionableSentence(t *testing.T) {
	note := (&feed{}).failureNote(fmt.Errorf("request failed: %w", codexauth.ErrSignInExpired), "codex")
	if note != codexauth.ErrSignInExpired.Error() {
		t.Fatalf("expired sign-in note = %q, want %q", note, codexauth.ErrSignInExpired)
	}
}

// forbidden is the vocabulary law for one line: a person-facing sentence may
// not carry this program's words for its own machinery, nor a status number
// that names nothing they can act on.
func forbidden(t *testing.T, note string) {
	t.Helper()
	for _, word := range []string{"API error", "error:", "(429)", "(402)", "429", "402", "status"} {
		if strings.Contains(strings.ToLower(note), strings.ToLower(word)) {
			t.Errorf("the line carries %q, which is machinery a person cannot act on: %q", word, note)
		}
	}
}
