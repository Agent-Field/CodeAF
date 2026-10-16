package appwire

// ── THE SPENDING CALLS: every call the filter matches, one CSV row each ─────
//
// spend.calls.export is the generic per-call file beside spend.ledger.export.
// The ledger's export is run grain — a run is one row, and its columns say what
// the ledger holds about a run — and it stays exactly as it is. This one is the
// file a spending screen's "Export CSV" saves: THE SAME FILTER the ledger and
// the report take (local days, both included and required, plus the same exact
// matches, agent among them), written as twelve fixed columns, one row per
// ledger record, in the order the calls were recorded.
//
// ONE ROW IS ONE RECORD. A ledger line from an older build may cover many
// provider requests; its row says so in requests and is never split into
// invented single calls. The date and time are the call's instant in the
// engine's zone, the zone the filter's days were read in, so a row is always
// inside the range that selected it.
//
// UNKNOWN IS BLANK, AND THE ANSWER SAYS WHICH COLUMNS ARE ALWAYS BLANK. The
// ledger records no crew, no billing provider and no task outcome, and the
// engine does not join them from today's crews, today's routing or today's task
// states: what a crew or an outcome is now is not what it was when the money was
// spent. Those three columns are empty on every row and not_recorded names them,
// so a client never has to guess whether "empty" meant "none" or "unknown". A
// call nobody could price has a blank amount_usd — it is not $0 — and the
// answer's unpriced_records counts it. A future ledger that records one of these
// facts is a different version of this file, not a quiet change to this one.
//
// THE ANSWER CARRIES NO RUN COUNT. The file is written in one pass that keeps no
// set of the runs it meets, so it cannot say how many there were, and it does
// not say a number it did not compute.
//
// AN ENGINE WITHOUT THE CAPABILITY DOES NOT HAVE THE VERB: spend.calls.export
// answers unknown_method from an engine that is not a [SpendCallsExporter].

import (
	"encoding/csv"
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// SpendCallsNotRecorded are the columns of the calls file the ledger does not
// hold, blank on every row.
var SpendCallsNotRecorded = []string{"crew", "provider", "outcome"}

// SpendCallsCSVHeader is the calls file's twelve columns, in order.
var SpendCallsCSVHeader = []string{
	"date", "time", "crew", "agent", "task", "model", "provider", "outcome",
	"amount_usd", "requests", "tokens_in", "tokens_out",
}

// SpendCallsExportParams is spend.calls.export's params: the ledger's filter,
// an absolute destination, and whether an existing file there may be replaced.
type SpendCallsExportParams struct {
	SpendLedgerFilter
	Destination string `json:"destination"`
	Overwrite   bool   `json:"overwrite,omitempty"`
}

// SpendCallsExportAsk is spend.calls.export as the engine is handed it: a
// checked filter and a checked destination.
type SpendCallsExportAsk struct {
	Filter      SpendLedgerFilter
	Destination string
	Overwrite   bool
}

// SpendCallsTotals is exactly the rows written. records equals rows; requests
// are provider requests, which a record from an older build may carry many of.
// cost_usd is absent where no record carried a price.
type SpendCallsTotals struct {
	Records         int      `json:"records"`
	Calls           int      `json:"calls"`
	TokensIn        int64    `json:"tokens_in"`
	TokensOut       int64    `json:"tokens_out"`
	CostUSD         *float64 `json:"cost_usd,omitempty"`
	UnpricedRecords int      `json:"unpriced_records"`
	UnbilledRecords int      `json:"unbilled_records"`
}

// SpendCallsExportResult is spend.calls.export's answer: where the file is, how
// many rows and bytes it holds, and the totals of exactly those rows.
type SpendCallsExportResult struct {
	Path        string             `json:"path"`
	Rows        int                `json:"rows"`
	Bytes       int64              `json:"bytes"`
	Totals      SpendCallsTotals   `json:"totals"`
	Skipped     SpendLedgerSkipped `json:"skipped"`
	NotRecorded []string           `json:"not_recorded"`
}

// SpendCallsExporter is the engine's half of the calls file. It is optional: an
// [Engine] that is not one refuses spend.calls.export as unknown_method.
type SpendCallsExporter interface {
	ExportSpendCalls(ask SpendCallsExportAsk) (SpendCallsExportResult, error)
}

// SpendCall is one ledger record as a row of the file. Time carries the zone
// the file is written in; every id is the one the ledger wrote.
type SpendCall struct {
	Time      time.Time
	Agent     string
	Task      string
	Model     string
	CostUSD   *float64
	Requests  int
	TokensIn  int64
	TokensOut int64
}

// SpendCallsCSVRecord is one call as its twelve cells. crew, provider and
// outcome are empty because the ledger records none of them. The amount is
// written at its full precision — the shortest decimal that reads back as the
// same number — so a column of amounts adds up to the total the answer states;
// a call nobody could price is an empty cell. Every text cell that a
// spreadsheet would read as a formula is written with a leading apostrophe.
func SpendCallsCSVRecord(c SpendCall) []string {
	amount := ""
	if c.CostUSD != nil {
		amount = strconv.FormatFloat(*c.CostUSD, 'f', -1, 64)
	}
	return []string{
		c.Time.Format("2006-01-02"), c.Time.Format("15:04:05"), "", csvText(c.Agent), csvText(c.Task), csvText(c.Model), "", "",
		amount, strconv.Itoa(c.Requests), strconv.FormatInt(c.TokensIn, 10), strconv.FormatInt(c.TokensOut, 10),
	}
}

// NewSpendCallsCSV is a CSV writer with the header already written.
func NewSpendCallsCSV(w *csv.Writer) error {
	return w.Write(SpendCallsCSVHeader)
}

// checkSpendCallsDestination refuses a destination that is not the exact
// absolute path of the file to write. The path is used as given, trailing
// spaces and all: it is not trimmed, cleaned or rewritten.
func checkSpendCallsDestination(dest string) error {
	if strings.TrimSpace(dest) == "" || strings.ContainsAny(dest, "\x00\r\n") || !filepath.IsAbs(dest) || filepath.Clean(dest) != dest {
		return Errorf(CodeBadParams, "destination is an absolute, clean path to the file to write")
	}
	return nil
}

func init() {
	methods["spend.calls.export"] = spendCallsExportMethod
}

var spendCallsExportMethod = shaped[SpendCallsExportParams](func(e Engine, raw json.RawMessage) (*SpendCallsExportResult, error) {
	x, ok := e.(SpendCallsExporter)
	if !ok {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", "spend.calls.export")
	}
	var p SpendCallsExportParams
	if err := decodeStrict("spend.calls.export", raw, &p); err != nil {
		return nil, err
	}
	filter, err := checkSpendLedgerFilter(p.SpendLedgerFilter)
	if err != nil {
		return nil, err
	}
	if err := checkSpendCallsDestination(p.Destination); err != nil {
		return nil, err
	}
	result, err := x.ExportSpendCalls(SpendCallsExportAsk{Filter: filter, Destination: p.Destination, Overwrite: p.Overwrite})
	if err != nil {
		return nil, err
	}
	result.NotRecorded = SpendCallsNotRecorded
	return &result, nil
})
