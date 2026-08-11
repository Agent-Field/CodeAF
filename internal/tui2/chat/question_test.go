package chat

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// 13.3.1: question machinery leaked raw into the transcript because the
// producer smuggled options through the body and v1's brace-scanner hid it.
// These tests fix the rule rather than the symptom: what is drawn comes from
// FIELDS, and prose is never parsed back into structure.

func askback(options ...store.QuestionOption) store.Message {
	return store.Message{
		SessionID:   testSession,
		Role:        store.RoleAgent,
		Body:        "Which importer should I rewrite first?",
		QuestionSeq: 42,
		Options:     options,
		Parts:       []store.MessagePart{store.QuestionRef(42)},
	}
}

func askedRows(t *testing.T, message store.Message) string {
	t.Helper()
	backend := &fakeBackend{}
	backend.add(message)
	app := newTestApp(backend, &fakeCommander{}, nil)
	poll(t, app)
	return whole(t, app, 80)
}

// A choice question renders numbered rows, in the producer's order — which is
// the order the answer router reads them back in, so the number on screen IS
// the number to type.
func TestAChoiceQuestionRendersNumberedOptions(t *testing.T) {
	out := askedRows(t, askback(
		store.QuestionOption{Label: "the CSV importer", Value: "csv"},
		store.QuestionOption{Label: "the JSON importer", Value: "json"},
		store.QuestionOption{Label: "neither", Value: "none", Hint: "I will explain"},
	))
	if !strings.Contains(out, tokens.GlyphNeedsHuman+" waiting on you") {
		t.Fatalf("the askback does not say a person is blocked:\n%s", out)
	}
	for _, want := range []string{"1 the CSV importer", "2 the JSON importer", "3 neither"} {
		if !strings.Contains(out, want) {
			t.Fatalf("the option row %q is missing:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "I will explain") {
		t.Fatalf("an option's hint was dropped:\n%s", out)
	}
	// The machine-facing continuation data is never drawn (5.14).
	for _, value := range []string{"csv", "json", "none"} {
		if strings.Contains(out, "\n"+value) || strings.Contains(out, " "+value+"\n") {
			t.Fatalf("the option VALUE %q reached the screen:\n%s", value, out)
		}
	}
}

// 5.20 rule 2: a consent question gets the inline y/n strip, each key beside
// the consequence it buys. No modal, no vague confirm.
func TestAConsentQuestionRendersTheInlineYesNoStrip(t *testing.T) {
	out := askedRows(t, askback(
		store.QuestionOption{Label: "yes", Value: "yes"},
		store.QuestionOption{Label: "no", Value: "no"},
	))
	if !strings.Contains(out, "y yes") || !strings.Contains(out, "n no") {
		t.Fatalf("the consent strip is not on screen:\n%s", out)
	}
	if strings.Contains(out, "1 yes") {
		t.Fatalf("a consent question was numbered instead of keyed:\n%s", out)
	}
}

// A consent question whose options are worded rather than spelled y/n is still
// recognized by SHAPE, which is the narrower and safer claim: being wrong
// renders a numbered pair instead of the wrong keys.
func TestAWordedConsentQuestionIsStillConsent(t *testing.T) {
	out := askedRows(t, askback(
		store.QuestionOption{Label: "Go ahead", Value: "approve"},
		store.QuestionOption{Label: "Cancel", Value: "cancel"},
	))
	if !strings.Contains(out, "y Go ahead") || !strings.Contains(out, "n Cancel") {
		t.Fatalf("a worded consent pair lost its keys:\n%s", out)
	}
}

// Two options that are not a yes/no pair are a CHOICE, and drawing them as
// consent would name keys that do not answer them.
func TestATwoWayChoiceIsNotConsent(t *testing.T) {
	out := askedRows(t, askback(
		store.QuestionOption{Label: "the CSV importer", Value: "csv"},
		store.QuestionOption{Label: "the JSON importer", Value: "json"},
	))
	if strings.Contains(out, "y the CSV") {
		t.Fatalf("a two-way choice was drawn as consent:\n%s", out)
	}
	if !strings.Contains(out, "1 the CSV importer") {
		t.Fatalf("a two-way choice was not numbered:\n%s", out)
	}
}

// A free-text question carries no options, so it renders the marker row and
// nothing else — the composer is the only instruction there is.
func TestAFreeTextQuestionRendersOnlyTheMarker(t *testing.T) {
	out := askedRows(t, askback())
	if !strings.Contains(out, "waiting on you") {
		t.Fatalf("a free-text question does not say it is waiting:\n%s", out)
	}
	if strings.Contains(out, "1 ") {
		t.Fatalf("a free-text question invented options:\n%s", out)
	}
}

// A question that arrived before the parts model still renders: its lifecycle
// sequence and its options are fields like any other. What is NOT read is the
// body — that is the whole of 13.3.1's rule.
func TestALegacyQuestionWithoutPartsStillRenders(t *testing.T) {
	message := askback(store.QuestionOption{Label: "carry on", Value: "yes"},
		store.QuestionOption{Label: "stop", Value: "no"})
	message.Parts = nil
	out := askedRows(t, message)
	if !strings.Contains(out, "y carry on") || !strings.Contains(out, "n stop") {
		t.Fatalf("a legacy question lost its options:\n%s", out)
	}
}

// The user's own answer is not an askback. A reply that carries the question's
// sequence is the ANSWER, and drawing an amber marker on it would say a person
// is blocked by the sentence they just typed.
func TestTheUsersAnswerIsNotDrawnAsAQuestion(t *testing.T) {
	backend := &fakeBackend{}
	backend.add(store.Message{SessionID: testSession, Role: store.RoleUser,
		Body: "1", QuestionSeq: 42})
	app := newTestApp(backend, &fakeCommander{}, nil)
	poll(t, app)
	out := whole(t, app, 80)
	if strings.Contains(out, "waiting on you") {
		t.Fatalf("the reader's own answer was drawn as an askback:\n%s", out)
	}
	if app.openQuestions() != 0 {
		t.Fatalf("the reader's answer counted as an open question")
	}
}

// A question with more options than a single keystroke can reach says so
// rather than numbering rows nobody can press.
func TestAnOverlongOptionListSaysWhatItHeldBack(t *testing.T) {
	options := make([]store.QuestionOption, 0, 12)
	for i := 0; i < 12; i++ {
		options = append(options, store.QuestionOption{Label: "option " + string(rune('a'+i))})
	}
	out := askedRows(t, askback(options...))
	if !strings.Contains(out, "9 option i") {
		t.Fatalf("the ninth option is missing:\n%s", out)
	}
	if strings.Contains(out, "10 option j") {
		t.Fatalf("an unreachable option was numbered:\n%s", out)
	}
	if !strings.Contains(out, "3 more, answer in words") {
		t.Fatalf("the held-back options are unaccounted for:\n%s", out)
	}
}

// The open-question count the footer paints amber is read off the same fact.
func TestAnAskbackFeedsTheAttentionCount(t *testing.T) {
	backend := &fakeBackend{}
	backend.add(askback(store.QuestionOption{Label: "yes"}, store.QuestionOption{Label: "no"}))
	app := newTestApp(backend, &fakeCommander{}, nil)
	poll(t, app)
	if got := app.openQuestions(); got != 1 {
		t.Fatalf("the attention count is %d", got)
	}
}
