package resident

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// What a job is still short of, read from its own record and handed to
// everything that works on it next.
//
// THE DEFECT THIS ANSWERS. The textual run of 2026-08-29 (s9) measured its
// coverage gap on every one of three gates and reported the same two unexercised
// behaviours each time — 2, then 2, then 2. Across those three repair rounds it
// briefed fourteen nodes, and THIRTEEN OF THE FOURTEEN BRIEFS NAME NEITHER
// BEHAVIOUR. One node, by luck of the planner's wording, mentioned one of them.
// So the run measured the same shortfall three times, spent three rounds and
// fourteen leaves on it, and never once told a worker what it was.
//
// The reason is that the findings were reaching the brief through PROSE. A
// planner is handed a goal, writes briefs in its own words, and whatever it does
// not happen to restate is gone — and a model summarising a page of instructions
// will drop a two-item list nine times in ten. The measurement was structured
// (store.DeliveryGate.Unexercised is a list, and was one deliberately), and then
// it was flattened back into a paragraph on the only path that mattered.
//
// So: THE OPEN FINDINGS ARE READ FROM THE RECORD AND WRITTEN INTO THE BRIEF
// VERBATIM, in a fixed section, by the composer rather than by a model. What a
// worker is told it is short of is not a thing another model gets to paraphrase.

// OpenFindings is everything a job's own record says is still outstanding.
//
// Every field is sourced from a journal row rather than from anybody's summary
// of one — FAILSAFE.md's second clause, applied to the job's account of itself.
// An empty value means the record does not say, never that the answer is no.
type OpenFindings struct {
	// Unexercised are the behaviours the request stated that no check in the
	// tree exercises, in the words the request used. store.DeliveryGate.
	Unexercised []string
	// Unasserted are the behaviours a check in the tree NAMES and no assertion
	// WEIGHS, each carrying the observables nothing asserted. store.DeliveryGate.
	Unasserted []string
	// Failing are the named checks the last reading of the project's own tests
	// found red. store.VerificationReading.Sample.
	Failing []string
	// Gap is the last finding the delivery gate recorded against this lineage.
	Gap string
	// Unclosed says a gap the gate raised was never closed — the repair was
	// refused or could not be bought — which is a different fact from a gap
	// that a later round answered.
	Unclosed bool
	// Unreadable says the last gate passed over a tree whose checks nobody
	// could read, which is not a pass over a checked delivery.
	Unreadable bool
	// Consumers is the changed-definition finding: one line per definition this
	// run reshaped that the rest of the project still uses the old way. It is a
	// reading of the world like Failing and unlike Gap — no suite and no name
	// comparison can make it, because the name is still there.
	// store.DeliveryGate.Consumers.
	Consumers []string
	// Unbound is the unbound-reference finding: one line per name this run's own
	// sources READ that nothing in the tree binds, each carrying the file and
	// line it is read at. It is a reading of the world like Failing and unlike
	// Gap, and it is the one a repair round can act on without running anything.
	// store.DeliveryGate.Unbound.
	Unbound []string
	// Mechanical says the standing gap is the MECHANICAL half's: a file the
	// plan promised and the disk does not hold. It is beside Gap rather than
	// inside it because the two are answered by different evidence — one is a
	// judge's sentence about a delivery, the other is a stat of the filesystem —
	// and nothing that reads a claim about the plan may overrule the second.
	Mechanical bool
}

// Empty reports that the record names nothing outstanding, in which case no
// composer should write a section: a heading announcing no findings costs
// tokens and teaches the model that the heading means nothing.
func (f OpenFindings) Empty() bool {
	return len(f.Unexercised) == 0 && len(f.Unasserted) == 0 && len(f.Failing) == 0 &&
		len(f.Consumers) == 0 && len(f.Unbound) == 0 && strings.TrimSpace(f.Gap) == "" &&
		!f.Unclosed && !f.Unreadable
}

// OpenFindingsHeader introduces the section. It is one wording, exported, and
// used by every path that hands work on, so a worker meets the same words
// whether it was spliced by a gap round, an overrun, a split or a retry.
const OpenFindingsHeader = "WHAT THIS JOB IS STILL SHORT OF — measured, not guessed. Read this before the assignment below; work that does not close these is work this job has already paid for once:"

