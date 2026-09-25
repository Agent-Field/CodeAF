package record

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/pool/judge"
	"github.com/Agent-Field/codeaf/internal/pool/outbox"
	"github.com/Agent-Field/codeaf/internal/pool/tally"
)

// A path with no file behind it is an install that has recorded nothing yet:
// an empty sheet and no error, never a fault.
func TestALoadOfAMissingPathAnswersAnEmptySheet(t *testing.T) {
	sheet, err := LoadSheet(filepath.Join(t.TempDir(), "absent", OwnSheetName))
	if err != nil {
		t.Fatalf("a missing sheet loaded as %v", err)
	}
	if got := Cells(sheet); len(got) != 0 {
		t.Fatalf("a missing sheet answered %d cells, want none", len(got))
	}
}

// Bytes that are there and hold no sheet document are a fault, named.
func TestALoadOfUnparsableBytesIsAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), OwnSheetName)
	if err := os.WriteFile(path, []byte("not a document"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSheet(path); err == nil {
		t.Fatal("unparsable bytes loaded as a sheet")
	}
}

// A sheet saved is the sheet loaded: every cell comes back with the
// observations it held, however many addresses it kept.
func TestASaveThenLoadRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pool", OwnSheetName)
	sheet := tally.New()
	sheet.Observe(Metric, "worker", "a/one", nil, 80)
	sheet.Observe(Metric, "worker", "a/one", nil, 90)
	sheet.Observe(Metric, "high", "b/two", nil, 70)
	sheet.Observe(Metric, "high", "b/two", map[string]string{"quant": "fp8"}, 55)
	sheet.Win("worker", "a/one", "b/two")
	if err := SaveSheet(path, sheet); err != nil {
		t.Fatalf("save: %v", err)
	}
	back, err := LoadSheet(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	for _, want := range []struct {
		role, model string
		dims        map[string]string
		cell        tally.Cell
	}{
		{"worker", "a/one", nil, tally.Cell{N: 2, Sum: 170, SumSq: 14500}},
		{"high", "b/two", nil, tally.Cell{N: 1, Sum: 70, SumSq: 4900}},
		{"high", "b/two", map[string]string{"quant": "fp8"}, tally.Cell{N: 1, Sum: 55, SumSq: 3025}},
	} {
		got, ok := back.Cell(Metric, want.role, want.model, want.dims)
		if !ok || got != want.cell {
			t.Fatalf("the %s/%s cell (%v) came back %+v ok %v, want %+v", want.role, want.model, want.dims, got, ok, want.cell)
		}
	}
	if over, under := back.Wins("worker", "a/one", "b/two"); over != 1 || under != 0 {
		t.Fatalf("the win came back %d over %d under, want 1 over 0", over, under)
	}

	// The file is private, and the save is atomic enough to leave no temp
	// file beside the sheet it wrote.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("the sheet was saved with mode %v, want 0600", info.Mode().Perm())
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != OwnSheetName {
		t.Fatalf("the pool directory holds %d entries, want the sheet alone", len(entries))
	}
}

// A second save over the first leaves the one file, whole: the rename
// replaces the sheet rather than appending to it.
func TestASaveOverAnExistingSheetReplacesIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), OwnSheetName)
	first := tally.New()
	first.Observe(Metric, "worker", "a/one", nil, 80)
	if err := SaveSheet(path, first); err != nil {
		t.Fatal(err)
	}
	second := tally.New()
	second.Observe(Metric, "high", "b/two", nil, 70)
	if err := SaveSheet(path, second); err != nil {
		t.Fatal(err)
	}
	back, err := LoadSheet(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := back.Cell(Metric, "worker", "a/one", nil); ok {
		t.Fatal("the replaced sheet still holds the first save's cell")
	}
	if _, ok := back.Cell(Metric, "high", "b/two", nil); !ok {
		t.Fatal("the replaced sheet does not hold the second save's cell")
	}
}

