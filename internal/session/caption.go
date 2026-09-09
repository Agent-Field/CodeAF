package session

// The line over a batch of work.
//
// Most captions cost no second call: the surface lifts the short narrating line
// the working model already wrote before its tools, or composes an honest floor
// from the batch's targets. Thinking is never a caption — it is machinery under
// the step. This file owns the last rung: a cheap narrator that names the
// discrete step while the batch runs, cancelled when the batch ends so a late
// answer cannot rewrite a settled title.
//
// TWO THINGS TRAVEL WITH THE SENTENCE, and both are here because they are
// decided here. The FAMILY of work is a one-word prefix on the same answer
// (actioncategory.go), so the surface's one still mark costs no second call. The
// ANCHOR is the batch's first call id, and it is what makes the cancellation
// above a fence rather than a hope: the check is a race — this goroutine can
// pass it and then be descheduled past the end of its own batch — so the event
// names the step it is about and a surface holding a different step drops it.
//
// AND THE LINE IS JOURNALED. A caption is not a message; nothing was said to the
// model, so there is nothing in the transcript to recover it from, and a
// conversation reopened without a `caption` line falls back to recomposing a
// title out of tool names — which turns "starting the local server" into
// "running 1 command" and a step the narrator called a `test` into a `run`.
// [sessionFile.appendCaption] writes it against the same anchor the event
// carries, so the record and the stream name the step the same way.

