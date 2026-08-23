package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/approval"
	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// THE HEADLESS ENV: the six host doors with nobody standing behind them.
//
// This is the whole capability surface a subharness gets from `aforge run
// subharness` (docs/SUBHARNESS-PRD.md §9, the "Headless" bullet). It implements
// [exec.Env] and NOTHING IN THIS FILE MAY REACH THE TASK SYSTEM — the PRD says
// it in one sentence, "the headless path must not import the task system", and
// the reason is structural rather than tidy: a run's roster row, its room and
// its ✕ are a PRESENTATION of a run, so a headless door that reached for them
// would make the presentation part of the definition and there would no longer
// be a way to run a program without a conversation around it.
//
// What is different here is not that the doors are weaker. It is that there is
// NOBODY TO ASK, and every door that would have asked somebody says so out loud
// rather than deciding on their behalf:
//
//   - [headlessEnv.Ask] takes the answer the gate declared in advance, or stops
//     the run. It never invents one. That is the anti-pattern this whole command
//     exists to not repeat — internal/subharness/exec_model.go's gate
//     AUTO-APPROVES when no Ask was wired and writes "nobody was there to ask,
//     so it carried on" into the trail afterwards, which is an apology rather
//     than a decision.
//   - [headlessEnv.Tool] refuses anything the approval policy would have put in
//     front of a person, on exactly internal/session/consent.go's reasoning:
//     blocking would hang a run forever on a question with no reader, and
//     allowing would make "prompt" mean "allow" wherever the surface is not a
//     terminal.
//
// EVERY CALL IS JOURNALED BEFORE ITS ANSWER RETURNS (internal/exec/journal.go).
// The journal is the only account a headless run leaves of itself, and summing
// its entries is how the run's ledger is computed — which is why no door here
// may return without writing its row first, including the doors that refuse.
type headlessEnv struct {
	// client is the provider this command built, and it is the only way this
	// program can spend on a model.
	client exec.Completer
	// tools is exec's own toolbox rather than the session's belt, because the
	// session's belt is the session's. It is the bare set: a workspace, a shell,
	// the reads and the writes, and whatever the whitelist armed.
	tools *exec.Toolbox
	// policy is the person's own consent policy, read from their settings by
	// [v3Policy]. It is loaded rather than assumed so that somebody who wrote
	// `read: allow` down gets what they wrote; everything it would ask about is
	// refused below, because asking is what this surface cannot do.
	policy approval.Policy
	// manifest is the program being run. Only its whitelist is read here, and it
	// is read as a CEILING: a tool it does not name is not a call this program
	// may make, whatever the belt holds.
	manifest exec.Manifest
	journal  *runJournal
	stderr   io.Writer

	// prompts resolves a promptRef to the prompt it names.
	//
	// A NAMED SEAM FOR THE RUNTIME LANE. [exec.Env.AI]'s promptRef names a prompt
	// ASSET IN A BUNDLE — a file under prompts/ that can be diffed, reviewed and
	// revised — and bundles are the runtime and store lanes' to build. Until one
	// exists there is nothing on disk to resolve against, and a Go runner
	// compiled into this binary has no bundle at all: its promptRef IS the
	// instruction its author wrote, because there is no file for it to be a
	// pointer into. So an unresolved ref is carried through as the instruction
	// rather than refused, and the lane that lands bundles fills this in.
	prompts func(ref string) (string, bool)
}

// newHeadlessEnv seats one program's capability surface. It is built per run and
// per manifest, because the whitelist a tool call is filtered against is the
// manifest's and nothing else's.
func newHeadlessEnv(client exec.Completer, tools *exec.Toolbox, policy approval.Policy,
	manifest exec.Manifest, journal *runJournal, stderr io.Writer) *headlessEnv {
	// The whitelist is armed before anything is drawn from the toolbox, so that a
	// program that named an optional tool — an image, a document — is told that
	// tool is present rather than absent. Arming a name this build does not have
	// admits nothing, which is what keeps "not on the list" and "not in this
	// build" two different answers below rather than one.
	if tools != nil && len(manifest.Whitelist) > 0 {
		tools.Arm(manifest.Whitelist...)
	}
	return &headlessEnv{client: client, tools: tools, policy: policy,
		manifest: manifest, journal: journal, stderr: stderr}
}

