package steploop

// aforge-embed: D11 — no-progress guard for the swe coder's step loop.
//
// This is an aforge-owned addition to the owned engine copy. Upstream
// swe-pro-go has no equivalent; the step loop's only convergence bound is
// MaxSteps (agent.steps), which is a hard ceiling rather than a progress
// signal. The guard catches a coder leaf that is spending steps without
// advancing — repeating the same tool call, going many steps without writing
// anything or learning anything new, or simply running past any honest leaf's
// measured need — and gives it one chance to conclude before terminating it
// with its partial result and a journaled reason.
//
// See internal/exec/noprogress.go for the same mechanism in aforge's own
// linear loop, and the divergence log (EMBEDDING.md D11) for the cherry-pick
// note.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/msgmodel"
)

// The three signals, and why each is a general criterion:
//
//   - Repeated identical tool calls: the same tool with the same input
//     producing the same output, several steps in a row.
//   - A stagnant window: several consecutive steps with no write tool and no
//     new tool output. A coder that is working writes or discovers; one that
//     does neither for several steps is circling.
//   - A step floor: past a generous multiple of what any honest coder leaf
//     has needed, the leaf is on rails. The conclude directive gives it a
//     chance to land, and a further trigger terminates.
//
// The thresholds match the linear loop's: 4 for the repeat cap, 6 for the
// stagnant window, 60 for the step floor. They are generous against the
// measured distribution of honest leaves and err on the side of firing late.

const (
	stepRepeatCap     = 4
	stepStagnantCap   = 6
	stepTurnFloor     = 60
	stepConcludeGrace = 4 // matching landingTurns in the linear loop
	// stepFloorWindow and stepFloorMinProductive shape the recent window the
	// floor reads: of the last twenty steps, fewer than five with a mutation
	// or new information is fidgeting — productive just often enough to dodge
	// the stagnant cap, never enough to finish. A step whose window shows
	// real work is never floor-stopped; the engine's own step and cost
	// budgets own that ceiling.
	stepFloorWindow        = 20
	stepFloorMinProductive = 5
)

// stepConcludeDirective is the message injected when the guard first fires.
// It mirrors the linear loop's directive in shape so the coder reads it as
// the same kind of instruction.
const stepConcludeDirective = "This task is not making forward progress — the same tool calls " +
	"are repeating without changing anything or learning anything new. Conclude " +
	"now: state what you finished and what you did not. Do not make any more tool calls."

// writeTools are the tool names that always write to the filesystem. A step
// containing any of these is a step that mutated something. The set is general
// — these are the harness's write tools — and erring toward marking a step as
// mutating is the safe direction: a false mutation never fires the guard.
var writeTools = map[string]bool{
	"write":       true,
	"edit":        true,
	"apply_patch": true,
}

// stepGuard tracks whether a coder leaf is making forward progress across the
// steps of one Run. It is not safe for concurrent use; one leaf is one loop.
type stepGuard struct {
	// Signal 1: the last tool call's signature and result, and the streak.
	lastCallKey   string
	lastResultKey string
	repeatCount   int

	// Signal 2: consecutive steps with no write and no new output.
	stagnantSteps int

	// Signal 3: total steps observed.
	steps int

	// productiveWindow is the ring of the last stepFloorWindow steps'
	// productivity (a step with a mutation or new information), and
	// productiveCount how many of those held true. The floor reads it to tell
	// a fidgeting leaf from a working one.
	productiveWindow [stepFloorWindow]bool
	productiveCount  int

	// seenOutputs is the set of all tool output hashes the leaf has seen.
	// An output whose hash is already here is "not new information".
	seenOutputs map[string]bool

	// concluded records whether the conclude directive has been injected.
	concluded         bool
	concludeRemaining int
}

func newStepGuard() *stepGuard {
	return &stepGuard{seenOutputs: map[string]bool{}}
}

// stepVerdict is the guard's decision after observing one step.
type stepVerdict int

const (
	stepProgress stepVerdict = iota
	stepConclude
	stepTerminate
)

// toolAction is one tool call's observable fields, extracted from the
// persisted parts.
type toolAction struct {
	tool   string
	input  string
	output string
}

