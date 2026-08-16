package resident

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/craft"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// The four verbs a person can aim at what the resident has LEARNED, as opposed
// to what it is doing: run this way of working, put it back to the version
// before this one, stop reaching for it, take that tool off the shelf.
//
// They arrive as ordinary journaled commands and are applied here, beside the
// charter and service verbs, for the reason every other door in this system is
// one command kind: a verb that acted from the surface and a verb that acted
// from a sentence would otherwise be two implementations of one promise, and
// the one nobody was looking at would be the one that stopped being true.
//
// Nothing here deletes anything. A retired craft keeps its file, its versions
// and everything it proved; a reverted one gains a version rather than losing
// one; a retired tool keeps its installed directory as the evidence of where it
// came from. Retirement is the resident being told to stop offering something,
// which is a different act from destroying it, and only one of the two is
// reversible by hand.

// craftReverter is the repository's own revert, asked for as a capability
// rather than added to [CraftShelf]. The shelf interface is what recognition
// and forging need, and every test in this package scripts it; widening it for
// one verb would make every one of those fakes carry a method they never call.
// *craft.Repo satisfies this; a shelf that does not simply cannot revert, and
// says so in words.
type craftReverter interface {
	Revert(name, toCommit, message string) (string, error)
}

func (r *Reconciler) applyCraftCommand(ctx context.Context, command store.Command) (commandOutcome, error) {
	name := strings.TrimSpace(command.Target)
	if name == "" {
		return craftRefusal("that did not name a way of working"), nil
	}
	if r.craftMind == nil || r.craftMind.shelf == nil {
		return craftRefusal("nothing in this window keeps the ways I have learned to work, so there is nothing to " +
			craftVerbWord(command.Kind)), nil
	}
	switch command.Kind {
	case store.CommandCraftRun:
		return r.runNamedCraft(ctx, command, name)
	case store.CommandCraftRevert:
		return r.revertCraft(command, name)
	case store.CommandCraftRetire:
		return r.retireNamedCraft(command, name)
	}
	return commandOutcome{}, fmt.Errorf("craft command %q is not one of run, revert or retire", command.Kind)
}

// runNamedCraft compiles a workflow the person asked for BY NAME and admits it
// as ordinary work. It is the recognition path with the matcher taken out — the
// same load, the same parameter seam, the same compiler — because the only
// thing a named run knows that recognition does not is which workflow it is.
func (r *Reconciler) runNamedCraft(ctx context.Context, command store.Command, name string) (commandOutcome, error) {
	// The shelf is a git-backed index read from several goroutines; the
	// recognition path takes this same lock for the same reason.
	r.craftMu.Lock()
	defer r.craftMu.Unlock()

	if retired := r.craftRetirement(name); retired != "" {
		return craftRefusal(fmt.Sprintf("%s is retired — you asked me to stop working this way (%s)",
			name, retired)), nil
	}
	workflow, err := r.craftMind.shelf.Load(name)
	if err != nil || workflow == nil {
		return craftRefusal(fmt.Sprintf("there is no way of working called %q", name)), nil
	}
	// The user's own words are where the workflow's holes are filled from — the
	// same prose seam recognition uses. A run fired from a page carries no prose
	// at all, and that is a legitimate call: a workflow whose parameters all have
	// defaults simply runs, and one that needs something asks for it below.
	instruction := strings.TrimSpace(command.Instruction)
	provenance := store.Provenance{
		Origin:    store.OriginUser,
		SessionID: command.SessionID,
		Intent:    craftNamedIntent(workflow, instruction),
	}
	use, err := r.craftAdmit(ctx, workflow, instruction, fmt.Sprintf("craft-%d", command.Seq), provenance)
	if err != nil {
		// A missing parameter is the one thing a named run cannot invent, so it
		// goes back through the door every other unanswerable gap uses: the
		// question is asked in the resident's own voice and the person's next
		// message answers it. Recognition treats the same failure as a miss and
		// plans instead — it may, because nobody asked for that workflow.
		return commandOutcome{
			status:  store.CommandRejected,
			result:  "asked the user for what " + name + " needs",
			receipt: craftParamQuestion(workflow, name, err),
			asAgent: true,
		}, nil
	}
	provenance.Craft = use.reference
	// Named by the ask, not by the workflow. Nothing on this path compiles the
	// request into a shorter reading of it, so the words themselves are the job's
	// name; the workflow's name rides in provenance and in the line below.
	TitleCraftRootFromRequest(&use.subtree, provenance.Intent)
	if err := r.store.Splice(store.RootID, use.subtree, provenance); err != nil {
		return commandOutcome{}, err
	}
	return commandOutcome{
		status:  store.CommandApplied,
		result:  fmt.Sprintf("spliced %d nodes", len(use.subtree.Nodes)),
		receipt: craftCompileReceipt(workflow),
	}, nil
}

