package jsrun

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/exec"
)

// THE BUNDLE, as this runtime needs it — and no more than that.
//
// PRD §6 describes a directory on disk: manifest.json, program.js, prompts/*.md,
// memory.md, evals/. That directory is the STORE LANE'S business, and this
// struct is deliberately not a picture of it. It is the four things a run
// actually reads plus the two doors that let a run look at the world without
// this package owning a filesystem: what to run, what it is allowed to do, the
// prompt assets its ai() calls may name, and where its own memory lives.
//
// KEEPING IT SMALL IS THE POINT. The store lane constructs one of these; a field
// here is a thing the store has to be able to fill, so every field that is not
// load-bearing for a run is a coupling nobody asked for. `evals/` is absent
// because nothing executes evals yet, and a slot for it here would be
// half-built machinery pretending to work.
type Bundle struct {
	// Manifest is everything true before the run: identity, cost shape, the
	// whitelist tool() filters through, the guards checked on the way in, and
	// the output shape a finished run has to produce.
	Manifest exec.Manifest

	// Program is the source of program.js, whole. It is compiled once by [New],
	// so a bundle that will not compile is refused at load rather than at run —
	// which is what makes a syntax error an AUTHORING error with a line number
	// rather than a run that mysteriously fell back.
	Program string

	// Source is the name errors are reported against. Empty is "program.js",
	// which is what the file is actually called; a store that keeps several
	// files in one bundle names the one it handed over.
	Source string

	// Prompts is the prompt assets by name, the map ai()'s first argument
	// selects from. The key is the bare name — `summarise` for
	// `prompts/summarise.md` — and [Runner] normalizes a `prompts/` prefix and a
	// `.md` suffix on the way in, so a program that names the file the way an
	// author thinks of it still resolves.
	//
	// A NAME THAT IS NOT IN HERE IS REFUSED, which is the whole enforcement of
	// the no-inline-prompts law (PRD §5, §12): prompts are files so they can be
	// diffed, reviewed and revised, and a program that could pass its prompt as a
	// string would put the one thing worth improving somewhere nothing can
	// improve it.
	Prompts map[string]string

	// Memory is this subharness's OWN memory — its memory.md in its own bundle,
	// not the session's. Nil is a bundle whose memory the store has not opened,
	// and then remember() and recall() fall through to the [exec.Env]'s own
	// doors, which is the honest fallback rather than a silent no-op.
	Memory Memory

	// Look is the free way a guard checks the world before a run: does this file
	// exist, is this tool on the belt. It is a door rather than a filesystem
	// because this package has none — goja has no filesystem unless the host
	// hands one in, and a runtime that quietly grew one for its own guards would
	// have handed itself the thing it refuses the program.
	//
	// NIL MEANS THE WORLD CANNOT BE LOOKED AT, and a file or tool guard that
	// cannot be checked DOES NOT PASS. The safe answer to "I could not tell" is
	// the long way, not the fast path.
	Look Look

	// Fuel is what this run may spend before it stops. The zero value takes the
	// manifest's own cost shape and this package's defaults; see [Fuel].
	Fuel Fuel
}

// Memory is a subharness's own accumulated domain notes — the memory.md in its
// bundle, opened by the store lane. The two methods are exactly the two doors
// [exec.Env] spells, so a store that already implements the Env halves
// implements this by having them.
type Memory interface {
	Remember(ctx context.Context, note string) error
	Recall(ctx context.Context, query string) ([]exec.Note, error)
}

// Look answers the two cheap questions a guard asks about the world. Both are
// free — no model call, no tool call, no spend — which is what makes a guard a
// guard rather than the first step of the run.
type Look interface {
	// FileExists says whether the path a [exec.GuardFile] names is there.
	FileExists(path string) bool
	// ToolOnBelt says whether the tool a [exec.GuardTool] names is actually
	// available to this session. It is a different question from the whitelist:
	// the whitelist is a ceiling the manifest declares, and the belt is what the
	// session really has.
	ToolOnBelt(name string) bool
}

