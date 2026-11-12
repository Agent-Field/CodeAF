package appwire

import (
	"bytes"
	"encoding/csv"
	"strings"
	"sync"
	"testing"
	"time"
)

// callsEngine serves the calls file and remembers what it was asked. It is not
// a SpendLedgerer: the two optional capabilities are independent.
type callsEngine struct {
	fakeEngine
	mu     sync.Mutex
	asks   []SpendCallsExportAsk
	result SpendCallsExportResult
}

func (c *callsEngine) ExportSpendCalls(ask SpendCallsExportAsk) (SpendCallsExportResult, error) {
	c.mu.Lock()
	c.asks = append(c.asks, ask)
	c.mu.Unlock()
	return c.result, nil
}

func TestSpendCallsExportIsUnknownMethodWithoutTheCapability(t *testing.T) {
	frame := serveOver(t, &fakeEngine{})(`{"id":1,"method":"spend.calls.export","params":{"from":"2026-10-01","to":"2026-10-15","destination":"/tmp/x.csv"}}`)
	if frameCode(frame) != CodeUnknownMethod {
		t.Fatalf("%v", frame)
	}
	// An engine with only the capability serves only it: the ledger's pair is
	// still unknown there.
	frame = serveOver(t, &callsEngine{})(`{"id":1,"method":"spend.ledger.export","params":{"from":"2026-10-01","to":"2026-10-15","destination":"/tmp/x.csv"}}`)
	if frameCode(frame) != CodeUnknownMethod {
		t.Fatalf("%v", frame)
	}
}

func TestSpendCallsExportOnTheWire(t *testing.T) {
	usd := 0.5
	engine := &callsEngine{result: SpendCallsExportResult{
		Path: "/tmp/codeaf-spending-2026-10.csv", Rows: 2, Bytes: 99,
		Totals: SpendCallsTotals{Records: 2, Calls: 3, TokensIn: 4, TokensOut: 5, CostUSD: &usd, UnpricedRecords: 1},
	}}
	dest := "/tmp/dir with space/codeaf spending.csv "
	frame := serveOver(t, engine)(`{"id":1,"method":"spend.calls.export","params":{"from":" 2026-10-01 ","to":"2026-10-31","agent":"chatA","model":"m/one","destination":"` + dest + `","overwrite":true}}`)
	result, ok := frame["result"].(map[string]any)
	if !ok {
		t.Fatalf("no result: %v", frame)
	}
	if len(engine.asks) != 1 {
		t.Fatalf("asked %d times", len(engine.asks))
	}
	ask := engine.asks[0]
	if ask.Filter.From != "2026-10-01" || ask.Filter.Agent != "chatA" || ask.Filter.Model != "m/one" || !ask.Overwrite {
		t.Fatalf("the engine was asked %+v", ask)
	}
	if ask.Destination != dest {
		t.Fatalf("the destination was rewritten: %q", ask.Destination)
	}
	totals := result["totals"].(map[string]any)
	if totals["cost_usd"] != 0.5 || totals["calls"] != float64(3) || totals["records"] != float64(2) {
		t.Fatalf("totals %v", totals)
	}
	// Nothing the pass did not compute is on the wire, and nothing is invented.
	for _, absent := range []string{"runs", "unknown_run_records", "run_cost_usd", "unknown_run_cost_usd"} {
		if _, there := totals[absent]; there {
			t.Fatalf("totals claim %s: %v", absent, totals)
		}
	}
	got := result["not_recorded"].([]any)
	if len(got) != 3 || got[0] != "crew" || got[1] != "provider" || got[2] != "outcome" {
		t.Fatalf("not_recorded %v", got)
	}
	if result["skipped"] == nil || result["path"] != "/tmp/codeaf-spending-2026-10.csv" {
		t.Fatalf("%v", result)
	}

	// No priced record, no cost: absent, not zero.
	empty := &callsEngine{}
	frame = serveOver(t, empty)(`{"id":1,"method":"spend.calls.export","params":{"from":"2026-10-01","to":"2026-10-31","destination":"/tmp/x.csv"}}`)
	totals = frame["result"].(map[string]any)["totals"].(map[string]any)
	if _, there := totals["cost_usd"]; there {
		t.Fatalf("a range with no price has a cost: %v", totals)
	}
}

