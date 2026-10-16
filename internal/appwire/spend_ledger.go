package appwire

// ── THE SPENDING LEDGER: one filter, a page to read, a file to keep ─────────
//
// spend.export answers every call in a range as a raw CSV in the app's own
// folder, and it stays exactly as it is. spend.ledger and spend.ledger.export
// are the generic pair beside it: ONE FILTER, read twice — a page of rows with
// the whole range's totals, and a CSV of the same rows written where the caller
// says. A client that reads a preview and then saves it asks the same question
// both times, and the file holds what the preview counted at the moment of the
// save (a later save is a later reading; the ledger only grows).
//
// THE GRAIN IS SAID ON EVERY ROW. A "run" row is every call one immutable
// execution made, added up whatever models it used; a "call" row is one ledger
// line that carries no run — an older build's, an auxiliary call's, a late
// receipt's — and it is never grouped with anything. The totals count the two
// apart (runs, unknown_run_records) and every figure includes both.
//
// UNKNOWN IS ABSENT. A row's name is present only where the engine holds a
// title for the thing the money was spent on; kind, subject and the ids are
// present only where the ledger wrote them (and, on a run, where every call
// agreed). A cost nobody could price is absent with unpriced_records saying
// so — it is not $0.
//
// AN ENGINE WITHOUT THE CAPABILITY DOES NOT HAVE THE VERB: both methods answer
// unknown_method from an engine that is not a [SpendLedgerer].

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// The two grains a ledger row has.
const (
	SpendGrainRun  = "run"
	SpendGrainCall = "call"
)

// The three things money is spent on, in the wire's words: a conversation is
// an agent here, as everywhere on this wire.
const (
	SpendKindAgent      = "agent"
	SpendKindTask       = "task"
	SpendKindAutomation = "automation"
)

// The page sizes spend.ledger takes.
const (
	SpendLedgerLimitDefault = 200
	SpendLedgerLimitMax     = 500
)

// SpendLedgerReplyMax is the most bytes one spend.ledger answer may be, as
// JSON. A page of 500 rows is not a bound by itself: one run may have met a
// hundred thousand models, and every text field may be as long as a ledger line.
// AN ANSWER PAST IT IS REFUSED, NEVER CUT: dropping models or rows to fit would
// show a preview that disagrees with the file saved from it. The export is not
// held to it, because it streams to disk a row at a time.
const SpendLedgerReplyMax = 2 << 20

// SpendLedgerFilter is the one question both methods ask. from and to are
// local calendar days, both included, and required. Every other field is an
// exact match on what the ledger wrote; subject needs kind.
type SpendLedgerFilter struct {
	From       string `json:"from"`
	To         string `json:"to"`
	Model      string `json:"model,omitempty"`
	Kind       string `json:"kind,omitempty" enum:"agent,task,automation"`
	Subject    string `json:"subject,omitempty"`
	Session    string `json:"session,omitempty"`
	Root       string `json:"root,omitempty"`
	Task       string `json:"task,omitempty"`
	Automation string `json:"automation,omitempty"`
	Run        string `json:"run,omitempty"`
	Workspace  string `json:"workspace,omitempty"`
}

// SpendLedgerParams is spend.ledger's params: the filter, a page size
// (default 200, at most 500) and the cursor the last page answered.
type SpendLedgerParams struct {
	SpendLedgerFilter
	Limit  int    `json:"limit,omitempty"`
	Cursor string `json:"cursor,omitempty"`
}

// SpendLedgerExportParams is spend.ledger.export's params: the filter, an
// absolute destination, and whether an existing file there may be replaced.
type SpendLedgerExportParams struct {
	SpendLedgerFilter
	Destination string `json:"destination"`
	Overwrite   bool   `json:"overwrite,omitempty"`
}

// SpendLedgerRow is one row at its grain. time is the first call's instant and
// end the last's, both with the engine's local offset.
type SpendLedgerRow struct {
	Grain string    `json:"grain" enum:"run,call"`
	Key   string    `json:"key"`
	Run   string    `json:"run,omitempty"`
	Time  time.Time `json:"time"
 "unicode/utf8"`
	End        time.Time `json:"end"`
	Kind       string    `json:"kind,omitempty" enum:"agent,task,automation"`
	Subject    string    `json:"subject,omitempty"`
	Name       string    `json:"name,omitempty"`
	Session    string    `json:"session,omitempty"`
	Root       string    `json:"root,omitempty"`
	Task       string    `json:"task,omitempty"`
	Automation string    `json:"automation,omitempty"`
	Workspace  string    `json:"workspace,omitempty"`
	Models     []string  `json:"models"`
	// UnnamedModelRecords is calls whose model nobody wrote down.
	UnnamedModelRecords int      `json:"unnamed_model_records,omitempty"`
	Records             int      `json:"records"`
	Calls               int      `json:"calls"`
	TokensIn            int64    `json:"tokens_in"`
	TokensOut           int64    `json:"tokens_out"`
	CostUSD             *float64 `json:"cost_usd,omitempty"`
	UnpricedRecords     int      `json:"unpriced_records,omitempty"`
	UnbilledRecords     int      `json:"unbilled_records,omitempty"`
}