// Every valid score is observed into the sheet and one row apiece is appended
// to the outbox, each row carrying the day, the judge and the door the run
// came in by.
func TestRecordObservesEveryScoreAndAppendsOneRowApiece(t *testing.T) {
	sheet := tally.New()
	box, err := outbox.Open(filepath.Join(t.TempDir(), "outbox.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer box.Close()
	recorder := &Recorder{Sheet: sheet, Outbox: box}

	scores := []judge.Score{
		{Role: judge.RoleWorker, Model: "a/one", Score: 80},
		{Role: judge.RoleHigh, Model: "b/two", Score: 66},
		{Role: judge.RoleMastermind, Model: "c/three", Score: 91},
	}
	if err := recorder.Record(scores, "z/fourth", "chat", "three", "2026-09-17"); err != nil {
		t.Fatalf("record: %v", err)
	}
	for _, want := range []struct {
		role  judge.Role
		model string
		mean  float64
	}{
		{judge.RoleWorker, "a/one", 80},
		{judge.RoleHigh, "b/two", 66},
		{judge.RoleMastermind, "c/three", 91},
	} {
		cell, ok := sheet.Cell(Metric, string(want.role), want.model, nil)
		if !ok || cell.N != 1 || cell.Mean() != want.mean {
			t.Fatalf("the %s seat observed %+v ok %v, want one observation of %v", want.role, cell, ok, want.mean)
		}
	}
	pending := box.Pending()
	if len(pending) != len(scores) {
		t.Fatalf("%d rows appended, want one per score", len(pending))
	}
	for i, row := range pending {
		var got Row
		if err := json.Unmarshal(row.Payload, &got); err != nil {
			t.Fatalf("row %d is not a row: %v", i, err)
		}
		want := Row{
			Schema: 1, Metric: Metric,
			Role:  string(scores[i].Role),
			Model: scores[i].Model,
			Score: scores[i].Score,
			Judge: "z/fourth", Door: "chat", Size: "three", Day: "2026-09-17",
		}
		if got != want {
			t.Fatalf("row %d is %+v, want %+v", i, got, want)
		}
	}
}

// RowsOf spells every row the same way: schema 1 under the role_quality
// metric, whatever the score.
func TestRowsOfSpellsEveryRowTheSameWay(t *testing.T) {
	rows := RowsOf([]judge.Score{
		{Role: judge.RoleWorker, Model: "a/one", Score: 80, Reason: "it worked"},
	}, "z/fourth", "chat", "solo", "2026-09-17")
	if len(rows) != 1 {
		t.Fatalf("%d rows, want one", len(rows))
	}
	if rows[0].Schema != 1 || rows[0].Metric != Metric || rows[0].Role != "worker" ||
		rows[0].Model != "a/one" || rows[0].Score != 80 || rows[0].Judge != "z/fourth" ||
		rows[0].Door != "chat" || rows[0].Size != "solo" || rows[0].Day != "2026-09-17" {
		t.Fatalf("the row is %+v", rows[0])
	}
	line, err := json.Marshal(rows[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"schema":1`, `"metric":"role_quality"`, `"role":"worker"`, `"day":"2026-09-17"`} {
		if !strings.Contains(string(line), key) {
			t.Fatalf("the row's json is missing %s: %s", key, line)
		}
	}
}

// A score whose seat is not one of the judged seats is skipped and named, and
// the scores beside it are recorded all the same.
func TestABadRoleIsSkippedAndNamedAndTheOthersLand(t *testing.T) {
	for _, bad := range []judge.Score{
		{Role: "planner", Model: "a/one", Score: 80},
		{Role: "", Model: "a/one", Score: 80},
		{Role: judge.RoleWorker, Model: "a/one", Score: 140},
		{Role: judge.RoleWorker, Model: "a/one", Score: -1},
	} {
		sheet := tally.New()
		box, err := outbox.Open(filepath.Join(t.TempDir(), "outbox.jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		recorder := &Recorder{Sheet: sheet, Outbox: box}
		err = recorder.Record([]judge.Score{
			bad,
			{Role: judge.RoleWorker, Model: "a/one", Score: 80},
		}, "z/fourth", "chat", "two", "2026-09-17")
		box.Close()
		if err == nil {
			t.Fatalf("a %s score at %v was recorded without a word", bad.Role, bad.Score)
		}
		if !strings.Contains(err.Error(), "skipped") {
			t.Fatalf("the error does not say what was skipped: %v", err)
		}
		// The good score landed in both records; the bad one in neither.
		if cells := Cells(sheet); len(cells) != 1 {
			t.Fatalf("the sheet holds %d cells, want the good score's one: %+v", len(cells), cells)
		}
		if cell, ok := sheet.Cell(Metric, "worker", "a/one", nil); !ok || cell.N != 1 || cell.Mean() != 80 {
			t.Fatalf("the good score did not land: %+v ok %v", cell, ok)
		}
		if pending := box.Pending(); len(pending) != 1 {
			t.Fatalf("%d rows appended, want the good score's one", len(pending))
		}
	}
}

// An outbox that cannot take a row costs the install nothing of its own
// evidence: every score is observed into the sheet all the same, and the
// first append error is what comes back.
func TestRecordObservesTheSheetWhateverTheOutboxDoes(t *testing.T) {
	sheet := tally.New()
	box, err := outbox.Open(filepath.Join(t.TempDir(), "outbox.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if err := box.Close(); err != nil {
		t.Fatal(err)
	}
	recorder := &Recorder{Sheet: sheet, Outbox: box}
	err = recorder.Record([]judge.Score{
		{Role: judge.RoleWorker, Model: "a/one", Score: 80},
		{Role: judge.RoleHigh, Model: "b/two", Score: 66},
		{Role: judge.RoleMastermind, Model: "c/three", Score: 91},
	}, "z/fourth", "chat", "three", "2026-09-17")
	if err == nil {
		t.Fatal("a closed outbox recorded without a word")
	}
	for _, want := range []struct {
		role  judge.Role
		model string
		mean  float64
	}{
		{judge.RoleWorker, "a/one", 80},
		{judge.RoleHigh, "b/two", 66},
		{judge.RoleMastermind, "c/three", 91},
	} {
		cell, ok := sheet.Cell(Metric, string(want.role), want.model, nil)
		if !ok || cell.N != 1 || cell.Mean() != want.mean {
			t.Fatalf("the %s seat was not observed despite the failed append: %+v ok %v", want.role, cell, ok)
		}
	}
}

// A recorder with no outbox keeps the scores locally only, and says nothing.
func TestANilOutboxRecordsLocallyOnly(t *testing.T) {
	sheet := tally.New()
	recorder := &Recorder{Sheet: sheet}
	err := recorder.Record([]judge.Score{
		{Role: judge.RoleWorker, Model: "a/one", Score: 80},
		{Role: judge.RoleHigh, Model: "b/two", Score: 66},
	}, "z/fourth", "chat", "two", "2026-09-17")
	if err != nil {
		t.Fatalf("a local-only record said %v", err)
	}
	if cell, ok := sheet.Cell(Metric, "worker", "a/one", nil); !ok || cell.N != 1 {
		t.Fatalf("the worker score did not land: %+v ok %v", cell, ok)
	}
	if cell, ok := sheet.Cell(Metric, "high", "b/two", nil); !ok || cell.N != 1 {
		t.Fatalf("the high score did not land: %+v ok %v", cell, ok)
	}
}

// A recorder with no sheet has nowhere to put what it is given, and says so.
func TestARecorderWithoutASheetRefuses(t *testing.T) {
	recorder := &Recorder{}
	if err := recorder.Record(nil, "z/fourth", "chat", "one", "2026-09-17"); err == nil {
		t.Fatal("a sheetless recorder recorded")
	}
}

// The own sheet's cells answer sorted by seat then model, each carrying the
// mean of its observations and the count behind it, and a cell recorded under
// dim labels is not one of them — the prior reads the sheet's plain cells.
func TestCellsAnswerSortedAndCarryTheMeanAndCount(t *testing.T) {
	sheet := tally.New()
	// Observed deliberately out of the order they must answer in.
	sheet.Observe(Metric, "worker", "b/later", nil, 60)
	sheet.Observe(Metric, "worker", "a/first", nil, 90)
	sheet.Observe(Metric, "worker", "a/first", nil, 80)
	sheet.Observe(Metric, "high", "c/top", nil, 70)
	sheet.Observe(Metric, "high", "a/first", map[string]string{"quant": "fp8"}, 40)

	got := Cells(sheet)
	if len(got) != 3 {
		t.Fatalf("%d cells answered, want the three plain ones: %+v", len(got), got)
	}
	want := []Cell{
		{Role: "high", Model: "c/top", Mean: 70, N: 1},
		{Role: "worker", Model: "a/first", Mean: 85, N: 2},
		{Role: "worker", Model: "b/later", Mean: 60, N: 1},
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("cell %d is %+v, want %+v", i, got[i], want[i])
		}
	}
}

// TestExampleRowJSONIsARow holds the example `codeaf telemetry show` prints
// to the row itself: it parses back into a Row, carries every key the row
// spells and no other, and names the day it was asked for.
func TestExampleRowJSONIsARow(t *testing.T) {
	day := time.Date(2026, 9, 19, 23, 59, 0, 0, time.UTC)
	text := ExampleRowJSON(day)
	var row Row
	if err := json.Unmarshal([]byte(text), &row); err != nil {
		t.Fatalf("example does not parse as a row: %v\n%s", err, text)
	}
	if row.Schema != rowSchema || row.Metric != Metric || row.Day != "2026-09-19" {
		t.Errorf("example row = %+v", row)
	}
	var keys map[string]any
	if err := json.Unmarshal([]byte(text), &keys); err != nil {
		t.Fatal(err)
	}
	rt := reflect.TypeOf(Row{})
	for i := 0; i < rt.NumField(); i++ {
		name, _, _ := strings.Cut(rt.Field(i).Tag.Get("json"), ",")
		if _, ok := keys[name]; !ok {
			t.Errorf("example row lacks %q", name)
		}
	}
	if len(keys) != rt.NumField() {
		t.Errorf("example row has %d keys, the row has %d", len(keys), rt.NumField())
	}
}
