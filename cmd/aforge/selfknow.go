package main

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/profile"
	"github.com/Agent-Field/aforge-v2/internal/provider"
)

const selfKnowledgeTTL = 5 * time.Minute

type cachedSelfKnowledge struct {
	profileDir string
	model      string
	text       string
	expires    time.Time
}

type selfKnowledgeBucket struct {
	tokens     []int
	turns      []int
	failures   int
	successes  int
	promotions int
	cost       float64
	surprises  []float64
}

var (
	selfKnowledgeMu     sync.Mutex
	selfKnowledgeCached cachedSelfKnowledge
)

func selfKnowledge(settings config.Config, model string) string {
	selfKnowledgeMu.Lock()
	defer selfKnowledgeMu.Unlock()

	now := time.Now()
	if selfKnowledgeCached.profileDir == settings.ProfileDir &&
		selfKnowledgeCached.model == model &&
		now.Before(selfKnowledgeCached.expires) {
		return selfKnowledgeCached.text
	}

	text := measureSelfKnowledge(settings, model)
	// Each specialist's own history, in its own block, under its own name. It
	// is appended rather than merged because these are different workers with
	// different capacities: a median that averaged a coding pipeline with a
	// lookup would describe neither, and the compiler choosing between them
	// needs exactly the difference the merge would destroy.
	for _, info := range exec.Subharnesses() {
		line := subharnessKnowledge(settings, model, info.Name)
		if line == "" {
			continue
		}
		if text != "" {
			text += "\n"
		}
		text += info.Name + ": " + line
	}
	selfKnowledgeCached = cachedSelfKnowledge{
		profileDir: settings.ProfileDir,
		model:      model,
		text:       text,
		expires:    now.Add(selfKnowledgeTTL),
	}
	return text
}

// subharnessKnowledge is the menu's measured line: one sentence about what this
// worker's leaves have actually cost, or nothing at all until enough of them
// have run to say anything honest. It is the hook exec.MenuText renders under
// each purpose, and it is why the compiler's choice is grounded in this
// session's own evidence rather than in the prior the worker shipped with.
//
// The gate is one evidence gate's worth of runs, the same MinSamples the ruler
// may not be rewritten below. A median of three leaves is not a measurement,
// and a compiler that read it as one would route on noise.
func subharnessKnowledge(settings config.Config, model, subharness string) string {
	measured, err := profile.Load(settings.ProfileDir, model, subharness)
	if err != nil || len(measured.Records) < profile.MinSamples {
		return ""
	}
	var tokens, turns []int
	var cost float64
	failures := 0
	for _, record := range measured.Records {
		tokens = append(tokens, record.Tokens)
		turns = append(turns, record.Turns)
		cost += record.Cost
		if positive, graded := record.Verdict.Graded(); graded && !positive {
			failures++
		}
	}
	samples := len(tokens)
	return fmt.Sprintf("median %d tokens, %d turns over %d runs; %.0f%% succeeded; avg cost $%.4f",
		selfKnowledgeMedian(tokens), selfKnowledgeMedian(turns), samples,
		100*float64(samples-failures)/float64(samples), cost/float64(samples))
}

// measuredInvoice is the same evidence subharnessKnowledge renders for the
// compiler, rendered for the three passes that decide whether to divide work.
//
// The two exist side by side rather than one being folded into the other
// because they answer different questions with the same measurements. The menu
// line prices a CHOICE between workers and is one sentence under each name; the
// invoice prices a DIVISION and needs what one piece costs before it does
// anything, and what reassembling several of them has cost. Merging them would
// make one of the two readers carry the other's numbers.
//
// It renders every worker's price list, generalist first, and it renders the
// empty string whenever nothing clears the evidence gate — which is a fresh
// machine, and which leaves every planning prompt byte for byte as it was.
func measuredInvoice(settings config.Config, model string) string {
	if strings.TrimSpace(model) == "" {
		model = settings.Model
	}
	workers := []string{plan.LinearSubharness}
	for _, info := range exec.Subharnesses() {
		workers = append(workers, info.Name)
	}
	invoices := make([]plan.Invoice, 0, len(workers))
	for _, worker := range workers {
		measured, err := profile.Load(settings.ProfileDir, model, worker)
		if err != nil {
			continue
		}
		// A worker below the evidence gate contributes nothing rather than a
		// thinner row: the whole value of this block is that a figure in it can
		// be read without a qualifier attached.
		if invoice, ok := plan.InvoiceFor(worker, measured.Records); ok {
			invoices = append(invoices, invoice)
		}
	}
	return plan.RenderInvoice(invoices...)
}

