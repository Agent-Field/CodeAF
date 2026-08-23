package subharness

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/exec"
)

// THIS STORE, SEEN AS ONE MORE PLACE A SUBHARNESS LIVES.
//
// A program saved here is a PAGE: the shape a design writes when somebody asks
// for one to be built for them (store.go owns the layout, and it is the only
// place that spells it). A program saved one directory over is a bundle. They
// are the same thing to everybody who is not the code that loads them — one
// catalog, one namespace, one list — and this file is what makes that true
// rather than promised: the store answers [exec.BundleSource], the registry
// takes it at [exec.LayerPages], and from there a page is a row like any other.
//
// It is the shape internal/substore's own source.go already proved, seam for
// seam, and deliberately so:
//
//	registry.UseBundles(exec.LayerPages, subharness.Default().Source(run))
//
// A STORE WITH NOTHING TO RUN PAGES IS NOT A SOURCE AT ALL, which is the
// codebase's law about a capability that cannot work stated at the earliest
// possible place: handed no runner, [Store.Source] answers a nil interface, and
// [exec.Registry.UseBundles] takes a nil source as a no-op. A build that cannot
// run one offers none.
//
// AND IT HOLDS NO CACHE. Every listing walks the directory as it stands, so a
// page saved a minute ago — by this session's own design, by another window — is
// on the next list without anybody refreshing anything. That is the same posture
// [exec.BundleSource] asks for by name, and the walk is cheap: a directory of
// names and one page read per name.

// RunPage is what actually runs a page: the name, the request in the person's own
// words, the model to think with, and a look at each step as it lands. It is the
// SAME function the conversation already runs a page with (internal/session's
// Config.RunHarness), handed here rather than reimplemented, so a run started
// from a list and a run started by a turn are one run through one path.
//
// The model may be empty, and that is what every run started from a list passes:
// empty means the model the runner was built on, which is the conversation's own.
type RunPage func(ctx context.Context, name, text, model string, step func(Trail)) (string, Usage, error)

// BriefField is the one thing a page is asked for before it runs.
//
// A PAGE TAKES ONE FREE-TEXT REQUEST AND THE OLD FORM CANNOT TAKE MORE: the
// runner threads a single string through the program (exec_model.go), so a
// manifest declaring a second field would be a card asking for something nothing
// downstream could read. One required field, named for what it is, is the whole
// of the honest front door.
const BriefField = "brief"

// briefSchema is that one field written as JSON Schema. The description is what
// the intake card draws under the field name, so it is written to somebody
// sitting in front of a blank row rather than to a reader of schemas.
const briefSchema = `{
  "type": "object",
  "required": ["brief"],
  "properties": {
    "brief": {"type": "string", "description": "what this run is about, in your own words"}
  }
}`

// Source is this store as a place the registry loads programs from.
func (s *Store) Source(run RunPage) exec.BundleSource {
	if s == nil || run == nil {
		return nil
	}
	return &source{store: s, run: run}
}

// source is the registry's view of one page store.
type source struct {
	store *Store
	run   RunPage
}

// Runner loads one page's head version.
//
// IT IS ASKED, NEVER SCANNED, and not found is (nil, false) rather than an
// error: it is the next layer's turn. A page that is THERE and cannot be read is
// the same answer, because somebody who typed a name is better served by "no
// subharness by that name" than by a run that ends at its first step.
func (b *source) Runner(name string) (exec.Runner, bool) {
	page, err := b.store.Load(name, 0)
	if err != nil {
		return nil, false
	}
	return pageRunner{manifest: manifestOf(page), run: b.run}, true
}

// Manifests is every page in this store, read from the disk as it stands.
//
// A PAGE THAT CANNOT BE READ IS ABSENT FROM THE LIST rather than a row that
// refuses when it is picked: a row nothing can run is a promise the surface
// behind it cannot keep. A store that cannot be read at all answers nothing,
// which is what [exec.BundleSource] asks for — a registry is not worth failing a
// launch over, and a list is not where somebody learns their disk is gone.
func (b *source) Manifests() []exec.Manifest {
	names, err := b.store.Names()
	if err != nil {
		return nil
	}
	manifests := make([]exec.Manifest, 0, len(names))
	for _, name := range names {
		page, err := b.store.Load(name, 0)
		if err != nil {
			continue
		}
		manifests = append(manifests, manifestOf(page))
	}
	return manifests
}

