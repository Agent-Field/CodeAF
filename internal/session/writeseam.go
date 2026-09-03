package session

// A TURN READS, A TASK WRITES — WITH A SMALL ALLOWANCE IN FRONT OF IT.
//
// ── THE RUN THIS WAS WRITTEN FROM ──
//
// A person typed "implement this issue" and it ran as a PLAIN CHAT TURN for
// seven minutes and forty-six seconds: forty-eight tool calls, `sed -i` edits in
// their live checkout, nobody watching, nothing to open. What finally moved it
// was the checkpoint ceiling at forty finished rounds — a governor sized for a
// READING grind, which counts rounds and knows nothing about what those rounds
// did to the disk. Nothing before it was a seam at all: the pre-turn route judge
// was deliberately demoted to triage, and the post-turn judge excuses any turn
// that used tools.
//
// ── THE RULING (owner, 2026-09-01, issue #272) ──
//
// A turn may make A SMALL BOUNDED EDIT INLINE — of the order of
// [writeAllowanceFiles] files or [writeAllowanceCalls] write calls — and then
// the next workspace write promotes it to a task through the road the ceiling
// already takes. READS STAY FREE, in any number: a turn that spends forty rounds
// looking at a repository has cost the person a wait and nothing else, and the
// checkpoint's own argument that slow-to-interrupt is the honest direction holds
// for exactly that turn. It fails for a writing one, because what is at stake
// there is not the wait — it is unreviewed edits in a directory somebody is
// standing in.
//
// ── WHY THE COUNTER IS ITS OWN THING AND NOT ANOTHER MARK ──
//
// The checkpoint meter prices READING: [checkpointPrice] and its doublings are
// rounds of a turn, and the ceiling is where the harness stops paying to look.
// Writes are a different unit and a different question, so this is a counter of
// its own with its own allowance, and it opens THE SAME DOOR — a counter that
// grew a second way to start a task would be two roads into the graph, which is
// the thing checkpoint.go exists to have exactly one of.
//
// ── AND IT IS A DOOR, NOT A CAGE ──
//
// The seam fires ONCE in a turn. Past it the ceiling is the governor again, as
// it always was. That is not a softness: the handover it opens is the ordinary
// one, which can be DECLINED when the running model and the mark's own reader
// both say nothing remains ([Agent.handOverRunningTurn]) — and a turn that wrote
// its two files and finished is exactly that turn. A seam that re-fired every
// round would ask those two minds the same question over and over and charge the
// person for each of them.

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

const (
	// writeAllowanceFiles is how many DISTINCT files under the workspace a turn
	// may change before the next write moves the work. Two, because the shape the
	// ruling protects is "fix the typo in this file and the test beside it" — one
	// obvious edit and the thing that goes with it — and a third file is where
	// somebody would have wanted to watch.
	writeAllowanceFiles = 2
	// writeAllowanceCalls is the same allowance measured the other way, for the
	// turn that edits one file over and over. Five, because a single small edit
	// is one or two calls and a re-read-and-retry of it is three or four; a sixth
	// is a turn that has started working rather than finishing.
	//
	// BOTH ARE CHECKED AND EITHER ONE SPENDS IT. The measured run crossed both
	// inside its first two minutes, and a rule that only counted files would have
	// let forty-eight calls against one file through.
	writeAllowanceCalls = 5
)

// writeSeamNote is the ONE LINE a person reads when a writing turn is moved.
//
// It is in the register every line in this house is held to (checkpoint.go's
// [inTheHouseRegister] states it): an observation, a middle dot, a promise. It
// says what was noticed rather than naming a counter, because a person who has
// just watched two files change does not need to be told a threshold's name.
const writeSeamNote = "this is changing more than a quick edit · moving it to a task that is watched and can split"

// writeMeter is ONE TURN'S account of what it has written under the workspace.
//
// It is minted at episode-init and dropped with the turn, exactly as the change
// ledger is (recovery.go), because the question it answers — has THIS turn
// written more than a small edit — is a fact about one turn and nothing else.
// The batch's calls run in parallel, so it holds its own lock.
type writeMeter struct {
	mu sync.Mutex
	// calls is how many write-shaped calls have landed, and files is which paths
	// they landed on. A path is counted once however many times it is written.
	calls int
	files map[string]bool
	// spent says the seam has already opened its door in this turn, so that a
	// handover the two minds declined is not asked for again every round after.
	spent bool
}

