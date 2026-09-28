package session

// THE SUMMARY IS THE LAST RUNG OF A COMPACTION PASS, and the only one that
// costs a model call.
//
// The two mechanical rungs (stub.go, the fold in loop.go) never touch a
// person's words and never touch the turn in hand, which is what makes them
// free and lossless. It is also what gives them a floor: a conversation whose
// weight is mostly what the person said — long questions, pasted logs, a story
// written together — or mostly the newest exchange, has nothing left that
// either rung may take, and every pass after that says "nothing to compact"
// while the window fills. Measured on 2026-09-28 against a 16k window: one
// question, one long answer and the next question were refused at 15,383 input
// tokens, and /compact found nothing eligible.
//
// So when the mechanical rungs cannot bring the conversation under the line
// the pass was asked to reach, the OLDEST part of the conversation — person and
// assistant alike — is replaced by one note the conversation's own model wrote
// about it. What never goes into a summary:
//
//   - the system prompt (message 0), which is rebuilt per turn anyway;
//   - the most recent person messages and everything after them
//     ([summaryKeepPersonMessages]; fewer only when keeping them cannot reach
//     the line), so the question being answered is always there word for word;
//   - the running turn, because the region ends before the person message that
//     opened it.
//
// The original lines stay above the compaction marker in the session journal,
// and the note names that file, so nothing is lost; the summary is what the
// MODEL reads from then on. A later summary folds the earlier note in rather
// than stacking a second one on it ([summaryPlan.previous]).
//
// THE CALL IS MADE WITH THE SESSION LOCK RELEASED. A summary takes seconds,
// and [Agent.Interrupt] wants the same lock; the old summarizer held it and
// could not be stopped. The region is copied before the call and compared
// after it, and a transcript that moved underneath — a rewind, anything that
// rewrote the prefix — keeps its shape and the summary is thrown away
// ([Agent.spliceSummaryLocked]). [Agent.compacting] stays set across the call,
// so no second pass can start in the gap.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/lane"
	"github.com/Agent-Field/codeaf/internal/provider"
)

const (
	// summaryKeepPersonMessages is how many of the person's most recent
	// messages a summary prefers to leave word for word, with everything after
	// the oldest of them. A region that cannot reach the pass's line while
	// keeping this many keeps fewer, down to one: the message the conversation
	// is currently answering is never summarized.
	summaryKeepPersonMessages = 3

	// summaryMinRegionTokens is the smallest region worth a model call. Below
	// it the note, its framing and the call's cost outweigh what it could free.
	summaryMinRegionTokens = 1024

	// summaryPromptTokens is what one summary request carries besides the
	// conversation it is summarizing: the instruction, the framing of the
	// user message, and the provider's message overhead.
	summaryPromptTokens = 512

	// summaryToolArgBytes and summaryToolResultBytes bound what one tool call's
	// arguments and one result contribute to the text the summarizer reads.
	// The summary needs to know what was done and what came back, not the
	// bytes: those stay in the journal the note points to.
	summaryToolArgBytes    = 400
	summaryToolResultBytes = 2000

	// summaryCallWindow bounds one summary request. The loop waits on it, so
	// it is a person's wait, and it is a bound rather than a budget.
	summaryCallWindow = 2 * time.Minute

	// auxRoleSummary names a summary's ledger line, so a person reading the
	// session's spending can see what the compaction call cost.
	auxRoleSummary = "summary"

	// purposeSummary is what the model-call log files a summary request under.
	purposeSummary callPurpose = "summary"
)

// summaryNotePrefix opens every summary note. It is the same opening an older
// codeaf's summarizer wrote ([legacyCompactionNote]), so [isCompactionNote]
// already treats both as the session's own words rather than the person's.
const summaryNotePrefix = "[context compacted]"

// summaryAnswerTokens is the largest summary asked for: a twentieth of the
// window, between 512 and 4,096 tokens.
func summaryAnswerTokens(window int) int {
	return min(4096, max(512, window/20))
}

// summaryPlan is one summary about to be written: the region it replaces and
// everything the call needs, taken under the lock and used outside it.
type summaryPlan struct {
	// end is where the region stops: messages[1:end] are summarized.
	end int
	// region is a copy of messages[1:end], compared against the live
	// transcript before the summary is spliced in.
	region []ai.Message
	// previous is the body of an earlier summary note at the head of the
	// region, which the new summary extends rather than repeats.
	previous string
	model    string
	window   int
	answer   int
	// pointer says where the original lines can be read back.
	pointer string
}