// revertCraft puts a workflow back to the version before the one on the shelf.
// The repository writes it as a NEW commit carrying the reason, so the version
// that failed stays in the history where the next reader can see it.
func (r *Reconciler) revertCraft(command store.Command, name string) (commandOutcome, error) {
	reason := strings.TrimSpace(command.Instruction)
	if len(strings.Fields(reason)) < craft.MinReasonWords {
		return craftRefusal("say in a few words what the newer version got wrong, and I'll put it back"), nil
	}

	r.craftMu.Lock()
	defer r.craftMu.Unlock()

	reverter, ok := r.craftMind.shelf.(craftReverter)
	if !ok {
		return craftRefusal("this window cannot change how that way of working is written down"), nil
	}
	versions, err := r.craftMind.shelf.History(name, 2)
	if err != nil {
		return craftRefusal(fmt.Sprintf("the history of %s could not be read: %s", name, firstLine(err.Error()))), nil
	}
	if len(versions) < 2 {
		return craftRefusal(fmt.Sprintf("%s has only one version, so there is nothing to go back to", name)), nil
	}
	if _, err := reverter.Revert(name, versions[1].Commit, reason); err != nil {
		return craftRefusal(fmt.Sprintf("%s could not be put back: %s", name, firstLine(err.Error()))), nil
	}
	return commandOutcome{
		status:  store.CommandApplied,
		result:  "reverted " + name,
		receipt: name + " is back to the version before this one.",
	}, nil
}

// retireNamedCraft stops a way of working being reached for. The file stays;
// only the reaching stops — see [CraftSurvival.Retired] for where that is
// recorded and why it lives with the evidence rather than beside it.
func (r *Reconciler) retireNamedCraft(command store.Command, name string) (commandOutcome, error) {
	r.craftMu.Lock()
	defer r.craftMu.Unlock()

	if retired := r.craftRetirement(name); retired != "" {
		return commandOutcome{
			status: store.CommandApplied, result: "already retired " + name,
			receipt: name + " was already retired.",
		}, nil
	}
	if workflow, err := r.craftMind.shelf.Load(name); err != nil || workflow == nil {
		return craftRefusal(fmt.Sprintf("there is no way of working called %q", name)), nil
	}
	if err := r.retireCraft(name, command.Instruction); err != nil {
		return commandOutcome{}, err
	}
	return commandOutcome{
		status:  store.CommandApplied,
		result:  "retired " + name,
		receipt: "I'll stop working the " + name + " way. The file and its history stay where they are.",
	}, nil
}

// applySkillRetire takes one forged tool off the shelf: the belief goes quiet,
// which is what every retrieval path reads, and the bin link goes with it on
// the spot rather than on the next tick — a person who just retired a tool
// should not find it still on their PATH.
func (r *Reconciler) applySkillRetire(command store.Command) (commandOutcome, error) {
	seq, err := strconv.ParseInt(strings.TrimSpace(command.Target), 10, 64)
	if err != nil || seq <= 0 {
		return craftRefusal("that did not name a tool"), nil
	}
	fact, found, err := r.store.FactBySeq(seq)
	if err != nil {
		return commandOutcome{}, err
	}
	if !found || fact.Kind != store.FactSkill {
		return craftRefusal(fmt.Sprintf("there is no tool numbered %d", seq)), nil
	}
	if fact.Status != store.FactActive && fact.Status != store.FactCandidate {
		return commandOutcome{
			status: store.CommandApplied, result: "already retired",
			receipt: "That tool was already off the shelf.",
		}, nil
	}
	if err := r.store.QuarantineFact(seq, 0, store.FactOriginUser); err != nil {
		return commandOutcome{}, err
	}
	// The same sweep the forge runs: the active fact view is the truth, and this
	// makes the bin directory agree with it. The installed directory is left
	// alone on purpose — it carries the provenance of where the tool came from.
	r.syncSkillBins()
	return commandOutcome{
		status:  store.CommandApplied,
		result:  "retired skill #" + strconv.FormatInt(seq, 10),
		receipt: skillRetiredLine(fact),
	}, nil
}

