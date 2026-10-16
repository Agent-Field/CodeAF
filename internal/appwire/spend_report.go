package appwire

// ── THE SPENDING REPORT: the ledger added up by day, model and subject ──────
//
// spend.report is the generic aggregate beside spend.ledger: the same filter
// (local calendar days, both required, plus the same exact matches), answered
// not as rows but as CELLS — one per local day, model and subject — with the
// range's totals. A calendar, a model flow and a sparkline are all folds of
// those cells; THE ENGINE FOLDS NOTHING. There is no top three and no "Other"
// here, and a range with more cells than [SpendReportCellsMax] is refused,
// never cut.
//
// UNKNOWN IS ABSENT. The ledger does not record which provider billed a call,
// whether the task it served landed, or which crew owned it, and the engine
// does not join those from what is true today: a crew or an outcome as it is
// now is not what it was when the money was spent. A cell therefore has no such
// field, and not_recorded says so on every answer. A call that named no model
// has no model on its cell — an empty model is "nobody said", not a model
// called Unknown. A cost nobody could price is absent with unpriced_records
// saying so; it is not $0.
//
// THE TOTALS DO NOT CLAIM A COMPLETE BILL. gaps carries what the engine knows
// it is missing: records this process failed to write since it started, ledger
// lines that did not parse or were too long to hold, and lines placed by
// their instant because they carried no valid day. None of them is range
// specific except days_derived, and an answer with every gap at zero is not
// proof that none exist — a record lost by an earlier run of the program left
// no mark.
//
// AN ENGINE WITHOUT THE CAPABILITY DOES NOT HAVE THE VERB: spend.report answers
// unknown_method from an engine that is not a [SpendReporter].

import (
	"encoding/json"
	"time"
	"unicode/utf8"
)

// The most a spend.report may hold or span. The reply is checked against
// SpendReportReplyMax before it is encoded and refused, never cut.
const (
	SpendReportCellsMax = 8192
	SpendReportDaysMax  = 366
	SpendReportReplyMax = 4 << 20
)

// SpendReportNotRecorded are the dimensions a spend screen may want and the
// ledger does not hold, said on every answer so no client has to guess.
var SpendReportNotRecorded = []string{"provider", "billing_source", "outcome", "crew"}

// SpendReportParams is spend.report's params: the ledger's filter, with from
// and to required and at most 366 days apart. There is no paging.
type SpendReportParams struct {
	SpendLedgerFilter
}

// SpendReportCell is everything the ledger wrote for one local day, model and
// subject. day is the day the ledger stored on the line. root is present only
// on a task cell, because a task id alone repeats in every conversation.
type SpendReportCell struct {
	Day             string   `json:"day"`
	Model           string   `json:"model,omitempty"`
	Kind            string   `json:"kind,omitempty" enum:"agent,task,automation"`
	Subject         string   `json:"subject,omitempty"`
	Root            string   `json:"root,omitempty"`
	Session         string   `json:"session,omitempty"`
	Agent           string   `json:"agent,omitempty"`
	Records         int      `json:"records"`
	Calls           int      `json:"calls"`
	TokensIn        int64    `json:"tokens_in"`
	TokensOut       int64    `json:"tokens_out"`
	CostUSD         *float64 `json:"cost_usd,omitempty"`
	UnpricedRecords int      `json:"unpriced_records,omitempty"`
	UnbilledRecords int      `json:"unbilled_records,omitempty"`
}

// SpendReportTotals is the whole filtered range, exactly. calls counts provider
// requests and records counts ledger lines; they are not the same number.
// runless_records is the lines that carry no run identity.
type SpendReportTotals struct {
	Cells           int      `json:"cells"`
	Records         int      `json:"records"`
	Calls           int      `json:"calls"`
	TokensIn        int64    `json:"tokens_in"`
	TokensOut       int64    `json:"tokens_out"`
	CostUSD         *float64 `json:"cost_usd,omitempty"`
	RunlessRecords  int      `json:"runless_records"`
	UnpricedRecords int      `json:"unpriced_records"`
	UnbilledRecords int      `json:"unbilled_records"`
}

