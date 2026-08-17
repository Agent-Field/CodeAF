package session

// The session's name.
//
// A conversation needs one word-sized handle — for the picker that lists
// sessions, for a window title, for a person deciding which of yesterday's
// three sessions to resume. Nobody wants to type it, and the first exchange
// already says what the session is about, so the session names itself once and
// then never again.
//
// Three properties are the whole design:
//
//   - ONE CALL, ONCE. It fires after the FIRST completed turn of a session that
//     has no name yet, and a session resumed from a journal already has its name
//     — the title line is read back at open. A namer that ran every turn would
//     be a tax on every turn, and a name that changed under the person would
//     make the picker unreadable.
//
//   - THE CHEAP MODEL. The model is resolved through internal/roles as
//     RoleTitle, which puts it on the low tier by default: a wrong title costs a
//     glance and is rewritten by the next session, so it is the archetypal cheap
//     call. A person who has configured nothing gets the session's own model,
//     which is roles.Resolve's floor and not a failure.
//
//   - ONLY A JOURNALED SESSION NAMES ITSELF. A session with no file has
//     nowhere to keep a name and nothing to be listed in — the name exists for
//     the picker, which lists files — so an in-memory conversation (a headless
//     one-shot, a test) pays for nothing. That is also why the gate reads "the
//     session file has no title yet": a resume already has its name.
//
//   - IT NEVER BREAKS THE TURN. The turn is already done and its usage already
//     sealed when this runs; a failed, empty or cancelled title leaves the
//     session unnamed and says nothing. There is no event kind for "a small
//     thing did not work", and inventing one — or borrowing EventError, which
//     means the turn ended badly — would report a fault about work the person
//     never asked for.

import (
	"context"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// titlePrompt is the whole instruction. Short, because the shape of the answer
// IS the requirement: eight words is a picker column, lowercase and unquoted is
// what every other label in this surface looks like.
const titlePrompt = "name this session in ≤8 words, lowercase, no quotes"

// titleClip bounds each half of the opening exchange handed to the namer. A
// title is derived from what the session is ABOUT, and the first paragraph of
// the question and of the answer says that; sending a 300KB tool-assisted reply
// would pay for a whole context to produce eight words.
const titleClip = 2000

// titleLimit bounds the name itself. Eight words asked for, 80 bytes accepted:
// the cap is a guard against a model that answers with a paragraph, not a
// second attempt at the instruction.
const titleLimit = 80

// maybeTitle names the session if it has no name yet. It runs at the end of a
// completed turn, on that turn's context and hub.
func (a *Agent) maybeTitle(ctx context.Context, hub *eventHub) {
	a.mu.Lock()
	if a.file == nil || a.titleTried || strings.TrimSpace(a.title) != "" || a.closed {
		a.mu.Unlock()
		return
	}
	// The attempt is marked before the call, not after it: this is ONE call per
	// session, and a namer that retried on every later turn would turn a
	// provider having a bad minute into a charge on every turn after it.
	a.titleTried = true
	question, answer := a.firstExchangeLocked()
	model := a.model
	source := a.config.RolesSource
	a.mu.Unlock()

	if question == "" {
		return
	}
	named, err := roles.Resolve(roles.Source(source), roles.RoleTitle, model)
	if err != nil {
		return
	}

	// WithoutStream for the reason the compaction summary uses it: this is
	// bookkeeping, and left on the turn's stream it would type itself into the
	// room. No tools either — the namer's only job is to produce eight words.
	response, err := a.client.CompleteWithMessages(
		provider.WithoutStream(ctx),
		[]ai.Message{
			textMessage("system", titlePrompt),
			textMessage("user", "First message:\n"+clip(question, titleClip)+
				"\n\nFirst reply:\n"+clip(answer, titleClip)),
		},
		ai.WithModel(named))
	if err != nil || response == nil {
		return
	}
	a.addAuxiliaryUsage(response)

	title := cleanTitle(response.Text())
	if title == "" {
		return
	}
	a.setTitle(title)
	if hub != nil {
		hub.send(Event{Kind: EventTitleChanged, Text: title})
	}
}

// setTitle records the name in the agent and in the journal.
func (a *Agent) setTitle(title string) {
	a.mu.Lock()
	a.title = title
	file := a.file
	a.mu.Unlock()
	if file != nil {
		file.appendTitle(title)
	}
	// The folder's row says what the journal says. Until now it has carried the
	// person's opening words as a placeholder (placemeta.go); this is the name
	// the conversation actually earned.
	a.stampTitle(title)
}

// firstExchangeLocked returns the session's opening question and the first
// thing the assistant said back, both as plain text.
//
// It reads from the front of the transcript rather than from the turn that just
// ended, which matters for the session whose first turn is not its first
// message — a resume that was never titled, a session whose opening turn was
// interrupted. The name should describe what the conversation is about, and
// that is where it was stated.
func (a *Agent) firstExchangeLocked() (string, string) {
	question, answer := "", ""
	for _, message := range a.messages {
		switch message.Role {
		case "user":
			if question == "" {
				question = messageContentText(message)
			}
		case "assistant":
			if question != "" && answer == "" {
				answer = messageContentText(message)
			}
		}
		if question != "" && answer != "" {
			break
		}
	}
	return strings.TrimSpace(question), strings.TrimSpace(answer)
}

func messageContentText(message ai.Message) string {
	var text strings.Builder
	for _, part := range message.Content {
		if part.Type == "text" && part.Text != "" {
			if text.Len() > 0 {
				text.WriteString("\n")
			}
			text.WriteString(part.Text)
		}
	}
	return text.String()
}

// cleanTitle takes the first line and strips the four things a model adds
// against the instruction: surrounding quotes, a trailing full stop, a leading
// label like "Title:", and the separators of a name answered as a SLUG.
//
// The slug is the one worth explaining. The instruction asks for words, and a
// model that has spent its life reading identifiers sometimes answers
// "porting_the_parser" — which is the right eight words welded into a filename.
// A name is read by a person, in a status line and in a list of yesterday's
// sessions, so the welding is undone at the moment the name is minted rather
// than at each of the places it is drawn.
//
// Only a ONE-TOKEN answer is touched. A title that already has a space in it is
// words, and a hyphen inside words is a hyphen somebody meant ("port-b failures"
// keeps it).
func cleanTitle(raw string) string {
	title := strings.TrimSpace(firstLine(raw))
	if label := strings.SplitN(title, ":", 2); len(label) == 2 &&
		strings.EqualFold(strings.TrimSpace(label[0]), "title") {
		title = strings.TrimSpace(label[1])
	}
	title = strings.Trim(title, `"'“”`)
	title = strings.TrimRight(title, ".")
	title = strings.TrimSpace(title)
	if !strings.ContainsAny(title, " \t") {
		title = strings.NewReplacer("_", " ", "-", " ").Replace(title)
		title = strings.Join(strings.Fields(title), " ")
	}
	return clip(strings.TrimSpace(title), titleLimit)
}
