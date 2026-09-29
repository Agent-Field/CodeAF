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
	"unicode"

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

// CompactPatience is how long a surface on the far side of a connection waits
// for a /compact it asked for. A pass may now ask the model for a summary, and
// the ten seconds every other call gets is shorter than one summary request on
// a slow model (twenty seconds each on deepseek-v3.2, 2026-09-28), so the
// surface said "did not answer in time" about a pass that then landed. Two
// summary requests is the most a manual pass over one window's worth of
// conversation makes, and the minute is for the fold and the journal around
// them.
const CompactPatience = 2*summaryCallWindow + time.Minute

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
	if policy.manual {
		// A PERSON'S /compact GOES ALL THE WAY IN ONE PASS. It asked for the
		// conversation as short as it may be made ([Agent.requestedCompactPolicy]),
		// so after the free rungs it summarizes whatever older conversation is
		// left, however far under the automatic lines that already is; the
		// region's own minimum ([summaryWorthIt]) decides whether it is worth a
		// request. It used to stop once the fold got under the automatic
		// target, and a person had to type /compact a second time — with
		// nothing on screen saying so — to get the summary (2026-09-28).
		return true
	}
	return transcript > policy.summarizeAbove
}

// beltTokens is what the tool definitions add to every request, estimated from
// their encoding. The transcript estimate does not carry them, and on a small
// window they are most of what is sent. It takes the belt's own lock, so it is
// read before the session lock is.
//
// A model the catalog says takes no tools is sent none (internal/provider's
// toolless.go), so for it the belt weighs nothing.
func (a *Agent) beltTokens() int {
	if a.config.SupportsParameter != nil {
		a.mu.Lock()
		model := a.model
		a.mu.Unlock()
		if supported, known := a.config.SupportsParameter(model, "tools"); known && !supported {
			return 0
		}
	}
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
// when there is no region worth a call — with why, in the words a person's
// /compact prints (empty where there is nothing useful to say).
//
// The cuts are tried from the most kept to the least ([Agent.summaryEndsLocked])
// and the first one that brings the conversation under the policy's line wins;
// when none does, an ordinary pass keeps the most recent three messages.
// Recovery still takes the largest worthwhile cut because a refused request
// needs every bit of room it can reclaim.
func (a *Agent) planSummaryLocked(policy compactPolicy) (summaryPlan, string, bool) {
	if !a.hasClientLocked() {
		return summaryPlan{}, "", false
	}
	window := a.trustedWindow()
	if window <= 0 {
		return summaryPlan{}, "", false
	}
	most := summaryAnswerTokens(window)
	// A window too small to hold one useful chunk and its answer cannot be
	// summarized into; the provider's own guard reports that case.
	if summaryChunkTokens(window, most, 0) < summaryMinRegionTokens {
		return summaryPlan{}, "the model's window is too small to write a summary into", false
	}
	persons := a.personMessagesLocked()
	if len(persons) == 0 {
		return summaryPlan{}, "there is no conversation to summarize yet", false
	}
	total := a.transcriptTokensLocked()
	type cut struct{ end, tokens, answer int }
	var fallback, chosen *cut
	protected := min(summaryKeepPersonMessages, len(persons))
	protectedWhy := ""
	// What the most a summary could have taken came to, for the sentence a
	// pass that takes nothing says about itself.
	freshest, noted, closed, kept := 0, false, false, 1
	for _, end := range a.summaryEndsLocked(persons) {
		if !a.regionClosedLocked(end) {
			continue
		}
		closed = true
		kept = 0
		for _, person := range persons {
			if person >= end {
				kept++
			}
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
		freshest = EstimateTokens(fresh)
		noted = end > 1 && strings.HasPrefix(messageContentText(a.messages[1]), summaryNotePrefix)
		if !summaryWorthIt(policy, tokens, EstimateTokens(fresh)) {
			if kept == protected {
				protectedWhy = summaryTooLittle(true, noted, freshest, kept)
			}
			// A CUT WITH NOTHING WORTH A REQUEST IS NOT A REASON TO KEEP LESS.
			// Fewer of the person's messages are kept only when keeping more
			// cannot get under the line; a conversation already under it has
			// nothing to gain from summarizing their third-newest message, and
			// a second /compact did exactly that until 2026-09-28.
			if total <= policy.summarizeTo {
				break
			}
			continue
		}
		candidate := &cut{end: end, tokens: tokens, answer: summaryTargetTokens(most, tokens)}
		if policy.recovering || (fallback == nil && kept == protected) {
			fallback = candidate
		}
		if total-tokens+candidate.answer+summaryNoteTokens <= policy.summarizeTo {
			chosen = candidate
			break
		}
	}
	if chosen == nil {
		chosen = fallback
	}
	if chosen == nil {
		if protectedWhy != "" && !policy.recovering {
			return summaryPlan{}, protectedWhy, false
		}
		return summaryPlan{}, summaryTooLittle(closed, noted, freshest, kept), false
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
	return plan, "", true
}

// summaryTooLittle is what a pass says when no region was worth a summary.
func summaryTooLittle(closed, noted bool, fresh, kept int) string {
	before := "your latest message"
	if kept > 1 {
		before = fmt.Sprintf("your last %d messages", kept)
	}
	switch {
	case !closed:
		return "the newest work is still in progress"
	case fresh == 0 && noted:
		return "nothing new since the last summary"
	case fresh == 0:
		return "there is nothing before " + before + " to summarize"
	case noted:
		return "only " + approxTokens(fresh) + " tokens since the last summary — too little to summarize"
	default:
		return "only " + approxTokens(fresh) + " tokens before " + before + " — too little to summarize"
	}
}

// summaryFailedWhy is what a pass says when the summary it asked for did not
// come back usable.
func summaryFailedWhy(ctx context.Context, err error) string {
	switch {
	case ctx.Err() != nil:
		return "the summary was interrupted"
	case errors.Is(err, errSummaryDeclined):
		return "the model declined to write a summary"
	case errors.Is(err, errEmptyAnswer):
		return "the model's summary came back empty or unreadable"
	default:
		return "the model could not write a summary: " + clip(strings.TrimSpace(err.Error()), 160)
	}
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
		if a.sessionNoteLocked(a.messages[index]) {
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
	reserve := plan.ceiling
	if a.config.ReasoningProfile != nil {
		if profile, known := a.config.ReasoningProfile(plan.model); known {
			reserve = max(reserve, provider.SummaryOutputReserve(profile, plan.ceiling))
		}
	}
	reserve = min(reserve, plan.window-provider.ContextSafetyTokens(plan.window)-summaryPromptTokens-summaryMinRegionTokens)
	for len(lines) > 0 {
		budget := summaryChunkTokens(plan.window, reserve, EstimateTokens(len(summary))) * bytesPerToken
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

// askForSummary asks the conversation's own model with no tools or stream. An
// empty length finish gets one larger request; both calls are billed.
func (a *Agent) askForSummary(ctx context.Context, plan summaryPlan, previous, chunk string) (string, error) {
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
	messages := []ai.Message{
		textMessage("system", summaryInstruction(plan.answer)),
		textMessage("user", ask.String()),
	}
	call := func(ceiling int) (*ai.Response, error) {
		callCtx, cancel := context.WithTimeout(ctx, summaryCallWindow)
		response, answered, err := a.completeWithNamedModel(callCtx, purposeSummary, messages, plan.model, ai.WithMaxTokens(ceiling))
		cancel()
		if err != nil {
			return nil, err
		}
		if response == nil {
			return nil, errEmptyAnswer
		}
		a.mu.Lock()
		running := a.running
		a.mu.Unlock()
		if running {
			a.addAuxiliaryUsageAs(response, answered, 1, auxRoleSummary)
		} else {
			a.addDetachedUsageAs(response, answered, 1, auxRoleSummary)
		}
		return response, nil
	}
	response, err := call(plan.ceiling)
	if err != nil {
		return "", err
	}
	if provider.EmptyAtCeiling(response, plan.ceiling) {
		// TWICE THE ALLOWANCE, NOT A SHARE OF THE WINDOW. The provider already
		// sizes the thinking room above the answer for the level the model runs
		// at, so what came back empty is the rare pass that thought past it; a
		// window-sized ceiling on a million-token model would ask endpoints for
		// more completion than any of them serves and be refused instead.
		retryCeiling := min(plan.window-provider.ContextSafetyTokens(plan.window)-summaryPromptTokens, 2*plan.ceiling)
		if retryCeiling > plan.ceiling {
			response, err = call(retryCeiling)
			if err != nil {
				return "", err
			}
		}
	}
	return summaryAnswer(response, chunk, plan.ceiling)
}

func summaryAnswer(response *ai.Response, chunk string, ceiling int) (string, error) {
	text := strings.TrimSpace(response.Text())
	if strings.EqualFold(strings.TrimSpace(provider.FinishReason(response)), "content_filter") || summaryRefusalWithoutSubstance(text, chunk) {
		return "", errSummaryDeclined
	}
	// An empty answer, machine markup and a model repeating itself are the
	// same failure here: nothing came back that could stand in for the region.
	if !briefIsProse(text) || briefRepeats(text) {
		return "", errEmptyAnswer
	}
	return clip(text, ceiling*bytesPerToken), nil
}

var errSummaryDeclined = errors.New("session: the model declined to write a summary")

// A short refusal opening is not a summary when it names nothing from the
// region. Generic refusal words are ignored so a shared "request" or "help"
// cannot make an otherwise empty refusal look like conversation substance.
func summaryRefusalWithoutSubstance(answer, region string) bool {
	if len(answer) > 512 {
		return false
	}
	lower := strings.ToLower(strings.TrimSpace(answer))
	opening := false
	for _, prefix := range []string{"i'm sorry", "i’m sorry", "i am sorry", "sorry,", "i can't", "i can’t", "i cannot", "i'm unable", "i am unable"} {
		if strings.HasPrefix(lower, prefix) {
			opening = true
			break
		}
	}
	if !opening {
		return false
	}
	words := func(text string) []string {
		return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) && r != '_' })
	}
	generic := map[string]bool{"sorry": true, "cannot": true, "could": true, "would": true, "help": true, "request": true, "content": true, "policy": true, "provide": true, "assist": true, "information": true, "about": true, "that": true, "this": true, "with": true, "your": true, "their": true, "there": true, "because": true, "unable": true, "person": true, "assistant": true, "called": true, "result": true, "tool": true, "attachment": true, "shown": true, "middle": true, "omitted": true, "context": true, "compacted": true, "folded": true}
	regionWords := make(map[string]bool)
	for _, word := range words(region) {
		if len(word) >= 5 && !generic[word] {
			regionWords[word] = true
		}
	}
	for _, word := range words(lower) {
		if len(word) >= 5 && !generic[word] && regionWords[word] {
			return false
		}
	}
	return true
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

Carry every specific fact, name, number, code, path and decision from the previous summary forward word for word unless the newer conversation explicitly supersedes it. Keep file paths, names, identifiers, numbers and error messages exact. Drop pleasantries, false starts and anything later superseded. Copy any line that begins with "[folded" verbatim: it points at the full record. Write in the third person ("the person asked", "the assistant changed"), as plain prose and short lists, with no preamble. Use at most about %d words.`, answer*3/4)
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
// messages the note replaced, and zero with the reason when nothing changed.
func (a *Agent) spliceSummaryLocked(plan summaryPlan, summary string) (int, string) {
	if len(a.messages) < plan.end || !reflect.DeepEqual(a.messages[1:plan.end], plan.region) {
		return 0, "the conversation changed while the summary was being written"
	}
	note := textMessage("user", summaryNote(summary, plan.pointer))
	replaced := 0
	for index := 1; index < plan.end; index++ {
		replaced += a.transcriptMessageBytesLocked(index)
	}
	// A summary that is not smaller than what it replaces is refused, for the
	// fold's reason: a pass that makes the conversation heavier is not one.
	if messageBytes(note) >= replaced {
		return 0, "the summary came out no shorter than what it would replace"
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
	return plan.end - 1, ""
}