// Prompted is an [exec.Env] that wants this bundle's prompt assets.
//
// IT EXISTS BECAUSE THE CONTRACT PASSES A REF AND THE MODEL NEEDS TEXT.
// [exec.Env.AI] takes a promptRef by design — the ref is what gets journaled,
// what a later revision is argued about, and what a per-call-site cache will one
// day be keyed on — but the Env that makes the actual model call has no bundle
// to resolve the ref against. So a run hands its prompts to an Env that asks for
// them, once, before the first call. An Env that does not implement this is
// handed nothing and resolves refs its own way; nothing about the contract
// changes either way.
type Prompted interface {
	UsePrompts(prompts map[string]string)
}

// Fuel is what one run may spend before it stops. All three are ceilings, all
// three end the same way — [exec.RunResult.Incomplete] with a sentence saying
// what ran out — and none of them is ever a hang.
//
// THE OPERATION BUDGET IS COUNTED IN HOST CALLS AND NOT IN VM INSTRUCTIONS, and
// that is a deviation from PRD §5's wording made deliberately rather than
// quietly. goja exposes interruption (any goroutine may call
// [goja.Runtime.Interrupt]) but no per-instruction hook, so nothing outside the
// interpreter can count instructions without rewriting the program's source. A
// host call is the operation this runtime can count exactly and the only one it
// can count for free — and the runaway loop that an instruction budget was
// wanted for is caught by [Fuel.Wall], which interrupts the VM mid-loop no
// matter what the loop is doing.
type Fuel struct {
	// Steps is how many host calls the program may make. Zero takes
	// [DefaultSteps].
	Steps int

	// Tokens is the ceiling on what the run's own ledger reports — input plus
	// output, summed across every ai() and every tool() as the journal records
	// them. Zero is NO token ceiling, which is the honest zero value: a caller
	// that has not been given a grant must not have one invented for it.
	Tokens int

	// Wall is the deadline. Zero takes the manifest's own cost shape —
	// [exec.SubharnessInfo.Deadline] of [Fuel.Tokens] — which is the one
	// spelling of a budget the whole wave uses and the reason there is no second
	// deadline constant in this package.
	Wall time.Duration
}

// DefaultSteps is how many host calls a run gets when nobody said. It is
// generous on purpose: the ceiling is a backstop against a loop that calls out
// forever, not a design constraint an author should be shaping a program around.
const DefaultSteps = 500

// steps and wall read the fuel with its defaults applied, so the defaults are
// stated once and every reader interpolates them.
func (f Fuel) steps() int {
	if f.Steps > 0 {
		return f.Steps
	}
	return DefaultSteps
}

func (f Fuel) wall(info exec.SubharnessInfo) time.Duration {
	if f.Wall > 0 {
		return f.Wall
	}
	return info.Deadline(f.Tokens)
}

// Validate says whether this is a bundle at all, in the prose its author needs
// to hear. It does NOT compile the program — that is [New]'s job, and the
// compiler's own message with its own line number is a better error than
// anything this function could write.
func (b Bundle) Validate() error {
	if err := b.Manifest.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(b.Program) == "" {
		return fmt.Errorf("%q has no program in it — there is nothing here to run", b.Manifest.Name)
	}
	// The one paid check before a run is capped at load rather than at run time,
	// so a subharness that would have been too expensive to check is an
	// authoring error with a sentence in it and never a run that quietly skipped
	// something. See [judgementBudget].
	asked := 0
	for _, guard := range b.Manifest.Guards {
		if guard.Kind == exec.GuardJudgement {
			asked++
		}
	}
	if asked > judgementBudget {
		return fmt.Errorf("%q asks the model %d questions before it runs — a check happens before the work and pays for nothing, so at most %d of them may cost anything",
			b.Manifest.Name, asked, judgementBudget)
	}
	return nil
}

// source is the name the compiler reports errors against.
func (b Bundle) source() string {
	if name := strings.TrimSpace(b.Source); name != "" {
		return name
	}
	return "program.js"
}

// promptNames is every asset this bundle has, in a stable order, for the error a
// program gets when it names one that is not there. A list that reordered
// between two runs would make the same mistake read as two different mistakes.
func (b Bundle) promptNames() []string {
	names := make([]string, 0, len(b.Prompts))
	for name := range b.Prompts {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
