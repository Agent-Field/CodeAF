package session

// THE SIX DOORS, WIRED TO THIS CONVERSATION.
//
// internal/exec's env.go is the whole capability surface a subharness has: six
// methods, and no seventh way to spend, reach out, or ask. This file is what
// those six reach on a surface that is a CONVERSATION somebody is sitting in
// front of — which is the half the old bridge never had, and the reason it had
// to write "nobody was there to ask, so it carried on" into its own trail.
//
// Each door rides a road this session already has. Nothing here is new
// machinery; what is new is that a program can reach them.
//
//	ai()        the session's model road — its client, its model, its ledger
//	tool()      the session's belt, filtered by the manifest's whitelist, and
//	            THROUGH THE SAME CONSENT DOORS any tool call goes through
//	ask()       the person, through the run's own question road, with the node
//	            moving to "awaiting your look" for as long as the question stands
//	remember()  the subharness's own memory, on the store lane's seam
//	recall()    the same
//	log()       the run's journal, its room, and the phase on its roster row
//
// ── THE TWO LAWS THIS FILE IS WRITTEN AROUND ──
//
// EVERY CALL IS JOURNALED BEFORE ITS ANSWER RETURNS. Not after, not on the way
// out: the entry IS the fact that the call happened, and a run killed mid-call
// has to leave behind exactly the calls it made — including the one that killed
// it. Every door below writes through [exec.Record] on both its paths.
//
// NOTHING IS EVER SILENTLY APPROVED. The named anti-pattern is the old bridge's
// auto-approving gate (internal/subharness's exec_model.go), which answered yes
// to every question because there was nobody to ask. Here a tool call the policy
// wants a person for goes to the person; a question the program puts goes to the
// person; and where there is genuinely nobody watching, the behaviour is what
// the gate DECLARED — a manifest default, or a stop — and never a guess.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/approval"
	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// SubharnessMemory is where a subharness keeps what it has learned about its own
// domain — its file in its own bundle, never this conversation's memory. A
// program that has learned that this company's brief always arrives as a PDF has
// learned something about its work and nothing about the person it is talking
// to, and the two must not share a page.
//
// IT IS A SEAM AND THE STORE LANE FILLS IT (docs/SUBHARNESS-CONTRACT.md §6: the
// stores, the versions and the journals beside each bundle are all that lane's).
// NIL IS A BUILD WITH NO BUNDLE MEMORY, and the two doors then answer
// [exec.NotWired] for their own names — which is the contract's own answer for a
// door with nothing behind it, and is what [exec.UnwiredEnv] does.
type SubharnessMemory interface {
	// Remember keeps one note in the named subharness's own memory.
	Remember(ctx context.Context, subharness, note string) error
	// Recall reads it back. An empty query is everything it has kept.
	Recall(ctx context.Context, subharness, query string) ([]exec.Note, error)
}

// promptSource is the optional half of a runner that carries prompt assets.
//
// [exec.Env.AI] takes a promptRef because a prompt is a FILE in a bundle — so it
// can be diffed, reviewed and revised — and never an inline string. But the
// bundle belongs to the RUNNER and this Env is built by the surface, so the only
// honest way for this door to resolve a ref is to ask the runner that is about to
// use it. A runner that implements this is asked; one that does not carries no
// prompt assets, and a ref against it is an error naming the ref rather than a
// prompt invented here.
//
// THE RUNTIME LANE IMPLEMENTS IT on the bundle-parameterized runner. Nothing in
// this build does yet: the runners registered today are fronted leaf workers,
// which take no Env at all ([exec.ExecutorRunner] says so about itself).
type promptSource interface {
	Prompt(ref string) (string, bool)
}

// subharnessEnv is one run's capability surface.
type subharnessEnv struct {
	agent    *Agent
	node     *TaskNode
	room     *taskRoom
	journal  *subharnessJournal
	manifest exec.Manifest
	// model is what this run's own model calls ride, resolved once: what the run
	// was asked for, or the conversation's.
	model string
	// prompts resolves a prompt asset out of the runner's bundle, and is nil for
	// a runner that carries none (see [promptSource]).
	prompts func(string) (string, bool)
	// ep and hub are the tool plane and the lane a tool call's own events go out
	// on. THEY ARE THE TURN'S MACHINERY BORROWED FOR A RUN, which is exactly what
	// makes "through the same consent doors" true rather than claimed: the gate,
	// the guardian and the pre-action chain are all reached by handing these two
	// to [Agent.executeTool], the one function every execution in this program
	// passes through.
	ep  *episode
	hub *eventHub
	// calls numbers this run's tool calls, so each one has an id of its own the
	// way a model's batch does.
	calls int
}