// SpendLedgerTotals is the whole filtered range, whatever the page. rows is
// runs plus unknown_run_records; cost_usd is every priced record, and
// run_cost_usd and unknown_run_cost_usd its two halves.
type SpendLedgerTotals struct {
	Rows              int      `json:"rows"`
	Runs              int      `json:"runs"`
	UnknownRunRecords int      `json:"unknown_run_records"`
	Records           int      `json:"records"`
	Calls             int      `json:"calls"`
	TokensIn          int64    `json:"tokens_in"`
	TokensOut         int64    `json:"tokens_out"`
	CostUSD           *float64 `json:"cost_usd,omitempty"`
	RunCostUSD        *float64 `json:"run_cost_usd,omitempty"`
	UnknownRunCostUSD *float64 `json:"unknown_run_cost_usd,omitempty"`
	UnpricedRecords   int      `json:"unpriced_records"`
	UnbilledRecords   int      `json:"unbilled_records"`
}

// SpendLedgerSkipped is the ledger lines a reading passed over: lines that did
// not parse, and lines too long to hold.
type SpendLedgerSkipped struct {
	Malformed int `json:"malformed"`
	Oversized int `json:"oversized"`
}

// SpendLedgerResult is spend.ledger's answer. next is absent on the last page.
type SpendLedgerResult struct {
	From    string             `json:"from"`
	To      string             `json:"to"`
	Totals  SpendLedgerTotals  `json:"totals"`
	Rows    []SpendLedgerRow   `json:"rows"`
	Next    string             `json:"next,omitempty"`
	Skipped SpendLedgerSkipped `json:"skipped"`
}

// SpendLedgerExportResult is spend.ledger.export's answer: where the file is,
// how many rows and bytes it holds, and the totals of exactly those rows.
type SpendLedgerExportResult struct {
	Path    string             `json:"path"`
	Rows    int                `json:"rows"`
	Bytes   int64              `json:"bytes"`
	Totals  SpendLedgerTotals  `json:"totals"`
	Skipped SpendLedgerSkipped `json:"skipped"`
}

// SpendLedgerAsk is spend.ledger as the engine is handed it: a checked filter,
// a page size already defaulted, and the cursor as it arrived.
type SpendLedgerAsk struct {
	Filter SpendLedgerFilter
	Limit  int
	Cursor string
}

// SpendLedgerExportAsk is spend.ledger.export as the engine is handed it.
type SpendLedgerExportAsk struct {
	Filter      SpendLedgerFilter
	Destination string
	Overwrite   bool
}

// SpendLedgerer is the engine's half of the ledger. Both methods are optional:
// an [Engine] that is not one refuses them as unknown_method.
type SpendLedgerer interface {
	SpendLedger(ask SpendLedgerAsk) (SpendLedgerResult, error)
	ExportSpendLedger(ask SpendLedgerExportAsk) (SpendLedgerExportResult, error)
}

func init() {
	for name, m := range spendLedgerMethods {
		methods[name] = m
	}
}

func spendLedgerer(e Engine, name string) (SpendLedgerer, error) {
	l, ok := e.(SpendLedgerer)
	if !ok {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", name)
	}
	return l, nil
}

// decodeStrict reads params into v and refuses a field it does not know. A
// filter whose name was misspelled would otherwise be a filter silently not
// applied, and a file saved with every row in it.
func decodeStrict(name string, raw json.RawMessage, v any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(v); err != nil {
		return Errorf(CodeBadParams, "%s: the params could not be read: %v", name, err)
	}
	return nil
}

