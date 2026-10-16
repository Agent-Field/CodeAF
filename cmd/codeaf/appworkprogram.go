package main

import (
	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/redact"
	"github.com/Agent-Field/codeaf/internal/session"
)

// appTaskProgram is a program's task on the wire: the engine's own read of it
// (session.AppProgramView, off the page AppTask already makes), re-bounded to
// what the page carries and with every text through the secret filter, as the
// task log's are. Nil in, nil out: a task no program was handed has no
// program, and nothing is made up for it.
func appTaskProgram(view *session.AppProgramView) *appwire.TaskProgram {
	if view == nil {
		return nil
	}
	// A FAR ENGINE THAT PREDATES THE READ STATE tied nothing to a process; what
	// it sent about the process's work is dropped rather than passed on as the
	// current run's.
	if view.Read != session.AppProgramReadComplete && view.Read != session.AppProgramReadPartial {
		cut := *view
		cut.Read, cut.ReadCut, cut.Calls, cut.Actions, cut.EarlierActions, cut.Before = session.AppProgramReadUnknown, "", nil, nil, 0, ""
		cut.LastStage, cut.Ending, cut.EndStatus = "", nil, ""
		view = &cut
	}
	out := &appwire.TaskProgram{
		Name: redact.Secrets(view.Name), LastStage: redact.Secrets(view.LastStage), CeilingUSD: view.CeilingUSD,
		Effort: redact.Secrets(view.Effort), Listens: view.Listens,
		Listening: view.Listening, Started: view.Started, InboxClosed: redact.Secrets(view.InboxClosed),
		Read: view.Read, ReadCut: view.ReadCut,
	}
	if view.Calls != nil {
		calls := *view.Calls
		out.Calls = &calls
	}
	for _, model := range view.Models {
		out.Models = append(out.Models, redact.Secrets(model))
	}
	if !view.StartedAt.IsZero() {
		at := view.StartedAt
		out.StartedAt = &at
	}
	if !view.EndedAt.IsZero() {
		at := view.EndedAt
		out.EndedAt = &at
	}
	for _, stage := range view.Stages {
		out.Stages = append(out.Stages, appwire.ProgramStage{Name: redact.Secrets(stage.Name), Word: redact.Secrets(stage.Word)})
	}
	out.Actions, out.EarlierActions, out.Before = appProgramActions(view.Read, view.Actions, view.EarlierActions, view.Before)
	if view.Ending != nil {
		out.Ending = &appwire.ProgramEnding{At: view.Ending.At, Status: redact.Secrets(view.EndStatus),
			Text: redact.Secrets(view.Ending.Text), Outcome: redact.Secrets(view.Ending.Outcome),
			Facts: appEndingFacts(view.Ending.Ending)}
	}
	return out
}

// appProgramActions is a page of a program's actions on the wire, with how
// many came before it and the cursor that reads them: every text through the
// secret filter, and the page held to the wire's own bound. A page cut here is
// counted into earlier, but only a COMPLETE read's count is a count, and only a
// complete read carries a cursor.
func appProgramActions(read string, actions []delegate.Shown, earlier int, before string) ([]appwire.ProgramAction, int, string) {
	if len(actions) > appwire.ProgramActionsMax {
		earlier += len(actions) - appwire.ProgramActionsMax
		actions = actions[len(actions)-appwire.ProgramActionsMax:]
	}
	var out []appwire.ProgramAction
	for _, shown := range actions {
		action := appwire.ProgramAction{At: shown.At, Step: redact.Secrets(shown.Step), Text: redact.Secrets(shown.Text),
			Outcome: redact.Secrets(shown.Outcome), Steer: shown.Steer, Memory: shown.Memory,
			Model: redact.Secrets(shown.Model), Reason: redact.Secrets(shown.Reason)}
		if shown.Lines {
			added, removed := shown.Added, shown.Removed
			action.Added, action.Removed = &added, &removed
		}
		out = append(out, action)
	}
	if read != appwire.ProgramReadComplete {
		return out, 0, ""
	}
	return out, earlier, before
}

// appProgramPager is a conversation whose programs' older actions can be paged
// (internal/session's AppProgramActions, and the remote client's).
type appProgramPager interface {
	AppProgramActions(id, before string) (session.AppProgramActionsPage, bool, error)
}

// ProgramActions answers one older page of a task's program's actions
// (tasks.program_actions).
func (b *appBackend) ProgramActions(id, before string) (appwire.ProgramActionsResult, error) {
	agent, local, ok := appSplitID(id)
	if !ok {
		return appwire.ProgramActionsResult{}, appwire.Errorf(appwire.CodeBadParams, "%s is not a task id", id)
	}
	_, live, err := b.heldLive(agent)
	if err != nil {
		return appwire.ProgramActionsResult{}, err
	}
	pager, ok := live.conn.Conv.(appProgramPager)
	if !ok {
		return appwire.ProgramActionsResult{}, appwire.Errorf(appwire.CodeUnknownMethod, "this engine does not serve tasks.program_actions")
	}
	page, found, err := pager.AppProgramActions(local, before)
	if err != nil {
		return appwire.ProgramActionsResult{}, appEngineError(err)
	}
	if !found {
		return appwire.ProgramActionsResult{}, appwire.Errorf(appwire.CodeNotFound, "there is no program's task %s", id)
	}
	if page.Gone {
		return appwire.ProgramActionsResult{}, appwire.Errorf(appwire.CodeConflict, "that page is of an earlier run of the program; read the task again")
	}
	out := appwire.ProgramActionsResult{Read: page.Read, ReadCut: page.ReadCut}
	out.Actions, out.EarlierActions, out.Before = appProgramActions(page.Read, page.Actions, page.EarlierActions, page.Before)
	return out, nil
}

// appEndingFacts is an ending's typed facts on the wire. Counts and flags are
// copied as the log kept them, so one nobody said stays absent.
func appEndingFacts(facts *delegate.EndFacts) *appwire.EndingFacts {
	if facts == nil || facts.Empty() {
		return nil
	}
	out := &appwire.EndingFacts{
		Claim: redact.Secrets(facts.Claim), Observed: redact.Secrets(facts.Observed), Submitted: facts.Submitted,
		SubmissionReason: redact.Secrets(facts.SubmissionReason), InnerStatus: redact.Secrets(facts.InnerStatus),
		VerificationFailing: facts.VerificationFailing, VerificationCommands: facts.VerificationCommands,
		Findings: facts.Findings, Blocking: facts.Blocking, Confirmed: facts.Confirmed, Likely: facts.Likely,
		Inconclusive: facts.Inconclusive, RuledOut: facts.RuledOut, Partial: facts.Partial,
		AnalysisIncomplete: redact.Secrets(facts.AnalysisIncomplete), NothingToAudit: facts.NothingToAudit,
	}
	for _, name := range facts.Reports {
		out.Reports = append(out.Reports, redact.Secrets(name))
	}
	// AN UNKNOWN ACCOUNT IS UNKNOWN ALONE, as the ending keeps it
	// (delegate.HuntCoverage): no attempted count and no failures beside it.
	if coverage := facts.HuntCoverage; coverage != nil && coverage.Unknown {
		out.HuntCoverage = &appwire.HuntCoverage{Unknown: true}
	} else if coverage != nil {
		hunt := &appwire.HuntCoverage{Attempted: coverage.Attempted}
		for _, failure := range coverage.Failures {
			hunt.Failures = append(hunt.Failures, appwire.HuntFailure{Strategy: redact.Secrets(failure.Strategy), Reason: redact.Secrets(failure.Reason)})
		}
		out.HuntCoverage = hunt
	}
	return out
}
