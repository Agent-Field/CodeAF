package main

import (
	"fmt"
	"io"
	"math"
	"sort"
	"time"

	"github.com/Agent-Field/codeaf/internal/callrows"
	"github.com/Agent-Field/codeaf/internal/lane"
)

// ── THE REPORT ──────────────────────────────────────────────────────────────
//
// Markdown, because the census beside it is markdown and both end up pasted into
// a design document or a pull request. The order is the order somebody should
// read it in: what was read, how good the estimate is, the table, and only then
// the moments that hurt — a reader who stops after the second section has
// stopped in the right place if the estimate turns out to be worthless.

// report writes the whole thing.
func report(out io.Writer, look settings, rows []callrows.Row, journal []seen) {
	measured := newWorld(rows, look.window)
	requests, unpaired := requestsOf(rows)
	halfLife := changeHalfLife(measured)
	floor := processNoise(measured)
	light := spotlightOf(requests, measured, look, look.incidents)
	stream := momentsOf(rows, requests, journal)

	fmt.Fprintln(out, "# The chooser, replayed")
	fmt.Fprintln(out)
	whatWasRead(out, look, rows, requests, journal, measured, unpaired, halfLife, floor)
	howGoodTheEstimateIs(out, requests, measured, look)

	said := map[string]answers{}
	for _, build := range candidates(halfLife, floor) {
		policy := build()
		said[policy.Name()] = pass(policy, stream, len(requests))
	}
	bills := bill(requests, said, measured, look, light)
	theTable(out, bills)
	theMomentsThatHurt(out, light)
	whatTheRolesWere(out, requests)
}

// candidates are the four policies, each built immediately before its own pass.
//
// THEY ARE CONSTRUCTORS AND NOT VALUES because two of them install themselves
// into [lane.Default] — the process has one registry and no exported way to make
// a second — so a candidate must not exist until the moment its own walk begins.
func candidates(halfLife time.Duration, floor float64) []func() Policy {
	return []func() Policy{
		func() Policy { return servedPolicy{} },
		func() Policy { return newDevChooser("current", 0) },
		func() Policy { return newQuantilePolicy(halfLife) },
		func() Policy { return newDevChooser("current+Q", floor) },
	}
}

func whatWasRead(out io.Writer, look settings, rows []callrows.Row, requests []asked, journal []seen, measured *world, unpaired int, halfLife time.Duration, floor float64) {
	first, last := span(rows)
	fmt.Fprintln(out, "## What was read")
	fmt.Fprintln(out)
	fmt.Fprintf(out, "- `%s` — %s lines, %s to %s\n", look.log, count(len(rows)), stamp(first), stamp(last))
	fmt.Fprintf(out, "- %s finished attempts: %s answers, %s refusals, %s cut off\n",
		count(measured.clean+measured.refusals+measured.censored), count(measured.clean), count(measured.refusals), count(measured.censored))
	guessed := 0
	for _, one := range requests {
		if one.guessed {
			guessed++
		}
	}
	fmt.Fprintf(out, "- %s replayable requests; %s starts had no row under them and were skipped\n", count(len(requests)), count(unpaired))
	fmt.Fprintf(out, "- %s of those never got an answer at all, so the work they asked for is this role's typical answer on this model rather than a measurement\n", count(guessed))
	switch {
	case look.journalMissing:
		fmt.Fprintf(out, "- no sightings journal at `%s` — every candidate starts cold\n", look.journal)
	default:
		from, to := journalSpan(journal)
		fmt.Fprintf(out, "- `%s` — %s public rows spanning %s of a %s replay, ending %s. **The journal is compacted into the belief file every few hundred observations, so almost none of the replay's span has a public prior**; every candidate is therefore starting from what this log itself taught it.\n",
			look.journal, count(len(journal)), plainly(to.Sub(from)), plainly(last.Sub(first)), stamp(to))
	}
	fmt.Fprintf(out, "- the window either side of a request a machine's own answers are read in: **%s**\n", plainly(look.window))
	fmt.Fprintf(out, "- the half-life the `quantile` candidate forgets on, measured from this log's own change-point rate: **%s** (the belief's own is %s)\n",
		plainly(halfLife), plainly(lane.HalfLife))
	if floor > 0 {
		fmt.Fprintf(out, "- the variance floor the `current+Q` candidate holds, measured from how far a machine's own days differ: **%.4f nats²**\n", floor)
	} else {
		fmt.Fprintln(out, "- **no variance floor could be measured**: no machine in this log answered on two different days, so `current+Q` is the same policy as `current` and its row says nothing")
	}
	fmt.Fprintln(out)
	fmt.Fprintf(out, "**%s rows of the evidence are a lower bound.** A stream this build cut off — a hedge's losing arm, a guard's wall, a caller who walked away — measured its machine's first token and writing rate and never found out whether the answer would have been usable. Its speed counts; its finishing does not; every wait below is therefore at most as long as the real one.\n", count(measured.censored))
	fmt.Fprintln(out)
}