func skillRetiredLine(fact store.Fact) string {
	name := strings.TrimSpace(craftBase(fact.Artifact))
	if name == "" {
		name = firstLine(fact.Body)
	}
	return clipLabel(name, 60) + " is off the shelf."
}

// craftBase is the tool's own name out of its installed path, without dragging
// path handling into a sentence.
func craftBase(artifact string) string {
	artifact = strings.TrimRight(strings.TrimSpace(artifact), "/")
	if cut := strings.LastIndex(artifact, "/"); cut >= 0 {
		return artifact[cut+1:]
	}
	return artifact
}

// craftRefusal is a rejection whose result and receipt are the same plain
// sentence. Every refusal here is something the person can act on — the wrong
// name, a missing reason, a version that does not exist — so the words that go
// in the journal are the words they read.
func craftRefusal(sentence string) commandOutcome {
	return commandOutcome{status: store.CommandRejected, result: sentence, receipt: sentence}
}

func craftVerbWord(kind store.CommandKind) string {
	switch kind {
	case store.CommandCraftRun:
		return "run"
	case store.CommandCraftRevert:
		return "put back"
	}
	return "retire"
}

// craftNamedIntent is what the job says it is for. The person's own words when
// they gave any, and otherwise the plain statement of what was asked for —
// never an empty intent, which is what every provenance-reading surface would
// then have to render as a blank.
func craftNamedIntent(workflow *craft.Workflow, instruction string) string {
	if instruction != "" {
		return instruction
	}
	return craftIntent(workflow, nil)
}

// craftParamQuestion turns the filler's refusal into one question in the
// product's voice. The parser's error names the params it could not fill, which
// is exactly what has to be asked for; the mechanism words around it are not.
func craftParamQuestion(workflow *craft.Workflow, name string, err error) string {
	missing := craftMissingParams(workflow, err)
	if len(missing) == 0 {
		return "I could not work out how to run " + name + " from that — say what it should work on."
	}
	return "To run " + name + " I need " + joinWords(missing) + ". What should I use?"
}

// craftMissingParams reads the param names back out of Fill's error. The error
// is written for a person to read and its shape is stable — "missing required
// param(s): a, b" — but a name that cannot be recovered from it is not worth
// guessing at, so an unreadable error simply yields none and the question above
// falls back to plain words.
func craftMissingParams(workflow *craft.Workflow, err error) []string {
	if err == nil {
		return nil
	}
	_, list, found := strings.Cut(err.Error(), ": missing required param")
	if !found {
		return nil
	}
	if _, after, ok := strings.Cut(list, ": "); ok {
		list = after
	}
	declared := map[string]bool{}
	for _, param := range workflow.Params {
		declared[strings.TrimSpace(param.Name)] = true
	}
	names := make([]string, 0, len(workflow.Params))
	for _, piece := range strings.Split(list, ",") {
		// Fill annotates a near miss as "topic (you passed topics)"; the name is
		// the first word of the piece either way.
		fields := strings.Fields(piece)
		if len(fields) == 0 {
			continue
		}
		if declared[fields[0]] {
			names = append(names, fields[0])
		}
	}
	return names
}

// joinWords is the one-line English list a sentence needs: "a", "a and b",
// "a, b and c".
func joinWords(words []string) string {
	switch len(words) {
	case 0:
		return ""
	case 1:
		return words[0]
	}
	return strings.Join(words[:len(words)-1], ", ") + " and " + words[len(words)-1]
}
