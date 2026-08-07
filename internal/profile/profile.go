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
	"time"

	"github.com/Agent-Field/aforge-v2/internal/provider"
)

// Record is one executed leaf.
type Record struct {
	Title   string `json:"title"`
	Summary string `json:"summary"`
	// Time is when this observation landed. Older profiles omit it; readers
	// may use the profile file's modification time as a coarse fallback.
	Time    time.Time `json:"time,omitempty"`
	Sources int       `json:"sources"`
	// SourcesKnown distinguishes an observed zero from old and direct records
	// whose omitted source count decoded to zero.
	SourcesKnown bool    `json:"sources_known,omitempty"`
	Size         string  `json:"size"`  // planner prediction, or direct when none was made
	Turns        int     `json:"turns"` // what it actually took
	Tokens       int     `json:"tokens"`
	Stop         string  `json:"stop"`
	Cost         float64 `json:"cost,omitempty"`
	Promoted     bool    `json:"promoted,omitempty"`

	// ExpectedTurns and ExpectedTokens are the profile's own medians for this
	// size before the record landed. Nil means the bucket had too little prior
	// evidence to make an expectation; that is distinct from a zero residual.
	ExpectedTurns  *int     `json:"expected_turns,omitempty"`
	ExpectedTokens *int     `json:"expected_tokens,omitempty"`
	Surprise       *float64 `json:"surprise,omitempty"`

	// Verdict is how the leaf actually ended. It replaced a `done` flag that was
	// the scheduler's StateDone carried across — true of a leaf that exhausted
	// its budget mid-edit as much as of one that finished — and the flag was
	// never read, because a ruler calibrated against it would have been
	// calibrated against the budget rather than against the work.
	Verdict provider.Verdict `json:"verdict,omitempty"`
}

// BucketDirect labels work dispatched without a planner size judgment. Keeping
// it separate lets the compiler learn direct-job costs without teaching the
// ruler that an unmeasured task was atomic.
const (
	BucketDirect = "direct"
	BucketReflex = "reflex"
)

func (r Record) rulerEvidence() bool {
	return r.Size != BucketDirect && r.Size != BucketReflex
}

// HasSourceCount keeps nonzero counts from legacy profiles usable while
// treating their indistinguishable zero value as unknown.
func (r Record) HasSourceCount() bool {
	return r.SourcesKnown || r.Sources != 0
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

	path string
	// modifiedAt is the coarse timestamp available to features reading an old
	// profile whose individual records predate Record.Time.
	modifiedAt time.Time
	mutex      sync.Mutex
}

// maxRecords bounds the file. Old measurements describe a ruler that has since
// been replaced, so keeping them forever would anchor to a state that no longer
// exists.
const maxRecords = 200

// MinSamples is how much evidence is needed before the ruler may be rewritten.
// Eight leaves is not statistics and this is not pretending to be — it is only
// enough to tell a systematically wrong anchor from one unlucky task.
const MinSamples = 8

// maxSurprise keeps one pathological run from dominating a bucket's error
// bar forever. Ten is still an honest 1000% miss while bounding bad telemetry.
const maxSurprise = 10.0

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
	if info, err := os.Stat(path); err == nil {
		profile.modifiedAt = info.ModTime().UTC()
	}
	return profile, nil
}

// Add appends measurements and returns the records as they were journaled.
// Each expectation is taken before its record enters the profile, so a leaf
// can never make its own prediction look better.
func (p *Profile) Add(records ...Record) []Record {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	added := make([]Record, 0, len(records))
	for _, record := range records {
		if strings.TrimSpace(record.Title) == "" {
			continue
		}
		if record.Time.IsZero() {
			record.Time = time.Now().UTC()
		}
		record.ExpectedTurns = nil
		record.ExpectedTokens = nil
		record.Surprise = nil
		if expectedTurns, expectedTokens, ok := p.expectation(record.Size); ok {
			record.ExpectedTurns = intPointer(expectedTurns)
			record.ExpectedTokens = intPointer(expectedTokens)
			surprise := (normalizedResidual(record.Turns, expectedTurns) +
				normalizedResidual(record.Tokens, expectedTokens)) / 2
			if surprise > maxSurprise {
				surprise = maxSurprise
			}
			record.Surprise = floatPointer(surprise)
		}
		p.Records = append(p.Records, record)
		added = append(added, record)
	}
	if len(p.Records) > maxRecords {
		p.Records = p.Records[len(p.Records)-maxRecords:]
	}
	return added
}

