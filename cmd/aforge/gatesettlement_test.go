package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/revision"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// s5Run is one graded headless run of the DeepSWE sweep, reduced to the only
// thing these tests weigh: the sequence of delivery-gate events it journaled,
// the exit code it was measured leaving with, and how many hidden checks it
// actually passed.
//
// It is read from testdata rather than written by hand because a settlement rule
// tested against invented events is a rule tested against the author's idea of
// what the gate emits. Every field of every row below came out of
// bench/deepswe/results/*-s5/graph.db on 2026-08-29; the gap strings are clipped
// to their first line, which is exactly what the stream prints.
type s5Run struct {
	Task  string `json:"task"`
	Exit  int    `json:"measured_exit"`
	F2P   string `json:"f2p"`
	Gates []struct {
		Node         string `json:"node"`
		Pass         bool   `json:"pass"`
		Gap          string `json:"gap"`
		PolishClosed bool   `json:"polish_closed"`
		Extended     bool   `json:"extended"`
		Mechanical   bool   `json:"mechanical"`
		Unclosed     bool   `json:"unclosed"`
		Overturned   bool   `json:"overturned"`
		Round        int    `json:"round"`
		Refused      string `json:"refused"`
	} `json:"gates"`
}

func (r s5Run) last() store.DeliveryGate {
	row := r.Gates[len(r.Gates)-1]
	return store.DeliveryGate{
		Pass: row.Pass, Gap: row.Gap, PolishClosed: row.PolishClosed,
		Extended: row.Extended, Mechanical: row.Mechanical, Unclosed: row.Unclosed,
		Overturned: row.Overturned, Round: row.Round, Refused: row.Refused,
	}
}

func loadS5Runs(t *testing.T) []s5Run {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "s5-delivery-gates.json"))
	if err != nil {
		t.Fatalf("read the s5 gate sequences: %v", err)
	}
	var runs []s5Run
	if err := json.Unmarshal(raw, &runs); err != nil {
		t.Fatalf("read the s5 gate sequences: %v", err)
	}
	if len(runs) != 5 {
		t.Fatalf("the sweep is five runs; testdata holds %d", len(runs))
	}
	return runs
}

// The verdict a person watching reads and the verdict the exit code carries are
// one fact, so they are one reading.
//
// They were two. deliveredWhole combined Pass, PolishClosed and Overturned;
// gateWords, in the same file, built its line out of Pass and Refused alone. On
// three of the five s5 runs they disagreed out loud — ink and ofetch ended on
//
//	gate: fail — The deliverable is a listing of files, not the answer itself …
//
// as the last thing anybody saw, and left with exit 0 over a gate their own
// repair round had closed and re-judged. This pins the agreement rather than
// either answer: whatever store.DeliveryGate.Whole says of an event, the word
// the stream prints for that same event says the same thing.
func TestTheGateLinePersonReadsAgreesWithTheExitCode(t *testing.T) {
	for _, run := range loadS5Runs(t) {
		gate := run.last()
		verdict, _ := gateWords(gate)
		// "refused" is its own word on purpose — a round the run declined to buy
		// is not a delivery that fell short — so what it must agree about is the
		// settlement, which a refusal states in Overturned.
		said := verdict == "pass" || (verdict == "refused" && gate.Overturned)
		if said != gate.Whole() {
			t.Errorf("%s: the stream says %q and the settlement says whole=%v for the same gate;\n"+
				"a person watching and a pipeline reading the exit code are looking at one event\ngap: %s",
				run.Task, verdict, gate.Whole(), gate.Gap)
		}
	}
}

