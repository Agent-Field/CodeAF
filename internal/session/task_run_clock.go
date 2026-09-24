package session

// A RUN'S WALL TIME IS ONE PAIR OF INSTANTS, and this file is where the pair is
// decided, so that no surface decides it again.
//
// A hand-off's run is measured from the HAND-OFF — the instant its row is born
// and first reads running ([beltRun.born], the first notice's StartedAt) — to
// the instant the program it handed its task to was gone
// ([delegate.ProgramRecord.EndedAt], stamped by the run's worker); for a run no
// program worked, or whose program never recorded an exit, to the instant the
// run's engine answered ([beltRun.ended]). Its elapsed time is the one minus the
// other ([runSpan]).
//
// EVERY SURFACE READS THAT PAIR, and it used to read four. The task page counted
// from the store's seeding, which is before the copy is cut (sixteen seconds on
// a real run), to whichever moment each kind of ending happened to write the
// store; the row and the card counted to the row's settling, which is after the
// landing and the summary refresh (up to a quarter of a minute more); the tasks
// tool and the landing note said no time at all. The same run read `22m 51s` on
// its page and `22m44s` on its card. Now the settle notice (and so the rail, the
// card and the checkpoint), the page's row ([planRunClocks]), the project index,
// the tasks tool and the landing note all carry the one pair. The program's own
// spawn stays on its record beside it, as a fact of the record, and nothing
// counts from it.

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/plandb"
)

// runClockEnd is a run's ending instant: the instant its program's process was
// gone when the program record carries one, and ending otherwise. A recorded
// exit that comes before the run's own start is not this run's — two clocks
// that disagree about the order of events are not two readings of one pair — and
// falls back to ending the same way a missing one does.
func runClockEnd(started time.Time, record delegate.ProgramRecord, ending time.Time) time.Time {
	if exited := record.EndedAt; !exited.IsZero() && (started.IsZero() || !exited.Before(started)) {
		return exited
	}
	return ending
}

// runSpan is the elapsed time between a pair, and zero for a pair that is not
// one — an end missing or before its start — which every surface draws as no
// time at all (the emptiness law).
func runSpan(started, ended time.Time) time.Duration {
	if started.IsZero() || ended.IsZero() || ended.Before(started) {
		return 0
	}
	return ended.Sub(started)
}

// beltRunProgram is the program record a run's worker wrote in the run's own
// task folder, and the zero record for a run no program worked, a run whose
// program has not been started, and a run with no store to hold a record.
func beltRunProgram(run *beltRun) delegate.ProgramRecord {
	if run == nil || run.delegate == nil || run.store == nil {
		return delegate.ProgramRecord{}
	}
	record, _ := delegate.ReadProgram(plandb.TaskDir(filepath.Dir(run.store.Path()), run.root))
	return record
}

// beltRunEndedAt is the instant a run's row settles at: the end of the run's
// one pair ([runClockEnd]), off the program record and the instant the engine
// answered. A run whose engine has not been heard from — a row settled by a
// road that never drove one — ends now, which is the reading it always had.
//
// IT IS NEVER THE INSTANT THE ROW SETTLES. The row used to be stamped when it
// was published, which is after the squash, the commit, the homecoming and a
// summary refresh that may wait six seconds for a model: none of that is the
// run's work, and all of it was counted as though it were.
func (a *Agent) beltRunEndedAt(run *beltRun) time.Time {
	a.beltMu.Lock()
	ending := run.ended
	a.beltMu.Unlock()
	if ending.IsZero() {
		ending = a.taskClockNow()
	}
	return runClockEnd(run.born, beltRunProgram(run), ending)
}

// beltRunSpan is how long a run took, off its one pair: the hand-off to the end
// [Agent.beltRunEndedAt] answers.
func (a *Agent) beltRunSpan(run *beltRun) time.Duration {
	return runSpan(run.born, a.beltRunEndedAt(run))
}

// runSpanWord spells a finished span EXACTLY AS THE TASK PAGE DOES (internal/
// tui3's countUpWord): seconds under a minute, then minutes and seconds, then
// hours and minutes, with a second rung of zero dropped — `22m 51s`, `1h 7m`,
// `5m` — and nothing at all under a second. It is restated here because the
// session cannot import a surface, and a run's time spelled one way on its page
// and another in the tasks tool or the landing note is two vocabularies for one
// fact. [taskSpanWord] keeps its own older spelling for the other rows it has
// always drawn.
func runSpanWord(d time.Duration) string {
	if d < time.Second {
		return ""
	}
	rungs := func(big int, bigUnit string, small int, smallUnit string) string {
		out := strconv.Itoa(big) + bigUnit
		if small == 0 {
			return out
		}
		return out + " " + strconv.Itoa(small) + smallUnit
	}
	switch {
	case d < time.Minute:
		return strconv.Itoa(int(d/time.Second)) + "s"
	case d < time.Hour:
		return rungs(int(d/time.Minute), "m", int(d%time.Minute/time.Second), "s")
	default:
		return rungs(int(d/time.Hour), "h", int(d%time.Hour/time.Minute), "m")
	}
}

