package session

// A SUBHARNESS RUNNING IS A TASK, AND A TASK IS A PLACE.
//
// This is the other half of the asymmetry harness_task.go fixed from one side.
// DESIGNING a harness became a node with a row, a room, an id and a stop; RUNNING
// one stayed what it always was — a turn, held open for as long as the program
// took, with the conversation frozen behind it (harness.go's runHarnessRoute).
// The thing a person most wants to walk away from was the one thing they could
// not: no number to refer to it by, no room to watch it in, and no way to end it
// short of interrupting the whole turn.
//
// So a run is admitted to the WORK GRAPH (task_run.go) as a node of its own, and
// it buys exactly what the design bought:
//
//	a row on the roster        with the program's own last line as its phase
//	a room                     enter it, watch the host calls land, esc out
//	a journal                  every ai(), tool(), ask() and log(), permanently
//	a stop                     the same ✕ and the same `Cancel("task:9")`
//	a settle card              what the run produced, or why it did not finish
//
// ── THE TASK SURFACE IS A PRESENTATION OF A RUN, NOT ITS DEFINITION ──
//
// Everything below is about drawing a run for somebody. The run itself is
// [exec.Runner.Run] and knows nothing about any of it — which is what lets the
// headless command (cmd/aforge) run the same program through the same runner
// with no task system in the process at all. If this file ever became necessary
// to running a subharness, the contract would have quietly acquired a dependency
// on a terminal.
//
// ── IT IS ADMITTED, NOT PROPOSED ──
//
// [Agent.proposeTask] asks before the work starts, on a countdown. This does
// not, for [Agent.reserveHarnessDesign]'s reason said about a different door:
// THE QUESTION ALREADY HAPPENED one step earlier. Every path into this file goes
// through the intake card and somebody confirming it — chat's proposal
// (tools_subharness.go), `/subharness <name>`, or a person filling the card in.
// A countdown here would be two questions about one decision.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// subharnessRunSpec is what one run node is admitted with: which program, the
// input the card settled, and — where chat raised it — the one line about why.
// It rides on [taskSpec] and is what tells [Agent.runTaskNode] which body this
// node has.
type subharnessRunSpec struct {
	name  string
	input json.RawMessage
	// why is chat's reason for having raised this run, in its own words. It is
	// empty on the `/subharness` path, where the person chose the program
	// themselves and needs no reason given back to them — the same emptiness
	// [SubharnessCard.Why] keeps, carried through so the room and the card agree.
	why string
	// model is what the run's own model calls ride, and empty is the
	// conversation's. A run that picked its own model would be spending the
	// person's money on a choice they never made (tools_harness.go says the same
	// about an adaptive run).
	model string
}

// subharnessInterruptedReport is what a run that was still going when the
// process ended says for itself. It says the two things somebody reading the row
// afterwards needs: it did not finish, and the account of how far it got is
// still there (task_store.go's [interrupt] states why it is never re-run).
const subharnessInterruptedReport = "the run did not finish before aforge closed; its journal is kept"

// subharnessStoppedWord is the report a run a person ended settles with.
const subharnessStoppedWord = "stopped; its journal is kept"

// subharnessNodeTitle is what a run is called on the roster and in the stop
// card.
//
// It leads with the word for the KIND of thing this is, for
// [harnessNodeTitle]'s reason: a chip is eighteen cells wide, and somebody
// scanning a strip of running work needs to know what sort of node this is
// before they need to know which one.
func subharnessNodeTitle(name string) string {
	return clip("run · "+strings.TrimSpace(name), hintLimit)
}

// THE ID IS TAKEN BEFORE THE NODE IS, exactly as a design's is
// ([Agent.reserveHarnessDesign] carries the whole argument): admit turns the
// frontier, so a fast run can settle before admit has returned, and a caller
// that had not been handed the number yet would be drawing a landing card for a
// node it cannot name.
func (a *Agent) reserveSubharnessRun() uint64 { return a.graph().reserve() }