// AI is one model call, and it is one call: no loop, no tools, no transcript.
//
// A program that wants a whole agent asks for the generalist; what this door is
// for is the step a typed program is made of — classify this, draft that,
// extract these five fields — and the shape it answers in is the schema the
// program declared. A SCHEMA THAT COULD NOT BE PRODUCED IS AN ERROR and never a
// text answer standing in for a structured one ([exec.AIOptions.Schema] states
// it): a caller that asked for fields and received prose would go on to read
// fields out of it, and the failure would surface three steps later wearing
// somebody else's name.
func (e *headlessEnv) AI(ctx context.Context, promptRef string, input any, opts exec.AIOptions) (answer exec.Answer, err error) {
	started := time.Now()
	material, marshalErr := json.Marshal(input)
	if marshalErr != nil {
		material = []byte("null")
	}
	// The row is written on the way out, whatever the way out is. A run killed
	// mid-call has to leave behind exactly the calls it made, and a call that
	// failed still cost what it spent before it failed.
	defer func() {
		_ = exec.Record(e.journal, exec.JournalEntry{
			At: time.Now(), Call: exec.CallAI, Ref: promptRef,
			Input: json.RawMessage(material), Output: answer.JSON,
			Spend: answer.Spend, Err: errorText(err), Elapsed: time.Since(started),
		})
	}()

	if e.client == nil {
		return exec.Answer{}, fmt.Errorf("there is no model behind this run to ask")
	}
	instruction := promptRef
	if e.prompts != nil {
		if resolved, ok := e.prompts(promptRef); ok {
			instruction = resolved
		}
	}
	options := []ai.Option{ai.WithSystem(instruction)}
	if !opts.Schema.Empty() {
		options = append(options, ai.WithSchema(json.RawMessage(opts.Schema)))
	}
	// The reasoning knob travels in the provider package's own vocabulary. The
	// zero value is "say nothing and let the model use its own default", which is
	// not the same request as off, so it leaves whatever the run was started
	// with in place rather than overwriting it with silence.
	if opts.Effort != provider.EffortNone {
		ctx = provider.WithReasoningEffort(ctx, opts.Effort)
	}
	response, callErr := e.client.CompleteWithMessages(ctx,
		[]ai.Message{{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: string(material)}}}},
		options...)
	if callErr != nil {
		return exec.Answer{}, callErr
	}
	answer = exec.Answer{Text: responseText(response), Spend: spendOfResponse(response)}
	if opts.Schema.Empty() {
		return answer, nil
	}
	shaped := strings.TrimSpace(answer.Text)
	if !json.Valid([]byte(shaped)) {
		return exec.Answer{Text: answer.Text, Spend: answer.Spend}, fmt.Errorf(
			"%q asked for its answer in a particular shape and what came back was not that shape", promptRef)
	}
	answer.JSON = json.RawMessage(shaped)
	return answer, nil
}

