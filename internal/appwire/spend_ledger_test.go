package appwire

import (
	"bufio"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// ledgerEngine is an engine that serves the ledger, and remembers what it was
// asked.
type ledgerEngine struct {
	fakeEngine
	mu      sync.Mutex
	asks    []SpendLedgerAsk
	exports []SpendLedgerExportAsk
}

func (l *ledgerEngine) SpendLedger(ask SpendLedgerAsk) (SpendLedgerResult, error) {
	l.mu.Lock()
	l.asks = append(l.asks, ask)
	l.mu.Unlock()
	if ask.Cursor == "stale" {
		return SpendLedgerResult{}, Errorf(CodeBadParams, "the cursor does not belong to this query")
	}
	usd := 0.5
	return SpendLedgerResult{
		From: ask.Filter.From, To: ask.Filter.To,
		Totals: SpendLedgerTotals{Rows: 2, Runs: 1, UnknownRunRecords: 1, Records: 3, Calls: 3, CostUSD: &usd, UnpricedRecords: 1},
		Rows: []SpendLedgerRow{
			{Grain: SpendGrainRun, Key: "run:r1", Run: "r1", Time: time.Date(2026, 10, 15, 9, 0, 0, 0, time.FixedZone("", -4*3600)), Models: []string{"a", "b"}, Records: 2, Calls: 2, CostUSD: &usd},
			{Grain: SpendGrainCall, Key: "call:0000000000000000", Models: []string{}, Records: 1, Calls: 1, UnpricedRecords: 1},
		},
	}, nil
}

func (l *ledgerEngine) ExportSpendLedger(ask SpendLedgerExportAsk) (SpendLedgerExportResult, error) {
	l.mu.Lock()
	l.exports = append(l.exports, ask)
	l.mu.Unlock()
	return SpendLedgerExportResult{Path: ask.Destination, Rows: 2, Bytes: 10}, nil
}

// serveOver is a running server over two pipes for any engine, as the app
// holds it; it answers one request line with the response that carries id 1.
func serveOver(t *testing.T, engine Engine) func(line string) map[string]any {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	go func() {
		_ = NewServer(outW).Serve(context.Background(), inR, Hello{Version: "test"}, engine)
		outW.Close()
	}()
	t.Cleanup(func() { inW.Close() })
	lines := bufio.NewScanner(outR)
	lines.Buffer(make([]byte, 1<<20), 1<<20)
	return func(line string) map[string]any {
		t.Helper()
		if _, err := io.WriteString(inW, line+"\n"); err != nil {
			t.Fatal(err)
		}
		got := make(chan map[string]any, 1)
		go func() {
			for lines.Scan() {
				var frame map[string]any
				if json.Unmarshal(lines.Bytes(), &frame) == nil && frame["id"] == float64(1) {
					got <- frame
					return
				}
			}
			got <- nil
		}()
		select {
		case frame := <-got:
			if frame == nil {
				t.Fatal("the output ended")
			}
			return frame
		case <-time.After(5 * time.Second):
			t.Fatal("no answer arrived")
			return nil
		}
	}
}

func frameCode(frame map[string]any) string {
	e, _ := frame["error"].(map[string]any)
	code, _ := e["code"].(string)
	return code
}

// AN ENGINE WITHOUT THE CAPABILITY DOES NOT HAVE THE VERB, said on the wire
// exactly as an absent method is.
func TestSpendLedgerIsUnknownMethodWithoutTheCapability(t *testing.T) {
	call := serveOver(t, &fakeEngine{})
	for _, name := range []string{"spend.ledger", "spend.ledger.export"} {
		frame := call(`{"id":1,"method":"` + name + `","params":{"from":"2026-10-01","to":"2026-10-15","destination":"/tmp/x.csv"}}`)
		if frameCode(frame) != CodeUnknownMethod {
			t.Fatalf("%s: %v", name, frame)
		}
	}
}

// THE PAGE ARRIVES AS THE WIRE SAYS: totals beside the rows, a cost nobody
// priced absent rather than zero, the default page size handed to the engine,
// and an empty list a list.
func TestSpendLedgerOnTheWire(t *testing.T) {
	engine := &ledgerEngine{}
	call := serveOver(t, engine)
	frame := call(`{"id":1,"method":"spend.ledger","params":{"from":" 2026-10-01 ","to":"2026-10-15","kind":"task","subject":"7","root":"abc"}}`)
	result, ok := frame["result"].(map[string]any)
	if !ok {
		t.Fatalf("no result: %v", frame)
	}
	if len(engine.asks) != 1 || engine.asks[0].Limit != SpendLedgerLimitDefault || engine.asks[0].Filter.From != "2026-10-01" || engine.asks[0].Filter.Kind != SpendKindTask {
		t.Fatalf("the engine was asked %+v", engine.asks)
	}
	rows := result["rows"].([]any)
	run, call0 := rows[0].(map[string]any), rows[1].(map[string]any)
	if run["grain"] != "run" || run["cost_usd"] != 0.5 || run["time"] != "2026-10-15T09:00:00-04:00" {
		t.Fatalf("run row %v", run)
	}
	if _, priced := call0["cost_usd"]; priced || call0["unpriced_records"] != float64(1) {
		t.Fatalf("an unpriced call row %v", call0)
	}
	if _, named := call0["name"]; named {
		t.Fatalf("a row nobody named has a name: %v", call0)
	}
	totals := result["totals"].(map[string]any)
	if totals["runs"] != float64(1) || totals["unknown_run_records"] != float64(1) {
		t.Fatalf("totals %v", totals)
	}
	if _, ok := result["next"]; ok {
		t.Fatalf("a last page has a next: %v", result)
	}
	if code := frameCode(call(`{"id":1,"method":"spend.ledger","params":{"from":"2026-10-01","to":"2026-10-15","cursor":"stale"}}`)); code != CodeBadParams {
		t.Fatalf("a stale cursor answered %q", code)
	}
}

// WHAT CANNOT BE ASKED IS REFUSED BEFORE THE ENGINE IS ASKED, and a field the
// method does not know is refused rather than ignored — a misspelled filter
// must not save every row.
func TestSpendLedgerRefusesBadParams(t *testing.T) {
	engine := &ledgerEngine{}
	for _, params := range []string{
		`{"to":"2026-10-15"}`,
		`{"from":"2026-10-15","to":"2026-10-14"}`,
		`{"from":"2026-02-30","to":"2026-03-01"}`,
		`{"from":"15/10/2026","to":"2026-10-15"}`,
		`{"from":"2026-10-01","to":"2026-10-15","limit":501}`,
		`{"from":"2026-10-01","to":"2026-10-15","limit":-1}`,
		`{"from":"2026-10-01","to":"2026-10-15","modle":"x"}`,
		`{"from":"2026-10-01","to":"2026-10-15","kind":"chat"}`,
		`{"from":"2026-10-01","to":"2026-10-15","subject":"7"}`,
		`{"from":"2026-10-01","to":"2026-10-15","model":"a\nb"}`,
		`{"from":"2026-10-01","to":"2026-10-15","cursor":"` + strings.Repeat("a", 1025) + `"}`,
	} {
		_, err := methods["spend.ledger"].call(engine, json.RawMessage(params))
		if wireCode(t, err) != CodeBadParams {
			t.Fatalf("%s: %v", params, err)
		}
	}
	for _, params := range []string{
		`{"from":"2026-10-01","to":"2026-10-15"}`,
		`{"from":"2026-10-01","to":"2026-10-15","destination":"spend.csv"}`,
		`{"from":"2026-10-01","to":"2026-10-15","destination":"/tmp/../tmp/spend.csv"}`,
		`{"from":"2026-10-01","to":"2026-10-15","destination":"/tmp/spend.csv","limit":5}`,
		`{"from":"2026-10-01","to":"2026-10-15","destination":"/tmp/sp\u0000end.csv"}`,
	} {
		_, err := methods["spend.ledger.export"].call(engine, json.RawMessage(params))
		if wireCode(t, err) != CodeBadParams {
			t.Fatalf("export %s: %v", params, err)
		}
	}
	if len(engine.asks) != 0 || len(engine.exports) != 0 {
		t.Fatalf("the engine was asked %+v %+v", engine.asks, engine.exports)
	}
	out, err := methods["spend.ledger.export"].call(engine, json.RawMessage(`{"from":"2026-10-01","to":"2026-10-15","model":"m","destination":"/tmp/spend.csv","overwrite":true}`))
	if err != nil || out.(*SpendLedgerExportResult).Path != "/tmp/spend.csv" || !engine.exports[0].Overwrite || engine.exports[0].Filter.Model != "m" {
		t.Fatalf("%v %+v", err, engine.exports)
	}
}

// A CELL IS ONE CELL whatever is in it — a quote, a comma, a newline — and a
// name a spreadsheet would run as a formula is written so it reads as text.
func TestSpendLedgerCSVRecord(t *testing.T) {
	usd := 0.25
	rows := []SpendLedgerRow{
		{Grain: SpendGrainRun, Key: "run:r", Run: "r", Kind: SpendKindAgent, Subject: "abc", Name: "fix \"the\" thing,\nnow", Models: []string{"a", "b"}, TokensIn: 5, TokensOut: 6, CostUSD: &usd, Records: 2, Calls: 2},
		{Grain: SpendGrainCall, Key: "call:7", Name: "=HYPERLINK(\"x\")", Workspace: "-w", Records: 1, Calls: 1, UnpricedRecords: 1},
		{Grain: SpendGrainRun, Key: "run:q", Run: "q", Records: 1, Calls: 1},
	}
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := NewSpendLedgerCSV(w); err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if err := w.Write(SpendLedgerCSVRecord(row)); err != nil {
			t.Fatal(err)
		}
	}
	w.Flush()
	records, err := csv.NewReader(&buf).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 4 || len(records[0]) != len(SpendLedgerCSVHeader) {
		t.Fatalf("%d records of %d cells", len(records), len(records[0]))
	}
	col := map[string]int{}
	for i, name := range records[0] {
		col[name] = i
	}
	if records[1][col["name"]] != "fix \"the\" thing,\nnow" || records[1][col["models"]] != "a;b" || records[1][col["cost_usd"]] != "0.25" || records[1][col["kind"]] != "agent" {
		t.Fatalf("row 1 %q", records[1])
	}
	if records[2][col["name"]] != "'=HYPERLINK(\"x\")" || records[2][col["workspace"]] != "'-w" || records[2][col["cost_usd"]] != "" || records[2][col["unpriced_records"]] != "1" {
		t.Fatalf("row 2 %q", records[2])
	}
	if records[3][col["name"]] != "q" || records[3][col["by"]] != "" {
		t.Fatalf("row 3 falls back to its run: %q", records[3])
	}
}
