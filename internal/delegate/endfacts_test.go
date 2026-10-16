package delegate

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var endAt = time.Date(2026, 10, 16, 9, 30, 0, 0, time.UTC)

// terminalOf sends an ending through the emitter and the reader — the program's
// real wire — and returns the terminal the host receives.
func terminalOf(t *testing.T, end Ending) Terminal {
	t.Helper()
	var wire bytes.Buffer
	if err := NewEmitter(&wire).Terminal(end); err != nil {
		t.Fatal(err)
	}
	reading, err := Read(&wire, &recorder{})
	if err != nil || reading.Terminal == nil {
		t.Fatalf("the terminal did not read back: %+v, %v", reading, err)
	}
	return *reading.Terminal
}

// THE ENDING'S WIRE IS PINNED: each program's real ending, through the emitter
// and the reader, becomes one `end` line in the file and one Shown on the page,
// and both are compared to the bytes in testdata. A change to either shape is a
// change to what an old build and a new one say to each other.
func TestTheEndLineAndTheShownEndingAreGoldenOnTheWire(t *testing.T) {
	cases := []struct {
		name string
		end  Ending
	}{
		{"review", Ending{Status: StatusPass, Message: "7 findings, 2 blocking the merge", CostUSD: 1.5,
			Deliverable: "the whole account", Extra: map[string]any{"partial": true, "pull_request": "acme/api#1511",
				"reports": []string{"review-report.md", "review-report.json"}, "findings": 7, "blocking": 2,
				"sessions": 3, "calls": 61, "cut_at": "cross-checking"}}},
		{"sec", Ending{Status: StatusBudget, Message: "3 problems", Extra: map[string]any{
			"status": "budget", "reports": []string{"sec-report.md", "sec-report.sarif", "sec-report.json"}, "sessions": 9, "calls": 61,
			"confirmed": 2, "likely": 1, "inconclusive": 4, "ruled_out": 5,
			"analysis_incomplete": "the run's dollar ceiling stopped the analysis",
			"hunt_coverage":       map[string]any{"attempted": 4, "failures": []map[string]string{{"strategy": "injection", "reason": "session failed"}}}}}},
		{"checks", Ending{Status: StatusPass, Message: "submitted", Claim: "the retry loop is bounded", Observed: "3 of 3 commands passed",
			Extra: map[string]any{"submitted": true}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := AppendAction(dir, EndAction(endAt, terminalOf(t, tc.end))); err != nil {
				t.Fatal(err)
			}
			line, err := os.ReadFile(filepath.Join(dir, ActionsFile))
			if err != nil {
				t.Fatal(err)
			}
			logged, _ := ReadActions(dir, 0)
			shown, ok := Delegate{Name: tc.name}.Reader()(logged[0])
			if !ok {
				t.Fatal("the ending was not shown")
			}
			page, _ := json.Marshal(shown)
			got := string(line) + string(page) + "\n"
			golden := filepath.Join("testdata", "end-"+tc.name+".golden")
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatal(err)
			}
			if got != string(want) {
				t.Fatalf("the wire changed.\n got: %s\nwant: %s", got, want)
			}
		})
	}
}

// A REAL SENIOR-DEV RUN KEEPS ITS OWN CLAIM AND CHECK, raw: the line carries
// submission_reason, the inner status and the verification counts exactly as the
// program wrote them, and the terminal's own mappers read the same sentences
// from them. Nothing is made up from prose, and the run's status stays the
// protocol's.
func TestARecordedSeniorDevEndingKeepsItsClaimAndItsCheck(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "senior-dev-stream.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	reading, err := Read(bytes.NewReader(raw), &recorder{})
	if err != nil || reading.Terminal == nil {
		t.Fatal(reading, err)
	}
	action := EndAction(endAt, *reading.Terminal)
	if action.Status != StatusPass || action.Message != "submitted and verified: tests pass" {
		t.Fatalf("the old fields moved: %+v", action)
	}
	want := `{"submitted":true,"submission_reason":"tests pass","status":"pass","verification_failing":0,"verification_commands":1}`
	if string(action.Data) != want {
		t.Fatalf("data = %s, want %s", action.Data, want)
	}
	facts := action.Ending()
	if facts == nil || facts.Claim != "" || facts.Observed != "" || facts.SubmissionReason != reading.Terminal.Claim() ||
		facts.InnerStatus != reading.Terminal.Verdict() || facts.VerificationFailing == nil || *facts.VerificationFailing != 0 ||
		facts.VerificationCommands == nil || *facts.VerificationCommands != 1 || facts.Submitted == nil || !*facts.Submitted {
		t.Fatalf("facts = %+v", facts)
	}
}

