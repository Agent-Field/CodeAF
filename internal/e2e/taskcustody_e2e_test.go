//go:build e2e

// CUSTODY, THROUGH THE PERSON'S OWN DOOR, ON A REAL MODEL (issue #567).
//
// The measured incident is one sentence: a turn was moved to a task while two
// pieces were still out under the conversation, and the drawing it moved on mixed
// a local job with the coordination of those two — wait for them, integrate their
// branches, fire the reviews, open the pull request. A task admitted by that road
// is a ROOT, and `tasks` inside a task answers only the children that task itself
// created, so the worker's first look at the rail said "No tasks have run in this
// project yet." and the rest of its deadline went on guessing at branch names.
//
// internal/session's taskcustody_unit_test.go reads the drawing on its own and
// taskcustody_test.go drives the road with a scripted model. NEITHER OF THEM ASKS
// A MODEL WHAT IT WOULD ACTUALLY DRAW. This one does: the mark reader and the
// handoff writer are real calls on [e2eModel], the person's sentence is the one
// from the field, and every assertion below is taken off THE DISK — the graph's
// own checkpoint file and the session journal — rather than off anything the
// model said in prose.
//
// ── THE TWO PIECES ARE OUT AND THEY COST NOTHING ──
//
// A piece is "still out" when it is a root of this conversation's graph and has
// not settled ([Agent.piecesStillOut]), and QUEUED is not settled. So the two are
// admitted through the person's own door ([session.Agent.StartTask]) with the
// admission governor's memory floor set past anything a machine has
// (custodyMemoryFloor, session.Config's TaskMinFreeMB): the frontier holds them
// for `machine busy`, they never take a worktree, they never make a model call,
// and they are genuinely being held by the conversation at the moment the brief
// is written. That is the e2e equivalent of the in-package fixture's runner that
// records nothing and settles nothing, and it is what keeps this file to a few
// cents rather than two real implementation tasks.
//
//	go test -tags e2e -count=1 -timeout 40m -v -run TestCustody ./internal/e2e/
package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/approval"
	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

const (
	// custodyMemoryFloor is the admission governor's memory floor, set past what
	// any machine has so that every node this conversation admits WAITS
	// (task_pressure.go's [admissionGovernor.holds]). It is how the two pieces
	// stay out for the length of a turn without a worker, a worktree or a call.
	custodyMemoryFloor = 1 << 40
	// custodyAttempts is the ask and two retries. The drawing is a real model's
	// answer and a cheap model does not always mix coordination into it; an
	// attempt that never crossed the write seam, or that drew nothing about the
	// pieces already out, has measured the sidecar's mood rather than the road,
	// so it is put again and the log says which attempt reached the seam.
	custodyAttempts = 3
	// custodyCap is what the whole file may spend before the run is a runaway
	// rather than an answer. Three attempts of one writing turn plus two shaper
	// calls measure in cents on this model.
	custodyCap = 1.00
)

// custodyAsk is the person's own sentence, in the shape the field produced it:
// a local job they want doing now, and — in their own words, misspelling and all
// left out only because the seam reads no keywords — the request that the rest be
// parallelized, with the coordination of the two pieces already out spelled after
// it. It has to do BOTH: three distinct files under the workspace is what crosses
// the write allowance (writeseam.go's writeAllowanceFiles) and opens the road at
// all.
const custodyAsk = "put a two-sentence header at the top of docs/one.md, docs/two.md and " +
	"docs/three.md saying what each file is for. then parallelize others as well please — " +
	"once tasks 1 and 2 are back, integrate their branches, run the reviews and open the " +
	"pull request."

// The two pieces the conversation hands out before the turn, by the numbers the
// person's sentence names them by. Ids are minted in order, so the first is 1 and
// the second is 2.
const (
	custodyFirstPiece  = "rewrite the folder picker rail so it reads the new settings row"
	custodySecondPiece = "rewrite the settings pane copy so it matches the folder picker"
)

// custodyRemainderHead opens the block the TRANSCRIPT keeps when part of a
// drawing did not travel. It is internal/session's own
// (checkpoint_custody.go's checkpointOwnRemainderHead), spelled again here
// because it is unexported there and this lane reads what a person would read.
const custodyRemainderHead = "WHAT STAYS HERE, FOR WHEN THE PIECES ALREADY OUT LAND:"