import (
	"context"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// The narrator is a ROLE registered from the file that makes the call. LOW is
// deliberate: a wrong caption costs a glance, the rows underneath remain the
// truth, and no decision downstream is made from these words.
func init() { roles.Register(roles.RoleCaption, roles.TierLow) }

// captionSystem is character only. A cheap model reads the end of the user
// message as the thing to do, so [captionPrompt] goes there and goes last.
const captionSystem = "You write one short status sentence a person can glance at while work runs."

// captionPrompt asks for a single short checklist sentence: what and where —
// and, in front of it, ONE WORD saying which family of work that is
// (actioncategory.go), which is what the surface draws its one still mark from.
//
// THE WORD IS A PREFIX ON THE SAME ANSWER AND NOT A SECOND QUESTION. It costs no
// extra call, no extra round trip and no JSON: `run | starting the local server`
// is one line a cheap model writes as easily as the sentence alone, and a model
// that ignores the prefix entirely still produces a caption that is exactly as
// good as it was before — [SplitActionLine] hands the whole line back and the
// tools name the family.
var captionPrompt = "Write one short status sentence for a person watching this work. " +
	"Begin with one word from this list, then a space, a vertical bar, a space, then the sentence: " +
	ActionCategoryWords() + ". " +
	"The sentence is a single sentence, 5 to 10 words, present tense, lowercase, no first person. " +
	"Say what is happening and where (path, repo, host, or topic) when you know it. " +
	"Not reasoning, not tool names, not two sentences. " +
	"Examples: run | starting the local server · search | listing open github issues · " +
	"read | reading the caption renderer. " +
	"Answer with the word, the bar and the sentence only."

const (
	// captionDwell is short on purpose: long enough that an instant batch pays
	// for no narrator, short enough that a multi-second command still gets a
	// semantic line while it runs. The old four-second wait left every ordinary
	// tool round looking like "running N calls" for its whole life.
	captionDwell = 500 * time.Millisecond
	// captionCalls bounds the narrator per turn. A turn that goes quiet six
	// times does not make its sixth auxiliary sentence worth its bill.
	captionCalls = 3
	// captionClip keeps the narrator on the tail of the work. A caption is about
	// what is happening now, not a digest of the whole conversation.
	captionClip = 1500
	// captionWordMax is the soft ceiling for one short sentence. The surface
	// wraps; it does not ellipsis-cut. When a line must shrink, trailing
	// dangling words are dropped so it does not end on "which are".
	captionWordMax = 10
)

// maybeCaption asks for one line about the batch in flight. It is called only
// from the dwell goroutine, never on the tool batch's waiting path.
func (a *Agent) maybeCaption(ctx context.Context, hub *eventHub, calls []ai.ToolCall, rendered []string) {
	ep := episodeFrom(ctx)
	if hub == nil || ep == nil || !ep.reserveCaption() {
		return
	}
	// The caller carries the rendered arguments because it already paid to make
	// them, but the narrator reads GLOSSES ONLY. Full arguments are machinery,
	// can be enormous, and are not the question this one-line digest answers.
	_ = rendered

	a.mu.Lock()
	var tail strings.Builder
	for _, message := range a.messages {
		text := strings.TrimSpace(messageContentText(message))
		if text == "" {
			continue
		}
		if tail.Len() > 0 {
			tail.WriteString("\n")
		}
		tail.WriteString(message.Role)
		tail.WriteString(": ")
		tail.WriteString(text)
	}
	model := a.model
	a.mu.Unlock()

	var batch strings.Builder
	for _, call := range calls {
		gloss := strings.TrimSpace(a.gloss(call))
		if gloss == "" {
			gloss = call.Function.Name
		}
		batch.WriteString("- ")
		batch.WriteString(gloss)
		batch.WriteByte('\n')
	}
	user := "Recent transcript:\n" + captionTail(tail.String()) +
		"\n\nCurrent batch:\n" + strings.TrimSpace(batch.String()) +
		"\n\n" + captionPrompt

	response, named, err := a.callRole(ctx, roles.RoleCaption, model,
		[]ai.Message{
			textMessage("system", captionSystem),
			// THE INSTRUCTION IS LAST, after the evidence it is about.
			textMessage("user", user),
		})
	if err != nil || response == nil {
		return
	}
	a.addAuxiliaryUsageAs(response, named, 1, auxRoleCaption)
	// THE FAMILY COMES OFF FIRST AND THE SENTENCE IS CLEANED AFTER, because
	// [cleanCaption]'s own budget is about the SENTENCE — ten words, one clause,
	// no dangling glue — and a label counted against it would cost the caption a
	// word. A line the parser could not read comes back whole and is cleaned
	// exactly as it always was.
	category, sentence := SplitActionLine(response.Text())
	line := cleanCaption(sentence)
	// AND A CAPTION THAT IS ONLY THE LABEL IS NO CAPTION. A cheap model asked for
	// a vocabulary sometimes answers with the vocabulary; "run" alone is an echo
	// of the instruction, not a sentence about the work, and the deterministic
	// composite standing in the slot is better than it.
	if _, echo := ParseActionCategory(line); echo {
		return
	}
	if line == "" {
		return
	}
	// THE ANCHOR IS THE BATCH'S FIRST CALL, and it is what makes this event
	// unambiguous rather than merely timely.
	//
	// The cancellation above is a RACE and not a fence: this goroutine can pass
	// `ctx.Err() == nil`, be descheduled, and reach [eventHub.send] after the
	// batch ended, another began, and its rows are already on screen. A surface
	// keying the event onto "the newest tool row of this turn" would then retitle
	// a step this sentence was never about — the defect existed for the text
	// alone and would have been inherited whole by the mark beside it. The id
	// travels so the surface can key on the step ITSELF; an event whose anchor
	// names no row it is holding is one the surface drops.
	anchor := ""
	if len(calls) > 0 {
		anchor = calls[0].ID
	}
	// THE ANSWER IS ACCEPTED ONCE FOR BOTH RECORD AND FRAME. A response that came
	// back after this batch's context was cancelled was never shown live, so
	// writing it would make the same conversation acquire a new title when it was
	// reopened. Once accepted, both writes proceed even if cancellation races in
	// immediately afterward; the anchor above keeps a delayed event on its own
	// step, and one later cancellation check would split the durable and live
	// accounts of what the narrator said.
	if ctx.Err() != nil {
		return
	}
	// AND THE RECORD IS WRITTEN WHERE THE EVENT IS SENT, with the same anchor and
	// the same words. A caption is not a message — nothing was said to the model
	// here — so it is journaled as a line of its own or it is lost the moment the
	// window closes, and a reopened conversation falls back to recomposing a
	// title out of tool names.
	a.file.appendCaption(anchor, line, category)
	hub.send(Event{Kind: EventCaption, Text: line, Category: category, CallID: anchor})
}

// reserveCaption spends one of this turn's narrator calls before the provider
// call begins. Failed and refused answers still cost a call and therefore count
// toward the bound.
func (ep *episode) reserveCaption() bool {
	if ep == nil {
		return false
	}
	ep.captionMu.Lock()
	defer ep.captionMu.Unlock()
	if ep.captionN >= captionCalls {
		return false
	}
	ep.captionN++
	return true
}

func captionTail(transcript string) string {
	transcript = strings.TrimSpace(transcript)
	if len(transcript) <= captionClip {
		return transcript
	}
	start := len(transcript) - captionClip
	for start < len(transcript) && !utf8RuneStart(transcript[start]) {
		start++
	}
	return strings.TrimSpace(transcript[start:])
}

// cleanCaption takes one plain short sentence and refuses an instruction echo.
// It never appends an ellipsis — the surface wraps what remains.
func cleanCaption(raw string) string {
	line := stripMarkup(strings.TrimSpace(firstLine(raw)))
	line = stripOpener(line)
	line = strings.Trim(line, `"'“”`)
	line = strings.TrimSpace(line)
	if line == "" || namesTheInstruction(line) {
		return ""
	}
	return shortCaption(line)
}

// shortCaption keeps ONE short sentence. Prefer a complete sentence under the
// word budget; if a longer sentence must shrink, drop trailing dangling words
// so the title does not end mid-clause ("… which are").
func shortCaption(line string) string {
	line = strings.TrimSpace(line)
	if line == "" {
		return ""
	}
	var pick string
	for _, sentence := range captionSentences(line) {
		words := strings.Fields(strings.TrimSpace(strings.TrimRight(sentence, ".!?;:")))
		if len(words) == 0 {
			continue
		}
		if len(words) < 3 {
			if pick == "" {
				pick = strings.Join(words, " ")
			}
			continue
		}
		if len(words) > captionWordMax {
			words = captionTrimDangling(words[:captionWordMax])
		}
		return strings.Join(words, " ")
	}
	return pick
}

func captionSentences(line string) []string {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}
	var out []string
	start := 0
	for i, r := range line {
		switch r {
		case '.', '!', '?':
			piece := strings.TrimSpace(line[start : i+1])
			if piece != "" {
				out = append(out, piece)
			}
			start = i + 1
		}
	}
	if rest := strings.TrimSpace(line[start:]); rest != "" {
		out = append(out, rest)
	}
	if len(out) == 0 {
		return []string{line}
	}
	return out
}

// captionTrimDangling drops trailing glue words left by a hard word budget so
// a caption reads as a finished short sentence, not a cut clause.
func captionTrimDangling(words []string) []string {
	dangling := map[string]bool{
		"a": true, "an": true, "the": true, "and": true, "or": true, "but": true,
		"to": true, "of": true, "in": true, "on": true, "at": true, "for": true,
		"from": true, "by": true, "with": true, "as": true, "into": true,
		"which": true, "that": true, "this": true, "these": true, "those": true,
		"who": true, "whom": true, "whose": true, "where": true, "when": true,
		"is": true, "are": true, "was": true, "were": true, "be": true, "been": true,
		"being": true, "have": true, "has": true, "had": true, "do": true, "does": true,
		"did": true, "will": true, "would": true, "can": true, "could": true,
		"should": true, "may": true, "might": true, "must": true,
		"actually": true, "still": true, "also": true, "just": true, "very": true,
	}
	for len(words) > 2 {
		last := strings.ToLower(strings.Trim(words[len(words)-1], ",;:"))
		if !dangling[last] {
			break
		}
		words = words[:len(words)-1]
	}
	return words
}
