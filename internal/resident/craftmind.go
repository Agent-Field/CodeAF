// Recognition is the half of craft that makes the shelf worth having. A
// workflow nobody invokes is a file, not a skill: the user asks for a deck in
// their own words, never by the name of a workflow they have not read, so the
// request has to find the craft on its own or every version the distiller
// writes is one nobody asks for again.
//
// The check sits in front of the PLANNER and nowhere else. Head routing still
// decides what kind of thing an instruction is, the compiler still reads it
// into a goal, and only then — where a goal would have become a planned graph
// — does a decisive match compile the craft's subtree instead. That placement
// is what keeps craft an optimization rather than a gate: everything that is
// not decisively answered by a stored workflow costs one local BM25 read and
// then plans exactly as it always did.
package resident

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"unicode"

	"github.com/Agent-Field/aforge-v2/internal/craft"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

const (
	// CraftDecisiveScore is how far past the retrieval floor a match has to sit
	// before the resident runs learned know-how instead of planning. The floor
	// is one shared word; a craft's own name counts four times a word in a step
	// brief, so a request that says what the craft is FOR clears this line
	// while one that merely brushes a step does not. The bar also gets safer as
	// the shelf grows: the more workflows there are, the rarer a name term is,
	// and the further a true match sits above it.
	CraftDecisiveScore = 1.5 * craft.MatchFloor
	// CraftOverwhelmingScore is what a craft with no survival record has to
	// clear to be used unasked. A freshly forged workflow is a draft: nothing
	// has run it, so the only evidence it works is that the distiller believed
	// it. Below this line a draft waits to be named — by the user, or by the
	// arrival brief that says it now exists.
	CraftOverwhelmingScore = 4 * craft.MatchFloor
	// craftMatches is how many candidates recognition asks for. One: the
	// resident either already has the answer or it plans, and a menu of
	// workflows is precisely the choice this loop exists to spare the user.
	craftMatches = 1
)

// CraftShelf is the craft repository as the resident uses it: what can be
// found, what can be read, what its versions say, and what can be written.
// *craft.Repo is the only implementation; tests script it.
type CraftShelf interface {
	Match(request string, k int) []craft.Scored
	Load(name string) (*craft.Workflow, error)
	List() ([]craft.Summary, error)
	History(name string, limit int) ([]craft.Version, error)
	Save(workflow *craft.Workflow, message string) (string, error)
}

// CraftParamFiller reads one workflow's declared holes out of the request that
// matched it. It is a model seam because the values are in the user's prose —
// "a deck on the Q3 numbers, keep it short" carries a topic and a tone that no
// pattern would agree on. A filler that cannot answer leaves the map empty and
// the ordinary planner takes the request.
type CraftParamFiller func(ctx context.Context, instruction string, workflow *craft.Workflow) (map[string]string, error)

// CraftMind is craft's thinking half: the shelf it reads and writes, the
// directory verifier paths resolve against, and the two model seams it borrows
// — one to read a request's parameters, one to repair a file the parser
// refused. Nil is the whole disabled state: without a shelf nothing here runs
// and the resident plans as it always has.
type CraftMind struct {
	shelf  CraftShelf
	dir    string
	fill   CraftParamFiller
	repair CraftRepairFunc
}

// NewCraftMind builds the recognizer and forge over one craft repository. dir
// makes verifier scripts absolute, exactly as the runner's does.
func NewCraftMind(shelf CraftShelf, dir string, fill CraftParamFiller, repair CraftRepairFunc) *CraftMind {
	if shelf == nil {
		return nil
	}
	return &CraftMind{shelf: shelf, dir: strings.TrimSpace(dir), fill: fill, repair: repair}
}

// WithCraftMind installs recognition and forging. Without it the craft
// repository is still run by name, and still swept — it is simply never
// reached for on its own.
func (r *Reconciler) WithCraftMind(mind *CraftMind) *Reconciler {
	r.craftMind = mind
	return r
}

// craftUse is one recognized craft, already compiled into the admission shape
// the planner would otherwise have produced.
type craftUse struct {
	subtree store.Subtree
	// reference is name@commit — the version every node of this run names, and
	// the key its outcome is measured under.
	reference string
	receipt   string
}