// custodyHeldWorkReason is the word the journal writes on a rung of the brief
// ladder that was blanked for assigning work already out (checkpoint.go's
// carryHeldWork), and custodyHeldWorkDecision is the ceiling that declined
// outright (checkpointCeilingHeldWork). Respelled for custodyRemainderHead's
// reason.
const (
	custodyHeldWorkReason   = "it assigned work this conversation is still holding"
	custodyHeldWorkDecision = "dropped:work-already-out"
)

// custodyWriteSeam is the door a turn that has written past its allowance takes
// (internal/session's checkpointSeamWrite), and it is the door the incident took.
// Every ending now writes one row naming its seam; a row with no seam on it was
// written before that existed. Respelled for custodyRemainderHead's reason.
const custodyWriteSeam = "write"

// custodyCeilingSeam and custodyMarkSeam are the other two doors, here so that a
// failure can say which one a run actually took rather than only that it was not
// the expected one.
const (
	custodyCeilingSeam = "ceiling"
	custodyMarkSeam    = "mark"
)

// TestNoWorkerIsBriefedToOwnThePiecesThisConversationIsHolding is the field
// replication of #567.
//
// WHAT IT ASSERTS IS ON DISK AND IN THE GRAPH, never in the model's prose: the
// brief a worker would open on (the graph's checkpoint file), the parentage of
// the two pieces (the same file), and where the withheld coordination went (the
// session journal's `kept`, and the transcript the next turn opens on).
func TestNoWorkerIsBriefedToOwnThePiecesThisConversationIsHolding(t *testing.T) {
	w := newWorld(t)
	// THE MASTERMIND TIER IS THE POINT OF THIS LINE. The mark reader
	// (roles.RoleMarkReader) and the handoff writer (roles.RoleHandoff) are
	// mastermind-tier, and a run that left them on the person's own row would be
	// evidence about somebody else's model.
	pinEveryTextModel(t)

	started := time.Now()
	var reached *custodyRun
	for attempt := 1; attempt <= custodyAttempts && reached == nil; attempt++ {
		run := runCustody(t, w, attempt)
		if run.usable() {
			reached = run
			continue
		}
		t.Logf("ATTEMPT %d did not reach the seam this file is about: %s", attempt, run.why())
	}
	usd, models := ledgerSince(t, started)
	t.Logf("RUN wall=%s spend=$%.6f models=%v", time.Since(started).Round(time.Second), usd, models)
	w.bill("custody run (every attempt, off the machine's own ledger)", usd)
	if usd > custodyCap {
		t.Errorf("this file spent $%.4f, past the $%.2f a few attempts of one writing turn should cost",
			usd, custodyCap)
	}
	if reached == nil {
		t.Fatalf("no attempt in %d crossed the write seam with a drawing about the pieces already out; "+
			"the road this file measures was never entered", custodyAttempts)
	}
	reached.assertCustody(t)
}

// ── one attempt ─────────────────────────────────────────────────────────────

// custodyRun is everything one attempt left behind.
type custodyRun struct {
	t     *testing.T
	w     *world
	agent *session.Agent
	place session.Place
	repo  string
	// pieces are the two the conversation handed out and is holding, and rows is
	// every node in the graph's checkpoint when the turn ended.
	pieces []custodyRow
	rows   []custodyRow
	// marks, carries and ceilings are the journal lines the checkpoint road wrote.
	marks    []custodyMark
	carries  []custodyCarry
	ceilings []custodyCeiling
	wall     time.Duration
	usd      float64
	models   []string
}