// A run that settles short says so, last, in words, naming what it did not do.
//
// happy-dom s5 and igel s5 both ended on a provenance refusal — "the same words
// were already worked on once" — which under SETTLEMENT §2 leaves the finding
// standing and the run partial. Both did leave with exit 2, which is the rule
// working. What neither of them printed was WHY: the last line a person saw was
// a ✓ on a node, and the finding the run itself agreed with was in the journal
// and nowhere a person could read it. FAILSAFE clause 3.
func TestAPartialRunNamesTheFindingItIsShortOf(t *testing.T) {
	partials := 0
	for _, run := range loadS5Runs(t) {
		gate := run.last()
		finding, reason, standing := gateStanding(gate)
		if gate.Whole() {
			if standing {
				t.Errorf("%s: a gate that settled whole owes no reservation, and named %q", run.Task, finding)
			}
			continue
		}
		partials++
		if !standing {
			t.Errorf("%s: the run is partial and named nothing it was short of", run.Task)
			continue
		}
		line := partialWords(finding, reason)
		if !strings.HasPrefix(line, "partial — gate: ") {
			t.Errorf("%s: the closing line is %q", run.Task, line)
		}
		// The finding is the news and the reason is the footnote, which is the
		// order gateWords already reads in.
		if !strings.Contains(line, finding) || !strings.Contains(line, "not repaired: "+reason) {
			t.Errorf("%s: the closing line drops half of what it is for: %q", run.Task, line)
		}
	}
	if partials == 0 {
		t.Fatal("no s5 run settled short, so nothing here was exercised")
	}
}