// subharnessEnv builds the six doors for one run and starts pumping the tool
// lane into the run's room.
func (a *Agent) subharnessEnv(node *TaskNode, room *taskRoom, journal *subharnessJournal,
	runner exec.Runner, manifest exec.Manifest, model string,
) *subharnessEnv {
	if strings.TrimSpace(model) == "" {
		model = a.Model()
	}
	env := &subharnessEnv{
		agent: a, node: node, room: room, journal: journal,
		manifest: manifest, model: model,
		ep: a.newEpisode(), hub: newEventHub(),
	}
	if source, ok := runner.(promptSource); ok {
		env.prompts = source.Prompt
	}
	// THE LANE IS TAKEN BEFORE ANYTHING CAN BE ON IT, and only the draining is
	// handed to a goroutine — [Agent.designHarnessNode] states the same law about
	// its own room: subscribing inside the pump would leave a window in which a
	// consent question was asked and nobody was carrying it.
	lane := env.hub.subscribe()
	go pumpSubharnessTools(lane, room)
	// The journal hands the same Env to the deoptimization path, so the long way
	// spends through the doors the program was spending through.
	journal.env = env
	return env
}

// close ends the tool lane. The room is the node's and closes with the node.
func (e *subharnessEnv) close() {
	if e != nil && e.hub != nil {
		e.hub.close()
	}
}

// pumpSubharnessTools carries one run's tool events into the room it is running
// in, so a person standing there watches the calls happen — and, above all, is
// SHOWN THE CONSENT QUESTION where it can be answered.
//
// The events are the turn loop's own kinds and a surface already knows how to
// draw every one of them; what is different is only where they arrive. A run
// with nobody in its room drops them, which is what an unwatched turn's events
// do too.
func pumpSubharnessTools(lane <-chan Event, room *taskRoom) {
	for event := range lane {
		if room != nil {
			room.publish(event)
		}
	}
}

// ── ai() ────────────────────────────────────────────────────────────────────

// AI is one model call on the session's own road: its client, the run's model,
// and the ledger the run's spend is folded into when it lands.
//
// THE PROMPT IS A FILE AND NEVER A STRING FROM THE CALLER. A program that could
// inline its prompt would put the one thing worth improving somewhere nothing
// can improve it (internal/exec's env.go states the law); this door resolves the
// ref against the runner's own bundle and refuses a ref nothing has, naming it.
//
// A SCHEMA THAT WAS ASKED FOR IS AN ANSWER THAT HAS TO PARSE. A model that could
// not produce the shape is an error rather than a text answer quietly standing
// in for a structured one — which is the difference between a program that
// stopped and a program that carried on with a string where its object should be.
func (e *subharnessEnv) AI(ctx context.Context, promptRef string, input any, opts exec.AIOptions) (exec.Answer, error) {
	started := time.Now()
	body, marshalErr := json.Marshal(input)
	if marshalErr != nil {
		return exec.Answer{}, e.failed(exec.CallAI, promptRef, nil, started, marshalErr)
	}
	prompt, ok := e.prompt(promptRef)
	if !ok {
		return exec.Answer{}, e.failed(exec.CallAI, promptRef, body, started,
			fmt.Errorf("there is no prompt called %s in this program", strconv.Quote(promptRef)))
	}

	call := provider.WithoutStream(ctx)
	if opts.Effort != provider.EffortNone {
		call = provider.WithConfiguredReasoningEffort(call, opts.Effort)
	}
	options := []ai.Option{ai.WithModel(e.model)}
	if !opts.Schema.Empty() {
		options = append(options, ai.WithSchema(json.RawMessage(opts.Schema)))
	}
	response, err := e.agent.client.CompleteWithMessages(call, []ai.Message{
		textMessage("system", prompt),
		// THE INPUT GOES LAST, as its own message and as JSON. The prompt is the
		// asset under review and the input is the material; two messages keep
		// them apart for the model the way the bundle keeps them apart on disk.
		textMessage("user", string(body)),
	}, options...)
	if err != nil {
		return exec.Answer{}, e.failed(exec.CallAI, promptRef, body, started, err)
	}
	if response == nil {
		return exec.Answer{}, e.failed(exec.CallAI, promptRef, body, started,
			errors.New("nothing came back from the model"))
	}

	answer := exec.Answer{Text: response.Text(), Spend: spendOfResponse(response, e.model)}
	if !opts.Schema.Empty() {
		structured := json.RawMessage(strings.TrimSpace(answer.Text))
		if !json.Valid(structured) {
			return exec.Answer{}, e.failed(exec.CallAI, promptRef, body, started,
				errors.New("the model did not answer in the shape this step asked for"))
		}
		answer.JSON = structured
	}
	e.record(exec.JournalEntry{
		Call: exec.CallAI, Ref: promptRef, Input: body, Output: answerOutput(answer),
		Spend: answer.Spend, Elapsed: time.Since(started),
	})
	return answer, nil
}

