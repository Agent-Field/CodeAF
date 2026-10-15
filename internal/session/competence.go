package session

// WHAT A MODEL HAS PROVEN, WRITTEN WHERE IT IS PROVEN.
//
// internal/competence keeps the ledger of checked results; this file is the
// session's one door into it, and the door out a reader uses to ask whether a
// result's source still exists.
//
// THE DOOR IS A RUN'S DECLARED CHECKS, AS THEY RAN ([Agent.recordRunProven]).
// This is the road every approved task takes by default (task.go's
// commitProposalToRun): the run's check seats re-run the declared checks
// through their bash, the plan store records each declared command's physical
// exit off the check's own trajectory ([plandb.VerdictBasis] of kind "run"),
// and when the run settles those exits become one result row on the one model
// that answered the run's work.
//
// THE LEGACY TASK GRAPH WRITES NOTHING. It runs only with the belt switched off
// (CODEAF_TASK_BELT=node), and the one model it could name for a node is the
// one the node was admitted on, not the one that answered — so its work is no
// evidence rather than evidence on a model nobody proved.
//
// NO UNDO IS WRITTEN. The one undo door there is, quick.undo, takes back a
// quick task, and a quick task runs no declared check, so it has no checked
// result to mark; writing it would be a row no reader could ever use. The
// ledger and its reader keep the undo shape for the day a door can reach
// checked work.
//
// WHAT THE CHECKER CONCLUDED IS NEVER READ HERE. A verdict is a model's
// judgement of another model's work; a result row is exit statuses. Work the
// checker accepted with a red declared check is a failed result, and work it
// refused over green checks is a passed one — the map counts what ran.

import (
	"database/sql"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/competence"
	"github.com/Agent-Field/codeaf/internal/plandb"
)

// recordRunProven writes a settled run's declared-check exits as one result,
// under the task row the conversation knows the run by.
//
// THE EXITS ARE THE PLAN STORE'S, read off the check's own trajectory by the
// gate that settled it, never a checker's words: only a check task whose
// basis is a "run" carries them. They are folded in the order the checks
// finished, the last run of a command standing, because every check of one run
// reads the run's one copy and a later run of a command is a later reading of
// the same tree.
//
// THE MODEL IS THE ONE THAT ANSWERED THE WORK — every model the run's work
// seats' calls were answered by, after any fallback, read off its spend
// ledger's ANSWERED rows (internal/run's recordSpend, from
// [Agent.AnsweredUsage]) — and the run is written only when every work row is
// answered and they name exactly one model. Two is a run nobody can
// attribute; none, or an answer that named no model, is a run nobody can name.
// Neither is guessed, and the model a seat was BUILT on is never read here. A
// program's run is work an outside program did, and is not this ledger's.
func (a *Agent) recordRunProven(run *beltRun) {
	if run == nil || run.store == nil || run.delegate != nil || strings.TrimSpace(run.ground) == "" {
		return
	}
	agent := a.journalID()
	if agent == "" {
		return
	}
	// A RUN OTHER HAND-OFFS JOINED IS SEVERAL GOALS IN ONE COPY, and nothing
	// the store keeps says which check proved which goal. Crediting every check
	// to the first goal's row would be a claim about work it did not do, so
	// such a run is no evidence.
	a.beltMu.Lock()
	joined := len(run.joined) > 0
	ended := run.ended
	a.beltMu.Unlock()
	if joined {
		return
	}
	tasks := runTasks(run.store.Tasks(), run.root)
	var checks []*plandb.Task
	ids := make([]string, 0, len(tasks))
	for _, task := range tasks {
		ids = append(ids, task.ID)
		if task.Role == plandb.RoleCheck && task.VerdictBasis.Kind == "run" && len(task.VerdictBasis.Runs) > 0 {
			checks = append(checks, task)
		}
	}
	if len(checks) == 0 {
		return
	}
	model, ok := planOneWorkModel(run.store.Path(), ids)
	if !ok {
		return
	}
	sort.SliceStable(checks, func(i, j int) bool { return checks[i].CompletedAt.Before(checks[j].CompletedAt) })
	var runs []competence.CheckRun
	for _, check := range checks {
		for _, ran := range check.VerdictBasis.Runs {
			if outcome, read := exitReading(ran.ExitCode); read {
				runs = append(runs, competence.CheckRun{Command: ran.Command, Outcome: outcome})
			}
		}
	}
	if ended.IsZero() {
		ended = time.Now()
	}
	task := strconv.FormatUint(run.row, 10)
	_ = competence.Append(competence.LedgerPath(), competence.Record{
		Kind: competence.KindResult, ID: competence.TaskID(agent, task), At: ended,
		Model: model, Place: run.ground, PlaceKind: placeKindOf(run.ground),
		Work: competence.WorkOf(run.title), Title: run.title,
		Source: competence.Source{Kind: competence.SourceTask, Agent: agent, Task: task, Transcript: a.config.SessionFile},
		Checks: runs,
	})
}

