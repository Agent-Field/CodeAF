package session

import (
	"encoding/json"
	"sort"
	"strings"
	"unicode"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/approval"
)

// ── the turn's own observations, as evidence for the reply check ────────────
//
// A LOOK IS NOT THE ONLY THING A TURN SEES. On the connected-canvas qloop (runs
// 16 and 17, deepseek-v4.1-flash) the largest cause left after the look check
// (#2836) was a reply that stated something no look covered, while the turn
// had already been told the truth by its own steps: "fits one page" after
// `pdfinfo` printed `Pages: 2`; "top recall (97.0%)" after a cell read and a
// look both said Milvus is 0.98; "Rejected up to 4" after a count that said
// Rejected was already 4 before the move. The reader was shown the looks and
// asked about the newest one only, so none of those could be raised.
//
// SO THE READER IS NOW SHOWN WHAT THE TURN OBSERVED, NOT ONLY WHAT IT LOOKED
// AT: the results its own steps returned — a shell line's output, an export's
// answer, a file it read back — beside the looks, oldest first. It is the
// turn's own transcript; nothing is run to get it, and the one reader call
// stays one call.
//
// EVERYTHING THE TURN SAW, NOT ONLY WHAT IT SAW SINCE ITS LAST CHANGE. The
// first sketch kept only outputs after the last step that may have changed the
// work, and it would have caught none of the three: every shell line counts as
// a change ([approval.ReadOnly] cannot read one), and the 0.98 was read before
// a patch that only respelled a sentence. An observation from before a later
// step is kept and marked as such, and the reader is told that where two
// disagree the later one wins.
//
// THE BILL IS CAPPED AND THE PICK IS CHEAP. An observation earns a place by
// sharing words with the reply — the names, labels and figures it states
// ([replyEvidenceTerms]) — minus the words nearly every output of the turn
// carries, so `canvas` and `link` select nothing. Of those that share any, the
// newest are kept first under [replyEvidenceBytes]; one long output is cut
// down to the stretches around what it shares ([replyEvidenceOneBytes]).
// What the harness wrote for the model to read — the manual, a skill's own
// pages — is instruction, not observation, and is never offered.

const (
	// replyEvidenceBytes bounds the observations the reader is shown, all of
	// them together, beside the looks' own [replyCheckLookBytes].
	replyEvidenceBytes = 3500
	// replyEvidenceOneBytes bounds one observation; a longer one is cut to the
	// stretches around the words it shares with the reply. It is sized to keep
	// a five-row, six-column sheet read whole (run 17's was 1,873 bytes): the
	// figure a reply gets wrong sits in a cell far from the name it belongs to.
	replyEvidenceOneBytes = 2000
	// replyEvidenceSpan is how much of a long output is kept on each side of a
	// word it shares with the reply.
	replyEvidenceSpan = 160
	// replyEvidenceCommandBytes bounds the line that names the step.
	replyEvidenceCommandBytes = 160
)

// turnObservation is one result this turn's own steps returned that is not a
// look: where its call sits, what ran, what came back, and whether a step that
// may change the work ran after it.
type turnObservation struct {
	at     int
	order  int
	step   string
	result string
	before bool
}

// observedThisTurn reads this turn — from the person's message to the end —
// for the results of its own steps that are not looks. A look is
// [lookedAtTheWork]'s; an empty result, and the harness's own pages, are
// nothing to compare against.
func observedThisTurn(messages []ai.Message) []turnObservation {
	from := personTurnOpenedAt(messages)
	results := map[string]string{}
	for _, message := range messages[from:] {
		if message.Role == "tool" {
			results[strings.TrimSpace(message.ToolCallID)] = strings.TrimSpace(partsText(message))
		}
	}
	var observed []turnObservation
	lastChange := -1
	order := 0
	for index := from; index < len(messages); index++ {
		message := messages[index]
		if message.Role != "assistant" {
			continue
		}
		for _, call := range message.ToolCalls {
			order++
			name := strings.TrimSpace(call.Function.Name)
			result := results[strings.TrimSpace(call.ID)]
			if name == "view_image" || lookWords(result) != "" {
				continue
			}
			if !approval.ReadOnly(name, json.RawMessage(call.Function.Arguments)) {
				lastChange = order
			}
			step := observedStep(name, call.Function.Arguments)
			if result == "" || isHarnessPage(name, step) {
				continue
			}
			observed = append(observed, turnObservation{at: index, order: order, step: step, result: result})
		}
	}
	for index := range observed {
		observed[index].before = observed[index].order < lastChange
	}
	return observed
}

