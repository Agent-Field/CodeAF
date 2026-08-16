package store

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// 13.5 bug 6 — the producer's half of 13.3 bug 1.
//
// The render half was already honest: a surface that draws Message.Options as a
// question block draws exactly the options the row carries. The producer was
// not. A question whose SENTENCE listed its own choices handed that surface two
// copies of one fact — the sentence, and the block under it — and the second
// copy is the one the reader distrusts, because an agent that says a thing twice
// is an agent that has lost track of what it said.
//
// These tests hold both ends: the prompt a question part carries never lists
// options, and the body a question is journaled with never stops carrying them,
// because 11.1's chat reads bodies and only bodies.

// v1QuestionFromBody is the existing chat's numbered reader, restated here as
// the compatibility oracle it is. It mirrors internal/tui's numberedQuestionPayload
// and parseQuestionOptionSegment exactly — prose is what precedes the first
// marker on a line, an option is "N label" — so a body that satisfies this test
// is a body the surface this package must not break can still draw as choices.
func v1QuestionFromBody(body string) (string, []string) {
	var prose []string
	var labels []string
	for _, line := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		first := strings.Index(line, "▸")
		if first < 0 {
			prose = append(prose, line)
			continue
		}
		if prefix := strings.TrimSpace(line[:first]); prefix != "" {
			prose = append(prose, prefix)
		}
		rest := line[first:]
		for rest != "" {
			rest = strings.TrimSpace(strings.TrimPrefix(rest, "▸"))
			segment := rest
			if next := strings.Index(rest, "▸"); next >= 0 {
				segment, rest = rest[:next], rest[next:]
			} else {
				rest = ""
			}
			fields := strings.Fields(strings.TrimSpace(segment))
			if len(fields) < 2 {
				continue
			}
			if _, err := strconv.Atoi(strings.TrimRight(fields[0], ".):")); err != nil {
				continue
			}
			labels = append(labels, strings.TrimSpace(segment[len(fields[0]):]))
		}
	}
	return strings.TrimSpace(strings.Join(prose, "\n")), labels
}

// questionTextPart is the prose a surface draws for a question message: the
// text part when the message is parts-native, which is what v2 renders and what
// the body is suppressed in favour of.
func questionTextPart(t *testing.T, parts []MessagePart) string {
	t.Helper()
	said := make([]string, 0, len(parts))
	block := false
	for _, part := range parts {
		switch part.Kind {
		case PartText:
			said = append(said, part.Text)
		case PartQuestion:
			block = true
		}
	}
	if !block {
		t.Fatalf("question message carries no question part: %+v", parts)
	}
	return strings.Join(said, "\n")
}

func TestAQuestionThatSaysItsOptionsInItsSentenceStillDrawsThemOnce(t *testing.T) {
	// The exact shape 13.5 caught, at the exact wording it caught it at: the
	// options inside the sentence AND in the typed column beside it.
	question := AgentQuestion{
		Seq:  91,
		Text: "Should I keep watching this when you're not here? ▸ 1 yes, always · ▸ 2 only while I'm around",
		Options: []QuestionOption{
			{Label: "yes, always", Value: "standing-watch:enable"},
			{Label: "only while I'm around", Value: "standing-watch:decline"},
		},
	}
	prose := questionTextPart(t, PartsForQuestion(question))
	if strings.Contains(prose, "▸") || strings.Contains(prose, "yes, always") ||
		strings.Contains(prose, "only while I'm around") {
		t.Fatalf("the question part re-listed its options as prose: %q", prose)
	}
	if prose != "Should I keep watching this when you're not here?" {
		t.Fatalf("prompt = %q, want the sentence alone", prose)
	}
}

func TestTheOneOptionPerLineSpellingStillLeavesOnlyTheSentence(t *testing.T) {
	options := []QuestionOption{{Label: "keep going"}, {Label: "deliver what landed"}}
	body := HumaneQuestionBody("It has spent half its bound. Keep going?", options)
	if prompt := QuestionPrompt(body); prompt != "It has spent half its bound. Keep going?" {
		t.Fatalf("prompt = %q", prompt)
	}
}

