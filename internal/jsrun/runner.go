// Package jsrun is THE JavaScript runtime for subharnesses — one generic Go
// runner parameterized by a bundle.
//
// There is not one runner per subharness and there must never be: there is one
// goja host, and a bundle is its argument ([New]). That is PRD §5's shape taken
// literally, and it is what makes "the person cannot tell which is which" a
// structural fact — a JS subharness and a Go one arrive at [exec.Registry] as
// the same [exec.Runner] and are described by the same [exec.Manifest].
//
// THE SANDBOX IS THE HOST API. goja has no filesystem, no network and no clock
// unless the host hands them in, so the six doors bound in host.go are the whole
// capability surface of a bundle, and they are exactly [exec.Env]'s six. There
// is no seventh way for a program to spend, reach out, or ask — which is what
// makes summing the journal the same thing as summing the run.
//
// THIS PACKAGE NEVER RUNS THE FALLBACK ITSELF. A guard that does not pass, or a
// program that breaks partway, comes back as [exec.RunResult] with FellBack set
// and NO error: the work still has to get done, and the caller that dispatched
// this runner is the one holding [exec.Registry.Generalist] and the original
// input. Deciding to run `linear` here would put the decision in the one place
// that cannot see whether the caller wanted a fallback at all — a headless
// invocation and a task node want different things — so the runner reports and
// the caller chooses.
package jsrun

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/dop251/goja"
)

// Runner is one bundle, compiled and ready to run. It implements [exec.Runner],
// which is the only interface anything outside this package needs from it.
//
// IT IS COMPILED ONCE, AT CONSTRUCTION. A bundle whose program.js will not parse
// is refused by [New] with the compiler's own message and its own line number,
// so a syntax error is an authoring error the store lane can hand straight back
// to whoever wrote it — never a run that mysteriously took the long way.
type Runner struct {
	bundle  Bundle
	program *goja.Program
}

// New compiles a bundle into a runner.
//
// THE COMPILE ERROR IS THE WHOLE AUTHORING ERROR STORY (PRD §5). It is returned
// VERBATIM, with the line and column goja put in it, and nothing here trims,
// rewords or salvages it. There is deliberately no repair ladder: the old
// system's four-rung JSON salvage (internal/subharness/salvage.go) has no
// equivalent here and must not be rebuilt, because accepting almost-JavaScript
// is how a program comes to mean something its author did not write.
func New(bundle Bundle) (*Runner, error) {
	if err := bundle.Validate(); err != nil {
		return nil, err
	}
	program, err := goja.Compile(bundle.source(), bundle.Program, false)
	if err != nil {
		return nil, err
	}
	return &Runner{bundle: bundle, program: program}, nil
}

// Manifest is everything true about this subharness before it runs.
func (r *Runner) Manifest() exec.Manifest { return r.bundle.Manifest }

// Prompts is this bundle's prompt assets, for a caller that has to resolve a
// ref the journal recorded. See [Prompted] for why the resolution lives outside
// this package.
func (r *Runner) Prompts() map[string]string { return r.bundle.Prompts }

// Run does the work: guards, then the program, then the promised shape.
//
// THE THREE ENDINGS ARE THE CONTRACT'S THREE AND NO OTHERS. It finished (Output
// set), it did not finish (Incomplete, with a sentence saying what ran out), or
// it needed a closer look and the caller should do the work the long way
// (FellBack). An error means the run could not be MADE to happen, and after
// [New] has compiled the program the only thing left in that class is input that
// is not JSON.
func (r *Runner) Run(ctx context.Context, input json.RawMessage, env exec.Env) (exec.RunResult, error) {
	if env == nil {
		return exec.RunResult{}, fmt.Errorf("%q was handed nothing to run against", r.bundle.Manifest.Name)
	}
	var typed any
	if len(input) > 0 && string(input) != "null" {
		if err := json.Unmarshal(input, &typed); err != nil {
			return exec.RunResult{}, fmt.Errorf("this input is not what %q takes: %w", r.bundle.Manifest.Name, err)
		}
	}

	// ONE RECORDER FOR THE WHOLE RUN, and it is what makes the ledger and the
	// journal incapable of disagreeing: every host call writes its entry through
	// this and folds its spend into the same total, so the run's Spend is the
	// sum of its journal by construction rather than by two pieces of arithmetic
	// that happen to agree.
	rec := &recorder{journal: journalFor(ctx, env), env: env}

	// The guards run BEFORE the program is entered and before goja is even
	// built. A check that had to start an interpreter to decide whether to start
	// an interpreter would not be the cheap precondition the PRD asked for.
	if fellBack := r.checkGuards(ctx, typed, rec); fellBack != "" {
		rec.deopt(fellBack)
		return exec.RunResult{FellBack: fellBack, Spend: rec.spend}, nil
	}

	running, err := r.newHost(ctx, typed, rec)
	if err != nil {
		return exec.RunResult{}, err
	}
	defer running.close()

	value, runErr := running.execute()
	if result, stopped := r.stopped(runErr, rec); stopped {
		return result, nil
	}

	output, err := marshalOutput(value)
	if err != nil {
		// A program that produced something Go cannot write down did run, and
		// it did not produce what it promised. That is incomplete, not broken.
		return exec.RunResult{
			Incomplete: "it finished without producing what it promised.",
			Report:     rec.report,
			Spend:      rec.spend,
		}, nil
	}

	result := exec.RunResult{Output: output, Report: rec.report, Spend: rec.spend}
	if missing := missingOutput(r.bundle.Manifest.Output, output); len(missing) > 0 {
		result.Output = nil
		result.Incomplete = fmt.Sprintf("it finished without %s.", spellList(missing))
	}
	return result, nil
}