// THE INNER STATUS IS NOT THE RUN'S: a handed-in change whose own check failed
// ends `done` on the line, and the inner `fail` and its counts ride beside it.
func TestASubmittedRunWhoseCheckFailedStillEndsDoneAndKeepsTheCheck(t *testing.T) {
	terminal := Terminal{Status: StatusPass, Message: "submitted, check failed", Data: map[string]json.RawMessage{
		"submitted": json.RawMessage(`true`), "status": json.RawMessage(`"fail"`), "submission_reason": json.RawMessage(`"fixed it"`),
		"verification_failing": json.RawMessage(`2`), "verification_commands": json.RawMessage(`3`)}}
	action := EndAction(endAt, terminal)
	facts := action.Ending()
	if action.Status != StatusPass || facts == nil || facts.InnerStatus != "fail" || *facts.VerificationFailing != 2 || *facts.VerificationCommands != 3 {
		t.Fatalf("action = %+v facts = %+v", action, facts)
	}
}

// A COUNT THAT WAS NOT SAID IS NOT ZERO, and JSON null is not saying it.
func TestNullAndMalformedFactsAreAbsentNotZero(t *testing.T) {
	data := map[string]json.RawMessage{
		"submitted": json.RawMessage(`null`), "partial": json.RawMessage(` null `), "findings": json.RawMessage(`null`),
		"blocking": json.RawMessage(`null`), "verification_failing": json.RawMessage(`null`), "verification_commands": json.RawMessage(`"3"`),
		"claim": json.RawMessage(`null`), "status": json.RawMessage(`null`), "confirmed": json.RawMessage(`1.5`)}
	if action := EndAction(endAt, Terminal{Status: StatusPass, Data: data}); len(action.Data) != 0 || action.Ending() != nil {
		t.Fatalf("a fact was invented: data = %s", action.Data)
	}
	// A count that is really zero stays a known zero beside the nulls.
	data["findings"] = json.RawMessage(`0`)
	facts := EndAction(endAt, Terminal{Status: StatusPass, Data: data}).Ending()
	if facts == nil || facts.Findings == nil || *facts.Findings != 0 || facts.Blocking != nil || facts.Submitted != nil || facts.VerificationFailing != nil {
		t.Fatalf("facts = %+v", facts)
	}
}

// A LIST WITH AN ELEMENT REMOVED IS A DIFFERENT LIST: a null or non-string
// report name drops the whole list, and a coverage with a null or half-empty
// failure row drops the whole account, so no failed hunt is invented.
func TestMalformedListsAndCoverageRowsAreDroppedWhole(t *testing.T) {
	for name, raw := range map[string]string{
		"null element": `[null,"a"]`,
		"null last":    `["a",null]`,
		"number":       `["a",1]`,
		"not a list":   `"a"`,
		"object":       `{"0":"a"}`,
	} {
		if facts := EndAction(endAt, Terminal{Status: StatusPass, Data: map[string]json.RawMessage{"reports": json.RawMessage(raw), "findings": json.RawMessage(`1`)}}).Ending(); facts == nil || facts.Reports != nil {
			t.Errorf("reports %s: facts = %+v", name, facts)
		}
	}
	for name, raw := range map[string]string{
		"null row":       `{"attempted":3,"failures":[null]}`,
		"null strategy":  `{"attempted":3,"failures":[{"strategy":null,"reason":"x"}]}`,
		"missing reason": `{"attempted":3,"failures":[{"strategy":"injection"}]}`,
		"null reason":    `{"attempted":3,"failures":[{"strategy":"injection","reason":null}]}`,
		"good then null": `{"attempted":3,"failures":[{"strategy":"a","reason":"b"},null]}`,
		"null attempted": `{"attempted":null,"failures":[]}`,
		"fractional":     `{"attempted":1.5}`,
		"null whole":     `null`,
	} {
		if facts := EndAction(endAt, Terminal{Status: StatusPass, Data: map[string]json.RawMessage{"hunt_coverage": json.RawMessage(raw), "findings": json.RawMessage(`1`)}}).Ending(); facts == nil || facts.HuntCoverage != nil {
			t.Errorf("coverage %s: facts = %+v", name, facts)
		}
	}
	good := EndAction(endAt, Terminal{Status: StatusPass, Data: map[string]json.RawMessage{
		"reports": json.RawMessage(`["a.md","b.json"]`), "hunt_coverage": json.RawMessage(`{"attempted":2,"failures":[{"strategy":"a","reason":"b"}]}`)}}).Ending()
	if good == nil || len(good.Reports) != 2 || good.HuntCoverage == nil || good.HuntCoverage.Attempted == nil || *good.HuntCoverage.Attempted != 2 || len(good.HuntCoverage.Failures) != 1 {
		t.Fatalf("good = %+v", good)
	}
}

