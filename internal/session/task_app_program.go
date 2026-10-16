package session

// A PROGRAM'S TASK, AS THE APP'S TASK READ CARRIES IT: the program record, and
// what the record's own process did, read within a fixed budget.
//
// THE APP NEVER READS A PROGRAM'S WHOLE LOGS. A task's action and conversation
// logs hold every launch its program has had, and the page read the terminal
// makes ([Agent.PlanTaskPage]) reads both whole; the app's read skips that
// ([Agent.planTaskPageFor]) and reads only the record's launch, through the one
// bounded reader of the log's owner ([delegate.ReadLaunchActions]) and the one
// reading of a line every page uses ([planProgramShownReader]). A launch it
// could not read within the budget is said to be partly read, never empty.
//
// WHAT THE RECORD DOES NOT HOLD IS NOT HERE. The program record is the latest
// process on the task and not a history, so there is no attempt number, no
// "auto", and no earlier run; a stage the program never named has no position,
// and no percent is made of any of it.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/plandb"
)

// The bounds of the app's read of a program. AppProgramActionsMax is how many
// actions one page of them carries (the wire's own bound is the same number; a
// test holds the two together); a hello naming more than appProgramStagesMax
// stages, or a stage longer than appProgramStageBytes, carries NO stages,
// because half a list would read as the program's.
const (
	AppProgramActionsMax = 60
	appProgramStagesMax  = 128
	appProgramStageBytes = 80
)

// The budget of one app read of a program: the most of its action log one
// launch may span, the most of its conversation log a call count reads, and the
// time the two together may take. They are variables for a test alone.
var (
	appProgramActionsBudget = delegate.ReadBudget{Bytes: 16 << 20, Lines: 200_000}
	appProgramCallsBudget   = delegate.ReadBudget{Bytes: 32 << 20, Lines: 200_000}
	appProgramReadTime      = 3 * time.Second
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
	// Read says how much of the current process's own lines this view was read
	// from ([AppProgramReadComplete] and its siblings), and ReadCut why it is
	// not complete (delegate's ReadCut words). Actions are the record's process's
	// and nothing else's; EarlierActions, Before, LastStage and Ending say
	// nothing at all unless Read is complete.
	Read, ReadCut string
	// LastStage is the stage the current process's newest stage record names.
	LastStage  string
	CeilingUSD float64
	// Calls is how many of the current process's calls reached a model, and
	// nil when that is not known: a count of every one of its calls, or nothing.
	Calls              *int `json:",omitempty"`
	Models             []string
	Effort             string
	Listens, Listening bool
	Started            bool
	InboxClosed        string
	// Actions are the newest of the process's actions, oldest first, without
	// their detail; EarlierActions how many of its own came before them, and
	// Before the cursor of the page before ([Agent.AppProgramActions]), empty
	// when there is none.
	Actions        []delegate.Shown
	EarlierActions int
	Before         string
	Ending         *delegate.Shown
	EndStatus      string
}

// How much of the current process's lines a view was read from.
const (
	// AppProgramReadComplete: every line the process wrote was read, so its
	// actions, their count, its last stage and its ending are exact.
	AppProgramReadComplete = "complete"
	// AppProgramReadPartial: the process's lines could not all be read — more
	// than the budget, a damaged line among them, a refusal of the disk, the
	// read's deadline. The actions carried are some of the process's; nothing
	// is said of how many there are, where it stands or how it ended.
	AppProgramReadPartial = "partial"
	// AppProgramReadUnknown: no line can be tied to the record's process — a
	// record written before launches were, or none yet (a relaunch before its
	// hello). Nothing is said about its work.
	AppProgramReadUnknown = "unknown"
)

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

// AppProgramActionsPage is one older page of a program's actions
// ([Agent.AppProgramActions]).
type AppProgramActionsPage struct {
	// Gone says the cursor's launch is no longer the task's: the program was
	// handed the task again since the page before was read, and the page asked
	// for is of an earlier run. Nothing else is set. IT IS A FIELD AND NOT AN
	// ERROR so it crosses --host as the fact it is, not as a sentence.
	Gone           bool `json:",omitempty"`
	Read, ReadCut  string
	Actions        []delegate.Shown
	EarlierActions int
	Before         string
}

