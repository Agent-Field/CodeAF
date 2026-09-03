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
const captionSystem = "You write short status lines a person can glance at while work runs."

// captionPrompt asks for a checklist step: what is happening and where, short
// enough to read mid-turn without studying the tool rows.
const captionPrompt = "Write one short status line for a person watching this work. " +
	"5 to 10 words. Present tense, lowercase, no first person. " +
	"Say what is happening and where (path, repo, host, or topic) when you know it. " +
	"This is a checklist step, not reasoning and not tool names. " +
	"Examples: listing open github issues · reading the caption renderer · ranking bugs by quality. " +
	"Answer with the line only."

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
	// captionWordMax is the person-facing length. Longer lines stop being a
	// glance and start being a paragraph; the surface wraps instead of cutting
	// with an ellipsis, so the budget is words, not characters with a mark.
	captionWordMax = 10
	// captionCharMax is only a safety bound for a single runaway token. It never
	// appends an ellipsis — the drawing path wraps.
	captionCharMax = 72
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

// cleanCaption takes exactly one plain status line and refuses an instruction
// echo. It keeps at most captionWordMax words and never appends an ellipsis —
// a long word is hard-cut; the surface wraps the rest.
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

// shortCaption is the person-facing length of a step title: enough to say what
// and where, short enough to glance. Words beyond the budget drop; there is no
// ellipsis mark, because the drawing path wraps instead of truncating.
func shortCaption(line string) string {
	fields := strings.Fields(strings.TrimSpace(line))
	if len(fields) == 0 {
		return ""
	}
	if len(fields) > captionWordMax {
		fields = fields[:captionWordMax]
	}
	out := strings.Join(fields, " ")
	runes := []rune(out)
	if len(runes) > captionCharMax {
		out = string(runes[:captionCharMax])
	}
	return strings.TrimSpace(out)
}
