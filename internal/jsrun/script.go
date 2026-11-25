package jsrun

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/dop251/goja"
)

// THE SCRIPT HOST: a model-written program that calls tools, on the same
// machine a saved program runs on.
//
// A saved program is a bundle with a manifest, prompts and a typed answer; a
// script is none of those. It is the body of one async function, it reaches the
// world only through `tools.<name>(args)`, and what it prints or returns is the
// whole of what the model reads back. Everything a tool call does on the way —
// the gate, the fence, the record — belongs to the caller handed in, so this
// file adds no policy of its own: a second gate here would be a gate the
// session's own chokepoint does not know about.
//
// CALLS RUN AT THE SAME TIME, AND ONLY THIS GOROUTINE TOUCHES THE INTERPRETER.
// goja is not safe across goroutines, so a call's own goroutine runs the caller
// and nothing else: it hands its answer back on a channel, and the waiting loop
// in [RunScript] settles the promise here, which runs the script's reactions.
// The loop ends when the script's own promise settles, when nothing is left in
// flight that could settle it, or when the watchdog stops the script. Calls
// still in flight are then cancelled and waited for, so every one of them ends
// recorded and none outlives the script.

// ScriptCaller runs one tool call for a script. A string answer reaches the
// script as a string; any other JSON-shaped value as the matching JavaScript
// value. An error reaches it as a rejected promise carrying the error's text.
type ScriptCaller func(ctx context.Context, name string, args map[string]any) (any, error)

// ScriptLimits are a script's ceilings and how many calls it may have out. A zero takes the default below,
// so a caller states only what it means to change.
type ScriptLimits struct {
	// Calls is how many tool calls the script may make.
	Calls int
	// Wall is the deadline for the whole script.
	Wall time.Duration
	// Output is how many bytes of text the script may produce. It bounds what
	// the script holds for the model, and the cut to the model's window is the
	// caller's, after this.
	Output int
	// Parallel is how many calls may be in flight at once; more wait their turn.
	Parallel int
}

// The defaults are backstops against a runaway script, not a shape an author
// should plan around.
const (
	DefaultScriptCalls    = 200
	DefaultScriptWall     = 10 * time.Minute
	DefaultScriptOutput   = 16 << 20
	DefaultScriptParallel = 4
)

func (l ScriptLimits) resolve() ScriptLimits {
	if l.Calls <= 0 {
		l.Calls = DefaultScriptCalls
	}
	if l.Wall <= 0 {
		l.Wall = DefaultScriptWall
	}
	if l.Output <= 0 {
		l.Output = DefaultScriptOutput
	}
	if l.Parallel <= 0 {
		l.Parallel = DefaultScriptParallel
	}
	return l
}

// ScriptResult is how one script ended. At most one of Failed and Stopped is
// set; neither set is a script that finished.
type ScriptResult struct {
	// Output is the text items and the return value in order, then the console
	// lines. A script that failed or stopped keeps what it produced before.
	Output string
	// Failed is the error nobody caught, in one line.
	Failed string
	// Stopped is why a ceiling or a cancellation ended the script.
	Stopped string
	// Calls is how many tool calls ran: a call still queued when the script
	// ended was cancelled before it ran and is not counted.
	Calls int
	// Unawaited is how many calls a script that finished had started and not
	// waited for: any still running were cancelled when it ended, and their
	// answers, errors included, were dropped.
	Unawaited int
}

// neverSettles is the failure for a script whose own promise is still pending
// when nothing it waits on can move: there are no timers, so a promise with no
// call behind it never settles.
const neverSettles = "the script is waiting on something that can never finish"

// RunScript runs one script. tools is every name the script may call; a name
// that is not a JavaScript identifier is also reachable with its other
// characters written as _.
func RunScript(ctx context.Context, source string, tools []string, call ScriptCaller, ceilings ScriptLimits) ScriptResult {
	ceilings = ceilings.resolve()
	// THE WRAPPER SHARES THE FIRST LINE, so goja's line numbers in an error are
	// the line numbers the model wrote.
	program, err := goja.Compile("script", "(async () => {"+source+"\n})()", false)
	if err != nil {
		return ScriptResult{Failed: brief(err)}
	}

	callCtx, cancel := context.WithCancel(ctx)
	s := &script{
		machine: newMachine(ctx, &recorder{}, limits{steps: ceilings.Calls, wall: ceilings.Wall, stepUnit: "tool calls"}),
		call:    call,
		callCtx: callCtx,
		max:     ceilings.Output,
		slots:   make(chan struct{}, ceilings.Parallel),
		done:    make(chan finished),
	}
	if err := s.bind(tools); err != nil {
		cancel()
		return ScriptResult{Failed: err.Error()}
	}
	s.watch()
	defer s.close()

	var result ScriptResult
	value, runErr := s.vm.RunProgram(program)
	switch why, stopped := interruption(runErr); {
	case stopped:
		result.Stopped = why
	case runErr != nil:
		result.Failed = brief(runErr)
	default:
		s.await(value, &result)
	}
	// Every call still out is cancelled and waited for; each returns promptly
	// and its answer is dropped, so none outlives the script.
	if s.overflow != "" && result.Stopped == "" {
		result.Stopped, result.Failed = s.overflow, ""
	}
	// A FINISHED SCRIPT SAYS WHAT IT LEFT BEHIND. A call started without await
	// is cancelled below, and a result that only said "finished" would read as
	// if that work had been done.
	if result.Stopped == "" && result.Failed == "" {
		result.Unawaited = s.inflight
	}
	cancel()
	s.running.Wait()
	result.Calls = int(s.ran.Load())
	result.Output = s.output()
	return result
}

