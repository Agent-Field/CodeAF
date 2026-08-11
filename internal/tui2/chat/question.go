package chat

import (
	"strconv"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The question block (13.3.1, 5.20 rule 2).
//
// A question is the most expensive row this product can draw: a blocked human
// is the state 5.9 puts above every other. It is also the row v1 got wrong in
// the most instructive way — the producer smuggled its options through the
// message BODY, and v1's brace-scanner quietly hid the mess by parsing prose
// back into structure. v2 does not scan braces and never will: an option that
// is not in a field is an option this surface does not know about, and saying
// so honestly is what makes the producer fix itself.
//
// So everything here reads FIELDS. [store.Message.Options] is the ordered,
// selectable answer set; [store.MessagePart] of kind [store.PartQuestion] and
// [store.Message.QuestionSeq] are the two places a row admits to being a
// question at all. A message with neither is not a question no matter what its
// prose looks like.
//
// Two shapes, one rule — the affordance names the key you actually press:
//
//   - CONSENT (5.20 rule 2): a two-option yes/no question renders the inline
//     y/n strip, each key beside the consequence it buys. No modal, no vague
//     confirm; the blast radius is the question's own words, one row above.
//   - CHOICE: numbered rows, `1`..`9`, in the order the producer set them —
//     which is the order the answer router reads them back in, so the number on
//     screen IS the number to type.
//
// Neither shape is drawn as a button. Until the click layer lands (13.3), an
// affordance that looked pressable and was not would be the same lie the
// awaiting line's interrupt hint exists to avoid.

// questionOptionCap bounds how many options are drawn as rows. Past nine the
// single-keystroke answer stops existing, and a list that long is a menu the
// producer should have made a free-text question.
const questionOptionCap = 9

// consentKeys are the two keys the y/n strip names.
var consentKeys = [2]string{"y", "n"}

// dressQuestion draws the askback: the amber marker row that says a person is
// blocked, and then whichever option shape the message actually carries.
//
// It reports nothing: the caller has already counted the question for the
// footer's attention column, because "how many questions are open" is a fact
// about the thread and not about how this one is drawn.
func (b *messageBlock) dressQuestion(message store.Message) {
	b.segs = append(b.segs, segment{
		kind: segRef, glyph: tokens.GlyphNeedsHuman, text: "waiting on you",
		hue: blocks.HueAttention, state: blocks.StateSettled, indent: bodyIndent,
	})
	options := message.Options
	if len(options) == 0 {
		return
	}
	if keys, ok := consentShape(options); ok {
		for i, option := range options[:2] {
			b.segs = append(b.segs, segment{
				kind: segRef, glyph: keys[i], text: optionLabel(option),
				hue: blocks.HueAttention, state: blocks.StateSettled,
				indent: bodyIndent + optionIndent,
			})
		}
		return
	}
	for i, option := range options {
		if i >= questionOptionCap {
			b.segs = append(b.segs, segment{
				kind: segRef, glyph: tokens.GlyphCollapsed,
				text: strconv.Itoa(len(options)-questionOptionCap) + " more, answer in words",
				hue:  blocks.HueNone, state: blocks.StateChrome,
				indent: bodyIndent + optionIndent,
			})
			break
		}
		b.segs = append(b.segs, segment{
			kind: segRef, glyph: strconv.Itoa(i + 1), text: optionLabel(option),
			hue: blocks.HueAttention, state: blocks.StateSettled,
			indent: bodyIndent + optionIndent,
		})
	}
}

// optionIndent sets the options one step under the marker row that introduced
// them (5.13's two-space rhythm), so a question with options reads as one group
// rather than as four unrelated reference rows.
const optionIndent = 2

// optionLabel is what one option row says: the label, and its hint behind the
// telemetry separator when the producer wrote one. The Value is machine-facing
// continuation data and is never drawn (5.14).
func optionLabel(option store.QuestionOption) string {
	label := strings.TrimSpace(option.Label)
	if label == "" {
		label = strings.TrimSpace(option.Value)
	}
	if hint := strings.TrimSpace(option.Hint); hint != "" {
		label += " " + tokens.GlyphSeparator + " " + hint
	}
	return label
}

// consentShape recognizes the two-option consent question and returns the keys
// to name it with.
//
// The class axis lives on [store.AgentQuestion], not on the message, and this
// surface renders messages — so the SHAPE is what it reads: exactly two
// options, the first affirmative and the second negative. That is a narrower
// claim than the class and a safer one, because being wrong renders a numbered
// pair instead of the wrong keys.
//
// REQUESTED SEAM: a question message should carry its own class, so a consent
// question with unusually worded options is still drawn as one.
func consentShape(options []store.QuestionOption) ([2]string, bool) {
	if len(options) != 2 {
		return consentKeys, false
	}
	if !affirmative(options[0]) || !negative(options[1]) {
		return consentKeys, false
	}
	return consentKeys, true
}

func affirmative(option store.QuestionOption) bool {
	return matchesWord(option, "y", "yes", "ok", "okay", "approve", "confirm", "go ahead", "do it")
}

func negative(option store.QuestionOption) bool {
	return matchesWord(option, "n", "no", "cancel", "stop", "don't", "do not", "abort", "leave it")
}

func matchesWord(option store.QuestionOption, words ...string) bool {
	for _, field := range []string{option.Value, option.Label} {
		field = strings.ToLower(strings.TrimSpace(field))
		field = strings.TrimRight(field, ".!")
		for _, word := range words {
			if field == word {
				return true
			}
		}
	}
	return false
}