// admitSubharnessRun puts one run into the work graph under an id
// [Agent.reserveSubharnessRun] already handed out.
//
// The spec's brief and acceptance are the node's frozen goal contract like any
// other node's, and they are filled honestly even though no auditor will read
// them: they are what the room's header and the project index show, and a node
// whose brief said nothing would be a row nobody can place.
func (a *Agent) admitSubharnessRun(id uint64, spec *subharnessRunSpec, manifest exec.Manifest) {
	a.graph().admit(id, taskSpec{
		title: subharnessNodeTitle(manifest.Name),
		// IT IS ALREADY NAMED, so the naming pass leaves it alone (taskname.go):
		// the row says the PROGRAM's own name, which is the only name this work
		// has, and paying a model call to write a prettier one would be the
		// harness disagreeing with the registry about what it is running.
		named:      true,
		summary:    subharnessNodeSummary(manifest, spec.why),
		brief:      strings.TrimSpace(manifest.Purpose),
		acceptance: "the answer this program promises, in the shape it declares",
		model:      spec.model,
		run:        spec,
	})
}

// subharnessNodeSummary is the two lines under the row: what the program is for,
// and — when chat raised it — why it was raised here. A run the person picked
// themselves draws only the first, because they already know the answer to the
// second.
func subharnessNodeSummary(manifest exec.Manifest, why string) string {
	summary := strings.TrimSpace(manifest.Purpose)
	if why = strings.TrimSpace(why); why == "" {
		return summary
	}
	if summary == "" {
		return why
	}
	return summary + "\n" + why
}

// runSubharnessNode is the run node's whole life, and it is the shape
// [Agent.designHarnessNode] proved one file over: every ending is a state and a
// report rather than an error, because everything that can go wrong here is
// something the person who asked for the run has to be told in words.
func (a *Agent) runSubharnessNode(ctx context.Context, node *TaskNode, listed *job) TaskState {
	spec := node.spec.run
	log := taskLog(listed)
	fmt.Fprintf(log, "task %d · %s\nrunning %s\n", node.id, node.title(), spec.name)

	registry := a.config.Subharnesses
	if registry == nil {
		// The registry went away between the admission and the run. It cannot
		// happen through any door this build has, and saying so plainly is
		// cheaper than a nil dereference nobody can read.
		return a.landSubharnessNode(node, errSubharnessUnwired.Error(), TaskFailed)
	}
	runner, err := registry.Subharness(spec.name)
	if err != nil {
		return a.landSubharnessNode(node, "there is nothing here called "+strconv.Quote(spec.name), TaskFailed)
	}
	manifest := runner.Manifest()

	// THE ROOM IS OPENED BEFORE THE FIRST HOST CALL, so that somebody who walks
	// in while the program is working finds the run in progress rather than an
	// empty room. It is the same order [Agent.designHarnessNode] takes for the
	// same reason.
	room := node.openRoom()
	journal := &subharnessJournal{agent: a, node: node, room: room, log: log}
	env := a.subharnessEnv(node, room, journal, runner, manifest, spec.model)
	defer env.close()

	if why := strings.TrimSpace(spec.why); why != "" && room != nil {
		// Chat's reason, said once in the room it is about. It is not journaled:
		// it is context for a person walking in, and the journal is the record of
		// what the PROGRAM did.
		room.publish(Event{Kind: EventTextDelta, Text: why})
	}

	result, runErr := runner.Run(ctx, spec.input, env)

	// ── THE DEOPTIMIZATION ──────────────────────────────────────────────────
	//
	// Two things ask for the long way, and neither of them is a failure a person
	// should be shown as one (internal/exec's deopt.go states the law).
	//
	//   - AN ERROR IS A RUN THAT COULD NOT BE MADE TO HAPPEN — a broken bundle, a
	//     step that could not be reached. The work still has to get done.
	//   - FellBack IS THE PROGRAM ITSELF SAYING SO: a guard did not pass, and the
	//     material is not what this program is for.
	//
	// A STOPPED RUN IS NEITHER, and the check for it comes first. A person who
	// pressed ✕ is not asking for the same work to be started again on a
	// generalist — they are asking for the spending to stop, which is the one
	// sentence cancel.go is written around.
	if ctx.Err() == nil && (runErr != nil || (exec.FellBack(result) && !result.Finished())) {
		because := result.FellBack
		if runErr != nil {
			because = runErr.Error()
		}
		result, runErr = a.deoptSubharness(ctx, node, room, journal, registry, manifest, spec, because)
	}

	// WHAT THE RUN SPENT IS FOLDED ONCE, HERE, and never per call. The doors of
	// the Env return their own spend and journal it, so folding at each of them
	// as well would bill the person twice for one call — the same double-count
	// [Agent.foldHarnessUsage] exists to avoid one lane over.
	spend := a.runSpend(result, journal)
	a.foldSubharnessSpend(spend, spec.model)
	// AND THE NOTE THE NEXT LIST DRAWS. It is written for every ending except
	// the one that has not happened yet: a run the PROCESS died under has not
	// finished and has not not-finished, and a note saying either would be this
	// session answering for a run the next one is going to settle
	// (task_store.go's interrupt).
	if ctx.Err() == nil || node.stoppedByPerson() {
		a.recordSubharnessRun(spec.name, result, runErr, spend)
	}

	switch {
	case node.stoppedByPerson():
		return a.landSubharnessNode(node, subharnessStoppedWord, TaskFailed)
	case ctx.Err() != nil:
		// The PROCESS ended under a running program. The node is left exactly as
		// it is so the checkpoint carries it, and the next session settles it
		// with one sentence (task_store.go's interrupt). Returning no state is
		// what keeps the record running ([Agent.runTaskNode]).
		node.doingNow("")
		return ""
	case runErr != nil:
		fmt.Fprintf(log, "run failed: %v\n", runErr)
		return a.landSubharnessNode(node, "the run could not be made to happen: "+runErr.Error(), TaskFailed)
	case result.Incomplete != "":
		// INCOMPLETE IS THE PROGRAM'S OWN SENTENCE and it travels verbatim: out
		// of budget, out of time, a question nobody was there to answer. Nothing
		// here may rewrite it into a fault, and nothing may call it a failure —
		// "incomplete" is one of the five sanctioned words for the state of work.
		fmt.Fprintf(log, "incomplete: %s\n", firstLine(result.Incomplete))
		return a.landSubharnessNode(node, subharnessReport(manifest, result), TaskFailed)
	}
	fmt.Fprintf(log, "done: %s\n", manifest.Name)
	return a.landSubharnessNode(node, subharnessReport(manifest, result), TaskDone)
}

