package session

// A PROGRAM'S TASK IS READ AS A CONVERSATION. A task a run handed to a program
// codeaf carries (senior-dev first; internal/delegate) has no steps worth a
// page of their own: the program's work happens inside the calls it makes to a
// model, and every one of those goes through the model API codeaf serves the
// run, which writes each call as one turn of the program's conversation with
// codeaf into the task's own record folder (delegate.ConversationFile). This
// file is the reading side of that record for the task page: whose
// conversation it is, the stages it said it would move through, the stage it
// is in now, and the turns themselves, cut to what a page draws.
//
// NOTHING HERE WRITES. The run's worker writes the program record when the
// program says hello (delegate.ProgramFile) and the model API writes the
// turns; this file reads both, inside the page read the surface already makes
// off its loop ([Agent.PlanTaskPage]) and the row read the side list already
// makes on its beat ([Agent.PlanTasks]), and never on a frame.

import (
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/plandb"
)

// planProgramTurns is how many of a program's calls one page carries: the
// newest, because the page opens stuck to its bottom edge and an hour's run
// makes hundreds. The rest are counted ([PlanProgram.Earlier]) rather than
// carried, which is also what keeps a page read over a connection the size of
// a page and not the size of the log.
const planProgramTurns = 200

// planProgramHead is the most of any one text a page carries, in bytes. The
// page draws the first line of what a program sent and of what the model
// answered and never more, so the rest of each text would cross the wire on
// every beat to be thrown away at the far end; the whole of it stays in the
// program's own record on disk.
const planProgramHead = 240

// PlanProgram is the program a task was handed to, as the task's page reads it:
// its name and stages off the program record, and its conversation with codeaf
// off the log the model API writes. A page carries one only for a task whose
// record folder holds either — which is to say only for a program's task — and
// every other page carries nil.
type PlanProgram struct {
	// Name is the program's own name, the word its command is spelled with. It
	// is empty only for a log with no record beside it, which is a program that
	// never said who it was; the page draws no name it was not given.
	Name string
	// Stages is every stage the program said, in its hello, it would move
	// through, in order. Empty when the program named none.
	Stages []string
	// Turns is the conversation: the newest [planProgramTurns] calls the
	// program made, in the order they started, each as its latest record says.
	// EVERY TEXT IN IT IS CUT TO ITS HEAD — the first line that says anything, at
	// most [planProgramHead] bytes — and the run's own copy is taken out of it
	// ([planRunCopies.strip]), because that is all the page draws. The record on
	// disk is untouched: this is a reading of it.
	Turns []delegate.Turn
	// Earlier is how many calls came before the first of Turns, which the page
	// says rather than draws. Zero when the page carries the whole conversation.
	Earlier int
	// Calls is how many calls reached a model: every call the log holds that
	// codeaf did not refuse, the one in flight included. It is counted over the
	// whole log and not over Turns, so it stays the run's own figure however
	// long the run has gone.
	Calls int
	// CeilingUSD is the dollar ceiling the run handed the program, off the
	// program record the worker writes at the hello (delegate.ProgramRecord),
	// and zero when the run set none or the program has not said hello yet —
	// which the page draws as no ceiling at all rather than as $0.00.
	CeilingUSD float64
}

// planProgramRecord is the program a task was handed to: the record the run's
// worker wrote in the task's own folder when the program said hello, and
// otherwise the name the conversation's live run carries for its root
// (`carried`), with no stages. False is every task no program was handed —
// which is every task but a program's run's root.
//
// THE CARRIED NAME IS FOR THE SECONDS BEFORE THE HELLO. A run is published the
// moment it starts and the program writes its hello a moment later, so a row
// read in between would otherwise wear no name at all; once the record is on
// disk it is the record that answers, during the run and after it.
func planProgramRecord(dir, id, carried string) (delegate.ProgramRecord, bool) {
	if record, ok := delegate.ReadProgram(plandb.TaskDir(dir, id)); ok {
		return record, true
	}
	if carried = strings.TrimSpace(carried); carried != "" {
		return delegate.ProgramRecord{Name: carried}, true
	}
	return delegate.ProgramRecord{}, false
}

// planProgramStage is the stage a program says it is in, read off its task's
// live step. The worker publishes a program's phase as the live step in the
// program's own words, `<name>: <stage> · <status>` (internal/run's
// delegateSink.Stage), so the stage is what is left without the name in front
// and without the status behind — `implement` — and nothing at all when no
// step is live, because a run whose program has ended is in no stage.
//
// IT IS READ FOR A PROGRAM'S TASK AND FOR NO OTHER. A live step of any other
// task is a command a worker is running, and a command is not a stage.
func planProgramStage(name string, live plandb.LiveStep) string {
	name, label := strings.TrimSpace(name), strings.TrimSpace(live.Command)
	if name == "" || label == "" {
		return ""
	}
	label = strings.TrimPrefix(label, name+": ")
	if stage, _, found := strings.Cut(label, " · "); found {
		label = stage
	}
	return strings.TrimSpace(label)
}

// planProgramRow names a row's program and the stage it is in now. It is the
// one place a row learns both, so the record's name and the name the live run
// carries are read into the row the same way.
func planProgramRow(row *PlanTaskRow, name string) {
	row.Program = strings.TrimSpace(name)
	row.Stage = planProgramStage(row.Program, row.Live)
}

// planCarriedRow gives a row the program the conversation's live run carries
// for it, when the program's own record has not reached the disk yet
// ([planProgramRecord] says why there is such a moment). A row that already
// names its program keeps the record's name.
func planCarriedRow(row *PlanTaskRow, carried string) {
	if row.Program != "" || strings.TrimSpace(carried) == "" {
		return
	}
	planProgramRow(row, carried)
}

