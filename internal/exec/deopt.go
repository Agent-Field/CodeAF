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

// DeoptHeldWord is what a person reads when the long way would go further than
// the program they said yes to was allowed to go, so it was not taken. It is a
// constant for [DeoptWord]'s reason, and it obeys the same vocabulary law:
// nothing here calls this a failure, a refusal or a policy — the step needed a
// closer look, the long way would reach past what was agreed, and the work
// stops where it is so somebody can say what they want done.
const DeoptHeldWord = "needed a closer look, and the long way would reach further than this program was allowed to — so nothing else was tried"

// DeoptHeldLine is [DeoptHeldWord] with the program's own reason after it, in
// [DeoptLine]'s shape and for its reason.
func DeoptHeldLine(because string) string {
	if because = strings.TrimSpace(because); because == "" {
		return DeoptHeldWord
	}
	return DeoptHeldWord + ": " + because
}

// deoptShellNames are the two spellings of THE ONE HAND THE LONG WAY CANNOT DO
// WITHOUT. The generalist's belt is a shell, the filesystem and the web, and the
// shell is the whole of it: anything write, edit and web can do, a command can
// do too. So a ceiling that already reaches a shell already reaches everything
// the fallback is going to use, and one that does not is a ceiling the fallback
// would climb over.
//
// Two spellings because two belts use two: a conversation's belt calls it `bash`
// (internal/exec/bare, the wire tool library) and a leaf's own belt calls it
// `sh` ([Toolbox]). A manifest may honestly have been written against either.
var deoptShellNames = []string{"bash", "sh"}

// DeoptHeld reports that this program's ceiling does not reach the long way, so
// the long way must not be taken.
//
// THE CEILING A PERSON APPROVED SURVIVES THE FALLBACK, AND THIS IS THE LINE
// THAT MAKES IT TRUE. [Manifest.Whitelist] is what the approval card showed
// somebody this program may reach — "can use · nothing" for the empty one
// (internal/subharness's cardFoot) — and every call the program makes during its
// run is checked against it. The generalist underneath the fallback has no such
// ceiling and cannot be given one: its five tools are unconditional and sit in a
// fixed order by law ([Toolbox.Definitions] states why the order is load-bearing
// and why the five never move), so there is no seam to filter them through.
//
// Without this, a subharness somebody approved to touch nothing became an
// unrestricted shell agent in their workspace the moment a guard did not pass —
// silently, with no second question, and with nothing on any surface saying the
// ceiling had come off. NEVER SILENTLY WIDEN WHAT SOMEBODY AGREED TO. Where the
// ceiling does not reach a shell the honest answer is that the work stops
// incomplete and says so, and the person decides what happens next.
func DeoptHeld(manifest Manifest) bool {
	for _, allowed := range manifest.Whitelist {
		for _, shell := range deoptShellNames {
			if strings.EqualFold(strings.TrimSpace(allowed), shell) {
				return false
			}
		}
	}
	return true
}

// DeoptWordFor and DeoptLineFor are which of the two sentences this program's
// fallback is going to produce, asked BEFORE it runs. A surface announces what
// is about to happen, and one that said "handled it the long way" over a run
// that was about to stop would be telling somebody the opposite of the truth.
func DeoptWordFor(manifest Manifest) string {
	if DeoptHeld(manifest) {
		return DeoptHeldWord
	}
	return DeoptWord
}

// DeoptLineFor is [DeoptWordFor] with the program's own reason after it.
func DeoptLineFor(manifest Manifest, because string) string {
	if DeoptHeld(manifest) {
		return DeoptHeldLine(because)
	}
	return DeoptLine(because)
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
//
// THE MANIFEST IS THE PROGRAM'S OWN, NOT THE GENERALIST'S, and it is here for
// one reason: it carries the ceiling somebody approved. [DeoptHeld] states the
// whole argument. A run whose ceiling does not reach the long way stops
// incomplete, in the person's own register, rather than quietly becoming a
// wider agent than anybody said yes to — and that ending is produced HERE, in
// the one function both surfaces reach, so neither can skip it.
func Deopt(ctx context.Context, registry *Registry, manifest Manifest, input json.RawMessage, env Env, because string) (RunResult, error) {
	if registry == nil {
		return RunResult{}, errors.New("there is no worker here to hand this to")
	}
	if DeoptHeld(manifest) {
		// Incomplete and never an error: the work did not happen, which is one of
		// the five sanctioned words for the state of work, and an error here would
		// read as a program that broke rather than a bound that held.
		return RunResult{Incomplete: DeoptHeldLine(because)}, nil
	}
	runner, err := registry.Subharness(LinearSubharness)
	if err != nil {
		// The registry may have been built without the worker fronted as a
		// runner — a surface that registered saved programs and nothing else.
		// The executor underneath it is the same worker either way, so it is
		// fronted here rather than refused.
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
