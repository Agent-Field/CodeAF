package session

// One compiler, reached by every door work enters the graph through: a person's
// typed task (task_person.go), a model's proposal (task.go), the parts of a
// division (task_divide_wip.go) and the automatic handover (route_judge.go). A
// continuation re-runs a node that already holds its context (task_continue.go)
// and a checkpoint restores it (task_store.go), so neither compiles a second
// one. admission_law_test.go fails the build if a new door composes its own.
//
// This is supporting context, not a constraint record. The selection is bounded
// in count, in bytes and in generations, so a constraint older than the window,
// deeper than the inheritance limit or in the elided middle of a message is not
// here. The authoritative copies stay where they were: the person's words on the
// spec, the journal behind the origin pointer, the full results in the record.
//
// Selection is newest-first to a byte budget, then printed oldest-first. Newest
// first is aimed at one failure — a twenty-first correction has to survive a
// budget the first twenty exchanges could fill — and printing oldest-first
// afterwards keeps a correction below the thing it corrects.

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

const (
	// admissionQuoteLimit bounds one quoted exchange. A longer one is elided in
	// the middle rather than cut off ([elide]), because a person's last sentence
	// is where a constraint most often is.
	admissionQuoteLimit = 600
	// admissionQuotesKept and admissionHandlesKept bound how many of each one
	// admission carries.
	admissionQuotesKept  = 8
	admissionHandlesKept = 6
	// admissionInputLimit bounds the opening of a call's arguments — enough to
	// recognise the call, never a claim to be the whole of it.
	admissionInputLimit  = 240
	admissionDetailLimit = 240
	// admissionInherited bounds what comes down from the parent's admission,
	// below the local cap: what is local is what the work is about.
	admissionInherited = 4
	// admissionDepthLimit is how many generations one entry may travel.
	admissionDepthLimit = 3
	// admissionBudget is the rendered size of the whole context, one overall
	// bound rather than one per section.
	admissionBudget = 5000
	// admissionTurnsRemembered is how many of the person's turns a session keeps
	// to select from. Larger than admissionQuotesKept: a session that remembered
	// only as many as it carries could not prefer anything.
	admissionTurnsRemembered = 24
	// admissionWindow bounds the walk rather than the selection — how far back
	// the compiler looks, deep enough for a long working turn and its batches.
	admissionWindow = 200
	// admissionOutcomesKept bounds the per-call outcome map, generously next to
	// admissionHandlesKept because the compiler selects the newest calls out of a
	// long turn.
	admissionOutcomesKept = 512
)

// ── what the session keeps so this can be compiled ──────────────────────────

// personTurn is one thing the person typed. The transcript cannot answer that
// question afterwards: every user-role message is user-role, including the ones
// the session wrote itself, and the bit saying who spoke does not survive the
// append. A line steered into a running node is the person typing too, which is
// why this is recorded where the person's words are recorded.
type personTurn struct {
	// seq makes the event the identity, so the same sentence typed again after a
	// correction is a second instruction rather than the first one repeated.
	seq  uint64
	key  string
	text string
}

// rememberPersonTurnLocked appends one turn to the bounded list the compiler
// selects from. The caller holds a.mu.
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
// The journal's own mark for lines the session wrote ([sessionEntry.Note]) is
// the only thing that can tell the person's words from a task's landing note
// after a restart.
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
// refuses to make, so an unrecorded call stays [AdmissionUnknown]. Nothing
// persists it, so work handed out after a restart gets handles whose outcome is
// unknown and says so.
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
		a.callOutcomes = make(map[*ai.ToolCall]callOutcome, len(calls))
	}
	// Emptied rather than trimmed: trimming needs an order this map does not
	// keep, and an id the compiler cannot find is answered "unknown", which is
	// the honest answer for an outcome nobody kept.
	if len(a.callOutcomes) > admissionOutcomesKept {
		a.callOutcomes = make(map[*ai.ToolCall]callOutcome, len(calls))
	}
	for index, call := range calls {
		if index >= len(results) {
			break
		}
		outcome := callOutcome{tool: call.Function.Name, failed: results[index].isError}
		if outcome.failed {
			outcome.detail = clip(firstLine(results[index].text), admissionDetailLimit)
		}
		a.callOutcomes[&calls[index]] = outcome
	}
}

// ── the source ──────────────────────────────────────────────────────────────