// prompt resolves one prompt asset out of the runner's bundle.
func (e *subharnessEnv) prompt(ref string) (string, bool) {
	if e.prompts == nil {
		return "", false
	}
	text, ok := e.prompts(ref)
	if !ok || strings.TrimSpace(text) == "" {
		return "", false
	}
	return text, true
}

// answerOutput is what an ai() call's answer looks like in the journal: the
// structured half when there is one, and the text otherwise. A journal reader
// wants what the step PRODUCED, and for a schema'd step that is the object.
func answerOutput(answer exec.Answer) json.RawMessage {
	if len(answer.JSON) > 0 {
		return answer.JSON
	}
	encoded, err := json.Marshal(answer.Text)
	if err != nil {
		return nil
	}
	return encoded
}

// spendOfResponse reads one provider response into the ledger's figures. It is
// the same reading [Agent.addAuxiliaryUsageAs] makes of the same struct, which
// is what keeps a run's ledger and the session's total agreeing about one call.
func spendOfResponse(response *ai.Response, model string) exec.Spend {
	if response == nil || response.Usage == nil {
		// The provider said nothing about this call. That is not the same fact as
		// a free call, and [exec.Spend.Reported] is what keeps the two apart —
		// so this stays zero and nothing downstream claims the work was free.
		return exec.Spend{Model: model, Calls: 1}
	}
	usage := response.Usage
	spend := exec.Spend{
		Model:      model,
		Calls:      1,
		Input:      usage.PromptTokens,
		Output:     usage.CompletionTokens,
		CacheRead:  usage.CacheReadTokens(),
		CacheWrite: usage.CacheCreationTokens(),
	}
	if usage.Cost != nil {
		spend.CostUSD = *usage.Cost
	}
	return spend
}

// ── tool() ──────────────────────────────────────────────────────────────────

// Tool calls one belt tool THROUGH THE SAME CONSENT DOORS any tool call goes
// through — [Agent.executeTool], the one chokepoint every execution in this
// program passes: the pre-action chain, the approval policy, the guardian, the
// file ledger and the error→fix lane.
//
// A GATE WRITTEN HERE INSTEAD WOULD BE A SECOND GATE. The turn loop's comment
// makes the argument about a tool appended later carrying no gate and nothing
// saying so; a program that reached the belt around the chokepoint would be
// exactly that hole, opened deliberately.
//
// TWO REFUSALS, TOLD APART. A tool not on the manifest's whitelist is refused
// HERE — the program declared what it may touch and this is that declaration
// being kept — and a tool that is not on this session's belt is ABSENT, which is
// a fact about this build. They are different facts about what the person can
// fix, so they are different sentences (internal/exec's env.go demands exactly
// this).
func (e *subharnessEnv) Tool(ctx context.Context, name string, args map[string]any) (exec.ToolResult, error) {
	started := time.Now()
	name = strings.TrimSpace(name)
	body, marshalErr := json.Marshal(args)
	if marshalErr != nil {
		return exec.ToolResult{}, e.failed(exec.CallTool, name, nil, started, marshalErr)
	}
	if !e.whitelisted(name) {
		return exec.ToolResult{}, e.failed(exec.CallTool, name, body, started,
			fmt.Errorf("%s is not one of the tools this program declared it uses", name))
	}
	if !e.onBelt(name) {
		return exec.ToolResult{}, e.failed(exec.CallTool, name, body, started,
			fmt.Errorf("there is no %s here", name))
	}

	e.calls++
	call := ai.ToolCall{
		ID:       fmt.Sprintf("subharness-%d-%d", e.journal.nodeID(), e.calls),
		Type:     "function",
		Function: ai.ToolCallFunction{Name: name, Arguments: string(body)},
	}
	// A CALL THE POLICY WANTS A PERSON FOR MOVES THE ROW. The question itself
	// goes out on the tool lane and is drawn in the run's room, but somebody who
	// is not standing in that room has only the roster to learn from — so the
	// phase says the run needs them for as long as the question stands, exactly
	// as it does for an ask() (see [subharnessEnv.Ask]).
	if decision, governed := e.agent.decide(call); governed && decision.Action == approval.ActionPrompt {
		defer e.working()
		e.needsALook()
	}
	result := e.agent.executeTool(ctx, e.ep, e.hub, call)
	if result.isError {
		return exec.ToolResult{}, e.failed(exec.CallTool, name, body, started, errors.New(result.text))
	}

	answer := exec.ToolResult{Text: result.text}
	// A tool that speaks JSON answers structured as well as in prose. Most of the
	// belt answers in prose and leaves this nil, which is what the contract's
	// ToolResult says about itself.
	if trimmed := strings.TrimSpace(result.text); json.Valid([]byte(trimmed)) && looksStructured(trimmed) {
		answer.JSON = json.RawMessage(trimmed)
	}
	e.record(exec.JournalEntry{
		Call: exec.CallTool, Ref: name, Input: body, Output: toolOutput(answer),
		Elapsed: time.Since(started),
	})
	return answer, nil
}

