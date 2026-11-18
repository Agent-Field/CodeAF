package factorytest

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/run"
)

// Delegate is the scripted delegate: the stage executor of every chat step.
// A step with nothing scripted does its work and reports it done, as a
// delegate on a good day does: write changes a file, a Follow PR round
// changes a file and reports it fixed, a final review reports the pull
// request ready. A scenario scripts the rounds it wants to go another way.
type Delegate struct {
	s *Scenario

	mu     sync.Mutex
	script map[string][]Round
	always map[string]Round
	// Ran is every round run, oldest first, as "step#round".
	Ran []string
	// heard is what each round was told beside its brief, as "step#round:
	// words": the notes its job carried and the request of a Follow PR round.
	heard []string
}

// Round is one scripted round of a step.
type Round struct {
	// Result is the round's report. A zero Result with nothing else set is
	// the step's good day.
	Result *factory.StageResult
	// Err fails the round, as a crashed delegate does.
	Err error
	// Ask puts a question from the step before it reports.
	Ask string
	// Spend is what the round cost, in dollars.
	Spend float64
	// Edit changes a file in the checkout before the report.
	Edit bool
	// entered is closed when the round begins, and the round then waits for
	// hold to be closed or for its runner to let it go.
	entered, hold chan struct{}
}

// Next scripts step's next rounds, in order, after any already scripted.
func (d *Delegate) Next(step string, rounds ...Round) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.script == nil {
		d.script = map[string][]Round{}
	}
	d.script[step] = append(d.script[step], rounds...)
}

// Always scripts every round of step that nothing else scripts.
func (d *Delegate) Always(step string, round Round) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.always == nil {
		d.always = map[string]Round{}
	}
	d.always[step] = round
}

// Forget takes down everything scripted for step.
func (d *Delegate) Forget(step string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.script, step)
	delete(d.always, step)
}

// Rounds is how many rounds of step have run.
func (d *Delegate) Rounds(step string) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	n := 0
	for _, ran := range d.Ran {
		if len(ran) > len(step) && ran[:len(step)+1] == step+"#" {
			n++
		}
	}
	return n
}

// Heard says a round of step was told words: in its notes, or as the request
// of a Follow PR round. round 0 is any round of the step.
func (d *Delegate) Heard(step string, round int, words string) bool {
	lead := step + "#"
	if round > 0 {
		lead = fmt.Sprintf("%s#%d:", step, round)
	}
	for _, told := range d.notes() {
		if strings.HasPrefix(told, lead) && strings.Contains(told, words) {
			return true
		}
	}
	return false
}

func (d *Delegate) notes() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.heard...)
}

func (d *Delegate) ran() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.Ran...)
}

func (d *Delegate) take(step string) (Round, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if queue := d.script[step]; len(queue) > 0 {
		d.script[step] = queue[1:]
		return queue[0], true
	}
	round, ok := d.always[step]
	return round, ok
}

// Proven is a report that says done with a claim shown and tests passing.
func Proven(words string) *factory.StageResult {
	return &factory.StageResult{Done: true, Output: words, Tests: &factory.TestCounts{Passed: 2},
		Claims: []factory.Claim{{Text: words, OK: true, Evidence: "focused tests passed", Medium: "test"}}}
}

// Fixed is a Follow PR round's report of a checked repair.
func Fixed(words string) *factory.StageResult {
	r := Proven(words)
	r.Repair = &factory.CIRepairResult{Decision: "fixed", Reason: words}
	return r
}

// NeedsYou is a Follow PR round's report that only the person can decide.
func NeedsYou(words string) *factory.StageResult {
	return &factory.StageResult{Done: true, Repair: &factory.CIRepairResult{Decision: "needs_you", Reason: words}}
}

// Run is one round of one step.
func (d *Delegate) Run(ctx context.Context, job run.Job) (factory.StageResult, error) {
	d.mu.Lock()
	d.Ran = append(d.Ran, fmt.Sprintf("%s#%d", job.Stage.Name, job.Round))
	told := append([]string(nil), job.Notes...)
	if f := job.Item.FollowUp; f != nil && f.Request != "" {
		told = append(told, f.Request)
	}
	for _, words := range told {
		d.heard = append(d.heard, fmt.Sprintf("%s#%d: %s", job.Stage.Name, job.Round, words))
	}
	d.mu.Unlock()
	round, scripted := d.take(job.Stage.Name)
	if round.entered != nil {
		close(round.entered)
		select {
		case <-round.hold:
		case <-ctx.Done():
			return factory.StageResult{}, ctx.Err()
		}
	}
	if round.Spend > 0 && job.Spend != nil {
		job.Spend(round.Spend)
	}
	if round.Ask != "" && job.Ask != nil {
		if _, err := job.Ask(ctx, factory.Asked{Question: round.Ask}); err != nil {
			return factory.StageResult{}, err
		}
	}
	if round.Err != nil {
		return factory.StageResult{}, round.Err
	}
	if err := ctx.Err(); err != nil {
		return factory.StageResult{}, err
	}
	follow := factory.IsCIRepairStage(job.Stage)
	readiness := follow && job.Item.FollowUp != nil && job.Item.FollowUp.Kind == "readiness" && job.Item.FollowUp.Readiness != nil
	if round.Edit || (!scripted || round.Result == nil) && (job.Stage.Name == "write" || follow && !readiness) {
		if err := d.edit(job.Dir); err != nil {
			return factory.StageResult{}, err
		}
	}
	if round.Result != nil {
		return *round.Result, nil
	}
	switch {
	case readiness:
		head := job.Item.FollowUp.Readiness.Head
		return factory.StageResult{Done: true, Repair: &factory.CIRepairResult{Decision: "no_change", Reason: "The diff and description match the checked result.",
			Readiness: &factory.ReadinessReview{Head: head, DiffReviewed: true, DescriptionMatches: true, FeedbackAddressed: true, NoUnfinishedWork: true}}}, nil
	case follow:
		return *Fixed("Reproduced the failure and repaired the changed code."), nil
	}
	return *Proven(job.Stage.Name + " went fine"), nil
}

// edit changes a file in the checkout, differently every time.
func (d *Delegate) edit(dir string) error {
	if dir == "" {
		return errors.New("the round has no checkout")
	}
	d.s.mu.Lock()
	d.s.edits++
	n := d.s.edits
	d.s.mu.Unlock()
	return os.WriteFile(filepath.Join(dir, "ledger.go"), []byte(fmt.Sprintf("package ledger\n\nconst balanced = %d\n", n)), 0o600)
}