// admissionSource is everything the compiler reads. It is a value rather than an
// agent so the selection can be tested for what it selects.
type admissionSource struct {
	// said is the person's own turns, oldest first.
	said []personTurn
	// asked is the request the brief already prints verbatim (task_brief.go's
	// [briefAskHeading]), excluded from the quotes so it is not printed twice
	// under two headings.
	asked string
	// messages is the working transcript, oldest first.
	messages []ai.Message
	// outcomes says how this particular call came back. Shallow snapshots retain
	// its pointer; a restarted process conservatively has no outcome metadata.
	outcomes map[*ai.ToolCall]callOutcome
	// record is the journal these words and results can be read out of: a path
	// read and grep open, which a conversation-store id is not. It is known
	// without reading anything — placing each quote at its own line would mean
	// parsing the whole journal on every spawn for a number that is ambiguous
	// anyway. Empty for a session with no file, and then nothing draws a pointer.
	record       string
	resultSource resultSource
	// scope names where these words were said and is part of every id: the same
	// sentence in two sessions is not one event.
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

// quoteCost and handleCost are the bytes an entry actually costs the worker's
// prompt: the rendered line with its bullet and newline, measured through the
// same functions the document is drawn with. Costing the struct fields instead
// would under-count the speaker, the path, the tool names and the separators,
// and the budget would bound a number nobody reads.
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

// admissionEvidenceFloor is the room quotes may not take. They are selected
// first, and a talkative conversation would otherwise spend everything on eight
// long paragraphs and leave the worker unable to see which calls had run —
// including the failed ones, which are the entries that save it money.
func admissionEvidenceFloor() int { return admissionRoom() / 3 }

// admissionCandidate is one quotable message with the position it was found at.
type admissionCandidate struct {
	order int
	quote AdmissionQuote
}

// admissionCandidates reads the transcript once and returns everything worth
// quoting, in transcript order.
func admissionCandidates(source admissionSource) []admissionCandidate {
	// The person's turns are matched into the transcript by content key. Two
	// identical messages share a key and are separate turns, so each match is
	// consumed once, oldest first.
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
			// Keyed by content, not by position: a fold moves every index after
			// it, so a position-derived id would name a different line after each
			// compaction and could collide with an inherited quote.
			quote := AdmissionQuote{
				ID: source.scope + "/a" + chatRefKey(message), Speaker: admissionAssistant,
				Text: elide(text, admissionQuoteLimit), From: source.from, Source: source.record,
			}
			for callIndex := range message.ToolCalls {
				call := &message.ToolCalls[callIndex]
				quote.Calls = append(quote.Calls, call.Function.Name)
			}
			candidates = append(candidates, admissionCandidate{order: index, quote: quote})
		}
	}
	// A turn the window no longer holds is still something the person said.
	// Those turns sit before everything in the window and keep the order they
	// were heard in, so the newest of them is still selected first and read last.
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
// event ([personTurn.seq]) and the record it can be read out of.
func (s admissionSource) personQuote(turn personTurn) AdmissionQuote {
	return AdmissionQuote{
		ID:      s.scope + "/p" + strconv.FormatUint(turn.seq, 10),
		Speaker: admissionPerson, Text: turn.text, From: s.from, Source: s.record,
	}
}

// sameAsk says whether a remembered turn is the request the brief already prints
// in full. Prefix rather than equality, because the remembered copy is bounded
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

// admissionSelfCall are the calls that hand work over rather than find anything
// out. Their arguments are the assignment itself, which the document above
// already prints in full, so a handle for one would repeat a whole contract
// inside the context of the task it created.
var admissionSelfCall = map[string]bool{"propose_task": true, "divide_work": true}

