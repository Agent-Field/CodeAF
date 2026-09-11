package session

// standing_fence_test.go holds round 3b of the review of aforge's own two acts
// for a firing — the report's placement and the note's delivery — each fenced
// where it happens (standing_publish.go's [effectFence]).
//
// The third review (c7ec2566f..4701f270d) found four ways past the fence: the
// pass's cancellation was sampled when the fence was MADE, so a cancellation
// between the decision and that moment published; a cancellation while the
// file was being compared was never read again before the rename; a store whose
// item lock could not be taken, or which could not be opened, let the act run
// unfenced; and a note held by cancellation left a run coded cut-off while its
// kind still said landed. And publication was not a compare-and-swap: a first
// publication could write over a file that appeared after the look, and a draft
// that could not be kept was left out of the line without a word.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/filelock"
	"github.com/Agent-Field/aforge-v2/internal/standing"
)

// B1, THE WINDOW BEFORE THE FENCE. The decision's last look at the context and
// the making of the fence were two moments, and the fence kept what it saw at
// the second: a pass cancelled between them made a fence that believed the
// decision itself had been cut off, and it published.
func TestAPassCancelledBeforeTheFenceWasMadePublishesNothing(t *testing.T) {
	root, workspace := t.TempDir(), t.TempDir()
	store, item := storedReporting(t, root, workspace)
	_ = store
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runner := standingChildRunner(t, root, &scriptedCompleter{})
	receipt, held, err := publishStandingReport(runner.fenceFor(ctx, item), workspace, item.Does.Report, "# Report\n- after the cancellation", "")
	if _, statErr := os.Stat(filepath.Join(workspace, "reports", "r.md")); !os.IsNotExist(statErr) || receipt != nil {
		t.Fatalf("a pass cancelled before its fence was made published (held %d, err %v)", held, err)
	}
	if held.code() != "cut-off" {
		t.Fatalf("held %q, want cut-off", held.code())
	}
}

