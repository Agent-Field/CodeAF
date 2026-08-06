// Package profile records what tasks actually cost this model, and uses it to
// recalibrate the ruler the planner sizes tasks with.
//
// The sizing anchors began as a prior — three invented examples of a too-small,
// a right-sized, and a too-big task — with a note that they should be replaced
// by measurement once an executor existed. This is that replacement.
//
// It is calibration, not learning, and the difference matters. Nothing here
// fits a model or predicts a number. It collects what real tasks cost, and when
// there is enough evidence that the current ruler is wrong, it asks once for
// three new examples drawn from tasks that actually ran. The output is still
// three sentences in a prompt; only their provenance changes.
package profile

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/Agent-Field/aforge-v2/internal/provider"
)

// Record is one executed leaf.
type Record struct {
	Title   string `json:"title"`
	Summary string `json:"summary"`
	Sources int    `json:"sources"`
	Size    string `json:"size"`  // what the planner predicted
	Turns   int    `json:"turns"` // what it actually took
	Tokens  int    `json:"tokens"`
	Stop    string `json:"stop"`

	// Verdict is how the leaf actually ended. It replaced a `done` flag that was
	// the scheduler's StateDone carried across — true of a leaf that exhausted
	// its budget mid-edit as much as of one that finished — and the flag was
	// never read, because a ruler calibrated against it would have been
	// calibrated against the budget rather than against the work.
	Verdict provider.Verdict `json:"verdict,omitempty"`
}

// Overran reports a task that could not finish inside its budget — the clearest
// evidence that the ruler let too much into one node.
//
// The verdict is the authority where there is one; the stop reason is read for
// records written before verdicts existed, so an old profile still calibrates.
func (r Record) Overran() bool {
	switch r.Verdict {
	case provider.VerdictBudgetStop, provider.VerdictTurnCap:
		return true
	case "":
		return r.Stop == "budget" || r.Stop == "turn-cap"
	default:
		return false
	}
}

// Profile is the accumulated experience of one model running one kind of work.
//
// Keyed by model and skill because capability is a property of the executor, not
// of the project. When specialised sub-harnesses arrive — a reviewer, a coding
// worker — each accumulates its own profile with no new machinery: a different
// skill is simply a different file.
type Profile struct {
	Model   string   `json:"model"`
	Skill   string   `json:"skill"`
	Anchors string   `json:"anchors,omitempty"` // empty means the built-in prior
	Records []Record `json:"records"`

	path  string
	mutex sync.Mutex
}

// maxRecords bounds the file. Old measurements describe a ruler that has since
// been replaced, so keeping them forever would anchor to a state that no longer
// exists.
const maxRecords = 200

// MinSamples is how much evidence is needed before the ruler may be rewritten.
// Eight leaves is not statistics and this is not pretending to be — it is only
// enough to tell a systematically wrong anchor from one unlucky task.
const MinSamples = 8

// Load reads the profile for a model and skill, returning an empty one when
// there is nothing recorded yet.
func Load(dir, model, skill string) (*Profile, error) {
	if strings.TrimSpace(dir) == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		dir = filepath.Join(home, ".aforge")
	}
	path := filepath.Join(dir, fmt.Sprintf("profile-%s-%s.json", slug(model), slug(skill)))
	profile := &Profile{Model: model, Skill: skill, path: path}

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return profile, nil
	}
	if err != nil {
		return profile, err
	}
	if err := json.Unmarshal(data, profile); err != nil {
		// A corrupt profile is not worth failing a run over; it is a cache of
		// observations, and the built-in prior is a safe place to restart from.
		return &Profile{Model: model, Skill: skill, path: path}, nil
	}
	profile.path = path
	return profile, nil
}

// Add appends measurements.
func (p *Profile) Add(records ...Record) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	for _, record := range records {
		if strings.TrimSpace(record.Title) == "" {
			continue
		}
		p.Records = append(p.Records, record)
	}
	if len(p.Records) > maxRecords {
		p.Records = p.Records[len(p.Records)-maxRecords:]
	}
}

