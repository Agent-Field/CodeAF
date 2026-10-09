package session

// THE INBOX: A STEP'S QUESTION GOES TO THE MANAGER FIRST.
//
// The manager conversation is the one inbox of its item (the owner's decision
// of 2026-10-09). Two halves live here:
//
//   - A STAGE'S `ask` DOES NOT REACH THE PERSON. In a conversation that is one
//     step of a factory run ([Config.Stage]), a clarifying question (a
//     clarification, a choice, a confirmation, a judgement) is handed to the
//     stage door when the door takes questions ([StageAsker]); the runner
//     puts it to the item's manager and, when the manager sends it on, to the
//     person, and the call returns the answer in words. THE CALL WAITS: the
//     step has nothing to do until it is answered, and a pause or a stop
//     ends the wait through ctx. A permission, a landing, an assumption and
//     a ratify keep their own roads ([askGoesUp]).
//   - `factory_answer` IS THE MANAGER'S REPLY. On the turn the runner gives
//     the manager with the question, `factory_answer` either answers it, and
//     the step goes on with the words, or sends it to the person with one line
//     of why, and the step waits for them. It answers through the door lent
//     to that turn ([InboxDoor]); on any other turn nothing is waiting on the
//     manager, and it says so with nothing done.

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/Agent-Field/codeaf/internal/exec/bare"
	"github.com/Agent-Field/codeaf/internal/factory"
)

// StageAsker is a [StageDoor] that also takes the stage's questions: Ask
// puts one to the item's manager (and on to the person when the manager sends
// it), and answers the answer in words. It waits until there is one, or ctx
// ends. The runner's stage door answers it; a door that does not leaves `ask`
// as it is everywhere else.
type StageAsker interface {
	Ask(ctx context.Context, q factory.Asked) (string, error)
}

// InboxDoor is what `factory_answer` answers through: the door the runner
// lends the manager's turn on a step's question. conversation is the asking
// conversation's session file. Each answers the line the model reads back.
type InboxDoor interface {
	AnswerStep(ctx context.Context, conversation, answer string) (string, error)
	SendOn(ctx context.Context, conversation, why string) (string, error)
}

// The words `factory_answer` hands back when nothing waits on the manager or
// the call says neither or both.
const (
	answerNothingWaits = "nothing changed: no step's question is waiting on you on this turn"
	answerNeedsOne     = "Invalid arguments: factory_answer needs answer, or ask_person with one line of why, not both."
)

const factoryAnswerDescription = "Reply to the question a step of your item asked you; the runner hands you the question on its own turn. " +
	"answer is your answer in words, and the step goes on with it at once: answer yourself when the issue, the recipe, the stages, the notes or what the person said here settle it. " +
	"ask_person is one line of why, and sends the question to the person in this conversation; the step waits for their answer. " +
	"Send it to the person when it turns on taste, scope, risk, credentials or access, money, or anything the issue and the recipe do not say. Never guess an answer to spare them."

const factoryAnswerSchemaJSON = `{"type":"object","properties":{` +
	`"answer":{"type":"string","description":"Your answer in words, for the step."},` +
	`"ask_person":{"type":"string","description":"One line of why the person decides it."}` +
	`},"additionalProperties":false}`

// The gloss a person reads beside the call is the answer, or the reason.
func init() { glossField["factory_answer"] = "answer" }

// factoryAnswerTool is the manager's reply to a step's question.
func (a *Agent) factoryAnswerTool() bare.Tool {
	return bare.Tool{
		Name:        "factory_answer",
		Description: factoryAnswerDescription,
		Schema:      json.RawMessage(factoryAnswerSchemaJSON),
		Execute: func(ctx context.Context, args json.RawMessage) (string, bool, error) {
			var parsed struct {
				Answer    string `json:"answer"`
				AskPerson string `json:"ask_person"`
			}
			if len(args) > 0 {
				if err := decodeToolArguments(args, &parsed); err != nil {
					return "Invalid arguments: " + err.Error(), true, nil
				}
			}
			answer := strings.Join(strings.Fields(parsed.Answer), " ")
			why := strings.Join(strings.Fields(parsed.AskPerson), " ")
			if (answer == "") == (why == "") {
				return answerNeedsOne, true, nil
			}
			door, ok := a.runDoorForTurn().(InboxDoor)
			if !ok || door == nil {
				return answerNothingWaits, true, nil
			}
			var line string
			var err error
			if answer != "" {
				line, err = door.AnswerStep(ctx, a.config.SessionFile, answer)
			} else {
				line, err = door.SendOn(ctx, a.config.SessionFile, why)
			}
			if err != nil {
				return "nothing changed: " + strings.Join(strings.Fields(err.Error()), " "), true, nil
			}
			return line, false, nil
		},
	}
}

// askInbox is `ask`'s road in a stage conversation: the question goes to the
// item's manager through the stage door, and the call answers what came back.
// ok is false when this is not that road (no stage, a door that takes no
// questions, a kind that keeps its own road, or a door that could not take
// it), and `ask` goes on as it does everywhere else.
func (a *Agent) askInbox(ctx context.Context, q Question) (string, bool, error) {
	asker, ok := a.config.Stage.(StageAsker)
	if !ok || asker == nil || !askGoesUp(q.Ask, true) {
		return "", false, nil
	}
	said, err := asker.Ask(ctx, stageAsked(q))
	if err != nil {
		if ctx.Err() != nil {
			return "", true, ctx.Err()
		}
		return "", false, nil
	}
	return said, true, nil
}

// stageAsked is q as the runner keeps it: the question in one line, its
// answers by their labels, and the asker's pick by its label with its reason.
func stageAsked(q Question) factory.Asked {
	out := factory.Asked{Question: strings.Join(strings.Fields(q.Head), " ")}
	if reason := strings.Join(strings.Fields(q.Reason), " "); reason != "" && out.Question != "" {
		out.Question += " (" + reason + ")"
	}
	labels := map[string]string{}
	for _, o := range q.Options {
		label := strings.Join(strings.Fields(o.Label), " ")
		if label == "" {
			continue
		}
		labels[o.Key] = label
		out.Options = append(out.Options, label)
	}
	if q.Pick != nil {
		if label := labels[q.Pick.Key]; label != "" {
			out.Pick = label
			if reason := strings.Join(strings.Fields(q.Pick.Reason), " "); reason != "" {
				out.Pick += ", because " + reason
			}
		}
	}
	return out
}