// deoptSubharness does the job the long way and says so in the room and in the
// journal, in the register the vocabulary law demands.
//
// THE PERSON IS TOLD THE STEP NEEDED A CLOSER LOOK. They are not told a program
// failed, a guard rejected their input, or a fallback was triggered: from their
// side the work is being done, and the only thing that changed is how. The
// sentence is [exec.DeoptLine] and it is the only one.
//
// EXCEPT WHERE THE LONG WAY WOULD REACH PAST THE CEILING SOMEBODY APPROVED, and
// then it is the other one. [exec.DeoptLineFor] picks between them from the
// program's own manifest, and it is asked BEFORE the room and the journal are
// told anything: announcing "handled it the long way" over a run that is about
// to stop would be this file saying the opposite of what happened.
func (a *Agent) deoptSubharness(ctx context.Context, node *TaskNode, room *taskRoom,
	journal *subharnessJournal, registry *exec.Registry, manifest exec.Manifest,
	spec *subharnessRunSpec, because string,
) (exec.RunResult, error) {
	line := exec.DeoptLineFor(manifest, because)
	// The note goes into the journal first, because the journal is the record
	// that outlives the room — and a run read back tomorrow has to say why its
	// second half looks nothing like its first.
	_ = exec.Record(journal, exec.JournalEntry{Call: exec.CallLog, Ref: spec.name, Note: line})
	node.doingNow(clip(exec.DeoptWordFor(manifest), hintLimit))
	if room != nil {
		room.publish(Event{Kind: EventNotice, Text: line})
	}
	// THE INPUT IS THE ORIGINAL INPUT. Falling back means doing the job as it
	// stood before any program touched it, which is the whole of what the long
	// way promises.
	return exec.Deopt(ctx, registry, manifest, spec.input, journal.env, because)
}

