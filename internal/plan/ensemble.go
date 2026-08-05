package plan

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// Ensemble execution is the second way to spend parallelism, and it is not
// decomposition.
//
// Decomposition splits work by subject and buys speed: five agents each take a
// different city, and nothing is lost because no two of them were ever going to
// read the same page. Ensemble does the opposite. It hands the whole job to N
// agents at once, each working alone on the same material, and merges what they
// come back with. It buys no speed at all — it buys recall, and it pays N times
// the money for it.
//
// The observation behind it is a real run. Asked to review a pull request, a
// malformed plan accidentally ran five nearly-complete independent reviews and
// merged them; the merge held 8 verified defects where a single careful pass
// had found 4. Nothing about the second run was smarter. The passes simply did
// not miss the same things, because attention is what is scarce in judgment
// work and independent attention does not overlap perfectly.
//
// That makes it a general pattern with a narrow shape: one judgment over one
// body of material, where a miss is expensive and hard to notice — reviews,
// evaluations, audits, comparisons, recommendations. Everything else is
// decomposition, and running three passes over separable work just buys the
// same facts three times. So the choice is made once, deliberately, from the
// goal, and the prompt that makes it says plainly that decompose is the answer
// almost every time.
//
// Independence is the whole mechanism, which constrains the graph in one
// unusual way: the panelists must not be told about each other. A brief that
// says "another agent is covering the error paths" produces exactly the
// correlated blind spot the redundancy was bought to remove. So the panelist
// brief is written once, against a plan view that contains a single pass, and
// copied verbatim to every panelist — identical in coverage, and unaware that
// coverage is being repeated.

const (
	// EnsembleAuto lets the planner judge, once, from the goal.
	EnsembleAuto = 0

	// EnsembleNever skips the judgment call entirely. It is the setting for a
	// caller that knows its goals are separable work and does not want to pay a
	// call to be told so.
	EnsembleNever = -1

	// DefaultPanelists is three. Two cannot break a tie and gives the merge no
	// way to tell a rare catch from a lone mistake; beyond three the marginal
	// finding per pass falls off well before the cost does.
	DefaultPanelists = 3

	// maxPanelists bounds a forced panel. The flag is an operator's instruction,
	// not a model's, but a typo that asks for 500 identical passes should cost a
	// clamp rather than a bill.
	maxPanelists = 16
)

// ensemblePrompt makes the one judgment this file exists for.
//
// It is written in the style of ground.go: a single call, from the goal alone,
// that settles something every later pass would otherwise decide badly or
// implicitly. The pressure in it runs one way on purpose. Asked whether extra
// scrutiny would help, a model says yes about anything, and an ensemble is the
// most expensive answer the planner can give — so the prompt states decompose
// as the default, names the exact shapes that qualify, and prices the choice in
// the currency being spent.
//
// The panel description is folded into the same call rather than taking a
// second one. The alternative was a judgment call followed by a shaping call,
// which costs an extra serial round on the one path that has no other work to
// hide behind. The fields are ignored outright when the verdict is decompose,
// so the only risk is a model filling them in hopefully, which the prompt
// closes by telling it to leave them empty.
const ensemblePrompt = `You decide how a goal should be worked: split up, or done several times over.

Almost every goal is SPLIT UP. The work divides by subject — different cities,
files, vendors, sections, components, questions — and each part produces a
different piece of the answer. Splitting buys speed, and nothing is lost by it,
because no two parts were going to look at the same material anyway.

A few goals are not like that. The work is one judgment over one body of
material, and the answer is not made of parts: it is one verdict, one list of
findings, one recommendation about the same thing. Review this pull request.
Evaluate this proposal. Audit these accounts. Assess this design. Choose between
these two options. Grade this submission. Splitting by subject buys nothing
there, because there is only one subject.

For that shape the useful redundancy is doing the whole job several times over,
independently, and merging the results. It is not a second opinion, and not a
review of a review: every pass does the complete job alone, without seeing the
others, and what one misses another catches. On a real code review, five
independent passes found 8 verified defects where a single careful pass found 4.
Neither pass was smarter. Attention is what is scarce in judgment work, and
independent attention does not overlap perfectly.

That recall is bought with money. N passes cost N times as much as one and
finish no sooner. It is worth paying when a miss is expensive and hard to
notice — a defect that ships, a risk nobody flagged, a candidate wrongly
rejected. It is not worth paying when the work is mechanical, when a mistake
would be obvious the moment anyone looked, or when the answer can simply be
looked up.

Return "ensemble" only when all three hold:
  1. The core work is one judgment over one body of material, not several
     separable subjects.
  2. It is work where careful people disagree and miss different things —
     reviewing, evaluating, auditing, comparing, recommending.
  3. Missing something matters more than the extra spend.

Otherwise return "decompose". That is the answer for most goals, including most
goals that mention analysis or research: gathering facts about several things is
separable work, and doing it three times over buys the same facts three times.
Building something is separable work. Writing a long document is separable work.

You are also shown the ordered stages the planner drew for this goal. Treat them
as evidence. A single stage means the planner found no gate in the work at all,
which fits one body of material being judged; several stages mean the goal has
real phases, and phases are separable work.

When you return "ensemble", describe the panel as well:
- pass_title and pass_summary: the one job every pass does, identically. Write
  it as the complete job, never as a share of it — every pass covers all of the
  material and produces its own full set of findings.
- pass_sources: what that pass must touch, named concretely.
- setup_title and setup_summary: the single piece of preparation, if any, that
  puts the material in place before anyone judges it — checking out the branch,
  assembling the corpus, collecting the submissions. Leave both empty when the
  material is already at hand. "Read the thing" is not preparation.
- deliverable: what the merge finally produces, in a few words.

When you return "decompose", leave all of those empty.`

var ensembleSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "mode":          { "type": "string", "enum": ["decompose", "ensemble"] },
    "reason":        { "type": "string" },
    "pass_title":    { "type": "string" },
    "pass_summary":  { "type": "string" },
    "pass_sources":  { "type": "array", "items": { "type": "string" } },
    "setup_title":   { "type": "string" },
    "setup_summary": { "type": "string" },
    "deliverable":   { "type": "string" }
  },
  "required": ["mode", "reason", "pass_title", "pass_summary", "pass_sources",
               "setup_title", "setup_summary", "deliverable"],
  "additionalProperties": false
}`)

// Panel is the verdict and, when the verdict is ensemble, the shape of the
// panel to build.
type Panel struct {
	Mode   string `json:"mode"`
	Reason string `json:"reason"`

	PassTitle   string   `json:"pass_title"`
	PassSummary string   `json:"pass_summary"`
	PassSources []string `json:"pass_sources"`

	SetupTitle   string `json:"setup_title"`
	SetupSummary string `json:"setup_summary"`

	Deliverable string `json:"deliverable"`
}

// Chosen reports whether the judgment came back ensemble.
func (p *Panel) Chosen() bool { return p != nil && p.Mode == "ensemble" }

// normalize fills in what a caller can survive without. A forced panel arrives
// with nothing decided — the operator asked for N passes, not for a shape — and
// a model that answered decompose leaves every shaping field empty, so both
// paths land here.
func (p *Panel) normalize(goal string) {
	p.PassTitle = trim(p.PassTitle)
	p.PassSummary = trim(p.PassSummary)
	p.SetupTitle = trim(p.SetupTitle)
	p.SetupSummary = trim(p.SetupSummary)
	p.Deliverable = trim(p.Deliverable)
	p.PassSources = cleanStrings(p.PassSources)

	if p.PassTitle == "" {
		p.PassTitle = "Pass"
	}
	if p.PassSummary == "" {
		p.PassSummary = "Do the whole of this job alone, from the material itself: " + trim(goal)
	}
	if p.Deliverable == "" {
		p.Deliverable = "the finished answer to the goal"
	}
	// A setup node with a title and nothing to do is worse than no setup node:
	// it adds a serial hop in front of every panelist for a step nobody can
	// describe. Half a setup is no setup.
	if p.SetupTitle == "" || p.SetupSummary == "" {
		p.SetupTitle, p.SetupSummary = "", ""
	}
}

// DecidePanel asks, once, whether this goal wants redundancy instead of
// decomposition. The spine is passed as evidence rather than as instruction:
// how many gates the planner found in the goal is the cheapest available signal
// about whether the work is one body of material or several.
func DecidePanel(ctx context.Context, client Completer, goal string, stages []Stage) (*Panel, *ai.Usage, error) {
	var evidence strings.Builder
	evidence.WriteString("Goal:\n")
	evidence.WriteString(strings.TrimSpace(goal))
	if len(stages) > 0 {
		evidence.WriteString("\n\nThe stages the planner drew for it:\n")
		evidence.WriteString(spineBlock(stages))
	}

	messages := []ai.Message{
		systemMessage(ensemblePrompt),
		userMessage(evidence.String()),
	}
	response, err := client.CompleteWithMessages(ctx, messages, ai.WithSchema(ensembleSchema))
	if err != nil {
		return nil, nil, fmt.Errorf("ensemble: %w", err)
	}
	var panel Panel
	if err := decodeJSON(response.Text(), &panel); err != nil {
		return nil, usageOf(response), annotate(fmt.Errorf("ensemble: %w", err), response)
	}
	panel.Mode = strings.ToLower(trim(panel.Mode))
	panel.Reason = trim(panel.Reason)
	return &panel, usageOf(response), nil
}

// panelistCharge is appended to the brief every panelist receives.
//
// It is the only part of the instruction that knows an ensemble is happening,
// and it is written to say so without ever revealing what the other panelists
// are looking at. A panelist told "the others are covering the error paths"
// stops covering the error paths, and the redundancy that was paid for
// evaporates into a shared blind spot.
//
// The second half is the boundary that keeps a single deliverable owner. N
// agents each writing the final report produces N reports and no answer; the
// panelists produce findings, the synthesis produces the deliverable.
const panelistCharge = `Work only from the material itself and your own reading of it. Report everything
you find, including anything that looks minor, borderline, or too obvious to be
worth writing down — nothing you leave out will be recovered later, and being
complete matters more here than being brief. Give the evidence for each finding:
the file and line, the quote, the number, the step that reproduces it.