// B1, THE CANCELLATION DURING THE LOOK. The file is compared before it is
// replaced, and the pass's context was read before the comparison and never
// after it: a pass cancelled while the file was being read was renamed over
// anyway. The report path here is a pipe, so the read waits until the test
// has cancelled the pass and then answers exactly what aforge last published.
func TestACancellationWhileTheFileIsComparedHoldsTheRename(t *testing.T) {
	root, workspace := t.TempDir(), t.TempDir()
	_, item := storedReporting(t, root, workspace)
	if err := os.MkdirAll(filepath.Join(workspace, "reports"), 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(workspace, "reports", "r.md")
	if err := syscall.Mkfifo(target, 0o644); err != nil {
		t.Skipf("no named pipes here: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		// Opening a pipe to write waits for its reader: this returns only once
		// the publisher is inside its comparison, past every earlier look.
		pipe, err := os.OpenFile(target, os.O_WRONLY, 0)
		if err != nil {
			t.Error(err)
			return
		}
		cancel()
		_, _ = pipe.WriteString(lastGoodReport)
		_ = pipe.Close()
	}()
	runner := standingChildRunner(t, root, &scriptedCompleter{})
	receipt, held, err := publishStandingReport(runner.fenceFor(ctx, item), workspace, item.Does.Report, "# Report\n- after the cancellation", sha256Hex(lastGoodReport))
	if receipt != nil || held.code() != "cut-off" {
		t.Fatalf("a pass cancelled during the comparison was renamed over the file: receipt %+v, held %q, err %v", receipt, held.code(), err)
	}
	if info, statErr := os.Lstat(target); statErr != nil || info.Mode()&os.ModeNamedPipe == 0 {
		t.Fatalf("the file at the report path was replaced (%v)", statErr)
	}
}

// B1, A STORE THAT CANNOT ANSWER. The stop is read under the item's own lock;
// a lock that could not be taken fell through to the act with no fence at all,
// which is reading "nobody could say" as "nobody said stop". It withholds now.
func TestAnItemLockThatCannotBeTakenWithholdsThePublication(t *testing.T) {
	root, workspace := t.TempDir(), t.TempDir()
	store, item := storedReporting(t, root, workspace)
	// A folder where the lock file goes: opening it to lock fails every time.
	lock := filepath.Join(filepath.Dir(store.ItemPath(item.ID)), item.ID+".lock")
	_ = os.Remove(lock)
	if err := os.Mkdir(lock, 0o700); err != nil {
		t.Fatal(err)
	}
	runner := standingChildRunner(t, root, &scriptedCompleter{})
	receipt, held, _ := publishStandingReport(runner.fenceFor(context.Background(), item), workspace, item.Does.Report, "# Report\n- unfenced", "")
	if _, err := os.Stat(filepath.Join(workspace, "reports", "r.md")); !os.IsNotExist(err) || receipt != nil {
		t.Fatalf("a publication whose stop could not be read went ahead (held %q)", held.code())
	}
	if held.code() != "stop-unknown" {
		t.Fatalf("held %q, want stop-unknown", held.code())
	}
}

// And a store that could not be opened at all was a fence with no store, which
// is the fence of a runner nobody could ever stop.
func TestAStoreThatCannotBeOpenedWithholdsThePublication(t *testing.T) {
	workspace := t.TempDir()
	root := filepath.Join(t.TempDir(), "not-a-folder")
	if err := os.WriteFile(root, []byte("a file where the store should be"), 0o600); err != nil {
		t.Fatal(err)
	}
	item := reporting(workspace)
	runner := standingChildRunner(t, root, &scriptedCompleter{})
	receipt, held, _ := publishStandingReport(runner.fenceFor(context.Background(), item), workspace, item.Does.Report, "# Report\n- unfenced", "")
	if _, err := os.Stat(filepath.Join(workspace, "reports", "r.md")); !os.IsNotExist(err) || receipt != nil {
		t.Fatalf("a publication with no readable store went ahead (held %q)", held.code())
	}
	if held.code() != "stop-unknown" {
		t.Fatalf("held %q, want stop-unknown", held.code())
	}
}

// cancelledInsideTheLock is a pass context that is cancelled the moment
// anybody holds the item's own lock — which, in a firing, is aforge at one of
// its two acts. Every look taken outside the lock (the run's turns, the
// decision) finds the pass running; the look the fence takes inside it finds it
// cancelled. It is the cancellation that arrives while an act is waiting at the
// fence, made deterministic.
type cancelledInsideTheLock struct {
	context.Context
	lock string
}

func (c cancelledInsideTheLock) Err() error {
	file, err := os.OpenFile(c.lock, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil
	}
	defer file.Close()
	if err := filelock.Lock(file, true, true); err != nil {
		return context.Canceled
	}
	_ = filelock.Unlock(file)
	return nil
}

// B1, THE KIND AND THE CODE. A note held back because the pass was cut off
// added a sentence and a code to a run whose kind still said it landed: the
// record said cut-off and landed at once. A clean run with no report whose note
// never went did not finish telling anybody, and it is recorded as that — one
// answer, one kind.
func TestANoteHeldByACancellationIsNotAlsoLanded(t *testing.T) {
	root, workspace := t.TempDir(), t.TempDir()
	store, err := standing.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	item := nightly(workspace)
	item.ID = ""
	made, err := store.Create(item)
	if err != nil {
		t.Fatal(err)
	}
	ctx := cancelledInsideTheLock{Context: context.Background(), lock: filepath.Join(root, made.ID+".lock")}
	model := &scriptedCompleter{steps: []step{saying("Three flaky tests, all the same timeout.")}}
	outcome, err := standingChildRunner(t, root, model).Run(ctx, made, filepath.Join(store.RunsDir(made.ID), standing.RunName(1)), "")
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Withheld != "cut-off" || outcome.Kind != standing.OutcomeFailed || !strings.Contains(outcome.Text, "cut off before its note was delivered") {
		t.Fatalf("outcome %s · %q · %q; want failed · cut-off saying the note was not delivered", outcome.Kind, outcome.Withheld, outcome.Text)
	}
}

// reportExists is a pass context that is cancelled from the moment the item's
// report is on disk: the publication finds the pass running and the note after
// it finds it cut off.
type reportExists struct {
	context.Context
	path string
}

func (c reportExists) Err() error {
	if _, err := os.Stat(c.path); err == nil {
		return context.Canceled
	}
	return nil
}

// AND A PUBLISHED REPORT IS THE END OF THE WORK. When the cut comes after the
// report was placed, the run landed — the receipt is the proof, the rule the
// pass's recovery already keeps — and what it did not do is tell anybody. Its
// record says landed with no withheld code, never cut-off beside a receipt.
func TestACutAfterThePublicationKeepsTheLandingAndSaysTheNoteDidNotGo(t *testing.T) {
	root, workspace := t.TempDir(), t.TempDir()
	store, item := storedReporting(t, root, workspace)
	ctx := reportExists{Context: context.Background(), path: filepath.Join(workspace, "reports", "r.md")}
	model := &scriptedCompleter{steps: []step{saying("<report>\n# Report\n- placed before the cut\n</report>")}}
	outcome, err := standingChildRunner(t, root, model).Run(ctx, item, filepath.Join(store.RunsDir(item.ID), standing.RunName(1)), "")
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Published == nil || outcome.Kind != "landed" || outcome.Withheld != "" || !strings.Contains(outcome.Text, "cut off before its note was delivered") {
		t.Fatalf("outcome %s · %q · published %v · %q; want landed, no code, the note named", outcome.Kind, outcome.Withheld, outcome.Published != nil, outcome.Text)
	}
}

// fileAppearsAtTheLook is a pass context whose look, taken by the fence
// immediately before the placement, is the moment somebody else creates the
// report file. It is the first publication's race made deterministic: the file
// was absent when aforge compared and present when it placed.
type fileAppearsAtTheLook struct {
	context.Context
	path, theirs string
	once         *sync.Once
	armed        *bool
}

func (c fileAppearsAtTheLook) Err() error {
	if *c.armed {
		c.once.Do(func() { _ = os.WriteFile(c.path, []byte(c.theirs), 0o644) })
	}
	return nil
}

// B2(a), THE FIRST PUBLICATION IS A CREATE, NOT A REPLACE. The file was looked
// for, found absent, and then renamed over whatever was there by the time the
// rename ran — so a file the person made in between was written over. The
// first placement now refuses a name that is taken, and the run waits on the
// person with its draft kept.
func TestAFileThatAppearsBeforeTheFirstPlacementIsNotWrittenOver(t *testing.T) {
	workspace := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspace, "reports"), 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(workspace, "reports", "r.md")
	const theirs = "# my own notes\n"
	armed := false
	ctx := fileAppearsAtTheLook{Context: context.Background(), path: target, theirs: theirs, once: &sync.Once{}, armed: &armed}
	runner := standingChildRunner(t, "", &scriptedCompleter{})
	fence := runner.fenceFor(ctx, reporting(workspace))
	armed = true
	receipt, held, err := publishStandingReport(fence, workspace, "reports/r.md", "# Report\n- the first page", "")
	raw, _ := os.ReadFile(target)
	if string(raw) != theirs || receipt != nil {
		t.Fatalf("a file that appeared before the first placement was written over: %q (held %q, err %v)", raw, held.code(), err)
	}
	if held.code() != "report-changed" {
		t.Fatalf("held %q, want report-changed", held.code())
	}
}

// And the same race, raced for real: a writer creating the file exclusively
// while aforge publishes its first report. Both can never succeed — one of the
// two finds the name taken — whichever wins the instant.
func TestAFirstPublicationAndARacingCreateNeverBothSucceed(t *testing.T) {
	iterations := 300
	if testing.Short() {
		iterations = 50
	}
	// The proof against the old logic raced many more (the round-3b record).
	if n, err := strconv.Atoi(os.Getenv("AFORGE_RACE_ITERATIONS")); err == nil && n > 0 {
		iterations = n
	}
	runner := standingChildRunner(t, "", &scriptedCompleter{})
	for i := 0; i < iterations; i++ {
		workspace := t.TempDir()
		reports := filepath.Join(workspace, "reports")
		if err := os.MkdirAll(reports, 0o755); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(reports, "r.md")
		done := make(chan struct{})
		created := make(chan bool, 1)
		go func() {
			// Wait until the publisher has its draft beside the report, then
			// try to create the report ourselves until one of us has it.
			for {
				select {
				case <-done:
					created <- false
					return
				default:
				}
				if temps, _ := filepath.Glob(filepath.Join(reports, ".report-*")); len(temps) > 0 {
					break
				}
			}
			delay := time.Duration(i%40) * 5 * time.Microsecond
			time.Sleep(delay)
			for {
				file, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
				if err == nil {
					_, _ = file.WriteString("theirs\n")
					_ = file.Close()
					created <- true
					return
				}
				if !errors.Is(err, os.ErrExist) {
					created <- false
					return
				}
				select {
				case <-done:
					created <- false
					return
				default:
				}
			}
		}()
		receipt, _, err := publishStandingReport(runner.fenceFor(context.Background(), reporting(workspace)), workspace, "reports/r.md", "ours", "")
		close(done)
		theirs := <-created
		if theirs && receipt != nil {
			raw, _ := os.ReadFile(target)
			t.Fatalf("iteration %d: the racing writer created the file and aforge still placed its report over it: %q (err %v)", i, raw, err)
		}
	}
}

// B2(c), A DRAFT THAT COULD NOT BE KEPT IS SAID. Both lines that hold a report
// back point at the draft beside the run, and both used to leave the pointer
// out, silently, when the draft could not be written — the person was told the
// report was held and not that its words were gone.
func TestAHeldDraftThatCouldNotBeKeptIsSaidOnTheLine(t *testing.T) {
	root, workspace := t.TempDir(), withLastGoodReport(t)
	store, item := storedReporting(t, root, workspace)
	runDir := filepath.Join(store.RunsDir(item.ID), standing.RunName(1))
	// A folder where the draft goes: writing it fails.
	if err := os.MkdirAll(filepath.Join(runDir, heldReportFile), 0o700); err != nil {
		t.Fatal(err)
	}
	model := &scriptedCompleter{steps: []step{saying("<report>\n# Report\n- the first page\n</report>")}}
	outcome, err := standingChildRunner(t, root, model).Run(context.Background(), item, runDir, "")
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Withheld != "report-changed" {
		t.Fatalf("outcome %+v, want report-changed", outcome)
	}
	if !strings.Contains(outcome.NeedsPerson, "the draft could not be kept") {
		t.Fatalf("the line hides that the draft was lost: %q", outcome.NeedsPerson)
	}
}

// And the rules check's hold says it the same way.
func TestARuleHoldWhoseDraftCouldNotBeKeptSaysSo(t *testing.T) {
	root, workspace := t.TempDir(), withLastGoodReport(t)
	runDir := admittedRun(t, root)
	if err := os.MkdirAll(filepath.Join(runDir, heldReportFile), 0o700); err != nil {
		t.Fatal(err)
	}
	model := &scriptedCompleter{steps: []step{
		saying("<report>\n# Report\n- Priya: priya@example.com\n</report>"),
		saying("<report>\n# Report\n- Priya: priya@example.com, still\n</report>"),
	}}
	runner, _ := ruledRunner(t, root, model,
		`{"rules":[{"rule":1,"kind":"prohibition","verdict":"broken","quote":"priya@example.com","why":"an email address"}]}`,
		`{"rules":[{"rule":1,"kind":"prohibition","verdict":"broken","quote":"priya@example.com, still","why":"still there"}]}`)
	outcome, err := runner.Run(context.Background(), reporting(workspace), runDir, "")
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Withheld != "held-by-rules" || !strings.Contains(outcome.NeedsPerson, "the draft could not be kept") {
		t.Fatalf("outcome %s · %q · %q; want held-by-rules saying the draft could not be kept", outcome.Kind, outcome.Withheld, outcome.NeedsPerson)
	}
}