// runSpend is what the run cost, read from the one place that can say.
//
// A RUNNER THAT REPORTED ITS OWN LEDGER IS BELIEVED, because a runner that
// journals its host calls sets [exec.RunResult.Spend] to the sum of them and
// adding the journal's total to it would be counting one run twice. A runner
// that reported nothing — the fronted leaf workers, which take no Env at all and
// spend through their own clients — is measured by its journal instead, which
// for those is empty and honestly zero.
func (a *Agent) runSpend(result exec.RunResult, journal *subharnessJournal) exec.Spend {
	if result.Spend.Reported() {
		return result.Spend
	}
	return journal.total()
}

// recordSubharnessRun tells the store how this run went, through the seam the
// store lane fills ([Config.SubharnessRecordRun]). A build that keeps no history
// records nothing and draws nothing, which is the same picture as a machine that
// has run nothing — deliberately.
//
// A RUN THAT COULD NOT BE MADE TO HAPPEN IS STILL A RUN THAT WAS TRIED, and it
// is recorded as unfinished with the error as its reason. A row that stayed
// silent about it would send somebody to try the same broken program again.
func (a *Agent) recordSubharnessRun(name string, result exec.RunResult, runErr error, spend exec.Spend) {
	record := a.config.SubharnessRecordRun
	if record == nil {
		return
	}
	note := SubharnessRunNote{At: time.Now(), Finished: result.Finished(), Why: result.Incomplete, CostUSD: spend.CostUSD}
	if runErr != nil {
		note.Finished, note.Why = false, runErr.Error()
	}
	record(name, note)
}

// landSubharnessNode settles the node with the report a person reads.
//
// THE STATE IS THE CALLER'S TO NAME, on [Agent.landHarnessNode]'s terms and for
// its reason, and the one thing decided here is the STOP: [TaskGraph.stop] sets
// `stopped` before it cuts the context, so a node this settles after a person's
// ✕ settles failed whatever the caller asked for.
func (a *Agent) landSubharnessNode(node *TaskNode, report string, state TaskState) TaskState {
	node.doingNow("")
	node.finish(report, nil, "", "")
	if node.stoppedByPerson() {
		return TaskFailed
	}
	return state
}

// subharnessReport is the settle card's account of one run: what the program
// said, what it left behind, and — when it did not finish — why not.
//
// IT IS NOT A SUMMARY OF THE OUTPUT. The output is typed and is rendered from
// its own schema wherever it is read; this is the run's account of itself
// ([exec.RunResult.Report] says the same). A run that said nothing at all is
// described by its name and its ending rather than by an empty line, because a
// card with a blank body is a card that looks broken.
func subharnessReport(manifest exec.Manifest, result exec.RunResult) string {
	var out strings.Builder
	if report := strings.TrimSpace(result.Report); report != "" {
		out.WriteString(report)
	} else if result.Incomplete == "" {
		fmt.Fprintf(&out, "%s finished", manifest.Name)
	}
	if fell := strings.TrimSpace(result.FellBack); fell != "" {
		writeLine(&out, fell)
	}
	if incomplete := strings.TrimSpace(result.Incomplete); incomplete != "" {
		writeLine(&out, "incomplete — "+incomplete)
	}
	// The files, where there are any. Nothing renders as nothing (the emptiness
	// law), so a run that made none says nothing about files at all.
	for _, artifact := range result.Artifacts {
		line := strings.TrimSpace(artifact.Path)
		if line == "" {
			line = strings.TrimSpace(artifact.Name)
		}
		if line == "" {
			continue
		}
		if note := strings.TrimSpace(artifact.Note); note != "" {
			line += " — " + note
		}
		writeLine(&out, line)
	}
	return out.String()
}

// writeLine appends one line to a report that may still be empty, so that a
// report which starts with its second clause does not start with a newline.
func writeLine(out *strings.Builder, line string) {
	if out.Len() > 0 {
		out.WriteString("\n")
	}
	out.WriteString(line)
}

// ── THE JOURNAL ─────────────────────────────────────────────────────────────