// observedStep names a step the way the reader can place it: the tool, and its
// command or path, on one line.
func observedStep(name, arguments string) string {
	what := ""
	for _, key := range []string{"command", "path", "file_path", "pattern", "query"} {
		if value := strings.TrimSpace(checkpointArgumentNamed(arguments, key)); value != "" {
			what = value
			break
		}
	}
	what = strings.Join(strings.Fields(what), " ")
	if what == "" {
		return name
	}
	return name + " " + clip(what, replyEvidenceCommandBytes)
}

// isHarnessPage reports a step that read what codeaf wrote for the model to
// follow — the manual, or a skill's own pages — rather than the work.
func isHarnessPage(name, step string) bool {
	if name == "manual" {
		return true
	}
	lower := strings.ToLower(step)
	return strings.Contains(lower, "skill.md") || strings.Contains(lower, "/skills/")
}

// replyEvidenceStop is the short words a reply shares with every output and
// that select nothing.
var replyEvidenceStop = map[string]bool{
	"the": true, "and": true, "for": true, "you": true, "your": true, "are": true, "but": true,
	"not": true, "can": true, "all": true, "its": true, "was": true, "has": true, "had": true,
	"our": true, "out": true, "any": true, "now": true, "new": true, "with": true, "from": true,
	"that": true, "this": true, "then": true, "than": true, "they": true, "them": true,
	"what": true, "when": true, "will": true, "would": true, "there": true, "their": true,
	"here": true, "also": true, "just": true, "only": true, "more": true, "each": true,
	"into": true, "like": true, "been": true, "were": true, "which": true, "does": true,
	"done": true, "made": true, "make": true, "say": true, "word": true, "want": true,
	"it's": true, "i'll": true, "i've": true, "how": true, "who": true, "why": true,
	"same": true, "still": true, "both": true, "every": true, "after": true,
	"before": true, "over": true, "under": true, "about": true, "keep": true, "kept": true,
}

// replyEvidenceTerms is what an observation may share with the reply: its
// words of three letters or more that are not [replyEvidenceStop], and its
// figures with the punctuation that spells them taken off ("$2,300" is 2300,
// "97.0%" is 97.0 and 97).
func replyEvidenceTerms(said string) []string {
	seen := map[string]bool{}
	var terms []string
	add := func(term string) {
		if term == "" || seen[term] {
			return
		}
		seen[term] = true
		terms = append(terms, term)
	}
	for _, field := range strings.FieldsFunc(asciiLower(said), func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == ',' || r == '\'')
	}) {
		field = strings.Trim(field, ".,'")
		if field == "" {
			continue
		}
		if unicode.IsDigit(rune(field[0])) {
			number := strings.ReplaceAll(field, ",", "")
			add(number)
			if whole, _, cut := strings.Cut(number, "."); cut {
				add(whole)
			}
			continue
		}
		if len([]rune(field)) < 3 || replyEvidenceStop[field] {
			continue
		}
		add(field)
	}
	return terms
}

// shared is the terms an output carries, read case-blind; a figure is matched
// only where it is not part of a longer one, so 97 is not found in 1997.
func shared(output string, terms []string) []string {
	lower := asciiLower(output)
	var found []string
	for _, term := range terms {
		if termAt(lower, term) >= 0 {
			found = append(found, term)
		}
	}
	return found
}