// checkSpendLedgerFilter trims the filter and refuses what cannot be asked.
// The days are checked as calendar days here; the engine reads them in its own
// zone.
func checkSpendLedgerFilter(f SpendLedgerFilter) (SpendLedgerFilter, error) {
	for _, field := range []*string{&f.From, &f.To, &f.Model, &f.Kind, &f.Subject, &f.Session, &f.Root, &f.Task, &f.Automation, &f.Run, &f.Workspace} {
		*field = strings.TrimSpace(*field)
		if strings.ContainsAny(*field, "\x00\r\n") {
			return f, Errorf(CodeBadParams, "a filter is one line")
		}
	}
	from, err := time.Parse("2006-01-02", f.From)
	if err != nil {
		return f, Errorf(CodeBadParams, "from is a day, as 2026-10-15")
	}
	to, err := time.Parse("2006-01-02", f.To)
	if err != nil {
		return f, Errorf(CodeBadParams, "to is a day, as 2026-10-15")
	}
	if to.Before(from) {
		return f, Errorf(CodeBadParams, "to is before from")
	}
	switch f.Kind {
	case "", SpendKindAgent, SpendKindTask, SpendKindAutomation:
	default:
		return f, Errorf(CodeBadParams, "kind is agent, task or automation")
	}
	if f.Subject != "" && f.Kind == "" {
		return f, Errorf(CodeBadParams, "subject needs its kind")
	}
	return f, nil
}

var spendLedgerMethods = map[string]method{
	"spend.ledger": shaped[SpendLedgerParams](func(e Engine, raw json.RawMessage) (*SpendLedgerResult, error) {
		l, err := spendLedgerer(e, "spend.ledger")
		if err != nil {
			return nil, err
		}
		var p SpendLedgerParams
		if err := decodeStrict("spend.ledger", raw, &p); err != nil {
			return nil, err
		}
		filter, err := checkSpendLedgerFilter(p.SpendLedgerFilter)
		if err != nil {
			return nil, err
		}
		switch {
		case p.Limit == 0:
			p.Limit = SpendLedgerLimitDefault
		case p.Limit < 0 || p.Limit > SpendLedgerLimitMax:
			return nil, Errorf(CodeBadParams, "limit is 1 to %d", SpendLedgerLimitMax)
		}
		if len(p.Cursor) > 1024 {
			return nil, Errorf(CodeBadParams, "cursor is the next an earlier page answered")
		}
		result, err := l.SpendLedger(SpendLedgerAsk{Filter: filter, Limit: p.Limit, Cursor: strings.TrimSpace(p.Cursor)})
		if err != nil {
			return nil, err
		}
		result.Rows = nonNil(result.Rows)
		if err := checkSpendLedgerReply(result, p.Limit); err != nil {
			return nil, err
		}
		return &result, nil
	}),
	"spend.ledger.export": shaped[SpendLedgerExportParams](func(e Engine, raw json.RawMessage) (*SpendLedgerExportResult, error) {
		l, err := spendLedgerer(e, "spend.ledger.export")
		if err != nil {
			return nil, err
		}
		var p SpendLedgerExportParams
		if err := decodeStrict("spend.ledger.export", raw, &p); err != nil {
			return nil, err
		}
		filter, err := checkSpendLedgerFilter(p.SpendLedgerFilter)
		if err != nil {
			return nil, err
		}
		dest := p.Destination
		if strings.TrimSpace(dest) == "" || strings.ContainsAny(dest, "\x00\r\n") || !filepath.IsAbs(dest) || filepath.Clean(dest) != dest {
			return nil, Errorf(CodeBadParams, "destination is an absolute, clean path to the file to write")
		}
		result, err := l.ExportSpendLedger(SpendLedgerExportAsk{Filter: filter, Destination: dest, Overwrite: p.Overwrite})
		if err != nil {
			return nil, err
		}
		return &result, nil
	}),
}

// checkSpendLedgerReply refuses an answer too large to carry, saying what to
// ask for instead.
func checkSpendLedgerReply(result SpendLedgerResult, limit int) error {
	if !spendLedgerFitsBeforeEncoding(result) {
		fewer := "ask for fewer rows with a smaller limit, or "
		if limit <= 1 {
			fewer = ""
		}
		return Errorf(CodeBadParams, "spend.ledger: this page exceeds the %d byte encoding budget; %snarrow the filter — a model, run or shorter range", SpendLedgerReplyMax, fewer)
	}
	data, err := json.Marshal(result)
	if err != nil {
		return Errorf(CodeEngine, "spend.ledger: the page could not be written as JSON: %v", err)
	}
	if len(data) <= SpendLedgerReplyMax {
		return nil
	}
	fewer := "ask for fewer rows with a smaller limit, or "
	if limit <= 1 {
		fewer = ""
	}
	return Errorf(CodeBadParams, "spend.ledger: this page is %d bytes, more than the %d one answer may carry; %snarrow the filter — a model, a run, or a shorter range — because a single run that used very many models is one row however small the page", len(data), SpendLedgerReplyMax, fewer)
}