// subharnessJournal is where a run's host calls go: the permanent record on the
// node's own job log, the live row in the room, and the running total that is
// the run's ledger.
//
// EVERY HOST CALL IS WRITTEN BEFORE ITS ANSWER RETURNS, which is
// internal/exec's journal.go's law and not a property of this implementation:
// a run killed halfway has to leave behind exactly the calls it made, and an
// implementation that answered first and wrote afterwards would lose the call
// that killed it.
//
// IT LOCKS, and the type it sums does not ([exec.Spend] says so about itself).
// The two are not in conflict: a run's host calls come from the one goroutine
// the run has, and what this lock is actually for is the ROOM and the ROSTER
// reading the total while the run is still going.
type subharnessJournal struct {
	agent *Agent
	node  *TaskNode
	room  *taskRoom
	log   io.Writer
	// env is the capability surface this journal belongs to, kept so the
	// deoptimization path can hand the same one to the generalist. It is set
	// once, by [Agent.subharnessEnv], and is nil in a test that journals with
	// nothing behind it.
	env exec.Env

	mu    sync.Mutex
	seq   int
	spend exec.Spend
}

// Write records one host call and shows it.
func (j *subharnessJournal) Write(entry exec.JournalEntry) error {
	if j == nil {
		return nil
	}
	j.mu.Lock()
	j.seq++
	if entry.Seq == 0 {
		entry.Seq = j.seq
	}
	if entry.At.IsZero() {
		entry.At = time.Now()
	}
	j.spend.Add(entry.Spend)
	j.mu.Unlock()

	if j.log != nil {
		fmt.Fprintln(j.log, journalLine(entry))
	}
	if j.room != nil {
		carried := entry
		j.room.publish(Event{Kind: EventSubharnessStep, ID: j.nodeID(), Entry: &carried})
	}
	return nil
}

// total is the run's ledger so far: the sum of every entry written, which is
// what internal/exec's journal.go means when it says the journal and the ledger
// can never disagree.
func (j *subharnessJournal) total() exec.Spend {
	if j == nil {
		return exec.Spend{}
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.spend
}

func (j *subharnessJournal) nodeID() uint64 {
	if j == nil || j.node == nil {
		return 0
	}
	return j.node.id
}

// journalLine is one entry as a line in the node's job log: which door, what it
// was about, and what went wrong if anything did. It is the compact form a
// person reads when they are not reading the whole entry.
func journalLine(entry exec.JournalEntry) string {
	line := entry.Call
	if ref := strings.TrimSpace(entry.Ref); ref != "" {
		line += " " + ref
	}
	if note := strings.TrimSpace(entry.Note); note != "" {
		line += " · " + firstLine(note)
	}
	if err := strings.TrimSpace(entry.Err); err != "" {
		line += " · " + firstLine(err)
	}
	return line
}

// ── THE SPEND FOLD ──────────────────────────────────────────────────────────

// foldSubharnessSpend charges what a run spent to the session that asked for it,
// and it is [Agent.foldHarnessUsage]'s body with the ledger's own figures rather
// than one package's spelling of them (internal/exec's Spend states the whole
// argument). Every sentence that function's comment makes is true of this one,
// which is why that one now goes through here:
//
//   - IT GOES THROUGH THE AUXILIARY DOOR AND THE TURN SEAL STAYS ZERO-TOKEN.
//     The figures did not come from this conversation's transcript, so folding
//     them into the turn would attribute a context this session never held to
//     the context it is about to send.
//   - THE MODEL IS THE RUN'S OWN, in the order of who knows best: what the
//     provider reported, then what the run was asked to ride, then this
//     conversation's.
//   - A RUN THE PROVIDER SAID NOTHING ABOUT FOLDS NOTHING. Zero tokens and no
//     cost is "the provider did not say", not "the run was free".
func (a *Agent) foldSubharnessSpend(spent exec.Spend, model string) {
	if !spent.Reported() {
		return
	}
	if spent.Model != "" {
		model = spent.Model
	}
	if model == "" {
		model = a.Model()
	}
	used := &ai.Usage{
		PromptTokens:             spent.Input,
		CompletionTokens:         spent.Output,
		CacheReadInputTokens:     spent.CacheRead,
		CacheCreationInputTokens: spent.CacheWrite,
	}
	// The cost is attached only when the provider gave one. A pointer to zero
	// and no pointer at all are the same arithmetic, but they are not the same
	// claim, and this struct is read elsewhere as the provider's own words.
	if spent.CostUSD > 0 {
		used.Cost = &spent.CostUSD
	}
	a.addAuxiliaryUsage(&ai.Response{Usage: used}, model, spent.Calls)
}
