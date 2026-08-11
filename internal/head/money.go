package head

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// Money is computed, never spoken into existence (13.3's head edge, under
// 5.23's ordering law).
//
// The failure: a charter proposal offered to run something "at $20.00 a run"
// against a measured cost of $0.0017. Ten thousand times the truth, in the one
// sentence a person is being asked to consent to spending on. Nothing in the
// machinery had computed $20.00; a model had written it, in prose, beside a
// number it had been shown.
//
// 5.23's ordering law is deterministic first, scribe second, frontier last, and
// it says out loud what this file exists to enforce: anything a template can say
// stays a template, because free beats ultra-cheap and both beat a frontier
// model doing arithmetic in a sentence. Money is the extreme case of that rule
// rather than an exception to it — an invented figure about tokens is a wrong
// answer, and an invented figure about dollars is a wrong answer the person acts
// on.
//
// So every money sentence in this package is assembled the same way: a figure is
// COMPUTED here from journaled rows and rendered here into words, and the model
// is handed the finished figure to write a sentence around. Where a model's own
// number could previously survive into a rail or a receipt, it no longer can.

// moneyUSD renders one amount at the precision it actually has.
//
// Two decimals is the product's ordinary spelling and it is exactly wrong for
// this job: a real measured cost of $0.0017 rendered at two decimals is "$0.00",
// which reads as free. A figure that rounds a true cost to nothing invites the
// same fabrication from the other end — a model shown "$0.00 a run" has been
// told the measurement is meaningless and will reach for a number that is not.
//
// Cents for anything a person would think of in cents, four decimals below that,
// and a floor that says "small" in words rather than pretending to a precision
// the journal does not have.
func moneyUSD(amount float64) string {
	switch {
	case math.IsNaN(amount) || math.IsInf(amount, 0):
		return "not measured"
	case amount <= 0:
		return "$0.00"
	case amount >= 0.01:
		return fmt.Sprintf("$%.2f", amount)
	case amount >= 0.0001:
		return fmt.Sprintf("$%.4f", amount)
	default:
		return "under $0.0001"
	}
}

// measuredCostPattern reads a per-run cost out of the measured self-knowledge
// block. The spellings are the ones that block actually writes ("avg cost
// $0.0017"), plus the bare "cost: $x" a briefer rendering uses.
var measuredCostPattern = regexp.MustCompile(`(?i)(?:avg(?:erage)?\s+cost|cost)\s*[:=]?\s*\$([0-9]+(?:\.[0-9]+)?)`)

// measuredStandingCost is what one firing of a standing rule should be priced
// at, read off the measured history rather than guessed.
//
// It takes the MEDIAN of every measurement in the block rather than the first,
// and the reason is what the block contains: one line per specialist, each with
// its own history, because a median that averaged a coding pipeline with a
// lookup would describe neither. Reading only the first line meant the price of
// every standing rule was set by whichever worker happened to sort first.
func measuredStandingCost(context string) (float64, bool) {
	matches := measuredCostPattern.FindAllStringSubmatch(context, -1)
	costs := make([]float64, 0, len(matches))
	for _, match := range matches {
		cost, err := strconv.ParseFloat(match[1], 64)
		if err != nil || cost < 0 || math.IsNaN(cost) || math.IsInf(cost, 0) {
			continue
		}
		costs = append(costs, cost)
	}
	if len(costs) == 0 {
		return 0, false
	}
	sort.Float64s(costs)
	return costs[len(costs)/2], true
}

// standingRails is the whole of what a charter proposal may claim about money,
// and the model contributes none of it.
//
// The compiler is asked for rails and answers with them, and what it answers is
// a guess dressed as a measurement: it has been shown the measured block and it
// writes a number that looks plausible beside it. Before this, that number
// survived whenever the block carried no measurement the pattern could read, and
// its justification — free prose, with a dollar figure in it — survived always.
// Both are now discarded and recomputed, which is the only arrangement under
// which the sentence a person consents to is true.
//
// What the model is still trusted with is everything that is not arithmetic: the
// invariant, the sentinel, the action, the cadence words.
func standingRails(proposed store.CharterSpecRails, reminder bool, graphContext string) store.CharterSpecRails {
	rails := proposed
	measured, isMeasured := measuredStandingCost(graphContext)
	switch {
	case isMeasured:
		rails.EstimatedCostUSD = measured
	default:
		// No measurement means no measurement. The default is the store's own
		// backstop rather than whatever the model wrote, because a rail is a
		// promise about spending and an unmeasured guess is not one.
		rails.EstimatedCostUSD = defaultStandingCostUSD
	}
	if rails.MaxPerDay <= 0 {
		rails.MaxPerDay = defaultStandingMaxPerDay
		if reminder {
			rails.MaxPerDay = 1
		}
	}
	rails.MaxPerDayJustification = standingRailSentence(rails, reminder, isMeasured)
	return rails
}

const (
	// defaultStandingCostUSD backstops a rule proposed before anything has been
	// measured. It matches the store's own per-firing backstop, so the number a
	// proposal says and the number a charter is created with are one number.
	defaultStandingCostUSD = 0.15
	// defaultStandingMaxPerDay is the ordinary daily ceiling. A reminder gets
	// one, because one a day is all a reminder is.
	defaultStandingMaxPerDay = 10
)

