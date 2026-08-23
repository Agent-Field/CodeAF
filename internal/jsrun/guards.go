package jsrun

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/provider"
)

// GUARDS AND DEOPTIMIZATION — the JIT's answer, which is PRD §1's frame for the
// whole design.
//
// Compiled code runs behind guards, and a guard that does not hold deoptimizes
// to the interpreter rather than aborting the program. Here the compiled code is
// the bundle and the interpreter is the general worker: a check that does not
// pass does NOT fail the task, it comes back as [exec.RunResult] with FellBack
// set, and the caller does the work the long way with the original input.
//
// A GUARD IS FREE OR NEARLY SO, which is what makes it a guard and not the first
// step of the run. Three kinds are decided by looking — a file, a tool, a field
// — and are checked FIRST no matter what order the manifest lists them in,
// because the fourth kind costs a model call and there is no reason to pay for
// it before finding out whether something free has already settled the question.
//
// NOTHING HERE MAY BE SPELLED "GUARD" WHERE A PERSON READS IT. The vocabulary
// law is explicit about this case: "needed a closer look", never "guard failed".
// [exec.Guard.Because] exists so the sentence was written by somebody who knew
// what the check was for, and every fallback below prefers it over anything this
// file could compose.

// judgementBudget is how many questions a subharness may ask the model before it
// runs. PRD §5 allows "at most one tiny model call" among the guards, and it is
// enforced at load ([Bundle.Validate]) rather than at run time so that a bundle
// which would have been too expensive to check is an authoring error with a
// sentence, not a run that quietly skipped a check.
const judgementBudget = 1

// checkGuards runs the manifest's preconditions and answers with the sentence a
// person reads when one of them did not pass. An empty answer is "go".
func (r *Runner) checkGuards(ctx context.Context, input any, rec *recorder) string {
	guards := r.bundle.Manifest.Guards
	if len(guards) == 0 {
		return ""
	}
	// The free rungs first, then the one that costs. See the law above.
	for _, guard := range guards {
		if guard.Kind == exec.GuardJudgement {
			continue
		}
		if !r.holds(guard, input) {
			return closerLook(guard, r.fallbackFor(guard))
		}
	}
	for _, guard := range guards {
		if guard.Kind != exec.GuardJudgement {
			continue
		}
		if !r.judged(ctx, guard, input, rec) {
			return closerLook(guard, "it needed a closer look before it could run")
		}
	}
	return ""
}

// holds answers the three free checks.
//
// A CHECK THAT CANNOT BE MADE DOES NOT PASS. When no [Look] was wired there is
// no way to find out whether a file is there or a tool is on the belt, and the
// safe answer to "I could not tell" is the long way — a fast path taken on an
// unverified assumption is the one failure mode guards exist to prevent.
func (r *Runner) holds(guard exec.Guard, input any) bool {
	switch guard.Kind {
	case exec.GuardFile:
		return r.bundle.Look != nil && r.bundle.Look.FileExists(guard.File)
	case exec.GuardTool:
		return r.bundle.Look != nil && r.bundle.Look.ToolOnBelt(guard.Tool)
	case exec.GuardField:
		return fieldMatches(input, guard.Field, guard.Pattern)
	default:
		// A kind this build does not know is a manifest from a newer binary. It
		// cannot be checked, so it does not pass.
		return false
	}
}

// judged asks the model one small question about the input, and reads one word
// back.
//
// THE QUESTION IS THE PROMPT HERE, AND THAT IS NOT THE INLINE-PROMPT HOLE. The
// no-inline-prompts law (host.go, resolvePrompt) binds ai() inside the PROGRAM,
// where a string literal would put the one improvable thing out of reach of
// review and revision. A guard's question is a field of manifest.json: already
// a versioned, diffable, reviewable asset, already drawn on the card, and the
// only place it could possibly live.
//
// ANYTHING BUT YES IS A NO. A model that answered strangely, an Env that failed,
// a call that was refused — all of them leave the run unable to say the
// precondition held, and the long way is what you do when you cannot say.
func (r *Runner) judged(ctx context.Context, guard exec.Guard, input any, rec *recorder) bool {
	started := time.Now()
	// Effort is off rather than unset: a yes-or-no about material already in
	// hand is the shape internal/provider's own comment names as worth an order
	// of magnitude in latency, and a guard that deliberated would stop being
	// cheap enough to be a guard.
	answer, err := rec.env.AI(ctx, guard.Question, input, exec.AIOptions{Effort: provider.EffortOff})
	entry := exec.JournalEntry{
		Call:    exec.CallAI,
		Ref:     guard.Question,
		Input:   encode(input),
		Output:  encode(answer.Text),
		Spend:   answer.Spend,
		Elapsed: time.Since(started),
	}
	if err != nil {
		entry.Err = err.Error()
	}
	rec.write(entry)
	if err != nil {
		return false
	}
	return affirmative(answer.Text)
}

// affirmative reads a one-word answer. It is generous about the spelling and
// strict about everything else: a model that wrote a paragraph did not answer
// the question it was asked.
func affirmative(text string) bool {
	word := strings.ToLower(strings.TrimSpace(text))
	word = strings.Trim(word, ".!\"'`")
	switch word {
	case "y", "yes", "true", "ok":
		return true
	}
	return false
}

// fieldMatches is the [exec.GuardField] check: does the named input field
// contain the pattern.
//
// IT IS A PLAIN SUBSTRING TEST AND NOT A REGULAR EXPRESSION, which
// internal/exec/manifest.go states as the rule and gives the reason for — a
// regular expression in a manifest is a program somebody did not know they were
// writing. It ignores case, because the author of a manifest is writing a word
// they expect to see and not a pattern they have debugged.
func fieldMatches(input any, field, pattern string) bool {
	fields, ok := input.(map[string]any)
	if !ok {
		return false
	}
	value, present := fields[field]
	if !present || value == nil {
		return false
	}
	return strings.Contains(strings.ToLower(spellValue(value)), strings.ToLower(pattern))
}

// spellValue writes one input field down as the text a substring test looks at.
// A string is itself; anything else is its JSON, so a check against a list or a
// nested object still has something to match against.
func spellValue(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprint(value)
	}
	return string(raw)
}

// closerLook composes the sentence a person reads, preferring the author's own
// account of what the check was for.
func closerLook(guard exec.Guard, fallback string) string {
	if because := strings.TrimSpace(guard.Because); because != "" {
		return "it needed a closer look — " + because
	}
	return fallback
}

// fallbackFor is what to say when the manifest did not say. It names the fact
// rather than the machinery: the file that is not there, the tool that is not on
// the belt, the field that does not look right.
func (r *Runner) fallbackFor(guard exec.Guard) string {
	switch guard.Kind {
	case exec.GuardFile:
		return fmt.Sprintf("it needed a closer look — %s is not here", guard.File)
	case exec.GuardTool:
		return fmt.Sprintf("it needed a closer look — the %s tool is not on the belt", guard.Tool)
	case exec.GuardField:
		return fmt.Sprintf("it needed a closer look — %s does not look the way this one needs", guard.Field)
	default:
		return "it needed a closer look before it could run"
	}
}
