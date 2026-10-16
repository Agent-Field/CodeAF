package delegate

// THE ENDING'S FACTS: the part of a terminal record's data that a surface may
// read from the action log without parsing a sentence.
//
// The log's last line used to keep a program's status and one sentence, and the
// rest of the terminal stayed with the run. A surface that wanted what
// senior-dev claimed beside what it observed, or how many findings a review
// held, had to take them back out of prose. The `end` line now carries a typed
// copy of exactly the keys below ([EndFacts]) and nothing else.
//
// AN ALLOW-LIST, NOT A FILTER. A program's data is its own: it may hold a
// transcript, a path, a token, a structure nobody here has seen. The copy names
// each key it keeps and checks each value's shape; a key of the wrong shape is
// DROPPED, NEVER COERCED, and a key that is not on the list is never read. An
// absent fact stays absent — a count that is not there is not zero, and a claim
// that is not there is not "".
//
// PURELY ADDITIVE. The line's status and message, and every vocabulary's
// reading of them, are untouched; an older log has no data on its `end` line and
// reads as before, and an older reader ignores the field.

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
)

// The bounds of the copy. Each is the most a key may hold; EndDataCap bounds the
// whole object as it is written.
const (
	// EndDataCap is the most bytes an `end` line's data may take, in JSON.
	EndDataCap = 16 << 10
	// endValueCap is the most bytes of one key's raw value that is even
	// decoded: a bigger one is left alone, which is also what keeps a deeply
	// nested or enormous value from being walked at all.
	endValueCap = 32 << 10
	// endTextCap is the most bytes of one sentence-length text kept.
	endTextCap = turnTextCap
	// endReportsMax and endReportCap bound the list of report files.
	endReportsMax = 16
	endReportCap  = 256
	// endFailuresMax bounds the hunts a coverage account names. An account
	// that names more is kept as unknown, never cut: sixteen of twenty failures
	// is a different account from twenty of twenty.
	endFailuresMax = 16
	endReasonCap   = 240
	// endCountMax is the largest count kept. A program that says more has said
	// something else.
	endCountMax = 1 << 30
)

// EndFacts is what an ending says that a surface may read as data. Every field
// is a pointer or empty when the program did not say it.
type EndFacts struct {
	// Claim is what the program's model said it did, and Observed what the
	// program itself checked. They are two facts, and a surface keeps them apart.
	Claim    string `json:"claim,omitempty"`
	Observed string `json:"observed,omitempty"`
	// Submitted says the program handed in a change of its own.
	Submitted *bool `json:"submitted,omitempty"`
	// SubmissionReason, InnerStatus, VerificationFailing and VerificationCommands
	// are senior-dev's own spelling of its claim and its check, kept as it wrote
	// them so a surface can use the mappers [Terminal.Claim] and
	// [Terminal.Observed] already have. InnerStatus is the program's verdict on
	// its own work (`pass`, `pass-unverified`, `fail`) and is NOT the run's
	// status, which is the line's own; a handed-in change ends `done` whatever
	// the inner word says. A count that was not said is nil, never 0.
	SubmissionReason     string `json:"submission_reason,omitempty"`
	InnerStatus          string `json:"status,omitempty"`
	VerificationFailing  *int   `json:"verification_failing,omitempty"`
	VerificationCommands *int   `json:"verification_commands,omitempty"`
	// Reports are the report files the program wrote, as it named them.
	Reports []string `json:"reports,omitempty"`
	// The counts. A review says Findings and Blocking; an audit says Confirmed,
	// Likely, Inconclusive and RuledOut.
	Findings     *int `json:"findings,omitempty"`
	Blocking     *int `json:"blocking,omitempty"`
	Confirmed    *int `json:"confirmed,omitempty"`
	Likely       *int `json:"likely,omitempty"`
	Inconclusive *int `json:"inconclusive,omitempty"`
	RuledOut     *int `json:"ruled_out,omitempty"`
	// Partial says a review's report may be missing parts. AnalysisIncomplete is
	// an audit's own sentence for why it stopped short.
	Partial            *bool  `json:"partial,omitempty"`
	AnalysisIncomplete string `json:"analysis_incomplete,omitempty"`
	// HuntCoverage is an audit's account of the hunts that did not finish.
	HuntCoverage *HuntCoverage `json:"hunt_coverage,omitempty"`
}

