package session

// The guardian: a small model asked whether one call is plainly safe, before
// the person is asked at all.
//
// It sits inside the consent path (consent.go) at exactly one point — after the
// policy has said PROMPT and after the session memo has had its say, before the
// question reaches a person — and it can only ever do one thing: turn a prompt
// into an allow. It cannot deny, it cannot widen an allow, and it never sees a
// call the policy already refused. That asymmetry is the whole safety argument:
// the worst a broken, confused or adversarially-prompted guardian can do is fail
// to save somebody a keystroke, and every path that does not end in the single
// word ALLOW ends exactly where it ended before this file existed.
//
// ── WHY IT IS OFF BY DEFAULT ──
//
// A gate that answers for the person must be opted into. The setting
// (approval.guardian) defaults to off, and with it off nothing here runs: no
// call, no cost, no branch. "The model asked, so a model decided" is a sentence
// somebody has to have agreed to in advance, in writing, in a file they can read
// back — which is what a settings row is.
//
// ── WHY THE PROMPT IS THE SHAPE IT IS ──
//
// One tool, its arguments, the rule that matched, and a binary contract. No
// conversation, no transcript, no memory: the guardian must judge THE CALL, and
// a guardian that could read the session would be a guardian the session can
// talk into things. The arguments are clipped because a judgement about a
// 300KB write is made in its first lines, and because a gate is not a place to
// spend a context window.
//
// ── WHERE IT DOES NOT RUN ──
//
// The early-start law (loop.go) consults the POLICY only, so a call the guardian
// would vouch for still waits for the batch rather than starting while the
// response streams. That is deliberate: an early start is thrown away on a
// retry, and paying for a guardian call per discarded attempt would be spending
// money to save a keystroke that was never going to be asked for.

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/approval"
	"github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// The guardian is a ROLE, registered from here rather than declared in
// internal/roles' default table, exactly as that package's own doc says an
// auxiliary call should: the package that makes the call owns the registration.
// The tier is low because this is a one-word classification of one short input —
// the archetypal cheap call — and a person who disagrees pins it
// (`roles.guardian: <model>`) without editing either package.
func init() { roles.Register(roles.RoleGuardian, roles.TierLow) }

// guardianArgsLimit bounds the arguments handed to the guardian. 2000 bytes is
// a command line, a path with a long glob, or the head of a patch — enough to
// see what a call DOES, which is the only question being asked.
const guardianArgsLimit = 2000

// guardianPrompt is the whole instruction, and the contract is binary on
// purpose: a gate with three answers has a middle one nobody has defined, and
// the first thing a model does with an undefined answer is use it.
//
// The word ALLOW is the only thing that opens the gate, so every failure mode —
// a refusal to answer, an explanation, an empty reply, a model that has decided
// to be helpful in prose — lands on ASK.
const guardianPrompt = `You are a safety gate. You judge ONE tool call a coding assistant wants to run, and you answer with exactly one word: ALLOW or ASK.

Answer ALLOW only if this specific call is plainly safe: read-only, inside the working directory, and reversible.

Answer ASK for anything that deletes, overwrites, sends, publishes, installs, or reaches outside the working directory — and for anything you are unsure about, cannot read, or would need to guess about.

When in doubt, ASK. Answer with the single word and nothing else.`

// guardianAllows reports whether the guardian vouched for this call.
//
// FALSE IS THE ANSWER TO EVERYTHING THAT IS NOT A CLEAR YES: the guardian is
// off, no model can be resolved, the provider failed, the turn was interrupted,
// the reply was anything other than the word. Each of those is a fall-through to
// the person on the caller's side, which is the behavior with no guardian at
// all.
//
// It rides the TURN's context, so Interrupt ends a pending guardian call on the
// same beat it ends everything else, and the blocked tool refuses the way an
// interrupted consent question already refuses.
// guardianAnswerWindow is how long the stand-in gets to say one word. It is
// generous for the answer and short for the wait: the call it is holding up has
// no question on screen behind it, so every second past this one is a turn that
// looks hung to the person watching it.
const guardianAnswerWindow = 10 * time.Second