// openFindingsLimit bounds each list in the section. The findings are the
// request's own behaviours and the runner's own check names, so a job with
// forty of them has a shape problem the brief cannot fix, and a brief that
// prints forty has stopped being read.
const openFindingsLimit = 12

// Words renders the fixed section, or nothing when there is nothing to say.
//
// The order is deliberate and it is the order of specificity: a named failing
// check is a thing to go and run, an unexercised behaviour is a thing to go and
// write, and the gate's own sentence is the judgement over both. A worker that
// reads only the first line has read the most actionable one.
func (f OpenFindings) Words() string {
	if f.Empty() {
		return ""
	}
	var section strings.Builder
	section.WriteString(OpenFindingsHeader)
	if len(f.Failing) > 0 {
		section.WriteString("\n\nChecks the last reading found failing — run them and make them pass:\n")
		section.WriteString(bulleted(f.Failing))
	}
	if len(f.Unbound) > 0 {
		section.WriteString("\n\nNames this work READS that nothing in the tree binds — read from " +
			"the source on disk, not guessed. Each needs the name bound, or the line that " +
			"reaches for it changed:\n")
		section.WriteString(bulleted(f.Unbound))
	}
	if len(f.Unexercised) > 0 {
		section.WriteString("\n\nBehaviours the request asks for that NO check in the tree exercises. " +
			"Each needs a check that would fail if the behaviour were removed:\n")
		section.WriteString(bulleted(f.Unexercised))
	}
	if len(f.Consumers) > 0 {
		section.WriteString("\n\nDefinitions this work reshaped that the rest of the project still uses " +
			"the old way. Each needs the callers brought with it, or the definition put back:\n")
		section.WriteString(bulleted(f.Consumers))
	}
	if len(f.Unasserted) > 0 {
		section.WriteString("\n\nBehaviours the request asks for that a check NAMES and no assertion " +
			"WEIGHS. Each already has a check that runs it; what each needs is an assertion on the " +
			"identifiers named after it, so the check would fail if the behaviour were wrong:\n")
		section.WriteString(bulleted(f.Unasserted))
	}
	if gap := strings.TrimSpace(f.Gap); gap != "" {
		section.WriteString("\n\nWhat the last review found missing:\n")
		section.WriteString(gap)
	}
	if f.Unclosed {
		section.WriteString("\n\nThat finding was never answered — no round has closed it yet.")
	}
	if f.Unreadable {
		section.WriteString("\n\nThe project's own checks could not be read on the last attempt, " +
			"so nothing here has been confirmed against them. Getting one reading is worth more than any new work.")
	}
	return section.String()
}

// bulleted is one list, one item per line, bounded and counted when it is cut —
// a list silently shortened is a list a reader believes is complete.
func bulleted(items []string) string {
	shown := items
	more := 0
	if len(shown) > openFindingsLimit {
		more, shown = len(shown)-openFindingsLimit, shown[:openFindingsLimit]
	}
	var out strings.Builder
	for _, item := range shown {
		out.WriteString("- ")
		out.WriteString(strings.TrimSpace(item))
		out.WriteString("\n")
	}
	if more > 0 {
		fmt.Fprintf(&out, "- and %d more the record holds\n", more)
	}
	return strings.TrimRight(out.String(), "\n")
}

