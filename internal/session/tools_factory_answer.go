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

// The post-PR actions `factory_answer` takes, spelled once for the schema,
// the status lines (internal/factory's manager_facts.go) and the door. Each is
// pressed through the conversation's [ManagerPresser], the door the job
// page's own buttons take, so the factory checks the same permission and the
// same merge gate. The decisions on a stop are beside [ManagerPress]
// (tools_factory_manager.go).
const (
	ActionMerge     = "merge"
	ActionMarkReady = "mark_ready"
	ActionApprove   = "approve"
	// ActionResume tells a job's Auto to carry on past the failing checks it
	// paused on. It is the person's alone: it is pressed only with their own
	// words from the running turn ([Agent.turnPersonWords]).
	ActionResume = "resume"
	// ActionOfferMerge shows the person the job page's Merge card again, as
	// it stands now, when they ask to merge or whether the job is ready to.
	// It merges nothing: the merge stays their press on the card.
	ActionOfferMerge = "offer_merge"
)

// The words `factory_answer` hands back when nothing waits on the manager or
// the call says neither or both.
const (
	answerNothingWaits = factoryAnswerRefused + "nothing changed: no step's question is waiting on you on this turn"
	answerNeedsOne     = factoryAnswerRefused + "Invalid arguments: factory_answer needs answer, or ask_person with one line of why, not both."
)

const factoryAnswerDescription = "Reply to a step question. During a temporary inbox turn, answer the supplied question. On an ordinary manager turn, read factory_status and pass its question_id to save the person's answer or delegated engineering choice durably; a stale question is refused. To send a waiting plan back with requested changes, use action=reject_plan and put the person's words in answer. When the person asks here to merge the pull request, or whether it is ready to merge, read factory_status first: when it says the person is asked to merge, use action=offer_merge with their words in answer, which shows them the Merge card again, updated; when it says merge is not offered, tell them why in its words and offer nothing. Use action=merge only when the person, with the card shown, tells you here to merge it now. When factory_status says the person is asked to mark the pull request ready, and the person asks for exactly that here, use action=mark_ready with their words in answer, and action=approve when it says publishing waits on their approval; when it says Auto is paused on a failing check and the person asks here to resume or continue, use action=resume with their words in answer; never on your own judgment. Never promise a state change before an accepted tool result. If an action fails, explain plainly that it did not happen, without tool names or internal identifiers; if the outcome is uncertain, say it is unconfirmed and read status before retrying. " +
	"answer is your answer in words, and the step goes on with it at once: answer yourself when the issue, the recipe, the stages, the notes or what the person said here settle it. " +
	"ask_person is one line of why, and sends the question to the person in this conversation; the step waits for their answer. " +
	"Send it to the person when it turns on taste, scope, risk, credentials or access, money, or anything the issue and the recipe do not say. Never guess an answer to spare them." +
	" " + factoryAnswerStops

// factoryAnswerStops is the description's part on a stop handed to the
// manager and on what it may press by itself. It is here, and in
// factory_status, because a job made before these actions keeps the brief it
// was made with.
const factoryAnswerStops = "A stop that is not a step's question comes to you first too (factory_status says the job waits on you, the manager). Settle it with one action, your instructions or reason in answer: " +
	"another_round, when a step ran out of rounds or a check found something you want fixed: the step gets one more round with your instructions; " +
	"send_back, when the fault is in the step before: the work goes back to it, with why; " +
	"rerun, when a step failed or its results were not saved: the step runs again as it was asked (step names it); " +
	"accept, when a check's finding or a step's result is fine as it stands: it is taken, with your reason recorded where the person sees it; " +
	"resume_pipeline, when Follow PR stopped (its limit, a scope question, a conflict, a reviewer asking for changes): it runs another round on the open pull request with your instructions; " +
	"retry_publish, when a publish the person already approved failed: it is tried again; " +
	"wait_github, when nothing is yours to do and the pull request waits on its checks or a review; " +
	"hand_to_person, for anything you cannot settle and for money, access, taste or risk: the stop goes to the person with one plain sentence of why, and it is always allowed. After two extra rounds on one step its next stop goes to the person. " +
	"approve_acts approves the public acts that wait for a yes, and decline_acts drops them, with why in answer. " +
	"What you may press is checked in code. merge is pressed only on a turn where the person told you to merge, in Manual and in Auto. In Manual, approve, mark_ready and approve_acts are pressed only on a turn where the person said so in their own words; in Auto you may press them yourself. A turn the factory started carries no words of theirs."

