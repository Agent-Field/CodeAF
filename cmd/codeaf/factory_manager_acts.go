package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/session"
)

// ── THE MANAGER'S PUBLIC ACTS, ITS OPEN TASK AND ITS DECISIONS ON A STOP ────
//
// These are the presses phase 2 of the reliability work gave the job's
// manager. Each is one POST to this machine's factory through the app's own
// press ([appLocalPress]), as every other manager press is; the factory's
// handler decides what happens, and a refusal comes back in its words.
//
// THE FACTORY CANNOT TELL THE MANAGER FROM THE PERSON on the app's own
// factory: both arrive with the one credential. So the caller says who is
// asking, in the body's `by`, and this file is the only place the manager's
// tools set it. It is always [byMainAgent] here: the manager never presses as
// the person through `act` or `task`, whatever the person said in the turn.

// The job verbs pressed here, and who the manager says it is.
const (
	factoryVerbAct          = "act"
	factoryVerbActsApprove  = "acts_approve"
	factoryVerbActsDecline  = "acts_decline"
	factoryVerbTask         = "task"
	factoryVerbManagerDecid = "manager_decide"
	byMainAgent             = "main_agent"
)

// actBody is the body of `act`: the acts proposed, who proposes them and one
// line of why.
type actBody struct {
	Acts []session.PublicAct `json:"acts"`
	By   string              `json:"by"`
	Why  string              `json:"why"`
}

// taskBody is the body of `task`.
type taskBody struct {
	Words string `json:"words"`
	Now   bool   `json:"now"`
	By    string `json:"by"`
}

// decideBody is the body of `manager_decide`.
type decideBody struct {
	Decision string `json:"decision"`
	Step     string `json:"step"`
	Words    string `json:"words"`
}

// wordsBody is the body of `acts_decline`.
type wordsBody struct {
	Words string `json:"words"`
}

// managerDecisions are the decisions on a stop, each with the line the
// manager reads back when the factory took it.
var managerDecisions = map[string]string{
	session.ActionAnotherRound:   "The step gets another round with your instructions.",
	session.ActionSendBack:       "The work is sent back to the step before, with your reason.",
	session.ActionRerun:          "The step is queued to run again.",
	session.ActionAccept:         "Accepted as it stands, with your reason on the job's record where the person sees it. The job carries on.",
	session.ActionResumePipeline: "Follow PR is queued for another round on the pull request.",
	session.ActionRetryPublish:   "The approved publish is queued to be tried again.",
	session.ActionHandToPerson:   "The stop is handed to the person, with your reason. The job waits on them; say that reason to them here in one plain sentence.",
	session.ActionWaitGitHub:     "The job goes back to waiting on GitHub.",
}

// pressed sends one verb and reads its refusal as the other presses do
// ([pressRefused]): an uncertain write is not a refusal, anything else is
// "Nothing changed" and the factory's sentence.
func pressed(ctx context.Context, press *localJobPress, job int, verb string, body any) error {
	if press == nil {
		return errNoPress
	}
	if err := (*press)(ctx, job, verb, body); err != nil {
		return pressRefused(err)
	}
	return nil
}

// What `factory_act` reads back about a push, which is the publish approval
// and never an act the factory keeps.
const (
	// pushPressed is a push the manager's ask approved: Auto lets it.
	pushPressed = "The push is approved by you, as Auto allows. It goes out when its Undo window closes; tell the person you approved it."
	// pushWaits follows why a push was left waiting on a person.
	pushWaits = "It goes out when publishing is approved; factory_status says who approves it and how."
	// pushUnread is a push the factory took whose road the job as last read
	// does not show.
	pushUnread = "The push is with the factory; read factory_status for where it stands."
)

