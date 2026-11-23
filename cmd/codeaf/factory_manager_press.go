package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"github.com/Agent-Field/codeaf/internal/session"
)

// ── THE MANAGER PRESSES WHAT THE JOB PAGE OFFERS, AS THE PERSON ────────────
//
// On the 2026-11-06 live audit the job page asked "Merge pull request #161?"
// and the person told the job's manager "approve it"; the manager could not
// see the card and had no verb to press it. factory_status now carries the
// decision ([factory.StatusForManager]), and `factory_answer` with
// action=merge or action=mark_ready presses it here.
//
// THE PRESS IS THE JOB PAGE'S OWN: the same POST the desk's Merge and Mark
// ready buttons send to this machine's factory ([appBackend.FactoryJobMerge],
// [appBackend.FactoryJobPRReady]), with the same credential, so the factory
// checks the same ownership, the same merge gate and the same GitHub sign-in.
// The manager gains nothing the person does not have. A merge pressed here
// waits out the same Undo window.
//
// ONLY THE APP HAS THAT DOOR. A terminal chat or a window over --host has no
// signed-in factory connection to send it through, and is told so in words.
//
// EVERY PRESS GOES THROUGH [seamRunDoor.Press], which holds the owner's
// authority table of 2026-11-17 before anything is sent: a merge only on the
// person's word, and outside Auto an approval only on it too. The public
// acts, the open task and the decisions on a stop are in
// factory_manager_acts.go.

// localJobPress posts one job verb to this machine's factory as the person.
type localJobPress func(ctx context.Context, job int, verb string, body any) error

// appLocalPress is the app's press, nil outside `codeaf app`.
var appLocalPress atomic.Pointer[localJobPress]

// installAppLocalPress hangs b's press for this process, and answers the
// function that takes it down again.
func installAppLocalPress(b *appBackend) func() {
	press := localJobPress(func(ctx context.Context, job int, verb string, body any) error {
		_, err := b.factoryHub().write(ctx, appLocalID, jobPath(job, verb), body)
		return err
	})
	appLocalPress.Store(&press)
	return func() { appLocalPress.CompareAndSwap(&press, nil) }
}

// pressAnswer is what the factory answered a press it did not take, as the
// manager's tool reads it back. It prints lead and THE FACTORY'S OWN SENTENCE,
// and still unwraps to the wire's error for a caller that asks its code.
type pressAnswer struct {
	lead string
	wire *appwire.Error
}

func (p pressAnswer) Error() string { return p.lead + p.wire.Message }
func (p pressAnswer) Unwrap() error { return p.wire }

// pressRefused is the error of a press the factory did not take. A refusal
// reads "Nothing changed: " and the factory's sentence. A write whose outcome
// is not known is no refusal: it keeps the factory's sentence with no lead,
// and the tool tells the model it was not confirmed.
//
// THE WIRE'S CODE WORD IS NEVER PART OF THE SENTENCE. The wire's error prints
// "conflict: …" or "bad_params: …", the app's own word for the kind of
// answer. The tools tell the model to say a refusal to the person in the
// factory's words, so until this the code word was read to them with it.
func pressRefused(err error) error {
	var wire *appwire.Error
	if !errors.As(err, &wire) {
		return fmt.Errorf("Nothing changed: %w", err)
	}
	if wire.Code == appwire.CodeUncertain {
		return pressAnswer{wire: wire}
	}
	return pressAnswer{lead: "Nothing changed: ", wire: wire}
}

// errNoPress is a press asked where no signed-in factory connection is.
var errNoPress = errors.New("Nothing changed: this window has no signed-in connection to the factory to press it with; the person can press it on the job page")