func TestProseThatMerelyCarriesTheGlyphIsNotMistakenForOptions(t *testing.T) {
	// The decoder is a decoder. A sentence that uses the marker as punctuation
	// is not a list, and losing half of it would be a worse bug than the one
	// this file exists to close.
	body := "▸ asked → answered · what the run cost"
	if prompt := QuestionPrompt(body); prompt != body {
		t.Fatalf("prompt = %q, want the line kept whole", prompt)
	}
}

func TestTheStandingWatchOfferSaysItsChoicesInFieldsAndKeepsTheOldBody(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "standing-watch-parts.db"))
	charter := mustTestCharter(t, "watched-charter", CharterActive, CharterRails{
		PerFiringBudgetUSD: 0.10, MaxFiringsPerDay: 3,
	})
	if err := graph.CreateCharter(charter); err != nil {
		t.Fatal(err)
	}
	if posted, err := graph.OfferStandingWatch("watch-session", charter.ID); err != nil || !posted {
		t.Fatalf("offer posted=%t err=%v", posted, err)
	}
	messages, err := graph.Messages("watch-session", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	var offer Message
	for _, message := range messages {
		if message.QuestionSeq != 0 {
			offer = message
		}
	}
	if offer.QuestionSeq == 0 {
		t.Fatalf("no standing-watch question message in %+v", messages)
	}

	// The new surface: the sentence once, and the options only in the column a
	// question block is drawn from.
	prose := questionTextPart(t, offer.Parts)
	if strings.Contains(prose, "▸") || strings.Contains(prose, "yes, always") {
		t.Fatalf("the offer smuggled its options into its prose: %q", prose)
	}
	if len(offer.Options) != 2 || offer.Options[0].Value != "standing-watch:enable" {
		t.Fatalf("options = %+v", offer.Options)
	}

	// The old surface (11.1): unchanged prompt, and both choices still readable
	// out of the body it has always read them out of.
	prompt, labels := v1QuestionFromBody(offer.Body)
	if prompt != "Should I keep watching this when you're not here?" {
		t.Fatalf("old chat would draw the prompt as %q", prompt)
	}
	if len(labels) != 2 || labels[0] != "yes, always" || labels[1] != "only while I'm around" {
		t.Fatalf("old chat would draw the choices as %q", labels)
	}
}

func TestTheStandDownAskSurfacesWithItsChoicesOnlyInFields(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "stand-down-parts.db"))
	charter := mustTestCharter(t, "stood-charter", CharterActive, CharterRails{
		PerFiringBudgetUSD: 0.10, MaxFiringsPerDay: 3,
	})
	if err := graph.CreateCharter(charter); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.OfferStandingWatch("watch-session", charter.ID); err != nil {
		t.Fatal(err)
	}
	if err := graph.RecordStandingWatchDecision(StandingWatchEnabled, "yes, always"); err != nil {
		t.Fatal(err)
	}
	if offered, err := graph.OfferStandingWatchStandDown("watch-session"); err != nil || !offered {
		t.Fatalf("stand-down offered=%t err=%v", offered, err)
	}
	pending, err := graph.PendingQuestions("watch-session", 10)
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending = %+v err=%v", pending, err)
	}
	message, err := graph.SurfaceQuestion(pending[0].Seq)
	if err != nil {
		t.Fatal(err)
	}
	prose := questionTextPart(t, message.Parts)
	if strings.Contains(prose, "▸") || strings.Contains(prose, "stand down") {
		t.Fatalf("the stand-down ask smuggled its options into its prose: %q", prose)
	}
	prompt, labels := v1QuestionFromBody(message.Body)
	if !strings.HasSuffix(prompt, "Keep watching?") {
		t.Fatalf("old chat would draw the prompt as %q", prompt)
	}
	if len(labels) != 2 || labels[0] != "keep watching" || labels[1] != "stand down" {
		t.Fatalf("old chat would draw the choices as %q", labels)
	}
}