// AN ENDING WITH NOTHING ON THE LIST IS THE LINE IT ALWAYS WAS, byte for byte.
func TestAnEndingWithNothingToKeepIsTheOldLine(t *testing.T) {
	dir := t.TempDir()
	terminal := Terminal{Status: StatusFail, Message: "it did not finish", Data: map[string]json.RawMessage{
		"cost_usd": json.RawMessage(`0.5`), "reason": json.RawMessage(`"x"`), "deliverable": json.RawMessage(`"long"`)}}
	if err := AppendAction(dir, EndAction(endAt, terminal)); err != nil {
		t.Fatal(err)
	}
	line, _ := os.ReadFile(filepath.Join(dir, ActionsFile))
	want := `{"at":"2026-10-16T09:30:00Z","kind":"end","status":"fail","message":"it did not finish"}` + "\n"
	if string(line) != want {
		t.Fatalf("line = %s, want %s", line, want)
	}
}

// ONLY THE LIST IS KEPT, AND ONLY IN SHAPE. Unknown keys — a transcript, a
// token, a path — never reach the file; a key of the wrong shape is dropped, not
// coerced; a value that is too large is not even decoded.
func TestTheEndingKeepsNothingOffTheListAndNothingOfTheWrongShape(t *testing.T) {
	big := `"` + strings.Repeat("x", endValueCap) + `"`
	data := map[string]json.RawMessage{
		"claim":               json.RawMessage(`"done"`),
		"observed":            json.RawMessage(`{"nested":"object"}`),
		"submitted":           json.RawMessage(`"yes"`),
		"reports":             json.RawMessage(`["a.md", 7]`),
		"findings":            json.RawMessage(`-1`),
		"blocking":            json.RawMessage(`2.5`),
		"confirmed":           json.RawMessage(`"3"`),
		"likely":              json.RawMessage(`true`),
		"inconclusive":        json.RawMessage(`4`),
		"ruled_out":           json.RawMessage(`0`),
		"partial":             json.RawMessage(`1`),
		"analysis_incomplete": json.RawMessage(big),
		"hunt_coverage":       json.RawMessage(`[1]`),
		"transcript":          json.RawMessage(`"SECRET-TRANSCRIPT"`),
		"token":               json.RawMessage(`"sk-live-SECRET"`),
		"deliverable":         json.RawMessage(`"SECRET-DELIVERABLE"`),
		"cost_usd":            json.RawMessage(`9`),
	}
	action := EndAction(endAt, Terminal{Status: StatusPass, Message: "m", Data: data})
	if string(action.Data) != `{"claim":"done","inconclusive":4,"ruled_out":0}` {
		t.Fatalf("data = %s", action.Data)
	}
	for _, leak := range []string{"SECRET", "nested", "cost_usd", "deliverable"} {
		if strings.Contains(string(action.Data), leak) {
			t.Fatalf("%q leaked into %s", leak, action.Data)
		}
	}
	// A zero count is a count, and an absent one is nothing.
	if facts := action.Ending(); facts.RuledOut == nil || *facts.RuledOut != 0 || facts.Confirmed != nil {
		t.Fatalf("facts = %+v", facts)
	}
}

