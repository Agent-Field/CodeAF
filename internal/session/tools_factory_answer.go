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
//     to that turn ([InboxDoor]). Ordinary manager turns use the durable
//     ManagerStatusDoor to answer the exact saved question after restart.

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

// ManagerPRDecider presses the job page's post-PR decisions for the person who
// asked in the manager's conversation: Merge, or Mark ready on a draft. It is
// the door the job page's own buttons take, so the factory checks the same
// permission and the same merge gate; the manager gains no power the person
// does not have. action is ActionMerge, ActionMarkReady or ActionApprove (the
// job's publication approval).
type ManagerPRDecider interface {
	DecidePR(ctx context.Context, conversation, action, words string) (string, error)
}

// The post-PR actions `factory_answer` takes, spelled once for the schema,
// the status lines (internal/factory's manager_facts.go) and the door.
const (
	ActionMerge     = "merge"
	ActionMarkReady = "mark_ready"
	ActionApprove   = "approve"
)

// The words `factory_answer` hands back when nothing waits on the manager or
// the call says neither or both.
const (
	answerNothingWaits = factoryAnswerRefused + "nothing changed: no step's question is waiting on you on this turn"
	answerNeedsOne     = factoryAnswerRefused + "Invalid arguments: factory_answer needs answer, or ask_person with one line of why, not both."
)

const factoryAnswerDescription = "Reply to a step question. During a temporary inbox turn, answer the supplied question. On an ordinary manager turn, read factory_status and pass its question_id to save the person's answer or delegated engineering choice durably; a stale question is refused. To send a waiting plan back with requested changes, use action=reject_plan and put the person's words in answer. When factory_status says the person is asked to merge the pull request or mark it ready, and the person asks for exactly that here, use action=merge or action=mark_ready with their words in answer, and action=approve when it says publishing waits on their approval; never on your own judgment. Never promise a state change before an accepted tool result. If an action fails, explain plainly that it did not happen, without tool names or internal identifiers; if the outcome is uncertain, say it is unconfirmed and read status before retrying. " +
	"answer is your answer in words, and the step goes on with it at once: answer yourself when the issue, the recipe, the stages, the notes or what the person said here settle it. " +
	"ask_person is one line of why, and sends the question to the person in this conversation; the step waits for their answer. " +
	"Send it to the person when it turns on taste, scope, risk, credentials or access, money, or anything the issue and the recipe do not say. Never guess an answer to spare them."

const factoryAnswerSchemaJSON = `{"type":"object","properties":{` +
	`"action":{"type":"string","enum":["answer","reject_plan","merge","mark_ready","approve"],"description":"Default answer saves a step reply. reject_plan sends only a waiting plan back with answer as the requested changes; never approves or continues it. merge presses Merge on the job's pull request, mark_ready marks its draft ready for review and approve approves publishing the checked work, each only when factory_status offers it and the person asked for it; answer carries the person's words."},` +
	`"question_id":{"type":"string","description":"The exact pending question_id returned by factory_status, required for a persisted step reply or a plan send-back outside a temporary inbox turn."},` +
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
				Action     string `json:"action"`
				QuestionID string `json:"question_id"`
				Answer     string `json:"answer"`
				AskPerson  string `json:"ask_person"`
			}
			if len(args) > 0 {
				if err := decodeToolArguments(args, &parsed); err != nil {
					return rejectToolArguments(ctx, "Invalid arguments: "+err.Error()), true, nil
				}
			}
			answer := strings.Join(strings.Fields(parsed.Answer), " ")
			why := strings.Join(strings.Fields(parsed.AskPerson), " ")
			if (answer == "") == (why == "") {
				return answerNeedsOne, true, nil
			}
			if parsed.Action == "reject_plan" {
				d, ok := a.runDoorForTurn().(ManagerPlanRejecter)
				if !ok || answer == "" || why != "" {
					return factoryAnswerFailure("Nothing changed: sending a plan back requires requested changes on an ordinary manager turn."), true, nil
				}
				line, err := d.RejectPendingPlan(ctx, a.config.SessionFile, parsed.QuestionID, strings.TrimSpace(parsed.Answer))
				if err != nil {
					return factoryAnswerFailure(err.Error()), true, nil
				}
				return line, false, nil
			}
			if parsed.Action == ActionMerge || parsed.Action == ActionMarkReady || parsed.Action == ActionApprove {
				d, ok := a.runDoorForTurn().(ManagerPRDecider)
				if !ok || answer == "" || why != "" {
					return factoryAnswerFailure("Nothing changed: merging, marking ready or approving publication needs the person's words in answer, on an ordinary manager turn."), true, nil
				}
				line, err := d.DecidePR(ctx, a.config.SessionFile, parsed.Action, answer)
				if err != nil {
					return factoryAnswerFailure(err.Error()), true, nil
				}
				return line, false, nil
			}
			if parsed.Action != "" && parsed.Action != "answer" {
				return factoryAnswerFailure("Nothing changed: unknown action."), true, nil
			}
			if d, ok := a.runDoorForTurn().(ManagerStatusDoor); ok {
				if answer == "" {
					return factoryAnswerFailure("Nothing changed: the persisted question is already with the person."), true, nil
				}
				line, err := d.AnswerPending(ctx, a.config.SessionFile, parsed.QuestionID, answer)
				if err != nil {
					return factoryAnswerFailure(err.Error()), true, nil
				}
				return line, false, nil
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
				return factoryAnswerFailure(err.Error()), true, nil
			}
			return line, false, nil
		},
	}
}

const factoryAnswerRefused = "Nothing changed. Tell the person plainly that the requested action did not happen and the job has not advanced; do not promise work has started or repeat tool names or internal identifiers. Read factory_status on an ordinary manager turn before retrying; during a temporary inbox turn use the supplied question. Recovery detail: "

// factoryAnswerFailure distinguishes an explicit refusal from an uncertain write.
func factoryAnswerFailure(detail string) string {
	if strings.HasPrefix(strings.ToLower(detail), "nothing changed") {
		return factoryAnswerRefused + detail
	}
	return "Action not confirmed. Read factory_status before retrying and use its current question_id and supported action. Tell the person plainly that the requested action was not confirmed; do not promise work has started or repeat tool names or internal identifiers. Recovery detail: " + detail
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
	// A JUDGEMENT IS A PERSON'S ([AskJudgement]): the step's question is
	// marked so, and the factory never lets its manager answer it.
	out := factory.Asked{Question: strings.Join(strings.Fields(q.Head), " "), Person: q.Ask == AskJudgement}
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
