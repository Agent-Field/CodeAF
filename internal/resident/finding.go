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

// FindingOf reads one delivery judgement as the finding a repair round would be
// bought to close.
//
// A gate that PASSED raises nothing: there is no finding, and a round bought
// after it is bought for something else. So is a gate whose refusal was
// overturned — the finding was weighed against the world and lost, and a lost
// finding is not a standing one.
//
// The names are the structured list of whichever kind was read, lower-cased and
// sorted before they are digested, because a list is the same list whatever
// order a model happened to emit it in and whatever case it used. That is the
// same normalisation RemainderDigest makes of a sentence, for the same reason,
// and it is the whole of what is done to them: this is an EQUALITY test, so a
// finding that cites one more name than it did is a different finding and buys
// its own round.
func FindingOf(gate store.DeliveryGate) store.GrowthFinding {
	if gate.Pass || gate.Overturned {
		return store.GrowthFinding{}
	}
	kind, names := "", []string(nil)
	switch {
	case gate.Unreadable:
		kind = FindingUnreadable
	case len(gate.OwnFailing) > 0:
		kind, names = FindingOwnFailing, gate.OwnFailing
	case len(gate.Unexercised) > 0:
		kind, names = FindingUnexercised, gate.Unexercised
	case len(gate.Unasserted) > 0:
		kind, names = FindingUnasserted, gate.Unasserted
	case len(gate.Consumers) > 0:
		kind, names = FindingConsumers, gate.Consumers
	case gate.Mechanical:
		kind, names = FindingMechanical, gate.Cited()
	case len(gate.Cited()) > 0 || strings.TrimSpace(gate.Gap) != "":
		kind, names = FindingReview, gate.Cited()
	default:
		// A refusal with neither a list nor a citation nor a sentence is a gate
		// that recorded nothing anyone could aim a round at. It is left empty
		// rather than given a kind, because an empty finding refuses nothing —
		// the fail-safe direction for a rule that stops work.
		return store.GrowthFinding{}
	}
	return store.GrowthFinding{Kind: kind, Names: findingDigest(names), Cited: namedFew(names)}
}

// findingDigest reduces a finding's names to something two rounds can be
// compared by. A finding with no names — an unreadable suite — digests to
// nothing, and its KIND is then the whole of its identity, which is correct:
// there is only one way for a project's own checks to be unreadable.
func findingDigest(names []string) string {
	cleaned := make([]string, 0, len(names))
	for _, name := range names {
		flat := strings.ToLower(strings.Join(strings.Fields(name), " "))
		if flat != "" {
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

// FindingWords says a finding as a person reads it: the first name it cites and
// how many more there are. It is what the run's closing line points at when it
// says a finding stood, so a person told "this did not move" is told what.
func FindingWords(finding store.GrowthFinding) string {
	if len(finding.Cited) == 0 {
		if finding.Kind == FindingUnreadable {
			return "this project's own checks could not be read"
		}
		return "the same finding"
	}
	words := strings.TrimSpace(finding.Cited[0])
	if more := len(finding.Cited) - 1; more > 0 {
		words += fmt.Sprintf(" and %d more", more)
	}
	return words
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
func WithFinding(ctx context.Context, finding store.GrowthFinding) context.Context {
	if ctx == nil {
		return nil
	}
	return context.WithValue(ctx, findingKey{}, finding)
}

// FindingFrom is that finding, or the empty one where nobody said.
func FindingFrom(ctx context.Context) store.GrowthFinding {
	if ctx == nil {
		return store.GrowthFinding{}
	}
	finding, _ := ctx.Value(findingKey{}).(store.GrowthFinding)
	return finding
}

// findingStood is how many rounds this job has ALREADY bought for exactly this
// finding, counting back from the newest.
//
// Rounds nobody bought for a finding do not break the run — an overrun or a
// resumption that happened between two repair rounds says nothing about whether
// the finding moved — but a round bought for a DIFFERENT finding does: the job
// changed what it was working on, and the count starts again from there.
func findingStood(rounds []store.JobGrowthRound, finding store.GrowthFinding) int {
	if finding.Empty() {
		return 0
	}
	stood := 0
	for index := len(rounds) - 1; index >= 0; index-- {
		row := rounds[index]
		if !row.Allowed || row.Finding.Empty() {
			continue
		}
		if !row.Finding.Same(finding) {
			break
		}
		stood++
	}
	return stood
}