// script is one run's host: the machine, the caller, and what the script has
// said so far.
type script struct {
	*machine
	call    ScriptCaller
	callCtx context.Context
	max     int
	written int
	items   []string
	console []string

	// slots bounds the calls in flight; done carries each finished call back
	// to this goroutine; inflight and running count what is still out, the
	// first for the waiting loop and the second for [script.drain].
	slots    chan struct{}
	done     chan finished
	inflight int
	running  sync.WaitGroup
	// ran counts the calls that reached the caller. A call is booked against
	// the ceiling when the script makes it, but one still queued for a slot
	// when the script ends never runs and is not a call made.
	ran atomic.Int64
	// overflow is why the output ceiling stopped the script, kept for the
	// stop [script.say] makes after the interpreter has finished, where an
	// interrupt would reach nothing.
	overflow string
}

// finished is one call's answer on its way back to the interpreter's goroutine.
// Settling it there is an ordinary call into the interpreter, and an interrupt
// raised while the script's reactions run comes back as the error.
type finished struct {
	settle func(any) error
	answer any
	err    error
	reject func(any) error
}

// bind puts the script's surface on the machine: tools, text and console.
func (s *script) bind(names []string) error {
	tools := s.vm.NewObject()
	bound := make(map[string]bool, len(names))
	for _, name := range names {
		if err := tools.Set(name, s.tool(name)); err != nil {
			return fmt.Errorf("the script could not be given %s: %w", name, err)
		}
		bound[name] = true
	}
	// AN ALIAS NEVER TAKES A TOOL'S OWN NAME, nor another alias already given,
	// so tools.<x> always reaches the tool spelled x when there is one.
	for _, name := range names {
		alias := identifier(name)
		if bound[alias] {
			continue
		}
		if err := tools.Set(alias, s.tool(name)); err != nil {
			return fmt.Errorf("the script could not be given %s: %w", name, err)
		}
		bound[alias] = true
	}
	console := s.vm.NewObject()
	for _, level := range []string{"log", "info", "warn", "error", "debug"} {
		if err := console.Set(level, s.log); err != nil {
			return fmt.Errorf("the script could not be given console.%s: %w", level, err)
		}
	}
	for name, value := range map[string]any{"tools": tools, "console": console, "text": s.text} {
		if err := s.vm.Set(name, value); err != nil {
			return fmt.Errorf("the script could not be given %s: %w", name, err)
		}
	}
	return nil
}

// tool is one tool's door: it books the call, runs it, and answers a promise.
func (s *script) tool(name string) func(goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		// THE ARGUMENTS ARE READ BEFORE THE CALL IS BOOKED: a call turned away
		// here never ran, so it spends nothing and is not a call made.
		promise, resolve, reject := s.vm.NewPromise()
		args, err := callArguments(name, call.Argument(0))
		if err != nil {
			_ = reject(s.error(err.Error()))
			return s.vm.ToValue(promise)
		}
		if !s.spendStep() {
			return goja.Undefined()
		}
		s.inflight++
		s.running.Add(1)
		go s.run(name, args, resolve, reject)
		return s.vm.ToValue(promise)
	}
}

// run is one call on its own goroutine. It touches the caller and the
// channels and never the interpreter.
func (s *script) run(name string, args map[string]any, resolve, reject func(any) error) {
	defer s.running.Done()
	back := finished{settle: resolve, reject: reject}
	select {
	case s.slots <- struct{}{}:
		// A SLOT FREED BY THE SCRIPT'S ENDING IS NOT AN INVITATION: when both
		// cases are ready the select picks either, so the ending is asked again.
		if back.err = s.callCtx.Err(); back.err == nil {
			s.ran.Add(1)
			back.answer, back.err = s.call(s.callCtx, name, args)
		}
		<-s.slots
	case <-s.callCtx.Done():
		back.err = s.callCtx.Err()
	}
	select {
	case s.done <- back:
	case <-s.callCtx.Done():
		// The script has ended and nobody reads this answer; [script.drain]
		// is waiting for this goroutine to return, and now it does.
	}
}

