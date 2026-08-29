// A finding has its own fixed point.
//
// THE DEFECT THIS ANSWERS. happy-dom v4-flash s13 was given 5400 seconds and a
// gate that was right. Four delivery judgements in a row carried ONE finding —
// "this work removed checks that existed before it", citing
// `IntersectionObserver disconnect() Does nothing` and three siblings, the same
// four names in the same order every time — and each of them bought a repair
// round. Every round changed files the job was about, so the standstill rule
// correctly saw motion; every round was handed a differently-worded remainder
// on the paths that read prose, so nothing there matched either. What stopped
// the run was arithmetic, `cause: rounds`, forty minutes and $0.135 later, at
// 9 of 14 hidden checks — where the same task on the previous seed, working the
// same problem, reached 12.
//
// A round is bought FOR something. The thing it was bought for is the finding
// the gate raised, and whether it moved is a question about that finding and
// about nothing else: not about the tree, which a stuck model changes freely,
// and not about the review's paragraph, which a model rewords for free. So the
// finding travels with the round that was bought for it, is journaled beside
// it, and is compared against the next round's — kind and cited names, from the
// structured record, never from the sentence.
package resident

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// The kinds of finding a delivery judgement can raise, in the order they are
// read off one gate.
//
// The order is SPECIFICITY, and it decides which finding a round is recorded as
// having been bought for when a gate raises more than one. A measurement of the
// repository — a behaviour nothing exercises, a check the run wrote and left
// red, a definition it reshaped under its callers — is more particular than a
// judge's sentence about the delivery as a whole, and it is the half a repair
// can be aimed at. The judge's own citations are last because they are the kind
// every gate has.
const (
	// FindingUnreadable is the one finding with no names: the project declared
	// a way of checking itself and this run could not read it.
	FindingUnreadable = "unreadable"
	// FindingOwnFailing is checks THIS work wrote that are red.
	FindingOwnFailing = "own-failing"
	// FindingUnexercised is behaviours the request states that no check touches.
	FindingUnexercised = "unexercised"
	// FindingUnasserted is behaviours a check names and no assertion weighs.
	FindingUnasserted = "unasserted"
	// FindingConsumers is definitions this run reshaped that the rest of the
	// project still uses the old way.
	FindingConsumers = "consumers"
	// FindingMechanical is a file the plan promised and the disk does not hold.
	FindingMechanical = "mechanical"
	// FindingReview is the judge's own finding, named by the spans it cited.
	// Every finding a gate raises that has no list of its own arrives here,
	// including the ones a later wave gives a list to — at which point it gets
	// a case above and stops being one of these.
	FindingReview = "review"
)

// Finding is one delivery judgement as the thing a repair round is bought to
// close: what KIND of finding it is, and every name it stands on.
//
// A FINDING'S IDENTITY IS A KIND AND ONE NAME. The set is not the finding; the
// set is whichever subset of the world one gate happened to weigh, and it
// rotates. ofetch v4-flash s15 raised four unexercised findings over one
// request, and the four sets digested to `f3c9d09f`, `8b9bcc0a`, `84152215`,
// `64a910f1` — four different values — while `Count a circuit failure for
// body-read/stream-consumption errors` stood in every one of them and was never
// closed. A rule that compares sets bought four rounds for one behaviour and
// stopped on the round cap; a rule that compares names stops on the second.
type Finding struct {
	Kind  string
	Names []string
}

// Empty reports that this judgement raised nothing a round could be bought for.
func (f Finding) Empty() bool { return strings.TrimSpace(f.Kind) == "" }

// Row is the comparable half, for the journal: the kind, and a digest of the
// whole set. The names travel beside it on the row (store.JobGrowth.BoughtFor)
// because they are what the per-name rule is actually made of.
func (f Finding) Row() store.GrowthFinding {
	if f.Empty() {
		return store.GrowthFinding{}
	}
	return store.GrowthFinding{Kind: f.Kind, Names: findingDigest(f.Names)}
}

// spendable is the names this finding is bought name by name. A kind with no
// names of its own — an unreadable suite — is its own single name, because
// there is exactly one way for a project's checks to be unreadable and a round
// bought for it is bought for that one thing.
func (f Finding) spendable() []string {
	if f.Empty() {
		return nil
	}
	if len(f.Names) == 0 {
		return []string{f.Kind}
	}
	return f.Names
}

