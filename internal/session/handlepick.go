package session

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/roles"
	"github.com/Agent-Field/codeaf/internal/teams"
)

// A member's handle, chosen by the conversation's own title model.
//
// A handle is ONE lowercase word naming what the conversation is about:
// @security for "santosh dev2 branch code complexity & security review",
// @milestones for "CodeAF repo issue tags & milestones", @gravity for "quantum
// gravity research updates / session monitor". The word list in internal/teams
// ([teams.DeriveHandle]) guesses one at once, so a member is addressable the
// moment it has a title, and the guesses it made of those three titles were
// @review, @reviewing and @session: a list cannot tell the subject of a title
// from the kind of work being done to it. A model can, in one word.
//
// IT IS THE CONVERSATION NAMER'S ERRAND WITH A ONE-WORD QUESTION, asked where
// the name is made. It goes through the same door ([Agent.callRoleChecked]) on
// the same role ([roles.RoleTitle]), so it lands on the cheap tier and is billed
// the same way, and it asks for a handful of tokens. It runs:
//
//   - right after the conversation's title is published ([Agent.publishTitle]),
//     which is the moment the title the word is read off exists; and
//   - once per process at a step boundary, for a conversation that already had
//     its title and is in a team whose handle for it is still the word list's
//     guess. That is the ONE-TIME pass over handles made before this: each is
//     chosen again, the same way, on its conversation's next turn. A timeout or
//     a dropped connection is asked once more; a refusal is not.
//
// IT RUNS ON THE MACHINE THAT OWNS THE MODEL AND THE STORE. Over --host that is
// the engine, where this session is, and the teams file is that machine's.
//
// ONLY A GUESS IS REPLACED ([teams.Member.HandleDerived]). A handle a person or
// the manager gave is never touched, and one the model chose is not chosen
// again ([teams.File.ChooseHandle]). A clash inside a team takes the model's
// second choice, then a word of the title in front of the first; never a
// number unless nothing else fits.
//
// EVERY RENAME IS SAID IN THE TEAM'S TRAFFIC, `@review is now @security`, from
// codeaf to everyone, so the manager and the members read it at their next
// step ([teamLine]) and the person sees it on the rail. The manager's role note
// names the members by handle and is read again when the teams file moves, so
// it refreshes on its own.

// handleAsk is the instruction, last in the user message for the reason
// titleSystem's comment gives.
const handleAsk = "Give this conversation a handle: ONE lowercase English word that names what it is about, its subject, not the kind of work (not review, fix, update, session, research). Then two other such words, in case the first is taken. Answer with the three words on one line, best first."

// handleTokens is the whole budget of one ask: three short words.
const handleTokens = 24

// handleRetryWait is the one pause before a handle ask is tried again. The
// guess already stands, so the wait is short: long enough that a timeout or a
// dropped connection has a chance to clear, and one try only.
const handleRetryWait = 250 * time.Millisecond

// handleBackoff is that wait's seam, so a test states it without spending it.
var handleBackoff = backoffWait

// handleChoices is how many words an answer is read for.
const handleChoices = 3

// chooseTeamHandlesLater starts the handle errand beside the turn for title,
// once per process, when this conversation is in a team whose handle for it is
// still a guess. roles is what the caller already read, nil to read them here.
// Nothing waits on it.
//
// A conversation in no team yet is not marked as asked: it may join one later
// in this process, and its first boundary in the team asks then.
func (a *Agent) chooseTeamHandlesLater(title string, roles []teamRole) {
	title = strings.TrimSpace(title)
	if title == "" || a.config.teamProfile() == "" {
		return
	}
	if roles == nil {
		roles = a.teamRoles()
	}
	if !rolesWantHandle(roles) {
		return
	}
	a.team.mu.Lock()
	if a.team.handleTried {
		a.team.mu.Unlock()
		return
	}
	a.team.handleTried = true
	a.team.mu.Unlock()
	a.mu.Lock()
	ctx, model, closed := a.titleCtx, a.model, a.closed
	if ctx == nil || closed {
		a.mu.Unlock()
		return
	}
	a.titleJobs.Add(1)
	a.mu.Unlock()
	go func() {
		defer a.titleJobs.Done()
		a.chooseTeamHandles(ctx, title, model)
	}()
}

// teamHandlePass is the boundary's half: a conversation with a title, in a team
// whose handle for it is a guess, has it chosen once. roles is what the
// boundary just read.
func (a *Agent) teamHandlePass(roles []teamRole) {
	if !rolesWantHandle(roles) {
		return
	}
	a.mu.Lock()
	title := a.title
	a.mu.Unlock()
	a.chooseTeamHandlesLater(title, roles)
}

// rolesWantHandle reports whether any of roles holds a guessed handle.
func rolesWantHandle(roles []teamRole) bool {
	for _, role := range roles {
		if role.derived {
			return true
		}
	}
	return false
}