// standingRailSentence is the money sentence itself, templated.
//
// It is the sentence the ratification question puts in front of the person, so
// it says three things and each of them is computed: what one run costs, how
// many runs a day are allowed, and what the worst day therefore is. It also says
// whether the rate was MEASURED or is a standing default, because "about $0.15 a
// run" read as a measurement when nothing has run yet is the same lie in a
// smaller size.
func standingRailSentence(rails store.CharterSpecRails, reminder, isMeasured bool) string {
	rate := moneyUSD(rails.EstimatedCostUSD)
	if !isMeasured {
		rate += " (nothing measured yet)"
	}
	if reminder && rails.MaxPerDay == 1 {
		return "one a day is all a reminder needs, at " + rate + " a run"
	}
	worst := rails.EstimatedCostUSD * float64(rails.MaxPerDay)
	return fmt.Sprintf("%s a run, at most %d a day, so the worst day is about %s",
		rate, rails.MaxPerDay, moneyUSD(worst))
}

// spendRateLines are the computed figures behind a money answer: what has
// actually been spent, what one run of it costs, and what the same rate comes to
// over the windows people ask about.
//
// It exists so the model never has to multiply. "What will this cost me a month"
// used to be answerable only by handing over a window total and a run count and
// hoping; the projection is arithmetic over journaled rows, which makes it a
// template's job by 5.23's ordering law, and a template cannot be off by a
// factor of ten thousand.
//
// The projections are labelled as projections and are stated as "at that rate",
// because they are what the observed rate continues to, not what will happen —
// and a number the person reads as a forecast when it is an extrapolation is the
// honesty failure this whole file is about, arriving from the other side.
func spendRateLines(window store.SpendWindow, median float64) []string {
	lines := make([]string, 0, 4)
	if window.Runs > 0 {
		lines = append(lines, fmt.Sprintf("%s over %d %s in that window, so %s a run on average",
			moneyUSD(window.Cost), window.Runs, pluralWord(window.Runs, "run", "runs"),
			moneyUSD(window.Cost/float64(window.Runs))))
	}
	if median > 0 {
		lines = append(lines, "the middle run of everything ever priced here cost "+moneyUSD(median))
	}
	days := window.Until.Sub(window.Since).Hours() / 24
	if days < 0.5 || window.Cost <= 0 {
		return lines
	}
	daily := window.Cost / days
	lines = append(lines, fmt.Sprintf(
		"that is %s a day over those %s; at the same rate a week is %s and thirty days is %s "+
			"(computed from the rows above — say these figures, never work one out yourself)",
		moneyUSD(daily), spendDayCount(days), moneyUSD(daily*7), moneyUSD(daily*30)))
	return lines
}

// spendDayCount renders the window's length the way a person says it, so the
// rate line can quote its own basis without a second unit vocabulary.
func spendDayCount(days float64) string {
	whole := int(math.Round(days))
	if whole < 1 {
		return "less than a day"
	}
	return fmt.Sprintf("%d %s", whole, pluralWord(whole, "day", "days"))
}

// measuredRunCost is the journal's own answer to "what does one run cost here",
// or zero when nothing has been priced. Zero is returned rather than a default
// because this figure is quoted as a measurement, and a default quoted as a
// measurement is the fabrication in miniature.
func (h *Head) measuredRunCost() float64 {
	if h == nil || h.store == nil {
		return 0
	}
	median, err := h.store.MeasuredCostPerRun()
	if err != nil || median < 0 || math.IsNaN(median) || math.IsInf(median, 0) {
		return 0
	}
	return median
}

// spendRateBlock is what the spending read hands the model in place of an
// invitation to multiply: the window's own rate, the journal's median run, and
// the projections that rate continues to.
//
// A caller that named no window still gets one. "What does this cost me" is a
// question about a span even when it is asked without one, and today's total has
// no span to project from — so an unbounded read projects from the last thirty
// days, which is the window that question is actually about.
func (h *Head) spendRateBlock(since, until time.Time) []string {
	if h == nil || h.store == nil {
		return nil
	}
	if since.IsZero() && until.IsZero() {
		now := time.Now()
		since, until = now.AddDate(0, 0, -30), now
	}
	window, err := h.store.SpendBetween(since, until)
	if err != nil {
		return nil
	}
	median := h.measuredRunCost()
	lines := spendRateLines(window, median)
	if len(lines) == 0 {
		return []string{noMoneyFigure}
	}
	return lines
}

// noMoneyFigure is what a money answer says when the journal holds nothing. It
// is a sentence rather than a zero because "nothing has been measured" and "it
// costs nothing" are different facts and only one of them is true.
const noMoneyFigure = "nothing priced has run here yet, so there is no rate to quote — say that rather than estimating one"

// containsMoney reports whether a string carries a dollar figure. It is used by
// the tests that pin this file's whole purpose: that no money sentence leaves
// this package carrying a figure a model wrote.
func containsMoney(text string) bool { return strings.Contains(text, "$") }