// craftCompile answers the one question the splice path asks: is this request
// something we already know how to do? A false is silent by construction —
// every miss is an ordinary plan, and the user is never asked to confirm a
// craft they did not bring up.
func (r *Reconciler) craftCompile(ctx context.Context, command store.Command) (craftUse, bool) {
	// The command sequence is the id namespace for the same reason task-<seq>
	// is: it is unique, it is derivable from the journal, and a retried splice
	// lands on the nodes it already made instead of beside them.
	return r.craftFor(ctx, command.Instruction, fmt.Sprintf("craft-%d", command.Seq), store.Provenance{
		Origin:    store.OriginUser,
		SessionID: command.SessionID,
		Intent:    command.Instruction,
	})
}

// craftFor is the recognition itself, separated from where the request came
// from. A standing watch firing is the case a learned workflow exists for —
// the same shape of work, over and over, on a schedule — and it was the one
// path that could not reach the shelf: admitCharterFiring compiled and planned
// directly, so the recurring overnight job planned itself from scratch every
// morning while the craft distilled from it sat unread.
//
// The caller supplies the id namespace and the provenance, because those are
// the only two things a firing and a chat splice genuinely differ on.
func (r *Reconciler) craftFor(ctx context.Context, request, rootID string, provenance store.Provenance) (craftUse, bool) {
	mind := r.craftMind
	if mind == nil || mind.shelf == nil {
		return craftUse{}, false
	}
	instruction := strings.TrimSpace(request)
	if instruction == "" || craftDeclined(instruction) {
		return craftUse{}, false
	}
	// The user's own words, not the compiled goal. The goal is the compiler's
	// paraphrase, and a paraphrase reaches for the same generic vocabulary
	// every workflow's description is written in — "report", "week", "file" —
	// which lifts unrelated crafts over the bar far faster than it lifts the
	// right one.
	matches := mind.shelf.Match(instruction, craftMatches)
	if len(matches) == 0 || matches[0].Score < CraftDecisiveScore {
		return craftUse{}, false
	}
	workflow, err := mind.shelf.Load(matches[0].Name)
	if err != nil || workflow == nil {
		return craftUse{}, false
	}
	if !r.craftProven(workflow.Name) &&
		matches[0].Score < CraftOverwhelmingScore && !craftNamedOutright(instruction, workflow.Name) {
		return craftUse{}, false
	}
	params, ok := r.craftParams(ctx, instruction, workflow)
	if !ok {
		return craftUse{}, false
	}

	provenance.Craft = CraftRef(workflow)
	subtree, err := CompileCraftAs(rootID, mind.dir, workflow, params, provenance)
	if err != nil {
		return craftUse{}, false
	}
	return craftUse{subtree: subtree, reference: provenance.Craft, receipt: craftUseReceipt(workflow)}, true
}

// craftParams fills the workflow's holes from the request. A missing required
// param is a miss, not a question: craft is an optimization, and stopping to
// interrogate the user about a workflow they never mentioned would cost more
// than the planning it saves.
func (r *Reconciler) craftParams(ctx context.Context, instruction string, workflow *craft.Workflow) (map[string]string, bool) {
	extracted := map[string]string{}
	if len(workflow.Params) > 0 && r.craftMind.fill != nil {
		if values, err := r.craftMind.fill(ctx, instruction, workflow); err == nil {
			extracted = values
		}
	}
	filled, err := workflow.Fill(extracted)
	if err != nil {
		return nil, false
	}
	return filled, true
}

// craftUseReceipt names what is about to run, which version of it, and what it
// may spend. The version is the load-bearing half: a craft is refined across
// commits, and a receipt that named only the craft would say the same thing
// about two runs that behaved differently.
func craftUseReceipt(workflow *craft.Workflow) string {
	return fmt.Sprintf("%s, ~$%.2f cap", craftCompileReceipt(workflow), craftCostCeiling(workflow.Limits))
}

// craftDeclineWords are how the user says "don't reach for what you already
// know". One guard rather than a cue system: any of these anywhere in the
// instruction skips craft matching entirely, and every other phrasing still
// reaches the planner on its own whenever the match is not decisive.
var craftDeclineWords = []string{
	" from scratch ", " fresh ", " freshly ", " afresh ",
	" no craft ", " without the craft ", " don t use the craft ", " do not use the craft ",
}

