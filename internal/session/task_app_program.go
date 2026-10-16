package session

// A PROGRAM'S TASK, AS THE APP'S TASK READ CARRIES IT. [Agent.AppTask] already
// reads the task's page ([Agent.PlanTaskPage]) and used to throw the page's
// [PlanProgram] away, so the app wire could say nothing of a specialist's
// stages, actions or ending. This file only re-shapes that one read for the
// app: no second read of the logs, no second reader, no policy of its own.
//
// WHAT THE RECORD DOES NOT HOLD IS NOT HERE. The program record is the latest
// process on the task and not a history, so there is no attempt number, no
// "auto", and no earlier run; a stage the program never named has no position,
// and no percent is made of any of it.

import (
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/delegate"
)

// The bounds of the app's read of a program. AppProgramActionsMax is how many
// of the newest actions cross to the app (the wire's own bound is the same
// number; a test holds the two together); a hello naming more than
// appProgramStagesMax stages, or a stage longer than appProgramStageBytes,
// carries NO stages, because half a list would read as the program's.
const (
	AppProgramActionsMax = 60
	appProgramStagesMax  = 128
	appProgramStageBytes = 80
)

// AppProgramView is a program's task as the app reads it. A nil view is a task
// no program was handed.
type AppProgramView struct {
	Name string
	// StartedAt and EndedAt are the latest process's clock; zero is unknown.
	StartedAt, EndedAt time.Time
	// Stages are the stages the program's hello named, in order, each with the
	// person's word for it ([AppProgramStageWord] says how a wordless stage reads).
	Stages []AppProgramStage
	// LastStage is the stage the newest stage record of the log names.
	LastStage          string
	CeilingUSD         float64
	Calls              int
	Models             []string
	Effort             string
	Listens, Listening bool
	Started            bool
	InboxClosed        string
	Actions            []delegate.Shown
	EarlierActions     int
	Ending             *delegate.Shown
	EndStatus          string
}

// AppProgramStage is one stage of a program's hello and the word a person reads
// for it; an empty Word is a stage with nothing standing before it to inherit.
type AppProgramStage struct {
	Name, Word string
}

// AppProgramStageWord is the word of a stage, by the rule the run's live step
// uses (internal/run delegateSink.live): the program's word for it; no word
// keeps the word already standing; and a program that gave no words at all is
// shown by its stage names.
func AppProgramStageWord(program delegate.Delegate, stage, standing string) string {
	if program.StageWords == nil {
		return stage
	}
	if word := strings.TrimSpace(program.StageWords[stage]); word != "" {
		return word
	}
	return standing
}

// appProgramView is the page's program for the app, nil when the page has none.
func (a *Agent) appProgramView(page PlanTaskPage) *AppProgramView {
	p := page.Program
	if p == nil {
		return nil
	}
	program := planProgramOf(a.config.Delegates, p.Name)
	view := &AppProgramView{
		Name: p.Name, StartedAt: p.StartedAt, EndedAt: p.EndedAt, LastStage: p.LastStage,
		CeilingUSD: p.CeilingUSD, Calls: p.Calls, Models: append([]string(nil), p.Models...), Effort: p.Effort,
		Listens: p.Listens, Listening: p.Listening, Started: p.Started, InboxClosed: p.InboxClosed,
		EarlierActions: p.EarlierActions, EndStatus: p.EndStatus,
	}
	// ONLY THE NEWEST ACTIONS CROSS, WITHOUT THEIR DETAIL: the app never draws
	// it, and over --host four hundred of them would be read to be thrown away.
	actions := p.Actions
	if len(actions) > AppProgramActionsMax {
		view.EarlierActions += len(actions) - AppProgramActionsMax
		actions = actions[len(actions)-AppProgramActionsMax:]
	}
	view.Actions = append([]delegate.Shown(nil), actions...)
	for i := range view.Actions {
		view.Actions[i].Detail = ""
	}
	if len(p.LastStage) > appProgramStageBytes {
		view.LastStage = ""
	}
	if p.Ending != nil {
		ending := *p.Ending
		view.Ending = &ending
	}
	if len(p.Stages) > appProgramStagesMax {
		return view
	}
	standing := ""
	var stages []AppProgramStage
	for _, stage := range p.Stages {
		standing = AppProgramStageWord(program, stage, standing)
		if len(stage) > appProgramStageBytes || len(standing) > appProgramStageBytes {
			return view
		}
		stages = append(stages, AppProgramStage{Name: stage, Word: standing})
	}
	view.Stages = stages
	return view
}