// howGoodTheEstimateIs is the calibration section, and it comes BEFORE the
// table on purpose.
//
// EVERY NUMBER IN THIS REPORT EXCEPT THESE IS A COUNTERFACTUAL. The one quantity
// that was both estimated from the window and observed on its own row is what
// the machine that really served actually cost, so the disagreement between the
// two is this instrument's own error, measured on the same log it is about. A
// reader who does not believe this section should not read the next one.
func howGoodTheEstimateIs(out io.Writer, requests []asked, measured *world, look settings) {
	// The counters are cleared first so that the staleness printed below is
	// exactly this pass's — one price per request, on the machine that served it.
	// Left running they would also carry the spotlight's own pricing, which is a
	// deliberately unrepresentative handful of the log's worst moments.
	measured.priced, measured.stale = 0, nil
	var errors []float64
	for _, one := range requests {
		if math.IsNaN(one.ownFelt) || one.ownFelt <= 0 || one.machine == "" {
			continue
		}
		estimated := measured.felt(lane.ID{Model: one.model, Lane: one.machine}, one.at, one.want, one.role.Visible(), one.id)
		if math.IsInf(estimated, 0) {
			continue
		}
		errors = append(errors, math.Abs(estimated-one.ownFelt)/one.ownFelt)
	}
	fmt.Fprintln(out, "## How good the estimate is")
	fmt.Fprintln(out)
	if len(errors) == 0 {
		fmt.Fprintln(out, "Nothing in this log was both estimated and observed, so nothing below is checked.")
		fmt.Fprintln(out)
		return
	}
	fmt.Fprintf(out, "The machine that really served, priced two ways — from the window, and from its own row — over %s requests:\n\n", count(len(errors)))
	fmt.Fprintf(out, "| median error | p75 | p90 |\n|---|---|---|\n| %.0f%% | %.0f%% | %.0f%% |\n\n",
		100*quantile(errors, 0.5), 100*quantile(errors, 0.75), 100*quantile(errors, 0.9))
	if measured.priced > 0 && len(measured.stale) > 0 {
		fmt.Fprintf(out, "**%s of prices were made from evidence outside the window**, because the machine answered nothing inside it; the nearest answer either side stood in, a median of %s away. A window is a preference here and not a wall — see `world.nearestAnswers` — because refusing to price those would leave the table comparing candidates only on the requests all of them happened to pick a busy machine for.\n\n",
			share(len(measured.stale), measured.priced), plainlyAge(measured.stale))
	}
	fmt.Fprintln(out, "**It is a floor on the error and not the error.** The only machine whose answer was also observed is the one that served, which by definition answered at that moment; every price in the table below is for a machine that did not, on evidence that is by construction thinner. The staleness line above is what bounds how much thinner.")
	fmt.Fprintln(out)
	fmt.Fprintf(out, "A regret smaller than the median error is noise. Read the table for differences that are several times it, and widen or narrow `-window` (now %s) to see whether a finding survives.\n\n", plainly(look.window))
}

func theTable(out io.Writer, bills map[string]map[string]*tally) {
	fmt.Fprintln(out, "## Regret")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Regret is the seconds a person or a task waited beyond the best machine available at that moment, for that role, on that model — the same quantity the chooser itself ranks by (`rolePatience.expected`), read off the log instead of off a belief. The oracle's own regret is zero by construction and is the row nobody can beat.")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "**Every row is scored over the same requests, and that set is the intersection of all of them.** A candidate that gave no opinion, or whose demand the world could not price, takes that request out of the set for everybody — which is the only way four policies can be compared at all, and also the reason the `scored` column is a fraction of the log rather than the whole of it. The `no opinion` and `unpriced` columns say who narrowed it. Dropping a candidate from a run therefore moves every other row: the widest-declining candidate is the one setting the size of the comparison.")
	fmt.Fprintln(out)
	for _, class := range classes {
		fmt.Fprintf(out, "### %s\n\n", class)
		fmt.Fprintln(out, "| policy | scored | mean regret | median | p90 | total | agreed with what served | no opinion | unpriced | switches | cache forfeited |")
		fmt.Fprintln(out, "|---|---|---|---|---|---|---|---|---|---|---|")
		for _, name := range order {
			held := bills[name][class]
			if held == nil || held.scored == 0 {
				continue
			}
			fmt.Fprintf(out, "| `%s` | %s | %.2fs | %.2fs | %.2fs | %s | %s | %s | %s | %s | %s tok |\n",
				name, count(held.scored), mean(held.regret), quantile(held.regret, 0.5), quantile(held.regret, 0.9),
				plainly(time.Duration(sum(held.regret))*time.Second),
				share(held.held, held.asked), share(held.silent, held.asked), share(held.unpriced, held.asked),
				count(held.switches), count(held.forfeited))
		}
		fmt.Fprintln(out)
	}
}

// order is the order the candidates are printed in: the baseline, the shipped
// chooser, then the two questions being asked of it.
var order = []string{"served", "current", "quantile", "current+Q"}