const factoryAnswerSchemaJSON = `{"type":"object","properties":{` +
	`"action":{"type":"string","enum":["answer","reject_plan","merge","mark_ready","approve","resume","offer_merge","another_round","send_back","rerun","accept","resume_pipeline","retry_publish","wait_github","hand_to_person","approve_acts","decline_acts"],"description":"Default answer saves a step reply. reject_plan sends only a waiting plan back with answer as the requested changes; never approves or continues it. offer_merge shows the person the job page's Merge card again, updated, when they ask to merge or whether it is ready; it merges nothing. merge presses Merge on the job's pull request, mark_ready marks its draft ready for review, approve approves publishing the checked work and resume tells Auto to carry on past the failing checks it paused on, each only when factory_status offers it and the person asked for it; answer carries the person's words. On a stop handed to you: another_round, send_back, rerun (with step), accept, resume_pipeline, retry_publish, wait_github or hand_to_person, with your instructions or reason in answer. approve_acts and decline_acts answer the public acts that wait for a yes."},` +
	`"step":{"type":"string","description":"The step a decision on a stop is about, by its name: the step rerun runs again. Leave it out for the step the job stopped on."},` +
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
				Step       string `json:"step"`
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
			if needs, ok := stopDecisions[parsed.Action]; ok {
				// A DECISION ON A STOP carries the manager's own words, and is
				// never also a question sent to the person.
				if why != "" || (needs && answer == "") {
					return factoryAnswerFailure("Nothing changed: " + parsed.Action + " needs your instructions or reason in answer, and no ask_person."), true, nil
				}
				line, bad := a.press(ctx, ManagerPress{Action: parsed.Action, Step: strings.TrimSpace(parsed.Step), Words: strings.TrimSpace(parsed.Answer)})
				return line, bad, nil
			}
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
			if parsed.Action == ActionResume {
				d, ok := a.runDoorForTurn().(ManagerPresser)
				if !ok || answer == "" || why != "" {
					return factoryAnswerFailure("Nothing changed: resuming Auto needs the person's words in answer, on an ordinary manager turn."), true, nil
				}
				// ONLY THE PERSON OVERRIDES A PAUSE. The words recorded are the
				// ones they sent in this turn, read from the session and never
				// from the model's arguments, and a turn with none — a runner's
				// note, factory work, a wake — cannot resume anything.
				said := a.turnPersonWords()
				if said == "" {
					return factoryAnswerFailure(factory.ManagerResumeRefusal), true, nil
				}
				line, err := d.Press(ctx, a.config.SessionFile, ManagerPress{Action: ActionResume, Words: said, Said: said})
				if err != nil {
					return factoryAnswerFailure(err.Error()), true, nil
				}
				return line, false, nil
			}
			if parsed.Action == ActionMerge || parsed.Action == ActionMarkReady || parsed.Action == ActionApprove || parsed.Action == ActionOfferMerge {
				d, ok := a.runDoorForTurn().(ManagerPresser)
				if !ok || answer == "" || why != "" {
					return factoryAnswerFailure("Nothing changed: merging, offering the merge, marking ready or approving publication needs the person's words in answer, on an ordinary manager turn."), true, nil
				}
				// MERGING IS THE PERSON'S IN EVERY MODE, so it is refused here
				// before the door is asked, and again at the door, which also
				// holds approve and mark_ready to the job's mode
				// (internal/factory's manager_authority.go).
				said := a.turnPersonWords()
				if parsed.Action == ActionMerge && said == "" {
					return factoryAnswerFailure(factory.ManagerMergeRefusal), true, nil
				}
				line, err := d.Press(ctx, a.config.SessionFile, ManagerPress{Action: parsed.Action, Words: answer, Said: said})
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
	if !ok || asker == nil {
		asker, ok = a.config.Asker, a.config.Asker != nil
	}
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
