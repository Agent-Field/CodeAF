package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixture is a small, hand-written log with one row of each shape the census
// has to tell apart, so that every reading below is checked against a file
// somebody can read rather than against a number somebody remembered.
const fixture = "testdata/calls.jsonl"

func censusOf(t *testing.T, look settings) string {
	t.Helper()
	rows, err := readLog(fixture)
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}
	var out bytes.Buffer
	report(&out, fixture, rows, look)
	return out.String()
}

func defaults() settings { return settings{top: 25, days: 3, min: 2, longest: 10} }

func TestTheCensusTellsAFailureInsideAnOpenedStreamFromASuccess(t *testing.T) {
	rows, err := readLog(fixture)
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}
	counted := map[statusClass]int{}
	for _, r := range rows {
		if r.finished() {
			counted[r.statusClass()]++
		}
	}
	// Four of the fixture's 200s carry an error and are failures whatever the
	// status column says; six are clean answers.
	if counted[classInStream] != 4 {
		t.Errorf("200-with-an-error rows: got %d, want 4", counted[classInStream])
	}
	if counted[classClean] != 6 {
		t.Errorf("clean 200s: got %d, want 6", counted[classClean])
	}
	if counted[classPaced] != 3 || counted[classRouting] != 1 || counted[classMalformed] != 1 {
		t.Errorf("statuses: 429 %d, 404 %d, 400 %d", counted[classPaced], counted[classRouting], counted[classMalformed])
	}
	if counted[classTransport] != 1 {
		t.Errorf("transport failures: got %d, want 1", counted[classTransport])
	}
}

func TestACauseIsReadFromWhatTheFailureSaysAndNotFromItsStatus(t *testing.T) {
	rows, _ := readLog(fixture)
	counted := map[causeFamily]int{}
	for _, r := range rows {
		if family, failed := r.cause(); failed {
			counted[family]++
		}
	}
	for _, want := range []struct {
		family causeFamily
		n      int
	}{
		{causeDeadline, 1},  // a live stream guillotined at ninety seconds
		{causeCanceled, 1},  // a hedge loser
		{causePaced, 3},     // the three 429s, one of them inside no stream at all
		{causeNetwork, 1},   // DNS on this laptop
		{causeRouting, 1},   // the account-policy 404
		{causeUpstream, 1},  // a 504 delivered inside an opened 200
		{causeMalformed, 1}, // our own bytes refused
		{causeWall, 1},      // this build's own five-minute cut
	} {
		if counted[want.family] != want.n {
			t.Errorf("%s: got %d, want %d", want.family, counted[want.family], want.n)
		}
	}
	if counted[causeUnread] != 0 {
		t.Errorf("%d failures were not classified at all", counted[causeUnread])
	}
}

func TestOneSignatureCoversEveryMachineThatSpokeIt(t *testing.T) {
	rows, _ := readLog(fixture)
	var paced []string
	for _, r := range rows {
		if r.Status == 429 {
			paced = append(paced, r.signature())
		}
	}
	if len(paced) != 3 {
		t.Fatalf("the fixture should hold three paced refusals, got %d", len(paced))
	}
	for _, got := range paced {
		if got != paced[0] {
			t.Fatalf("two spellings of one refusal:\n %q\n %q", paced[0], got)
		}
	}
	// The machine, the model and the numbers are out; the status stays, because
	// it is the digit that says which failure this is.
	if !strings.Contains(paced[0], "API error (429)") {
		t.Errorf("the router's own status is missing from the signature: %q", paced[0])
	}
	for _, gone := range []string{"CoreWeave", "deepseek/deepseek-v4-flash-0731"} {
		if strings.Contains(paced[0], gone) {
			t.Errorf("%q should have been normalised out of %q", gone, paced[0])
		}
	}
	// And a failure that arrived inside an opened stream says so, so it is never
	// folded in with the same sentence arriving before the headers.
	for _, r := range rows {
		if strings.Contains(r.Error, "context deadline exceeded") {
			if !strings.HasPrefix(r.signature(), "IN-STREAM@200 ") {
				t.Errorf("an in-stream failure lost its marking: %q", r.signature())
			}
		}
	}
}

func TestAChainIsRebuiltFromRisingAttemptsAndKnowsWhetherItMoved(t *testing.T) {
	rows, _ := readLog(fixture)
	many := multiAttempt(chainsIn(rows))
	if len(many) != 1 {
		t.Fatalf("the fixture holds one multi-attempt chain, got %d", len(many))
	}
	held := many[0]
	if len(held.rows) != 4 {
		t.Errorf("the chain's attempts: got %d, want 4", len(held.rows))
	}
	if held.stayedPut() {
		t.Error("the chain moved to DeepInfra on its fourth attempt and is not a chain that stayed put")
	}
	if got := held.machines(); len(got) != 2 || got[0] != "CoreWeave" || got[1] != "DeepInfra" {
		t.Errorf("the machines it walked: %v", got)
	}
	if held.outcome() != classClean {
		t.Errorf("it ended in a clean answer, not %s", held.outcome())
	}
}

