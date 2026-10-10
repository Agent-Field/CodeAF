package decide

import (
	"fmt"
	"strings"
)

// Confidence is measured here, before any model is asked. The judge role is a
// separate call that runs only when this result sits under the place's
// threshold; this function never makes that call.

const (
	// ThresholdDefault is what a place uses until somebody sets another.
	// The place menu calls that row "Decision confidence…".
	ThresholdDefault = 90
	// ThresholdMin and ThresholdMax are the ends of that menu. A number
	// outside them is not a threshold, and it does not authorise a decision.
	ThresholdMin = 50
	ThresholdMax = 100

	// HistoryFloor is the smallest sample the ratio treats as complete. Fewer
	// asks are scored as if this many had been asked, so two agreements cannot
	// read like a habit. Six is the sample the Why? line is written from.
	HistoryFloor = 6

	// KnowsMatchPercent is what one live knows line that supports the answer
	// is worth. The learning proposal draws it as "92% · matches what
	// Marketing knows". More lines do not stack past that figure; a longer
	// history is what climbs.
	KnowsMatchPercent = 92
)

// irreversibleCeiling sits one under the lowest threshold a place may set, so
// the number fails percent >= threshold for every threshold from 50 to 100.
const irreversibleCeiling = ThresholdMin - 1

// Stakes are the same three words a question already uses. This package does
// not import the session engine to borrow the type.
const (
	StakesReversible   = "reversible"
	StakesCostly       = "costly"
	StakesIrreversible = "irreversible"
)

// Proposal is the answer a place would give, and the words the because line
// is built from. Kind and Subject select the history; Choice is the answer
// counted as "the same". Said is the past tense in the sentence ("allowed").
// PlaceName is the place in "matches what Marketing knows".
type Proposal struct {
	Kind      string
	Subject   string
	Choice    string
	Said      string
	Stakes    string
	PlaceName string
}

// Answer is one earlier reply of a question in this place. Answers of another
// kind, or about another subject, are not this cohort.
type Answer struct {
	ID      string
	Kind    string
	Subject string
	Choice  string
}

// Knows is one line the place holds. Supports is the caller's statement that
// the line backs this answer; this scorer does not read the prose and decide
// that for itself. A replaced line never counts.
type Knows struct {
	ID       string
	Supports bool
	Replaced bool
}

// Evidence is the history and the knows lines for one scoring.
type Evidence struct {
	Answers []Answer
	Knows   []Knows
}

// Result is the measurement. Percent 0 with an empty Because is nothing
// measured (the emptiness law). Basis is the ids the percent rests on:
// agreeing answers, and a knows line when that line is what the percent used
// or what it was weighed beside.
type Result struct {
	Percent int      `json:"percent"`
	Because string   `json:"because,omitempty"`
	Basis   []string `json:"basis,omitempty"`
}

// Decides reports whether this result may answer without the person.
// A threshold of 0 is the default, 90. A threshold outside 50–100 does not
// decide. An irreversible result is capped under 50, so it fails here for
// every legal threshold.
func (r Result) Decides(threshold int) bool {
	if threshold == 0 {
		threshold = ThresholdDefault
	}
	if threshold < ThresholdMin || threshold > ThresholdMax {
		return false
	}
	return r.Percent >= threshold && r.Percent <= 100
}

// Score is the deterministic confidence of p given ev.
func Score(p Proposal, ev Evidence) Result {
	same, asked, answerIDs := cohort(p, ev.Answers)
	history := historyPercent(same, asked)
	supported, lineIDs := supportingKnows(ev.Knows)
	knows := 0
	if supported {
		knows = KnowsMatchPercent
	}
	percent := history
	if knows > percent {
		percent = knows
	}
	if percent > 100 {
		percent = 100
	}
	irreversible := strings.TrimSpace(p.Stakes) == StakesIrreversible
	if irreversible && percent > irreversibleCeiling {
		percent = irreversibleCeiling
	}

	var because string
	var basis []string
	switch {
	case history >= knows && history > 0:
		because = historyBecause(p, same)
		basis = joinIDs(answerIDs, lineIDs)
	case supported:
		because = knowsBecause(p.PlaceName)
		if irreversible {
			because += ". Irreversible."
		}
		basis = lineIDs
	}
	return Result{Percent: percent, Because: because, Basis: basis}
}

// historyPercent is 100 × same / asked, with two holds on that ratio.
//
// The denominator is at least HistoryFloor, so a short run is not a full
// sample. A fifth of a count is always added as well, which is what keeps
// six of six at 97 rather than 100. A much longer unanimous run can still
// round to 100. In integers the specimen is
// round(500 × same / (5 × max(asked, HistoryFloor) + 1)).
func historyPercent(same, asked int) int {
	if same <= 0 || asked <= 0 {
		return 0
	}
	if same > asked {
		same = asked
	}
	n := asked
	if n < HistoryFloor {
		n = HistoryFloor
	}
	den := 5*n + 1
	return (500*same + den/2) / den
}

// cohort counts answers of this kind about this subject. n asked is that
// count; n same is how many of them chose the proposal. When the proposal
// names no subject, every answer of the kind is in the cohort.
func cohort(p Proposal, answers []Answer) (same, asked int, ids []string) {
	kind := strings.TrimSpace(p.Kind)
	subject := strings.TrimSpace(p.Subject)
	choice := strings.TrimSpace(p.Choice)
	seen := map[string]bool{}
	for _, a := range answers {
		if strings.TrimSpace(a.Kind) != kind {
			continue
		}
		if subject != "" && strings.TrimSpace(a.Subject) != subject {
			continue
		}
		asked++
		if choice == "" || strings.TrimSpace(a.Choice) != choice {
			continue
		}
		same++
		id := strings.TrimSpace(a.ID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	return same, asked, ids
}

func supportingKnows(lines []Knows) (bool, []string) {
	seen := map[string]bool{}
	var ids []string
	supported := false
	for _, l := range lines {
		if !l.Supports || l.Replaced {
			continue
		}
		supported = true
		id := strings.TrimSpace(l.ID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	return supported, ids
}

func historyBecause(p Proposal, same int) string {
	said := strings.TrimSpace(p.Said)
	if said == "" {
		said = strings.TrimSpace(p.Choice)
	}
	subject := strings.TrimSpace(p.Subject)
	if said == "" || subject == "" || same <= 0 {
		return ""
	}
	unit := "times"
	if same == 1 {
		unit = "time"
	}
	return fmt.Sprintf("You %s %s here %d %s. %s", said, subject, same, unit, stakesSentence(p.Stakes))
}

func knowsBecause(place string) string {
	place = strings.TrimSpace(place)
	if place == "" {
		place = "this place"
	}
	return "matches what " + place + " knows"
}

func stakesSentence(stakes string) string {
	switch strings.TrimSpace(stakes) {
	case StakesIrreversible:
		return "Irreversible."
	case StakesCostly:
		return "Costly."
	default:
		return "Reversible."
	}
}

func joinIDs(a, b []string) []string {
	if len(a) == 0 && len(b) == 0 {
		return nil
	}
	out := make([]string, 0, len(a)+len(b))
	out = append(out, a...)
	out = append(out, b...)
	return out
}