// ReadOpenFindings assembles the findings from one lineage's own journal.
//
// It reads the LINEAGE and not the node, because a repair round is a different
// node id from the work it repairs — that is the whole shape of the defect in
// (A) as well — so a reader that asked the node would find a fresh row with
// nothing on it and conclude the job was short of nothing.
//
// A failure to read is not a failure to work: this is an account of the job and
// never a part of it, so an unreadable journal answers with nothing and lets the
// work go on with the brief it would otherwise have had.
func ReadOpenFindings(graph *store.Store, lineage string) OpenFindings {
	findings := OpenFindings{}
	if graph == nil || strings.TrimSpace(lineage) == "" {
		return findings
	}
	gates, err := graph.DeliveryGateLineage(lineage)
	if err == nil {
		// Oldest first, so the last word wins on every field that has one. A
		// coverage measurement is a photograph of the tree as it stood, and the
		// newest photograph is the only one still true.
		for _, gate := range gates {
			if len(gate.Unexercised) > 0 {
				findings.Unexercised = append([]string(nil), gate.Unexercised...)
			}
			if len(gate.Unasserted) > 0 {
				findings.Unasserted = append([]string(nil), gate.Unasserted...)
			}
			if len(gate.Consumers) > 0 {
				findings.Consumers = append([]string(nil), gate.Consumers...)
			}
			if len(gate.Unbound) > 0 {
				findings.Unbound = append([]string(nil), gate.Unbound...)
			}
			if gate.Pass {
				// A gate that passed answers the gap before it. What it cannot
				// answer is the coverage above, which is a measurement rather
				// than a judgement and stands until it is re-measured.
				findings.Gap, findings.Unclosed, findings.Mechanical = "", false, false
			} else if strings.TrimSpace(gate.Gap) != "" {
				findings.Gap, findings.Unclosed = strings.TrimSpace(gate.Gap), gate.Unclosed
				findings.Mechanical = gate.Mechanical
			}
			findings.Unreadable = gate.Unreadable
		}
	}
	findings.Failing = failingChecks(graph, lineage)
	return findings
}

// failingChecks is every check the lineage's newest reading found red.
//
// The newest reading and not the union of all of them: a check that was red
// three rounds ago and is green now is not a finding, it is history, and
// handing it to a worker as outstanding work is how a round gets spent
// re-fixing something that is already fixed.
func failingChecks(graph *store.Store, lineage string) []string {
	nodes, err := graph.LineageNodes(lineage)
	if err != nil {
		return nil
	}
	// Newest first, and it stops at the first node that took a reading at all.
	// This runs on every claim of every leaf, so walking the whole lineage and
	// keeping the last answer would make the cost of composing a brief grow
	// with the size of the job — and it would be the same answer: a node that
	// read the tree AFTER another node read it has the newer photograph, and
	// the older one is history rather than a finding.
	for index := len(nodes) - 1; index >= 0; index-- {
		readings, err := graph.VerificationsFor(nodes[index].ID)
		if err != nil || len(readings) == 0 {
			continue
		}
		var newest []string
		took := false
		for _, reading := range readings {
			if !reading.Read {
				continue
			}
			took = true
			newest = nil
			if reading.Red > 0 && len(reading.Sample) > 0 {
				newest = append([]string(nil), reading.Sample...)
			}
		}
		if !took {
			continue
		}
		sort.Strings(newest)
		return newest
	}
	return nil
}

// ── what the world still says is wrong ───────────────────────────────────────

// The kinds of standing evidence, as the journal spells them. They are the
// vocabulary of store.JobGrowth.CoveredDespite and they name READINGS, never
// judgements: each one is something a mechanism measured on the tree and has
// not since measured away.
const (
	EvidenceFailing     = "failing-checks"
	EvidenceUnexercised = "unexercised"
	EvidenceUnasserted  = "unasserted"
	EvidenceConsumers   = "consumers"
	EvidenceUnbound     = "unbound"
	EvidenceLost        = "lost-names"
	EvidenceMechanical  = "mechanical-gap"

	// EvidenceFinding is a review's own finding, held in the hand of the round
	// being bought rather than read back off the journal. It is the kind for
	// the findings no reading names — a judge's citation, a suite nobody could
	// read — and it stands for the same reason all the others do: something
	// outside the account being judged says this job is not finished.
	EvidenceFinding = "open-finding"
)

