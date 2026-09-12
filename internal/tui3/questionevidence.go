package tui3

import (
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// ── ONE ANSWER'S CASE, AND THE EVIDENCE IT BROUGHT ──────────────────────────
//
// A QUESTION IS A DECISION HANDED TO THE PERSON WITH ITS EVIDENCE ATTACHED
// (owner ruling 2026-09-09), and the evidence is per answer: what it means, what
// taking it leaves true, why the asker would take it, what would change its
// mind, how sure it is, and whatever it drew — a diagram, a diff, a table, a
// layout, a picture. The owner's picks of 2026-09-11 draw that evidence in three
// places:
//
//   - beside the list, in a pane that follows the pointer (preview A, a panel at
//     a hundred columns and wider, and page A, the page `o` opens);
//   - under the answer the pointer is on (preview-narrow B, the same panel
//     narrower than that, and the page on a narrow frame);
//   - folded to one line under the pointer, on a panel whose answers brought
//     nothing to look at (rec A: `already a dependency · fairly sure`).
//
// ALL THREE READ ONE ACCOUNT OF THE CASE ([questionCase]) AND DRAW BLOCKS
// THROUGH ONE RENDERER ([app.questionBlockRows]). The page used to spell the
// labelled lines itself and the panel spelled its one-line case itself, so the
// two disagreed about whether `would switch if` joined a model's `If you…` twice
// — which the page had fixed and the panel had not.

// questionCaseLine is one labelled line of an answer's case: the word that says
// which line it is, and what the asker said.
type questionCaseLine struct {
	label string
	text  string
	// pick is true for the lines only the asker's own pick carries. The one-line
	// fold under a panel's pointer says only those, because the answer's row is
	// already carrying what it leaves true beside its word.
	pick bool
}

// questionConfidenceLabel labels how sure the asker is, on the rows that say
// each line's word in front of it.
const questionConfidenceLabel = "confidence · "

// questionCase is one answer's case, in the order a person weighs it: what
// taking it leaves true, why the asker would take it, what would change its
// mind, and how sure it is.
//
// THE EMPTINESS LAW. A line with nothing the asker said is no line at all —
// never an empty one, never the word "unknown" — and the last three exist only
// on the answer the asker picked, because a reason is a reason FOR something.
func questionCase(q session.Question, at int) []questionCaseLine {
	if at < 0 || at >= len(q.Options) {
		return nil
	}
	out := make([]questionCaseLine, 0, 4)
	if then := strings.TrimSpace(q.Options[at].Consequence); then != "" {
		out = append(out, questionCaseLine{label: questionThenWord, text: then})
	}
	if q.Pick == nil || strings.TrimSpace(q.Pick.Key) != questionOptionKeyAt(q, at) {
		return out
	}
	if why := strings.TrimSpace(q.Pick.Reason); why != "" {
		out = append(out, questionCaseLine{label: questionWhyWord, text: why, pick: true})
	}
	if change := strings.TrimSpace(q.Pick.WouldChange); change != "" {
		out = append(out, questionCaseLine{label: questionWouldSwitchWord, text: questionAfterIf(change), pick: true})
	}
	if sure := questionConfidenceWord(q.Pick.Confidence); sure != "" {
		out = append(out, questionCaseLine{label: questionConfidenceLabel, text: sure, pick: true})
	}
	return out
}

// questionPickCase is the asker's case for its pick on ONE line — rec A's fold
// under a panel's pointer: `survives a crash mid-write · fairly sure · would
// switch if …`. It is [questionCase]'s pick lines joined, with the one label a
// bare clause cannot do without.
func questionPickCase(q session.Question, key string) string {
	at, ok := questionOptionAt(q, key)
	if !ok {
		return ""
	}
	parts := make([]string, 0, 3)
	for _, line := range questionCase(q, at) {
		switch {
		case !line.pick:
		case line.label == questionWouldSwitchWord:
			parts = append(parts, line.label+line.text)
		default:
			parts = append(parts, line.text)
		}
	}
	return strings.Join(parts, " · ")
}

// questionEvidenceRows draws one answer's evidence at `width` cells: its word
// (where `head` asks for it — a pane beside the list has to say whose evidence
// it is, a fold under the answer's own row does not), what it means in the
// reading ink, the labelled case dim, and every block it brought, a blank row
// above each.
//
// THE `something else…` ROW HAS NO EVIDENCE, and draws none: it is the person's
// answer, and there is nothing the asker said about it.
func (a *app) questionEvidenceRows(q session.Question, at, width int, head bool) []string {
	if at < 0 || at >= len(q.Options) || width <= 0 {
		return nil
	}
	option := q.Options[at]
	out := make([]string, 0, 12)
	if head {
		word := strings.TrimSpace(option.Label)
		if word == "" {
			word = questionOptionKeyAt(q, at)
		}
		for _, line := range wrap(word, width) {
			out = append(out, a.pal.bold(a.pal.ink(line)))
		}
	}
	// AN ANSWER WITH NO BODY DRAWS NO ROW FOR ONE. [wrap] answers an empty
	// string with one empty line, which is right for a paragraph and wrong here,
	// where it would be a blank row claiming the asker said something.
	if body := strings.TrimSpace(option.Body); body != "" {
		for _, line := range wrap(body, width) {
			out = append(out, a.pal.ink(line))
		}
	}
	// EVERY DIM LINE SAYS WHICH LINE IT IS. Four grey sentences stacked in one
	// column read as one grey paragraph nobody can tell apart (the owner,
	// 2026-09-10: "the same line and next line in options look same"), so what
	// an answer MEANS is ink and what it costs and why the asker would take it
	// are dim AND labelled.
	case_ := questionCase(q, at)
	if len(case_) > 0 && len(out) > 0 {
		out = append(out, "")
	}
	for _, line := range case_ {
		for _, row := range wrap(line.label+line.text, width) {
			out = append(out, a.pal.dim(row))
		}
	}
	for _, block := range option.Blocks {
		if len(out) > 0 {
			out = append(out, "")
		}
		out = append(out, a.questionBlockRows(block, 0, width)...)
	}
	return out
}

// questionCut keeps at most `room` rows of evidence, and says how much it left
// out on the last row it keeps.
//
// A CUT IS SAID, NEVER SILENT: evidence that stops mid-diagram with nothing
// under it reads as a diagram that ends there. The foot names the count and
// the way to the rest — the page `o` opens, where the same evidence has the
// height of the frame.
func (a *app) questionCut(rows []string, room int, offer string) []string {
	if room <= 0 || len(rows) <= room {
		if room <= 0 {
			return nil
		}
		return rows
	}
	kept := append([]string{}, rows[:room-1]...)
	more := a.icon(tokens.GEllipsis) + " " + itoa(len(rows)-room+1) + questionMoreWord
	if offer != "" {
		more += questionKeyGap + offer
	}
	return append(kept, a.pal.dim(more))
}