// exitReading is what one recorded exit says: zero passed, any other exit the
// check gave failed, and the shell's own "could not run it" codes (126, 127)
// and a negative code are not a reading of the work.
//
// A check a signal killed arrives as the shell's 128+signal (internal/exec/
// bare), and one the run cut off or whose ending nobody saw arrives as 1 (the
// run's trajectory): both are failures, and neither is ever a zero.
func exitReading(code int) (competence.Outcome, bool) {
	switch {
	case code == 0:
		return competence.Passed, true
	case code < 0, code == 126, code == 127:
		return "", false
	default:
		return competence.Failed, true
	}
}

// runTasks is the run's root and everything under it. A conversation's plan
// store may hold more than one run, and only this one's tasks are its.
func runTasks(all []*plandb.Task, root string) []*plandb.Task {
	children := make(map[string][]*plandb.Task, len(all))
	var out []*plandb.Task
	for _, task := range all {
		if task.ID == root {
			out = append(out, task)
			continue
		}
		children[task.ParentID] = append(children[task.ParentID], task)
	}
	queue := []string{root}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		for _, child := range children[id] {
			out = append(out, child)
			queue = append(queue, child.ID)
		}
	}
	return out
}

// planOneWorkModel is the one model that answered the named tasks' WORK, read
// off the spend ledger on its own read-only connection: rows whose role was
// "work" when the money was spent, so a planner's and a checker's calls are
// never the work's.
//
// EVERY WORK ROW MUST BE AN ANSWERED ONE ([plandb.Store.AddAnsweredSpend]). A
// row on the model a seat was built on — every row written before answered
// rows existed, a resumed run's earlier seats, money no breakdown covered —
// says what was configured, not what answered, and one such row makes the
// whole run unattributable even when its name matches the answered rows. It is
// false too when the work was answered by no model or by more than one.
func planOneWorkModel(path string, ids []string) (string, bool) {
	if len(ids) == 0 {
		return "", false
	}
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return "", false
	}
	defer db.Close()
	marks := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, 0, len(ids)+1)
	args = append(args, plandb.RoleWork)
	for _, id := range ids {
		args = append(args, id)
	}
	rows, err := db.Query(`SELECT DISTINCT TRIM(model), answered FROM spend WHERE role = ? AND task_id IN (`+marks+`) LIMIT 3`, args...)
	if err != nil {
		return "", false
	}
	defer rows.Close()
	var models []string
	for rows.Next() {
		var model string
		var answered int
		if rows.Scan(&model, &answered) != nil || answered != 1 || model == "" {
			return "", false
		}
		models = append(models, model)
	}
	if rows.Err() != nil || len(models) != 1 {
		return "", false
	}
	return models[0], true
}

func placeKindOf(dir string) string {
	if dir == "" {
		return competence.PlaceFolder
	}
	if _, repository := repositoryRoot(dir); repository {
		return competence.PlaceRepo
	}
	return competence.PlaceFolder
}

// CheckedSourceGone says what of a result's source has been deleted: the whole
// conversation, the task inside it, or nothing. A source with no transcript on
// record is unknown and answers nothing, rather than a deletion nobody saw.
func CheckedSourceGone(source competence.Source) string {
	transcript := strings.TrimSpace(source.Transcript)
	if transcript == "" {
		return ""
	}
	dir := filepath.Dir(transcript)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return competence.SourceConversation
	}
	if _, err := os.Stat(filepath.Join(dir, conversationDeletedFile)); err == nil {
		return competence.SourceConversation
	}
	if source.Kind == competence.SourceTask && source.Task != "" && taskDeletions(transcript)[source.Task] {
		return competence.SourceTask
	}
	return ""
}

// CheckedSourceTitle is the owning conversation's title as its meta.json says
// it now, or "" when it has none or cannot be read.
func CheckedSourceTitle(source competence.Source) string {
	transcript := strings.TrimSpace(source.Transcript)
	if transcript == "" {
		return ""
	}
	meta, err := LoadMeta(filepath.Dir(transcript))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(meta.Title)
}

// add folds one banked call into the tally under the model that answered it.
// A nil tally is made on first use; a call that names no model is kept under
// "", which a reader takes as unknown and never as the seat's own model.
func (m *modelShares) add(model string, call Usage) {
	if *m == nil {
		*m = modelShares{}
	}
	model = strings.TrimSpace(model)
	share := (*m)[model]
	share.Input += call.Input
	share.Output += call.Output
	share.CacheRead += call.CacheRead
	share.CacheWrite += call.CacheWrite
	share.CostUSD += call.CostUSD
	share.Calls += call.Calls
	(*m)[model] = share
}

// AnsweredUsage is this session's money split by the model that answered each
// call — after any fallback, by the response's own name — and nil when nothing
// was banked. Its shares sum to [Agent.Usage]'s tokens and dollars. It routes
// nothing; it is a reading of what already happened.
func (a *Agent) AnsweredUsage() map[string]Usage {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.answered) == 0 {
		return nil
	}
	out := make(map[string]Usage, len(a.answered))
	for model, share := range a.answered {
		out[model] = share
	}
	return out
}
