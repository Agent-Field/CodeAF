package exec

import (
	"fmt"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// StopToolTimeouts is the reason recorded when one command has timed out often
// enough that running it again would repeat a fact the leaf already knows.
const StopToolTimeouts StopReason = "tool-timeouts"

// toolTimeoutRepeatCap is how many timeouts of one exact command establish that
// the command does not finish in this round. The key includes the tool name and
// the call's exact argument text, so harmless JSON reformatting undercounts and
// leaves the leaf alone rather than stopping it on an approximation.
const toolTimeoutRepeatCap = 3

// toolTimeoutGuard counts timeouts by exact call across one round. It is not
// safe for concurrent use; the loop presents a completed turn to it only after
// all of that turn's calls have returned.
type toolTimeoutGuard struct {
	timeouts map[string]int
}

func newToolTimeoutGuard() *toolTimeoutGuard {
	return &toolTimeoutGuard{timeouts: map[string]int{}}
}

// observe records only the timeout fact carried by a tool result. Successful
// calls and every other failure leave the count alone because neither says that
// this exact command does not return here.
func (g *toolTimeoutGuard) observe(calls []ai.ToolCall, results []Result) (string, int, bool) {
	for index, call := range calls {
		if index >= len(results) || !results[index].timedOut {
			continue
		}
		key := callSignature(call)
		g.timeouts[key]++
		if g.timeouts[key] >= toolTimeoutRepeatCap {
			return toolTimeoutReason(call), g.timeouts[key], true
		}
	}
	return "", 0, false
}

// toolTimeoutReason names the command using the shell tool's one parser. An
// array therefore reads with && because each step runs only after the previous
// one succeeds; malformed arguments fall back to their raw text so the reason
// never hides the command that reached the bound.
func toolTimeoutReason(call ai.ToolCall) string {
	command := shellCommandOf(call)
	if command == "" {
		command = strings.TrimSpace(call.Function.Arguments)
	}
	return fmt.Sprintf(
		"the same command timed out %d times, so it was stopped rather than run again: %s",
		toolTimeoutRepeatCap, snip(command, ranArgumentBytes))
}

// journalToolTimeout writes the ending where both the headless stream and the
// remainder judgement already read it. Attempt stays unset because the
// executor does not know which attempt of a claim it is, and guessing would
// make the durable record less truthful than leaving it unknown.
func journalToolTimeout(history *store.Store, task Task, outcome *Outcome, reason string) {
	if history == nil || strings.TrimSpace(task.StoreNodeID) == "" {
		return
	}
	record := store.LeafExhausted{
		Bound: string(StopToolTimeouts), Reason: reason, Turns: outcome.Turns,
		Meter: outcome.Meter.Name, Reached: outcome.Meter.Reached,
		Allowance: outcome.Meter.Allowed, Unit: outcome.Meter.Unit,
	}
	_ = history.RecordLeafExhausted(task.StoreNodeID, record)
}