// HuntCoverage is how many hunts an audit attempted and which failed, as the
// audit measured them. It is either Unknown alone or a whole account — an
// Attempted of at least one and every failure, never more failures than
// attempts — which is exactly what sec's own reader (schemas.Coverage) accepts.
type HuntCoverage struct {
	Attempted *int          `json:"attempted,omitempty"`
	Failures  []HuntFailure `json:"failures,omitempty"`
	// Unknown marks accounting the audit could not read.
	Unknown bool `json:"unknown,omitempty"`
}

// MarshalJSON writes the account in the only two shapes it reads back as:
// `{"unknown":true}` alone, or an attempted count with its failures ALWAYS AS A
// LIST. An omitted empty list would read back as missing failures, which is
// unknown, so "none of four failed" would become "could not be read" on the
// next read.
func (c HuntCoverage) MarshalJSON() ([]byte, error) {
	if c.Unknown || c.Attempted == nil {
		return []byte(`{"unknown":true}`), nil
	}
	failures := c.Failures
	if failures == nil {
		failures = []HuntFailure{}
	}
	return json.Marshal(struct {
		Attempted int           `json:"attempted"`
		Failures  []HuntFailure `json:"failures"`
	}{*c.Attempted, failures})
}

// HuntFailure is one hunt that failed and the reason it gave.
type HuntFailure struct {
	Strategy string `json:"strategy"`
	Reason   string `json:"reason"`
}

// Empty says the copy holds nothing.
func (f EndFacts) Empty() bool {
	return f.Claim == "" && f.Observed == "" && f.Submitted == nil && len(f.Reports) == 0 &&
		f.SubmissionReason == "" && f.InnerStatus == "" && f.VerificationFailing == nil && f.VerificationCommands == nil &&
		f.Findings == nil && f.Blocking == nil && f.Confirmed == nil && f.Likely == nil &&
		f.Inconclusive == nil && f.RuledOut == nil && f.Partial == nil &&
		f.AnalysisIncomplete == "" && f.HuntCoverage == nil
}

// endFactsOf is the allow-listed copy of a terminal's data, nil when nothing on
// the list was there in a usable shape.
func endFactsOf(data map[string]json.RawMessage) *EndFacts {
	var facts EndFacts
	facts.Claim = endText(data["claim"])
	facts.Observed = endText(data["observed"])
	facts.Submitted = endBool(data["submitted"])
	facts.SubmissionReason = endText(data["submission_reason"])
	facts.InnerStatus = label(endText(data["status"]))
	facts.VerificationFailing = endCount(data["verification_failing"])
	facts.VerificationCommands = endCount(data["verification_commands"])
	facts.Reports = endReports(data["reports"])
	facts.Findings = endCount(data["findings"])
	facts.Blocking = endCount(data["blocking"])
	facts.Confirmed = endCount(data["confirmed"])
	facts.Likely = endCount(data["likely"])
	facts.Inconclusive = endCount(data["inconclusive"])
	facts.RuledOut = endCount(data["ruled_out"])
	facts.Partial = endBool(data["partial"])
	facts.AnalysisIncomplete = endText(data["analysis_incomplete"])
	if raw, present := data["hunt_coverage"]; present {
		facts.HuntCoverage = endCoverage(raw)
	}
	if facts.Empty() {
		return nil
	}
	return &facts
}

