package praf

// The review's progress as the run's records: every router reasoner the
// orchestrator calls is a stage it is in and, when it returns, one step that
// says what it found.

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/praf/orch"
	"github.com/Agent-Field/codeaf/internal/praf/reasoners"
)

// The stages a review reports, in order. starting and report are the
// program's own; the rest are pr-af's pipeline phases, read off the reasoners
// it calls.
const (
	stageStarting    = "starting"
	stageIntake      = "intake"
	stageAnatomy     = "anatomy"
	stagePlan        = "plan"
	stageReview      = "review"
	stageFilter      = "filter"
	stageEvidence    = "evidence"
	stageChallenge   = "challenge"
	stageDeepen      = "deepen"
	stageCrossRef    = "cross-ref"
	stageObligations = "obligations"
	stageCoverage    = "coverage"
	stageReport      = "report"
	// stagePost is the one stage of a post run.
	stagePost = "post"
)

// Stages is every stage a review reports, in the order it reaches them.
var Stages = []string{
	stageStarting, stageIntake, stageAnatomy, stagePlan, stageReview, stageFilter,
	stageEvidence, stageChallenge, stageDeepen, stageCrossRef, stageObligations,
	stageCoverage, stageReport,
}

// stageWords is each stage in a person's words, for the task's row.
var stageWords = map[string]string{
	stageStarting:    "starting",
	stageIntake:      "reading the pull request",
	stageAnatomy:     "mapping the change",
	stagePlan:        "planning the review",
	stageReview:      "reviewing",
	stageFilter:      "weighing findings",
	stageEvidence:    "checking evidence",
	stageChallenge:   "challenging findings",
	stageDeepen:      "deepening findings",
	stageCrossRef:    "cross-checking",
	stageObligations: "checking obligations",
	stageCoverage:    "looking for gaps",
	stageReport:      "writing the review",
	stagePost:        "posting",
}

// phase is how one router reasoner reads on the page: its stage, what it is
// doing while it runs, what it did when it returns, and optionally the part
// of the change one call was about.
type phase struct {
	stage, doing, did string
	detail            func(input map[string]any) string
}

// phases maps every router reasoner to its stage. The orchestrator calls them
// through orch.Deps.Local by these names, which is what makes the pipeline's
// own phase boundaries the run's stages without the orchestrator knowing it is
// watched.
var phases = map[string]phase{
	reasoners.NameIntakePhase:         {stage: stageIntake, doing: "reading the pull request", did: "read the pull request"},
	reasoners.NameAnatomyPhase:        {stage: stageAnatomy, doing: "mapping the change", did: "mapped the change"},
	reasoners.NamePlanningPhase:       {stage: stagePlan, doing: "choosing what to review", did: "chose what to review"},
	reasoners.NameMetaSemantic:        {stage: stagePlan, doing: "choosing what to review", did: "looked through the semantic lens"},
	reasoners.NameMetaMechanical:      {stage: stagePlan, doing: "choosing what to review", did: "looked through the mechanical lens"},
	reasoners.NameMetaSystemic:        {stage: stagePlan, doing: "choosing what to review", did: "looked through the systemic lens"},
	reasoners.NameReviewDimension:     {stage: stageReview, doing: "reviewing", did: "reviewed", detail: dimensionDetail},
	reasoners.NamePostWorthinessGate:  {stage: stageFilter, doing: "dropping findings not worth raising", did: "dropped findings not worth raising"},
	reasoners.NameEvidenceVerifier:    {stage: stageEvidence, doing: "checking findings against the code", did: "checked a finding against the code"},
	reasoners.NameAdversaryPhase:      {stage: stageChallenge, doing: "challenging findings", did: "challenged the findings"},
	reasoners.NameDeepenFindings:      {stage: stageDeepen, doing: "deepening findings", did: "deepened the findings"},
	reasoners.NameCompoundFinderPhase: {stage: stageCrossRef, doing: "looking for interacting findings", did: "looked for interacting findings"},
	reasoners.NameCompoundDedupPhase:  {stage: stageCrossRef, doing: "merging interacting findings", did: "merged interacting findings"},
	reasoners.NameExtractObligations:  {stage: stageObligations, doing: "listing what the change must do", did: "listed what the change must do"},
	reasoners.NameVerifyObligation:    {stage: stageObligations, doing: "checking what the change must do", did: "checked one thing the change must do"},
	reasoners.NameCoverageGate:        {stage: stageCoverage, doing: "looking for gaps", did: "looked for gaps"},
}

