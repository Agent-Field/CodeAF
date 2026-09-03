package main

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/pair"
	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// Row 14. The answer `aforge plan show` gives is THE PLAN, and a `goal:` line in
// front of it is the difference between `aforge plan show p.json > table.txt`
// keeping a table and keeping a table with a preamble stuck to its head. The
// same is true of the receipt for a file `--out` wrote: useful, and not the
// plan.
func TestThePlanDoorsKeepTheirPreambleBesideTheAnswerAndNotInIt(t *testing.T) {
	commentary := captureAside(t)
	graph := &plan.Graph{Goal: "keep the preamble beside the plan", NextID: 2, Nodes: []plan.Node{
		{ID: 1, Title: "one step", Brief: "do the one thing", State: plan.StatePending},
	}}
	encoded, err := graph.JSON()
	if err != nil {
		t.Fatal(err)
	}
	planFile := filepath.Join(t.TempDir(), "plan.json")
	if err := os.WriteFile(planFile, encoded, 0o644); err != nil {
		t.Fatal(err)
	}
	written := filepath.Join(t.TempDir(), "copy.json")

	answer, err := captureStdout(t, func() error { return runShow("plan show", []string{planFile}) })
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(answer, "goal:") {
		t.Fatalf("the goal line is on stdout, in front of the plan:\n%s", answer)
	}
	if !strings.Contains(commentary.String(), "goal:") {
		t.Fatalf("the goal line was not written to the aside either:\n%q", commentary.String())
	}

	commentary.Reset()
	loaded := loadTestPlan(t, planFile)
	answer, err = captureStdout(t, func() error { return emit(loaded, written, false) })
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(answer, "written to") {
		t.Fatalf("the receipt for the file is on stdout, where the plan is:\n%s", answer)
	}
	if !strings.Contains(commentary.String(), "written to "+written) {
		t.Fatalf("the receipt for the file was not written to the aside:\n%q", commentary.String())
	}
}

func loadTestPlan(t *testing.T, path string) *plan.Graph {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	graph, err := plan.Load(data)
	if err != nil {
		t.Fatal(err)
	}
	return graph
}

// Row 15. `Seats.Report` exists so the doors cannot label the same fact
// differently, and `plan run` used to spell the models line itself — its own
// label, its own padding, its own placement for the inheritance notice. The next
// field added to the report would have been missing on exactly one door.
//
// It is read structurally rather than by grepping the source text, because a
// comment quoting the old shape must not be able to fail it.
func TestEveryDoorPrintsTheModelsLineThroughTheOneReport(t *testing.T) {
	fileSet := token.NewFileSet()
	names, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	reports := 0
	for _, name := range names {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		source, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := parser.ParseFile(fileSet, name, source, 0)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			receiver, ok := selector.X.(*ast.Ident)
			if !ok || receiver.Name != "seats" {
				return true
			}
			switch selector.Sel.Name {
			case "Report":
				reports++
			case "Sentence", "Line":
				position := fileSet.Position(call.Pos())
				t.Errorf("%s:%d builds the models line out of seats.%s() and its own padding\n"+
					"there is one sentence and Seats.Report owns it — a door that spells its own "+
					"will be missing the next field added to the report",
					filepath.Base(position.Filename), position.Line, selector.Sel.Name)
			}
			return true
		})
	}
	if reports == 0 {
		t.Fatal("no door calls seats.Report() at all, so this test is watching nothing")
	}
}

// Row 13. `TRIED COST LEARNED` over no rows is a table claiming rows that are
// not there, and a person reads it as a reader that failed rather than as a day
// with nothing on it. A header is never printed without a row under it.
func TestASelfSpendDayWithNothingOnItSaysSoInsteadOfPrintingAHeader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.db")
	graph, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.Close(); err != nil {
		t.Fatal(err)
	}
	var answer bytes.Buffer
	if err := runWhyTo([]string{"self", "--db", path}, &answer, time.Now()); err != nil {
		t.Fatal(err)
	}
	printed := answer.String()
	if strings.Contains(printed, "TRIED") {
		t.Fatalf("a column header was printed with no row under it:\n%s", printed)
	}
	if strings.TrimSpace(printed) == "" {
		t.Fatal("`aforge why self` answered a day with nothing on it with silence, which reads as a broken command")
	}
}

