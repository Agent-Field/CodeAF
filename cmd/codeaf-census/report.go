package main

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

// report prints the whole census as markdown, in the order
// docs/design/recovery/DESIGN.md §1 and §8 ask for.
//
// MARKDOWN AND NOT A TABLE LIBRARY, because the output's destination is a
// document beside the design it argues with — census-20260910.md is the first
// run of it — and a nightly run's diff against yesterday's should be readable
// in a pull request rather than in a terminal.
func report(out io.Writer, path string, rows []row, look settings) {
	finishes := finished(rows)
	fmt.Fprintf(out, "# Call census — `%s`\n\n", path)
	if len(rows) == 0 {
		// The emptiness law, in a file as on a screen: a log with nothing in it
		// says so in one line and invents no zeroes to fill a table with.
		fmt.Fprintln(out, "Nothing has been written to this log yet.")
		return
	}
	first, last := window(rows)
	fmt.Fprintf(out, "%s → %s · %d rows · %d finished attempts\n\n",
		stamp(first), stamp(last), len(rows), len(finishes))

	statusTable(out, finishes)
	causeTable(out, finishes)
	signatureTable(out, finishes, look.top)
	pacedSection(out, finishes)
	chainSection(out, rows, look.longest)
	healthSection(out, finishes, look)
	surprises(out, rows, finishes)
}

func finished(rows []row) []row {
	var done []row
	for _, r := range rows {
		if r.Finished() {
			done = append(done, r)
		}
	}
	return done
}

// window is the first and last moment anything in this log carried. A row that
// was lost to a number JSON could not spell carries no moment at all, and it
// sorts to the front; reading the window off the ends of the slice would report
// a census that began at the zero of the calendar.
func window(rows []row) (time.Time, time.Time) {
	var first, last time.Time
	for _, r := range rows {
		if r.At.IsZero() {
			continue
		}
		if first.IsZero() || r.At.Before(first) {
			first = r.At
		}
		if r.At.After(last) {
			last = r.At
		}
	}
	return first, last
}

func stamp(at time.Time) string {
	if at.IsZero() {
		return "(no stamp)"
	}
	return at.Format("2006-01-02 15:04")
}

// ── §1's first table ────────────────────────────────────────────────────────

func statusTable(out io.Writer, finishes []row) {
	counted := map[statusClass]int{}
	for _, r := range finishes {
		counted[r.statusClass()]++
	}
	fmt.Fprint(out, "## Finishes by status\n\n")
	fmt.Fprintln(out, "| class | count | % |")
	fmt.Fprintln(out, "| --- | ---: | ---: |")
	for _, class := range classesInOrder {
		if counted[class] == 0 {
			continue
		}
		fmt.Fprintf(out, "| %s | %d | %s |\n", class, counted[class], share(counted[class], len(finishes)))
	}
	fmt.Fprintln(out)
}

func causeTable(out io.Writer, finishes []row) {
	counted := map[causeFamily]int{}
	total, ours := 0, 0
	for _, r := range finishes {
		family, failed := r.cause()
		if !failed {
			continue
		}
		counted[family]++
		total++
		if ourOwnDoing[family] {
			ours++
		}
	}
	fmt.Fprint(out, "## Cause families\n\n")
	if total == 0 {
		fmt.Fprint(out, "Nothing failed.\n\n")
		return
	}
	fmt.Fprintln(out, "| family | n | % of bad rows |")
	fmt.Fprintln(out, "| --- | ---: | ---: |")
	for _, family := range familiesInOrder {
		if counted[family] == 0 {
			continue
		}
		fmt.Fprintf(out, "| %s | %d | %s |\n", family, counted[family], share(counted[family], total))
	}
	fmt.Fprintln(out)
	// THE HEADLINE IS THE SPLIT AND NOT THE TOTAL. A number that counts a race
	// this build won, a turn a person walked away from and an errand deadline
	// somebody else set as "failures" is a measurement of our own policy read
	// back as provider health, which is the reading the first census made and
	// the reason the two fields it needed now exist.
	fmt.Fprintf(out, "**%d of %d bad rows (%s) are this build acting on its own calls** — "+
		"an arm cut off because the other one answered, a caller leaving, a caller's own deadline, "+
		"or a bound this package set. The remaining %d are the world.\n\n",
		ours, total, share(ours, total), total-ours)
	if n := counted[causeExhaust]; n > 0 {
		fmt.Fprintf(out, "Of those, %d are hedge exhaust: the question they were sent for WAS answered.\n\n", n)
	}
}