// chooseTeamHandles is the errand: one ask, then the word written to every team
// whose handle for this conversation is a guess, and each rename said in that
// team's Traffic.
func (a *Agent) chooseTeamHandles(ctx context.Context, title, model string) {
	profile := a.config.teamProfile()
	if profile == "" {
		return
	}
	if !rolesWantHandle(a.teamRoles()) {
		return
	}
	choices := a.askForHandle(ctx, title, model)
	if len(choices) == 0 || ctx.Err() != nil {
		return
	}
	a.team.mu.Lock()
	keys := a.teamKeysLocked()
	a.team.mu.Unlock()
	file, err := teams.Load(profile)
	if err != nil {
		return
	}
	for _, role := range rolesFor(file, keys, a.teamDefaults(profile)) {
		if !role.derived {
			continue
		}
		var old, now string
		err := teams.Update(profile, func(f *teams.File) error {
			// Made again under the store's lock, against the file as it is now:
			// a person may have typed a handle since the roles were read.
			var err error
			old, now, err = f.ChooseHandle(role.id, role.key, choices, title)
			return err
		})
		if err != nil || old == "" || now == old {
			continue
		}
		_ = teams.AppendTraffic(profile, role.id, handleRenameEntry(old, now))
	}
}

// handleRenameEntry is the Traffic line that says a member's handle changed.
// It names no member key, so it is never read as the member's own state
// ([askingFromEvents] reads the newest event about a member).
func handleRenameEntry(old, now string) teams.Entry {
	return teams.Entry{
		Kind: teams.KindEvent,
		From: teams.FromSystem,
		To:   teams.ToEveryone,
		Text: "@" + old + " is now @" + now,
	}
}

// askForHandle is one question for the words. A transient failure, a timeout or
// the network, is asked once more after [handleRetryWait]. A refusal is not,
// and neither is an answer that names nothing: the guess already stands, and a
// model that will not answer leaves it. A later turn does not ask again
// ([Agent.chooseTeamHandlesLater] asks once per process).
func (a *Agent) askForHandle(ctx context.Context, title, model string) []string {
	choices, err := a.askForHandleOnce(ctx, title, model)
	if len(choices) > 0 || !handleMayRetry(ctx, err) {
		return choices
	}
	if handleBackoff(ctx, handleRetryWait) != nil {
		return nil
	}
	choices, _ = a.askForHandleOnce(ctx, title, model)
	return choices
}

// askForHandleOnce is one call for the words.
func (a *Agent) askForHandleOnce(ctx context.Context, title, model string) ([]string, error) {
	callCtx, cancel := context.WithTimeout(ctx, titleAskWindow)
	defer cancel()
	ask := "Conversation title:\n" + clip(title, titleClip) + "\n\n" + handleAsk
	response, named, err := a.callRoleChecked(callCtx, roles.RoleTitle, model,
		[]ai.Message{textMessage("system", titleSystem), textMessage("user", ask)},
		func(response *ai.Response, named string) bool {
			if len(cleanHandleChoices(response.Text())) > 0 {
				return true
			}
			a.addDetachedUsageAs(response, named, 1, auxRoleTitle)
			return false
		}, ai.WithMaxTokens(handleTokens))
	if err != nil || response == nil {
		return nil, err
	}
	a.addDetachedUsageAs(response, named, 1, auxRoleTitle)
	return cleanHandleChoices(response.Text()), nil
}

// handleMayRetry reports whether one failed handle ask may be tried again.
// A cancelled errand is not a failure, and a refusal is the provider saying no
// to the same question.
func handleMayRetry(ctx context.Context, err error) bool {
	if err == nil || ctx.Err() != nil {
		return false
	}
	if _, refused := provider.RefusalFrom(err); refused {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	return isRetryable(err.Error())
}

// cleanHandleChoices is a model's answer as handle words: the first line's
// words, lowercased, with anything but letters and digits taken off, each a
// valid handle ([teams.ValidHandle]) and not one of the words the instruction
// names as work rather than a subject. It is nil when nothing usable is left.
func cleanHandleChoices(raw string) []string {
	line := strings.TrimSpace(raw)
	if at := strings.IndexByte(line, '\n'); at >= 0 {
		line = line[:at]
	}
	var out []string
	for _, w := range strings.FieldsFunc(strings.ToLower(line), func(r rune) bool {
		return unicode.IsSpace(r) || r == ',' || r == '/' || r == '|' || r == ';'
	}) {
		w = strings.Map(func(r rune) rune {
			if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
				return r
			}
			return -1
		}, w)
		if teams.ValidHandle(w) != nil || handleWorkWords[w] || strings.Trim(w, "0123456789") == "" {
			continue
		}
		if !handleSeen(out, w) {
			out = append(out, w)
		}
		if len(out) == handleChoices {
			break
		}
	}
	return out
}

// handleWorkWords are words a handle may not be, because they name the kind of
// work or the conversation itself rather than what it is about; a small model
// that echoes the instruction answers with them.
var handleWorkWords = map[string]bool{
	"review": true, "reviewing": true, "fix": true, "update": true, "updates": true,
	"session": true, "research": true, "conversation": true, "handle": true,
	"subject": true, "word": true, "words": true, "the": true, "one": true, "not": true,
}

func handleSeen(list []string, w string) bool {
	for _, have := range list {
		if have == w {
			return true
		}
	}
	return false
}