// Press is every press the manager's tools make (`factory_answer`,
// `factory_act`, `factory_task`) for the conversation that manages the job:
// refused in words unless the manager may press it and the job offers it now.
//
// THE AUTHORITY TABLE IS HELD HERE, ONCE, FOR EVERY TOOL ([managerMayPress]):
// what the manager may press by itself depends on the job's mode and on
// whether the person spoke in this turn, and no choice of tool or of words
// gets round it.
func (d seamRunDoor) Press(ctx context.Context, conversation string, p session.ManagerPress) (string, error) {
	it, err := d.managed(conversation)
	if err != nil {
		return "", fmt.Errorf("Nothing changed: %w", err)
	}
	if err := managerMayPress(it, p); err != nil {
		return "", err
	}
	press := appLocalPress.Load()
	reread := func() (factory.Item, error) { return d.managed(conversation) }
	switch p.Action {
	case session.ActionResume:
		return pressAutoResume(ctx, press, it, p.Said)
	case session.ActionOfferMerge:
		return pressOfferMerge(ctx, press, it)
	case session.ActionMerge, session.ActionMarkReady, session.ActionApprove:
		return pressPRDecisionBy(ctx, press, it, p.Action, strings.TrimSpace(p.Said) != "", reread)
	case session.ActionAct:
		return pressActs(ctx, press, it, p.Acts, p.Words, reread)
	case session.ActionTask:
		return pressTask(ctx, press, it, p.Words, p.Now)
	case session.ActionApproveActs:
		return pressActsApprove(ctx, press, it, strings.TrimSpace(p.Said) != "", reread)
	case session.ActionDeclineActs:
		return pressActsDecline(ctx, press, it, p.Words)
	}
	if _, ok := managerDecisions[p.Action]; ok {
		return pressManagerDecision(ctx, press, it, p.Action, p.Step, p.Words)
	}
	return "", errors.New("Nothing changed: unknown action")
}

// managerSameTurnRefusal is the manager approving, outside Auto, acts it
// proposed in the same turn.
const managerSameTurnRefusal = "Nothing changed: you proposed these in this same turn, so the person has not read them yet. Show them exactly what will be posted or changed, and approve when they answer."

// managerMayPress holds one press to the authority table
// ([factory.ManagerMay]): merging and resuming a paused Auto are pressed only
// on a turn the person spoke in; outside Auto so are approving publication,
// marking the pull request ready and approving proposed acts. Everything else
// is the manager's in both modes.
func managerMayPress(it factory.Item, p session.ManagerPress) error {
	said := strings.TrimSpace(p.Said) != ""
	switch p.Action {
	case session.ActionMerge:
		return factory.ManagerMay(it, factory.ManagerMerge, said)
	case session.ActionResume:
		return factory.ManagerMay(it, factory.ManagerResumeAuto, said)
	case session.ActionMarkReady:
		return factory.ManagerMay(it, factory.ManagerMarkReady, said)
	case session.ActionApprove:
		if err := factory.ManagerMay(it, factory.ManagerApprove, said); err != nil {
			return err
		}
		// THE PUBLISH APPROVAL CARRIES THE ACTS THAT WAIT BESIDE THE PUSH
		// (serve's approveActsWithPush), so it is the same yes as approving
		// them and is held to the same rule: a reply proposed in this turn
		// would go out with the push the person approved.
		if factory.OffersPublishApproval(it) && actsUnread(it, p.TurnAt) {
			return errors.New(managerSameTurnRefusal)
		}
		return nil
	case session.ActionApproveActs:
		if err := factory.ManagerMay(it, factory.ManagerApproveActs, said); err != nil {
			return err
		}
		if actsUnread(it, p.TurnAt) {
			return errors.New(managerSameTurnRefusal)
		}
		return nil
	}
	return nil
}

// actsUnread says whether, outside Auto, the job holds a proposed act that
// began in the turn that started at turnAt. THE PERSON'S YES IS FOR WHAT THEY
// HAVE READ: outside Auto, acts proposed in this same turn have not been shown
// to them yet, because their words opened the turn before the acts existed.
// Without this the manager could propose a reply and approve it in one breath.
// A zero turnAt says nothing is known of the turn, so nothing is held back.
func actsUnread(it factory.Item, turnAt time.Time) bool {
	if factory.Automatic(it) || turnAt.IsZero() {
		return false
	}
	for _, act := range factory.ProposedActs(it) {
		if !act.At.Before(turnAt) {
			return true
		}
	}
	return false
}