// ── the signatures ──────────────────────────────────────────────────────────

type signatureTally struct {
	signature string
	n         int
	status    map[int]int
	machines  map[string]int
	tags      map[string]int
}

func signatureTable(out io.Writer, finishes []row, top int) {
	tallies := map[string]*signatureTally{}
	bad := 0
	for _, r := range finishes {
		signature := r.signature()
		if signature == "" {
			continue
		}
		bad++
		held := tallies[signature]
		if held == nil {
			held = &signatureTally{
				signature: signature,
				status:    map[int]int{},
				machines:  map[string]int{},
				tags:      map[string]int{},
			}
			tallies[signature] = held
		}
		held.n++
		held.status[r.Status]++
		if machine := firstNonEmpty(r.Served, r.Lane); machine != "" {
			held.machines[machine]++
		}
		if r.Tag != "" {
			held.tags[r.Tag]++
		}
	}
	fmt.Fprintf(out, "## Error signatures (%d failing rows, %d signatures)\n\n", bad, len(tallies))
	if len(tallies) == 0 {
		fmt.Fprintln(out)
		return
	}
	ordered := make([]*signatureTally, 0, len(tallies))
	for _, held := range tallies {
		ordered = append(ordered, held)
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].n != ordered[j].n {
			return ordered[i].n > ordered[j].n
		}
		return ordered[i].signature < ordered[j].signature
	})
	if top > 0 && top < len(ordered) {
		ordered = ordered[:top]
	}
	fmt.Fprintln(out, "| n | signature | status | top machines | tags |")
	fmt.Fprintln(out, "| ---: | --- | --- | --- | --- |")
	for _, held := range ordered {
		fmt.Fprintf(out, "| %d | %s | %s | %s | %s |\n",
			held.n, cell(held.signature), statusesOf(held.status),
			topOf(held.machines, 3), topOf(held.tags, 3))
	}
	fmt.Fprintln(out)
}

func statusesOf(counted map[int]int) string {
	var each []string
	for status := range counted {
		if status == 0 {
			each = append(each, "none")
			continue
		}
		each = append(each, fmt.Sprintf("%d", status))
	}
	sort.Strings(each)
	return strings.Join(each, ", ")
}

// ── the pacing depth ────────────────────────────────────────────────────────

// pacedSection is the 429 half of the design's §1: how many, from whom, how
// deep into a retry chain, and — the field the first census found was NEVER
// present — whether the provider said when to come back.
func pacedSection(out io.Writer, finishes []row) {
	byMachine := map[string]int{}
	depth := map[int]int{}
	total, named := 0, 0
	for _, r := range finishes {
		family, failed := r.cause()
		if !failed || family != causePaced {
			continue
		}
		total++
		if machine := firstNonEmpty(r.Served, r.Lane); machine != "" {
			byMachine[machine]++
		} else {
			byMachine["(none)"]++
		}
		depth[r.Attempt]++
		if r.RetryAfterS > 0 {
			named++
		}
	}
	fmt.Fprintf(out, "## Paced (429)\n\n%d finishes were the provider saying not yet; %d of them carried a comeback instruction.\n\n", total, named)
	if total == 0 {
		return
	}
	fmt.Fprintln(out, "| machine | n |")
	fmt.Fprintln(out, "| --- | ---: |")
	for _, entry := range ranked(byMachine, 20) {
		fmt.Fprintf(out, "| %s | %d |\n", entry.name, entry.n)
	}
	fmt.Fprintln(out)
	fmt.Fprint(out, "Retry depth at the moment of the refusal:\n\n")
	var depths []int
	for at := range depth {
		depths = append(depths, at)
	}
	sort.Ints(depths)
	var heads, counts []string
	for _, at := range depths {
		heads = append(heads, fmt.Sprintf("%d", at))
		counts = append(counts, fmt.Sprintf("%d", depth[at]))
	}
	fmt.Fprintf(out, "| attempt | %s |\n", strings.Join(heads, " | "))
	fmt.Fprintf(out, "| --- | %s |\n", strings.Repeat("--: | ", len(heads)))
	fmt.Fprintf(out, "| n | %s |\n\n", strings.Join(counts, " | "))
}

// ── the chains ──────────────────────────────────────────────────────────────