func (a *Agent) guardianAllows(ctx context.Context, hub *eventHub, call ai.ToolCall, decision approval.Decision) bool {
	if !a.config.Guardian {
		return false
	}
	// NOBODY STANDS IN FOR THE PERSON ON WHAT LEAVES IN THEIR NAME. A guardian
	// saves a keystroke on a read; a message that has gone out over somebody's
	// own address cannot be called back, and the point of asking about it is that
	// THEY saw it. The list is internal/approval's, so the gate and the stand-in
	// cannot drift on which calls it means — plus the tools an account named
	// itself, which no list could have held (connectcaps.go).
	if a.actsInThePersonsName(call.Function.Name, json.RawMessage(call.Function.Arguments)) {
		return false
	}
	a.mu.Lock()
	model := a.model
	source := a.config.RolesSource
	closed := a.closed
	a.mu.Unlock()
	if closed {
		return false
	}
	judge, err := roles.Resolve(roles.Source(source), roles.RoleGuardian, model)
	if err != nil {
		return false
	}

	// AND IT CARRIES ITS OWN DEADLINE. This call is made INSIDE the tool batch,
	// with no card on screen and nothing for the person to answer: a guardian
	// model that stalls is a bash call blocked behind a question nobody was
	// asked, under a status line that says "working". The turn's context alone
	// bounds nothing — the provider's HTTP client is built with no timeout — so
	// a stall would last until somebody interrupted the turn. A one-word answer
	// does not need ten seconds; a guardian that cannot manage one in that time
	// falls through below exactly as a refusal does, and the person is asked.
	judgeCtx, done := context.WithTimeout(ctx, guardianAnswerWindow)
	defer done()

	// WithoutStream for the reason the title and the compaction summary use it:
	// this is bookkeeping about the conversation, not something anybody said, and
	// left on the turn's stream it would type a word into the room.
	//
	// AND IT NAMES ITSELF A GATE. The guardian reads finished work and answers
	// one word about whether it is safe, so its answer has to be right where its
	// speed is worth little — and nobody is reading its stream, which is what
	// keeps it off the status line of the answer it is standing in front of
	// (internal/lane's roles.go).
	response, err := a.client.CompleteWithMessages(
		provider.WithRole(provider.WithoutStream(judgeCtx), lane.RoleJudge),
		[]ai.Message{
			textMessage("system", guardianPrompt),
			textMessage("user", guardianQuestion(call, decision)),
		},
		ai.WithModel(judge))
	if err != nil || response == nil {
		return false
	}
	// The person pays for it, so it is folded into the session's auxiliary usage
	// — the same pocket the title and the summary come out of, and for the same
	// reason it is not charged to the turn: no turn asked for it.
	a.addAuxiliaryUsage(response, judge, 1)

	if !guardianSaysAllow(response.Text()) {
		return false
	}
	if hub != nil {
		hub.send(Event{
			Kind: EventGuardianAllowed,
			Tool: call.Function.Name,
			Hint: a.gloss(call),
			Args: argsText(call),
			Rule: decision.Rule,
			Text: "allowed by the guardian",
		})
	}
	return true
}

// guardianQuestion is the one call, as the guardian reads it.
func guardianQuestion(call ai.ToolCall, decision approval.Decision) string {
	var out strings.Builder
	out.WriteString("tool: " + call.Function.Name + "\n")
	if decision.Rule != "" {
		out.WriteString("matched rule: " + decision.Rule + "\n")
	}
	arguments := strings.TrimSpace(call.Function.Arguments)
	if arguments == "" {
		arguments = "(none)"
	}
	out.WriteString("arguments:\n" + clip(arguments, guardianArgsLimit))
	return out.String()
}

// guardianSaysAllow reads the verdict. It accepts the word and the decorations a
// model puts around one word — quotes, backticks, bold, a full stop — and
// nothing else: a reply that says ALLOW inside a sentence is a model explaining
// itself, and an explanation is not an answer to a binary contract.
func guardianSaysAllow(reply string) bool {
	verdict := strings.TrimSpace(firstLine(reply))
	verdict = strings.Trim(verdict, "`*\"'“”.: ")
	return strings.EqualFold(verdict, "ALLOW")
}
