package appwire

import (
	"strings"
	"sync"
	"testing"
)

// reportEngine serves the report and remembers what it was asked.
type reportEngine struct {
	fakeEngine
	mu     sync.Mutex
	asks   []SpendReportAsk
	result SpendReportResult
}

func (r *reportEngine) SpendReport(ask SpendReportAsk) (SpendReportResult, error) {
	r.mu.Lock()
	r.asks = append(r.asks, ask)
	r.mu.Unlock()
	return r.result, nil
}

func TestSpendReportIsUnknownMethodWithoutTheCapability(t *testing.T) {
	frame := serveOver(t, &fakeEngine{})(`{"id":1,"method":"spend.report","params":{"from":"2026-10-01","to":"2026-10-31"}}`)
	if frameCode(frame) != CodeUnknownMethod {
		t.Fatalf("%v", frame)
	}
}

func TestSpendReportOnTheWire(t *testing.T) {
	usd := 0.25
	engine := &reportEngine{result: SpendReportResult{
		From: "2026-10-01", To: "2026-10-31",
		Totals: SpendReportTotals{Cells: 2, Records: 3, Calls: 3},
		Cells: []SpendReportCell{
			{Day: "2026-10-14", Model: "m/a", Records: 2, Calls: 2, CostUSD: &usd},
			{Day: "2026-10-14", Records: 1, Calls: 1, UnpricedRecords: 1},
		},
	}}
	frame := serveOver(t, engine)(`{"id":1,"method":"spend.report","params":{"from":" 2026-10-01 ","to":"2026-10-31","kind":"task","subject":"7","root":"abc"}}`)
	result, ok := frame["result"].(map[string]any)
	if !ok {
		t.Fatalf("no result: %v", frame)
	}
	if len(engine.asks) != 1 || engine.asks[0].Filter.From != "2026-10-01" || engine.asks[0].Filter.Kind != SpendKindTask || engine.asks[0].Filter.Root != "abc" {
		t.Fatalf("the engine was asked %+v", engine.asks)
	}
	cells := result["cells"].([]any)
	priced, unpriced := cells[0].(map[string]any), cells[1].(map[string]any)
	if priced["cost_usd"] != 0.25 {
		t.Fatalf("priced cell %v", priced)
	}
	for _, absent := range []string{"cost_usd", "model", "kind", "subject", "root"} {
		if _, there := unpriced[absent]; there {
			t.Fatalf("a cell nobody could fill has %q: %v", absent, unpriced)
		}
	}
	if unpriced["unpriced_records"] != float64(1) {
		t.Fatalf("unpriced cell %v", unpriced)
	}
	// Unknown dimensions are named on every answer, and no cell carries them.
	if got := result["not_recorded"].([]any); len(got) != len(SpendReportNotRecorded) {
		t.Fatalf("not_recorded %v", got)
	}
	for _, name := range []string{"provider", "outcome", "crew", "billing_source"} {
		if _, there := priced[name]; there {
			t.Fatalf("a cell claims %s", name)
		}
	}
	if result["limits"].(map[string]any)["max_cells"] != float64(SpendReportCellsMax) {
		t.Fatalf("limits %v", result["limits"])
	}
	if models, ok := result["models"].([]any); !ok || len(models) != 0 {
		t.Fatalf("an empty list is a list: %v", result["models"])
	}
}

func TestSpendReportRefusesWhatCannotBeAsked(t *testing.T) {
	call := serveOver(t, &reportEngine{})
	for name, params := range map[string]string{
		"no range":        `{}`,
		"no to":           `{"from":"2026-10-01"}`,
		"bad day":         `{"from":"2026-13-01","to":"2026-10-01"}`,
		"reversed":        `{"from":"2026-10-02","to":"2026-10-01"}`,
		"all history":     `{"from":"2000-01-01","to":"2026-10-01"}`,
		"367 days":        `{"from":"2025-10-01","to":"2026-10-02"}`,
		"unknown field":   `{"from":"2026-10-01","to":"2026-10-02","crew":"x"}`,
		"cursor":          `{"from":"2026-10-01","to":"2026-10-02","cursor":"x"}`,
		"limit":           `{"from":"2026-10-01","to":"2026-10-02","limit":5}`,
		"subject no kind": `{"from":"2026-10-01","to":"2026-10-02","subject":"x"}`,
		"newline":         `{"from":"2026-10-01","to":"2026-10-02","model":"a\nb"}`,
	} {
		frame := call(`{"id":1,"method":"spend.report","params":` + params + `}`)
		if frameCode(frame) != CodeBadParams {
			t.Errorf("%s: %v", name, frame)
		}
	}
	// 366 inclusive days is the longest; the engine is reached.
	engine := &reportEngine{}
	frame := serveOver(t, engine)(`{"id":1,"method":"spend.report","params":{"from":"2025-10-02","to":"2026-10-02"}}`)
	if _, ok := frame["result"]; !ok || len(engine.asks) != 1 {
		t.Fatalf("366 days: %v", frame)
	}
}

func TestSpendReportReplyIsRefusedNotCut(t *testing.T) {
	big := strings.Repeat("m", 300<<10)
	var cells []SpendReportCell
	for range 20 {
		cells = append(cells, SpendReportCell{Day: "2026-10-01", Model: big, Records: 1, Calls: 1})
	}
	frame := serveOver(t, &reportEngine{result: SpendReportResult{Cells: cells}})(`{"id":1,"method":"spend.report","params":{"from":"2026-10-01","to":"2026-10-02"}}`)
	if frameCode(frame) != CodeBadParams {
		t.Fatalf("an answer past the budget: %v", frame)
	}
	over := make([]SpendReportCell, SpendReportCellsMax+1)
	frame = serveOver(t, &reportEngine{result: SpendReportResult{Cells: over}})(`{"id":1,"method":"spend.report","params":{"from":"2026-10-01","to":"2026-10-02"}}`)
	if frameCode(frame) != CodeBadParams {
		t.Fatalf("an engine that returned more cells than the cap: %v", frame)
	}
}