// A chain is a question, and two attempts a quarter of an hour apart are two
// questions. The gap is what stops a census from reporting a person who came
// back after lunch as a retry.
func TestAGapWiderThanTheWindowStartsANewChain(t *testing.T) {
	log := writeLines(t,
		`{"ts":"2026-09-08T10:00:00.000Z","id":"1","tag":"turn","model":"m/one","attempt":1,"status":429,"error":"API error (429): rate limited"}`,
		`{"ts":"2026-09-08T10:40:00.000Z","id":"2","tag":"turn","model":"m/one","attempt":2,"status":429,"error":"API error (429): rate limited"}`,
	)
	rows, err := readLog(log)
	if err != nil {
		t.Fatal(err)
	}
	if many := multiAttempt(chainsIn(rows)); len(many) != 0 {
		t.Errorf("forty minutes apart is two questions, not a chain of %d", len(many[0].rows))
	}
}

func TestLaneHealthIsKeyedOnTheMachineThatServedAndNeverOnTheOneAskedFor(t *testing.T) {
	got := censusOf(t, defaults())
	// The fixture has three rows asking for Relace and one of them served by
	// Wafer. A table keyed on the demand would say Relace answered three times.
	if !strings.Contains(got, "| Wafer |") {
		t.Errorf("the machine that served is missing from lane health:\n%s", got)
	}
	if strings.Contains(got, "| Relace | 3 |") {
		t.Errorf("lane health was keyed on the machine that was asked for:\n%s", got)
	}
}

func TestTheChecksCountEveryFindingTheFirstCensusMade(t *testing.T) {
	got := censusOf(t, defaults())
	for _, want := range []string{
		"the machine asked for is not the machine that served",
		"a failed attempt that recorded no cost",
		"started and never finished",
		"carried no tag",
		"a wait or a cost no second could hold",
		"a row JSON could not spell and lost",
		"ran past twice the hazard ceiling it was armed with",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the checks are missing %q:\n%s", want, got)
		}
	}
	// The fixture carries one 1.99e+146 `cost_s` and one row whose NaN JSON
	// could not spell at all, and the census must see both.
	if !strings.Contains(got, "| a wait or a cost no second could hold | 1 | ") {
		t.Errorf("the absurd cost_s was not counted:\n%s", got)
	}
	if !strings.Contains(got, "| a row JSON could not spell and lost | 1 | ") {
		t.Errorf("the unspellable row was not counted:\n%s", got)
	}
	if !strings.Contains(got, "| started and never finished | 1 | ") {
		t.Errorf("the orphaned start row was not counted:\n%s", got)
	}
}

// The one field this wave renamed is on ten days of already-written rows under
// its old name, and a census that could not read those would lose the window it
// exists to measure.
func TestTheHazardCeilingIsReadUnderBothOfItsSpellings(t *testing.T) {
	log := writeLines(t,
		`{"ts":"2026-09-08T10:00:00.000Z","id":"1","model":"m/one","status":200,"ms":50000,"deadline_ms":10000}`,
		`{"ts":"2026-09-08T10:01:00.000Z","id":"2","model":"m/one","status":200,"ms":50000,"hazard_ceiling_ms":10000}`,
	)
	rows, err := readLog(log)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.hazardCeiling() != 10000 {
			t.Errorf("row %s read its hazard ceiling as %d", r.ID, r.hazardCeiling())
		}
	}
}

// A census run against a log nothing has been written to says so in a sentence
// and invents no zeroes to fill a table with — the emptiness law, in a file.
func TestAnEmptyLogDrawsNoTable(t *testing.T) {
	log := writeLines(t)
	rows, err := readLog(log)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	report(&out, log, rows, defaults())
	if strings.Contains(out.String(), "| --- |") {
		t.Errorf("an empty log drew a table:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "Nothing has been written to this log yet.") {
		t.Errorf("an empty log did not say so:\n%s", out.String())
	}
}

// A log being appended to by a live process can end mid-line, and an instrument
// that refused to run over one would be an instrument nobody could use while
// the thing it measures is running.
func TestATornLastLineDoesNotStopTheCensus(t *testing.T) {
	log := filepath.Join(t.TempDir(), "calls.jsonl")
	whole := `{"ts":"2026-09-08T10:00:00.000Z","id":"1","model":"m/one","status":200,"ms":100}`
	if err := os.WriteFile(log, []byte(whole+"\n{\"ts\":\"2026-09-08T10:01"), 0o644); err != nil {
		t.Fatal(err)
	}
	rows, err := readLog(log)
	if err != nil {
		t.Fatalf("a torn line ended the census: %v", err)
	}
	if len(rows) != 1 {
		t.Errorf("rows kept: got %d, want 1", len(rows))
	}
}

func writeLines(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "calls.jsonl")
	body := ""
	if len(lines) > 0 {
		body = strings.Join(lines, "\n") + "\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