func chainSection(out io.Writer, rows []row, longest int) {
	all := chainsIn(rows)
	many := multiAttempt(all)
	stayed := 0
	for _, c := range many {
		if c.stayedPut() {
			stayed++
		}
	}
	fmt.Fprintf(out, "## Retry chains\n\n%d chains; %d with more than one attempt.\n\n", len(all), len(many))
	if len(many) == 0 {
		return
	}
	fmt.Fprintf(out, "**%d of %d multi-attempt chains (%s) never left the machine they started on.**\n\n",
		stayed, len(many), share(stayed, len(many)))

	byLength := map[int]int{}
	ended := map[statusClass]int{}
	for _, c := range many {
		byLength[len(c.rows)]++
		ended[c.outcome()]++
	}
	var lengths []int
	for at := range byLength {
		lengths = append(lengths, at)
	}
	sort.Ints(lengths)
	var spelled []string
	for _, at := range lengths {
		spelled = append(spelled, fmt.Sprintf("%d→%d", at, byLength[at]))
	}
	fmt.Fprintf(out, "Length: %s\n\n", strings.Join(spelled, ", "))

	fmt.Fprintln(out, "| ended as | n |")
	fmt.Fprintln(out, "| --- | ---: |")
	for _, class := range classesInOrder {
		if ended[class] == 0 {
			continue
		}
		fmt.Fprintf(out, "| %s | %d |\n", class, ended[class])
	}
	fmt.Fprintln(out)

	fmt.Fprintf(out, "The %d longest:\n\n", longest)
	fmt.Fprintln(out, "| start | dur | attempts | tag/node | machines in order | ended as |")
	fmt.Fprintln(out, "| --- | ---: | ---: | --- | --- | --- |")
	for _, c := range longestChains(many, longest) {
		fmt.Fprintf(out, "| %s | %ds | %d | %s | %s | %s |\n",
			stamp(c.began()), int(c.duration().Seconds()), len(c.rows),
			cell(firstNonEmpty(c.tag, "–")+"/"+firstNonEmpty(c.node, "–")),
			cell(strings.Join(c.machines(), ", ")), c.outcome())
	}
	fmt.Fprintln(out)
}

// ── lane health ─────────────────────────────────────────────────────────────

type health struct {
	model    string
	machine  string
	n        int
	clean    int
	millis   []int64
	failures map[statusClass]int
	// traced is how many of these finishes said anything about their
	// connection, and warm how many of those rode one the pool already had.
	// The pair is kept rather than a rate because a log written by a build
	// older than the tracing carries neither, and a share computed over rows
	// that could not answer would read as a cold pool rather than as silence.
	traced int
	warm   int
	// opening is how long the ones that were NOT warm spent resolving,
	// connecting and shaking hands — the part of `ms` that was never the
	// model's.
	opening []int64
}

// healthSection is the clean-answer rate and median latency of each (model,
// machine) pair over the recent window.
//
// IT IS KEYED ON THE MACHINE THAT SERVED AND NEVER ON THE ONE THAT WAS ASKED
// FOR — the design's third reading, and the reason this section exists at all.
// On a third of this log's rows the two disagree, so a health table keyed on the
// demand is a table of which machines this build BELIEVES are slow.
func healthSection(out io.Writer, finishes []row, look settings) {
	if len(finishes) == 0 {
		return
	}
	_, newest := window(finishes)
	since := newest.AddDate(0, 0, -look.days)
	pairs := map[string]*health{}
	counted := 0
	for _, r := range finishes {
		if r.At.Before(since) || r.Model == "" {
			continue
		}
		if r.Exhaust() {
			// AN ARM WE CUT OFF SAYS NOTHING ABOUT THE MACHINE IT WAS SENT TO.
			// It is left out of the health table altogether rather than counted
			// as a failure of the lane that was still working on it when this
			// build stopped listening — which would mark down exactly the
			// machines the hedge is aimed at.
			continue
		}
		machine := strings.TrimSpace(r.Served)
		if machine == "" {
			// UNSERVED AND NOT ASSUMED. A row that never learned who answered
			// is a row with no fact about a machine in it, and folding it under
			// the lane that was asked for is exactly the attribution the design
			// says the whole ledger has been keyed on wrongly.
			machine = "(unnamed)"
		}
		key := r.Model + "\x00" + machine
		held := pairs[key]
		if held == nil {
			held = &health{model: r.Model, machine: machine, failures: map[statusClass]int{}}
			pairs[key] = held
		}
		held.n++
		counted++
		// WHAT THE CONNECTION COST, kept beside the answer's own time. A cold
		// pool and a slow model are the same number in `ms` and in `ttft_ms`,
		// and these two columns are the whole of the difference
		// (internal/provider's conntrace.go).
		if r.ConnReused != nil {
			held.traced++
			if *r.ConnReused {
				held.warm++
			} else {
				// A CONNECTION OPENED FRESH COUNTS EVEN WHEN IT COST NOTHING
				// MEASURABLE. On a fast link the three parts together round to
				// zero milliseconds, and dropping those rows would leave the
				// median describing only the expensive handshakes — the
				// emptiness law is about a figure nobody measured, and this one
				// was measured and came out small.
				held.opening = append(held.opening, r.DNSms+r.ConnectMs+r.TLSms)
			}
		}
		class := r.statusClass()
		if class == classClean {
			held.clean++
			if r.Millis > 0 {
				held.millis = append(held.millis, r.Millis)
			}
			continue
		}
		held.failures[class]++
	}
	fmt.Fprintf(out, "## Lane health, last %d days (%d finishes; a clean answer is 200 with no error; n ≥ %d)\n\n",
		look.days, counted, look.min)
	ordered := make([]*health, 0, len(pairs))
	for _, held := range pairs {
		if held.n >= look.min {
			ordered = append(ordered, held)
		}
	}
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].n > ordered[j].n })
	if len(ordered) == 0 {
		fmt.Fprint(out, "No pair has been asked enough times to say anything about.\n\n")
		return
	}
	fmt.Fprintln(out, "| model | served | n | ok % | median ms | warm % | opening ms | top failures |")
	fmt.Fprintln(out, "| --- | --- | ---: | ---: | ---: | ---: | ---: | --- |")
	for _, held := range ordered {
		spelledFailures := map[string]int{}
		for class, n := range held.failures {
			spelledFailures[string(class)] = n
		}
		fmt.Fprintf(out, "| %s | %s | %d | %s | %s | %s | %s | %s |\n",
			cell(held.model), cell(held.machine), held.n,
			trimShare(held.clean, held.n), median(held.millis),
			warmth(held.warm, held.traced), median(held.opening),
			topOf(spelledFailures, 3))
	}
	fmt.Fprintln(out)
}

