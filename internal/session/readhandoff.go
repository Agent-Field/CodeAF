package session

// readhandoff.go — the engine's answer to a sweep the model will not hand off.
//
// THE PROMPT TAUGHT THE JUDGEMENT AND THE MODELS RECITED IT WITHOUT ACTING ON
// IT. Three formulations of the hand-off rule — the original doctrine, the
// reason stated plainly, and the default reversed — produced identical
// behaviour across two model families: every bounded lookup stayed inline, and
// an explicit "run these in parallel" came back as sequential calls. A model
// deep in a turn does not feel the two costs the rule is about: its own
// context, which every raw read spends, and the person's clock. The loop is
// the one place those costs are facts rather than instruction, so the loop
// spends the hand-off itself.
//
// THE RULE IS ABOUT THE SHAPE OF THE READING, never about its domain. A turn
// that reaches its third distinct read-only target — read, grep, find, ls, the
// same set the early-start law enumerates in [earlyTools] — is enumerating a
// set, and a set's value to the person is its distilled result, never the
// watching of it being read. One file stays a step. Two files stay a step:
// that is the chat reading what it must hold to defend its answer. The third
// distinct target is where reading-to-reason ends and reading-to-report
// begins, and from there the remainder goes to a quick task whose last message
// alone comes back.
//
// THE FALSE POSITIVE THIS OWES AN ANSWER TO is the turn that reads several
// files because the answer must reason across them. It is answered by the
// shape of the fallback, not by a list of exceptions: the quick task returns
// the distilled answer, and a model that then needs one file's exact bytes to
// defend that answer reads that one file — a single targeted read, which never
// re-arms the sweep. What the chat loses is the raw text it would not have
// been able to quote anyway; what it keeps is the ability to interrogate.
//
// EVERY FAILURE FALLS BACK TO THE ORDINARY PATH. A refusal at the door, a
// quick task that fails, a wait that outlives its budget, an empty answer —
// any of them runs the batch inline exactly as if the hook were not here, and
// the sweep is disabled for the rest of the turn so a failure is paid once.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/provider"
)

// readSweepTargets is the number of distinct read-only targets at which a turn
// stops being a reader and starts being a sweeper. Three, because two files is
// a comparison the chat may be holding and three is a set.
const readSweepTargets = 3

// readSweepWait is the most a turn will block on the hand-off before falling
// back to running the batch inline. The quick task's own deadline is far
// longer; this is the conversation's patience, not the worker's.
const readSweepWait = 4 * time.Minute

// readSweepStartWait is the most a turn will wait for the quick task to START.
// The frontier starts a node it may start within a breath of admission, so a
// node still queued after this long is a node the governor is holding — and a
// held node is a reason to run the batch inline now, not to freeze the chat.
const readSweepStartWait = 30 * time.Second

// readSweep is one turn's ledger of read-only calls. It lives beside the
// turn's warmBatch: same lifetime, same owner, reset by the same events that
// break a reading run — any call that is not one of the four readers.
type readSweep struct {
	targets  map[string]bool
	glosses  []string
	disabled bool
}

// sweepKey identifies one read-only call by what it reads, not by its id: the
// same read issued twice is one target, and a re-read of a file the turn
// already holds never moves the count. Arguments are JSON-compacted so
// whitespace variations do not register as distinct targets.
func sweepKey(call ai.ToolCall) string {
	raw := strings.TrimSpace(call.Function.Arguments)
	var compacted bytes.Buffer
	if err := json.Compact(&compacted, []byte(raw)); err == nil {
		raw = compacted.String()
	}
	return call.Function.Name + "\x00" + raw
}

// count folds one EXECUTED batch into the ledger. A batch with anything but
// the four readers in it breaks the run: the turn moved from reading to doing,
// and the next read starts a new count.
func (s *readSweep) count(calls []ai.ToolCall) {
	if s.disabled {
		return
	}
	for _, call := range calls {
		if !earlyTools[call.Function.Name] {
			s.targets = nil
			s.glosses = nil
			return
		}
	}
	for _, call := range calls {
		if s.targets == nil {
			s.targets = map[string]bool{}
		}
		key := sweepKey(call)
		if !s.targets[key] {
			s.targets[key] = true
			s.glosses = append(s.glosses, gloss(call))
		}
	}
}