// dimensionDetail names a reviewer by the files it was given, which is what a
// person recognizes; its dimension's prompt is a paragraph.
func dimensionDetail(input map[string]any) string {
	var names []string
	switch files := input["target_files"].(type) {
	case []string:
		for _, s := range files {
			if s != "" {
				names = append(names, s)
			}
		}
	case []any:
		for _, f := range files {
			if s, ok := f.(string); ok && s != "" {
				names = append(names, s)
			}
		}
	}
	switch len(names) {
	case 0:
		return ""
	case 1, 2:
		return strings.Join(names, ", ")
	default:
		return fmt.Sprintf("%s, %s and %d more", names[0], names[1], len(names)-2)
	}
}

// phaseTracker is the run's orch.LocalCaller. Every pipeline phase comes
// through it by name: it reports the stage, runs the reasoner, and reports
// the finished call as a step. The counts make a fan-out read as progress —
// "reviewing, 3 of 8 done" — instead of a stage that sits still for twenty
// minutes.
type phaseTracker struct {
	host     delegate.Host
	handlers map[string]handler

	mu      sync.Mutex
	started map[string]int
	done    map[string]int
	current string
}

var _ orch.LocalCaller = (*phaseTracker)(nil)

func newPhaseTracker(host delegate.Host, handlers map[string]handler) *phaseTracker {
	return &phaseTracker{host: host, handlers: handlers, started: map[string]int{}, done: map[string]int{}, current: stageStarting}
}

// stage is the stage the review is in now.
func (p *phaseTracker) stage() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.current
}

// CallLocal runs one reasoner under its name.
func (p *phaseTracker) CallLocal(ctx context.Context, name string, input map[string]any) (any, error) {
	run, ok := p.handlers[name]
	if !ok {
		return nil, fmt.Errorf("unknown reasoner %q", name)
	}
	ph, ok := phases[name]
	if !ok {
		ph = phase{stage: p.stage(), doing: "working", did: "finished a step"}
	}

	// Records are written under p.mu so the counts reach codeaf in the order
	// they were taken: a fan-out finishing on two goroutines at once must not
	// report "2 of 3 done" before "1 of 3 done".
	p.mu.Lock()
	p.started[ph.stage]++
	p.current = ph.stage
	p.stageLocked(ph)
	p.mu.Unlock()

	out, err := run(ctx, input)

	command := ph.did
	if ph.detail != nil {
		if d := ph.detail(input); d != "" {
			command += " " + d
		}
	}
	p.mu.Lock()
	p.done[ph.stage]++
	p.host.Step(delegate.StepRecord{Tool: name, Step: ph.stage, Command: plainWords(command), Observation: observe(out, err)})
	p.stageLocked(ph)
	p.mu.Unlock()
	return out, err
}

// stageLocked reports the stage with what it is doing and, once it fans out,
// how many of its calls are done. Callers hold p.mu.
func (p *phaseTracker) stageLocked(ph phase) {
	data := map[string]any{"doing": ph.doing}
	if started := p.started[ph.stage]; started > 1 {
		data["started"], data["done"] = started, p.done[ph.stage]
	}
	p.host.Stage(delegate.StageRecord{Stage: ph.stage, Status: "running", Data: stageData(data)})
}

// observeTitles is how many findings a step's observation names.
const observeTitles = 3

// observe is what one call found, for its step: the error when it failed;
// otherwise how many findings it came back with and the first few by title,
// or the size of each list it returned when it returned no findings.
func observe(out any, err error) string {
	if err != nil {
		return "error: " + firstSentence(err.Error())
	}
	// A reasoner's map may hold typed slices; one JSON round trip reads every
	// shape alike.
	var m map[string]any
	if raw, err := json.Marshal(out); err != nil || json.Unmarshal(raw, &m) != nil || len(m) == 0 {
		return ""
	}
	if findings, ok := m["findings"].([]any); ok {
		if len(findings) == 0 {
			return "nothing found"
		}
		line := fmt.Sprintf("%d finding%s", len(findings), plural(len(findings)))
		var titles []string
		for _, f := range findings {
			if len(titles) == observeTitles {
				break
			}
			if finding, ok := f.(map[string]any); ok {
				if title, _ := finding["title"].(string); strings.TrimSpace(title) != "" {
					titles = append(titles, oneLine(title))
				}
			}
		}
		if len(titles) > 0 {
			line += ": " + strings.Join(titles, "; ")
		}
		return plainWords(line)
	}
	keys := make([]string, 0, len(m))
	for k, v := range m {
		if list, ok := v.([]any); ok && len(list) > 0 {
			keys = append(keys, fmt.Sprintf("%s: %d", strings.ReplaceAll(k, "_", " "), len(list)))
		}
	}
	sort.Strings(keys)
	return plainWords(strings.Join(keys, ", "))
}

// stageData is a stage record's data, or nothing when it does not fit.
func stageData(fields map[string]any) json.RawMessage {
	data, err := json.Marshal(fields)
	if err != nil || len(data) > delegate.StageDataCap {
		return nil
	}
	return data
}
