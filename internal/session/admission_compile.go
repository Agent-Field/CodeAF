package session

// One compiler, at every door work enters the graph through.
//
// The doors are a person's typed task (task_person.go), a model's proposal
// (task.go), the parts of a division (task_divide_wip.go) and the automatic
// handover that starts work nobody asked for (route_judge.go). A continuation
// re-runs a node that already holds its context (task_continue.go) and a
// checkpoint restores it (task_store.go), so neither compiles a second one.
// admission_law_test.go is what keeps a new door from quietly composing its own.
//
// WHAT IT IS NOT. This is supporting context, not a constraint record: the
// selection is bounded in count, in bytes and in generations, so a constraint
// older than the window, deeper than the inheritance limit or further into a
// message than the clip is NOT here. That is why every entry carries a source
// and why the section rule says the record is partial. The authoritative copies
// live where they always did — the person's own words on the spec, the journal
// behind the origin pointer, and the full tool results in the chat log.
//
// HOW IT CHOOSES. Newest first to a byte budget, then printed oldest first.
// Newest-first is the whole policy and it is aimed at one failure: a person's
// twenty-first correction has to survive a budget the first twenty exchanges
// could fill. Printing oldest-first afterwards keeps a correction below the
// thing it corrects.

import (
	"bytes"
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// The bounds, in bytes and counts because both are exactly measurable here.
const (
	// admissionQuoteLimit bounds one quoted exchange. A quote longer than this is
	// ELIDED IN THE MIDDLE rather than cut off ([elide]): a person's last
	// sentence is where a constraint most often is, and a head-only clip drops
	// precisely that.
	admissionQuoteLimit = 600
	// admissionQuotesKept and admissionHandlesKept bound how many of each one
	// admission carries.
	admissionQuotesKept  = 8
	admissionHandlesKept = 6
	// admissionInputLimit keeps a handle an exact input reference — the path that
	// was read, the pattern that was searched — rather than a tool name.
	admissionInputLimit  = 240
	admissionDetailLimit = 240
	// admissionInherited bounds what comes down from the parent's own admission,
	// below the local cap: what is local is what the work is about.
	admissionInherited = 4
	// admissionDepthLimit is how many generations one entry may travel.
	admissionDepthLimit = 3
	// admissionBudget is the rendered size of the whole context — one overall
	// bound rather than one per section.
	admissionBudget = 5000
	// admissionTurnsRemembered is how many of the person's turns a session keeps
	// to select from. Larger than admissionQuotesKept on purpose: a session that
	// remembered only as many as it carries could not prefer anything.
	admissionTurnsRemembered = 24
	// admissionWindow bounds the WALK, not the selection: how far back down the
	// transcript the compiler looks. Deep enough for a long working turn and its
	// batches, which is what a proposal made mid-work quotes from.
	admissionWindow = 200
	// admissionOutcomesKept bounds the per-call outcome map. It is generous next
	// to admissionHandlesKept because the compiler selects the newest calls out
	// of a long turn.
	admissionOutcomesKept = 512
)

// ── what the session keeps so this can be compiled ──────────────────────────

// personTurn is one thing THE PERSON typed. It is kept because the transcript
// cannot answer the question afterwards: every user-role message is user-role,
// including the ones the session wrote itself, and the bit that says who spoke
// does not survive the append.
//
// A line steered into a running node is the person typing, which is why this is
// recorded where the person's words are recorded rather than at a conversation's
// turn start.
type personTurn struct {
	// seq makes the EVENT the identity. The same sentence typed twice, before
	// and after a correction, is two instructions, so retyping never collapses
	// onto the earlier turn.
	seq  uint64
	key  string
	text string
}

// rememberPersonTurnLocked appends one of the person's turns to the bounded list
// the compiler selects from. The caller holds a.mu.
func (a *Agent) rememberPersonTurnLocked(message ai.Message, text string) {
	a.personSeq++
	a.personTurns = append(a.personTurns, personTurn{
		seq: a.personSeq, key: chatRefKey(message), text: elide(text, admissionQuoteLimit),
	})
	if len(a.personTurns) > admissionTurnsRemembered {
		a.personTurns = a.personTurns[len(a.personTurns)-admissionTurnsRemembered:]
	}
}

// restorePersonTurnsLocked rebuilds the remembered turns from a replayed
// transcript, so a conversation reopened tomorrow hands out work with the
// context it had today.
//
// The journal marks which user-role lines the session wrote itself
// ([sessionEntry.Note]), and that mark is the only thing that can tell the
// person's words from a task's landing note after a restart. Without this the
// records would silently be empty on every resumed session — which is a worse
// answer than an older one, because nothing would say so.
func (a *Agent) restorePersonTurnsLocked(messages []ai.Message) {
	for _, message := range messages {
		if message.Role != "user" || a.file.isNote(message) {
			continue
		}
		if text := strings.TrimSpace(messageContentText(message)); text != "" {
			a.rememberPersonTurnLocked(message, text)
		}
	}
}

// callOutcome is whether one finished call came back a failure, and its own
// first line when it did.
//
// It is recorded at the batch's fan-out because that is where the answer exists:
// a tool result in the transcript is a string, and the flag the tool returned is
// gone by then. Guessing at it from the words is the invention this package
// refuses to make, so an unrecorded call stays [AdmissionUnknown].
//
// IT IS PER-PROCESS. Nothing persists it, so work handed out after a restart
// gets handles whose outcome is unknown and says so.
type callOutcome struct {
	tool   string
	failed bool
	detail string
}

// noteCallOutcomes records how one batch of calls came back, taking the lock
// once for the batch.
func (a *Agent) noteCallOutcomes(calls []ai.ToolCall, results []toolResult) {
	if len(calls) == 0 {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.callOutcomes == nil {
		a.callOutcomes = make(map[string]callOutcome, len(calls))
	}
	// Emptied rather than trimmed: trimming needs an order this map does not
	// keep, and the compiler already answers "unknown" for an id it cannot find,
	// which is the honest answer for an outcome nobody kept.
	if len(a.callOutcomes) > admissionOutcomesKept {
		a.callOutcomes = make(map[string]callOutcome, len(calls))
	}
	for index, call := range calls {
		if index >= len(results) {
			break
		}
		outcome := callOutcome{tool: call.Function.Name, failed: results[index].isError}
		if outcome.failed {
			outcome.detail = clip(firstLine(results[index].text), admissionDetailLimit)
		}
		a.callOutcomes[call.ID] = outcome
	}
}

// ── the source ──────────────────────────────────────────────────────────────

// admissionSource is everything the compiler reads. It is a value rather than an
// agent so the selection can be tested for what it selects.
type admissionSource struct {
	// said is the person's own turns, oldest first.
	said []personTurn
	// asked is the request the brief already prints verbatim (task_brief.go's
	// [briefAskHeading]) and is excluded from the quotes: printing it twice under
	// two headings reads as two instructions that happen to agree.
	asked string
	// messages is the working transcript, oldest first.
	messages []ai.Message
	// outcomes says how a finished call came back, by call id.
	outcomes map[string]callOutcome
	// record is the journal these words and results can be read out of — a path
	// `read` and `grep` open, which a conversation-store id is not (loop.go says
	// the same of a fold marker). Empty for a session with no file, and then no
	// entry draws a pointer.
	//
	// IT IS A PATH KNOWN WITHOUT READING ANYTHING. Placing each quote at its own
	// LINE would mean parsing the whole journal at every admission — a cost on
	// the hot path of spawning work, taken to produce a number that is ambiguous
	// anyway for a sentence somebody typed twice. The words are the grep token
	// for a quote and the call id is the grep token for a result.
	record string
	// scope names where these words were said, and is part of every id: a hash of
	// the same sentence said by two people in two sessions is not one event.
	scope string
	// from is whose transcript this is: empty for the conversation, "task 7" for
	// a node.
	from string
	// inherited is the context of the work this admission happens inside.
	inherited AdmissionContext
}

// compileAdmission is the one selection every door reaches.
func compileAdmission(source admissionSource) AdmissionContext {
	context := AdmissionContext{Version: AdmissionContextVersion}
	candidates := admissionCandidates(source)
	sort.SliceStable(candidates, func(one, two int) bool {
		return candidates[one].order > candidates[two].order
	})
	spent := 0
	seen := make(map[string]bool, len(candidates))
	kept := make([]admissionCandidate, 0, admissionQuotesKept)
	for _, candidate := range candidates {
		if len(kept) >= admissionQuotesKept {
			break
		}
		if candidate.quote.ID == "" || seen[candidate.quote.ID] {
			continue
		}
		cost := quoteCost(candidate.quote)
		if spent+cost > admissionRoom()-admissionEvidenceFloor() {
			continue
		}
		seen[candidate.quote.ID] = true
		spent += cost
		kept = append(kept, candidate)
	}
	sort.SliceStable(kept, func(one, two int) bool { return kept[one].order < kept[two].order })
	for _, candidate := range kept {
		context.Quotes = append(context.Quotes, candidate.quote)
	}
	context.Evidence = admissionEvidence(source, &spent)
	context.inherit(source.inherited, seen, spent)
	return context
}

// quoteCost and handleCost are THE BYTES THE ENTRY ACTUALLY COSTS THE WORKER'S
// PROMPT: the rendered line, its bullet and its newline, measured through the
// same [AdmissionQuote.line] the document is drawn with. Costing the fields
// instead would under-count everything the rendering adds — the speaker, the
// path, the tool names a line was said alongside, the separators — and the
// budget would then be a bound on a number nobody reads.
func quoteCost(quote AdmissionQuote) int {
	return len("· "+quote.line()) + 1
}

func handleCost(handle AdmissionHandle) int {
	return len("· "+handle.line()) + 1
}

// admissionOverhead is what the two sections cost before a single entry: their
// headings, their rules, and the five separator bytes [composeBrief] writes
// around each one. It comes off the budget up front, so the bound holds for the
// whole rendered context rather than for the entries alone.
var admissionOverhead = len(admissionQuotesHeading) + len(admissionQuotesRule) +
	len(admissionEvidenceHeading) + len(admissionEvidenceRule) + 2*len("\n\n"+"\n"+"\n\n")

// admissionRoom is what the entries themselves may spend.
func admissionRoom() int { return admissionBudget - admissionOverhead }

// admissionEvidenceFloor is the room the quotes may NOT take. Quotes are
// selected first, and a talkative conversation would otherwise spend the whole
// budget on eight long paragraphs and leave a worker with no idea which calls
// had already run — including the failed ones, which are the entries that save
// it money. A third is enough for two or three handles at their own bounds.
func admissionEvidenceFloor() int { return admissionRoom() / 3 }

// admissionCandidate is one quotable message with the position it was found at.
type admissionCandidate struct {
	order int
	quote AdmissionQuote
}

// admissionCandidates reads the transcript once and returns everything worth
// quoting, in transcript order.
func admissionCandidates(source admissionSource) []admissionCandidate {
	// The person's turns are matched into the transcript by content key so they
	// land in the order they were said. Two identical messages share a key and
	// are separate turns, so each match is consumed once, oldest first.
	pending := make(map[string][]personTurn, len(source.said))
	asked := strings.TrimSpace(source.asked)
	for _, turn := range source.said {
		if strings.TrimSpace(turn.text) == "" || sameAsk(turn.text, asked) {
			continue
		}
		pending[turn.key] = append(pending[turn.key], turn)
	}
	candidates := make([]admissionCandidate, 0, len(source.messages))
	for index, message := range source.messages {
		switch {
		case message.Role == "user":
			key := chatRefKey(message)
			waiting := pending[key]
			if len(waiting) == 0 {
				continue
			}
			turn := waiting[0]
			pending[key] = waiting[1:]
			candidates = append(candidates, admissionCandidate{order: index,
				quote: source.personQuote(turn)})
		case message.Role == "assistant":
			text := strings.TrimSpace(messageContentText(message))
			if text == "" || strings.HasPrefix(text, stubMarker) {
				continue
			}
			// KEYED BY CONTENT, NOT BY POSITION. A fold rewrites the transcript
			// and every index after it moves, so an id made of the position would
			// name a different line after every compaction — and could collide
			// with an older quote inherited from a parent.
			quote := AdmissionQuote{
				ID: source.scope + "/a" + chatRefKey(message), Speaker: admissionAssistant,
				Text: elide(text, admissionQuoteLimit), From: source.from, Source: source.record,
			}
			for _, call := range message.ToolCalls {
				quote.Calls = append(quote.Calls, call.Function.Name)
			}
			candidates = append(candidates, admissionCandidate{order: index, quote: quote})
		}
	}
	// A TURN THE WORKING WINDOW NO LONGER HOLDS IS STILL SOMETHING THE PERSON
	// SAID: compaction rewrites the transcript and a session outlives many of
	// them. Those turns sit before everything in the window and IN THE ORDER THEY
	// WERE HEARD — the newest of them is the newest thing the person said that
	// this window cannot show, so it must be selected first and read last, like
	// every other quote here.
	unmatched := make([]personTurn, 0, len(source.said))
	for _, turn := range source.said {
		waiting := pending[turn.key]
		if len(waiting) == 0 || waiting[0].seq != turn.seq {
			continue
		}
		pending[turn.key] = waiting[1:]
		unmatched = append(unmatched, turn)
	}
	for index, turn := range unmatched {
		candidates = append(candidates, admissionCandidate{
			order: index - len(unmatched), quote: source.personQuote(turn),
		})
	}
	return candidates
}

// personQuote is one of the person's turns as a quote: an id that numbers the
// EVENT (see [personTurn.seq]) and the record it can be read out of.
func (s admissionSource) personQuote(turn personTurn) AdmissionQuote {
	return AdmissionQuote{
		ID:      s.scope + "/p" + strconv.FormatUint(turn.seq, 10),
		Speaker: admissionPerson, Text: turn.text, From: s.from, Source: s.record,
	}
}

// sameAsk says whether a remembered turn is the request the brief already prints
// in full. Prefix rather than equality because the remembered copy is bounded
// and the printed one is not.
func sameAsk(turn, asked string) bool {
	turn, asked = strings.TrimSpace(turn), strings.TrimSpace(asked)
	if turn == "" || asked == "" {
		return false
	}
	if turn == asked {
		return true
	}
	head, _, elided := strings.Cut(turn, elisionMark)
	if elided {
		return strings.HasPrefix(asked, head)
	}
	return strings.HasPrefix(asked, turn)
}

// admissionSelfCall are the calls that HAND WORK OVER rather than find anything
// out. Their arguments are the assignment itself — the brief, the deliverable,
// the acceptance — which the document above already prints in full, so a handle
// for one would repeat a whole contract inside the context of the task it
// created.
var admissionSelfCall = map[string]bool{"propose_task": true, "divide_work": true}

// admissionEvidence is the calls that already ran, newest first to what is left
// of the budget, printed oldest first.
//
// A failure outranks a success of the same age, and that is the only weighting
// here. It is not a reading of what the results mean: a successful call's body
// is still readable behind its handle, while a dropped failure costs the next
// worker the same failed call and the same money.
func admissionEvidence(source admissionSource, spent *int) []AdmissionHandle {
	room := admissionRoom() - *spent
	if room <= 0 {
		return nil
	}
	type found struct {
		order  int
		handle AdmissionHandle
	}
	answered := make(map[string]ai.Message, len(source.messages))
	for _, message := range source.messages {
		if message.Role == "tool" && message.ToolCallID != "" {
			answered[message.ToolCallID] = message
		}
	}
	handles := make([]found, 0, 8)
	for index, message := range source.messages {
		if message.Role != "assistant" {
			continue
		}
		for _, call := range message.ToolCalls {
			if call.ID == "" || call.Function.Name == "" || admissionSelfCall[call.Function.Name] {
				continue
			}
			handle := AdmissionHandle{
				Call: call.ID, Tool: call.Function.Name, From: source.from,
				Input: clip(compactArguments(call.Function.Arguments), admissionInputLimit),
			}
			_, wasAnswered := answered[call.ID]
			switch outcome, known := source.outcomes[call.ID]; {
			case known && outcome.failed:
				handle.Outcome, handle.Detail = AdmissionFailed, outcome.detail
			case known:
				handle.Outcome = AdmissionOK
			case !wasAnswered:
				handle.Outcome = AdmissionUnanswered
			}
			// A pointer only where there is something to fetch. The record is the
			// journal, which writes the call id on the result's own line, so the
			// grep the rendered line names really does find it.
			if wasAnswered {
				handle.Source = source.record
			}
			handles = append(handles, found{order: index, handle: handle})
		}
	}
	sort.SliceStable(handles, func(one, two int) bool {
		oneFailed := handles[one].handle.Outcome == AdmissionFailed
		twoFailed := handles[two].handle.Outcome == AdmissionFailed
		if oneFailed != twoFailed {
			return oneFailed
		}
		return handles[one].order > handles[two].order
	})
	kept := make([]found, 0, admissionHandlesKept)
	seen := make(map[string]bool, len(handles))
	for _, candidate := range handles {
		if len(kept) >= admissionHandlesKept {
			break
		}
		if seen[candidate.handle.Call] {
			continue
		}
		cost := handleCost(candidate.handle)
		if cost > room {
			continue
		}
		seen[candidate.handle.Call] = true
		room -= cost
		*spent += cost
		kept = append(kept, candidate)
	}
	sort.SliceStable(kept, func(one, two int) bool { return kept[one].order < kept[two].order })
	out := make([]AdmissionHandle, 0, len(kept))
	for _, candidate := range kept {
		out = append(out, candidate.handle)
	}
	return out
}

// compactArguments is a call's arguments as one line, re-encoded so a model's
// own whitespace does not spend the input bound. Arguments that are not JSON
// stand as written: a handle whose input could not be parsed is still worth more
// than no handle.
func compactArguments(arguments string) string {
	arguments = strings.TrimSpace(arguments)
	if arguments == "" {
		return ""
	}
	var out bytes.Buffer
	if err := json.Compact(&out, []byte(arguments)); err != nil {
		return strings.Join(strings.Fields(arguments), " ")
	}
	return out.String()
}

// inherit carries the parent admission's entries down one generation,
// deduplicated against what this admission holds and bounded by what is left of
// the budget. The inherited half goes in front because it is older.
func (c *AdmissionContext) inherit(parent AdmissionContext, seen map[string]bool, spent int) {
	if parent.empty() {
		return
	}
	carried := make([]AdmissionQuote, 0, admissionInherited)
	for _, quote := range parent.Quotes {
		if len(carried) >= admissionInherited {
			break
		}
		quote.Depth++
		if quote.Depth > admissionDepthLimit || seen[quote.ID] {
			continue
		}
		cost := quoteCost(quote)
		if spent+cost > admissionRoom() {
			continue
		}
		seen[quote.ID], spent = true, spent+cost
		carried = append(carried, quote)
	}
	c.Quotes = append(carried, c.Quotes...)
	// Evidence is inherited only when it FAILED. A handle addresses a result in
	// the producer's own record, which a grandchild that never made the call has
	// no use for; what does travel is that the call went badly, which is what
	// stops a family paying for it twice.
	carriedEvidence := make([]AdmissionHandle, 0, admissionInherited)
	held := make(map[string]bool, len(c.Evidence))
	for _, handle := range c.Evidence {
		held[handle.Call] = true
	}
	for _, handle := range parent.Evidence {
		if len(carriedEvidence) >= admissionInherited || handle.Outcome != AdmissionFailed {
			continue
		}
		handle.Depth++
		if handle.Depth > admissionDepthLimit || held[handle.Call] {
			continue
		}
		cost := handleCost(handle)
		if spent+cost > admissionRoom() {
			continue
		}
		held[handle.Call], spent = true, spent+cost
		carriedEvidence = append(carriedEvidence, handle)
	}
	c.Evidence = append(carriedEvidence, c.Evidence...)
}

// ── the door ────────────────────────────────────────────────────────────────

// admissionContext is what every door hands to the spec it is admitting.
//
// In a node it compiles the node's OWN transcript and inherits its parent's
// admission, which is what makes a division carry both what the conversation
// established and what the worker learned before it divided.
func (a *Agent) admissionContext() AdmissionContext {
	source := admissionSource{scope: "chat/" + a.id}
	// The graph is read before the agent's own lock is taken. Both readings want
	// a lock and this package takes the graph's while holding an agent's in other
	// lanes, so the other order here would be the pair that deadlocks.
	if a.config.InTask && a.config.taskID != 0 {
		source.from = "task " + strconv.FormatUint(a.config.taskID, 10)
		source.scope = source.from + "/" + a.id
		if parent := a.graph().node(a.config.taskID); parent != nil {
			// A node's request is its parent's sentence ([Agent.taskRequest]), and
			// that is what its brief prints.
			source.asked = parent.request()
			source.inherited = parent.admission().restored()
		}
	}
	a.mu.Lock()
	// Copied out rather than referred to: a fold rewrites entries of a.messages
	// in place (turnfold.go) and the person may type again, so a compiler walking
	// the live slices after the lock is let go races the session it describes.
	source.said = append([]personTurn(nil), a.personTurns...)
	source.messages = admissionWindowOf(a.messages)
	source.outcomes = make(map[string]callOutcome, len(a.callOutcomes))
	for id, outcome := range a.callOutcomes {
		source.outcomes[id] = outcome
	}
	if !a.config.InTask {
		source.asked = a.personAsk
	}
	file := a.file
	a.mu.Unlock()
	// THE RECORD IS A PATH AND NOT A STORE ID. `store:412` is not something a
	// worker's `read` or `grep` can open (loop.go states the same rule for a fold
	// marker), and the journal holds the whole conversation with each result
	// under its own call id.
	source.record = file.journalName()
	return compileAdmission(source)
}

// admissionWindowOf copies the tail of the transcript the compiler reads.
func admissionWindowOf(messages []ai.Message) []ai.Message {
	from := len(messages) - admissionWindow
	if from < 0 {
		from = 0
	}
	return append([]ai.Message(nil), messages[from:]...)
}

// elisionMark is what stands where a quote lost its middle.
const elisionMark = " […] "

// elide bounds a quote while KEEPING BOTH ENDS. A person's last sentence is
// where a constraint most often is — "and don't touch the tests" — so a
// head-only clip drops exactly the words a worker most needs. The cut is marked,
// and the quote's Source is what leads to the rest.
func elide(text string, limit int) string {
	text = strings.TrimSpace(text)
	if limit <= len(elisionMark)+2 || len(text) <= limit {
		return text
	}
	room := limit - len(elisionMark)
	tail := room / 3
	head := room - tail
	return strings.TrimSpace(text[:head]) + elisionMark + strings.TrimSpace(text[len(text)-tail:])
}