// Tool is one bare tool call, and it has THREE separate refusals that are told
// apart on purpose. internal/exec/env.go says it exactly: a tool not on the
// whitelist is refused here, a tool not on the belt is absent, and the two are
// different facts about what the person can fix. The third is this surface's
// own — a call the approval policy would have put in front of somebody, in a
// room with nobody in it.
func (e *headlessEnv) Tool(ctx context.Context, name string, args map[string]any) (result exec.ToolResult, err error) {
	started := time.Now()
	arguments, marshalErr := json.Marshal(args)
	if marshalErr != nil {
		arguments = []byte("{}")
	}
	// wentWrong is what the TOOL said went wrong, which is a different fact from
	// the refusals below: a refusal never ran, and a tool that ran and failed is
	// a call that happened and cost what it spent. Both land in the one row.
	var wentWrong string
	defer func() {
		note := errorText(err)
		if note == "" {
			note = wentWrong
		}
		_ = exec.Record(e.journal, exec.JournalEntry{
			At: time.Now(), Call: exec.CallTool, Ref: name,
			Input: json.RawMessage(arguments), Output: result.JSON,
			Spend: result.Spend, Err: note, Elapsed: time.Since(started),
		})
	}()

	name = strings.TrimSpace(name)
	if name == "" {
		return exec.ToolResult{}, fmt.Errorf("a tool call needs the name of a tool")
	}
	// THE WHITELIST IS A CEILING AND NOT A GRANT. An empty one is not an
	// oversight — it is a program that was written to spend on the model and
	// nothing else — so it refuses everything rather than admitting everything.
	if len(e.manifest.Whitelist) == 0 {
		return exec.ToolResult{}, fmt.Errorf(
			"%s spends on the model and nothing else, so %q is not a call it can make",
			e.programName(), name)
	}
	if !whitelisted(e.manifest.Whitelist, name) {
		return exec.ToolResult{}, fmt.Errorf("%q is not one of the tools %s may use — it may use: %s",
			name, e.programName(), strings.Join(e.manifest.Whitelist, ", "))
	}
	if !e.present(name) {
		// A capability that cannot work is absent rather than broken, and saying
		// so is the whole difference between "you may not" and "there is nothing
		// here": one of them is fixed by editing the program and the other by
		// running it somewhere that has the tool.
		return exec.ToolResult{}, fmt.Errorf(
			"%s may use %q and this build does not have it, so there is nothing here to call", e.programName(), name)
	}
	// NOBODY IS WATCHING, and denying is the only honest answer. This is
	// internal/session/consent.go's own reasoning, reached without importing it:
	// waiting would hang the run forever on a question with no reader, and
	// running it anyway would make "prompt" mean "allow" wherever the surface is
	// not a terminal. Nothing here auto-approves and nothing here remembers an
	// approval, because there is nobody whose answer could be remembered.
	if decision := e.policy.Check(name, json.RawMessage(arguments)); decision.Action != approval.ActionAllow {
		return exec.ToolResult{}, fmt.Errorf(
			"%q needs somebody to say yes (%s) and nobody is here to ask, so it was refused", name, decision.Rule)
	}
	if e.tools == nil {
		return exec.ToolResult{}, fmt.Errorf(
			"%s may use %q and this run has no workspace to use it in", e.programName(), name)
	}
	answered := e.tools.Execute(ctx, name, string(arguments))
	result = exec.ToolResult{Text: answered.Content, Spend: spendOfUsage(answered.Usage)}
	if body := strings.TrimSpace(answered.Content); json.Valid([]byte(body)) {
		result.JSON = json.RawMessage(body)
	}
	// A tool that went wrong answers with what went wrong rather than killing the
	// run — that is exec's own law about its Result type, and a program reads the
	// mistake and picks another move exactly as a model would. The journal keeps
	// the fact that this one went wrong.
	if answered.IsError {
		wentWrong = firstLine(answered.Content)
	}
	return result, nil
}

