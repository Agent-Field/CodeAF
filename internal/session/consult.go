package session

// `consult` — the model's one-shot question to the tier that thinks, made a
// verb instead of a wish.
//
// THE CASCADE'S DOOR FORM. The confirmations that work in this build are
// HARNESS-ARMED — the route judge fires on every words-only answer, the
// division review rides divide_work — because the mark reader's measurement
// says the model that most needs a second opinion asks for it least. What this
// door adds is the escape valve for uncertainty the model CAN name: the
// fan-out shape it is unsure of, a reading it cannot resolve, a choice between
// roads. The armed doors still carry the reliability; this one carries the
// cases a harness cannot see.
//
// IT IS BOUNDED PER TURN, because the question it answers is "has this turn
// already bought its second opinion", and a second opinion-by-default is what
// the mastermind tier's patience (roles.go) is meant to be spent against.
// [rotationalConsultCeiling] is the count, spelled once, so a model reasoning
// from the wire reasons from the enforcement.

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// RoleConsult is the second-opinion call. MASTERMIND, for RoleRouterConfirm's
// reason: a wrong answer here routes real spend, and the mistake a cheap model
// makes confidently is the one this verb exists to catch.
func init() { roles.Register(roles.RoleConsult, roles.TierMastermind) }

const (
	// rotationalConsultCeiling is how many questions one turn may buy through
	// `consult`. TWO: the first is the opinion, the second is the disagreement
	// with it, and a third is a conversation that should have been its own task.
	rotationalConsultCeiling = 2
	// consultQuestionLimit and consultContextLimit bound the wire form. The
	// question is one thought; the context is the facts the answer needs, cut
	// at a size a busy model still stops. Neither is a transcript — the
	// person's words already ride the caller's prompt.
	consultQuestionLimit = 2_000
	consultContextLimit  = 8_000
)

// consultDescription is what the model reads before it asks. THE BOUND IS
// INTERPOLATED for taskDescription's reason: a number a model reasons with
// must be the number the code enforces.
var consultDescription = "One question to the tier that thinks, and its answer, " +
	"for a decision that is bigger than your own confidence — a fan-out shape, " +
	"a reading you cannot resolve, a choice between roads. Words in, a sentence " +
	"out; it does no work. At most " + strconv.Itoa(rotationalConsultCeiling) + " a turn."

// consultSchemaJSON is the wire form: one question, and the facts the answer
// needs, because the answering model holds none of yours.
const consultSchemaJSON = `{"type":"object","properties":{` +
	`"question":{"type":"string","description":"The uncertainty you can name, one thought"},` +
	`"context":{"type":"string","description":"The facts the answer needs. It holds none of your conversation."}` +
	`},"required":["question"],"additionalProperties":false}`

// consultAnswer is the question parsed out of the wire form. The fields are
// exported for [encoding/json], tagged on the wire's own names, and trimmed at
// the door so an empty one is a call the model can make again.
type consultAnswer struct {
	Question string `json:"question"`
	Context  string `json:"context"`
}

// consultSpent is the receipt a turn that emptied its budget gets, with the
// count, so the answer is arithmetic and not a shrug.
func consultSpent() string {
	return "no: " + strconv.Itoa(rotationalConsultCeiling) + " consults are already spent this turn"
}

// consultTool is the door.
//
// IT IS A CONVERSATION'S VERB AND NO WORKER'S, absent the way every
// conditional door on this belt is absent: a task node's second opinion rides
// the division review instead (task_divide.go), which is the armed version of
// the same instinct, and handing the model a second door to the same tier
// would be two answers to one question.
func (a *Agent) consultTool() bare.Tool {
	return bare.Tool{
		Name:        "consult",
		Description: consultDescription,
		Schema:      json.RawMessage(consultSchemaJSON),
		Execute:     a.startConsult,
	}
}

// startConsult is the tool's whole life: parse, bound, ask, answer. Everything
// it can answer badly is an ordinary tool result, the way every other door on
// this belt answers: an empty question is a call the model can make again.
func (a *Agent) startConsult(ctx context.Context, args json.RawMessage) (string, bool, error) {
	var parsed consultAnswer
	if err := decodeToolArguments(args, &parsed); err != nil {
		return invalidArgumentsPrefix + err.Error(), true, nil
	}
	parsed.Question = strings.TrimSpace(parsed.Question)
	parsed.Context = strings.TrimSpace(parsed.Context)
	if parsed.Question == "" {
		return "Invalid arguments: question is empty", false, nil
	}
	a.mu.Lock()
	if a.consultCalls >= rotationalConsultCeiling {
		a.mu.Unlock()
		return consultSpent(), false, nil
	}
	a.consultCalls++
	a.mu.Unlock()

	question := clip(parsed.Question, consultQuestionLimit)
	if parsed.Context != "" {
		question += "\n\nWHAT THE QUESTION NEEDS TO KNOW:\n" + clip(parsed.Context, consultContextLimit)
	}
	resp, from, err := a.callRole(ctx, roles.RoleConsult, a.model,
		[]ai.Message{
			textMessage("system", "You answer one question for a model working one desk down. It cannot ask you again, so decide: say what it should do and the one reason that carries it, plainly, in words. No tools, no ceremony, no new work."),
			textMessage("user", question),
		})
	if err != nil || resp == nil {
		return "consult is unanswered: " + errString(err), false, nil
	}
	a.addAuxiliaryUsage(resp, from, 1)
	answer := strings.TrimSpace(resp.Text())
	if answer == "" {
		return "consult is unanswered: the tier that thinks said nothing", false, nil
	}
	return answer, false, nil
}

// errString is the refusal's tail: the error if one exists, a plain word for
// the model that got back nothing at all.
func errString(err error) string {
	if err == nil {
		return "no answer came back"
	}
	return err.Error()
}
