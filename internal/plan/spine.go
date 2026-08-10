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
stage's output exists. If two stages could run at the same time, they are one
stage.

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
the same time. A stage exists where output feeds input, and nowhere else.

Do not add a final merge, synthesis, or summary stage. That is added
automatically after you.

` + titleRule + `

` + proportionRule

var spineSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "stages": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "title":   { "type": "string" },
          "summary": { "type": "string" }
        },
        "required": ["title", "summary"],
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
type SpineChoice struct {
	Stages  []Stage
	Samples int
	Spread  []int // stage count of each sample, ascending
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
func Spine(ctx context.Context, client Completer, goal, terrain string, samples int) (*SpineChoice, Usage, error) {
	return spineWithProgress(ctx, client, goal, terrain, samples, nil)
}

func spineWithProgress(ctx context.Context, client Completer, goal, terrain string, samples int, progress Progress) (*SpineChoice, Usage, error) {
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
			stages, usage, err := spineOnce(ctx, client, goal, terrain)
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

	choice := &SpineChoice{Stages: medoid(candidates), Samples: len(candidates)}
	for _, candidate := range candidates {
		choice.Spread = append(choice.Spread, len(candidate))
	}
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

func spineOnce(ctx context.Context, client Completer, goal, terrain string) ([]Stage, *ai.Usage, error) {
	ctx = provider.WithCall(ctx, provider.ClassPlanSpine)
	messages := []ai.Message{
		systemMessage(spinePrompt),
		userMessage(goalBlock(goal, terrain)),
	}
	var decoded struct {
		Stages []Stage `json:"stages"`
	}
	response, err := structured(ctx, client, messages, spineSchema, &decoded)
	if err != nil {
		return nil, usageOf(response), fmt.Errorf("spine: %w", err)
	}
	stages := make([]Stage, 0, len(decoded.Stages))
	for _, stage := range decoded.Stages {
		stage.Title = trim(stage.Title)
		stage.Summary = trim(stage.Summary)
		if stage.Title == "" && stage.Summary == "" {
			continue
		}
		stages = append(stages, stage)
	}
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