// AppProgramActions is the page of a program's actions before the cursor
// before, which the app's task read ([AppProgramView.Before]) or the page
// before handed out. Found is false for a task that is not this
// conversation's, or no program's.
func (a *Agent) AppProgramActions(id, before string) (AppProgramActionsPage, bool, error) {
	launch, index, ok := appProgramCursor(before)
	if !ok {
		return AppProgramActionsPage{}, false, errors.New("that is not a page of a program's actions")
	}
	_, source, found := a.planTaskPageFor(id, false)
	if !found {
		return AppProgramActionsPage{}, false, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), appProgramReadTime)
	defer cancel()
	view := a.appProgramRead(ctx, source, index, launch)
	if view == nil {
		return AppProgramActionsPage{}, false, nil
	}
	if view.Read == AppProgramReadUnknown {
		return AppProgramActionsPage{Gone: true}, true, nil
	}
	return AppProgramActionsPage{Read: view.Read, ReadCut: view.ReadCut, Actions: view.Actions,
		EarlierActions: view.EarlierActions, Before: view.Before}, true, nil
}

// appProgramRead reads one task's program for the app within the read's
// budget, or nil for a task no program was handed. It reads the actions
// before index before of the launch, the newest when before is negative. A
// launch other than want, when want is named, reads as unknown: a page of
// actions is only ever the launch its cursor was cut from.
func (a *Agent) appProgramRead(ctx context.Context, source planProgramSource, before int, want string) *AppProgramView {
	record, known := planProgramRecord(source.dir, source.id, source.carried)
	taskDir := plandb.TaskDir(source.dir, source.id)
	if !known && !planProgramLogged(taskDir) {
		return nil
	}
	program := planProgramOf(a.config.Delegates, record.Name)
	view := &AppProgramView{
		Name: record.Name, StartedAt: record.StartedAt, EndedAt: record.EndedAt, Read: AppProgramReadUnknown,
		CeilingUSD: record.CeilingUSD, Models: append([]string(nil), record.Models...), Effort: record.Effort,
		Listens: program.Listens, Listening: record.Listening, Started: !record.StartedAt.IsZero(), InboxClosed: record.InboxClosed,
		Stages: appProgramStages(program, record.Stages),
	}
	if !delegate.ValidLaunch(record.Launch) || (want != "" && want != record.Launch) {
		return view
	}
	page := delegate.ReadLaunchActions(ctx, taskDir, record.Launch, planProgramShownReader(program, source.copies),
		before, AppProgramActionsMax, appProgramActionsBudget)
	view.Actions = page.Actions
	for i := range view.Actions {
		view.Actions[i].Detail = ""
	}
	// THE CALLS ARE COUNTED FOR THE NEWEST PAGE ALONE: an older page of actions
	// carries no count, and is not made to pay for one.
	if before < 0 {
		if calls, known := delegate.CountLaunchCalls(ctx, taskDir, record.Launch, appProgramCallsBudget); known {
			view.Calls = &calls
		}
	}
	if !page.Complete {
		view.Read, view.ReadCut = AppProgramReadPartial, page.Cut
		return view
	}
	view.Read, view.EarlierActions = AppProgramReadComplete, page.First
	if page.First > 0 {
		view.Before = record.Launch + "." + strconv.Itoa(page.First)
	}
	if len(page.LastStage) <= appProgramStageBytes {
		view.LastStage = page.LastStage
	}
	if page.Ending != nil {
		ending := *page.Ending
		ending.Detail = ""
		view.Ending, view.EndStatus = &ending, page.EndStatus
	}
	return view
}

// appProgramCursor reads a cursor appProgramRead cut: a launch and the index
// of the first action of the page after the one it asks for.
func appProgramCursor(cursor string) (string, int, bool) {
	launch, at, found := strings.Cut(cursor, ".")
	index, err := strconv.Atoi(at)
	if !found || err != nil || index <= 0 || !delegate.ValidLaunch(launch) {
		return "", 0, false
	}
	return launch, index, true
}

// planProgramLogged answers whether a task's record folder holds either of a
// program's logs, without reading them.
func planProgramLogged(taskDir string) bool {
	for _, name := range []string{delegate.ActionsFile, delegate.ConversationFile} {
		if _, err := os.Stat(filepath.Join(taskDir, name)); err == nil {
			return true
		}
	}
	return false
}

// appProgramStages is a hello's stages with the person's word for each, or
// none when the hello is longer, or a stage or its word longer, than a page
// carries.
func appProgramStages(program delegate.Delegate, names []string) []AppProgramStage {
	if len(names) > appProgramStagesMax {
		return nil
	}
	standing := ""
	var stages []AppProgramStage
	for _, stage := range names {
		standing = AppProgramStageWord(program, stage, standing)
		if len(stage) > appProgramStageBytes || len(standing) > appProgramStageBytes {
			return nil
		}
		stages = append(stages, AppProgramStage{Name: stage, Word: standing})
	}
	return stages
}
