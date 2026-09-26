package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/store"
	"github.com/Agent-Field/codeaf/internal/watchdog"
)

// THE EMPTINESS LAW ON THE LAST LINE OF A HEADLESS RUN.
//
// A run that never got started ended `0s · 0 nodes · $0.0000` — three claims
// nobody earned, on the one line a person reads to find out what happened, and
// printed directly under the sentence saying it did not run.
func TestTheHeadlessFooterLeavesOutWhatIsZero(t *testing.T) {
	var out, errs bytes.Buffer
	request := doRequest{stdout: &out, stderr: &errs}
	_ = reportErrand(request, headlessOutcome{
		Deliverable: "nothing ran", Artifacts: []string{}, stop: stopError,
	})
	printed := out.String()
	for _, claim := range []string{"0s", "0 nodes", "$0.0000", "$0.00"} {
		if strings.Contains(printed, claim) {
			t.Fatalf("a run that measured nothing still printed %q:\n%s", claim, printed)
		}
	}
	if strings.HasSuffix(printed, "\n\n") {
		t.Fatalf("the footer went, and left its separator behind:\n%q", printed)
	}

	// And a run that did measure something says all three.
	out.Reset()
	_ = reportErrand(request, headlessOutcome{
		Deliverable: "done", Artifacts: []string{}, Seconds: 12, Nodes: 3, Spend: 0.4213,
	})
	printed = out.String()
	for _, want := range []string{"12s", "3 nodes", "$0.42"} {
		if !strings.Contains(printed, want) {
			t.Fatalf("the footer dropped %q, which this run really did:\n%s", want, printed)
		}
	}
}

// The footer's spend is written by the one helper that owns how a spend is
// written, so the receipt and every figure beside it agree — cents, and four
// decimals under a cent, and nothing at all for zero.
func TestTheFooterWritesASpendTheWayEverythingElseDoes(t *testing.T) {
	for _, row := range []struct {
		spend float64
		want  string
	}{
		{spend: 0, want: ""},
		// A real spend too small for four places is a floor, never four zeros.
		{spend: 0.00002, want: "<$0.0001"},
		{spend: 0.0005688764200000001, want: "$0.0006"},
		{spend: 3.4, want: "$3.40"},
	} {
		if got := config.SpentFigure(row.spend); got != row.want {
			t.Fatalf("a spend of %v is written %q, want %q", row.spend, got, row.want)
		}
	}
}

// `codeaf doctor` is what somebody runs when nothing works, and on a fresh
// machine it read as a machine that had measured zero rather than one that had
// not measured: `$0.00 today · rail $500.00` and `0 active charters · 0 pending
// questions`.
func TestDoctorDoesNotPrintAZeroItNeverMeasured(t *testing.T) {
	path := filepath.Join(t.TempDir(), "graph.db")
	graph, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.Close(); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := runDoctorWith([]string{"--db", path}, &output, 500, fakeDoctorWatch{status: watchdog.Status{}}); err != nil {
		t.Fatal(err)
	}
	printed := output.String()
	for _, claim := range []string{"$0.00", "0 active", "0 pending"} {
		if strings.Contains(printed, claim) {
			t.Fatalf("doctor printed %q on a machine that has not measured anything:\n%s", claim, printed)
		}
	}
	// The rail is a figure somebody chose, so it stays.
	if !strings.Contains(printed, "rail $500.00") {
		t.Fatalf("doctor stopped naming the daily rail, which is a real limit:\n%s", printed)
	}
}

// The same law on the notebook's last line: `today's spend: $0.00 of $500.00
// daily rail` is a measurement nobody made.
func TestTheNotebookDoesNotPrintAZeroSpend(t *testing.T) {
	path := filepath.Join(t.TempDir(), "graph.db")
	graph, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.Close(); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := runNotebookTo([]string{"--db", path}, &output, time.Now()); err != nil {
		t.Fatal(err)
	}
	printed := output.String()
	if strings.Contains(printed, "$0.00") {
		t.Fatalf("the notebook printed a spend of nothing:\n%s", printed)
	}
	if !strings.Contains(printed, "daily rail") {
		t.Fatalf("the notebook stopped naming the daily rail:\n%s", printed)
	}
}