func theMomentsThatHurt(out io.Writer, light *spotlight) {
	fmt.Fprintln(out, "## The moments that hurt")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "The requests where the machine that answered was furthest from the best one available, one per machine per minute. What each candidate would have demanded at exactly that moment is beside it, with what that machine was measurably doing then.")
	fmt.Fprintln(out)
	if len(light.worst) == 0 {
		fmt.Fprintln(out, "Nothing in this log could be priced both ways.")
		fmt.Fprintln(out)
		return
	}
	for _, one := range light.worst {
		fmt.Fprintf(out, "### %s · %s · %s · %s\n\n", stamp(one.at), one.model, named(one.role), one.class)
		fmt.Fprintf(out, "Asked for `%s`, served by `%s` at %s. The best machine then was `%s` at %s — %s of it was avoidable.\n\n",
			blankOr(one.asked), one.machine, seconds(one.servedAt), one.best, seconds(one.bestAt), seconds(one.regret))
		fmt.Fprintln(out, "| policy | would have demanded | expected wait |")
		fmt.Fprintln(out, "|---|---|---|")
		for _, name := range order {
			demand, answered := one.demands[name]
			if !answered {
				fmt.Fprintf(out, "| `%s` | — | no opinion |\n", name)
				continue
			}
			fmt.Fprintf(out, "| `%s` | `%s` | %s |\n", name, demand.machine, seconds(demand.felt))
		}
		fmt.Fprintln(out)
	}
}

// whatTheRolesWere is the coverage of the one join this instrument had to make
// itself, printed so that a reader can see how much of the table rests on it.
func whatTheRolesWere(out io.Writer, requests []asked) {
	counted := map[string]int{}
	for _, one := range requests {
		counted[string(one.role)]++
	}
	fmt.Fprintln(out, "## What the calls were for")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "`calls.jsonl` records the call site's own word for an errand and never the role the chooser was steered by, so the two are joined by `roleOf` in this tool. **That join is the weakest thing here**: put a `role` column on the row and it can be deleted.")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "| role | requests |")
	fmt.Fprintln(out, "|---|---|")
	names := make([]string, 0, len(counted))
	for name := range counted {
		names = append(names, name)
	}
	sort.SliceStable(names, func(i, j int) bool { return counted[names[i]] > counted[names[j]] })
	for _, name := range names {
		fmt.Fprintf(out, "| %s | %s |\n", named(lane.Role(name)), count(counted[name]))
	}
	fmt.Fprintln(out)
}

// ── SPELLING NUMBERS THE WAY A PERSON READS THEM ────────────────────────────

func span(rows []callrows.Row) (first, last time.Time) {
	for _, row := range rows {
		if row.At.IsZero() {
			continue
		}
		if first.IsZero() || row.At.Before(first) {
			first = row.At
		}
		if row.At.After(last) {
			last = row.At
		}
	}
	return first, last
}

func journalSpan(journal []seen) (first, last time.Time) {
	if len(journal) == 0 {
		return time.Time{}, time.Time{}
	}
	return journal[0].at, journal[len(journal)-1].at
}

func stamp(at time.Time) string {
	if at.IsZero() {
		return "—"
	}
	return at.Format("2006-01-02 15:04:05")
}

func seconds(value float64) string {
	if math.IsInf(value, 0) {
		return "never finishes"
	}
	return fmt.Sprintf("%.1fs", value)
}

func share(part, whole int) string {
	if whole <= 0 {
		return "—"
	}
	return fmt.Sprintf("%.0f%%", 100*float64(part)/float64(whole))
}

// count groups thousands, because a five-figure row count read without them is
// a number somebody has to count the digits of.
func count(value int) string {
	if value < 0 {
		return "—"
	}
	digits := fmt.Sprintf("%d", value)
	var grouped []byte
	for index := 0; index < len(digits); index++ {
		if index > 0 && (len(digits)-index)%3 == 0 {
			grouped = append(grouped, ',')
		}
		grouped = append(grouped, digits[index])
	}
	return string(grouped)
}

// plainly is a duration in a person's words rather than Go's.
// plainlyAge is the median of a set of ages, in a person's words.
func plainlyAge(ages []time.Duration) string {
	if len(ages) == 0 {
		return "—"
	}
	values := make([]float64, len(ages))
	for index, age := range ages {
		values[index] = age.Seconds()
	}
	return plainly(time.Duration(quantile(values, 0.5)) * time.Second)
}

func plainly(value time.Duration) string {
	switch {
	case value <= 0:
		return "—"
	case value < time.Minute:
		return fmt.Sprintf("%.0fs", value.Seconds())
	case value < time.Hour:
		return fmt.Sprintf("%.0fm", value.Minutes())
	case value < 48*time.Hour:
		return fmt.Sprintf("%.1fh", value.Hours())
	}
	return fmt.Sprintf("%.1f days", value.Hours()/24)
}

// named is a role in words. THE ROLE WITH NO NAME HAS ONE HERE, because a
// heading that renders it as nothing reads as a bug rather than as a call whose
// site never said what it was for — which is what it is, and is the commonest
// thing in the table after the two that do say.
func named(role lane.Role) string {
	if role == lane.RoleUnknown {
		return "(nothing said)"
	}
	return string(role)
}

func blankOr(value string) string {
	if value == "" {
		return "no preference"
	}
	return value
}