// due says whether this batch pushes the turn's distinct read-only targets to
// the sweep line. The batch's own readers are what count: a bash or a write
// riding beside them changes nothing, because the calls in one batch are
// emitted blind to each other's results — the sweep a model sizes with a wc is
// still a sweep, and the read-only subset is what gets handed off.
func (s *readSweep) due(calls []ai.ToolCall) bool {
	if s.disabled || len(calls) == 0 {
		return false
	}
	seen := map[string]bool{}
	for key := range s.targets {
		seen[key] = true
	}
	readers := 0
	for _, call := range calls {
		if !earlyTools[call.Function.Name] {
			continue
		}
		readers++
		seen[sweepKey(call)] = true
	}
	return readers > 0 && len(seen) >= readSweepTargets
}

// handoffReadSweep runs the interception: the batch's readers and the rest of
// their set go to one quick task, and the task's distilled answer comes back
// as the first reader's result. Everything else in the batch — the wc that
// sized the reading, the write the model already knew it wanted — runs the
// ordinary way beside the hand-off, since it was emitted blind to the reads.
// A nil return is every failure road at once — the caller then runs the whole
// batch inline exactly as if the hook had not fired, and the sweep is disabled
// so the failure is not re-tried round after round.
func (a *Agent) handoffReadSweep(ctx context.Context, ep *episode, hub *eventHub, user userMessage, calls []ai.ToolCall, sweep *readSweep, warm *warmBatch) []toolResult {
	sweep.disabled = true
	var readers, rest []ai.ToolCall
	for _, call := range calls {
		if earlyTools[call.Function.Name] {
			readers = append(readers, call)
		} else {
			rest = append(rest, call)
		}
	}
	id, _, refusal := a.admitQuick(quickAsk{
		line:  sweepBrief(user, sweep.glosses, readers),
		title: sweepTitle(user),
	})
	if refusal.said != "" {
		return nil
	}

	if len(calls) > 0 {
		a.tellPhase(provider.PhaseRunning, calls[0].Function.Name, time.Now())
		defer a.endPhase()
	}

	// THE ROWS THE PERSON SEES SAY WHERE THE READING WENT. The intercepted
	// calls are announced with the hand-off as their hint and finished when
	// the answer lands; the rest of the batch draws its own rows in
	// runToolsWarm, while the quick task runs.
	rendered := make([]string, len(readers))
	for index, call := range readers {
		rendered[index] = argsText(call)
		if hub != nil {
			hub.send(Event{
				Kind:   EventToolBegin,
				Tool:   call.Function.Name,
				Hint:   fmt.Sprintf("handed to quick task %d", id),
				Args:   rendered[index],
				CallID: call.ID,
			})
		}
	}
	started := time.Now()
	var restResults []toolResult
	if len(rest) > 0 {
		restResults = a.runToolsWarm(ctx, ep, rest, hub, warm)
	}
	answer, ok := a.awaitQuickAnswer(ctx, id)
	if !ok || strings.TrimSpace(answer) == "" {
		// FALLBACK: The quick task failed, timed out, or returned an empty answer.
		// Clean up the task node so it does not linger in the graph consuming resources.
		a.cancelTask(id, "read handoff failed; running inline")

		// Any non-reader calls in the batch already ran in restResults and must NOT
		// be run a second time. Run the readers inline and stitch the results back together.
		readerResults := a.runToolsWarm(ctx, ep, readers, hub, warm)
		byID := map[string]toolResult{}
		for index, call := range readers {
			byID[call.ID] = readerResults[index]
		}
		for index, call := range rest {
			byID[call.ID] = restResults[index]
		}
		results := make([]toolResult, len(calls))
		for index, call := range calls {
			results[index] = byID[call.ID]
		}
		sweep.targets = nil
		sweep.glosses = nil
		// Keep sweep.disabled = true so failure is paid once this turn.
		a.noteCallOutcomes(calls, results)
		return results
	}

	took := time.Since(started)
	for index, call := range readers {
		a.file.appendTook(call.ID, took)
		if hub != nil {
			hub.send(Event{
				Kind:   EventToolFinished,
				Tool:   call.Function.Name,
				Args:   rendered[index],
				CallID: call.ID,
				Took:   took,
			})
		}
	}

	sweep.targets = nil
	sweep.glosses = nil
	sweep.disabled = false

	byID := map[string]toolResult{}
	for index, call := range readers {
		text := sweepCoveredWord(id)
		if index == 0 {
			text = sweepHandoffWord(id) + "\n\n" + answer
		}
		byID[call.ID] = a.finishToolResult(ep, call, toolResult{text: text, harness: true})
	}
	for index, call := range rest {
		byID[call.ID] = restResults[index]
	}
	results := make([]toolResult, len(calls))
	for index, call := range calls {
		results[index] = byID[call.ID]
	}
	a.noteCallOutcomes(calls, results)
	return results
}

