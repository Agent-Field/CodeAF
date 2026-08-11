package chat

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/footer"
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

// -- answering (JOURNEY 6) -----------------------------------------------------

// askedApp is a window with one open question in it.
func askedApp(t *testing.T, options ...store.QuestionOption) (*App, *fakeBackend) {
	t.Helper()
	backend := &fakeBackend{}
	backend.add(askback(options...))
	app := newTestApp(backend, &fakeCommander{}, nil)
	poll(t, app)
	return app, backend
}

// The gap this closes: the row said "1 the CSV importer" and the digit fell
// into the draft. A named key that does nothing is 5.20 rule 3 broken by the
// affordance that names it.
func TestADigitAnswersTheOpenQuestion(t *testing.T) {
	app, backend := askedApp(t,
		store.QuestionOption{Label: "the CSV importer", Value: "csv"},
		store.QuestionOption{Label: "the JSON importer", Value: "json"},
	)
	msg := press(app, "2")
	if msg == nil {
		t.Fatal("a digit over an open question did nothing")
	}
	if draft := app.composer.Draft(); draft != "" {
		t.Fatalf("the digit fell into the draft: %q", draft)
	}
	if _, ok := msg.(postResultMsg); !ok {
		t.Fatalf("answering produced %#v", msg)
	}
	posted := backend.posted[len(backend.posted)-1]
	if posted.Body != "json" {
		t.Fatalf("the answer said %q, want the option's value", posted.Body)
	}
	// A bare "2" is the right answer only by luck once a second question is
	// open: what the reader pointed at goes with the words.
	if posted.QuestionSeq != 42 {
		t.Fatalf("the answer aims at question %d, want 42", posted.QuestionSeq)
	}
	if posted.Role != store.RoleUser {
		t.Fatalf("the answer was journaled as %q", posted.Role)
	}
}

// One question takes one answer. A second keystroke on a row already answered
// would post a second reply to a question that has one.
func TestAQuestionTakesOneAnswer(t *testing.T) {
	app, _ := askedApp(t,
		store.QuestionOption{Label: "yes please", Value: "a"},
		store.QuestionOption{Label: "no thanks", Value: "b"},
		store.QuestionOption{Label: "later", Value: "c"},
	)
	if msg := press(app, "1"); msg == nil {
		t.Fatal("the first answer did nothing")
	}
	if msg := press(app, "2"); msg != nil {
		t.Fatalf("an answered question took a second answer: %#v", msg)
	}
}

// A draft in progress keeps its digits. The reader is mid-sentence, and a
// number in a sentence is a number.
func TestADraftInProgressKeepsItsDigits(t *testing.T) {
	app, _ := askedApp(t,
		store.QuestionOption{Label: "the CSV importer", Value: "csv"},
		store.QuestionOption{Label: "the JSON importer", Value: "json"},
	)
	press(app, "n")
	press(app, "o")
	if draft := app.composer.Draft(); draft != "no" {
		t.Fatalf("draft = %q, want the two letters typed", draft)
	}
	press(app, "2")
	if draft := app.composer.Draft(); draft != "no2" {
		t.Fatalf("a digit was stolen from a draft in progress: %q", draft)
	}
}

// 5.20 rule 2's inline strip, on the gate it was written for. The real consent
// options read "yes, start it" and "hold it — I'll trim it first"
// (internal/consent), which an exact whole-label match never recognized.
func TestTheRealConsentGateGetsTheYesNoStrip(t *testing.T) {
	options := []store.QuestionOption{
		{Label: "yes, start it", Value: "approve"},
		{Label: "hold it — I'll trim it first", Value: "hold"},
	}
	if !consentShape(options, nil) {
		t.Fatal("the real consent gate does not render as a consent question")
	}
	app, backend := askedApp(t, options...)
	out := whole(t, app, 80)
	for _, want := range []string{"y yes, start it", "n hold it"} {
		if !strings.Contains(out, want) {
			t.Fatalf("the y/n strip is missing %q:\n%s", want, out)
		}
	}
	if msg := press(app, "y"); msg == nil {
		t.Fatal("y did not answer the consent gate")
	}
	if posted := backend.posted[len(backend.posted)-1]; posted.Body != "approve" {
		t.Fatalf("y answered with %q", posted.Body)
	}
}

// The producer's own word outranks the heuristic: a part that says this is a
// numbered choice is a numbered choice, whatever its labels start with.
func TestTheQuestionPartOutranksTheWordHeuristic(t *testing.T) {
	options := []store.QuestionOption{{Label: "yes, start it"}, {Label: "no, stop"}}
	if !consentShape(options, nil) {
		t.Fatal("two yes/no labels are not recognized without a part")
	}
	choose := &store.QuestionPart{Kind: store.QuestionChoose}
	if consentShape(options, choose) {
		t.Fatal("a part saying choose was overruled by the labels")
	}
	confirm := &store.QuestionPart{Kind: store.QuestionConfirm}
	if !consentShape([]store.QuestionOption{{Label: "left"}, {Label: "right"}}, confirm) {
		t.Fatal("a part saying confirm was ignored")
	}
}

// A word further into a label is not what a key stands for.
func TestOnlyTheLeadingWordDecidesAConsentLabel(t *testing.T) {
	options := []store.QuestionOption{
		{Label: "rewrite it and say yes when done"},
		{Label: "leave it running, no rush"},
	}
	if consentShape(options, nil) {
		t.Fatal("a label that merely contains yes/no was read as a consent gate")
	}
}

// Arrows walk the options and enter takes the one under the cursor.
func TestArrowsWalkTheOptionsAndEnterAnswers(t *testing.T) {
	app, backend := askedApp(t,
		store.QuestionOption{Label: "the CSV importer", Value: "csv"},
		store.QuestionOption{Label: "the JSON importer", Value: "json"},
	)
	press(app, "down")
	block := app.openAsk()
	if block == nil || block.chosen != 1 {
		t.Fatalf("down did not put the cursor on the first option: %#v", block)
	}
	press(app, "down")
	if block.chosen != 2 {
		t.Fatalf("the cursor is on %d, want 2", block.chosen)
	}
	if !strings.Contains(whole(t, app, 80), tokens.GlyphAccentRail+" the JSON importer") {
		t.Fatalf("the cursor is not visible:\n%s", whole(t, app, 80))
	}
	if msg := press(app, "enter"); msg == nil {
		t.Fatal("enter over a chosen option did nothing")
	}
	if posted := backend.posted[len(backend.posted)-1]; posted.Body != "json" {
		t.Fatalf("enter answered with %q", posted.Body)
	}
}

// 5.22's digit precedence, said out loud: the footer names what a bare number
// does right now, from exactly the state the key ladder consults.
func TestTheFooterSaysWhatADigitDoes(t *testing.T) {
	app, _ := askedApp(t,
		store.QuestionOption{Label: "the CSV importer", Value: "csv"},
		store.QuestionOption{Label: "the JSON importer", Value: "json"},
	)
	if app.status.keyMode != footer.KeyModeAnswer || app.status.keyCount != 2 {
		t.Fatalf("the footer says mode %d over %d rows", app.status.keyMode, app.status.keyCount)
	}
	// A consent gate names y and n, so the digit column would be advertising
	// keys that do nothing.
	consent, _ := askedApp(t,
		store.QuestionOption{Label: "yes, start it", Value: "approve"},
		store.QuestionOption{Label: "hold it — I'll trim it first", Value: "hold"},
	)
	if consent.status.keyMode == footer.KeyModeAnswer {
		t.Fatal("a consent gate advertised digits")
	}
}
