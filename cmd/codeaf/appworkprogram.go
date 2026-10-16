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
	out := &appwire.TaskProgram{
		Name: redact.Secrets(view.Name), LastStage: redact.Secrets(view.LastStage), CeilingUSD: view.CeilingUSD,
		Calls: view.Calls, Models: view.Models, Effort: redact.Secrets(view.Effort), Listens: view.Listens,
		Listening: view.Listening, Started: view.Started, InboxClosed: redact.Secrets(view.InboxClosed),
		EarlierActions: view.EarlierActions, Actions: []appwire.ProgramAction{},
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
		out.Stages = append(out.Stages, appwire.ProgramStage{Name: stage.Name, Word: stage.Word})
	}
	actions := view.Actions
	if len(actions) > appwire.ProgramActionsMax {
		out.EarlierActions += len(actions) - appwire.ProgramActionsMax
		actions = actions[len(actions)-appwire.ProgramActionsMax:]
	}
	for _, shown := range actions {
		action := appwire.ProgramAction{At: shown.At, Step: redact.Secrets(shown.Step), Text: redact.Secrets(shown.Text),
			Outcome: redact.Secrets(shown.Outcome), Steer: shown.Steer, Memory: shown.Memory,
			Model: redact.Secrets(shown.Model), Reason: redact.Secrets(shown.Reason)}
		if shown.Lines {
			added, removed := shown.Added, shown.Removed
			action.Added, action.Removed = &added, &removed
		}
		out.Actions = append(out.Actions, action)
	}
	if view.Ending != nil {
		out.Ending = &appwire.ProgramEnding{At: view.Ending.At, Status: redact.Secrets(view.EndStatus),
			Text: redact.Secrets(view.Ending.Text), Outcome: redact.Secrets(view.Ending.Outcome),
			Facts: appEndingFacts(view.Ending.Ending)}
	}
	return out
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
		AnalysisIncomplete: redact.Secrets(facts.AnalysisIncomplete),
	}
	for _, name := range facts.Reports {
		out.Reports = append(out.Reports, redact.Secrets(name))
	}
	if coverage := facts.HuntCoverage; coverage != nil {
		hunt := &appwire.HuntCoverage{Attempted: coverage.Attempted, Unknown: coverage.Unknown}
		for _, failure := range coverage.Failures {
			hunt.Failures = append(hunt.Failures, appwire.HuntFailure{Strategy: redact.Secrets(failure.Strategy), Reason: redact.Secrets(failure.Reason)})
		}
		out.HuntCoverage = hunt
	}
	return out
}