func craftDeclined(instruction string) bool {
	padded := craftWords(instruction)
	for _, phrase := range craftDeclineWords {
		if strings.Contains(padded, phrase) {
			return true
		}
	}
	return false
}

// craftNamedOutright is the user asking for a craft by name — "use the
// presentation craft". The word "craft" is required beside the name because a
// draft has no evidence behind it yet, and merely saying "presentation" is
// asking for a deck, not for the untried file that makes one.
func craftNamedOutright(instruction, name string) bool {
	padded := craftWords(instruction)
	if !strings.Contains(padded, " craft ") {
		return false
	}
	for _, word := range strings.FieldsFunc(strings.ToLower(name), func(r rune) bool {
		return r == '-' || r == '_'
	}) {
		if !strings.Contains(padded, " "+word+" ") {
			return false
		}
	}
	return true
}

// craftWords lowercases an instruction into space-delimited words padded at
// both ends, so a phrase test is a word test: "refresh the cache" does not
// contain " fresh ".
func craftWords(text string) string {
	lowered := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			return unicode.ToLower(r)
		}
		return ' '
	}, text)
	return " " + strings.Join(strings.Fields(lowered), " ") + " "
}

// CraftSurvival is one craft's record: the runs that settled, and the runs
// that failed or were cancelled. It rides as the measured value of a trait,
// which is where this system's second-order facts about itself already live —
// no new table, and no per-run bookkeeping in the notebook the user reads.
type CraftSurvival struct {
	For     int `json:"for"`
	Against int `json:"against"`
}

// CraftSurvivalKey is the trait name one craft's record is kept under. Both a
// version reference (name@commit) and a bare name are legal keys, and they are
// different records on purpose.
func CraftSurvivalKey(reference string) string {
	return "craft:" + strings.TrimSpace(reference)
}

// recordCraftOutcome measures one craft run the way channel credibility
// measures a belief: the outcome is the evidence, written where the next
// decision will read it. A settled job counts for the version that ran, a
// failed or cancelled one against it.
func (r *Reconciler) recordCraftOutcome(node store.Node, settled bool) {
	reference := strings.TrimSpace(node.Provenance.Craft)
	if r == nil || r.store == nil || reference == "" || node.Parent != store.RootID {
		return
	}
	name := reference
	if cut := strings.LastIndex(reference, "@"); cut > 0 {
		name = reference[:cut]
	}
	// Two keys, because they answer two questions. The version key measures the
	// file that actually ran, which is the only fair unit for a workflow that
	// gets refined. The bare name carries whether this craft has ever settled
	// anything at all — without it, every refinement would go back to being a
	// draft nobody may use, and a craft would be punished for improving.
	for _, key := range []string{reference, name} {
		record := r.craftSurvival(key)
		if settled {
			record.For++
		} else {
			record.Against++
		}
		if _, err := r.store.RecordTrait(CraftSurvivalKey(key), store.TraitMeasurement{
			Value: record, N: record.For + record.Against, Updated: r.now(),
		}); err != nil {
			// A dropped write here is not cosmetic: the bare-name record is what
			// says this craft has ever settled anything, so losing the first one
			// leaves a working workflow permanently a draft nobody may reach for.
			// There is nothing to retry against a store that refused, but the
			// loss belongs in the log rather than nowhere.
			log.Printf("craft survival %s: %v", key, err)
		}
	}
}

// craftSurvival reads one key's record. A trait's value is stored as whatever
// was measured, so it comes back through JSON rather than as a Go value.
func (r *Reconciler) craftSurvival(key string) CraftSurvival {
	if r == nil || r.store == nil {
		return CraftSurvival{}
	}
	measurement, _, found, err := r.store.Trait(CraftSurvivalKey(key))
	if err != nil || !found {
		return CraftSurvival{}
	}
	encoded, err := json.Marshal(measurement.Value)
	if err != nil {
		return CraftSurvival{}
	}
	var record CraftSurvival
	if err := json.Unmarshal(encoded, &record); err != nil {
		return CraftSurvival{}
	}
	return record
}

// craftProven reports whether any version of this craft has ever carried a job
// to a clean landing. Everything else is a draft.
func (r *Reconciler) craftProven(name string) bool {
	return r.craftSurvival(name).For > 0
}