// Save writes the profile back.
func (p *Profile) Save() error {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	if p.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(p.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p.path, data, 0o644)
}

// Spread is what the profile knows, and it deliberately reports range rather
// than a single number. Identical per-city briefs came back at 9, 16 and 25
// turns in one run; a median alone would encode a precision that is not there.
type Spread struct {
	Samples  int
	MinTurns int
	Median   int
	MaxTurns int
	Overran  int
	MedianKb int
}

// Measure summarises the recorded work.
func (p *Profile) Measure() Spread {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	if len(p.Records) == 0 {
		return Spread{}
	}
	turns := make([]int, 0, len(p.Records))
	tokens := make([]int, 0, len(p.Records))
	spread := Spread{Samples: len(p.Records)}
	for _, record := range p.Records {
		turns = append(turns, record.Turns)
		tokens = append(tokens, record.Tokens)
		if record.Overran() {
			spread.Overran++
		}
	}
	sort.Ints(turns)
	sort.Ints(tokens)
	spread.MinTurns = turns[0]
	spread.MaxTurns = turns[len(turns)-1]
	spread.Median = turns[len(turns)/2]
	spread.MedianKb = tokens[len(tokens)/2] / 1000
	return spread
}

// NeedsRecalibration decides whether the ruler is worth rewriting.
//
// Two guards, both about not chasing noise. There must be enough samples to see
// a pattern, and the pattern must actually contradict the current ruler — either
// tasks are routinely exhausting their budget, which means the anchors are
// letting too much into one node, or almost nothing ever comes close, which
// means they are splitting work that did not need splitting.
func (p *Profile) NeedsRecalibration() (bool, string) {
	spread := p.Measure()
	if spread.Samples < MinSamples {
		return false, fmt.Sprintf("%d samples, need %d", spread.Samples, MinSamples)
	}
	overranShare := float64(spread.Overran) / float64(spread.Samples)
	switch {
	case overranShare >= 0.9:
		// Nearly everything overran. That is not evidence about task size — a
		// ruler that was merely too generous would still let some tasks finish.
		// It says the budget itself is set wrong, and recalibrating the ruler
		// from it would encode a configuration mistake as a fact about the work.
		// This confound is the main hazard of a closed calibration loop, and it
		// fired on the first real run.
		return false, fmt.Sprintf("%d of %d tasks overran — that points at the budget, not the ruler",
			spread.Overran, spread.Samples)
	case overranShare >= 0.25:
		return true, fmt.Sprintf("%d of %d tasks exhausted their budget — the ruler is too generous",
			spread.Overran, spread.Samples)
	case spread.Median <= 3 && spread.MaxTurns <= 6:
		return true, fmt.Sprintf("median %d turns across %d tasks — the ruler is splitting work that did not need it",
			spread.Median, spread.Samples)
	default:
		return false, fmt.Sprintf("median %d turns, %d of %d overran — the ruler holds",
			spread.Median, spread.Overran, spread.Samples)
	}
}

// Evidence picks the tasks worth showing a recalibration call: the cheapest few,
// the ones nearest the middle, and the ones that ran out of budget. Real
// examples at each end of the observed range are what a ruler is made of.
func (p *Profile) Evidence(each int) (small, middle, large []Record) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	sorted := append([]Record(nil), p.Records...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Turns < sorted[j].Turns })
	if len(sorted) == 0 {
		return nil, nil, nil
	}
	for _, record := range sorted {
		if record.Overran() && len(large) < each {
			large = append(large, record)
		}
	}
	small = sorted[:min(each, len(sorted))]
	centre := len(sorted) / 2
	from := max(0, centre-each/2)
	to := min(len(sorted), from+each)
	return small, sorted[from:to], large
}

var nonWord = regexp.MustCompile(`[^a-zA-Z0-9]+`)

func slug(value string) string {
	return strings.Trim(nonWord.ReplaceAllString(strings.ToLower(value), "-"), "-")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
