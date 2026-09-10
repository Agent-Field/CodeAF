package calllog

import (
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// quiet puts the once-per-process complaint and the marshal seam back the way
// they were found, so a test that exercises either does not spend the process's
// one line on behalf of the tests after it.
func quiet(t *testing.T) *strings.Builder {
	t.Helper()
	var said strings.Builder
	previousStderr, previousMarshal := stderr, marshalJSON
	stderr = &said
	unspellable, unspellableField = sync.Once{}, sync.Once{}
	t.Cleanup(func() {
		stderr = previousStderr
		marshalJSON = previousMarshal
		unspellable, unspellableField = sync.Once{}, sync.Once{}
	})
	return &said
}

// This is the issue's own replication (#334). It passed before the fix, which
// was the defect: the +Inf row was dropped and nothing said so.
func TestAnInfiniteFieldStillLeavesItsRow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "calls.jsonl")
	t.Setenv(EnvVar, path)
	fresh(t, path)
	// The row is kept and the DEFECT goes to stderr; this test is about the
	// row, so the line is caught rather than spent on the test binary's output.
	quiet(t)
	Append(Record{ID: "start", Phase: PhaseStart, Model: "m"})
	Append(Record{ID: "end-inf", Model: "m", Status: 200, CostS: math.Inf(1)})
	Append(Record{ID: "end-ok", Model: "m", Status: 200, CostS: 1})

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the log: %v", err)
	}
	if !strings.Contains(string(data), "end-inf") {
		t.Fatalf("the +Inf row was dropped silently; the log holds:\n%s", data)
	}
	if !strings.Contains(string(data), "end-ok") {
		t.Fatalf("the row after it was lost too; the log holds:\n%s", data)
	}
}

// #334 asked for the row to be kept and the missing figure to be named in the
// row's own `note`. Half of that is now wrong. Nothing in the request path can
// produce one of these figures — [control.Seconds] carries "nothing" and "past
// pricing" as themselves — so a value reaching this repair is a builder defect,
// and under the emptiness law a row with no number carries nothing rather than
// a sentence about a float.
func TestANumberJSONCannotSpellIsTakenOffTheRowAndTheRowStaysWhole(t *testing.T) {
	path := filepath.Join(t.TempDir(), "calls.jsonl")
	fresh(t, path)
	quiet(t)

	rows := []Record{
		{ID: "inf", CostS: math.Inf(1)},
		{ID: "neg", WaitS: math.Inf(-1)},
		{ID: "nan", Cost: math.NaN()},
		{ID: "waste", WasteUSD: math.NaN()},
	}
	for _, row := range rows {
		Append(Record{
			Time: "2026-09-02T09:00:00.000Z", ID: row.ID, Model: "m", Status: 200,
			Finish: "stop", CompletionTokens: 466, Cost: row.Cost,
			CostS: row.CostS, WaitS: row.WaitS, WasteUSD: row.WasteUSD,
		})
	}

	records := readLines(t, path)
	if len(records) != len(rows) {
		t.Fatalf("every row is written whatever it carries: %d of %d", len(records), len(rows))
	}
	for index, record := range records {
		if record.Note != "" {
			t.Errorf("row %d explains a float to somebody who asked about a call: %q", index, record.Note)
		}
		if record.CostS != 0 || record.WaitS != 0 || record.Cost != 0 || record.WasteUSD != 0 {
			t.Errorf("row %d kept a figure JSON cannot write: %+v", index, record)
		}
		// The rest of the row is what the log exists for, and it survives whole.
		if record.Status != 200 || record.Finish != "stop" || record.CompletionTokens != 466 {
			t.Errorf("row %d lost the facts around the number: %+v", index, record)
		}
	}
}