// looksStructured says whether a tool's prose answer is really an object or an
// array. `json.Valid` says yes to a bare number and to a quoted word, and
// promoting `42` from a tool that measures something into a structured answer
// would be this file inventing a shape the tool never promised.
func looksStructured(text string) bool {
	return strings.HasPrefix(text, "{") || strings.HasPrefix(text, "[")
}

func toolOutput(result exec.ToolResult) json.RawMessage {
	if len(result.JSON) > 0 {
		return result.JSON
	}
	encoded, err := json.Marshal(result.Text)
	if err != nil {
		return nil
	}
	return encoded
}

// whitelisted says whether the manifest lets this program touch this tool.
//
// AN EMPTY WHITELIST IS NO TOOLS AT ALL, and that is the safe reading rather
// than a convenient one: a manifest that lists what it uses is a declaration, and
// reading an absent declaration as "everything" would hand a program that never
// asked for a shell the whole belt. A program that wants tools names them.
func (e *subharnessEnv) whitelisted(name string) bool {
	for _, allowed := range e.manifest.Whitelist {
		if strings.EqualFold(strings.TrimSpace(allowed), name) {
			return true
		}
	}
	return false
}

// onBelt says whether this session actually carries the tool. It asks the belt
// itself rather than a list, so a tool armed for an account this conversation
// connected mid-run is found (connect.go's armFamily).
func (e *subharnessEnv) onBelt(name string) bool {
	for _, tool := range e.agent.beltTools() {
		if tool.Name == name {
			return true
		}
	}
	return false
}

// ── ask() ───────────────────────────────────────────────────────────────────

// subharnessReply is one answer travelling from the surface to the blocked
// question.
type subharnessReply struct {
	text string
	// takingOver is the third answer, and it is the one the old gate got right:
	// a person watching a program they wrote go slightly wrong does not want to
	// kill it and does not want to wave it through — they want to take it from
	// here (internal/exec's AskAnswer states the whole argument).
	takingOver bool
}