func newWriteMeter() *writeMeter { return &writeMeter{files: map[string]bool{}} }

// wrote records one landed write-shaped call and the paths it changed.
func (m *writeMeter) wrote(paths []string) {
	if m == nil || len(paths) == 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	for _, path := range paths {
		m.files[path] = true
	}
}

// pastAllowance reports whether this turn has spent the allowance, AND CLAIMS
// THE DOOR when it has. The claim is here rather than at the caller because two
// step boundaries can never both be the one that moved the work.
func (m *writeMeter) pastAllowance() bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.spent {
		return false
	}
	if m.calls < writeAllowanceCalls && len(m.files) < writeAllowanceFiles {
		return false
	}
	m.spent = true
	return true
}

// ── the seam as a hook citizen ──────────────────────────────────────────────

// writeSeam is the counter's two hooks: it opens at episode-init and records the
// calls that actually wrote something at post-feedback.
//
// It NEVER VETOES, for [changeLedger]'s reason: a citizen that both watches the
// environment and can stop a call is a citizen whose bookkeeping bug is an
// outage. All it can do is make a turn end one boundary earlier than it would
// have, on a road that can still decline.
type writeSeam struct{ agent *Agent }

func (*writeSeam) Name() string { return "writes" }

func (w *writeSeam) EpisodeInit(*episode) {
	meter := newWriteMeter()
	w.agent.mu.Lock()
	w.agent.writes = meter
	w.agent.mu.Unlock()
}

// PostFeedback counts the calls that CHANGED SOMETHING. A write that failed
// changed nothing, and a turn moved to a task over a refused edit would be the
// harness governing an intention.
func (w *writeSeam) PostFeedback(_ context.Context, _ *episode, _ *eventHub, calls []ai.ToolCall, results []toolResult, _ bool) {
	meter := w.agent.writeMeterNow()
	if meter == nil {
		return
	}
	workspace := strings.TrimSpace(w.agent.config.Workspace)
	for index, call := range calls {
		if index >= len(results) || results[index].isError {
			continue
		}
		meter.wrote(workspaceWrites(workspace, call))
	}
}

// writeMeterNow is this turn's counter, or nil in a session that has never run
// an episode — which is every unit test of the pieces below.
func (a *Agent) writeMeterNow() *writeMeter {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.writes
}

// ── what counts as a write ──────────────────────────────────────────────────

// workspaceWrites answers the paths one call would change UNDER THE WORKSPACE
// ROOT, and nothing for a call that changes nothing there.
//
// THE WORKSPACE ROOT IS THE WHOLE OF THE SCOPE. A turn writing into /tmp is
// keeping notes; a turn writing into the directory the person is standing in is
// doing the work, and the second is the only one this counts. It is the same
// containment test the task ground law uses (taskoutside.go's [withinDir]) so
// that two parts of this package cannot disagree about what "in here" means.
func workspaceWrites(workspace string, call ai.ToolCall) []string {
	if strings.TrimSpace(workspace) == "" {
		return nil
	}
	if path, writes := mutatedPath(call); writes {
		// THE HANDS WITH A NAMED DESTINATION ARE READ FROM THE ONE PREDICATE THAT
		// KNOWS THEM (recovery.go's [mutatedPath]): edit, write, and the three
		// edit_video actions that put a file on disk. A respelt hand must not be
		// able to mean one thing there and another thing here.
		if !filepath.IsAbs(path) {
			path = filepath.Join(workspace, path)
		}
		if withinDir(workspace, filepath.Clean(path)) {
			return []string{filepath.Clean(path)}
		}
		return nil
	}
	if call.Function.Name != "bash" {
		return nil
	}
	command, ok := bashCommandOf(call)
	if !ok {
		return nil
	}
	return bashWritesInside(workspace, command)
}

// bashCommandOf reads the command out of a `bash` call.
func bashCommandOf(call ai.ToolCall) (string, bool) {
	var args struct {
		Command string `json:"command"`
	}
	if json.Unmarshal([]byte(call.Function.Arguments), &args) != nil {
		return "", false
	}
	command := strings.TrimSpace(args.Command)
	return command, command != ""
}