// pressAutoResume tells a job's Auto to carry on past the failing checks it
// paused on, as the person, with the words they sent (factory's override.go).
// The factory checks the caller may approve the job and records who overrode
// which finding on the job's history.
func pressAutoResume(ctx context.Context, press *localJobPress, it factory.Item, words string) (string, error) {
	found, err := sendAutoResume(ctx, press, it, words)
	if err != nil {
		return "", err
	}
	return "Auto resumed, as the person: it carries on past " + found + ", and the job's history records the override. A new failing check pauses it again. This confirms the override, not the publication; read factory_status for the outcome.", nil
}

// sendAutoResume is the press itself, for every caller that makes it (the
// job's manager here, the factory's own manager in factory_foreman_press.go):
// the rule on the job as saved, then the one verb with the person's words. It
// answers the findings the override carries the job past.
func sendAutoResume(ctx context.Context, press *localJobPress, it factory.Item, words string) (string, error) {
	if !factory.OffersAutoResume(it) {
		return "", errors.New("Nothing changed: Auto is not paused on a failing check")
	}
	if press == nil {
		return "", errNoPress
	}
	found := factory.OverrideSummary(factory.FailingFindings(it))
	if err := (*press)(ctx, it.ID, serve.ResumeAutoVerb, struct {
		Words string `json:"words,omitempty"`
	}{words}); err != nil {
		return "", pressRefused(err)
	}
	return found, nil
}

// pressOfferMerge shows the person the job page's Merge card again, through
// the job's offer_merge door, when the page offers Merge now (factory's
// merge_offer.go). When it does not, it refuses with the gate's own words, so
// the manager says why and offers nothing. It merges nothing.
func pressOfferMerge(ctx context.Context, press *localJobPress, it factory.Item) (string, error) {
	probe := it
	if err := factory.OfferMerge(&probe, time.Now()); err != nil {
		return "", errors.New("Nothing changed: " + err.Error())
	}
	if press == nil {
		return "", errNoPress
	}
	if err := (*press)(ctx, it.ID, serve.OfferMergeVerb, guardBody{}); err != nil {
		return "", pressRefused(err)
	}
	return fmt.Sprintf("The Merge card for pull request #%d is shown to the person again on the job page, as it stands now. Nothing is merged: the merge stays their press on that card, with its Undo window.", it.PullRequest.Number), nil
}

// pressPRDecision checks the decision against the job as saved and presses it.
// reread reads the job again after a press that changes what the page offers.
func pressPRDecision(ctx context.Context, press *localJobPress, it factory.Item, action string, reread func() (factory.Item, error)) (string, error) {
	return pressPRDecisionBy(ctx, press, it, action, true, reread)
}

