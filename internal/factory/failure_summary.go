package factory

import (
	"strings"
	"time"
	"unicode/utf8"
)

// FailureSummaryMost is the most characters a failure's one-line summary
// holds ([Failure.Summary]): a headline, never the failure's whole text.
const FailureSummaryMost = 90

// FailureSummary is s as a failure's headline: one line, its whitespace
// folded, at most [FailureSummaryMost] characters, cut at a word with `…`
// when it is longer. "" stays "".
func FailureSummary(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= FailureSummaryMost {
		return s
	}
	r := []rune(s)[:FailureSummaryMost-1]
	cut := string(r)
	if i := strings.LastIndex(cut, " "); i > FailureSummaryMost/2 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " ·,;:") + "…"
}

// SetFailureSummary puts line on the job's failure and on the failed step's,
// when the failure is still the one at (the person may have run the job again
// since) and has no summary yet. It reports whether it wrote one.
func SetFailureSummary(it *Item, at time.Time, line string) bool {
	line = FailureSummary(line)
	f := it.Failure
	if line == "" || f == nil || !f.At.Equal(at) || f.Summary != "" {
		return false
	}
	f.Summary = line
	if it.Stream != nil {
		for i := range it.Stream.Phases {
			if pf := it.Stream.Phases[i].Failure; pf != nil && pf.Step == f.Step && pf.At.Equal(at) {
				pf.Summary = line
			}
		}
	}
	return true
}

// FailureSummaryAsk is what the job's manager is asked when the step that
// failed gave no summary: one line from the failure's own words, nothing else.
const FailureSummaryAsk = "A step of this job failed. Write ONE line of at most 90 characters a person reads first, saying what passed and what failed, in plain words, from the failure below only. Answer with the line alone: no quotes, no tools.\n\nStep: %s\nFailure: %s"