// Ask is the gate, and WHAT IT MEANS WITH NOBODY THERE IS DECLARED PER GATE.
//
// THE NAMED ANTI-PATTERN IS internal/subharness/exec_model.go's auto-approving
// gate: with no Ask wired it approves and writes "nobody was there to ask, so it
// carried on" into the trail. That sentence is the tell — a decision that has to
// apologise for itself in the record was made by the wrong party. So:
//
//   - A gate that DECLARED a default has already said what it means unattended,
//     and that answer is taken and journaled as the answer nobody was there to
//     give.
//   - A gate that declared NOTHING gets [exec.AskAnswer.Unanswered]. EMPTY IS
//     NOT A YES (internal/exec/env.go says so on the field itself): the program
//     stops and says which question it stopped at, and it does not guess.
//
// Nothing here ever returns an approval, and nothing here ever returns
// TakingOver — taking over is a person continuing by hand, and there is no
// person in this room to continue.
func (e *headlessEnv) Ask(_ context.Context, question string, opts exec.AskOptions) (exec.AskAnswer, error) {
	answer := exec.AskAnswer{}
	note := "nobody was here to answer this and it declared no answer of its own, so the run stops here"
	if declared := strings.TrimSpace(opts.Default); declared != "" {
		answer.Text = declared
		note = "nobody was here to answer this, so it took the answer it declared in advance"
	} else {
		answer.Unanswered = true
	}
	output, marshalErr := json.Marshal(answer.Text)
	if marshalErr != nil {
		output = nil
	}
	_ = exec.Record(e.journal, exec.JournalEntry{
		At: time.Now(), Call: exec.CallAsk, Ref: strings.TrimSpace(question),
		Output: output, Note: note,
	})
	return answer, nil
}

// Remember keeps one note in this subharness's own memory.
//
// A NAMED SEAM FOR THE STORE LANE, and this is the honest shape of it until that
// lane lands. A bundle's memory is a file beside the bundle in
// `~/.aforge/subharnesses/` or `.aforge/subharnesses/`, and both of those
// directories, their versioning and the run journals kept next to them are the
// store lane's whole assignment (docs/SUBHARNESS-CONTRACT.md §6). There is
// nowhere for a note to go until it exists, so this answers exactly what the
// contract's own UnwiredEnv answers — a typed [exec.NotWired] naming the door —
// rather than accepting a note and dropping it, which would be a program taught
// that it had learned something it had not.
func (e *headlessEnv) Remember(_ context.Context, note string) error {
	err := &exec.NotWired{Call: exec.CallRemember}
	_ = exec.Record(e.journal, exec.JournalEntry{
		At: time.Now(), Call: exec.CallRemember, Ref: firstLine(note), Err: err.Error(),
	})
	return err
}

// Recall reads that memory back, and is unwired for the same reason
// [headlessEnv.Remember] is — see the seam note there. The store lane fills both
// in one change, because a memory that can be written and not read is not a
// memory.
func (e *headlessEnv) Recall(_ context.Context, query string) ([]exec.Note, error) {
	err := &exec.NotWired{Call: exec.CallRecall}
	_ = exec.Record(e.journal, exec.JournalEntry{
		At: time.Now(), Call: exec.CallRecall, Ref: strings.TrimSpace(query), Err: err.Error(),
	})
	return nil, err
}

// Log is the one door to a person's attention that costs nothing, and in a
// headless run IT IS THE ONLY PROGRESS THERE IS. It goes to stderr rather than
// stdout because stdout is the run's answer and a caller pipes it somewhere.
//
// The vocabulary law reaches the line, and it reaches it from the program's own
// words: work is running, finishing, done, incomplete, or needs your look.
// Nothing here dresses a status up or translates it.
func (e *headlessEnv) Log(_ context.Context, status string) error {
	line := strings.TrimSpace(status)
	if line == "" {
		// Nothing renders as nothing. A program that logged an empty string has
		// said nothing, and a bare bullet on stderr would be a row about silence.
		return nil
	}
	if e.stderr != nil {
		fmt.Fprintf(e.stderr, "  · %s\n", line)
	}
	return exec.Record(e.journal, exec.JournalEntry{At: time.Now(), Call: exec.CallLog, Note: line})
}

// programName is what this program is called where a refusal names it. It is
// quoted so a name and the prose around it stay apart at a glance.
func (e *headlessEnv) programName() string {
	if name := strings.TrimSpace(e.manifest.Name); name != "" {
		return fmt.Sprintf("%q", name)
	}
	return "this program"
}

// present reports that this build actually has the named tool. It reads the
// toolbox's own definitions rather than a list written here, so a tool that
// arrives or leaves internal/exec changes this answer without changing this
// file.
func (e *headlessEnv) present(name string) bool {
	if e.tools == nil {
		return false
	}
	for _, definition := range e.tools.Definitions() {
		if definition.Function.Name == name {
			return true
		}
	}
	return false
}

