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

	// summaryMinRecoveryTokens is the smallest region worth a call when a
	// refused request is being recovered ([summaryWorthIt]).
	summaryMinRecoveryTokens = 128

	// summaryMinAnswerTokens is the shortest summary asked for.
	summaryMinAnswerTokens = 128

	// summaryNoteTokens is the note's own framing around a summary: the opening
	// sentence and the journal pointer.
	summaryNoteTokens = 96

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
	// answer is how long the summary is asked to be, and ceiling the most the
	// request allows it to write: room for a model that overshoots the words
	// it was asked for, which [Agent.spliceSummaryLocked] still refuses if the
	// note ends up no smaller than what it replaces.
	answer  int
	ceiling int
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

// planSummaryLocked chooses the region a summary replaces, and reports false
// when there is no region worth a call.
//
// The cuts are tried from the most kept to the least ([Agent.summaryEndsLocked])
// and the first one that brings the conversation under the policy's line wins;
// when none does, the most aggressive worthwhile cut is taken, because a pass
// that reaches for the line and misses still buys more room than one that
// stops short of it.
func (a *Agent) planSummaryLocked(policy compactPolicy) (summaryPlan, bool) {
	if !a.hasClientLocked() {
		return summaryPlan{}, false
	}
	window := a.trustedWindow()
	if window <= 0 {
		return summaryPlan{}, false
	}
	most := summaryAnswerTokens(window)
	// A window too small to hold one useful chunk and its answer cannot be
	// summarized into; the provider's own guard reports that case.
	if summaryChunkTokens(window, most, 0) < summaryMinRegionTokens {
		return summaryPlan{}, false
	}
	persons := a.personMessagesLocked()
	if len(persons) == 0 {
		return summaryPlan{}, false
	}
	total := a.transcriptTokensLocked()
	type cut struct{ end, tokens, answer int }
	var fallback, chosen *cut
	for _, end := range a.summaryEndsLocked(persons) {
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
		if !summaryWorthIt(policy, tokens, EstimateTokens(fresh)) {
			continue
		}
		candidate := &cut{end: end, tokens: tokens, answer: summaryTargetTokens(most, tokens)}
		fallback = candidate
		if total-tokens+candidate.answer+summaryNoteTokens <= policy.summarizeTo {
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
	end := chosen.end
	region := make([]ai.Message, end-1)
	copy(region, a.messages[1:end])
	plan := summaryPlan{
		end: end, region: region, model: a.model, window: window,
		answer: chosen.answer, ceiling: min(most, 2*chosen.answer),
	}
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

// summaryWorthIt says whether a region is worth a model call.
//
// A REFUSED REQUEST NEEDS WHAT IT NEEDS. Recovery is bounded by its own
// allowance and is asked for a specific reclaim, so any region the model can
// say more briefly is worth it there — an earlier note included, rewritten
// shorter — however little that frees: on 2026-09-28 a turn ended refused 78
// tokens over the window while a 740-token region sat unsummarized under a
// 1,024-token floor.
//
// EVERY OTHER PASS WANTS NEW MATERIAL. What is worth a call there is what the
// last summary has not already read ([summaryMinRegionTokens] of it): a region
// that is mostly an earlier note would be the model rewriting its own summary
// on every step, shorter and vaguer each time.
func summaryWorthIt(policy compactPolicy, tokens, fresh int) bool {
	if policy.recovering {
		return tokens >= summaryMinRecoveryTokens
	}
	return fresh >= summaryMinRegionTokens
}

// summaryTargetTokens is how long a summary of a region is asked to be: never
// more than half the region it replaces, never more than the window's own cap,
// and never so short that it cannot say anything.
func summaryTargetTokens(most, region int) int {
	return min(most, max(summaryMinAnswerTokens, region/2))
}

// summaryEndsLocked lists where a summary's region may end, from the cut that
// keeps the most to the one that keeps the least:
//
//  1. the person's three most recent messages and everything after them;
//  2. their two most recent;
//  3. their most recent, AND THE REPLY THAT CAME BEFORE IT — the answer a
//     person's "translate it" or "keep going" is about, which a summary cannot
//     stand in for (on 2026-09-28 a model asked to translate a story that had
//     been summarized away translated the summary instead);
//  4. their most recent alone, which is never summarized.
func (a *Agent) summaryEndsLocked(persons []int) []int {
	var ends []int
	for keep := min(summaryKeepPersonMessages, len(persons)); keep >= 2; keep-- {
		ends = append(ends, persons[len(persons)-keep])
	}
	last := persons[len(persons)-1]
	floor := 0
	if len(persons) > 1 {
		floor = persons[len(persons)-2]
	}
	for index := last - 1; index > floor; index-- {
		message := a.messages[index]
		if message.Role == "assistant" && len(message.ToolCalls) == 0 && strings.TrimSpace(messageContentText(message)) != "" {
			ends = append(ends, index)
			break
		}
	}
	return append(ends, last)
}

// personMessagesLocked lists the indices of the messages the person wrote —
// the user role, less every message this package writes there.
func (a *Agent) personMessagesLocked() []int {
	var persons []int
	for index := 1; index < len(a.messages); index++ {
		if a.messages[index].Role != "user" {
			continue
		}
		if isCodeafNote(messageContentText(a.messages[index])) || a.file.isNote(a.messages[index]) {
			continue
		}
		persons = append(persons, index)
	}
	return persons
}

// isCodeafNote reports whether a user-role message is one this package wrote
// into the conversation rather than something the person typed, by the
// opening each is built with.
//
// IT IS WIDER THAN [isCompactionNote] AND [isVolatileNote] because a summary
// has to know which messages are the person's to keep: on 2026-09-28 the
// truncation continuation — which has no tag and reads as plain words — was
// kept as though it were the person's latest message, and the request it
// continued was summarized away in its place.
func isCodeafNote(text string) bool {
	if isCompactionNote(text) || isVolatileNote(text) {
		return true
	}
	for _, opening := range codeafNoteOpenings {
		if strings.HasPrefix(text, opening) {
			return true
		}
	}
	return false
}

// codeafNoteOpenings are the openings of the notes a turn writes into the
// conversation as the user role (checkpoint.go, inherit.go, looped.go and the
// truncation continuation in loop.go).
var codeafNoteOpenings = []string{
	truncationContinuationNote,
	"[carry on] ",
	checkpointChoiceLead,
	"[silent] You have made ",
	"[stuck] You have sent the same ",
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
		budget := summaryChunkTokens(plan.window, plan.ceiling, EstimateTokens(len(summary))) * bytesPerToken
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
	ctx = provider.WithContextBudget(ctx, provider.ContextBudget{Window: plan.window, Reserve: plan.ceiling})
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
	}, plan.model, ai.WithMaxTokens(plan.ceiling))
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
	return clip(text, plan.ceiling*bytesPerToken), nil
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