// summaryWanted says whether a pass that has already stubbed and folded still
// needs a summary. transcript is the conversation's size after those rungs.
func summaryWanted(policy compactPolicy, pass compactionPass, transcript int) bool {
	// A LINE THE TRANSCRIPT CANNOT REACH IS NOT A REASON TO SUMMARIZE. When the
	// tool definitions alone are past it, no summary gets the request under
	// it, and a pass that summarized anyway would be back one step later
	// paying for another. The refusal recovery still summarizes: it is asked
	// for a reclaim it can reach.
	if !policy.summarize || policy.summarizeTo <= 0 {
		return false
	}
	if policy.manual && pass.empty() {
		// A person asked for a shorter conversation and the free rungs found
		// nothing. The region's own minimum decides whether a summary is worth it.
		return true
	}
	return transcript > policy.summarizeAbove
}

// beltTokens is what the tool definitions add to every request, estimated from
// their encoding. The transcript estimate does not carry them, and on a small
// window they are most of what is sent. It takes the belt's own lock, so it is
// read before the session lock is.
func (a *Agent) beltTokens() int {
	definitions := a.beltDefinitions()
	if len(definitions) == 0 {
		return 0
	}
	encoded, err := json.Marshal(definitions)
	if err != nil {
		return 0
	}
	return EstimateTokens(len(encoded))
}

// planSummaryLocked chooses the region a summary replaces. It prefers keeping
// [summaryKeepPersonMessages] of the person's messages whole, and keeps fewer
// only when that cannot bring the conversation under target. It reports false
// when there is no region worth a call.
func (a *Agent) planSummaryLocked(target int) (summaryPlan, bool) {
	if !a.hasClientLocked() {
		return summaryPlan{}, false
	}
	window := a.trustedWindow()
	if window <= 0 {
		return summaryPlan{}, false
	}
	answer := summaryAnswerTokens(window)
	// A window too small to hold one useful chunk and its answer cannot be
	// summarized into; the provider's own guard reports that case.
	if summaryChunkTokens(window, answer, 0) < summaryMinRegionTokens {
		return summaryPlan{}, false
	}
	persons := a.personMessagesLocked()
	total := a.transcriptTokensLocked()
	var fallback, chosen []int
	for keep := min(summaryKeepPersonMessages, len(persons)); keep >= 1; keep-- {
		end := persons[len(persons)-keep]
		if !a.regionClosedLocked(end) {
			continue
		}
		bytes, fresh := 0, 0
		for index := 1; index < end; index++ {
			size := a.transcriptMessageBytesLocked(index)
			bytes += size
			if index > 1 || !strings.HasPrefix(messageContentText(a.messages[index]), summaryNotePrefix) {
				fresh += size
			}
		}
		tokens := EstimateTokens(bytes)
		// WHAT IS WORTH A CALL IS WHAT THE LAST SUMMARY HAS NOT ALREADY READ.
		// A region that is mostly an earlier note would be the model rewriting
		// its own summary on every step, shorter and vaguer each time.
		if EstimateTokens(fresh) < summaryMinRegionTokens {
			continue
		}
		candidate := []int{end, tokens}
		fallback = candidate
		if total-tokens+answer <= target {
			chosen = candidate
			break
		}
	}
	if chosen == nil {
		chosen = fallback
	}
	if chosen == nil {
		return summaryPlan{}, false
	}
	end := chosen[0]
	region := make([]ai.Message, end-1)
	copy(region, a.messages[1:end])
	plan := summaryPlan{end: end, region: region, model: a.model, window: window, answer: answer}
	if text := messageContentText(region[0]); region[0].Role == "user" && strings.HasPrefix(text, summaryNotePrefix) {
		plan.previous = summaryNoteBody(text)
	}
	journal, _, _ := a.file.messageLines(region[0], region[len(region)-1])
	switch {
	case journal != "":
		plan.pointer = "grep or read " + journal
	case a.chatlog.ref(region[0]) != "":
		plan.pointer = "the full record is in the store"
	default:
		plan.pointer = "the full record is in the session journal"
	}
	return plan, true
}

// personMessagesLocked lists the indices of the messages the person wrote —
// the user role, less the notes this package injects there.
func (a *Agent) personMessagesLocked() []int {
	var persons []int
	for index := 1; index < len(a.messages); index++ {
		if a.messages[index].Role != "user" {
			continue
		}
		text := messageContentText(a.messages[index])
		if isCompactionNote(text) || isVolatileNote(text) || a.file.isNote(a.messages[index]) {
			continue
		}
		persons = append(persons, index)
	}
	return persons
}

// regionClosedLocked says every tool call made in messages[1:end] is answered
// inside it. A call summarized away with its result left behind is an orphan
// every provider refuses, on this request and every one after it.
func (a *Agent) regionClosedLocked(end int) bool {
	answers := toolAnswerPositions(a.messages)
	for index := 1; index < end; index++ {
		for call := range a.messages[index].ToolCalls {
			if at, ok := answers[&a.messages[index].ToolCalls[call]]; ok && at >= end {
				return false
			}
		}
	}
	return true
}