// StandingEvidence is what the WORLD still says is wrong with this job, as the
// kinds that stand.
//
// COVERAGE IS A CLAIM ABOUT THE PLAN; A FINDING IS EVIDENCE FROM THE WORLD. The
// satisfaction question asks a model whether the acceptance points of a plan are
// mapped onto work that has landed or is running. That is a reading of the
// PLAN, and it can be true of a job whose checks are red, whose stated
// behaviours nothing exercises, whose public names the change deleted, and whose
// callers were left behind by a definition it reshaped — because none of those
// is a point on the plan. ink v4-flash s14 refused three rounds as
// goal-already-covered while its own readings ran 172 checks with 18 red and its
// own gate held six behaviours nothing exercised; igel s13 refused two while the
// surface photograph reported eight deleted public names, five times running.
//
// So a claim about the plan may never overrule a measurement of the world. What
// coverage is still allowed to do is refuse a job the world has nothing standing
// against — which is the question it was built for.
//
// IT IS READ AT THE JOB AND NOT AT THE LINEAGE THAT ASKED. A repair round is a
// different lineage from the work it repairs, and the readings sit on the nodes
// that took them: igel s13's refusal was weighed under `task-2-x1`, whose id
// namespace does not contain `task-2-x1-n1`, which is where the eight lost names
// had been journaled ninety seconds earlier. The job has one world.
//
// A read that fails answers nothing standing, which lets coverage speak. That is
// the direction the caps are under: the round cap, the standstill, the finding
// fixed point and the wall all still hold whatever this says.
func StandingEvidence(graph *store.Store, jobRoot string) []string {
	if graph == nil {
		return nil
	}
	root, _ := OverrunLineage(strings.TrimSpace(jobRoot))
	if root == "" {
		return nil
	}
	findings := ReadOpenFindings(graph, root)
	standing := make([]string, 0, 7)
	if len(findings.Failing) > 0 {
		standing = append(standing, EvidenceFailing)
	}
	if len(findings.Unexercised) > 0 {
		standing = append(standing, EvidenceUnexercised)
	}
	if len(findings.Unasserted) > 0 {
		standing = append(standing, EvidenceUnasserted)
	}
	if len(findings.Consumers) > 0 {
		standing = append(standing, EvidenceConsumers)
	}
	// And a name nothing binds, which is the least arguable of them: the
	// reference is in one file, the definition is in none, and no ruling about
	// coverage puts a binding in the tree.
	if len(findings.Unbound) > 0 {
		standing = append(standing, EvidenceUnbound)
	}
	if lostPublicNames(graph, root) > 0 {
		standing = append(standing, EvidenceLost)
	}
	// A mechanical gap is a file the plan promised and the disk does not hold.
	// It stands while it is unclosed, and no ruling about coverage makes a
	// missing file appear.
	if findings.Mechanical && findings.Unclosed {
		standing = append(standing, EvidenceMechanical)
	}
	if len(standing) == 0 {
		return nil
	}
	return standing
}

// standingAgainst is what the world says is wrong with this job when the round
// about to be bought was bought FOR something — the finding a review is raising
// right now, which no journal can be relied on to hold yet.
//
// THE JOURNAL IS WRITTEN ONE EVENT LATER THAN IT IS READ. The delivery gate
// asks for its repair round and records its row afterwards, in that order, so
// StandingEvidence looking for the gate's own finding in DeliveryGateLineage
// finds the round BEFORE it: journal seq 331 was the growth, 332 was the gate
// (2026-09-01, deepseek-v4-flash). The package test that covered the same path
// recorded the gate first and passed over the inverted order for months. A
// finding held in the hand needs no journal to be standing — it is standing by
// construction, because a live review is raising it — so it is read from the
// hand and the journal is asked for everything else.
//
// The empty finding adds nothing, which is exactly StandingEvidence's answer
// for a round nobody bought for a finding.
func standingAgainst(graph *store.Store, jobRoot string, finding Finding) []string {
	standing := StandingEvidence(graph, jobRoot)
	if finding.Empty() {
		return standing
	}
	kind := findingEvidence(finding)
	for _, held := range standing {
		if held == kind {
			return standing
		}
	}
	return append([]string{kind}, standing...)
}

// findingEvidence is a live finding said in the vocabulary of the readings, so
// an autopsy sorting the journal by what stood does not have to learn a second
// set of words for the same measurements.
//
// The kinds with no reading of their own — a judge's own citation, a suite
// nobody could read — answer EvidenceFinding, which says the true thing about
// them: a review is holding something open against this job.
func findingEvidence(finding Finding) string {
	switch finding.Kind {
	case FindingOwnFailing, "own-checks-failing", "removed-checks", "regression":
		return EvidenceFailing
	case FindingUnexercised:
		return EvidenceUnexercised
	case FindingUnasserted:
		return EvidenceUnasserted
	case FindingConsumers:
		return EvidenceConsumers
	case FindingUnbound, "unbound-names":
		return EvidenceUnbound
	case "removed-public-name":
		return EvidenceLost
	case FindingMechanical:
		return EvidenceMechanical
	}
	return EvidenceFinding
}