// await settles finished calls until the script's own promise does, nothing
// is left that could, or the watchdog stops the script. The value is always a
// promise: the source is wrapped in an async function ([RunScript]).
func (s *script) await(value goja.Value, result *ScriptResult) {
	promise := value.Export().(*goja.Promise)
	for promise.State() == goja.PromiseStatePending {
		if s.inflight == 0 {
			result.Failed = neverSettles
			return
		}
		select {
		case back := <-s.done:
			s.inflight--
			// A CANCELLED TURN IS A STOP, whichever news arrives first: the
			// call's own cancellation can reach this loop before the watchdog
			// does, and it must not read as the script failing.
			if s.ctx.Err() != nil {
				result.Stopped = stoppedWhy
				return
			}
			err := s.deliver(back)
			if why, stopped := interruption(err); stopped {
				result.Stopped = why
				return
			}
			if err != nil {
				result.Failed = brief(err)
				return
			}
		case why := <-s.halted:
			result.Stopped = why
			return
		}
	}
	s.settle(promise, result)
}

// deliver settles one call's promise on the interpreter's goroutine.
func (s *script) deliver(back finished) error {
	if back.err != nil {
		return back.reject(s.error(back.err.Error()))
	}
	return back.settle(s.vm.ToValue(back.answer))
}

// callArguments reads a call's one argument: an object, or nothing at all.
func callArguments(name string, value goja.Value) (map[string]any, error) {
	if value == nil || goja.IsUndefined(value) || goja.IsNull(value) {
		return map[string]any{}, nil
	}
	if args, ok := value.Export().(map[string]any); ok {
		return args, nil
	}
	return nil, fmt.Errorf("tools.%s takes one object of arguments, like {command: \"ls\"}", identifier(name))
}

// error is a plain JavaScript Error carrying text, so `e.message` is the
// tool's own words and `String(e)` reads "Error: …".
func (s *script) error(message string) goja.Value {
	made, err := s.vm.New(s.vm.Get("Error"), s.vm.ToValue(message))
	if err != nil {
		return s.vm.ToValue(message)
	}
	return made
}

// text adds one output item.
func (s *script) text(call goja.FunctionCall) goja.Value {
	s.say(&s.items, render(call.Argument(0)))
	return goja.Undefined()
}

// log adds one console line, its arguments joined by spaces.
func (s *script) log(call goja.FunctionCall) goja.Value {
	parts := make([]string, 0, len(call.Arguments))
	for _, argument := range call.Arguments {
		parts = append(parts, render(argument))
	}
	s.say(&s.console, strings.Join(parts, " "))
	return goja.Undefined()
}

// say keeps one line, or stops the script when keeping it would pass the
// output ceiling. The stop is the interrupt, so no catch can write past it.
func (s *script) say(into *[]string, line string) {
	if s.written+len(line)+1 > s.max {
		s.overflow = fmt.Sprintf("it wrote more than %d bytes of output.", s.max)
		s.stop(halt{why: s.overflow})
		return
	}
	s.written += len(line) + 1
	*into = append(*into, line)
}

// settle reads the script's own promise once it is no longer pending, which
// [script.await] guarantees.
func (s *script) settle(promise *goja.Promise, result *ScriptResult) {
	if promise.State() == goja.PromiseStateRejected {
		result.Failed = brief(fmt.Errorf("%s", render(promise.Result())))
		return
	}
	if returned := promise.Result(); returned != nil && !goja.IsUndefined(returned) {
		s.say(&s.items, render(returned))
	}
}

// output is the items, then the console lines under their own heading.
func (s *script) output() string {
	out := strings.Join(s.items, "\n")
	if len(s.console) > 0 {
		if out != "" {
			out += "\n"
		}
		out += "console:\n" + strings.Join(s.console, "\n")
	}
	return out
}

// render writes one value the way a person reads it: a string as itself, an
// error as "Error: message", anything else as JSON.
func render(value goja.Value) string {
	if value == nil || goja.IsUndefined(value) {
		return "undefined"
	}
	if goja.IsNull(value) {
		return "null"
	}
	if text, ok := value.Export().(string); ok {
		return text
	}
	if object, ok := value.(*goja.Object); ok && object.ClassName() == "Error" {
		return value.String()
	}
	encoded, err := json.Marshal(value.Export())
	if err != nil {
		return value.String()
	}
	return string(encoded)
}

// identifier writes a tool name as a JavaScript identifier: every character
// that cannot appear in one becomes _, and a leading digit gets one in front.
func identifier(name string) string {
	var b strings.Builder
	for i, r := range name {
		switch {
		case r == '_' || r == '$' || unicode.IsLetter(r):
			b.WriteRune(r)
		case unicode.IsDigit(r):
			if i == 0 {
				b.WriteRune('_')
			}
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}