// Ask puts one question to the person and waits.
//
// WHAT IT MEANS WITH NOBODY THERE IS DECLARED PER GATE AND NEVER ASSUMED. This
// is the exact line the old bridge crossed: its human.gate auto-approved and
// wrote "nobody was there to ask, so it carried on" into the trail afterwards,
// which is a program deciding on a person's behalf and then telling them. Here a
// question asked where nobody is watching takes the answer the gate DECLARED
// ([exec.AskOptions.Default]) — and where the gate declared none, the run stops
// and says which question it stopped at. EMPTY IS NOT A YES.
//
// WHILE THE QUESTION STANDS THE NODE NEEDS A PERSON, and the row says so. That
// is the "awaiting your look" phase a design's card already wears, borrowed
// here for the same reason: a surface has to be able to tell a run that is
// WORKING from a run that is WAITING ON SOMEBODY, because they belong in
// different tallies and only one of them is going to move on its own.
func (e *subharnessEnv) Ask(ctx context.Context, question string, opts exec.AskOptions) (exec.AskAnswer, error) {
	started := time.Now()
	question = strings.TrimSpace(question)
	if question == "" {
		return exec.AskAnswer{}, e.failed(exec.CallAsk, "", nil, started,
			errors.New("a question with nothing in it is not a question"))
	}
	input := askInput(opts)

	if !e.agent.config.AskConsent {
		answer := unattendedAnswer(opts)
		e.record(exec.JournalEntry{
			Call: exec.CallAsk, Ref: question, Input: input, Output: askOutput(answer),
			Note: unattendedNote(answer), Elapsed: time.Since(started),
		})
		return answer, nil
	}

	replies := make(chan subharnessReply, 1)
	id := e.journal.nodeID()
	e.agent.mu.Lock()
	if e.agent.closed {
		e.agent.mu.Unlock()
		return exec.AskAnswer{}, e.failed(exec.CallAsk, question, input, started, errAgentClosed)
	}
	if e.agent.subharnessAsks == nil {
		e.agent.subharnessAsks = make(map[uint64]chan subharnessReply, 1)
	}
	e.agent.subharnessAsks[id] = replies
	e.agent.mu.Unlock()
	defer e.agent.forgetSubharnessAsk(id)

	e.needsALook()
	defer e.working()
	if e.room != nil {
		e.room.publish(Event{Kind: EventSubharnessAsk, ID: id, Text: question, Args: string(input)})
	}

	select {
	case reply := <-replies:
		answer := exec.AskAnswer{Text: reply.text, TakingOver: reply.takingOver}
		e.record(exec.JournalEntry{
			Call: exec.CallAsk, Ref: question, Input: input, Output: askOutput(answer),
			Elapsed: time.Since(started),
		})
		return answer, nil
	case <-ctx.Done():
		// The node was stopped or the session closed under the question. Either
		// way the call happened and is journaled with what ended it — a run
		// killed on a question has to leave the question behind.
		return exec.AskAnswer{}, e.failed(exec.CallAsk, question, input, started, ctx.Err())
	}
}

// unattendedAnswer is what a question gets where nobody is watching: the gate's
// own declared answer, or a stop.
func unattendedAnswer(opts exec.AskOptions) exec.AskAnswer {
	if answer := strings.TrimSpace(opts.Default); answer != "" {
		return exec.AskAnswer{Text: answer}
	}
	return exec.AskAnswer{Unanswered: true}
}

// unattendedNote is the line the journal carries for such an answer, and it says
// WHICH of the two happened. A trail that recorded only the answer would be
// indistinguishable from a person having typed it, which is the confusion the old
// bridge's trail line existed to paper over.
func unattendedNote(answer exec.AskAnswer) string {
	if answer.Unanswered {
		return "nobody was there and this question declared no answer, so the run stops here"
	}
	return "nobody was there; the answer this question declared was taken"
}

func askInput(opts exec.AskOptions) json.RawMessage {
	encoded, err := json.Marshal(struct {
		Options []string `json:"options,omitempty"`
		Default string   `json:"default,omitempty"`
	}{Options: opts.Options, Default: opts.Default})
	if err != nil {
		return nil
	}
	return encoded
}

func askOutput(answer exec.AskAnswer) json.RawMessage {
	encoded, err := json.Marshal(struct {
		Text       string `json:"text,omitempty"`
		TakingOver bool   `json:"taking_over,omitempty"`
		Unanswered bool   `json:"unanswered,omitempty"`
	}{Text: answer.Text, TakingOver: answer.TakingOver, Unanswered: answer.Unanswered})
	if err != nil {
		return nil
	}
	return encoded
}

// AnswerSubharness answers the question a running subharness is waiting on, on
// the run's own node id — the number on its roster row, in its ✕, and the one a
// person says out loud.
//
// TAKING OVER IS THE THIRD ANSWER and it is not a stop: the run ends where it
// stands with its journal complete, and what the person typed becomes the run's
// answer for whoever picks the work up. That is the difference between this and
// `Cancel("task:9")`, which ends the work and keeps nothing but the trail.
//
// An id nobody is waiting on — a question whose run was stopped, a second press
// — is ignored rather than reported, exactly as [Agent.ResolveConsent] ignores a
// late answer: the answer is simply late, and the surface has already seen the
// row move.
func (a *Agent) AnswerSubharness(id uint64, text string, takingOver bool) {
	a.mu.Lock()
	replies := a.subharnessAsks[id]
	delete(a.subharnessAsks, id)
	a.mu.Unlock()
	if replies == nil {
		return
	}
	// The channel is buffered by one and taken off the map under the lock, so
	// this never blocks and never delivers twice.
	replies <- subharnessReply{text: strings.TrimSpace(text), takingOver: takingOver}
}

