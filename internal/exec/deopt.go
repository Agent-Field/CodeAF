package exec

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

// THE DEOPTIMIZATION: what happens when a program could not do the job.
//
// A subharness is a bet — that this shape of work is regular enough to be a
// function. The bet loses sometimes: a guard says the material is not what the
// program expects, a step cannot produce its declared shape, a bundle breaks
// halfway. What must never happen when it loses is that the person's work stops,
// because they did not ask for a program, they asked for the work.
//
// So the losing bet falls back to THE LONG WAY: the generalist takes the same
// input the program was given and does the job the way it would have been done
// if no subharness had ever existed. That is why [Registry.Generalist] is
// exposed by name — the fallback has to be the worker the person would otherwise
// have had, or "handled it the long way" is a sentence about something else.
//
// IT LIVES HERE BECAUSE BOTH SURFACES DO IT. The task-node door
// (internal/session) and the headless command (cmd/aforge) reach exactly this
// function, so the rule for when to fall back, the worker it falls back to, and
// the sentence a person reads are written once. Two spellings of a fallback are
// two programs disagreeing about what happened.
//
// THE VOCABULARY LAW REACHES THE SENTENCE. Nothing here calls a deopt a failure,
// a degradation or an error, because from the person's side it is none of those:
// the step needed a closer look and it was handled the long way. [DeoptLine] is
// that sentence and it is the only one.

// DeoptWord is the person-facing account of a run that was handled the long way.
// It is a constant because a surface drawing it and a journal recording it have
// to be quoting one sentence.
const DeoptWord = "needed a closer look — handled it the long way"

// DeoptLine is [DeoptWord] with the program's own reason after it, where the
// program gave one. A guard's Because line is written for a person to read, so
// it is carried through rather than summarized; a run that said nothing extra
// draws the bare word, never an empty colon (the emptiness law).
func DeoptLine(because string) string {
	if because = strings.TrimSpace(because); because == "" {
		return DeoptWord
	}
	return DeoptWord + ": " + because
}

// FellBack reports that this result is asking to be handled the long way. It is
// asked in one place so that no surface invents a second reading of it, and it
// is the same shape [RunResult.Finished] is: a question about the result rather
// than a field every caller re-tests.
func FellBack(result RunResult) bool { return strings.TrimSpace(result.FellBack) != "" }

// Deopt does the job the long way and answers what the generalist produced.
//
// THE INPUT IS THE ORIGINAL INPUT, unchanged. A fallback that reshaped what it
// was given would be a third worker nobody registered — and the whole promise of
// the long way is that it is the job as it stood before any program touched it.
//
// The result carries the fell-back sentence forward, so a run that was handled
// this way says so wherever it is read afterwards, whatever the generalist made
// of it. A generalist that itself could not be reached is an error rather than a
// silent nothing: there is no fourth worker under this one.
func Deopt(ctx context.Context, registry *Registry, input json.RawMessage, env Env, because string) (RunResult, error) {
	if registry == nil {
		return RunResult{}, errors.New("there is no worker here to hand this to")
	}
	runner, err := registry.Subharness(LinearSubharness)
	if err != nil {
		// The registry may have been built without the baseline fronted as a
		// runner — a surface that registered specialists and nothing else. The
		// executor underneath it is the same worker either way, so it is fronted
		// here rather than refused.
		general := registry.Generalist()
		if general == nil {
			return RunResult{}, errors.New("there is no worker here to hand this to")
		}
		runner, err = FrontExecutor(general, linearManifest)
		if err != nil {
			return RunResult{}, err
		}
	}
	result, err := runner.Run(ctx, input, env)
	if err != nil {
		return RunResult{}, err
	}
	result.FellBack = DeoptLine(because)
	return result, nil
}