// manifestOf is one page described the way every other subharness is described.
//
// WHAT IS NOT THERE IS LEFT EMPTY, which is the emptiness law reaching a
// registry. Three fields are deliberately blank:
//
//   - CUES. A page carries none — the trigger vocabulary a design writes down
//     dies with the session that wrote it (harness_build.go says so at the line
//     that drops them) — and a manifest with no cues is found by being NAMED,
//     which is already this contract's documented posture (exec's manifest.go).
//   - THE BUDGET SHAPE. A page states no floor and no slope, so nothing here
//     invents one: a row drawing "up to 15m" for a program that never made that
//     claim would be the list speaking for it.
//   - THE OUTPUT SCHEMA. A page promises no shape. What a run PRODUCES is still
//     answered ([pageRunner.Run] fills it), but a promise nobody made is not
//     written down as though they had.
//
// The provenance is left empty too, and that is not an omission: the registry
// stamps it from the layer the program was found at, which is the one rule that
// keeps a store from claiming to be something else (exec's manifest.go).
func manifestOf(page Harness) exec.Manifest {
	return exec.Manifest{
		SubharnessInfo: exec.SubharnessInfo{
			Name:    page.Id.Name,
			Purpose: strings.TrimSpace(page.Id.Desc),
		},
		Input: exec.Schema(briefSchema),
	}
}

// pageRunner is one page behind the [exec.Runner] door.
//
// IT RUNS THE PAGE THE WAY THE CONVERSATION ALREADY DOES and adds nothing: the
// walk, the trace saved beside the page, the report and the bill are all
// [RunPage]'s, which is the seam the surface built once. What this type owns is
// the translation — a typed input in, a typed answer and a ledger out — and the
// progress a person watching gets while it works.
type pageRunner struct {
	manifest exec.Manifest
	run      RunPage
}

func (p pageRunner) Manifest() exec.Manifest { return p.manifest }

// Run threads the card's one field through the program.
//
// EVERY STEP IS SAID AS IT LANDS. The walk is watched and each step is logged
// through the [exec.Env]'s own door, so the room shows the program working and
// the roster row carries the step it is on — which is what every other
// subharness run already looks like, and what a run started from a turn has
// always shown (internal/session's harness.go).
//
// WHAT COMES BACK IS THE RUN'S OWN ACCOUNT and it is carried twice on purpose,
// exactly as a fronted leaf worker's is (exec's runner.go): once as the report a
// person reads, and once as the answer, because the finish line is machine-
// checkable ([exec.RunResult.Finished]) and a run that produced nothing at all
// would otherwise be recorded as never having finished.
func (p pageRunner) Run(ctx context.Context, input json.RawMessage, env exec.Env) (exec.RunResult, error) {
	var typed struct {
		Brief string `json:"brief"`
	}
	if len(input) > 0 && string(input) != "null" {
		if err := json.Unmarshal(input, &typed); err != nil {
			return exec.RunResult{}, fmt.Errorf("this input is not what %q takes: %w", p.manifest.Name, err)
		}
	}
	brief := strings.TrimSpace(typed.Brief)
	if brief == "" {
		return exec.RunResult{}, fmt.Errorf("%q needs a %s — there is nothing here to do", p.manifest.Name, BriefField)
	}
	// The model is left to the runner's own: a run started from a list is started
	// on the conversation's model, and picking one here would be spending the
	// person's money on a choice they never made.
	report, spent, err := p.run(ctx, p.manifest.Name, brief, "", func(step Trail) {
		_ = env.Log(ctx, stepWord(step))
	})
	if err != nil {
		return exec.RunResult{}, err
	}
	result := exec.RunResult{
		Report: strings.TrimSpace(report),
		Spend: exec.Spend{
			Model:      spent.Model,
			Calls:      spent.Calls,
			Input:      spent.Input,
			Output:     spent.Output,
			CacheRead:  spent.CacheRead,
			CacheWrite: spent.CacheWrite,
			CostUSD:    spent.CostUSD,
		},
	}
	if result.Report == "" {
		result.Incomplete = "it finished without producing anything"
		return result, nil
	}
	answer, err := json.Marshal(struct {
		Result string `json:"result"`
	}{Result: result.Report})
	if err != nil {
		return exec.RunResult{}, err
	}
	result.Output = answer
	return result, nil
}

// stepWord is one step as the row and the room show it: the node's own id, and
// what it said when it said something. The id is the program's own word for this
// piece of work, which is the vocabulary a person reading the page would use for
// it — nothing here renames a step into machinery.
func stepWord(step Trail) string {
	word := strings.TrimSpace(step.Id)
	if problem := strings.TrimSpace(step.Err); problem != "" {
		if word == "" {
			return problem
		}
		return word + " · " + problem
	}
	return word
}