// whitelisted is the manifest's ceiling, asked one name at a time.
func whitelisted(list []string, name string) bool {
	for _, allowed := range list {
		if strings.TrimSpace(allowed) == name {
			return true
		}
	}
	return false
}

// runJournal is the run's own account of itself: every host call, in order,
// held in memory and — where the caller named a file — appended to it as one
// JSON object per line.
//
// IT IS ALSO THE LEDGER. Summing the entries' spend is the only way this command
// computes what a run cost, which is precisely what makes the journal and the
// ledger unable to disagree (internal/exec/journal.go states the law). There is
// no counter beside this one.
//
// NOTHING HERE LOCKS, which is the same law [exec.Spend] states about itself and
// for the same reason: host calls are made from the one goroutine a run has.
type runJournal struct {
	entries []exec.JournalEntry
	total   exec.Spend
	seq     int
	// sink is where the entries are also written, one JSON object per line, when
	// the caller asked for a file. Nil is a run nobody is keeping a file for,
	// which is the ordinary case and not an error.
	sink io.Writer
}

// Write records one entry. The sequence number is stamped here rather than by
// the caller, because it counts from one WITHIN A RUN and only the journal knows
// where a run started.
func (j *runJournal) Write(entry exec.JournalEntry) error {
	if j == nil {
		return nil
	}
	j.seq++
	entry.Seq = j.seq
	if entry.At.IsZero() {
		entry.At = time.Now()
	}
	j.entries = append(j.entries, entry)
	j.total.Add(entry.Spend)
	if j.sink == nil {
		return nil
	}
	encoded, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(j.sink, string(encoded))
	return err
}

// Ledger is what this run cost, which is the sum of what its calls cost and can
// be nothing else.
func (j *runJournal) Ledger() exec.Spend {
	if j == nil {
		return exec.Spend{}
	}
	return j.total
}

// responseText reads one completion's words out. A response with several text
// parts is joined rather than truncated: the parts are one answer split by the
// transport, and reading only the first is how a reasoning model's actual answer
// gets thrown away.
func responseText(response *ai.Response) string {
	if response == nil || len(response.Choices) == 0 {
		return ""
	}
	var body strings.Builder
	for _, part := range response.Choices[0].Message.Content {
		if part.Type == "text" || part.Type == "" {
			body.WriteString(part.Text)
		}
	}
	return body.String()
}

// spendOfResponse reads one model call into the ledger's figures.
//
// The call is counted even when the provider reported no usage at all, because a
// missing count is not a call that did not happen — [exec.Spend.Calls] says so
// on the field. Cache reads sit BESIDE the prompt count rather than inside it,
// which is the same reading internal/exec's own spendOf takes of a leaf's usage.
func spendOfResponse(response *ai.Response) exec.Spend {
	spend := exec.Spend{Calls: 1}
	if response == nil {
		return spend
	}
	spend.Model = strings.TrimSpace(response.Model)
	usage := response.Usage
	if usage == nil {
		return spend
	}
	spend.Input = usage.PromptTokens
	spend.Output = usage.CompletionTokens
	spend.CacheRead = usage.CacheReadTokens()
	spend.CacheWrite = usage.CacheCreationTokens()
	if usage.Cost != nil {
		spend.CostUSD = *usage.Cost
	}
	return spend
}

// spendOfUsage reads a tool's own spend into the ledger's figures. Most tools
// spend nothing and leave every field zero, which is the honest answer and not a
// gap.
func spendOfUsage(usage exec.Usage) exec.Spend {
	return exec.Spend{
		Calls: usage.Calls, Input: usage.PromptTokens, Output: usage.CompletionTokens,
		CacheRead: usage.CachedTokens, CostUSD: usage.Cost,
	}
}

// errorText is an error as the journal carries it, and the empty string when
// there was none. Nothing renders as nothing, here as everywhere.
func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