// writeSummary asks the conversation's model for the summary, one chunk at a
// time when the region is larger than one request can carry. It is called
// with the session lock released.
func (a *Agent) writeSummary(ctx context.Context, plan summaryPlan) (string, error) {
	summary := plan.previous
	lines := summaryLines(plan.region)
	for len(lines) > 0 {
		budget := summaryChunkTokens(plan.window, plan.answer, EstimateTokens(len(summary))) * bytesPerToken
		if budget <= 0 {
			return "", errors.New("session: the summary no longer fits the window")
		}
		var chunk strings.Builder
		taken := 0
		for taken < len(lines) {
			line := lines[taken]
			if chunk.Len() > 0 && chunk.Len()+len(line) > budget {
				break
			}
			if len(line) > budget {
				line = clipMiddle(line, budget)
			}
			chunk.WriteString(line)
			taken++
		}
		lines = lines[taken:]
		next, err := a.askForSummary(ctx, plan, summary, chunk.String())
		if err != nil {
			return "", err
		}
		summary = next
	}
	return summary, nil
}

// summaryChunkTokens is how much conversation one summary request may carry
// once the answer, the safety allowance, the instruction and the summary so
// far are set aside.
func summaryChunkTokens(window, answer, previous int) int {
	return window - answer - provider.ContextSafetyTokens(window) - summaryPromptTokens - previous
}

// askForSummary is one request. It rides the conversation's own model with no
// tools and no stream, and it is billed as the summary it is.
func (a *Agent) askForSummary(ctx context.Context, plan summaryPlan, previous, chunk string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, summaryCallWindow)
	defer cancel()
	ctx = provider.WithRole(provider.WithoutStream(ctx), lane.RoleAuxiliary)
	ctx = provider.WithReasoningEffort(ctx, provider.EffortLow)
	ctx = provider.WithContextBudget(ctx, provider.ContextBudget{Window: plan.window, Reserve: plan.answer})
	var ask strings.Builder
	if previous != "" {
		ask.WriteString("Summary so far:\n\n")
		ask.WriteString(previous)
		ask.WriteString("\n\nMore of the conversation, which the updated summary must also cover:\n\n")
	} else {
		ask.WriteString("The conversation:\n\n")
	}
	ask.WriteString(chunk)
	ask.WriteString("\n\nWrite the summary now.")
	response, err := a.completeWithModel(ctx, purposeSummary, []ai.Message{
		textMessage("system", summaryInstruction(plan.answer)),
		textMessage("user", ask.String()),
	}, plan.model, ai.WithMaxTokens(plan.answer))
	if err != nil {
		return "", err
	}
	if response == nil {
		return "", errEmptyAnswer
	}
	a.mu.Lock()
	running := a.running
	a.mu.Unlock()
	if running {
		a.addAuxiliaryUsageAs(response, plan.model, 1, auxRoleSummary)
	} else {
		a.addDetachedUsageAs(response, plan.model, 1, auxRoleSummary)
	}
	text := strings.TrimSpace(response.Text())
	// An empty answer, machine markup and a model repeating itself are the
	// same failure here: nothing came back that could stand in for the region.
	if !briefIsProse(text) || briefRepeats(text) {
		return "", errEmptyAnswer
	}
	return clip(text, plan.answer*bytesPerToken*5/4), nil
}

// summaryInstruction is the summarizer's system message.
func summaryInstruction(answer int) string {
	return fmt.Sprintf(`You are compacting a conversation between a person and an AI assistant so that it fits in the assistant's context window. Your summary replaces the messages you are shown, and the assistant will continue from it as if it had read them.

Cover, in this order of importance:
- what the person asked for, and every constraint, preference or correction they gave (quote their exact words where the wording matters);
- decisions made and the reasons for them;
- what was done: files created or changed, commands run and their outcomes, errors hit and how they were resolved;
- facts learned about the project or the world that the work depends on;
- what is unfinished or still open, and what was about to happen next.

Keep file paths, names, identifiers, numbers and error messages exact. Drop pleasantries, false starts and anything later superseded. Copy any line that begins with "[folded" verbatim: it points at the full record. Write in the third person ("the person asked", "the assistant changed"), as plain prose and short lists, with no preamble. Use at most about %d words.`, answer*3/4)
}