Deliver the findings themselves, as a plain list, each one standing on its own.
Do not write %s, do not rank them down to a shortlist, and do not stop early
because you have found enough.`

// ensembleMergeBrief is the instruction the synthesis node receives, and it is
// the most important prompt in this file.
//
// Everything upstream of it is only a way of producing several overlapping
// answers; whether the ensemble was worth its cost is decided entirely by how
// they are combined. The failure mode is specific and it is the intuitive one:
// treat agreement as truth, keep what the passes have in common, and discard the
// rest as noise. That is an intersection, and it throws away precisely the
// findings the redundancy was bought to catch — the four extra defects in the
// run that motivated this were, by definition, the ones only some passes saw.
//
// So the contract is union-with-dedup, and the only thing that earns the right
// to drop a finding is checking it. Being found once is not evidence against
// anything.
func ensembleMergeBrief(panelists int, deliverable string) string {
	return fmt.Sprintf(`You have the results of %d independent passes over the same material. Each was
done alone, by an agent that could not see the others, so they overlap heavily
and they will disagree in places. Both of those are expected.

Produce %s: one merged result, not %d results side by side.

Take the union of what they found, not the intersection. A finding that appears
in only one pass is not weaker for having been found once — it is usually the
reason the work was done more than once at all. Check it, and keep it if it
holds. The only thing that earns the right to drop a finding is having checked
it and found it wrong, or having already stated it as another finding.

Where two passes describe the same thing in different words, merge them into one
entry and keep the sharpest evidence of the two. Where two passes contradict each
other, go back to the material and settle it yourself — do not prefer the more
confident wording, do not average them, and do not report both and leave it open.
Say what you checked.

Do not describe the passes, count them, attribute findings to them, or report
that they agreed. The reader wants the merged result, in the form the goal asked
for, as though one very thorough agent had produced it.`, panelists, deliverable, panelists)
}

// ensembleMergeContract is the working method for the merge, written here
// rather than generated because the method is the same every time and it is the
// one prompt in the system that must not drift.
const ensembleMergeContract = `Read every result in full before writing anything. A merge written incrementally
from the first result and patched with the rest inherits the first pass's
blind spots, which is the one outcome this arrangement exists to avoid.

Work through them in three passes of your own:
- Pair up. Group entries that are the same finding in different words. Same
  place, same cause, same consequence is one entry, however differently phrased.
  Different consequence is a different finding, however similar the wording.
- Verify. Every entry only one pass reports, and every pair that disagrees, gets
  checked against the material itself before you keep or drop it. Checking is
  cheap here and it is the whole basis on which you are allowed to discard
  anything.
- Order. Rank what survives by consequence, not by how many passes mentioned it.
  How many passes saw something measures how visible it was, not how much it
  matters.

Carry the evidence into the merged result — file and line, quote, number,
reproduction — so a reader can confirm any entry without rerunning any of this.

