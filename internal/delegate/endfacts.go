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
	"math"
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
	Attempted int           `json:"attempted"`
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
		func(f *EndFacts) { f.AnalysisIncomplete = "" },
	}
	for i := 0; ; i++ {
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		if enc.Encode(copied) != nil {
			return nil
		}
		if line := bytes.TrimSpace(buf.Bytes()); len(line) <= EndDataCap {
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
	var n float64
	if !endDecodable(raw) || json.Unmarshal(raw, &n) != nil {
		return nil
	}
	if n < 0 || n > endCountMax || n != math.Trunc(n) {
		return nil
	}
	whole := int(n)
	return &whole
}

// endReports is a flat list of strings. A list holding anything else is
// dropped whole, because a list with an element removed is a different list.
func endReports(raw json.RawMessage) []string {
	var names []string
	if !endDecodable(raw) || json.Unmarshal(raw, &names) != nil {
		return nil
	}
	kept := make([]string, 0, min(len(names), endReportsMax))
	for _, name := range names {
		if len(kept) == endReportsMax {
			break
		}
		if name = cut(oneLine(name), endReportCap); name != "" {
			kept = append(kept, name)
		}
	}
	if len(kept) == 0 {
		return nil
	}
	return kept
}

func endCoverage(raw json.RawMessage) *HuntCoverage {
	if !endDecodable(raw) {
		return nil
	}
	var fields struct {
		Attempted *float64 `json:"attempted"`
		Failures  []struct {
			Strategy string `json:"strategy"`
			Reason   string `json:"reason"`
		} `json:"failures"`
		Unknown bool `json:"unknown"`
	}
	if json.Unmarshal(raw, &fields) != nil {
		return nil
	}
	coverage := HuntCoverage{Unknown: fields.Unknown}
	if fields.Attempted != nil {
		n := *fields.Attempted
		if n < 0 || n > endCountMax || n != math.Trunc(n) {
			return nil
		}
		coverage.Attempted = int(n)
	} else if !coverage.Unknown {
		return nil
	}
	for _, failure := range fields.Failures {
		if len(coverage.Failures) == endFailuresMax {
			break
		}
		coverage.Failures = append(coverage.Failures, HuntFailure{
			Strategy: label(failure.Strategy), Reason: cut(oneLine(failure.Reason), endReasonCap)})
	}
	return &coverage
}

// endDecodable says a key's value is present and small enough to read at all.
func endDecodable(raw json.RawMessage) bool {
	return len(raw) > 0 && len(raw) <= endValueCap
}
