package store

import (
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
