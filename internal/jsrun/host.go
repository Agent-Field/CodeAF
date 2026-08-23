package jsrun

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/dop251/goja"
)

// THE HOST: the six doors, and no seventh.
//
// What a bundle can reach is exactly what is bound here, and what is bound here
// is exactly [exec.Env] — ai, tool, ask, remember, recall, log. goja starts with
// no filesystem, no network and no clock beyond Date, so this file is not a
// restriction on a larger surface; it IS the surface. Nothing else is bound and
// nothing else may be: a door added here without being added to [exec.Env] would
// be a way for a JS subharness to do something a Go one cannot, and the whole
// contract rests on there being no such thing.
//
// EVERY DOOR JOURNALS BEFORE IT ANSWERS. The entry is written, with its input,
// its output and its spend, and only then does the value go back to the program
// — so a run killed halfway leaves behind exactly the calls it made, including
// the one that was in flight when it was killed. That is the ordering, stated
// once here and obeyed by all six.
//
// HOW A PROGRAM STARTS AND WHAT IT RETURNS. The bundle's program.js is
// evaluated; if it left a function called `run` behind, it is called with the
// typed input and ITS return value is the run's output. Otherwise the script's
// own completion value is the output. The input is also a global called `input`,
// so the shortest honest bundle is one expression. There is no event loop: host
// calls are ordinary synchronous calls, and a program that wants a sequence
// writes a sequence.
type host struct {
	runner  *Runner
	ctx     context.Context
	vm      *goja.Runtime
	rec     *recorder
	fuel    Fuel
	input   any
	unwatch func()
}

// newHost builds the interpreter for one run and binds the six doors into it.
func (r *Runner) newHost(ctx context.Context, input any, rec *recorder) (*host, error) {
	vm := goja.New()
	h := &host{runner: r, ctx: ctx, vm: vm, rec: rec, fuel: r.bundle.Fuel, input: input}

	// The Env is handed this bundle's prompt assets where it asked for them, so
	// that the ref the journal records can be resolved to the text the model
	// actually sees. See [Prompted].
	if wants, ok := rec.env.(Prompted); ok {
		wants.UsePrompts(r.bundle.Prompts)
	}

	doors := map[string]func(goja.FunctionCall) goja.Value{
		exec.CallAI:       h.ai,
		exec.CallTool:     h.tool,
		exec.CallAsk:      h.ask,
		exec.CallRemember: h.remember,
		exec.CallRecall:   h.recall,
		exec.CallLog:      h.log,
	}
	for name, door := range doors {
		if err := vm.Set(name, door); err != nil {
			return nil, fmt.Errorf("%q could not be given its %s() door: %w", r.bundle.Manifest.Name, name, err)
		}
	}
	if err := vm.Set("input", vm.ToValue(input)); err != nil {
		return nil, fmt.Errorf("%q could not be handed its input: %w", r.bundle.Manifest.Name, err)
	}

	h.unwatch = watch(ctx, vm, h.fuel.wall(r.bundle.Manifest.SubharnessInfo))
	return h, nil
}

// close stops the watchdog. A run that finished on its own must not leave a
// goroutine waiting to interrupt an interpreter nobody is using.
func (h *host) close() {
	if h.unwatch != nil {
		h.unwatch()
	}
}

// execute evaluates the bundle and reads its answer out.
func (h *host) execute() (goja.Value, error) {
	value, err := h.vm.RunProgram(h.runner.program)
	if err != nil {
		return nil, err
	}
	if entry, ok := goja.AssertFunction(h.vm.Get("run")); ok {
		return entry(goja.Undefined(), h.vm.ToValue(h.input))
	}
	return value, nil
}

// throw raises a JavaScript error inside the program.
//
// A FAILED DOOR IS CATCHABLE AND A SPENT BUDGET IS NOT, and the difference is
// deliberate: a tool that was not there, a prompt that does not exist, a model
// call that failed are all things a program may legitimately have a plan B for,
// so they arrive as ordinary exceptions. Running out of fuel arrives through the
// interrupt (fuel.go), which no `catch` can reach. An error that nobody catches
// ends the run as a fallback, which is [Runner.stopped]'s job.
func (h *host) throw(err error) {
	panic(h.vm.NewGoError(err))
}

// ── ai ──────────────────────────────────────────────────────────────────────