func measureSelfKnowledge(settings config.Config, model string) string {
	measured, err := profile.Load(settings.ProfileDir, model, "linear")
	if err != nil || len(measured.Records) < 5 {
		return ""
	}

	buckets := make(map[string]selfKnowledgeBucket)
	var globalTokens, globalTurns []int
	for _, record := range measured.Records {
		switch record.Size {
		case profile.BucketReflex, profile.BucketDirect, "atomic", "borderline", "oversized", "synthesis":
		default:
			continue
		}
		bucket := buckets[record.Size]
		bucket.tokens = append(bucket.tokens, record.Tokens)
		bucket.turns = append(bucket.turns, record.Turns)
		bucket.cost += record.Cost
		globalTokens = append(globalTokens, record.Tokens)
		globalTurns = append(globalTurns, record.Turns)
		if record.Surprise != nil {
			bucket.surprises = append(bucket.surprises, *record.Surprise)
		}
		if record.Size == profile.BucketReflex {
			if record.Promoted {
				bucket.promotions++
			} else if record.Verdict == provider.VerdictVerifiedSuccess ||
				record.Verdict == provider.VerdictUnverifiedSuccess {
				bucket.successes++
			}
		}
		positive, graded := record.Verdict.Graded()
		if graded && !positive {
			bucket.failures++
		}
		buckets[record.Size] = bucket
	}
	if len(globalTokens) == 0 {
		return ""
	}
	globalTokenMedian := selfKnowledgeMedian(globalTokens)
	globalTurnMedian := selfKnowledgeMedian(globalTurns)

	lines := make([]string, 0, len(buckets))
	for _, size := range []string{profile.BucketReflex, profile.BucketDirect, "atomic", "borderline", "oversized", "synthesis"} {
		bucket, ok := buckets[size]
		if !ok {
			continue
		}
		samples := len(bucket.tokens)
		tokens := selfKnowledgeShrunkMedian(selfKnowledgeMedian(bucket.tokens), globalTokenMedian, samples)
		turns := selfKnowledgeShrunkMedian(selfKnowledgeMedian(bucket.turns), globalTurnMedian, samples)
		miss := ""
		if len(bucket.surprises) > 0 {
			miss = fmt.Sprintf("; typical miss: ±%.0f%%", 100*selfKnowledgeMean(bucket.surprises))
		}
		if size == profile.BucketReflex {
			lines = append(lines, fmt.Sprintf("%s: median %d tokens, %d turns; n=%d, shrunk toward global%s; success=%.1f%%; promoted=%.1f%%; avg cost=$%.4f",
				size, tokens, turns, samples, miss,
				100*float64(bucket.successes)/float64(samples), 100*float64(bucket.promotions)/float64(samples), bucket.cost/float64(samples)))
			continue
		}
		failureShare := 100 * float64(bucket.failures) / float64(samples)
		lines = append(lines, fmt.Sprintf("%s: median %d tokens, %d turns; n=%d, shrunk toward global%s; failures=%.1f%%",
			size, tokens, turns, samples, miss, failureShare))
	}
	return strings.Join(lines, "\n")
}

// Eight pseudo-samples matches profile.MinSamples and the router's MinGraded:
// one evidence gate worth of work makes local and global history count equally.
const selfKnowledgeShrinkage = 8

func selfKnowledgeShrunkMedian(observed, global, samples int) int {
	return int(math.Round(float64(samples*observed+selfKnowledgeShrinkage*global) /
		float64(samples+selfKnowledgeShrinkage)))
}

func selfKnowledgeMean(values []float64) float64 {
	var total float64
	for _, value := range values {
		total += value
	}
	return total / float64(len(values))
}

func selfKnowledgeMedian(values []int) int {
	sort.Ints(values)
	return values[len(values)/2]
}