// PendingSubharnessAsk reports whether a run is waiting on a person right now,
// and is what a surface asks before drawing an answer box for a room it has just
// walked into. Zero is nothing waiting.
func (a *Agent) PendingSubharnessAsk(id uint64) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	_, waiting := a.subharnessAsks[id]
	return waiting
}

// forgetSubharnessAsk takes a question off the register, whether it was answered
// or the run ended under it.
func (a *Agent) forgetSubharnessAsk(id uint64) {
	a.mu.Lock()
	delete(a.subharnessAsks, id)
	a.mu.Unlock()
}

// ── remember() and recall() ─────────────────────────────────────────────────

// Remember keeps one note in this subharness's own memory.
func (e *subharnessEnv) Remember(ctx context.Context, note string) error {
	started := time.Now()
	memory := e.agent.config.SubharnessMemory
	if memory == nil {
		return e.failed(exec.CallRemember, note, nil, started, &exec.NotWired{Call: exec.CallRemember})
	}
	if err := memory.Remember(ctx, e.manifest.Name, note); err != nil {
		return e.failed(exec.CallRemember, note, nil, started, err)
	}
	e.record(exec.JournalEntry{Call: exec.CallRemember, Ref: note, Elapsed: time.Since(started)})
	return nil
}

// Recall reads that memory back. An empty query is everything it has kept.
func (e *subharnessEnv) Recall(ctx context.Context, query string) ([]exec.Note, error) {
	started := time.Now()
	memory := e.agent.config.SubharnessMemory
	if memory == nil {
		return nil, e.failed(exec.CallRecall, query, nil, started, &exec.NotWired{Call: exec.CallRecall})
	}
	notes, err := memory.Recall(ctx, e.manifest.Name, query)
	if err != nil {
		return nil, e.failed(exec.CallRecall, query, nil, started, err)
	}
	output, _ := json.Marshal(notes)
	e.record(exec.JournalEntry{Call: exec.CallRecall, Ref: query, Output: output, Elapsed: time.Since(started)})
	return notes, nil
}

// ── log() ───────────────────────────────────────────────────────────────────

// Log raises one visible progress row, in the program's own words.
//
// IT IS THE ONE DOOR TO A PERSON'S ATTENTION THAT COSTS NOTHING, so it reaches
// all three places a person might be looking: the journal, which is the
// permanent record; the room, which is where somebody watching the run is
// standing; and the ROW'S PHASE, which is the only one of the three visible to
// somebody who is doing something else. A run whose program logs nothing draws
// no phase at all rather than an invented one — the emptiness law, at the one
// place in this file where a blank is tempting to fill.
func (e *subharnessEnv) Log(ctx context.Context, status string) error {
	status = strings.TrimSpace(status)
	if status == "" {
		return nil
	}
	e.record(exec.JournalEntry{Call: exec.CallLog, Note: status})
	if e.node != nil {
		e.node.doingNow(clip(firstLine(status), hintLimit))
	}
	return nil
}

// ── the two things every door does ──────────────────────────────────────────

// record writes one finished call into the run's journal.
func (e *subharnessEnv) record(entry exec.JournalEntry) {
	_ = exec.Record(e.journal, entry)
}

// failed journals a call that did not work and hands back the error to raise.
//
// A CALL THAT FAILED IS STILL A CALL THAT HAPPENED, and it still cost whatever
// it spent before it failed. So the entry is written on this path exactly as it
// is on the other one, with what went wrong in it — a journal that only recorded
// successes would be a record of a run that never had any trouble.
func (e *subharnessEnv) failed(call, ref string, input json.RawMessage, started time.Time, err error) error {
	e.record(exec.JournalEntry{
		Call: call, Ref: ref, Input: input, Err: err.Error(), Elapsed: time.Since(started),
	})
	return err
}

// needsALook and working move the row between the two phases a run has: WORKING,
// which draws nothing extra, and WAITING ON SOMEBODY, which is the phase a
// design's card already wears.
//
// THE WORD IS BORROWED RATHER THAN RESPELLED. A surface tells work that is
// running from work that needs a person by comparing this exact string
// (internal/tui3's railGroupOf), and a second spelling of it over here would be
// the same fact written down twice with only one of them maintained.
func (e *subharnessEnv) needsALook() {
	if e.node != nil {
		e.node.doingNow(HarnessPhaseAsking)
	}
}

func (e *subharnessEnv) working() {
	if e.node != nil {
		e.node.doingNow("")
	}
}