// runCustody is one attempt: a fresh checkout, a fresh conversation, two pieces
// handed out and held, and one real turn.
func runCustody(t *testing.T, w *world, attempt int) *custodyRun {
	t.Helper()
	repo := newCustodyGround(t)
	agent, place := w.open(repo, custodyConfig(w))
	run := &custodyRun{t: t, w: w, agent: agent, place: place, repo: repo}

	started := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	// THE TWO PIECES, THROUGH THE DOOR `/task` OPENS. Held at the frontier by the
	// memory floor, so each of these is a shaper call and nothing else.
	for _, brief := range []string{custodyFirstPiece, custodySecondPiece} {
		id, title, _, err := agent.StartTask(ctx, brief)
		if err != nil {
			t.Fatalf("hand out %q: %v", brief, err)
		}
		t.Logf("PIECE %d handed out: %q", id, title)
	}
	cancel()

	run.pieces = run.awaitHeldPieces()
	t.Logf("ATTEMPT %d: %d pieces out under the conversation: %s", attempt, len(run.pieces), run.pieceLog())

	w.say(agent, custodyAsk, answerNo)

	run.wall = time.Since(started)
	run.rows = readCustodyRows(t, place.Tasks())
	run.marks, run.carries, run.ceilings = readCustodyJournal(t, place.Transcript())
	run.usd, run.models = ledgerSince(t, started)
	run.report(attempt)
	return run
}