// endDataOf is the `end` line's data as it is written: the allow-listed copy as
// JSON, held to [EndDataCap] by dropping whole keys, the least settled first,
// and nil when there is nothing to keep. Half an object is never written.
func endDataOf(facts *EndFacts) json.RawMessage {
	if facts == nil {
		return nil
	}
	copied := *facts
	drops := []func(*EndFacts){
		// A COVERAGE IS NEVER DROPPED, ONLY BLURRED: an audit's counts with no
		// coverage beside them read as an audit whose every hunt finished, so
		// an account too big to keep is kept as unknown, which is a few bytes.
		func(f *EndFacts) {
			if f.HuntCoverage != nil {
				f.HuntCoverage = &HuntCoverage{Unknown: true}
			}
		},
		func(f *EndFacts) { f.Reports = nil },
		func(f *EndFacts) { f.Observed = "" },
		func(f *EndFacts) { f.Claim = "" },
		func(f *EndFacts) { f.SubmissionReason = "" },
		func(f *EndFacts) { f.AnalysisIncomplete = "" },
	}
	for i := 0; ; i++ {
		// MEASURED AS IT IS WRITTEN: json.Marshal escapes <, > and & the way
		// AppendAction's own marshal of this raw message will, so the cap is
		// the file's byte count and not a smaller one.
		line, err := json.Marshal(copied)
		if err != nil {
			return nil
		}
		if len(line) <= EndDataCap {
			if copied.Empty() {
				return nil
			}
			return json.RawMessage(line)
		}
		if i >= len(drops) {
			return nil
		}
		drops[i](&copied)
	}
}

// endFactsOfRaw re-reads a stored `end` line's data through the same allow-list,
// so a log edited by hand, or written by a build with another list, shows only
// what this build would have kept.
func endFactsOfRaw(raw json.RawMessage) *EndFacts {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || len(trimmed) > EndDataCap*2 || trimmed[0] != '{' {
		return nil
	}
	var data map[string]json.RawMessage
	if json.Unmarshal(trimmed, &data) != nil {
		return nil
	}
	return endFactsOf(data)
}

// Ending is the typed facts of an `end` line, nil for any other line and for an
// ending that kept none.
func (a Action) Ending() *EndFacts {
	if a.Kind != ActionEnd {
		return nil
	}
	return endFactsOfRaw(a.Data)
}

func endText(raw json.RawMessage) string {
	var s string
	if !endDecodable(raw) || json.Unmarshal(raw, &s) != nil {
		return ""
	}
	return cut(strings.TrimSpace(s), endTextCap)
}

func endBool(raw json.RawMessage) *bool {
	var b bool
	if !endDecodable(raw) || json.Unmarshal(raw, &b) != nil {
		return nil
	}
	return &b
}

// endCount is a whole, non-negative number, and nil for anything else — a
// string, a fraction, a negative, a boolean.
func endCount(raw json.RawMessage) *int {
	if !endDecodable(raw) {
		return nil
	}
	// Read as digits, not as a float: 0.99999999999999999 is not 1.
	n, err := strconv.ParseInt(string(bytes.TrimSpace(raw)), 10, 64)
	if err != nil || n < 0 || n > endCountMax {
		return nil
	}
	whole := int(n)
	return &whole
}

// endReports is a flat list of strings. A list holding anything else is
// dropped whole, because a list with an element removed is a different list.
// A NAME IS A PATH AND IS NEVER REWRITTEN: one that is empty, longer than
// [endReportCap], or that [oneLine] would change (a newline, a tab, a doubled
// or edge space) drops the whole list rather than being cut or folded into a
// path that names a different file.
func endReports(raw json.RawMessage) []string {
	var names []*string
	if !endDecodable(raw) || json.Unmarshal(raw, &names) != nil || len(names) == 0 || len(names) > endReportsMax {
		return nil
	}
	kept := make([]string, 0, len(names))
	for _, element := range names {
		// A null or empty element is a malformed list, and the whole list goes.
		if element == nil {
			return nil
		}
		name := *element
		if name == "" || len(name) > endReportCap || oneLine(name) != name {
			return nil
		}
		kept = append(kept, name)
	}
	return kept
}

