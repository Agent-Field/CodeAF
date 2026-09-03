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
const captionSystem = "You name the discrete step a working session is on right now."

const captionPrompt = "Name this step in one line under 60 characters, present tense, " +
	"lowercase, no first person. A step is a checklist item (like \"listing github " +
	"issues\"), not reasoning and not tool names. Answer with the line only."

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
	// captionLimit is both the prompt's promise and the surface's column guard.
	captionLimit = 60
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

// cleanCaption takes exactly one plain sentence and refuses an instruction
// echo. The shared opener and echo hands are the ones the cheap namers already
// use for the same failure.
func cleanCaption(raw string) string {
	line := stripMarkup(strings.TrimSpace(firstLine(raw)))
	line = stripOpener(line)
	line = strings.Trim(line, `"'“”`)
	line = strings.TrimSpace(line)
	if line == "" || namesTheInstruction(line) {
		return ""
	}
	return strings.TrimSpace(clip(line, captionLimit))
}