// termAt is where term first stands in lower, or -1. A word may stand inside a
// longer word ("page" in "pages"); a figure may not stand inside a longer
// figure.
func termAt(lower, term string) int {
	if !unicode.IsDigit(rune(term[0])) {
		return strings.Index(lower, term)
	}
	for from := 0; from < len(lower); {
		at := strings.Index(lower[from:], term)
		if at < 0 {
			return -1
		}
		at += from
		end := at + len(term)
		left := at == 0 || !isFigureByte(lower[at-1])
		right := end >= len(lower) || !isFigureByte(lower[end]) || (lower[end] == '.' && (end+1 >= len(lower) || !unicode.IsDigit(rune(lower[end+1]))))
		if left && right {
			return at
		}
		from = at + 1
	}
	return -1
}

// asciiLower folds only A to Z, so every byte of the folded text stands where
// it stood in the original and a position found in one cuts the other.
func asciiLower(text string) string {
	folded := []byte(text)
	for index, b := range folded {
		if b >= 'A' && b <= 'Z' {
			folded[index] = b + ('a' - 'A')
		}
	}
	return string(folded)
}

func isFigureByte(b byte) bool { return b >= '0' && b <= '9' || b == '.' }

// relevantObservations is the reader's share of what the turn observed: the
// observations that carry any of the reply's terms once the terms nearly every
// observation carries are set aside, newest kept first under
// [replyEvidenceBytes], then put back in order.
func relevantObservations(said string, observed []turnObservation) []turnObservation {
	terms := replyEvidenceTerms(said)
	if len(terms) == 0 || len(observed) == 0 {
		return nil
	}
	// A TERM EVERY OUTPUT CARRIES PICKS NONE OF THEM OUT. With four or more
	// outputs to choose from, a term found in more than half of them is not
	// counted; with fewer there is no telling the common from the apt.
	if len(observed) >= 4 {
		counts := map[string]int{}
		for _, observation := range observed {
			for _, term := range shared(observation.result, terms) {
				counts[term]++
			}
		}
		var kept []string
		for _, term := range terms {
			if counts[term]*2 <= len(observed) {
				kept = append(kept, term)
			}
		}
		terms = kept
	}
	var picked []turnObservation
	room := replyEvidenceBytes
	for index := len(observed) - 1; index >= 0 && room > 0; index-- {
		observation := observed[index]
		found := shared(observation.result, terms)
		if len(found) == 0 {
			continue
		}
		observation.result = evidenceStretches(observation.result, found, min(replyEvidenceOneBytes, room))
		room -= len(observation.step) + len(observation.result)
		picked = append(picked, observation)
	}
	sort.SliceStable(picked, func(left, right int) bool { return picked[left].order < picked[right].order })
	return picked
}

// evidenceStretches is an output cut to at most limit bytes. A short one is
// kept whole; a long one keeps the stretches around the terms it shares with
// the reply, in the order they stand, joined by " … ", because the figure the
// reply is wrong about is rarely at the head of a long JSON line.
func evidenceStretches(output string, found []string, limit int) string {
	if len(output) <= limit {
		return output
	}
	lower := asciiLower(output)
	type span struct{ from, to int }
	var spans []span
	for _, term := range found {
		at := termAt(lower, term)
		if at < 0 {
			continue
		}
		spans = append(spans, span{max(0, at-replyEvidenceSpan), min(len(output), at+len(term)+replyEvidenceSpan)})
	}
	sort.Slice(spans, func(left, right int) bool { return spans[left].from < spans[right].from })
	var merged []span
	for _, next := range spans {
		if last := len(merged) - 1; last >= 0 && next.from <= merged[last].to {
			merged[last].to = max(merged[last].to, next.to)
			continue
		}
		merged = append(merged, next)
	}
	var out strings.Builder
	for _, piece := range merged {
		text := strings.ToValidUTF8(output[piece.from:piece.to], "")
		if out.Len() > 0 {
			text = " … " + text
		}
		if out.Len()+len(text) > limit {
			out.WriteString(clip(text, limit-out.Len()))
			break
		}
		out.WriteString(text)
	}
	return out.String()
}
