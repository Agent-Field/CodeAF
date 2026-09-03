package plan

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/Agent-Field/aforge-v2/internal/guard"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// spinePrompt asks for the ordered skeleton and nothing else.
//
// Every guard in it pushes toward fewer stages, because a stage is the one
// thing in this design that genuinely costs wall clock — nodes within a stage
// are free, stages are serial. Left alone a model returns the tidy five-phase
// plan it saw a million times in training, and each of those phases is a
// barrier the binding pass then has to spend a whole round tearing down.
//
// The single-stage escape is stated as a good answer rather than tolerated as
// an edge case. A general-purpose harness is handed small goals constantly —
// draft this email, summarise that page — and a planner that cannot say "this
// is one piece of work" will manufacture a ten-node graph for something one
// agent finishes in a paragraph.
const spinePrompt = `You break a goal into its ordered stages.

` + agentPremise + `

A stage boundary is a hard gate: nothing in the next stage can begin until this
stage's output exists. But a gate is not merely "B reads A's output" — a
single agent reads its own files sequentially all the time, and that sequence
lives inside one stage, not between two. A gate exists only when the work in
the later stage would otherwise run at the same time as the earlier stage —
the gate is what serialises work that would otherwise be concurrent. If the
two could not run in parallel even without the gate, they are one stage.

Three things and only three things make a real gate. Separate stages require
at least one:
1. Parallelism that would otherwise be serialized: the earlier stage and the
   later stage are genuinely independent workers, and the only thing keeping
   the later one from starting is that it needs the earlier one's output.
   Without the gate they would race; the gate makes the race a handoff. The
   parallelism must be worth its price: every worker pays a fixed cost of
   orientation and setup before it produces anything, so work whose parts are
   each smaller than that fixed cost is cheaper inside one worker, not spread
   across several. Parallel units that one agent could finish in a single
   sitting are not a reason for stages.
2. Worker isolation: the later stage needs a different workspace, harness, or
   set of skills than the earlier one — a setup change that cannot happen
   inside one agent's turn loop.
3. Context-window pressure, which is measured for you and never estimated by
   you. Where the material the goal names is larger than one worker holds, you
   are told so below in as many words, with both figures. Told that, treat it
   as a gate: lay the work out so that each stage's worker carries its own
   share of the material and not all of it. Not told it, context pressure is
   not a reason for a stage — do not guess at the size of anything.

A single agent working through its own files in sequence meets none of these.
The sequence is inside the worker, not between workers, and serialising it
into stages adds barriers without buying any concurrency. That is one stage.

Every stage you add makes the whole goal slower, because stages run one after
another. Use the fewest that are genuinely gated: 1 to 4.

Return exactly one stage when the goal has no real gate — when everything in it
could be worked on at the same time, or when it is small enough that one agent
finishes it in a single pass. That is a correct and common answer, not a
failure to decompose.

A goal that bundles several requests which do not feed each other is that
single-stage case in disguise, and it is the one most often missed: the order
they were listed in is the order they were spoken in, never a gate. Do not lay
them out as stages — laying a bundle end to end makes every request wait for
strangers. They are one stage, they divide into parts there, and they run at
the same time. A stage exists where one of the three gates above fires, and
nowhere else.

One request among several is the exception, and missing it is the worse of the
two mistakes: the request whose own job is to work over what the others produce
— to assemble them, compare them, weigh them against each other, or write them
up as a single thing. It cannot begin before they have finished, and the
others are genuinely independent workers that would run at the same time
without it. Put it in a stage of its own behind them; left beside them it
starts against the very material it exists to consume, and produces that
material itself rather than wait. That is the only reason requests spoken in
one breath ever need a second stage.


For every stage, list in "needs" the numbers of the earlier stages whose
output it consumes — the files, results or answers it reads, without which it
cannot begin. A stage that starts from the goal and the workspace alone lists
none: []. This list is the schedule, and the order you write the stages in is
not: stages that need nothing from each other run at the same time whatever
order they are listed in, and a stage waits for exactly the stages it names.
Naming a stage because it was written earlier, rather than because its output
is consumed, makes work wait for strangers.

Do not add a final merge, synthesis, or summary stage. That is added
automatically after you.

` + titleRule + `

` + proportionRule + `

` + verdictRule

var spineSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "stages": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "title":   { "type": "string" },
          "summary": { "type": "string" },
          "needs":   { "type": "array", "items": { "type": "integer" } }
        },
        "required": ["title", "summary", "needs"],
        "additionalProperties": false
      }
    }
  },
  "required": ["stages"],
  "additionalProperties": false
}`)

// SpineChoice records what the sampling saw, so the spread is reportable rather
// than hidden. The spread is the honest measure of how much of the final graph
// was decided by luck.
//
// Drawn is how many stages each sample wrote and Spread is how many it has
// once the stages' stated needs are read (see levelled). The two differ
// exactly when a model laid independent work end to end, which is worth
// seeing in a report: it is the difference between a plan that ran one worker
// and a plan that ran seven.
type SpineChoice struct {
	Stages  []Stage
	Samples int
	Drawn   []int // stage count of each sample as written, ascending
	Spread  []int // stage count of each sample after levelling, ascending
	Agreed  bool  // every sample proposed the same number of stages
}

// Spine runs the planner's one serial call, several times at once, and keeps
// the most typical answer.
//
// This is a variance fix, not a quality fix. The spine decides the stage count
// and the framing that every later pass inherits, so a single unlucky sample
// does not degrade the graph a little — it changes the graph entirely. Measured
// on one fixed goal, single-sample runs produced 0, 9, 15 and 23 edges, and the
// 0-edge run came from an outlier spine that no later pass could recover from.
//
// Sampling is close to free here in the only currency that matters. The calls
// run concurrently, so the wall clock is one call however many we take, and the
// spine is the cheapest call in the system — three samples cost a fraction of a
// cent. Selection is done in code rather than by a judge call precisely to keep
// it that way: a judge would add a serial round to the one path that has no
// other serial work to hide behind.
//
// The terrain is handed in beside the goal because the stage count is a
// judgment about the work, and what is already on disk is half of that judgment:
// a goal whose first stage is "gather the responses" is one stage shorter when
// the responses are sitting in the workspace already. Like grounding, this runs
// before there is a graph to read a preamble from, so it takes the snapshot
// directly. Empty leaves the prompt exactly as it was.
// The requests the ask was read as containing travel beside the terrain and for
// the same reason. The stage count is a judgment about the work, and how the
// person divided their own ask is part of that judgment: several requests that
// do not feed each other are one stage, and one request written over what the
// others produce is the second. Nothing here decides which; the block says what
// was asked and the model reads it. Empty leaves the prompt exactly as it was.
// The measurement of what the goal names is the third thing handed in, and it
// is the only one of the three the spine is not asked to judge. Gate 3 above
// used to ask this call whether the work was too large for one worker to hold —
// a question about a number lying on the disk, put to a model that cannot see
// it. Now the number is read and stated, and where it says the named material
// is larger than one worker's reach a sample answering "one stage" is set aside
// before the vote rather than argued with inside it. See reach.go and
// admissible.
func Spine(ctx context.Context, client Completer, goal, terrain string, asked []string, named Measurement, samples int) (*SpineChoice, Usage, error) {
	return spineWithProgress(ctx, client, goal, terrain, asked, named, samples, nil)
}

func spineWithProgress(ctx context.Context, client Completer, goal, terrain string, asked []string, named Measurement, samples int, progress Progress) (*SpineChoice, Usage, error) {
	goal = strings.TrimSpace(goal)
	if goal == "" {
		return nil, Usage{}, errors.New("goal is required")
	}
	if samples < 1 {
		samples = 1
	}

	type result struct {
		stages []Stage
		usage  *ai.Usage
		err    error
	}
	results := make([]result, samples)
	var group sync.WaitGroup
	var progressMutex sync.Mutex
	completed := 0
	for index := 0; index < samples; index++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			// One faulted sample is one fewer candidate, which is the shape the
			// selection below already handles: it chooses among what came back
			// and reports the failures with the rest. A sample that already
			// landed keeps its slot — the only thing after it is the progress
			// tick, and a caller's callback must not cost a good spine.
			landed := false
			defer func() {
				if recovered := recover(); recovered != nil {
					fault := guard.Note(fmt.Sprintf("plan/spine sample %d", index+1), recovered)
					if !landed {
						results[index] = result{err: fault}
					}
				}
			}()
			stages, usage, err := spineOnce(ctx, client, goal, terrain, asked, named)
			results[index] = result{stages: stages, usage: usage, err: err}
			landed = true
			if progress != nil && samples > 1 {
				// The unlock is deferred because emitProgress runs the caller's
				// callback: a fault inside it would otherwise leave this mutex
				// held and every other sample parked on it forever.
				func() {
					progressMutex.Lock()
					defer progressMutex.Unlock()
					completed++
					emitProgress(progress, "spine", fmt.Sprintf("sample %d/%d", completed, samples), "")
				}()
			}
		}(index)
	}
	group.Wait()

	var usage Usage
	var candidates [][]Stage
	var failures []error
	for _, item := range results {
		usage.Add(item.usage)
		if item.err != nil {
			failures = append(failures, item.err)
			continue
		}
		candidates = append(candidates, item.stages)
	}
	if len(candidates) == 0 {
		return nil, usage, joinErrors(failures)
	}

	// Levelled BEFORE the vote: two samples that wrote seven stages naming no
	// needs and one that wrote a single stage are the same answer, and the
	// medoid must see them as one so that the list-shaped pair cannot outvote
	// the one that said it plainly.
	choice := &SpineChoice{Samples: len(candidates)}
	for index, candidate := range candidates {
		choice.Drawn = append(choice.Drawn, len(candidate))
		candidates[index] = levelled(candidate)
		choice.Spread = append(choice.Spread, len(candidates[index]))
	}
	// The spread above is what the samples said, all of them, and it stays that
	// way: what the measurement narrows is the field the medoid chooses from,
	// never the record of what was drawn.
	choice.Stages = medoid(admissible(candidates, named))
	sort.Ints(choice.Drawn)
	sort.Ints(choice.Spread)
	choice.Agreed = choice.Spread[0] == choice.Spread[len(choice.Spread)-1]
	return choice, usage, nil
}

// medoid returns the candidate most like the others. With one outlier among
// three, the two that resemble each other both outscore it, so the odd answer
// is dropped without anything having to judge which is better — a question
// nothing cheap can answer, and the wrong question anyway. We are not looking
// for the best spine, only for the one that is not an accident.
func medoid(candidates [][]Stage) []Stage {
	if len(candidates) == 1 {
		return candidates[0]
	}
	best, bestScore := 0, -1.0
	for i, candidate := range candidates {
		score := 0.0
		for j, other := range candidates {
			if i != j {
				score += similarity(candidate, other)
			}
		}
		if score > bestScore {
			best, bestScore = i, score
		}
	}
	return candidates[best]
}

// similarity blends what the two spines say with how they are shaped. Word
// overlap alone would rank a four-stage spine close to a one-stage spine that
// happens to reuse its vocabulary, and stage count is the single most
// consequential thing a spine decides — it sets the depth of everything below.
func similarity(a, b []Stage) float64 {
	wordsA, wordsB := vocabulary(a), vocabulary(b)
	shared := 0
	for word := range wordsA {
		if wordsB[word] {
			shared++
		}
	}
	union := len(wordsA) + len(wordsB) - shared
	overlap := 0.0
	if union > 0 {
		overlap = float64(shared) / float64(union)
	}

	longest := len(a)
	if len(b) > longest {
		longest = len(b)
	}
	shape := 1.0
	if longest > 0 {
		shape = 1 - float64(abs(len(a)-len(b)))/float64(longest)
	}
	return 0.5*overlap + 0.5*shape
}

// vocabulary reduces a spine to the content words it uses. Short tokens are
// dropped because "the" and "and" appear in every spine and would pull every
// pair of candidates toward the same score.
func vocabulary(stages []Stage) map[string]bool {
	words := map[string]bool{}
	for _, stage := range stages {
		for _, word := range strings.FieldsFunc(strings.ToLower(stage.Title+" "+stage.Summary), func(r rune) bool {
			return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
		}) {
			if len(word) > 3 {
				words[word] = true
			}
		}
	}
	return words
}

func spineOnce(ctx context.Context, client Completer, goal, terrain string, asked []string, named Measurement) ([]Stage, *ai.Usage, error) {
	ctx = provider.WithCall(ctx, provider.ClassPlanSpine)
	messages := []ai.Message{
		systemMessage(spinePrompt),
		userMessage(goalBlock(goal, terrain, asked, named.Line())),
	}
	var decoded struct {
		Stages []Stage `json:"stages"`
	}
	response, err := structured(ctx, client, messages, spineSchema, &decoded)
	if err != nil {
		return nil, usageOf(response), fmt.Errorf("spine: %w", err)
	}
	stages := keptStages(decoded.Stages)
	if len(stages) == 0 {
		// Schema-valid and useless: the reply parsed, so nothing upstream of here
		// could have caught it. This is the semantic half of verification and the
		// only place it can be observed.
		provider.Report(ctx, provider.VerdictSemanticFailure)
		return nil, usageOf(response), annotate(errors.New("spine: no stages returned"), response)
	}
	provider.Report(ctx, provider.VerdictVerifiedSuccess)
	return stages, usageOf(response), nil
}

func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