// extractToolActions reads the completed tool parts from a step's parts and
// returns them in order. Pending or running parts carry no result and are
// not progress evidence either way, so they are skipped.
func extractToolActions(parts msgmodel.Parts) []toolAction {
	var actions []toolAction
	for _, part := range parts {
		toolPart, ok := part.(msgmodel.ToolPart)
		if !ok {
			continue
		}
		state := toolPart.State.ToolStatus()
		if state != msgmodel.ToolStatusCompleted && state != msgmodel.ToolStatusError {
			continue
		}
		input := ""
		if raw := toolPart.State.ToolInput(); raw != nil {
			if bytes, err := json.Marshal(raw); err == nil {
				input = string(bytes)
			}
		}
		output := ""
		if state == msgmodel.ToolStatusCompleted {
			if completed, ok := toolPart.State.(msgmodel.ToolStateCompleted); ok {
				output = completed.Output
			}
		} else {
			if errState, ok := toolPart.State.(msgmodel.ToolStateError); ok {
				output = "ERROR:" + errState.Error
			}
		}
		actions = append(actions, toolAction{
			tool:   toolPart.Tool,
			input:  input,
			output: output,
		})
	}
	return actions
}

// observe records one step's tool activity and returns the guard's verdict.
func (g *stepGuard) observe(parts msgmodel.Parts) stepVerdict {
	g.steps++
	actions := extractToolActions(parts)

	// A step with no completed tool calls is a thinking step — it carries no
	// signal either way. The guard leaves the stagnant counter where it is.
	// If the conclude directive was already injected, a thinking step is the
	// model complying (delivering its answer); the loop's ShouldExit path
	// handles the finish.
	if len(actions) == 0 {
		return stepProgress
	}

	mutated := false
	newInfo := false

	for _, action := range actions {
		if writeTools[action.tool] {
			mutated = true
		}
		key := hashOutput(action.output)
		if !g.seenOutputs[key] {
			g.seenOutputs[key] = true
			newInfo = true
		}
	}

	// Signal 1: repeated identical calls. The check is on the LAST call of
	// the step, matching the linear loop.
	last := actions[len(actions)-1]
	lastSig := last.tool + "\x00" + last.input
	lastRes := hashOutput(last.output)
	if lastSig == g.lastCallKey && lastRes == g.lastResultKey {
		g.repeatCount++
	} else {
		g.repeatCount = 1
	}
	g.lastCallKey = lastSig
	g.lastResultKey = lastRes

	// Signal 2: stagnant steps.
	if mutated || newInfo {
		g.stagnantSteps = 0
	} else {
		g.stagnantSteps++
	}
	// The floor's window records the same judgment: this step was productive
	// or it was not.
	{
		slot := int(g.steps-1) % stepFloorWindow
		if g.productiveWindow[slot] {
			g.productiveCount--
		}
		g.productiveWindow[slot] = mutated || newInfo
		if g.productiveWindow[slot] {
			g.productiveCount++
		}
	}

	// If the conclude directive was already injected, count down the grace.
	// A re-trigger of any signal terminates immediately.
	if g.concluded {
		if g.repeatCount >= stepRepeatCap || g.stagnantSteps >= stepStagnantCap {
			return stepTerminate
		}
		g.concludeRemaining--
		if g.concludeRemaining <= 0 {
			return stepTerminate
		}
		return stepProgress
	}

	// Signal 1.
	if g.repeatCount >= stepRepeatCap {
		return stepConclude
	}
	// Signal 2.
	if g.stagnantSteps >= stepStagnantCap {
		return stepConclude
	}
	// Signal 3: step floor, read against the recent window. Past the floor a
	// leaf whose window is mostly unproductive is fidgeting; a leaf whose
	// window shows real work is left to the engine's own budgets.
	if g.steps >= stepTurnFloor && g.productiveCount < stepFloorMinProductive {
		return stepConclude
	}

	return stepProgress
}

func (g *stepGuard) markConcluded() {
	g.concluded = true
	g.concludeRemaining = stepConcludeGrace
}

// reason assembles the journaled reason for a guard termination.
func (g *stepGuard) reason() string {
	var b strings.Builder
	b.WriteString("no-progress guard: ")
	switch {
	case g.repeatCount >= stepRepeatCap:
		b.WriteString("the same tool call repeated ")
		b.WriteString(stepItoa(g.repeatCount - 1))
		b.WriteString(" times with identical results")
	case g.stagnantSteps >= stepStagnantCap:
		b.WriteString(stepItoa(g.stagnantSteps))
		b.WriteString(" consecutive steps with no writes and no new information")
	default:
		b.WriteString("ran past ")
		b.WriteString(stepItoa(stepTurnFloor))
		b.WriteString(" steps with no forward progress")
	}
	return b.String()
}

func hashOutput(output string) string {
	sum := sha256.Sum256([]byte(output))
	return hex.EncodeToString(sum[:])
}

func stepItoa(n int) string {
	if n == 0 {
		return "0"
	}
	negative := n < 0
	if negative {
		n = -n
	}
	var digits [20]byte
	i := len(digits)
	for n > 0 {
		i--
		digits[i] = byte('0' + n%10)
		n /= 10
	}
	if negative {
		i--
		digits[i] = '-'
	}
	return string(digits[i:])
}