// pressActs proposes public acts on the job's pull request as the manager.
//
// A MERGE IS NEVER SENT AS AN ACT. Whatever the mode and whatever the person
// said, `factory_act` with kind merge only shows the person the Merge card
// again, through the door `factory_answer`'s offer_merge takes
// ([pressOfferMerge]); the merge stays their press. The other acts go to the
// factory in one `act`, and the factory holds them to the job's mode: carried
// out in Auto, waiting for the person's approval in Manual.
func pressActs(ctx context.Context, press *localJobPress, it factory.Item, acts []session.PublicAct, why string, reread func() (factory.Item, error)) (string, error) {
	var send []session.PublicAct
	merge := false
	for _, act := range acts {
		if act.Kind == session.ActMerge {
			merge = true
			continue
		}
		send = append(send, act)
	}
	if len(send) == 0 && !merge {
		return "", errors.New("Nothing changed: no act was given")
	}
	var lines []string
	if len(send) > 0 {
		// A PUSH IS NOT KEPT AS AN ACT: it is the publish approval, pressed or
		// left waiting ([factory.PushRoad]). What the factory will do with it
		// is read from the job as it stood before the press, as the factory
		// read it, so the line says which.
		var kept []session.PublicAct
		pushes := 0
		for _, act := range send {
			if act.Kind == session.ActPush || act.Kind == session.ActOpenPR {
				pushes++
				continue
			}
			kept = append(kept, act)
		}
		road, roadWords, roadErr := factory.PushRoad(it, factory.ActByMainAgent)
		if err := pressed(ctx, press, it.ID, factoryVerbAct, actBody{Acts: send, By: byMainAgent, Why: why}); err != nil {
			return "", err
		}
		if len(kept) > 0 {
			what := actsWords(kept)
			if factory.Automatic(it) {
				lines = append(lines, "Accepted: "+what+". This job is in Auto, so the factory carries this out now and writes each act to the job's log. Nothing is posted until factory_status shows it done.")
			} else {
				lines = append(lines, "Proposed: "+what+". This job is not in Auto, so nothing is sent until the person approves. Tell them exactly that, and show them the text that will be posted. They approve by saying yes here.")
			}
		}
		switch {
		case pushes == 0:
		case roadErr != nil:
			lines = append(lines, pushUnread)
		case road == factory.RoadPress:
			lines = append(lines, pushPressed)
		default:
			lines = append(lines, "The push was not sent. "+roadWords+" "+pushWaits)
		}
		if reread != nil {
			if now, err := reread(); err == nil {
				if acts := factory.ManagerActsWords(now); len(acts) > 0 {
					lines = append(lines, "Now on the job: "+strings.Join(acts, " "))
				}
			}
		}
	}
	if merge {
		line, err := pressOfferMerge(ctx, press, it)
		switch {
		case err != nil && len(send) == 0:
			return "", err
		case err != nil:
			lines = append(lines, "The merge was not offered to the person. "+err.Error())
		default:
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, " "), nil
}

// actsWords says a set of acts in a line: "2 public acts (push, reply)".
func actsWords(acts []session.PublicAct) string {
	kinds := make([]string, len(acts))
	for i, act := range acts {
		kinds[i] = act.Kind
	}
	noun := "public acts"
	if len(acts) == 1 {
		noun = "public act"
	}
	return fmt.Sprintf("%d %s (%s)", len(acts), noun, strings.Join(kinds, ", "))
}

// pressActsApprove approves every public act that waits on the person. said
// is whether the person spoke in this turn: the line says whose yes it was.
func pressActsApprove(ctx context.Context, press *localJobPress, it factory.Item, said bool, reread func() (factory.Item, error)) (string, error) {
	if err := pressed(ctx, press, it.ID, factoryVerbActsApprove, guardBody{}); err != nil {
		return "", err
	}
	line := "The proposed public acts are approved"
	if said {
		line += ", as the person"
	}
	line += ". The factory carries them out and writes each to the job's log. This confirms the approval, not the acts; read factory_status for the outcome."
	if reread != nil {
		if now, err := reread(); err == nil {
			if acts := factory.ManagerActsWords(now); len(acts) > 0 {
				line += " Now on the job: " + strings.Join(acts, " ")
			}
		}
	}
	return line, nil
}

// pressActsDecline drops the public acts that wait on the person.
func pressActsDecline(ctx context.Context, press *localJobPress, it factory.Item, words string) (string, error) {
	if err := pressed(ctx, press, it.ID, factoryVerbActsDecline, wordsBody{Words: words}); err != nil {
		return "", err
	}
	return "The proposed public acts are declined. Nothing was sent.", nil
}

// pressTask sends a delegate into the job's checkout with the manager's
// instructions.
func pressTask(ctx context.Context, press *localJobPress, it factory.Item, instructions string, now bool) (string, error) {
	if err := pressed(ctx, press, it.ID, factoryVerbTask, taskBody{Words: instructions, Now: now, By: byMainAgent}); err != nil {
		return "", err
	}
	when := "It runs when the round in progress ends, or at once when nothing is running."
	if now {
		when = "It interrupts the round in progress and runs now."
	}
	return "Task taken: a delegate does it in the job's checkout. " + when + " If it changes files, the checks that had passed run again. Anything public that follows keeps the job's Manual or Auto rule. Read factory_status for the outcome.", nil
}

// pressManagerDecision presses one decision on a stop the manager holds.
func pressManagerDecision(ctx context.Context, press *localJobPress, it factory.Item, decision, step, words string) (string, error) {
	if err := pressed(ctx, press, it.ID, factoryVerbManagerDecid, decideBody{Decision: decision, Step: step, Words: words}); err != nil {
		return "", err
	}
	return managerDecisions[decision] + " This confirms the decision, not its outcome; read factory_status for that.", nil
}