// admissionEvidence is the calls that already ran, newest first to what is left
// of the budget, printed oldest first.
//
// A failure outranks a success of the same age. That is not a reading of what
// the results mean: a successful call's body is still readable behind its
// handle, while a dropped failure costs the next worker the same failed call.
func admissionEvidence(source admissionSource, spent *int) []AdmissionHandle {
	room := admissionRoom() - *spent
	if room <= 0 {
		return nil
	}
	type found struct {
		order  int
		result int
		handle AdmissionHandle
	}
	answered := make(map[*ai.ToolCall]int)
	for index, call := range toolResultCalls(source.messages) {
		answered[call] = index
	}
	handles := make([]found, 0, 8)
	for index, message := range source.messages {
		if message.Role != "assistant" {
			continue
		}
		for callIndex := range message.ToolCalls {
			call := &message.ToolCalls[callIndex]
			if call.ID == "" || call.Function.Name == "" || admissionSelfCall[call.Function.Name] {
				continue
			}
			handle := AdmissionHandle{
				Call: call.ID, Tool: call.Function.Name, From: source.from,
				Input: clip(compactArguments(call.Function.Arguments), admissionInputLimit),
			}
			resultIndex, wasAnswered := answered[call]
			switch outcome, known := source.outcomes[call]; {
			case !wasAnswered:
				handle.Outcome = AdmissionUnanswered
			case known && outcome.failed:
				handle.Outcome, handle.Detail = AdmissionFailed, outcome.detail
			case known:
				handle.Outcome = AdmissionOK
			}
			// A pointer only where there is something to fetch. The journal writes
			// the call id on the result's own line, so the grep the rendered line
			// names really does find it.
			if wasAnswered {
				handle.Source = source.record
			}
			handles = append(handles, found{order: index, result: resultIndex, handle: handle})
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
	for _, candidate := range handles {
		if len(kept) >= admissionHandlesKept {
			break
		}
		cost := handleCost(candidate.handle)
		if cost > room {
			continue
		}
		// Only selected evidence may file a result. If the more precise pointer
		// would exceed the existing budget, retain the journal reference instead.
		if candidate.handle.Outcome != AdmissionUnanswered && source.resultSource != nil {
			candidate.handle.Result = source.resultSource(source.messages[candidate.result])
			if pointedCost := handleCost(candidate.handle); pointedCost <= room {
				cost = pointedCost
			} else {
				candidate.handle.Result = ""
			}
		}
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
	// Evidence is inherited only when it failed. A handle addresses a result in
	// the producer's own record, which a grandchild that never made the call has
	// no use for; what travels is that the call went badly, which is what stops a
	// family paying for it twice.
	carriedEvidence := make([]AdmissionHandle, 0, admissionInherited)
	held := make(map[AdmissionHandle]bool, len(c.Evidence))
	for _, handle := range c.Evidence {
		held[handle.identity()] = true
	}
	for _, handle := range parent.Evidence {
		if len(carriedEvidence) >= admissionInherited || handle.Outcome != AdmissionFailed {
			continue
		}
		handle.Depth++
		if handle.Depth > admissionDepthLimit || held[handle.identity()] {
			continue
		}
		cost := handleCost(handle)
		if spent+cost > admissionRoom() {
			continue
		}
		held[handle.identity()], spent = true, spent+cost
		carriedEvidence = append(carriedEvidence, handle)
	}
	c.Evidence = append(carriedEvidence, c.Evidence...)
}

// ── the door ────────────────────────────────────────────────────────────────

// admissionContext is what every door hands to the spec it is admitting. In a
// node it compiles the node's own transcript and inherits its parent's, which is
// what makes a division carry both what the conversation established and what
// the worker learned before it divided.
func (a *Agent) admissionContext() AdmissionContext {
	source := admissionSource{scope: "chat/" + a.id}
	// The graph is read before the agent's own lock is taken. Both readings want
	// a lock, and this package takes the graph's while holding an agent's in
	// other lanes, so the other order here would be the pair that deadlocks.
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
	source.outcomes = make(map[*ai.ToolCall]callOutcome, len(a.callOutcomes))
	for id, outcome := range a.callOutcomes {
		source.outcomes[id] = outcome
	}
	if !a.config.InTask {
		source.asked = a.personAsk
	}
	file := a.file
	a.mu.Unlock()
	source.record = file.journalName()
	place := a.resultPlaceNow()
	source.resultSource = func(message ai.Message) string {
		pointer := a.fullResultPointer(message, place)
		if pointer == "" || strings.HasPrefix(pointer, "grep ") {
			return ""
		}
		if !filepath.IsAbs(pointer) {
			pointer = filepath.Join(place.workspace, pointer)
		}
		return pointer
	}
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

// elisionMark stands where a quote lost its middle.
const elisionMark = " […] "

// elide bounds a quote while keeping both ends, because a head-only clip drops
// the trailing sentence a constraint most often lives in. The cut is marked, and
// the quote's Source leads to the rest.
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