func TestSeveralNonFiniteNumbersLeaveTheCallsOwnSentenceAloneAndAreSaidOnceOnStderr(t *testing.T) {
	path := filepath.Join(t.TempDir(), "calls.jsonl")
	fresh(t, path)
	said := quiet(t)
	for range 3 {
		Append(Record{
			Time: "2026-09-02T09:00:00.000Z", Model: "m", Status: 200,
			Note:  "pinned lane coreweave was silent for 10s.",
			WaitS: 12.5, CostS: math.Inf(1), WasteUSD: math.NaN(),
		})
	}

	records := readLines(t, path)
	if len(records) != 3 {
		t.Fatalf("three rows: %d", len(records))
	}
	if note := records[0].Note; note != "pinned lane coreweave was silent for 10s." {
		t.Errorf("the call's own sentence is the whole note: %q", note)
	}
	if records[0].WaitS != 12.5 {
		t.Errorf("a number JSON can spell stays on the row: %v", records[0].WaitS)
	}
	if records[0].CostS != 0 || records[0].WasteUSD != 0 {
		t.Errorf("the numbers it cannot spell are off the row: %+v", records[0])
	}
	// A DEFECT IS SAID WHERE A DEFECT BELONGS, and once: three rows carrying two
	// such figures each would otherwise be six lines of stderr per second.
	if lines := strings.Count(said.String(), "\n"); lines != 1 {
		t.Fatalf("the complaint is made exactly once; it said:\n%s", said.String())
	}
	// The one line names the FIRST figure the table reached, which is the order
	// [measured] lists them in: a complaint per figure per row would be the
	// noise this Once exists to prevent.
	for _, want := range []string{"waste_usd", "NaN"} {
		if !strings.Contains(said.String(), want) {
			t.Errorf("the complaint should name %s: %q", want, said.String())
		}
	}
}

func TestARecordNoShapeCanWriteIsSaidOnceAndDoesNotSilenceTheLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "calls.jsonl")
	fresh(t, path)
	said := quiet(t)

	// A failure no Record can produce today, so that the branch which gives up
	// on a row is exercised rather than believed.
	marshalJSON = func(any) ([]byte, error) { return nil, errors.New("no shape for this record") }
	for range 3 {
		Append(Record{Time: "2026-09-02T09:00:00.000Z", ID: "unwritable", Model: "m"})
	}
	if lines := strings.Count(said.String(), "\n"); lines != 1 {
		t.Fatalf("a record that cannot be written is said exactly once; it said:\n%s", said.String())
	}
	if !strings.Contains(said.String(), "no shape for this record") {
		t.Errorf("the line should carry what went wrong: %q", said.String())
	}

	marshalJSON = json.Marshal
	Append(Record{Time: "2026-09-02T09:00:01.000Z", ID: "after", Model: "m", Status: 200})
	records := readLines(t, path)
	if len(records) != 1 || records[0].ID != "after" {
		t.Fatalf("the calls after an unwritable record keep their rows: %+v", records)
	}
}

func TestAnUnwritableRecordIsNotRepairedIntoSomethingElse(t *testing.T) {
	// dropNonFinite reports whether it changed anything, because a record that
	// failed for another reason must reach the complaint rather than be
	// marshalled a second time to no purpose.
	record := Record{ID: "plain", CostS: 1}
	if dropNonFinite(&record) {
		t.Fatalf("a record with only finite numbers is left exactly as it came: %+v", record)
	}
	if record.Note != "" {
		t.Errorf("and nothing is written on it: %q", record.Note)
	}
}

// THE TABLE IN finite.go IS THE RECORD'S FLOATS AND NOTHING ELSE. A number
// added to Record and not to the table would be a row dropped again, quietly
// and for the same reason as #334, so the two are checked against each other
// here rather than by eye.
func TestEveryNumberARecordCarriesIsOneTheLogCanRescue(t *testing.T) {
	var record Record
	known := map[string]bool{}
	for _, field := range measured(&record) {
		known[field.name] = true
	}
	shape := reflect.TypeOf(record)
	floats := 0
	for index := range shape.NumField() {
		field := shape.Field(index)
		if field.Type.Kind() != reflect.Float64 {
			continue
		}
		floats++
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if !known[name] {
			t.Errorf("Record.%s (%q) is a number JSON can refuse and finite.go does not know it: a row carrying it is still dropped", field.Name, name)
		}
	}
	if floats != len(known) {
		t.Errorf("the table names %d numbers and the record carries %d", len(known), floats)
	}
}