The mistakes to watch for: keeping only what the passes agreed on, which throws
away the recall you paid for; concatenating the results and leaving the reader to
deduplicate them; and settling a contradiction by picking the more confident
wording instead of going back and looking.`

// Ensemble rewrites a graph as a panel: an optional shared setup, N independent
// panelists, and one synthesis node that merges them.
//
// The graph keeps its goal and its grounding — those were settled before this
// choice was made and are just as true for a panel — and loses its stages and
// nodes, because decomposition and redundancy are alternatives, not layers.
//
// Nothing structural is new. Panelists are ordinary work nodes and the merge is
// an ordinary synthesis node, so nothing downstream of the planner has to learn
// what an ensemble is; a graph that ran through the normal executor before still
// does.
func Ensemble(ctx context.Context, client Completer, graph *Graph, panel Panel, panelists int) (Usage, error) {
	panelists = clampPanelists(panelists)
	panel.normalize(graph.Goal)

	graph.Nodes = nil
	graph.NextID = 1
	graph.Stages = nil

	setupID := 0
	if panel.SetupTitle != "" {
		graph.Stages = append(graph.Stages, Stage{Title: panel.SetupTitle, Summary: panel.SetupSummary})
		setupID = graph.Add(Node{
			Stage:   1,
			Title:   panel.SetupTitle,
			Summary: panel.SetupSummary,
			Size:    SizeAtomic,
			Kind:    KindWork,
		})
	}

	passStage := len(graph.Stages) + 1
	graph.Stages = append(graph.Stages, Stage{
		Title:   "Panel",
		Summary: fmt.Sprintf("%d independent passes over the same material: %s", panelists, panel.PassSummary),
	})

	ids := make([]int, 0, panelists)
	for index := 0; index < panelists; index++ {
		node := Node{
			Stage: passStage,
			// Titles differ only by number. They are handles — the graph needs
			// them distinct — and the work behind them is deliberately the same.
			Title:   fmt.Sprintf("%s %d", panel.PassTitle, index+1),
			Summary: panel.PassSummary,
			Sources: append([]string(nil), panel.PassSources...),
			Size:    SizeAtomic,
			Kind:    KindWork,
		}
		if setupID != 0 {
			node.Needs = []int{setupID}
		}
		ids = append(ids, graph.Add(node))
	}

	// The merge reads the panelists and nothing else. It could be handed the
	// setup's output too, but the material is reachable through what the
	// panelists cite, and routing it here would put the whole body of material
	// into the one context that already holds N full result sets.
	graph.Add(Node{
		Stage:    passStage + 1,
		Title:    "Synthesis",
		Summary:  fmt.Sprintf("Merge the %d independent results into %s.", panelists, panel.Deliverable),
		Needs:    ids,
		Kind:     KindSynthesis,
		Brief:    ensembleMergeBrief(panelists, panel.Deliverable),
		Contract: ensembleMergeContract,
	})

	// Working methods are left to the ordinary contract pass, which writes one
	// per node at run time. Two panelists reasoning about the same material in
	// slightly different ways is harmless, and arguably the point — it is their
	// coverage that has to be identical, not their technique.
	return writePanelBriefs(ctx, client, graph, panel, ids, setupID)
}

// writePanelBriefs writes the panelists' one shared instruction, and the setup
// node's.
//
// It runs whether or not briefs were asked for, which is the one place this
// path departs from the normal one. In a decomposed graph a brief is a
// convenience the executor can fill in later; here it is load-bearing. The
// executor's own brief pass writes one instruction per node, which would give
// the panelists N separately-worded jobs and quietly turn identical coverage
// into approximate coverage — the failure this whole arrangement is built to
// avoid. Writing it once here and copying it is the only way the coverage is
// provably the same.
func writePanelBriefs(ctx context.Context, client Completer, graph *Graph, panel Panel, ids []int, setupID int) (Usage, error) {
	// The plan the brief writer is shown contains one pass, not N. This is the
	// mechanism of independence, not a cosmetic choice: shown its eight
	// siblings, the brief writer does what it is told to do everywhere else in
	// this system and carves out a boundary between them.
	setup := Node{ID: 1, Title: panel.SetupTitle, Summary: panel.SetupSummary, Kind: KindWork}
	pass := Node{ID: 1, Title: panel.PassTitle, Summary: panel.PassSummary, Sources: panel.PassSources, Kind: KindWork}
	var catalog strings.Builder
	if setupID != 0 {
		pass.ID = 2
		fmt.Fprintf(&catalog, "  %d. %s\n", setup.ID, setup.Title)
	}
	fmt.Fprintf(&catalog, "  %d. %s\n", pass.ID, pass.Title)
	shared := graph.context() + "\nThe full plan:\n" + catalog.String()

	var inputs []string
	if setupID != 0 {
		inputs = []string{fmt.Sprintf("%q (%s)", setup.Title, setup.Summary)}
	}

	var (
		group                 sync.WaitGroup
		mutex                 sync.Mutex
		usage                 Usage
		failures              []error
		passBrief, setupBrief string
	)
	write := func(node Node, inputs []string, into *string) {
		defer group.Done()
		brief, callUsage, err := writeBrief(ctx, client, shared, node, inputs)
		mutex.Lock()
		defer mutex.Unlock()
		usage.Add(callUsage)
		if err != nil {
			failures = append(failures, err)
			return
		}
		*into = brief
	}
	group.Add(1)
	go write(pass, inputs, &passBrief)
	if setupID != 0 {
		group.Add(1)
		go write(setup, nil, &setupBrief)
	}
	group.Wait()

	if node := graph.Node(setupID); node != nil && setupBrief != "" {
		node.Brief = setupBrief
	}
	if passBrief != "" {
		charged := passBrief + "\n\n" + fmt.Sprintf(panelistCharge, panel.Deliverable)
		for _, id := range ids {
			if node := graph.Node(id); node != nil {
				// Byte-identical, on purpose. Two briefs that differ only in
				// phrasing are two different jobs to an agent reading one of them.
				node.Brief = charged
			}
		}
	}
	return usage, joinErrors(failures)
}

// ensembleHook is the single entry point plan.Build uses, kept here so that the
// integration in plan.go is four lines that can be deleted without leaving a
// trace behind.
//
// It sits after grounding and the spine because both are already paid for and
// both feed the decision — the grounding is inherited by the panel verbatim,
// and the stage count is the evidence the judgment call leans on. Its one cost
// on the ordinary path is a single extra call round, which EnsembleNever buys
// back for a caller that never wants a panel.
func ensembleHook(ctx context.Context, client Completer, graph *Graph, options Options, report Progress, start time.Time) (*Graph, bool, error) {
	if options.Ensemble == EnsembleNever {
		return nil, false, nil
	}
	forced := options.Ensemble >= 2

	panel, usage, err := DecidePanel(ctx, client, graph.Goal, graph.Stages)
	graph.Usage.Add(usage)
	switch {
	case err != nil && !forced:
		// A failed judgment falls back to decomposition rather than failing the
		// plan — the ensemble is an optimisation, and the plan without it is
		// still a plan. It is reported rather than swallowed: a run where this
		// call is failing every time is buying a call and getting nothing.
		report("ensemble", time.Since(start), fmt.Sprintf("undecided (%s), decomposing", clipReason(err.Error(), 40)))
		return nil, false, nil
	case err != nil:
		// Forced. The operator asked for N passes, so the shape is defaulted
		// rather than the instruction refused.
		panel = &Panel{}
	case !forced && !panel.Chosen():
		report("ensemble", time.Since(start), "decompose: "+clipReason(panel.Reason, 40))
		return nil, false, nil
	}

	panelists := DefaultPanelists
	if forced {
		panelists = clampPanelists(options.Ensemble)
	}
	ensembleUsage, ensembleErr := Ensemble(ctx, client, graph, *panel, panelists)
	graph.Usage.merge(ensembleUsage)
	detail := fmt.Sprintf("%d independent passes + merge", panelists)
	// Read the setup back off the graph rather than off the verdict: a
	// half-described setup is dropped during normalisation, and the report
	// should say what was built.
	if len(graph.Leaves()) > panelists {
		detail = "setup + " + detail
	}
	report("ensemble", time.Since(start), detail)

	// The panelists — or the setup, when there is one — are dispatchable the
	// moment the briefs are written, exactly as in a decomposed plan.
	if options.OnReady != nil {
		elapsed := time.Since(start)
		for _, node := range graph.Nodes {
			if node.Kind == KindWork && len(node.Needs) == 0 {
				options.OnReady(node, elapsed)
			}
		}
	}
	return graph, true, ensembleErr
}

// clipReason fits a model's sentence or a provider's error into one column of
// the progress report. It counts runes rather than bytes: these strings carry
// em dashes and quotes, and a byte-sliced one prints as a broken glyph.
func clipReason(text string, width int) string {
	runes := []rune(strings.Join(strings.Fields(text), " "))
	if len(runes) <= width || width < 2 {
		return string(runes)
	}
	return string(runes[:width-1]) + "…"
}

// clampPanelists keeps a panel between "enough to disagree" and "an obvious
// typo". Below two there is no panel; above the ceiling the marginal finding is
// long gone and only the bill is still growing.
func clampPanelists(panelists int) int {
	if panelists < 2 {
		return DefaultPanelists
	}
	if panelists > maxPanelists {
		return maxPanelists
	}
	return panelists
}