// expectation returns the median turns and tokens for one size bucket. The
// same evidence floor that protects ruler changes protects predictions: below
// it, surprise is unknown rather than deceptively recorded as zero.
func (p *Profile) expectation(size string) (int, int, bool) {
	turns := make([]int, 0, len(p.Records))
	tokens := make([]int, 0, len(p.Records))
	for _, record := range p.Records {
		if record.Size != size {
			continue
		}
		turns = append(turns, record.Turns)
		tokens = append(tokens, record.Tokens)
	}
	if len(turns) < MinSamples {
		return 0, 0, false
	}
	sort.Ints(turns)
	sort.Ints(tokens)
	return turns[len(turns)/2], tokens[len(tokens)/2], true
}

func normalizedResidual(actual, expected int) float64 {
	difference := actual - expected
	if difference < 0 {
		difference = -difference
	}
	return float64(difference) / float64(max(expected, 1))
}

func intPointer(value int) *int { return &value }

func floatPointer(value float64) *float64 { return &value }

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
	if err := os.WriteFile(p.path, data, 0o644); err != nil {
		return err
	}
	if info, err := os.Stat(p.path); err == nil {
		p.modifiedAt = info.ModTime().UTC()
	} else {
		p.modifiedAt = time.Now().UTC()
	}
	return nil
}

// RecordsSnapshot returns a stable copy of the measurements and the profile
// file's last modification time. Derived views use the latter only for legacy
// records written before per-record timestamps existed.
func (p *Profile) RecordsSnapshot() ([]Record, time.Time) {
	if p == nil {
		return nil, time.Time{}
	}
	p.mutex.Lock()
	defer p.mutex.Unlock()
	return append([]Record(nil), p.Records...), p.modifiedAt
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

// ReflexStats is the measured boundary between work that finishes as one
// quick action and work that promotes into the compiled path.
type ReflexStats struct {
	Samples      int
	Successes    int
	Promotions   int
	MedianTurns  int
	MedianTokens int
	AverageCost  float64
}

// MeasureReflex summarises reflex records without admitting them as planner
// ruler evidence. An unverified completion still counts here: this statistic
// asks whether the micro-leaf finished, not whether it should rate a model.
func (p *Profile) MeasureReflex() ReflexStats {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	var stats ReflexStats
	var turns, tokens []int
	var cost float64
	for _, record := range p.Records {
		if record.Size != BucketReflex {
			continue
		}
		stats.Samples++
		turns = append(turns, record.Turns)
		tokens = append(tokens, record.Tokens)
		cost += record.Cost
		if record.Promoted {
			stats.Promotions++
		} else if record.Verdict == provider.VerdictVerifiedSuccess ||
			record.Verdict == provider.VerdictUnverifiedSuccess {
			stats.Successes++
		}
	}
	if stats.Samples == 0 {
		return stats
	}
	sort.Ints(turns)
	sort.Ints(tokens)
	stats.MedianTurns = turns[len(turns)/2]
	stats.MedianTokens = tokens[len(tokens)/2]
	stats.AverageCost = cost / float64(stats.Samples)
	return stats
}

// Measure summarises the recorded work.
func (p *Profile) Measure() Spread {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	turns := make([]int, 0, len(p.Records))
	tokens := make([]int, 0, len(p.Records))
	var spread Spread
	for _, record := range p.Records {
		if !record.rulerEvidence() {
			continue
		}
		turns = append(turns, record.Turns)
		tokens = append(tokens, record.Tokens)
		spread.Samples++
		if record.Overran() {
			spread.Overran++
		}
	}
	if spread.Samples == 0 {
		return Spread{}
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
	sorted := make([]Record, 0, len(p.Records))
	for _, record := range p.Records {
		if record.rulerEvidence() {
			sorted = append(sorted, record)
		}
	}
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