// Row 13, the other half. `aforge services` on a healthy machine printed
// absolutely nothing and exited 0, which is indistinguishable from a command
// that broke. `aforge cache` has had the sentence all along.
func TestNothingBeingKeptRunningIsASentenceAndNotSilence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "quiet.db")
	graph, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.Close(); err != nil {
		t.Fatal(err)
	}
	printed, err := captureStdout(t, func() error { return runServices([]string{"--db", path}) })
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(printed) == "" {
		t.Fatal("`aforge services` answered an idle machine with silence, which reads as a broken command")
	}
}

// Row 20. `--all` was read by hand and only when it was the FIRST word after
// `revoke`, so `aforge devices revoke laptop --all` was refused — and refused
// with a usage line that did not mention `--all` at all. Somebody taking back
// access to their own machine was told the wrong grammar for the gesture they
// had just typed correctly.
func TestDevicesRevokeReadsAllInEitherPositionAndSaysSoInTheUsage(t *testing.T) {
	if !strings.Contains(usageText, "--all") {
		t.Fatal("`aforge --help` does not name `--all` on `aforge devices revoke`")
	}
	if shape := usageForCommand("devices revoke"); !strings.Contains(shape, "--all") {
		t.Fatalf("`aforge devices revoke --help` does not name --all:\n%s", shape)
	}
	for _, args := range [][]string{{"revoke", "--all", "laptop"}, {"revoke", "laptop", "--all"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			book := pair.BookAt(filepath.Join(t.TempDir(), "devices.json"))
			for _, key := range []string{"key-one", "key-two"} {
				if err := book.Admit(pair.Paired{Label: "laptop", Key: key, Since: time.Now()}); err != nil {
					t.Fatal(err)
				}
			}
			printed, err := captureStdout(t, func() error { return revokeDevice(book, args[1:]) })
			if err != nil {
				t.Fatalf("%v — and the whole point of the row is that this spelling was refused", err)
			}
			left, err := book.Devices()
			if err != nil {
				t.Fatal(err)
			}
			if len(left) != 0 {
				t.Fatalf("%d devices are still admitted after --all stopped them: %v\nsaid: %s", len(left), left, printed)
			}
			if !strings.Contains(printed, "stopped") {
				t.Fatalf("nothing said what happened:\n%s", printed)
			}
		})
	}
}

// Row 29. A defect report carrying `aforge dev` names nothing: not the commit,
// not the day, not the machine, and the first reply to it is always a question.
// `make build` stamps the revision and the moment; the toolchain and the
// platform are always there.
func TestVersionNamesTheBuildAndTheMachineItWasBuiltFor(t *testing.T) {
	printed := versionString()
	for _, want := range []string{"aforge ", runtime.Version(), runtime.GOOS + "/" + runtime.GOARCH} {
		if !strings.Contains(printed, want) {
			t.Errorf("`aforge version` said %q, which does not carry %q", printed, want)
		}
	}
	if strings.TrimSpace(printed) == "aforge dev" {
		t.Fatalf("`aforge version` said %q and nothing else — a bug report carrying it names nothing", printed)
	}
	// AND AN UNSTAMPED BUILD SAYS IT IS UNSTAMPED. Under `go test` there is no
	// linker stamp, so this is the branch that runs here, and the bare word
	// `dev` reads like a release name rather than the absence it is.
	if !strings.Contains(printed, "no revision stamped") {
		t.Skipf("this binary carries a stamped revision (%q), so there is no absence to check here", printed)
	}
	if !strings.Contains(printed, "make build") {
		t.Errorf("%q says the revision is missing without naming what puts it there", printed)
	}
}

// Row 26. One verb in the binary answered `--help` differently from the other
// twenty-two: it printed the page list, so the gesture stopped meaning "how do
// I call this". The usage goes on top; the list is still under it, so nothing
// is lost.
func TestManualHelpPrintsTheCommandsUsageAboveThePageList(t *testing.T) {
	for _, spelling := range []string{"-h", "--help"} {
		t.Run(spelling, func(t *testing.T) {
			out, _ := captureUsage(t)
			if err := runManualWith([]string{spelling}, os.Stdout); err != nil && err != exitHelped {
				t.Fatalf("aforge manual %s: %v", spelling, err)
			}
			printed := out.String()
			usage := strings.Index(printed, "aforge manual <page>")
			listing := strings.Index(printed, "starting-aforge")
			if usage < 0 {
				t.Fatalf("aforge manual %s printed no usage line:\n%s", spelling, printed)
			}
			if listing < 0 {
				t.Fatalf("aforge manual %s stopped printing the page list:\n%s", spelling, printed)
			}
			if usage > listing {
				t.Fatalf("aforge manual %s printed the page list above its usage:\n%s", spelling, printed)
			}
		})
	}
}