func TestSpendCallsExportRefusesWhatCannotBeAsked(t *testing.T) {
	engine := &callsEngine{}
	call := serveOver(t, engine)
	for name, params := range map[string]string{
		"no range":        `{"destination":"/tmp/x.csv"}`,
		"no to":           `{"from":"2026-10-01","destination":"/tmp/x.csv"}`,
		"bad day":         `{"from":"2026-13-01","to":"2026-10-01","destination":"/tmp/x.csv"}`,
		"reversed":        `{"from":"2026-10-02","to":"2026-10-01","destination":"/tmp/x.csv"}`,
		"no destination":  `{"from":"2026-10-01","to":"2026-10-02"}`,
		"blank":           `{"from":"2026-10-01","to":"2026-10-02","destination":"   "}`,
		"relative":        `{"from":"2026-10-01","to":"2026-10-02","destination":"x.csv"}`,
		"unclean":         `{"from":"2026-10-01","to":"2026-10-02","destination":"/tmp/../x.csv"}`,
		"newline":         `{"from":"2026-10-01","to":"2026-10-02","destination":"/tmp/a\nb.csv"}`,
		"nul":             `{"from":"2026-10-01","to":"2026-10-02","destination":"/tmp/a\u0000b.csv"}`,
		"unknown field":   `{"from":"2026-10-01","to":"2026-10-02","destination":"/tmp/x.csv","crew":"x"}`,
		"misspelt filter": `{"from":"2026-10-01","to":"2026-10-02","destination":"/tmp/x.csv","modle":"x"}`,
		"cursor":          `{"from":"2026-10-01","to":"2026-10-02","destination":"/tmp/x.csv","cursor":"x"}`,
		"bad kind":        `{"from":"2026-10-01","to":"2026-10-02","destination":"/tmp/x.csv","kind":"crew"}`,
		"subject no kind": `{"from":"2026-10-01","to":"2026-10-02","destination":"/tmp/x.csv","subject":"x"}`,
		"filter newline":  `{"from":"2026-10-01","to":"2026-10-02","destination":"/tmp/x.csv","agent":"a\nb"}`,
		"overwrite type":  `{"from":"2026-10-01","to":"2026-10-02","destination":"/tmp/x.csv","overwrite":"yes"}`,
	} {
		frame := call(`{"id":1,"method":"spend.calls.export","params":` + params + `}`)
		if frameCode(frame) != CodeBadParams {
			t.Errorf("%s: %v", name, frame)
		}
	}
	if len(engine.asks) != 0 {
		t.Fatalf("a refused ask reached the engine: %+v", engine.asks)
	}
}

// THE FILE'S TWELVE COLUMNS, EXACTLY, and the three the ledger cannot fill are
// blank on every row.
func TestSpendCallsCSVHasTheTwelveColumns(t *testing.T) {
	want := "date,time,crew,agent,task,model,provider,outcome,amount_usd,requests,tokens_in,tokens_out"
	if strings.Join(SpendCallsCSVHeader, ",") != want || len(SpendCallsCSVHeader) != 12 {
		t.Fatalf("%v", SpendCallsCSVHeader)
	}
	zone := time.FixedZone("Toronto", -4*3600)
	usd := 0.0123456789012345
	small := 0.0000001
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := NewSpendCallsCSV(w); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 14, 23, 59, 58, 123, time.UTC).In(zone)
	for _, c := range []SpendCall{
		{Time: at, Agent: "chatA", Task: "3", Model: "m/one", CostUSD: &usd, Requests: 41, TokensIn: 400, TokensOut: 40},
		{Time: at, Agent: "=cmd", Model: "weird, \"model\"\nname é ", CostUSD: &small, Requests: 1},
		{Time: at, Requests: 1, TokensIn: 1, TokensOut: 1},
	} {
		if err := w.Write(SpendCallsCSVRecord(c)); err != nil {
			t.Fatal(err)
		}
	}
	w.Flush()
	rows, err := csv.NewReader(&buf).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 {
		t.Fatalf("%v", rows)
	}
	first := strings.Join(rows[1], "|")
	if first != "2026-10-14|19:59:58||chatA|3|m/one|||0.0123456789012345|41|400|40" {
		t.Fatalf("%q", first)
	}
	for i, row := range rows {
		if len(row) != 12 {
			t.Fatalf("row %d has %d cells", i, len(row))
		}
		if i > 0 && (row[2] != "" || row[6] != "" || row[7] != "") {
			t.Fatalf("a row claims a crew, provider or outcome: %v", row)
		}
	}
	if rows[2][3] != "'=cmd" || rows[2][5] != "weird, \"model\"\nname é " || rows[2][8] != "0.0000001" {
		t.Fatalf("%q", rows[2])
	}
	if rows[3][8] != "" || rows[3][4] != "" || rows[3][5] != "" {
		t.Fatalf("an unpriced call has an amount or a model: %q", rows[3])
	}
}