// FindingOf reads one delivery judgement as the finding a repair round would be
// bought to close.
//
// THE MEASUREMENT NAMES ITSELF WHERE IT CAN. store.DeliveryGate.Finding is the
// verification lane's own word for which measurement raised a gap —
// `removed-checks`, `regression`, `own-checks-failing`, `removed-public-name` —
// and where the record carries it, it IS the kind: a reading of the world says
// what it is, and nothing here gets to guess a better answer from the fields it
// happened to fill in. The cases below are for the gaps no measurement raised —
// a model judge reading the request, a coverage mapping — which have no such
// word and still need an identity, because a round bought for one of those is
// bought for something just as particular.
//
// A gate that PASSED raises nothing: there is no finding, and a round bought
// after it is bought for something else. So is a gate whose refusal was
// overturned — the finding was weighed against the world and lost, and a lost
// finding is not a standing one.
//
// THE NAMES COME FROM THE STRUCTURED FIELDS AND NOT FROM THE CITATION SAMPLE.
// A gate's citations are the spans one refusal was built on, bounded and
// chosen for a sentence a person reads; the lists are the measurement's whole
// answer. Reading the sample would spend and un-spend names according to which
// twelve a paragraph happened to quote.
func FindingOf(gate store.DeliveryGate) Finding {
	if gate.Pass || gate.Overturned {
		return Finding{}
	}
	if measured := strings.TrimSpace(gate.Finding); measured != "" {
		return Finding{Kind: measured, Names: measuredNames(gate)}
	}
	switch {
	case gate.Unreadable:
		return Finding{Kind: FindingUnreadable}
	case len(gate.OwnFailing) > 0:
		return Finding{Kind: FindingOwnFailing, Names: gate.OwnFailing}
	case len(gate.Unexercised) > 0:
		return Finding{Kind: FindingUnexercised, Names: gate.Unexercised}
	case len(gate.Unasserted) > 0:
		return Finding{Kind: FindingUnasserted, Names: gate.Unasserted}
	case len(gate.Consumers) > 0:
		return Finding{Kind: FindingConsumers, Names: gate.Consumers}
	case gate.Mechanical:
		return Finding{Kind: FindingMechanical, Names: gate.Cited()}
	case len(gate.Cited()) > 0 || strings.TrimSpace(gate.Gap) != "":
		return Finding{Kind: FindingReview, Names: gate.Cited()}
	}
	// A refusal with neither a list nor a citation nor a sentence is a gate that
	// recorded nothing anyone could aim a round at. It is left empty rather than
	// given a kind, because an empty finding refuses nothing — the fail-safe
	// direction for a rule that stops work.
	return Finding{}
}

// measuredNames is the whole answer behind a measurement's own word, preferring
// the list the measurement filled to the citations it was summarised into.
// A kind whose evidence has no list of its own keeps its citations, which are
// then the only names there are.
func measuredNames(gate store.DeliveryGate) []string {
	for _, list := range [][]string{gate.OwnFailing, gate.Unexercised, gate.Unasserted, gate.Consumers} {
		if len(list) > 0 {
			return list
		}
	}
	return gate.Cited()
}

// findingDigest reduces a finding's names to something two rounds can be
// compared by. A finding with no names — an unreadable suite — digests to
// nothing, and its KIND is then the whole of its identity.
//
// It is no longer what decides whether a round is bought — the names are — and
// it stays because it is what an autopsy sorts by: two rows with one digest are
// two gates that weighed the identical set, which is a fact worth being able to
// see at a glance.
func findingDigest(names []string) string {
	cleaned := make([]string, 0, len(names))
	for _, name := range names {
		if flat := findingName(name); flat != "" {
			cleaned = append(cleaned, flat)
		}
	}
	if len(cleaned) == 0 {
		return ""
	}
	sort.Strings(cleaned)
	sum := sha256.Sum256([]byte(strings.Join(cleaned, "\n")))
	return hex.EncodeToString(sum[:8])
}

// findingName is one name as it is compared: case dropped and runs of
// whitespace collapsed, because those are the two ways one behaviour is written
// twice without being a different behaviour, and nothing else is. Whole names
// on both sides — this is an equality test, never a similarity one.
func findingName(name string) string {
	return strings.ToLower(strings.Join(strings.Fields(name), " "))
}