// pressPRDecisionBy is [pressPRDecision] knowing whose decision it was: said
// is whether the person spoke in the turn. An approval the manager gave by
// itself in Auto is not said back as the person's. THE FACTORY STILL RECORDS
// IT UNDER THE PERSON'S SIGN-IN: the app's own factory has one credential, and
// the approve door carries no word for who asked.
func pressPRDecisionBy(ctx context.Context, press *localJobPress, it factory.Item, action string, said bool, reread func() (factory.Item, error)) (string, error) {
	pr := it.PullRequest
	if pr == nil {
		pr = &factory.PullRequest{}
	}
	if err := sendPRDecision(ctx, press, it, action); err != nil {
		return "", err
	}
	if action == session.ActionApprove {
		// THE SAME APPROVAL PASSES THE FINAL REVIEW, which marks the draft
		// ready and publishes nothing. The line says which it was: read back
		// as "publishes the checked work", the manager told the person a push
		// was on its way.
		if w := it.Wait; w != nil && w.Action == factory.ActApproveReady {
			whose := "as the person"
			if !said {
				whose = "by you, as Auto allows"
			}
			return fmt.Sprintf("Approved, %s: the final review's result stands, and the pull request is marked ready for review when the Undo window (%s) on the job page closes. Nothing is pushed or merged. This confirms the approval, not the outcome; read factory_status for that.", whose, factory.ApprovalUndo), nil
		}
		if !said {
			return fmt.Sprintf("Publication approved by you, as Auto allows. It waits out its Undo window (%s) on the job page, then the factory publishes the checked work. This confirms the approval, not the publication; read factory_status for the outcome, and tell the person you approved it.", factory.ApprovalUndo), nil
		}
		return fmt.Sprintf("Publication approved, as the person. It waits out its Undo window (%s) on the job page, then the factory publishes the checked work. This confirms the approval, not the publication; read factory_status for the outcome.", factory.ApprovalUndo), nil
	}
	if action == session.ActionMarkReady {
		line := fmt.Sprintf("Pull request #%d is marked ready for review. Reviewers are notified; nothing was pushed or merged.", pr.Number)
		// WHAT THE PAGE OFFERS NOW, read from the job as the press left it.
		// The live walk's manager marked #177 ready and then said merge was
		// not offered, repeating the draft-era status, while the page offered
		// Merge. The reply carries the fresh merge gate, so it cannot.
		if reread != nil {
			if now, err := reread(); err == nil {
				if words := factory.PRDecisionWords(now); len(words) > 0 {
					line += " Now on the job page: " + strings.Join(words, " ")
				}
			}
		}
		return line, nil
	}
	return fmt.Sprintf("Merge pressed for pull request #%d, as the person. It waits out its Undo window (%s) on the job page, then the factory checks the gate again and merges the approved commit. This confirms the press, not the merge; read factory_status for the outcome.", pr.Number, factory.MergeUndo), nil
}

// sendPRDecision is the press itself, for every caller that makes it (the
// job's manager above, the factory's own manager in
// factory_foreman_press.go): the decision checked against the job as saved,
// then the one verb with the body its handler expects. An approval names the
// job's head commit and a merge the pull request's head, so each acts on what
// was read.
func sendPRDecision(ctx context.Context, press *localJobPress, it factory.Item, action string) error {
	pr := it.PullRequest
	if pr == nil {
		pr = &factory.PullRequest{}
	}
	switch action {
	case session.ActionMarkReady:
		if !factory.OffersMarkReady(it) {
			return errors.New("Nothing changed: this job has no open draft pull request to mark ready")
		}
	case session.ActionApprove:
		if !factory.OffersPublishApproval(it) {
			return errors.New("Nothing changed: no publication waits on an approval")
		}
		// ONLY A PERSON PASSES A PERSON'S APPROVE STEP: the manager never
		// presses it, even with the person's words.
		if st, _, ok := factory.WaitingGateStage(it); ok && factory.PersonOnly(st) {
			return errors.New(personOnlyApproval)
		}
	case session.ActionMerge:
		if v := factory.MergeOffered(it); !v.OK {
			return errors.New("Nothing changed: merge is not offered: " + v.Why)
		}
	default:
		return errors.New("Nothing changed: unknown action")
	}
	if press == nil {
		return errNoPress
	}
	var err error
	switch action {
	case session.ActionMarkReady:
		err = (*press)(ctx, it.ID, "pr-ready", guardBody{})
	case session.ActionApprove:
		err = (*press)(ctx, it.ID, factoryVerbApprove, api.Approve{Commit: it.HeadCommit})
	default:
		err = (*press)(ctx, it.ID, factoryVerbMerge, struct {
			Commit string      `json:"commit,omitempty"`
			Expect *api.Expect `json:"expect,omitempty"`
		}{Commit: pr.HeadSHA})
	}
	if err != nil {
		return pressRefused(err)
	}
	return nil
}

// personOnlyApproval is an approval only a person passes, asked of a manager.
const personOnlyApproval = "Nothing changed: only a person can pass this approval; they press Approve on the job page"