// ai is one model call, named by a prompt asset in the bundle.
func (h *host) ai(call goja.FunctionCall) goja.Value {
	if !h.spendStep() {
		return goja.Undefined()
	}
	ref, err := h.resolvePrompt(call.Argument(0))
	if err != nil {
		h.rec.write(exec.JournalEntry{Call: exec.CallAI, Err: err.Error()})
		h.throw(err)
	}
	payload := call.Argument(1).Export()
	opts, err := h.aiOptions(call.Argument(2))
	if err != nil {
		h.rec.write(exec.JournalEntry{Call: exec.CallAI, Ref: ref, Err: err.Error()})
		h.throw(err)
	}

	started := time.Now()
	answer, callErr := h.rec.env.AI(h.ctx, ref, payload, opts)
	entry := exec.JournalEntry{
		Call:    exec.CallAI,
		Ref:     ref,
		Input:   encode(payload),
		Output:  encode(answer.Text),
		Spend:   answer.Spend,
		Elapsed: time.Since(started),
	}
	if len(answer.JSON) > 0 {
		entry.Output = answer.JSON
	}
	if callErr != nil {
		entry.Err = callErr.Error()
	}
	h.rec.write(entry)
	h.spendTokens()
	if callErr != nil {
		h.throw(callErr)
	}

	// A call that asked for a shape gets the shape; one that did not gets the
	// text. Handing back a wrapper with both would make every call site unpack
	// something, and [exec.Answer] already says JSON is filled only when a
	// schema asked for it.
	if len(answer.JSON) > 0 {
		return h.parse(answer.JSON, answer.Text)
	}
	return h.vm.ToValue(answer.Text)
}

// resolvePrompt turns ai()'s first argument into the name of an asset in this
// bundle, and REFUSES A PROMPT WRITTEN INLINE.
//
// This is the enforcement of PRD §5 and §12's one shared rule: prompts are files
// under prompts/ so they can be diffed, reviewed, revised into a new version
// and — later — carry a per-call-site cache of accepted outputs. A program that
// could pass its prompt as a string would put the one thing in the whole system
// worth improving somewhere nothing can improve it, and it would do it silently,
// which is why the refusal is prose that says what to do instead rather than a
// message about an argument type.
func (h *host) resolvePrompt(value goja.Value) (string, error) {
	if value == nil || goja.IsUndefined(value) || goja.IsNull(value) {
		return "", fmt.Errorf("ai() needs the name of a prompt in this subharness as its first argument")
	}
	ref := strings.TrimSpace(value.String())
	if ref == "" {
		return "", fmt.Errorf("ai() needs the name of a prompt in this subharness as its first argument")
	}
	if looksInline(ref) {
		return "", fmt.Errorf("ai() takes the NAME of a prompt in this subharness, not the prompt itself — " +
			"put those words in a file under prompts/ and name the file here, so the prompt can be read, reviewed and revised")
	}
	name := promptName(ref)
	if _, ok := h.runner.bundle.Prompts[name]; !ok {
		available := h.runner.bundle.promptNames()
		if len(available) == 0 {
			return "", fmt.Errorf("there is no prompt called %q here — this subharness carries no prompts", name)
		}
		return "", fmt.Errorf("there is no prompt called %q here — the prompts it has are %s",
			name, strings.Join(available, ", "))
	}
	return name, nil
}

// looksInline says this is a prompt and not the name of one. A name is a file
// name: one word, short. Prose has spaces and lines in it, and the two are told
// apart by the properties a file name actually has rather than by guessing at
// what somebody meant.
func looksInline(ref string) bool {
	return strings.ContainsAny(ref, " \t\n\r") || len(ref) > 120
}

// promptName reads a ref the way an author writes one. `summarise`,
// `summarise.md` and `prompts/summarise.md` are the same asset, because those
// are the three ways somebody who is looking at the directory will type it.
func promptName(ref string) string {
	ref = strings.TrimPrefix(ref, "./")
	ref = strings.TrimPrefix(ref, "prompts/")
	return strings.TrimSuffix(ref, ".md")
}

// aiOptions reads ai()'s third argument: a shape to constrain the answer, and
// how hard to think.
func (h *host) aiOptions(value goja.Value) (exec.AIOptions, error) {
	var opts exec.AIOptions
	if value == nil || goja.IsUndefined(value) || goja.IsNull(value) {
		return opts, nil
	}
	fields, ok := value.Export().(map[string]any)
	if !ok {
		return opts, fmt.Errorf("ai()'s third argument is a set of options, written like {schema: …, effort: \"low\"}")
	}
	if shape, present := fields["schema"]; present && shape != nil {
		raw, err := json.Marshal(shape)
		if err != nil {
			return opts, fmt.Errorf("this schema cannot be written down: %w", err)
		}
		opts.Schema = exec.Schema(raw)
		if err := opts.Schema.Validate(); err != nil {
			return opts, err
		}
	}
	if effort, present := fields["effort"]; present && effort != nil {
		parsed, ok := provider.ParseEffort(fmt.Sprint(effort))
		if !ok {
			return opts, fmt.Errorf("%q is not an amount of effort — they are off, low, medium and high", fmt.Sprint(effort))
		}
		opts.Effort = parsed
	}
	return opts, nil
}