// DEPTH, SIZE AND COUNT ARE BOUNDED: a value ten thousand levels deep under a
// key on the list, under a key off it, and a list or text far past its bounds
// each end as a bounded line, and nothing panics or hangs.
func TestAHostileEndingIsBoundedAndNeverPanics(t *testing.T) {
	deep := strings.Repeat("[", 10000) + strings.Repeat("]", 10000)
	deepObject := strings.Repeat(`{"a":`, 10000) + `1` + strings.Repeat("}", 10000)
	var failures []string
	for i := 0; i < 20; i++ {
		failures = append(failures, `{"strategy":"`+strings.Repeat("s", 100)+`","reason":"`+strings.Repeat("é", 300)+`"}`)
	}
	var names []string
	for i := 0; i < 100; i++ {
		names = append(names, `"`+strings.Repeat("r", 1000)+`"`)
	}
	data := map[string]json.RawMessage{
		"claim":         json.RawMessage(deep),
		"reports":       json.RawMessage(deepObject),
		"hunt_coverage": json.RawMessage(`{"attempted":100,"failures":[` + strings.Join(failures, ",") + `],"extra":` + strings.Repeat("[", 4000) + strings.Repeat("]", 4000) + `}`),
		"unknown":       json.RawMessage(deep),
	}
	action := EndAction(endAt, Terminal{Status: StatusPass, Data: data})
	if len(action.Data) > EndDataCap {
		t.Fatalf("data is %d bytes", len(action.Data))
	}
	if facts := action.Ending(); facts == nil || facts.HuntCoverage == nil || len(facts.HuntCoverage.Failures) != endFailuresMax ||
		len(facts.HuntCoverage.Failures[0].Reason) > endReasonCap || len(facts.HuntCoverage.Failures[0].Strategy) > labelCap {
		t.Fatalf("facts = %+v", facts)
	}
	// The largest the copy can be, with text that doubles when it is escaped.
	loud := strings.Repeat("\x01", endTextCap)
	worst := map[string]json.RawMessage{
		"claim": mustJSON(t, loud), "observed": mustJSON(t, loud), "analysis_incomplete": mustJSON(t, loud),
		"reports":       json.RawMessage(`[` + strings.Join(names, ",") + `]`),
		"hunt_coverage": json.RawMessage(`{"attempted":1,"failures":[` + strings.Join(failures, ",") + `]}`),
	}
	if worst := EndAction(endAt, Terminal{Status: StatusPass, Data: worst}); len(worst.Data) > EndDataCap || len(worst.Data) == 0 {
		t.Fatalf("the worst case wrote %d bytes", len(worst.Data))
	}
	// A line written by hand reaches the file through the same list.
	dir := t.TempDir()
	if err := AppendAction(dir, Action{At: endAt, Kind: ActionEnd, Status: "pass", Data: json.RawMessage(`{"findings":2,"secret":"x","claim":"` + strings.Repeat("c", 9000) + `"}`)}); err != nil {
		t.Fatal(err)
	}
	logged, _ := ReadActions(dir, 0)
	if len(logged[0].Data) > EndDataCap || strings.Contains(string(logged[0].Data), "secret") {
		t.Fatalf("a hand-built line kept %s", logged[0].Data)
	}
	// And a log edited after it was written is read through the list again.
	tampered := Action{At: endAt, Kind: ActionEnd, Message: "m", Data: json.RawMessage(`{"findings":2,"secret":"x"}`)}
	shown, ok := Delegate{Name: "x"}.Reader()(tampered)
	if !ok || shown.Ending == nil || *shown.Ending.Findings != 2 {
		t.Fatalf("shown = %+v", shown)
	}
	if page, _ := json.Marshal(shown); strings.Contains(string(page), "secret") {
		t.Fatalf("the page carries %s", page)
	}
}

// THE OTHER LINES ARE UNTOUCHED: a stage keeps its data under the stage cap, a
// step has no ending, and a stage line that happens to carry the list's keys
// shows no ending facts.
func TestOnlyTheEndingLineCarriesEndingFacts(t *testing.T) {
	stage := StageAction(endAt, StageRecord{Stage: "report", Status: "running", Data: []byte(`{"findings":3}`)})
	shown, ok := Delegate{Name: "x"}.Reader()(stage)
	if !ok || shown.Ending != nil || string(stage.capped().Data) != `{"findings":3}` {
		t.Fatalf("stage shown %+v, data %s", shown, stage.capped().Data)
	}
	if (Action{Kind: ActionStep, Data: json.RawMessage(`{"findings":3}`)}).Ending() != nil {
		t.Fatal("a step has an ending")
	}
}

func mustJSON(t *testing.T, s string) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// THE CAP IS THE FILE'S: a text of < > & is six bytes each once the log's own
// marshal escapes it, and the line on disk must still fit and read back.
func TestTheEndLineOnDiskHoldsTheCapAndReadsBack(t *testing.T) {
	loud := mustJSON(t, strings.Repeat("&", endTextCap))
	dir := t.TempDir()
	terminal := Terminal{Status: StatusPass, Message: "m", Data: map[string]json.RawMessage{
		"claim": loud, "observed": loud, "submission_reason": loud, "analysis_incomplete": loud}}
	if err := AppendAction(dir, EndAction(endAt, terminal)); err != nil {
		t.Fatal(err)
	}
	logged, err := ReadActions(dir, 0)
	if err != nil || len(logged) != 1 || len(logged[0].Data) > EndDataCap || logged[0].Ending() == nil {
		t.Fatalf("read back %d bytes, %+v, %v", len(logged[0].Data), logged[0].Ending(), err)
	}
}

// A COUNT NOBODY SAID IS NOT ZERO, in a coverage as anywhere, and a count that
// is not whole digits is not a count.
func TestCoverageAttemptedUnsaidAndFractionalCountsAreAbsent(t *testing.T) {
	facts := EndAction(endAt, Terminal{Status: StatusPass, Data: map[string]json.RawMessage{
		"hunt_coverage": json.RawMessage(`{"unknown":true}`), "findings": json.RawMessage(`0.99999999999999999`), "blocking": json.RawMessage(`1.0`)}}).Ending()
	if facts == nil || facts.HuntCoverage == nil || facts.HuntCoverage.Attempted != nil || !facts.HuntCoverage.Unknown || facts.Findings != nil || facts.Blocking != nil {
		t.Fatalf("facts = %+v", facts)
	}
}