// FindingNoun is what this kind's names ARE, in a person's words. The closing
// line counts them, and "4 findings" tells a reader nothing that "4 behaviours"
// does not tell them better.
func FindingNoun(kind string, count int) string {
	noun := "finding"
	switch kind {
	case FindingUnexercised, FindingUnasserted:
		noun = "behaviour"
	case FindingOwnFailing, "own-checks-failing", "removed-checks", "regression":
		noun = "check"
	case "removed-public-name":
		noun = "public name"
	case FindingConsumers:
		noun = "definition"
	case FindingMechanical:
		noun = "promised file"
	}
	if count == 1 {
		return noun
	}
	return noun + "s"
}

// FindingWords says a bounded handful of names as a person reads them.
func FindingWords(names []string) string {
	if len(names) == 0 {
		return ""
	}
	shown := names
	more := 0
	if len(shown) > findingsNamed {
		more, shown = len(shown)-findingsNamed, shown[:findingsNamed]
	}
	trimmed := make([]string, 0, len(shown))
	for _, name := range shown {
		trimmed = append(trimmed, strings.TrimSpace(name))
	}
	words := strings.Join(trimmed, "; ")
	if more > 0 {
		words += fmt.Sprintf("; and %d more", more)
	}
	return words
}

// findingsNamed bounds how many names one closing line spells. Three is what a
// sentence carries; the count in front of them is the finding.
const findingsNamed = 3

// findingNamesLimit bounds how many names one journal row keeps. A job with more
// than this many behaviours open at once has a shape problem no journal can fix,
// and a row that printed them all would be a row nobody reads — but it is well
// above the widest checklist this system has been measured raising (18), because
// TRUNCATING THIS LIST SILENTLY UN-SPENDS A NAME.
const findingNamesLimit = 64

func boundedNames(names []string) []string {
	if len(names) == 0 {
		return nil
	}
	if len(names) > findingNamesLimit {
		names = names[:findingNamesLimit]
	}
	return append([]string(nil), names...)
}

// ── the finding this round is being bought for ───────────────────────────────
//
// It rides the context the growth call already travels on, for the reason the
// plan anchor and the plan records do: the paths that grow a running job are
// reached through signatures owned by other waves, and the finding is a
// property of the JUDGEMENT that convened the growth rather than of any of
// them. A caller that says nothing keeps exactly the governors it had.

type findingKey struct{}

// WithFinding names the finding a growth about to be asked for is bought to
// close. The empty finding removes it.
func WithFinding(ctx context.Context, finding Finding) context.Context {
	if ctx == nil {
		return nil
	}
	return context.WithValue(ctx, findingKey{}, finding)
}

// FindingFrom is that finding, or the empty one where nobody said.
func FindingFrom(ctx context.Context) Finding {
	if ctx == nil {
		return Finding{}
	}
	finding, _ := ctx.Value(findingKey{}).(Finding)
	return finding
}

// spentNames is every name of this finding that has already had its two rounds.
//
// A NAME THAT STOOD THROUGH TWO ROUNDS BOUGHT FOR IT IS SPENT. "Stood" needs no
// separate reading: the name is in the finding a gate is raising NOW, so every
// earlier round bought for it ended with it still open. Two such rounds and it
// may buy no more — the same floor every other rule here keeps, one name at a
// time instead of one set at a time.
//
// Rounds of another kind are not counted. `unexercised: X` and `regression: X`
// are two different things about one name, answered by different work, and a
// round bought for one says nothing about the other.
func spentNames(rounds []store.JobGrowthRound, finding Finding) []string {
	names := finding.spendable()
	if len(names) == 0 {
		return nil
	}
	bought := make(map[string]int, len(names))
	for _, round := range rounds {
		if !round.Allowed || round.Finding.Kind != finding.Kind {
			continue
		}
		for _, name := range round.BoughtFor {
			if flat := findingName(name); flat != "" {
				bought[flat]++
			}
		}
	}
	spent := make([]string, 0, len(names))
	for _, name := range names {
		if bought[findingName(name)] >= 2 {
			spent = append(spent, name)
		}
	}
	if len(spent) == 0 {
		return nil
	}
	return spent
}

// unspentNames is the rest: the names that can still buy a round. A round is
// bought only if its finding holds at least one of them.
func unspentNames(finding Finding, spent []string) []string {
	if len(spent) == 0 {
		return finding.spendable()
	}
	gone := make(map[string]bool, len(spent))
	for _, name := range spent {
		gone[findingName(name)] = true
	}
	fresh := make([]string, 0, len(finding.spendable()))
	for _, name := range finding.spendable() {
		if !gone[findingName(name)] {
			fresh = append(fresh, name)
		}
	}
	return fresh
}