// ── the page's row reads the same pair ──────────────────────────────────────

// planRunClocks is what a listing needs to put a program's run on the one pair:
// the run rows this conversation keeps, by the store id each run's root task
// carries, and the root the live run in this process holds. It is read once
// for a whole listing, the way [Agent.planCarriedPrograms] is read beside it.
type planRunClocks struct {
	rows map[string]TaskNotice
	live string
}

// planRunClocks reads the conversation's run rows and its live run. It builds
// no graph: a conversation that never handed work off has no rows to read.
func (a *Agent) planRunClocks() planRunClocks {
	var clocks planRunClocks
	a.beltMu.Lock()
	if a.beltRun != nil {
		clocks.live = a.beltRun.root
	}
	a.beltMu.Unlock()
	g := a.tasker()
	if g == nil {
		return clocks
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, notice := range g.runRowsLocked() {
		if notice.Run != "" || notice.Kind == TaskKindJob {
			continue
		}
		if clocks.rows == nil {
			clocks.rows = make(map[string]TaskNotice)
		}
		clocks.rows[strconv.FormatUint(notice.ID, 10)] = notice
	}
	return clocks
}

// apply puts a PROGRAM's row on the run's one pair. Every other row keeps the
// store's own pair, which is what an ordinary task's page has always counted.
//
// THE ROW'S START IS THE HAND-OFF: the run row's StartedAt, and for a run whose
// row this conversation never kept, the program's own spawn off its record. Its
// end is the run row's EndedAt once it has settled, and before that the
// program's recorded exit — so a page read in the seconds between the program
// exiting and the row settling already stops its clock where the row will. A
// record from before the program's clock was written leaves the store's pair
// standing, because a guess at a better pair is worse than the pair it has.
//
// AND A PROGRAM'S RUN NOTHING HERE IS DRIVING IS NOT RUNNING. A store's root
// left open — codeaf closed or crashed while the program ran, and nothing wrote
// its ending — read `running` on its page for ever, offered a stop for a
// process that was long gone, and counted its clock up without bound (a page
// read `1h 10m` over a run of twenty-nine minutes, and `11h` the next morning).
// Such a row now reads `failed`, the store's own word that every surface draws
// as incomplete, ended at the run's last sign of life ([planLastActivity]),
// with no live step and no stage. It is asked of the store's ROOT alone, which
// is the one task a program's run hands its program; root is that task's id.
func (c planRunClocks) apply(row *PlanTaskRow, dir string, task *plandb.Task, root string) {
	if row == nil || task == nil || row.Program == "" {
		return
	}
	record, _ := delegate.ReadProgram(plandb.TaskDir(dir, task.ID))
	kept := c.rows[task.ID]
	started := kept.StartedAt
	if started.IsZero() {
		started = record.StartedAt
	}
	if !started.IsZero() {
		row.Started = started
		row.Ended = kept.EndedAt
		if row.Ended.IsZero() {
			row.Ended = runClockEnd(started, record, time.Time{})
		}
	}
	if task.ID != root || terminalStoreStatus(task.Status) || c.live == task.ID {
		return
	}
	row.Status = string(plandb.StatusFailed)
	row.Live, row.Stage = plandb.LiveStep{}, ""
	if row.Ended.IsZero() {
		if last := planLastActivity(dir, task); !row.Started.IsZero() && !last.Before(row.Started) {
			row.Ended = last
		}
	}
}

// planLastActivity is the last sign of life a task's run left: the latest of
// the store's own last write to the task and the last write to its
// conversation log and its trajectory. It is read only for a run nothing is
// driving, which is the one moment the question is asked, and it reads the
// files' clocks rather than the files.
func planLastActivity(dir string, task *plandb.Task) time.Time {
	last := task.UpdatedAt
	folder := plandb.TaskDir(dir, task.ID)
	for _, name := range []string{delegate.ConversationFile, planTrajectoryFile} {
		if info, err := os.Stat(filepath.Join(folder, name)); err == nil && info.ModTime().After(last) {
			last = info.ModTime()
		}
	}
	return last
}

// planRowSpanWord is a program's run's time as the tasks tool says it:
// `ran 22m 51s` for a run that has ended, `running for 3m 2s` for one that is
// running, and nothing for a run with no start. It reads the row's pair, which
// for a program's row is the run's one pair ([planRunClocks.apply]).
//
// EVERY OTHER ROW SAYS NO TIME, as it never has: its pair is the store's own,
// which counts a part from when it was added rather than from when anybody
// started it, and a figure the tool cannot stand behind is left out rather than
// said.
func planRowSpanWord(row PlanTaskRow, now time.Time) string {
	if row.Program == "" || row.Started.IsZero() {
		return ""
	}
	if !row.Ended.IsZero() {
		if span := runSpanWord(runSpan(row.Started, row.Ended)); span != "" {
			return "ran " + span
		}
		return ""
	}
	switch strings.TrimSpace(row.Status) {
	case string(plandb.StatusClaimed), string(plandb.StatusRunning):
		if span := runSpanWord(runSpan(row.Started, now)); span != "" {
			return "running for " + span
		}
	}
	return ""
}