// awaitHeldPieces waits until the graph's checkpoint holds both pieces as ROOTS
// THAT HAVE NOT SETTLED, which is the whole of what "still out" means, and fails
// naming what is there instead. It reads THE FILE for [readCustodyRows]'s reason.
func (r *custodyRun) awaitHeldPieces() []custodyRow {
	deadline := time.Now().Add(30 * time.Second)
	for {
		var out []custodyRow
		for _, row := range readCustodyRows(r.t, r.place.Tasks()) {
			if row.Parent == 0 && !row.settled() {
				out = append(out, row)
			}
		}
		if len(out) == 2 {
			return out
		}
		if time.Now().After(deadline) {
			r.t.Fatalf("the conversation is not holding two pieces; the checkpoint holds:\n%s",
				custodyRowLog(readCustodyRows(r.t, r.place.Tasks())))
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// usable reports that this attempt actually entered the road the file is about,
// and BOTH HALVES ARE READ OFF FACTS EITHER BUILD WRITES — which is the whole
// requirement of a gate used to judge the same test with the fix and without it.
//
// THE FIRST HALF IS THE SEAM. `wrote` is the write seam's own word for the mark
// it takes when a turn has changed more of the disk than the allowance
// (writeseam.go), and without it the road was never entered at all.
//
// THE SECOND HALF IS THAT THE MATERIAL A WORKER WOULD HAVE OPENED ON WAS ABOUT
// THE PIECES ALREADY OUT, and each build says so in its own way: with the fix,
// a rung of the ladder blanked for it, a drawing reduced against it, or a ceiling
// that declined outright; without the fix, THE ADMITTED TASK'S OWN BRIEF, which
// is the incident. An attempt where none of those is true is an attempt where a
// cheap model simply did not reach for its siblings, and it has measured the
// sidecar's mood rather than the road — so it is put again.
func (r *custodyRun) usable() bool {
	return r.crossedTheWriteSeam() && r.materialWasAboutThePiecesOut()
}

func (r *custodyRun) crossedTheWriteSeam() bool {
	for _, mark := range r.marks {
		if mark.Decision == "wrote" {
			return true
		}
	}
	return false
}

func (r *custodyRun) materialWasAboutThePiecesOut() bool {
	if r.blankedForHeldWork() || r.declinedForHeldWork() || r.realKept() != "" {
		return true
	}
	for _, mark := range r.marks {
		if namesAPieceStillOut(mark.Sketch, r.pieces) {
			return true
		}
	}
	for _, row := range r.admitted() {
		if namesAPieceStillOut(row.Brief, r.pieces) || coordinationOverThePiecesOut(row.Brief) != "" {
			return true
		}
	}
	return false
}

// why says which half of [custodyRun.usable] was missing, for the log line an
// attempt that is put again writes.
func (r *custodyRun) why() string {
	if !r.crossedTheWriteSeam() {
		return "the turn never crossed the write seam — it changed less of the disk than the allowance"
	}
	var sketches []string
	for _, mark := range r.marks {
		sketches = append(sketches, fmt.Sprintf("%s → %q", mark.Decision, mark.Sketch))
	}
	return "the seam fired, but nothing a worker would have opened on was about the pieces " +
		"already out; the marks were: " + strings.Join(sketches, "; ")
}

// assertCustody is the whole of what this file claims, in the order a reader of
// a failure wants it: the brief first, because that is the incident.
func (r *custodyRun) assertCustody(t *testing.T) {
	t.Helper()

	// ── NO WORKER IS BRIEFED TO OWN THE PIECES THIS CONVERSATION IS HOLDING ──
	//
	// Every task admitted off this turn, read out of the graph's own checkpoint:
	// the brief it opens on, the request above it and the done-condition under it.
	// A worker cannot see a sibling, so a duty naming one is a duty it can never
	// discharge — which is the incident, word for word.
	admitted := r.admitted()
	for _, row := range admitted {
		t.Logf("ADMITTED task %d %q\n  request: %s\n  brief: %s\n  acceptance: %s",
			row.ID, row.Title, shorten(row.Request, 300), shorten(row.Brief, 1400),
			shorten(row.Acceptance, 300))
		for what, text := range map[string]string{
			"brief": row.Brief, "request": row.Request, "acceptance": row.Acceptance,
		} {
			if named := namedPiecesStillOut(text, r.pieces); len(named) > 0 {
				t.Errorf("task %d's %s assigns work this conversation is still holding (%s), "+
					"which is work that task can never see:\n%s",
					row.ID, what, strings.Join(named, ", "), text)
			}
		}
		// AND THE COORDINATION VERBS, over the pieces by the words the incident's
		// own drawing used for them. This is the looser reading and it is here
		// because a brief that says "once the other two land" assigns exactly the
		// same impossible duty while naming no id at all.
		if phrase := coordinationOverThePiecesOut(row.Brief); phrase != "" {
			t.Errorf("task %d's brief coordinates work already out (%q), which is this "+
				"conversation's own:\n%s", row.ID, phrase, row.Brief)
		}
	}

	// ── AND THE TWO PIECES ARE STILL THIS CONVERSATION'S OWN ──
	//
	// The other repair — the one the law refused — would have moved them under the
	// new task to make them visible to it, and that is the boundary that makes a
	// worker's world knowable.
	for _, piece := range r.pieces {
		now, found := r.row(piece.ID)
		if !found {
			t.Errorf("piece %d %q is gone from the checkpoint", piece.ID, piece.Title)
			continue
		}
		if now.Parent != 0 {
			t.Errorf("piece %d %q was re-parented under %d; a piece this conversation handed "+
				"out is its own", now.ID, now.Title, now.Parent)
		}
		if now.settled() {
			t.Errorf("piece %d %q settled during the turn (%s), so this run proves nothing about "+
				"a conversation that is holding something", now.ID, now.Title, now.State)
		}
	}

	// ── AND WHATEVER A TASK WAS ADMITTED ON, IT IS A SELF-CONTAINED REMAINDER ──
	if len(admitted) > 1 {
		t.Errorf("one turn admitted %d tasks; the road starts one", len(admitted))
	}

	// ── AND THE WITHHELD COORDINATION CAME BACK TO THE CONVERSATION ──
	//
	// It is the thing this session still owes: the turn woken when those two
	// pieces land opens on the transcript, so a remainder dropped on the floor
	// here is a remainder nobody ever does. Three readers are told and any one of
	// them is the proof — the journal's `kept` on the mark, the transcript block,
	// and the ladder rung that was blanked for naming work already out.
	kept := r.realKept()
	_, inTranscript := transcriptHas(r.agent, custodyRemainderHead)
	blanked := r.blankedForHeldWork()
	declined := r.declinedForHeldWork()
	t.Logf("CUSTODY signals: journal kept=%q, transcript remainder=%v, rungs blanked=%v, ceiling declined=%v",
		shorten(kept, 400), inTranscript, blanked, declined)
	if kept == "" && !inTranscript && !blanked && !declined {
		t.Errorf("nothing on disk says the conversation kept the coordination it drew: "+
			"no `kept` on any mark, no %q block in the transcript, no rung blanked for "+
			"work already out, no ceiling that declined for it.\nthe marks were:\n%s",
			custodyRemainderHead, r.markLog())
	}
	// AND WHERE A TASK ACTUALLY TRAVELLED, the remainder has to be in the
	// transcript, because that is the ONE road that seals the turn: the next turn
	// of this conversation opens on that record and nowhere else. A road that
	// declined started nothing and sealed nothing, so it deliberately writes no
	// assistant message (checkpoint.go), and the journal is the whole account.
	if kept != "" && len(admitted) > 0 && !inTranscript {
		t.Errorf("the journal says %q stayed with the conversation and a task still travelled, "+
			"but the transcript the next turn opens on carries no %q block, so nobody will ever do it",
			shorten(kept, 200), custodyRemainderHead)
	}

	// ── AND HOWEVER THE TURN ENDED, THE FILE HAS ONE ROW SAYING SO ──
	//
	// THIS IS THE THING THE FIRST RUNS OF THIS TEST COULD NOT SEE. The ending row
	// used to be written by the CEILING alone, and this incident goes through the
	// WRITE SEAM — so every run finished with an empty list of them while the
	// refusal had plainly happened, and the only evidence left was the ladder. The
	// row now belongs to the ending rather than to the clock that noticed it.
	//
	// The turn either moved or declined and both write one; what is asserted is
	// that SOMETHING was written and that it names the door it came through, since
	// a row that cannot say which seam took it is a row from before this existed.
	t.Logf("CUSTODY endings: %+v", r.ceilings)
	if len(r.ceilings) == 0 {
		t.Errorf("the turn ended — %d tasks admitted, declined=%v — and the file holds no row "+
			"saying how, so nothing outside this test could ever tell the two apart",
			len(admitted), declined)
	}
	for _, ending := range r.ceilings {
		if ending.Seam == "" {
			t.Errorf("an ending row names no seam: %+v", ending)
		}
	}
	// AND THE DECLINE CARRIES ITS REASON ON THE SAME LINE AS ITS WORD, because an
	// autopsy greps the decision and should not then have to go hunting through a
	// ladder that may not even have a rung to blame.
	if row, found := r.endingForHeldWork(); found {
		if row.Seam != custodyWriteSeam {
			t.Errorf("the decline was taken at seam %q; this incident goes through %q "+
				"(the other two doors are %q and %q)",
				row.Seam, custodyWriteSeam, custodyMarkSeam, custodyCeilingSeam)
		}
		if !strings.Contains(row.Reason, custodyHeldWorkReason) {
			t.Errorf("the decline gives its reason as %q, want %q", row.Reason, custodyHeldWorkReason)
		}
		if row.TaskID != 0 {
			t.Errorf("a row that started nothing names task %d", row.TaskID)
		}
	}
}

// ── readers ─────────────────────────────────────────────────────────────────

// admitted is every ROOT node that is not one of the two pieces handed out —
// which is every task this turn's own road started.
func (r *custodyRun) admitted() []custodyRow {
	out := make(map[uint64]bool, len(r.pieces))
	for _, piece := range r.pieces {
		out[piece.ID] = true
	}
	var rows []custodyRow
	for _, row := range r.rows {
		if row.Parent == 0 && !out[row.ID] {
			rows = append(rows, row)
		}
	}
	sort.Slice(rows, func(a, b int) bool { return rows[a].ID < rows[b].ID })
	return rows
}

func (r *custodyRun) row(id uint64) (custodyRow, bool) {
	for _, row := range r.rows {
		if row.ID == id {
			return row, true
		}
	}
	return custodyRow{}, false
}

// realKept is what the journal says did NOT travel, from the last mark that
// withheld anything, AND IT IGNORES `(waiting)`.
//
// `(waiting)` is the sidecar's own word for a turn with nothing to hand anybody
// (checkpoint.go's checkpointWaitingShape): it reads as the conversation's own
// part, so a conversation that is holding pieces has it written back out as the
// remainder — which is true and is not WORK. Counting it would let an attempt
// that drew nothing at all look like an attempt that kept some coordination.
func (r *custodyRun) realKept() string {
	for at := len(r.marks) - 1; at >= 0; at-- {
		kept := strings.TrimSpace(r.marks[at].Kept)
		if kept != "" && kept != "(waiting)" {
			return kept
		}
	}
	return ""
}

// blankedForHeldWork reports that a rung of the brief ladder was thrown away for
// assigning work this conversation is still holding.
func (r *custodyRun) blankedForHeldWork() bool {
	for _, carry := range r.carries {
		if strings.Contains(carry.Reason, custodyHeldWorkReason) {
			return true
		}
	}
	return false
}

// declinedForHeldWork reports the ceiling that started nothing at all because
// everything it could have carried was about work already out.
func (r *custodyRun) declinedForHeldWork() bool {
	_, found := r.endingForHeldWork()
	return found
}

// endingForHeldWork is that row itself, for the assertions that read the seam it
// was taken at and the reason it gives.
func (r *custodyRun) endingForHeldWork() (custodyCeiling, bool) {
	for _, ceiling := range r.ceilings {
		if ceiling.Decision == custodyHeldWorkDecision {
			return ceiling, true
		}
	}
	return custodyCeiling{}, false
}

func (r *custodyRun) pieceLog() string {
	var out []string
	for _, piece := range r.pieces {
		out = append(out, fmt.Sprintf("task %d %q (%s)", piece.ID, piece.Title, piece.State))
	}
	return strings.Join(out, ", ")
}

func (r *custodyRun) markLog() string {
	var out []string
	for _, mark := range r.marks {
		out = append(out, fmt.Sprintf("  mark decision=%s model=%s cost=$%.6f\n    drew: %s\n    kept: %s",
			mark.Decision, mark.Model, mark.CostUSD, mark.Sketch, mark.Kept))
	}
	if len(out) == 0 {
		return "  (no mark was ever read)"
	}
	return strings.Join(out, "\n")
}

// report is the one block per attempt a person reading the log wants.
func (r *custodyRun) report(attempt int) {
	r.t.Logf("ATTEMPT %d: wall=%s spend=$%.6f models=%v\n%s\ncarries: %v\nceilings: %v\nthe checkpoint holds:\n%s",
		attempt, r.wall.Round(time.Second), r.usd, r.models, r.markLog(),
		r.carries, r.ceilings, custodyRowLog(r.rows))
}

// ── the readings this lane makes of prose ───────────────────────────────────
//
// They mirror internal/session's own (checkpoint_custody.go's namesHeldWork and
// numbersBesideTheNoun) rather than importing them, because the whole point of
// this package is that it drives the engine through the doors a surface has —
// and because a test that shared the reading under test could not fail on it.

// namedPiecesStillOut is every piece of a conversation's ledger that a text
// REFERS TO: by its number standing beside the word this program uses for the
// thing, or by the whole of its name where the text quotes it.
func namedPiecesStillOut(text string, pieces []custodyRow) []string {
	if strings.TrimSpace(text) == "" || len(pieces) == 0 {
		return nil
	}
	words := custodyWords(text)
	numbered := custodyNumbersBesideTheNoun(words)
	var named []string
	for _, piece := range pieces {
		switch {
		case numbered[piece.ID]:
			named = append(named, fmt.Sprintf("task %d by number", piece.ID))
		case len(custodyWords(piece.Title)) >= 2 && custodyContainsRun(words, custodyWords(piece.Title)):
			named = append(named, fmt.Sprintf("task %d by name (%q)", piece.ID, piece.Title))
		}
	}
	return named
}

func namesAPieceStillOut(text string, pieces []custodyRow) bool {
	return len(namedPiecesStillOut(text, pieces)) > 0
}

// custodyNumbersBesideTheNoun reads the ids a text refers to, positionally: a
// number is a reference only where it stands beside `task` or `tasks`, and the
// run of numbers a joiner holds together is read off that noun.
func custodyNumbersBesideTheNoun(words []string) map[uint64]bool {
	joiners := map[string]bool{"and": true, "to": true, "or": true}
	ids := map[uint64]bool{}
	for index, word := range words {
		if word != "task" && word != "tasks" {
			continue
		}
		numbered := false
		for _, next := range words[index+1:] {
			if joiners[next] {
				if numbered {
					continue
				}
				break
			}
			id, err := strconv.ParseUint(next, 10, 64)
			if err != nil {
				break
			}
			ids[id] = true
			numbered = true
		}
	}
	return ids
}

// custodyWords is one text as a sequence of lowercase words, with every mark
// that is not a letter or a digit read as a space.
func custodyWords(text string) []string {
	fields := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
	})
	return fields
}

func custodyContainsRun(words, run []string) bool {
	if len(run) == 0 || len(run) > len(words) {
		return false
	}
	for start := 0; start+len(run) <= len(words); start++ {
		matched := true
		for offset, word := range run {
			if words[start+offset] != word {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

// custodyCoordinationPhrases are the ways the incident's own drawing referred to
// the pieces without naming one: a wait, a gathering, or a plural standing in for
// them. A brief carrying one of these has been handed the same impossible duty
// under a different spelling.
var custodyCoordinationPhrases = []string{
	"still out",
	"their branches",
	"the other two",
	"once they land",
	"once they are back",
	"once they report",
	"wait for the other",
	"the two pieces already out",
	"the pieces already out",
	"the other running task",
	"the running tasks",
	"the sibling task",
	"sibling tasks",
	"the parallel tasks",
	"the two tasks",
	"both tasks",
	"the other tasks",
}

// coordinationOverThePiecesOut answers the first of those a text carries, and ""
// for a text that carries none.
func coordinationOverThePiecesOut(text string) string {
	lower := strings.ToLower(text)
	for _, phrase := range custodyCoordinationPhrases {
		if strings.Contains(lower, phrase) {
			return phrase
		}
	}
	return ""
}

// readsAsCoordination is the looser half of [custodyRun.usable]: a drawing that
// waits for, integrates, lands or reviews something is a drawing with a
// coordination part in it, whether or not it named a piece.
func readsAsCoordination(shape string) bool {
	lower := strings.ToLower(shape)
	for _, verb := range []string{
		"wait for", "waiting for", "once the", "integrate", "merge the", "land the",
		"review the", "open the pull request", "pull request", "still out", "their branches",
	} {
		if strings.Contains(lower, verb) {
			return true
		}
	}
	return false
}

// ── the machine ─────────────────────────────────────────────────────────────

// custodyConfig is the v3 door's own wiring for a conversation that can start
// tasks and write files, with the one departure this file needs named where it is
// made.
func custodyConfig(w *world) func(*session.Config) {
	profile := w.settings.ProfileDir
	return func(cfg *session.Config) {
		cfg.Divide = true
		cfg.TaskModel = config.TaskModelAt(profile)
		cfg.ModelFallbacks = config.ParseModelFallbacks(config.ModelFallbacksAt(profile))
		// The verified frontier and repair rounds are off for families_e2e_test.go's
		// reason: what this file measures is the brief a worker would open on, and a
		// checker's opinion between a node and the person's folder would say nothing
		// about it.
		cfg.TaskAudit = false
		cfg.TaskRepairRounds = 0
		cfg.TaskAutoApproveSeconds = 0
		// THE ONE DEPARTURE, AND IT IS WHAT MAKES THIS FILE CHEAP. The admission
		// governor's memory floor, set past anything a machine has, holds every node
		// this conversation admits at the frontier for `machine busy`
		// (task_pressure.go). The two pieces are therefore ROOTS THAT HAVE NOT
		// SETTLED for the whole of the turn — which is the entire fact
		// [Agent.piecesStillOut] reads — without a worktree, a worker or a call.
		cfg.TaskMinFreeMB = custodyMemoryFloor
		// AND THE TURN MAY WRITE. The ambient lane's policy allows the reading tools
		// only, which is right for a conversation that reads and remembers; this one
		// has to cross the write allowance to reach the road at all.
		policy, err := approval.Load(map[string]any{"default": "allow"})
		if err != nil {
			panic("approval.Load: " + err.Error())
		}
		cfg.ApprovalPolicy = &policy
		// AskConsent stays TRUE, which [world.conversationConfig] already set: a
		// screenless session never checkpoints (checkpoint.go's [Agent.checkpoints]),
		// so turning it off here would turn off the road under test.
	}
}

// newCustodyGround is the person's disposable checkout: one commit, three
// documents with a little material in them, and an identity of their own.
func newCustodyGround(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "picker")
	if err := os.MkdirAll(filepath.Join(dir, "docs"), 0o755); err != nil {
		t.Fatalf("make the person's repository: %v", err)
	}
	gitAt(t, dir, "init", "--quiet")
	gitAt(t, dir, "config", "user.name", "the person")
	gitAt(t, dir, "config", "user.email", "person@localhost")
	gitAt(t, dir, "config", "commit.gpgsign", "false")
	for name, body := range map[string]string{
		"docs/one.md":   "# one\n\nthe folder picker rail, as it stands today.\n",
		"docs/two.md":   "# two\n\nthe settings pane copy, as it stands today.\n",
		"docs/three.md": "# three\n\nwhat the two of them share.\n",
		"README.md":     "# the folder picker\n\nthree documents, one rewrite.\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}
	gitAt(t, dir, "add", ".")
	gitAt(t, dir, "commit", "--quiet", "-m", "the material")
	return dir
}

// ── the disk, as a reader outside the engine sees it ────────────────────────

// custodyRow is one node of the graph's checkpoint as the FILE holds it
// (internal/session's task_store.go). It carries the three texts a worker opens
// on, which is what families_e2e_test.go's [taskRow] has no need of and this file
// is entirely about.
type custodyRow struct {
	ID         uint64 `json:"id"`
	Parent     uint64 `json:"parent"`
	Title      string `json:"title"`
	Request    string `json:"request"`
	Brief      string `json:"brief"`
	Acceptance string `json:"acceptance"`
	State      string `json:"state"`
	Worktree   string `json:"worktree"`
}

// settled reads the engine's own predicate off the file: a node is settled when
// it is neither queued nor running.
func (r custodyRow) settled() bool {
	return r.State != string(session.TaskQueued) && r.State != string(session.TaskRunning)
}

// readCustodyRows reads the graph's checkpoint. A file that is not there yet is
// no nodes, which is what a conversation that has admitted nothing looks like.
func readCustodyRows(t *testing.T, path string) []custodyRow {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var file struct {
		Nodes []custodyRow `json:"nodes"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return file.Nodes
}

func custodyRowLog(rows []custodyRow) string {
	if len(rows) == 0 {
		return "  (no nodes)"
	}
	var out []string
	for _, row := range rows {
		out = append(out, fmt.Sprintf("  task %d parent=%d state=%s %q", row.ID, row.Parent, row.State, row.Title))
	}
	return strings.Join(out, "\n")
}

// The three journal lines the checkpoint road writes (internal/session's
// sessionfile.go). Re-declared for [custodyRow]'s reason: the records are
// unexported there, and what a reader outside the engine has is the file.
type (
	custodyMark struct {
		Model    string  `json:"model"`
		CostUSD  float64 `json:"costUsd"`
		Sketch   string  `json:"sketch"`
		Kept     string  `json:"kept"`
		Decision string  `json:"decision"`
	}
	custodyCarry struct {
		Rung    string `json:"rung"`
		Outcome string `json:"outcome"`
		Reason  string `json:"reason"`
		Used    bool   `json:"used"`
	}
	custodyCeiling struct {
		Seam     string `json:"seam"`
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
		TaskID   uint64 `json:"taskId"`
		Carry    string `json:"carry"`
	}
)

// readCustodyJournal reads the session file back and answers the checkpoint
// road's own three kinds of line, in the order they were written.
func readCustodyJournal(t *testing.T, path string) ([]custodyMark, []custodyCarry, []custodyCeiling) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, nil
	}
	var marks []custodyMark
	var carries []custodyCarry
	var ceilings []custodyCeiling
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var entry struct {
			Type    string          `json:"type"`
			Mark    *custodyMark    `json:"mark"`
			Carry   *custodyCarry   `json:"carry"`
			Ceiling *custodyCeiling `json:"ceiling"`
		}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		switch {
		case entry.Mark != nil:
			marks = append(marks, *entry.Mark)
		case entry.Carry != nil:
			carries = append(carries, *entry.Carry)
		case entry.Ceiling != nil:
			ceilings = append(ceilings, *entry.Ceiling)
		}
	}
	return marks, carries, ceilings
}