// parse hands structured JSON back to the program as a JavaScript value,
// falling back to the text when the bytes will not parse. A model that promised
// a shape and sent something else is the Env's problem to report; this door's
// job is not to lose what did come back.
func (h *host) parse(raw json.RawMessage, fallback string) goja.Value {
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return h.vm.ToValue(fallback)
	}
	return h.vm.ToValue(decoded)
}

// ── tool ────────────────────────────────────────────────────────────────────

// tool calls one belt tool, THROUGH THE MANIFEST'S WHITELIST FIRST.
//
// The filter is here rather than in the Env on purpose: the whitelist is a fact
// about THIS subharness, and the Env is one capability surface shared by every
// runner in the process. A refused call never reaches [exec.Env.Tool] at all —
// it is not a tool call that was denied downstream, it is a tool call this
// subharness was never allowed to make — but it is still journaled, because a
// program reaching for something it may not have is exactly the kind of fact a
// later revision is argued from.
func (h *host) tool(call goja.FunctionCall) goja.Value {
	if !h.spendStep() {
		return goja.Undefined()
	}
	name := strings.TrimSpace(call.Argument(0).String())
	if name == "" || goja.IsUndefined(call.Argument(0)) {
		err := fmt.Errorf("tool() needs the name of a tool as its first argument")
		h.rec.write(exec.JournalEntry{Call: exec.CallTool, Err: err.Error()})
		h.throw(err)
	}
	if err := h.whitelisted(name); err != nil {
		h.rec.write(exec.JournalEntry{Call: exec.CallTool, Ref: name, Err: err.Error()})
		h.throw(err)
	}

	args, _ := call.Argument(1).Export().(map[string]any)
	started := time.Now()
	result, callErr := h.rec.env.Tool(h.ctx, name, args)
	entry := exec.JournalEntry{
		Call:    exec.CallTool,
		Ref:     name,
		Input:   encode(args),
		Output:  encode(result.Text),
		Spend:   result.Spend,
		Elapsed: time.Since(started),
	}
	if len(result.JSON) > 0 {
		entry.Output = result.JSON
	}
	if callErr != nil {
		entry.Err = callErr.Error()
	}
	h.rec.write(entry)
	h.spendTokens()
	if callErr != nil {
		h.throw(callErr)
	}
	if len(result.JSON) > 0 {
		return h.parse(result.JSON, result.Text)
	}
	return h.vm.ToValue(result.Text)
}

// whitelisted says whether this subharness may call that tool, in the prose its
// author needs. The empty whitelist has its own sentence because it is a
// different fact: not "you named the wrong tool" but "this one spends only on
// the model".
func (h *host) whitelisted(name string) error {
	manifest := h.runner.bundle.Manifest
	if len(manifest.Whitelist) == 0 {
		return fmt.Errorf("%q may not call tools — it spends only on the model", manifest.Name)
	}
	for _, allowed := range manifest.Whitelist {
		if allowed == name {
			return nil
		}
	}
	return fmt.Errorf("%q may not call %q — the tools it may use are %s",
		manifest.Name, name, strings.Join(manifest.Whitelist, ", "))
}

// ── ask ─────────────────────────────────────────────────────────────────────

// ask puts a question to the person and waits.
//
// TWO OF THE THREE ANSWERS END THE RUN, and the program never sees them. Taking
// over ends it where it stands with what they typed as the report — a person
// watching something go slightly wrong wants to continue by hand, not to kill it
// and not to wave it through. Nobody there and no default declared ends it
// naming the question it stopped at, because the alternative is guessing on
// somebody's behalf, and the old bridge's auto-approving gate
// (internal/subharness/exec_model.go) is the anti-pattern this whole signature
// exists to make unnecessary.
func (h *host) ask(call goja.FunctionCall) goja.Value {
	if !h.spendStep() {
		return goja.Undefined()
	}
	question := strings.TrimSpace(call.Argument(0).String())
	if question == "" || goja.IsUndefined(call.Argument(0)) {
		err := fmt.Errorf("ask() needs a question as its first argument")
		h.rec.write(exec.JournalEntry{Call: exec.CallAsk, Err: err.Error()})
		h.throw(err)
	}
	opts := h.askOptions(call.Argument(1))

	started := time.Now()
	answer, callErr := h.rec.env.Ask(h.ctx, question, opts)
	entry := exec.JournalEntry{
		Call:    exec.CallAsk,
		Ref:     question,
		Input:   encode(opts.Options),
		Output:  encode(answer.Text),
		Elapsed: time.Since(started),
	}
	if callErr != nil {
		entry.Err = callErr.Error()
	}
	h.rec.write(entry)
	if callErr != nil {
		h.throw(callErr)
	}
	switch {
	case answer.TakingOver:
		h.rec.report = answer.Text
		h.stop(halt{why: "you took it from here."})
		return goja.Undefined()
	case answer.Unanswered:
		h.stop(halt{why: fmt.Sprintf("nobody was there to answer: %s", question)})
		return goja.Undefined()
	}
	return h.vm.ToValue(answer.Text)
}