// bashWritesInside answers the paths one shell command NAMES as things it would
// change inside a directory.
//
// ── WHY BASH IS COUNTED HERE AND NOT IN THE CHANGE LEDGER ──
//
// recovery.go leaves bash out of its ledger deliberately and says why: a shell
// command's effects are whatever it did, and a ledger that guessed at them would
// OFFER TO REVERT a set of files that is not the set that changed. That argument
// is about a destructive move made on the guess, and it is right.
//
// This is a different job with a different cost of being wrong. A counter that
// reads one path too many moves a turn to a task one boundary early; a counter
// that cannot see bash at all misses the exact shape the ruling was written from
// — forty-eight `sed -i` calls in somebody's checkout, not one of which is an
// `edit` call. So the paths a command NAMES are counted, the ones it does not
// name are not claimed, and the walk is taskoutside.go's own: the same segment
// reader, the same `cd` carried through it, the same tables of which hands write
// which operands.
func bashWritesInside(root, command string) []string {
	var wrote []string
	cwd := root
	seen := map[string]bool{}
	keep := func(path string) {
		clean := filepath.Clean(path)
		if !withinDir(root, clean) || seen[clean] {
			return
		}
		seen[clean] = true
		wrote = append(wrote, clean)
	}
	for _, segment := range taskSegments(command) {
		words, assignments := taskSegmentHead(stripRedirections(segment))
		if len(words) == 0 {
			continue
		}
		if target := redirectTarget(cwd, segment); target != "" {
			keep(target)
		}
		program := filepath.Base(words[0])
		rest := words[1:]
		switch {
		case program == "cd":
			cwd = resolveCd(cwd, rest)
		case program == "git":
			// A git command that only reads is looking, whatever it is aimed at.
			// One that writes is aimed at a repository rather than at a file, so
			// the repository is what is counted.
			if dir, verb, after := gitAim(cwd, assignments, rest); verb != "" && !gitOnlyReads(verb, after) {
				keep(dir)
			}
		default:
			for _, path := range writesAimedAt(cwd, program, rest) {
				keep(path)
			}
		}
	}
	return wrote
}

// writesAimedAt answers the paths one non-git hand would change, and it is
// [writeAimedOutside]'s reading turned inside out: that one stops at the first
// path outside a ground because a refusal needs one name, and this one wants
// them all because it is counting.
func writesAimedAt(cwd, program string, rest []string) []string {
	operands := commandOperands(rest)
	switch {
	case program == "patch":
		// `patch` takes its target from the diff it is fed and writes relative to
		// the directory it stands in, so the directory is the only honest answer.
		return []string{cwd}
	case program == "sed" || program == "perl":
		if !inPlaceEdit(rest) {
			return nil
		}
	case writesEveryOperand[program] || writesItsLastOperand[program]:
	default:
		return nil
	}
	if writesItsLastOperand[program] && len(operands) > 1 {
		operands = operands[len(operands)-1:]
	}
	paths := make([]string, 0, len(operands))
	for _, operand := range operands {
		paths = append(paths, resolvePath(cwd, operand))
	}
	return paths
}

// ── the promotion ───────────────────────────────────────────────────────────

// checkpointWriting moves a turn that has written past the allowance onto the
// one road, and reports whether the turn is over.
//
// IT IS [Agent.checkpointCeiling]'S ROAD WITH TWO THINGS CHANGED, and everything
// else about it — the dowry, the parts at the head of the brief, the task, the
// gap, the sealed turn — is [Agent.handOverRunningTurn]'s and is not restated
// here. The two are the LINE, which says what was noticed, and the VERDICT,
// which is NOT armed to split: the ceiling arms one because a turn that outran
// forty rounds is measured evidence of breadth, and three files changed is
// evidence of nothing of the sort.
//
// THE MARK'S OWN READER IS ASKED, and that is what makes this safe to fire on a
// counter. The handover can only be declined on TWO MINDS agreeing that nothing
// remains, and the second of them is this reading; without it a turn that made
// its two edits and finished would be converted into a task nobody needed.
func (a *Agent) checkpointWriting(ctx context.Context, hub *eventHub, turn *Usage, started time.Time, model string, rounds int, verdict routeVerdict, spoke bool) bool {
	read := a.readMark(ctx)
	a.journalMarkRead(read, 0, rounds, checkpointDecisionWrote)
	return a.handOverRunningTurn(ctx, hub, turn, started, model, writeSeamNote, verdict, read, spoke).moved
}
