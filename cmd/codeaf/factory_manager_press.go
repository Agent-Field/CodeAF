package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
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

// errNoPress is a press asked where no signed-in factory connection is.
var errNoPress = errors.New("Nothing changed: this window has no signed-in connection to the factory to press it with; the person can press it on the job page")

// DecidePR is `factory_answer`'s merge and mark_ready for the conversation
// that manages the job: refused in words unless the job page offers it now.
func (d seamRunDoor) DecidePR(ctx context.Context, conversation, action, _ string) (string, error) {
	it, err := d.managed(conversation)
	if err != nil {
		return "", err
	}
	return pressPRDecision(ctx, appLocalPress.Load(), it, action, func() (factory.Item, error) { return d.managed(conversation) })
}

// pressPRDecision checks the decision against the job as saved and presses it.
// reread reads the job again after a press that changes what the page offers.
func pressPRDecision(ctx context.Context, press *localJobPress, it factory.Item, action string, reread func() (factory.Item, error)) (string, error) {
	pr := it.PullRequest
	if pr == nil {
		pr = &factory.PullRequest{}
	}
	switch action {
	case session.ActionMarkReady:
		if !factory.OffersMarkReady(it) {
			return "", errors.New("Nothing changed: this job has no open draft pull request to mark ready")
		}
	case session.ActionApprove:
		if !factory.OffersPublishApproval(it) {
			return "", errors.New("Nothing changed: no publication waits on an approval")
		}
		// ONLY A PERSON PASSES A PERSON'S APPROVE STEP: the manager never
		// presses it, even with the person's words.
		if st, _, ok := factory.WaitingGateStage(it); ok && factory.PersonOnly(st) {
			return "", errors.New("Nothing changed: only a person can pass this approval; they press Approve on the job page")
		}
	case session.ActionMerge:
		if v := factory.MergeOffered(it); !v.OK {
			return "", errors.New("Nothing changed: merge is not offered: " + v.Why)
		}
	default:
		return "", errors.New("Nothing changed: unknown action")
	}
	if press == nil {
		return "", errNoPress
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
		var wire *appwire.Error
		if errors.As(err, &wire) && wire.Code == appwire.CodeUncertain {
			return "", err
		}
		return "", fmt.Errorf("Nothing changed: %w", err)
	}
	if action == session.ActionApprove {
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