// askOptions reads the chips a question offers and the answer an unattended run
// takes. A malformed options object is not worth refusing a question over — the
// question is the thing the person reads.
func (h *host) askOptions(value goja.Value) exec.AskOptions {
	var opts exec.AskOptions
	if value == nil || goja.IsUndefined(value) || goja.IsNull(value) {
		return opts
	}
	fields, ok := value.Export().(map[string]any)
	if !ok {
		return opts
	}
	if listed, ok := fields["options"].([]any); ok {
		for _, option := range listed {
			opts.Options = append(opts.Options, fmt.Sprint(option))
		}
	}
	if fallback, ok := fields["default"]; ok && fallback != nil {
		opts.Default = fmt.Sprint(fallback)
	}
	return opts
}

// ── remember and recall ─────────────────────────────────────────────────────

// remember keeps one note in this subharness's OWN memory.
//
// The bundle's memory door is preferred over the Env's because that is what the
// memory IS — memory.md in this bundle, not the session's, so that a subharness
// which has learned that this company's brief always arrives as a PDF has
// learned something about its domain and nothing about the conversation. A
// bundle whose store did not open one falls through to the Env rather than
// quietly swallowing the note.
func (h *host) remember(call goja.FunctionCall) goja.Value {
	if !h.spendStep() {
		return goja.Undefined()
	}
	note := strings.TrimSpace(call.Argument(0).String())
	if note == "" || goja.IsUndefined(call.Argument(0)) {
		err := fmt.Errorf("remember() needs a note to keep")
		h.rec.write(exec.JournalEntry{Call: exec.CallRemember, Err: err.Error()})
		h.throw(err)
	}
	started := time.Now()
	var callErr error
	if memory := h.runner.bundle.Memory; memory != nil {
		callErr = memory.Remember(h.ctx, note)
	} else {
		callErr = h.rec.env.Remember(h.ctx, note)
	}
	entry := exec.JournalEntry{Call: exec.CallRemember, Ref: note, Elapsed: time.Since(started)}
	if callErr != nil {
		entry.Err = callErr.Error()
	}
	h.rec.write(entry)
	if callErr != nil {
		h.throw(callErr)
	}
	return goja.Undefined()
}

// recall reads that memory back. An empty query is everything it has kept.
func (h *host) recall(call goja.FunctionCall) goja.Value {
	if !h.spendStep() {
		return goja.Undefined()
	}
	query := strings.TrimSpace(call.Argument(0).String())
	if goja.IsUndefined(call.Argument(0)) {
		query = ""
	}
	started := time.Now()
	var (
		notes   []exec.Note
		callErr error
	)
	if memory := h.runner.bundle.Memory; memory != nil {
		notes, callErr = memory.Recall(h.ctx, query)
	} else {
		notes, callErr = h.rec.env.Recall(h.ctx, query)
	}
	entry := exec.JournalEntry{
		Call:    exec.CallRecall,
		Ref:     query,
		Output:  encode(notes),
		Elapsed: time.Since(started),
	}
	if callErr != nil {
		entry.Err = callErr.Error()
	}
	h.rec.write(entry)
	if callErr != nil {
		h.throw(callErr)
	}
	kept := make([]any, 0, len(notes))
	for _, note := range notes {
		kept = append(kept, map[string]any{"text": note.Text, "at": note.At})
	}
	return h.vm.ToValue(kept)
}

// ── log ─────────────────────────────────────────────────────────────────────

// log raises one visible progress row, in the program's own words. It is the one
// door to a person's attention that costs nothing, and the LAST thing it said
// becomes the run's report — the journal already carries every row, and
// [exec.RunResult.Report] is one line.
func (h *host) log(call goja.FunctionCall) goja.Value {
	if !h.spendStep() {
		return goja.Undefined()
	}
	status := strings.TrimSpace(call.Argument(0).String())
	if status == "" || goja.IsUndefined(call.Argument(0)) {
		return goja.Undefined()
	}
	started := time.Now()
	callErr := h.rec.env.Log(h.ctx, status)
	entry := exec.JournalEntry{Call: exec.CallLog, Note: status, Elapsed: time.Since(started)}
	if callErr != nil {
		entry.Err = callErr.Error()
	}
	h.rec.write(entry)
	if callErr == nil {
		h.rec.report = status
	}
	return goja.Undefined()
}
