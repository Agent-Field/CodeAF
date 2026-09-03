package session

// The line over a batch of work.
//
// Most captions cost no second call: the surface lifts the short narrating line
// the working model already wrote before its tools, or composes an honest floor
// from the batch's targets. Thinking is never a caption — it is machinery under
// the step. This file owns the last rung: a cheap narrator that names the
// discrete step while the batch runs, cancelled when the batch ends so a late
// answer cannot rewrite a settled title.

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

// captionPrompt asks for a single short checklist sentence: what and where.
const captionPrompt = "Write one short status sentence for a person watching this work. " +
	"A single sentence, 5 to 10 words, present tense, lowercase, no first person. " +
	"Say what is happening and where (path, repo, host, or topic) when you know it. " +
	"Not reasoning, not tool names, not two sentences. " +
	"Examples: listing open github issues · reading the caption renderer · ranking bugs by quality. " +
	"Answer with the sentence only."

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
		},
		ai.WithMaxTokens(80))
	if err != nil || response == nil {
		return
	}
	a.addAuxiliaryUsageAs(response, named, 1, auxRoleCaption)
	if line := cleanCaption(response.Text()); line != "" && ctx.Err() == nil {
		hub.send(Event{Kind: EventCaption, Text: line})
	}
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