// warmth is how often this pair rode a connection the pool already had, and
// nothing at all for rows written by a build that did not measure it. THE
// EMPTINESS LAW MATTERS HERE MORE THAN ANYWHERE ELSE IN THIS TABLE: a zero
// would read as a pool that is always cold, which is exactly the finding this
// column exists to report, and reporting it from silence would be a lie.
func warmth(warm, traced int) string {
	if traced == 0 {
		return "—"
	}
	return trimShare(warm, traced)
}

// median is the middle of what was measured, and nothing at all when nothing
// was — the emptiness law, in a table cell.
func median(values []int64) string {
	if len(values) == 0 {
		return "—"
	}
	sorted := append([]int64(nil), values...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	return fmt.Sprintf("%d", sorted[len(sorted)/2])
}

// ── the surprising checks ───────────────────────────────────────────────────

// surprises is the section that is not a distribution but a list of things that
// should not be true, each with the number of rows that say it is.
//
// EVERY ONE OF THEM IS A FINDING FROM THE FIRST CENSUS, kept as a check so that
// a wave which fixes one can prove it, and so a wave which breaks one again is
// told the night it happens.
func surprises(out io.Writer, rows, finishes []row) {
	var (
		overCeiling int
		disagreed   int
		attributed  int
		unpricedBad int
		failures    int
		absurd      int
		unspelled   int
		noTag       int
		unwritten   int
		wildest     float64
	)
	var (
		cutByABound  int
		exhausted    int
		rescueDenied int
	)
	// refusedBy is what stopped a rescue the controller had already called for,
	// by the machine word the row carries. It is a reading of its own because a
	// rescue that was ASKED FOR and did not happen is the one shape a census of
	// waits cannot see any other way: the row looks like an ordinary slow call.
	refusedBy := map[string]int{}
	appliedBy := map[string]int{}
	endedBy := map[string]int{}
	started := map[string]bool{}
	ended := map[string]bool{}
	for _, r := range rows {
		if r.Raw != "" && r.Time == "" {
			unspelled++
			continue
		}
		if r.ID != "" {
			if r.Finished() {
				ended[r.ID] = true
			} else {
				started[r.ID] = true
			}
		}
	}
	for _, r := range finishes {
		if ceiling := r.HazardCeiling(); ceiling > 0 && r.Millis > 2*ceiling {
			overCeiling++
		}
		if r.Served != "" && r.Lane != "" {
			attributed++
			if !strings.EqualFold(r.Served, r.Lane) {
				disagreed++
			}
		}
		if r.Tag == "" {
			noTag++
		}
		if r.Ended != "" {
			unwritten++
			endedBy[r.Ended]++
		}
		if word := strings.TrimSpace(r.AppliedWord); word != "" {
			cutByABound++
			appliedBy[word]++
		}
		if word := strings.TrimSpace(r.Refused); word != "" {
			rescueDenied++
			refusedBy[word]++
		}
		if r.Exhaust() {
			exhausted++
		}
		if r.Failed() {
			failures++
			if r.Cost == 0 {
				unpricedBad++
			}
		}
		for _, seconds := range []float64{r.CostS, r.WaitS} {
			if nonFinite(seconds) || (seconds != 0 && (seconds > absurdSeconds || seconds < -absurdSeconds)) {
				absurd++
			}
			if seconds > wildest {
				wildest = seconds
			}
		}
	}
	orphans := 0
	for id := range started {
		if !ended[id] {
			orphans++
		}
	}
	fmt.Fprint(out, "## The checks\n\n")
	fmt.Fprintln(out, "| check | n | of |")
	fmt.Fprintln(out, "| --- | ---: | ---: |")
	fmt.Fprintf(out, "| ran past twice the hazard ceiling it was armed with | %d | %d |\n", overCeiling, len(finishes))
	fmt.Fprintf(out, "| the machine asked for is not the machine that served | %d | %d |\n", disagreed, attributed)
	fmt.Fprintf(out, "| a failed attempt that recorded no cost | %d | %d |\n", unpricedBad, failures)
	fmt.Fprintf(out, "| started and never finished | %d | %d |\n", orphans, len(started))
	fmt.Fprintf(out, "| carried no tag | %d | %d |\n", noTag, len(finishes))
	fmt.Fprintf(out, "| a wait or a cost no second could hold | %d | %d |\n", absurd, len(finishes))
	fmt.Fprintf(out, "| a row JSON could not spell and lost | %d | %d |\n", unspelled, len(rows))
	fmt.Fprintf(out, "| closed by the transport because no path wrote it | %d | %d |\n", unwritten, len(finishes))
	fmt.Fprintf(out, "| cut by a bound this build set, and says which | %d | %d |\n", cutByABound, len(finishes))
	fmt.Fprintf(out, "| exhaust of a race that was won, counted as a failure until now | %d | %d |\n", exhausted, len(finishes))
	fmt.Fprintf(out, "| a rescue the controller called for that never left, and says why | %d | %d |\n", rescueDenied, len(finishes))
	if unwritten > 0 {
		fmt.Fprintf(out, "\nClosed as: %s.\n", topOf(endedBy, 6))
	}
	if cutByABound > 0 {
		fmt.Fprintf(out, "\nCut by: %s.\n", topOf(appliedBy, 6))
	}
	if rescueDenied > 0 {
		fmt.Fprintf(out, "\nRescues refused by: %s.\n", topOf(refusedBy, 6))
	}
	if wildest > absurdSeconds {
		fmt.Fprintf(out, "\nThe largest figure in seconds on any row is %g.\n", wildest)
	}
	fmt.Fprintln(out)
}

// ── the shared spellings ────────────────────────────────────────────────────

type entry struct {
	name string
	n    int
}

func ranked(counted map[string]int, top int) []entry {
	ordered := make([]entry, 0, len(counted))
	for name, n := range counted {
		ordered = append(ordered, entry{name: name, n: n})
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].n != ordered[j].n {
			return ordered[i].n > ordered[j].n
		}
		return ordered[i].name < ordered[j].name
	})
	if top > 0 && top < len(ordered) {
		ordered = ordered[:top]
	}
	return ordered
}

func topOf(counted map[string]int, top int) string {
	ordered := ranked(counted, top)
	if len(ordered) == 0 {
		return "—"
	}
	var spelled []string
	for _, held := range ordered {
		spelled = append(spelled, fmt.Sprintf("%s %d", held.name, held.n))
	}
	return cell(strings.Join(spelled, ", "))
}

// share is a percentage to one decimal, and "—" when there is nothing to take a
// share of. A percentage of nothing is not zero per cent.
func share(part, whole int) string {
	if whole == 0 {
		return "—"
	}
	return fmt.Sprintf("%.1f", 100*float64(part)/float64(whole))
}

func trimShare(part, whole int) string { return share(part, whole) }

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// cell makes one string safe to sit in a markdown table: a pipe inside a cell
// would end the column, and a signature full of router prose is exactly where
// one turns up.
func cell(value string) string {
	value = strings.ReplaceAll(value, "|", "\\|")
	value = strings.ReplaceAll(value, "\n", " ")
	return strings.TrimSpace(value)
}