// The record the gate is held to is the JOB's, not the one leaf's.
//
// A repair round writes nothing — the work landed under its parent — so the gate
// judging one was handed an empty artifact list and told the run had been
// watched from beginning to end. Evidence.namedBlock then states, of a file
// sitting on disk, that "nothing of that name is among what was left behind",
// and textual s5 and igel s5 are both that sentence coming back out of the judge
// as the finding they refused the delivery on. Meanwhile the settlement printed
// the same run's --json artifact list out of the OTHER record and named the file.
//
// The deliverable's spelling is relative and the record's is absolute, which is
// the second half of the same defect: identity is namedAs, by path, and never
// string equality on whatever spelling either side happened to use.
func TestTheGateReadsTheJobsRecordAndNotOneLeafs(t *testing.T) {
	workspace := t.TempDir()
	produced := filepath.Join(workspace, "examples", "rich_log_follow_state.py")
	if err := os.MkdirAll(filepath.Dir(produced), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(produced, []byte("class RichLogFollowStateApp:\n    pass\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The job's record holds the absolute path, because that is what a leaf's
	// workspace records. The repair node being judged produced nothing at all.
	record := &errandRegistry{}
	record.add(produced)

	joined := jobArtifacts(record, nil)
	if len(joined) == 0 {
		t.Fatal("the gate was handed an empty record while the job's own registry held a file")
	}
	// And the request names it the way a person writes it: relative, no leading
	// workspace. That must read as the same file.
	evidence := revision.Evidence{
		Artifacts: joined,
		Named:     []string{"examples/rich_log_follow_state.py"},
		Observed:  true,
	}
	if _, ok := revision.ProducedFile("examples/rich_log_follow_state.py", evidence.Artifacts); !ok {
		t.Fatalf("a relative spelling did not answer to the absolute path the record holds:\n%v",
			evidence.Artifacts)
	}
	// What the judge is shown of that record is pinned next door, where the
	// block can be rendered: internal/revision, TestTheRecordSaysProducedForA
	// FileTheRequestNamedRelatively.
	// A leaf that DID write something is still in the list: the union is the
	// record, not a replacement of it.
	if got := jobArtifacts(record, []string{"/elsewhere/other.py"}); len(got) != 2 {
		t.Errorf("the job's record and the leaf's own paths did not join: %v", got)
	}
	// And a driver with no record at all — every driver but the errand — is
	// left exactly where it was.
	if got := jobArtifacts(nil, []string{produced}); len(got) != 1 || got[0] != produced {
		t.Errorf("a nil record changed the leaf's own list: %v", got)
	}
}

// s5Acquittal is the one refusal of the sweep that acquitted a true finding.
type s5Acquittal struct {
	Citations   []string `json:"citations"`
	Deliverable string   `json:"deliverable"`
	Artifacts   []string `json:"artifacts"`
	Refusal     string   `json:"refusal_recorded"`
	Overturned  bool     `json:"overturned_recorded"`
}

// Prose in the deliverable may not overturn a finding about a run that left
// files behind.
//
// textual s5, node task-2-x3. The review was right: the example file the request
// named had never been written by that node, and the run scored 1 of 20 hidden
// checks. It was acquitted anyway, because every item the citation names —
// RichLogFollowStateApp, FollowChanged, the six button ids — appears in the
// deliverable's own summary of work it claimed to have done. The gate weighed
// the CLAIM against the request and called it evidence, which is FAILSAFE
// clause 2 broken in the strict sense the clause states it: the evidence was
// sourced from the component being checked.
//
// The rule kept: where the run produced nothing but the message, the message IS
// what it left behind, and a citation settled against it is settled against
// everything there is. That is the twelve-profiles case this door was built for
// (TestADeliverableContainingWhatWasAskedForDoesNotFailForLackingIt) and it is
// the same rule, not an exception to it.
func TestTheDeliverablesOwnProseCannotOverturnAFindingAboutFilesOnDisk(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "s5-textual-acquittal.json"))
	if err != nil {
		t.Fatalf("read the s5 acquittal: %v", err)
	}
	var measured s5Acquittal
	if err := json.Unmarshal(raw, &measured); err != nil {
		t.Fatalf("read the s5 acquittal: %v", err)
	}
	if !measured.Overturned {
		t.Fatal("testdata no longer holds the acquittal this pins")
	}
	// The measured input, unchanged, against a run that left nothing behind:
	// the containment fires, which is what made this refusal possible at all.
	if closed := revision.AdmitGapPresent(measured.Citations, measured.Deliverable,
		revision.Evidence{}); closed == "" {
		t.Fatalf("the s5 input no longer reaches the door this test is about; "+
			"the case it pins cannot be exercised\ncitation: %s", measured.Citations[0])
	}
	// And against the run as it actually was: three files in the record. The
	// record is the world, the text is a claim about it, and a claim settles
	// nothing.
	if closed := revision.AdmitGapPresent(measured.Citations, measured.Deliverable,
		revision.Evidence{Artifacts: measured.Artifacts, Observed: true}); closed != "" {
		t.Fatalf("the deliverable's own prose overturned a finding about a run that left "+
			"%d files behind, and the run shipped 1 of 20 hidden checks over exit 0\nrefusal: %s",
			len(measured.Artifacts), closed)
	}
}

// EVERY GATE IS HELD TO THE JOB'S RECORD, AND THIS IS THE SOURCE TEST THAT SAYS
// SO.
//
// The defect was not a wrong comparison; it was a wrong ARGUMENT. gateEvidence
// takes a list of what the run left behind, and both of its callers handed it
// `absolute` — the artifacts of the one leaf being judged — while the settlement
// forty lines away narrated the job's own registry. A repair node produces
// nothing, so the gate that judged one was told the run had left nothing behind
// and refused deliveries whose files were on disk (textual s5 task-2-x3, igel s5
// task-2-x1; docs/design/gate/SETTLEMENT.md §5).
//
// A behaviour test cannot see that, because both lists are the right TYPE and a
// unit test supplies whichever one it means. What is checkable is the seam: the
// record a gate is held to is assembled by jobArtifacts and by nothing else, and
// a third caller added later gets the same record without having to know why.
func TestEveryDeliveryGateIsHeldToTheJobsRecord(t *testing.T) {
	source, err := os.ReadFile("chat.go")
	if err != nil {
		t.Fatalf("read the wiring: %v", err)
	}
	const call = "gateEvidence(node, task.Spec, outcome,"
	body := string(source)
	found := 0
	for index := strings.Index(body, call); index >= 0; {
		found++
		// The argument after the outcome is the record, and it must be the
		// job's. Whitespace and a line break may sit between them, so the test
		// reads the next non-empty token rather than a fixed offset.
		rest := strings.TrimSpace(body[index+len(call):])
		if !strings.HasPrefix(rest, "jobArtifacts(") {
			t.Errorf("a delivery gate is held to a record that is not the job's:\n\t%s…",
				strings.SplitN(rest, "\n", 2)[0])
		}
		next := strings.Index(body[index+len(call):], call)
		if next < 0 {
			break
		}
		index += len(call) + next
	}
	if found == 0 {
		t.Fatal("no gate call site was found; this test has stopped watching anything")
	}
}

// A REPAIR THAT MOVED NOTHING MAY NOT CLOSE A FINDING ABOUT THE WORLD, AND THE
// WIRING ASKS ONE FUNCTION WHETHER IT DID.
//
// The seam set PolishClosed from the re-judgement's verdict alone. That reads as
// airtight — the second gate re-runs the whole world half before it answers —
// and it is not, because of what the repair in front of it had been: a
// composition rewrites the account of work that already landed and runs nothing,
// so the second gate reads a better summary of the identical world the first one
// failed. ink s5 and ofetch s5 both settled whole that way, at 7 of 25 and 44 of
// 47 hidden checks (docs/design/gate/SETTLEMENT.md §8).
//
// The rule lives in revision.RepairClosed, which weighs the verdict, the tree
// stamp taken either side of the round, and whether the finding's ground is the
// world. This pins that the settlement asks it rather than deciding for itself —
// the same thing the record test above pins, one field along, and for the same
// reason: an expression at a wiring seam is a rule nobody can see to get right.
func TestThePolishVerdictIsWeighedAgainstWhatTheRepairMoved(t *testing.T) {
	source, err := os.ReadFile("chat.go")
	if err != nil {
		t.Fatalf("read the wiring: %v", err)
	}
	body := string(source)
	const assigned = "evidence.PolishClosed = "
	index := strings.Index(body, assigned)
	if index < 0 {
		t.Fatal("nothing sets PolishClosed; this test has stopped watching anything")
	}
	if strings.Contains(body[index+len(assigned):], assigned) {
		t.Error("PolishClosed is set in two places, so the rule has two copies")
	}
	line := strings.SplitN(body[index+len(assigned):], "\n", 2)[0]
	if !strings.HasPrefix(strings.TrimSpace(line), "revision.RepairClosed(") {
		t.Errorf("the settlement decides for itself whether a repair closed the gate:\n\t%s", line)
	}
	// And the stamp it is weighed against is taken twice, on either side of the
	// round: one call is a stamp nothing is compared to.
	if strings.Count(body, "revision.TreeStamp(") != 2 {
		t.Errorf("the tree is stamped %d times; a repair is judged on the difference between two",
			strings.Count(body, "revision.TreeStamp("))
	}
}

// And the closing line names the one reason a person would never guess: a repair
// ran, and it changed nothing on disk.
func TestAnUnmovedRepairIsNamedInTheClosingLine(t *testing.T) {
	// The ink s5 event as this build would journal it: the composition's pass
	// did not count, so PolishClosed is absent and Unmoved says why.
	gate := store.DeliveryGate{
		Gap:     "The deliverable is a listing of files, not the answer itself.",
		Unmoved: true,
	}
	if gate.Whole() {
		t.Fatal("a gate no repair closed settled whole")
	}
	finding, reason, standing := gateStanding(gate)
	if !standing {
		t.Fatal("the run is partial and named nothing it was short of")
	}
	if !strings.Contains(reason, "changed nothing on disk") {
		t.Errorf("the reason does not say what the repair did: %q", reason)
	}
	line := partialWords(finding, reason)
	if !strings.HasPrefix(line, "partial — gate: ") || !strings.Contains(line, "not repaired: ") {
		t.Errorf("the closing line is %q", line)
	}
	// A refusal, when there is one, is the more specific reason and wins.
	gate.Refused = "there is not enough time left on the run to finish it"
	if _, reason, _ = gateStanding(gate); reason != gate.Refused {
		t.Errorf("a refusal was talked over by the fallback: %q", reason)
	}
}
