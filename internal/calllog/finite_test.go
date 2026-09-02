package calllog

import (
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
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
	unspellable = sync.Once{}
	t.Cleanup(func() {
		stderr = previousStderr
		marshalJSON = previousMarshal
		unspellable = sync.Once{}
	})
	return &said
}

// This is the issue's own replication (#334). It passed before the fix, which
// was the defect: the +Inf row was dropped and nothing said so.
func TestAnInfiniteFieldStillLeavesItsRow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "calls.jsonl")
	t.Setenv(EnvVar, path)
	fresh(t, path)
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

func TestANumberJSONCannotSpellIsTakenOffTheRowAndSaidInWords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "calls.jsonl")
	fresh(t, path)

	rows := []struct {
		id     string
		record Record
		says   string
	}{
		{"inf", Record{CostS: math.Inf(1)}, "cost_s was +Inf and is not on this row."},
		{"neg", Record{WaitS: math.Inf(-1)}, "wait_s was -Inf and is not on this row."},
		{"nan", Record{Cost: math.NaN()}, "cost was NaN and is not on this row."},
		{"waste", Record{WasteUSD: math.NaN()}, "waste_usd was NaN and is not on this row."},
	}
	for _, row := range rows {
		Append(Record{
			Time: "2026-09-02T09:00:00.000Z", ID: row.id, Model: "m", Status: 200,
			Finish: "stop", CompletionTokens: 466, Cost: row.record.Cost,
			CostS: row.record.CostS, WaitS: row.record.WaitS, WasteUSD: row.record.WasteUSD,
		})
	}

	records := readLines(t, path)
	if len(records) != len(rows) {
		t.Fatalf("every row is written whatever it carries: %d of %d", len(records), len(rows))
	}
	for index, record := range records {
		if record.Note != rows[index].says {
			t.Errorf("row %d should say what went missing: %q, want %q", index, record.Note, rows[index].says)
		}
		// The rest of the row is what the log exists for, and it survives whole.
		if record.Status != 200 || record.Finish != "stop" || record.CompletionTokens != 466 {
			t.Errorf("row %d lost the facts around the number: %+v", index, record)
		}
	}
}

func TestSeveralNonFiniteNumbersAreAllNamedAfterWhateverTheCallAlreadyNoted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "calls.jsonl")
	fresh(t, path)
	Append(Record{
		Time: "2026-09-02T09:00:00.000Z", Model: "m", Status: 200,
		Note:  "pinned lane coreweave was silent for 10s.",
		WaitS: 12.5, CostS: math.Inf(1), WasteUSD: math.NaN(),
	})

	records := readLines(t, path)
	if len(records) != 1 {
		t.Fatalf("one row: %d", len(records))
	}
	note := records[0].Note
	if !strings.HasPrefix(note, "pinned lane coreweave was silent for 10s.") {
		t.Errorf("the call's own sentence comes first and is kept: %q", note)
	}
	for _, want := range []string{"waste_usd was NaN", "cost_s was +Inf"} {
		if !strings.Contains(note, want) {
			t.Errorf("the note should name %s: %q", want, note)
		}
	}
	if records[0].WaitS != 12.5 {
		t.Errorf("a number JSON can spell stays on the row: %v", records[0].WaitS)
	}
	if records[0].CostS != 0 || records[0].WasteUSD != 0 {
		t.Errorf("the numbers it cannot spell are off the row: %+v", records[0])
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