// summaryLines renders the region as the text the summarizer reads, one entry
// per message. An earlier summary note is skipped here: its body travels as
// the summary so far ([summaryPlan.previous]).
func summaryLines(region []ai.Message) []string {
	names := make(map[string]string, 8)
	lines := make([]string, 0, len(region))
	for index, message := range region {
		text := strings.TrimSpace(messageContentText(message))
		switch message.Role {
		case "user":
			if index == 0 && strings.HasPrefix(text, summaryNotePrefix) {
				continue
			}
			if isVolatileNote(text) {
				continue
			}
			if isCompactionNote(text) {
				lines = append(lines, text+"\n\n")
				continue
			}
			lines = append(lines, "PERSON: "+text+summaryImages(message)+"\n\n")
		case "assistant":
			var line strings.Builder
			if text != "" {
				line.WriteString("ASSISTANT: " + text + "\n")
			}
			for _, call := range message.ToolCalls {
				names[call.ID] = call.Function.Name
				line.WriteString("ASSISTANT CALLED " + call.Function.Name + " " +
					clipMiddle(call.Function.Arguments, summaryToolArgBytes) + "\n")
			}
			if line.Len() > 0 {
				lines = append(lines, line.String()+"\n")
			}
		case "tool":
			name := names[message.ToolCallID]
			if name == "" {
				name = "tool"
			}
			lines = append(lines, "RESULT OF "+name+": "+clipMiddle(text, summaryToolResultBytes)+"\n\n")
		}
	}
	return lines
}

// summaryImages says a message carried pictures, which the summarizer is not
// shown.
func summaryImages(message ai.Message) string {
	images := 0
	for _, part := range message.Content {
		if part.Type != "text" {
			images++
		}
	}
	if images == 0 {
		return ""
	}
	return fmt.Sprintf(" [%d attachment%s not shown]", images, plural(images))
}

// clipMiddle keeps the start and the end of a long text, which is where a
// command's purpose and its outcome usually are.
func clipMiddle(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	const gap = "\n… [middle omitted] …\n"
	if limit <= len(gap)+2 {
		return clip(text, limit)
	}
	head := (limit - len(gap)) / 2
	tail := limit - len(gap) - head
	for head > 0 && !utf8RuneStart(text[head]) {
		head--
	}
	start := len(text) - tail
	for start < len(text) && !utf8RuneStart(text[start]) {
		start++
	}
	return text[:head] + gap + text[start:]
}

// summaryNote is the user-role message a summary stands in the conversation
// as. User role because it is context handed TO the model, and marked in plain
// words because a model that mistakes a summary for a transcript will answer
// things inside it again.
func summaryNote(summary, pointer string) string {
	return summaryNotePrefix + " The earlier part of this conversation was summarized to fit the " +
		"context window. This note is that summary — a record, not something either of us just said. " +
		"The original messages are not lost: " + pointer + ".\n\n" + summary
}

// summaryNoteBody is the summary a note carries, without its framing.
func summaryNoteBody(note string) string {
	if _, body, found := strings.Cut(note, "\n\n"); found {
		return strings.TrimSpace(body)
	}
	return ""
}

// spliceSummaryLocked replaces the planned region with the summary note, if
// the region is still exactly what was summarized. It reports how many
// messages the note replaced, and zero when nothing changed.
func (a *Agent) spliceSummaryLocked(plan summaryPlan, summary string) int {
	if len(a.messages) < plan.end || !reflect.DeepEqual(a.messages[1:plan.end], plan.region) {
		return 0
	}
	note := textMessage("user", summaryNote(summary, plan.pointer))
	replaced := 0
	for index := 1; index < plan.end; index++ {
		replaced += a.transcriptMessageBytesLocked(index)
	}
	// A summary that is not smaller than what it replaces is refused, for the
	// fold's reason: a pass that makes the conversation heavier is not one.
	if messageBytes(note) >= replaced {
		return 0
	}
	a.alignReasoningLocked()
	rebuilt := make([]ai.Message, 0, len(a.messages)-plan.end+2)
	rebuiltReasoning := make([]provider.MessageReasoning, 0, cap(rebuilt))
	rebuilt = append(rebuilt, a.messages[0], note)
	rebuiltReasoning = append(rebuiltReasoning, a.messageReasoning[0], provider.MessageReasoning{})
	rebuilt = append(rebuilt, a.messages[plan.end:]...)
	rebuiltReasoning = append(rebuiltReasoning, a.messageReasoning[plan.end:]...)
	// THE RUNNING TURN'S FLOOR MOVES WITH THE REBUILD, as it does for a fold:
	// the region ends before the turn's opening message, so everything from
	// the floor on shifts down by the messages the note replaced.
	if a.turnFloor >= plan.end {
		a.turnFloor -= plan.end - 2
	} else if a.turnFloor > 1 {
		a.turnFloor = 2
	}
	a.messages = rebuilt
	a.messageReasoning = rebuiltReasoning
	return plan.end - 1
}