// awaitQuickAnswer blocks on the node's landing — the per-attempt done channel
// every landing closes — and returns the kept answer. Two clocks bound it, not
// one: a node that has not STARTED inside [readSweepStartWait] is a node the
// governor is holding, and a conversation does not freeze for a held node —
// the batch runs inline and the task is cancelled. Once started, the wait is
// the conversation's patience and the turn's own context: a stopped turn
// abandons the wait.
func (a *Agent) awaitQuickAnswer(ctx context.Context, id uint64) (string, bool) {
	timer := time.NewTimer(readSweepWait)
	defer timer.Stop()
	startTimer := time.NewTimer(readSweepStartWait)
	defer startTimer.Stop()
	for {
		node := a.graph().node(id)
		if node == nil {
			return "", false
		}
		switch node.stateNow() {
		case TaskDone:
			return node.result().text, true
		case TaskFailed:
			return "", false
		case TaskQueued:
			// Verified via startTimer below.
		}
		select {
		case <-node.done:
			// done is per-attempt and reopened on a retry, so a closed channel
			// is a reason to look at the state again, never a verdict.
		case <-ctx.Done():
			return "", false
		case <-startTimer.C:
			if node.stateNow() == TaskQueued {
				return "", false
			}
		case <-timer.C:
			return "", false
		}
	}
}

// sweepBrief is the whole of what the worker knows: the person's question, the
// reading already done, and the calls to perform first. A quick task opens
// cold — no transcript, no inherit — so the question travels in the brief or
// not at all.
func sweepBrief(user userMessage, glosses []string, calls []ai.ToolCall) string {
	question := clip(strings.TrimSpace(messageTextValue(user.message)), briefAskLimit)
	var b strings.Builder
	b.WriteString("A conversation began a reading it should not finish in its own context. ")
	if question != "" {
		b.WriteString("The person asked: " + question + "\n\n")
	}
	if len(glosses) > 0 {
		b.WriteString("The chat already ran these read-only calls; their results are in the conversation, so do not repeat them unless you must:\n")
		for _, g := range glosses {
			b.WriteString("- " + g + "\n")
		}
		b.WriteString("\n")
	}
	b.WriteString("Perform these calls first, then continue whatever reading the question needs:\n")
	for _, call := range calls {
		b.WriteString("- " + gloss(call) + "\n")
	}
	b.WriteString("\nReturn ONLY the distilled answer to the person's question — the answer itself, not a narration of what you read. Do not write or edit any file.")
	return b.String()
}

// sweepTitle names the row the person sees while the reading runs.
func sweepTitle(user userMessage) string {
	question := strings.TrimSpace(messageTextValue(user.message))
	if question == "" {
		return "reading for the chat"
	}
	return "reading: " + clip(firstLine(question), 60)
}

// sweepHandoffWord is the lead on the distilled answer, and the whole of what
// the model is told about the hand-off: that it happened, that the raw reads
// never entered the conversation, and the one road back to exact bytes.
func sweepHandoffWord(id uint64) string {
	return fmt.Sprintf("[This reading was handed to quick task %d, which finished it and returned the distilled answer below — the raw reads never entered this conversation. If you need one file's exact text to defend the answer, read that one file directly.]", id)
}

// sweepCoveredWord answers every call after the first in an intercepted batch:
// one hand-off covers the set, and the answer rides on the first call.
func sweepCoveredWord(id uint64) string {
	return fmt.Sprintf("[Covered by the handoff to quick task %d — the distilled answer is on the first call of this batch.]", id)
}
