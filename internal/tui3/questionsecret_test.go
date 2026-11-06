package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

// A CREDENTIAL TYPED INTO A SECRET BOX IS NEVER A DRAFT AND NEVER A RECEIPT.
//
// Observed 2026-11-05 driving request_secret in a real terminal: the token was
// masked while typed, but the debounced draft wrote it whole to
// v3/draft-*.txt, and the answer's receipt drew `with: ghp_…` on screen.
func TestASecretBoxIsNeitherDraftedNorDrawnBack(t *testing.T) {
	const token = "ghp_FAKEtuiToken0123456789abcdef"
	lab := newQuestionLab(t)
	lab.raise(session.Question{
		ID: 11, Kind: session.QuestionAsk, Ask: session.AskClarification,
		Head: "GitHub token", Reason: "to call the API", Stakes: session.StakesReversible,
		Options: []session.AnswerOption{{Key: session.SecretDeclineKey, Label: session.SecretDeclineLabel}},
		Input:   session.InputShape{Kind: session.InputText, Secret: true},
	})
	lab.tick(questionSettle)
	lab.rows()
	lab.a.input.insert(token)
	if !lab.a.typingSecret() {
		t.Fatal("the box over a secret question does not read as a secret box")
	}
	if got := lab.a.mainDraftText(); got != "" {
		t.Fatalf("the secret is offered to the draft file: %q", got)
	}
	if !lab.press("enter") {
		t.Fatal("enter did not answer the secret question")
	}
	if len(lab.answer) != 1 || lab.answer[0].Change != token {
		t.Fatalf("the engine was handed %+v, want the typed token", lab.answer)
	}
	screen := lab.plain()
	if strings.Contains(screen, token) || strings.Contains(screen, "ghp_") {
		t.Fatalf("the receipt draws the secret back:\n%s", screen)
	}
	if !strings.Contains(screen, questionSecretGiven) {
		t.Fatalf("the receipt does not say the secret was given:\n%s", screen)
	}
	if lab.a.typingSecret() {
		t.Fatal("the box still reads as a secret box once the question is answered")
	}
}