// endCoverage reads an audit's hunt coverage by sec's own rules
// (schemas.Coverage), so the line never says more than the program's reader
// would. A key that is present is an account: either it reads whole, or it is
// UNKNOWN — NEVER ABSENT, NEVER A ZERO. An audit's counts beside no coverage
// read as an audit whose every hunt finished, and `{"attempted":4}` alone reads
// as none of four failing; both are claims sec itself refuses to make.
//
//   - `unknown: true` is unknown alone, whatever else the account says.
//   - Otherwise attempted must be a whole count of at least one, failures a
//     present, non-null list no longer than attempted, and every row must name
//     its strategy and its reason.
//   - Anything else — null, a non-object, a value too big to decode, a
//     malformed row, more failures than the copy keeps — is unknown.
//
// THE KEYS ARE THE PRODUCER'S, EXACTLY. sec marshals schemas.HuntCoverage, so
// its keys are always `attempted`, `failures` and `unknown`, and a row's
// `strategy` and `reason`. sec's reader matches keys without regard to case;
// this one does not guess which of two spellings would win, so a key that
// differs from one of those only in case makes the account unknown rather than
// silently ignored — an ignored `Unknown: true` would read as a whole account.
// Numbers are whole digits as everywhere here; a spelling sec's own marshal
// never writes (1.0, 1e2) is unknown, which says less than sec, never more.
func endCoverage(raw json.RawMessage) *HuntCoverage {
	unknown := &HuntCoverage{Unknown: true}
	if !endDecodable(raw) {
		return unknown
	}
	fields, ok := endObject(raw, "attempted", "failures", "unknown")
	if !ok {
		return unknown
	}
	if rawUnknown, present := fields["unknown"]; present && !isNull(rawUnknown) {
		said := endBool(rawUnknown)
		if said == nil || *said {
			return unknown
		}
	}
	attempted := endCount(fields["attempted"])
	rawFailures := fields["failures"]
	if attempted == nil || *attempted < 1 || !endDecodable(rawFailures) {
		return unknown
	}
	var rows []json.RawMessage
	if json.Unmarshal(rawFailures, &rows) != nil || rows == nil || len(rows) > *attempted || len(rows) > endFailuresMax {
		return unknown
	}
	coverage := HuntCoverage{Attempted: attempted}
	for _, rawRow := range rows {
		row, ok := endObject(rawRow, "strategy", "reason")
		if !ok {
			return unknown
		}
		var strategy, reason *string
		if json.Unmarshal(row["strategy"], &strategy) != nil || json.Unmarshal(row["reason"], &reason) != nil ||
			strategy == nil || reason == nil {
			return unknown
		}
		kept := HuntFailure{Strategy: label(*strategy), Reason: cut(oneLine(*reason), endReasonCap)}
		if kept.Strategy == "" || kept.Reason == "" {
			return unknown
		}
		coverage.Failures = append(coverage.Failures, kept)
	}
	return &coverage
}

// endObject decodes a JSON object whose keys are compared exactly, and refuses
// one that spells any of names in another case: such a key is not the
// producer's and its meaning to a case-blind reader depends on order.
func endObject(raw json.RawMessage, names ...string) (map[string]json.RawMessage, bool) {
	var fields map[string]json.RawMessage
	if !endDecodable(raw) || json.Unmarshal(raw, &fields) != nil || fields == nil {
		return nil, false
	}
	for key := range fields {
		for _, name := range names {
			if key != name && strings.EqualFold(key, name) {
				return nil, false
			}
		}
	}
	return fields, true
}

func isNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

// endDecodable says a key's value is present, small enough to read at all, and
// not JSON null. NULL IS ABSENT, NOT A ZERO: json.Unmarshal of null into a
// scalar succeeds and leaves false, 0 or "", which would invent a known false or
// a known zero count from a program that said nothing.
func endDecodable(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && len(trimmed) <= endValueCap && !bytes.Equal(trimmed, []byte("null"))
}