// stopped reads what the interpreter came back with and turns it into one of the
// two endings that are not "it finished".
//
// AN UNCAUGHT ERROR IS A FALLBACK AND NOT A FAILED TASK. This is the JIT's own
// answer, which PRD §1 names as the frame for the whole design: compiled code
// runs behind guards, and when a guard does not hold you deoptimize to the
// interpreter rather than aborting the program. The work still gets done, by the
// general worker, with the original input — the caller does that, not this file.
func (r *Runner) stopped(err error, rec *recorder) (exec.RunResult, bool) {
	if err == nil {
		return exec.RunResult{}, false
	}
	var interrupted *goja.InterruptedError
	if errors.As(err, &interrupted) {
		why := "it stopped before it finished."
		if stop, ok := interrupted.Value().(halt); ok {
			why = stop.why
		}
		return exec.RunResult{Incomplete: why, Report: rec.report, Spend: rec.spend}, true
	}
	fellBack := "it stopped partway through — " + brief(err)
	rec.deopt(fellBack)
	return exec.RunResult{FellBack: fellBack, Report: rec.report, Spend: rec.spend}, true
}

// brief is the one line of an interpreter error a person is shown. goja's
// message already carries the file, the line and the column; what it also
// carries is a whole stack trace, and a stack trace is a thing to put in a
// journal entry rather than in a sentence.
func brief(err error) string {
	line := strings.TrimSpace(err.Error())
	if cut := strings.IndexByte(line, '\n'); cut >= 0 {
		line = strings.TrimSpace(line[:cut])
	}
	return line
}

// marshalOutput turns what the program returned into the typed answer. A value
// that is not there at all — no return, `undefined`, `null` — is a run that did
// not produce its shape, and the error says so by being one.
func marshalOutput(value goja.Value) (json.RawMessage, error) {
	if value == nil || goja.IsUndefined(value) || goja.IsNull(value) {
		return nil, errors.New("nothing was produced")
	}
	encoded, err := json.Marshal(value.Export())
	if err != nil {
		return nil, err
	}
	if len(encoded) == 0 || string(encoded) == "null" {
		return nil, errors.New("nothing was produced")
	}
	return encoded, nil
}

// missingOutput names the promised fields the answer does not have.
//
// IT IS THE SHALLOW CHECK AND THAT IS DELIBERATE. [exec.Schema.Fields] is what
// the intake card draws a form from, and reading the same shallow view here is
// what keeps one account of what a schema says. Validating a document against a
// whole JSON Schema is a different job, and building a second, deeper model of
// schemas in this package would give the wave two answers to "what does this
// subharness promise".
func missingOutput(schema exec.Schema, output json.RawMessage) []string {
	if schema.Empty() {
		return nil
	}
	var produced map[string]json.RawMessage
	if err := json.Unmarshal(output, &produced); err != nil {
		// The promise names fields and this answer has none — not an object at
		// all — so every required field is missing.
		var names []string
		for _, field := range schema.Fields() {
			if field.Required {
				names = append(names, field.Name)
			}
		}
		return names
	}
	var missing []string
	for _, field := range schema.Fields() {
		if !field.Required {
			continue
		}
		value, present := produced[field.Name]
		if !present || len(value) == 0 || string(value) == "null" {
			missing = append(missing, field.Name)
		}
	}
	return missing
}

// spellList writes a list of names the way a sentence does.
func spellList(names []string) string {
	quoted := make([]string, 0, len(names))
	for _, name := range names {
		quoted = append(quoted, "`"+name+"`")
	}
	switch len(quoted) {
	case 0:
		return ""
	case 1:
		return quoted[0]
	case 2:
		return quoted[0] + " and " + quoted[1]
	default:
		return strings.Join(quoted[:len(quoted)-1], ", ") + " and " + quoted[len(quoted)-1]
	}
}

// spellDuration writes a length of time the way a person says one. It exists
// because "15m0s" is a machine's spelling and the sentence it lands in is read
// by somebody who wants to know how long their run had.
func spellDuration(d time.Duration) string {
	switch {
	case d >= time.Hour:
		hours := int(d.Round(time.Minute) / time.Hour)
		return plural(hours, "hour")
	case d >= time.Minute:
		return plural(int(d.Round(time.Second)/time.Minute), "minute")
	case d >= time.Second:
		return plural(int(d.Round(time.Second)/time.Second), "second")
	default:
		return "under a second"
	}
}

func plural(count int, unit string) string {
	if count == 1 {
		return "1 " + unit
	}
	return fmt.Sprintf("%d %ss", count, unit)
}