// Bound strings BEFORE json.Marshal allocates its complete encoded buffer. A
// page may name a very long held title even though its usage rows were small.
// Reserve a conservative fixed allowance for field names, numbers and times;
// string escaping is counted without copying the strings.
func spendLedgerFitsBeforeEncoding(result SpendLedgerResult) bool {
	if len(result.Rows) > SpendLedgerLimitMax {
		return false
	}
	remaining := SpendLedgerReplyMax - (len(result.Rows)+1)*1024
	take := func(s string) bool {
		remaining -= 2
		for len(s) > 0 && remaining >= 0 {
			r, n := utf8.DecodeRuneInString(s)
			cost := n
			switch {
			case r == '"' || r == '\\' || r == '\b' || r == '\f' || r == '\n' || r == '\r' || r == '\t':
				cost = 2
			case r < 0x20 || r == '<' || r == '>' || r == '&' || r == 0x2028 || r == 0x2029 || (r == utf8.RuneError && n == 1):
				cost = 6
			}
			remaining -= cost
			s = s[n:]
		}
		return remaining >= 0
	}
	if !take(result.From) || !take(result.To) || !take(result.Next) {
		return false
	}
	for _, row := range result.Rows {
		for _, s := range []string{row.Grain, row.Key, row.Run, row.Kind, row.Subject, row.Name, row.Session, row.Root, row.Task, row.Automation, row.Workspace} {
			if !take(s) {
				return false
			}
		}
		for _, model := range row.Models {
			if !take(model) {
				return false
			}
		}
	}
	return remaining >= 0
}

// ── the file ────────────────────────────────────────────────────────────────

// SpendLedgerCSVHeader is spend.ledger.export's columns, in order. The first
// eight are the ones a person reads; by is always empty, because nothing in
// the ledger records who asked for a call, and the rest say the grain and the
// ids a row can be traced back by.
var SpendLedgerCSVHeader = []string{
	"time", "kind", "name", "models", "tokens_in", "tokens_out", "cost_usd", "by",
	"grain", "run", "key", "subject", "session", "root", "task", "automation", "workspace",
	"end_time", "records", "calls", "unpriced_records", "unbilled_records", "unnamed_model_records",
}

// SpendLedgerCSVRecord is one row as its CSV record. name falls back to the
// subject's own id, and then to the run's, when nothing holds a title; a cost
// nobody could price is an empty cell. Every text cell that a spreadsheet
// would read as a formula is written with a leading apostrophe.
//
// THE COST IS WRITTEN AT ITS FULL PRECISION, NOT ROUNDED TO FOUR PLACES. Most
// calls cost less than a hundredth of a cent, so a column of four-decimal
// figures would sum to a different bill from the one the totals state —
// money lost in the file without a word. The shortest decimal that reads back
// as the same number is written; a spreadsheet can round it for display.
func SpendLedgerCSVRecord(row SpendLedgerRow) []string {
	name := row.Name
	if name == "" {
		name = row.Subject
	}
	if name == "" {
		name = row.Run
	}
	cost := ""
	if row.CostUSD != nil {
		cost = strconv.FormatFloat(*row.CostUSD, 'f', -1, 64)
	}
	count := func(n int) string { return strconv.Itoa(n) }
	return []string{
		row.Time.Format(time.RFC3339), row.Kind, csvText(name), csvText(strings.Join(row.Models, ";")),
		strconv.FormatInt(row.TokensIn, 10), strconv.FormatInt(row.TokensOut, 10), cost, "",
		row.Grain, csvText(row.Run), row.Key, csvText(row.Subject), csvText(row.Session), csvText(row.Root),
		csvText(row.Task), csvText(row.Automation), csvText(row.Workspace),
		row.End.Format(time.RFC3339), count(row.Records), count(row.Calls),
		count(row.UnpricedRecords), count(row.UnbilledRecords), count(row.UnnamedModelRecords),
	}
}

// csvText keeps a cell from being read as a formula by a spreadsheet.
func csvText(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

// NewSpendLedgerCSV is a CSV writer with the header already written.
func NewSpendLedgerCSV(w *csv.Writer) error {
	return w.Write(SpendLedgerCSVHeader)
}
