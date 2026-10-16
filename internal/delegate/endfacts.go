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
	// endFailuresMax bounds the hunts a coverage account names; the count of
	// attempts is kept whole.
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
// audit measured them.
type HuntCoverage struct {
	Attempted *int          `json:"attempted,omitempty"`
	Failures  []HuntFailure `json:"failures,omitempty"`
	// Unknown marks accounting the audit could not read.
	Unknown bool `json:"unknown,omitempty"`
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
	facts.HuntCoverage = endCoverage(data["hunt_coverage"])
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
		func(f *EndFacts) { f.HuntCoverage = nil },
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
		name := cut(oneLine(*element), endReportCap)
		if name == "" {
			return nil
		}
		kept = append(kept, name)
	}
	return kept
}

func endCoverage(raw json.RawMessage) *HuntCoverage {
	if !endDecodable(raw) {
		return nil
	}
	// Exact, case-sensitive keys, as the top level reads them.
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return nil
	}
	coverage := HuntCoverage{Attempted: endCount(fields["attempted"])}
	if unknown := endBool(fields["unknown"]); unknown != nil {
		coverage.Unknown = *unknown
	}
	if coverage.Attempted == nil && !coverage.Unknown {
		return nil
	}
	if rawFailures := fields["failures"]; len(rawFailures) > 0 {
		var rows []map[string]*string
		if !endDecodable(rawFailures) || json.Unmarshal(rawFailures, &rows) != nil {
			return nil
		}
		// A row that is null, or lacks either text, would be written as a hunt
		// that failed for no reason; the account is malformed and goes whole.
		for _, row := range rows {
			if row == nil || row["strategy"] == nil || row["reason"] == nil {
				return nil
			}
			strategy, reason := label(*row["strategy"]), cut(oneLine(*row["reason"]), endReasonCap)
			if strategy == "" || reason == "" {
				return nil
			}
			if len(coverage.Failures) == endFailuresMax {
				continue // the account names the first hunts only; Attempted stays whole
			}
			coverage.Failures = append(coverage.Failures, HuntFailure{Strategy: strategy, Reason: reason})
		}
	}
	return &coverage
}

// endDecodable says a key's value is present, small enough to read at all, and
// not JSON null. NULL IS ABSENT, NOT A ZERO: json.Unmarshal of null into a
// scalar succeeds and leaves false, 0 or "", which would invent a known false or
// a known zero count from a program that said nothing.
func endDecodable(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && len(trimmed) <= endValueCap && !bytes.Equal(trimmed, []byte("null"))
}