// SpendReportGaps is what the engine knows the totals are missing.
type SpendReportGaps struct {
	// DroppedSinceStart is spending records this process failed to write; it is
	// not about the range and not about earlier runs of the program.
	DroppedSinceStart int64 `json:"dropped_since_start"`
	Malformed         int   `json:"malformed"`
	Oversized         int   `json:"oversized"`
	// DaysDerived is lines that carried no valid day and were placed by their
	// instant in the engine's zone.
	DaysDerived int `json:"days_derived"`
}

// SpendReportLimits are the bounds the answer was made under.
type SpendReportLimits struct {
	MaxCells int   `json:"max_cells"`
	MaxBytes int64 `json:"max_bytes"`
}

// SpendReportResult is spend.report's answer. cells are sorted by day, model,
// kind, subject, root. models are the distinct named models in the cells.
type SpendReportResult struct {
	From        string            `json:"from"`
	To          string            `json:"to"`
	Totals      SpendReportTotals `json:"totals"`
	Cells       []SpendReportCell `json:"cells"`
	Models      []string          `json:"models"`
	Gaps        SpendReportGaps   `json:"gaps"`
	NotRecorded []string          `json:"not_recorded"`
	Limits      SpendReportLimits `json:"limits"`
}

// SpendReportAsk is spend.report as the engine is handed it: a checked filter.
type SpendReportAsk struct {
	Filter SpendLedgerFilter
}

// SpendReporter is the engine's half of the report. It is optional: an
// [Engine] that is not one refuses spend.report as unknown_method.
type SpendReporter interface {
	SpendReport(ask SpendReportAsk) (SpendReportResult, error)
}

func init() {
	methods["spend.report"] = spendReportMethod
}

var spendReportMethod = shaped[SpendReportParams](func(e Engine, raw json.RawMessage) (*SpendReportResult, error) {
	r, ok := e.(SpendReporter)
	if !ok {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", "spend.report")
	}
	var p SpendReportParams
	if err := decodeStrict("spend.report", raw, &p); err != nil {
		return nil, err
	}
	filter, err := checkSpendLedgerFilter(p.SpendLedgerFilter)
	if err != nil {
		return nil, err
	}
	from, _ := time.Parse("2006-01-02", filter.From)
	to, _ := time.Parse("2006-01-02", filter.To)
	if int(to.Sub(from).Hours()/24)+1 > SpendReportDaysMax {
		return nil, Errorf(CodeBadParams, "spend.report covers at most %d days; ask for a shorter range", SpendReportDaysMax)
	}
	result, err := r.SpendReport(SpendReportAsk{Filter: filter})
	if err != nil {
		return nil, err
	}
	result.Cells = nonNil(result.Cells)
	result.Models = nonNil(result.Models)
	result.NotRecorded = SpendReportNotRecorded
	result.Limits = SpendReportLimits{MaxCells: SpendReportCellsMax, MaxBytes: SpendReportReplyMax}
	if err := checkSpendReportReply(result); err != nil {
		return nil, err
	}
	return &result, nil
})

// checkSpendReportReply refuses an answer too large to carry. The strings are
// counted before json.Marshal allocates its buffer, with a fixed allowance per
// cell for field names, numbers and punctuation.
func checkSpendReportReply(result SpendReportResult) error {
	tooLarge := Errorf(CodeBadParams, "spend.report: this answer exceeds the %d byte encoding budget; narrow the filter — a model, a subject or a shorter range", SpendReportReplyMax)
	if len(result.Cells) > SpendReportCellsMax {
		return tooLarge
	}
	remaining := SpendReportReplyMax - (len(result.Cells)+1)*512
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
	for _, cell := range result.Cells {
		for _, s := range []string{cell.Day, cell.Model, cell.Kind, cell.Subject, cell.Root, cell.Session, cell.Agent} {
			if !take(s) {
				return tooLarge
			}
		}
	}
	for _, model := range result.Models {
		if !take(model) {
			return tooLarge
		}
	}
	if remaining < 0 {
		return tooLarge
	}
	data, err := json.Marshal(result)
	if err != nil {
		return Errorf(CodeEngine, "spend.report: the answer could not be written as JSON: %v", err)
	}
	if len(data) > SpendReportReplyMax {
		return tooLarge
	}
	return nil
}