// planCarriedPrograms is the program this conversation's live run was handed
// to, keyed by the run's root task: at most one entry, and none while no
// program's run is live. It is read once for a whole listing, under the belt's
// own lock, the way [Agent.planDisplayRunCopy] reads the live copy beside it.
func (a *Agent) planCarriedPrograms() map[string]string {
	a.beltMu.Lock()
	defer a.beltMu.Unlock()
	if a.beltRun == nil || a.beltRun.delegate == nil {
		return nil
	}
	return map[string]string{a.beltRun.root: a.beltRun.delegate.Name}
}

// planRootIsProgram answers whether a run's root task was handed to a program:
// its record folder holds the program's record, or the conversation's live run
// carries a program for it before that record has reached the disk.
func (a *Agent) planRootIsProgram(store *plandb.Store, rootID string) bool {
	id := planTaskID(rootID)
	_, ok := planProgramRecord(filepath.Dir(store.Path()), id, a.planCarriedPrograms()[id])
	return ok
}

// planProgramPage reads one task's program and conversation for its page, or
// nil for a task that is not a program's: no record in its folder, no name the
// live run carries for it, and no conversation log.
//
// A LOG THAT CANNOT BE READ IS A CONVERSATION WITH NO TURNS YET, never a page
// that fails. [delegate.ReadTurns] answers a missing log as nothing said and
// skips a line cut mid-write; anything worse leaves the page with its brief and
// its pinned line, which is still the truth about a run that has said nothing
// this page can read.
func planProgramPage(dir, id, carried string, copies planRunCopies) *PlanProgram {
	record, known := planProgramRecord(dir, id, carried)
	all, _ := delegate.ReadTurns(plandb.TaskDir(dir, id), 0)
	if !known && len(all) == 0 {
		return nil
	}
	program := &PlanProgram{Name: record.Name, CeilingUSD: record.CeilingUSD}
	if len(record.Stages) > 0 {
		program.Stages = append([]string(nil), record.Stages...)
	}
	for _, turn := range all {
		if strings.TrimSpace(turn.Refused) == "" {
			program.Calls++
		}
	}
	kept := all
	if len(kept) > planProgramTurns {
		kept = kept[len(kept)-planProgramTurns:]
	}
	program.Earlier = len(all) - len(kept)
	if len(kept) > 0 {
		program.Turns = make([]delegate.Turn, len(kept))
		for i, turn := range kept {
			program.Turns[i] = planTurnForPage(turn, copies)
		}
	}
	return program
}

// planTurnForPage is one turn as a page carries it: every text cut to its head
// and the run's own copy taken out of it, and every other field — the call's
// clock, its models, its size and price, whether it was refused or failed —
// exactly as the log says. An absent list stays absent, so a turn that sent
// nothing new reads back the same across the wire as it was written.
func planTurnForPage(turn delegate.Turn, copies planRunCopies) delegate.Turn {
	head := func(text string) string { return planTextHead(copies.strip(text)) }
	if len(turn.Sent) > 0 {
		sent := make([]delegate.Said, len(turn.Sent))
		for i, said := range turn.Sent {
			said.Text = head(said.Text)
			sent[i] = said
		}
		turn.Sent = sent
	}
	turn.Reply = head(turn.Reply)
	if len(turn.Calls) > 0 {
		calls := make([]delegate.ToolUse, len(turn.Calls))
		for i, call := range turn.Calls {
			call.Args = planTextHead(copies.strip(call.Args))
			calls[i] = call
		}
		turn.Calls = calls
	}
	turn.Refused = head(turn.Refused)
	turn.Failed = head(turn.Failed)
	return turn
}

// planTextHead is the first line of a text that says anything, bounded to
// [planProgramHead] bytes on a rune boundary with the cut marked.
func planTextHead(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return clip(line, planProgramHead)
		}
	}
	return ""
}

// strip takes the run's own copy out of a line a program wrote, FOR THE PAGE
// ONLY. A program's tools name every file by its absolute path, and the path of
// the run's copy is the same forty-odd cells in front of every one of them:
// fitted to a row from the right, it was all the row said, and the file the
// call was about was the part cut off. What a path means inside the copy is the
// part after it, so that is what the page carries.
//
// TWO FOLDERS ARE A COPY, and they are the two [planRunCopies] already names:
// the live run's copy (or the row's own folder, when no run is live), and any
// folder directly under the conversation's own folder of copies — an ended
// run's copy has been given back, and its program's words still name it.
func (c planRunCopies) strip(text string) string {
	if text == "" {
		return text
	}
	if live := strings.TrimSpace(c.live); live != "" && filepath.IsAbs(live) {
		text = strings.ReplaceAll(text, strings.TrimRight(filepath.Clean(live), "/")+"/", "")
	}
	root := strings.TrimSpace(c.root)
	if root == "" || !filepath.IsAbs(root) {
		return text
	}
	prefix := strings.TrimRight(filepath.Clean(root), "/") + "/"
	var out strings.Builder
	for {
		at := strings.Index(text, prefix)
		if at < 0 {
			break
		}
		rest := text[at+len(prefix):]
		// A COPY IS ONE FOLDER DOWN, named without a space or a quote in it: the
		// root followed by anything else is some other path that happens to
		// start there, and it is left as it was written.
		slash := strings.IndexByte(rest, '/')
		if slash <= 0 || strings.ContainsAny(rest[:slash], " \t\"'") {
			out.WriteString(text[:at+len(prefix)])
			text = rest
			continue
		}
		out.WriteString(text[:at])
		text = rest[slash+1:]
	}
	out.WriteString(text)
	return out.String()
}
