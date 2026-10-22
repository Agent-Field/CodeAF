package selector

import (
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/Agent-Field/codeaf/internal/pool/contract"
)

const (
	// UCBAlpha is the credibility half-width for the entrance gate.
	UCBAlpha = 1.28
	// CredibilityFloor is the gate fraction of the top UCB.
	CredibilityFloor = 0.8
)

// Candidate is one model under consideration for a seat, with its prior.
type Candidate struct {
	Model  *Model
	Prior  prior
	Q      float64 // posterior mean
	Sigma  float64 // posterior sigma
	Cost   float64 // $ per seat instance (ascending bill basis)
	UCB    float64
	LCB    float64
	Passes bool // capability + credibility gate
}

// SeatDecision is the selector's output for one seat of one class.
type SeatDecision struct {
	Class        string
	Role         string
	Chosen       string // chosen model id
	ChosenQ      float64
	ChosenCost   float64
	ChosenSigma  float64
	ChosenPrior  string // scored|line|vendor|global
	Candidates   int
	PassedCount  int
	TopLCB       float64
	TopUCB       float64
	Bill         float64 // volume-weighted bill for the class
	AllInherited bool    // every passed candidate has an inherited prior
	Event        *contract.DecisionEvent
}

// Engine runs decisions for the five classes against a snapshot + catalog.
type Engine struct {
	Snap    *SharedSnapshot
	Cat     *ModelCatalog
	Classes []ClassShape
	priors  *PriorEngine
	// eventSink receives every DecisionEvent the engine emits.
	eventSink []*contract.DecisionEvent
}

func NewEngine(s *SharedSnapshot, c *ModelCatalog, classes []ClassShape) *Engine {
	if classes == nil {
		classes = DefaultClasses()
	}
	return &Engine{Snap: s, Cat: c, Classes: classes, priors: NewPriorEngine(s, c)}
}

// Events returns the shadow DecisionEvents emitted so far.
func (e *Engine) Events() []*contract.DecisionEvent { return e.eventSink }

// capGate checks the capability gates for a class.
func capGate(m *Model, cs ClassShape) bool {
	// A ROW THAT PUBLISHES NO WINDOW IS NOT REFUSED FOR IT — the same law the
	// live seats keep (internal/crewroute's seatContext): zero is the
	// provider not saying, and the candidates were already priced and
	// health-checked by the caller that assembled the catalog.
	if m.ContextLength > 0 && m.ContextLength < cs.MinContext {
		return false
	}
	if cs.NeedsTools && !m.HasToolUse() {
		return false
	}
	if cs.NeedsImages && !m.HasImages() {
		return false
	}
	return true
}

// seatCost is the per-instance dollar cost of one seat: prompt (with cache
// share) plus completion. The cache share is a schema-field approximation:
// when the model publishes a cache-read price we credit it on 50% of prompt
// tokens, else the prompt price applies to the whole prompt.
func seatCost(m *Model, cs ClassShape) float64 {
	cacheShare := 0.5
	prompt := m.PromptPrice
	if m.CacheRead > 0 {
		prompt = (1-cacheShare)*m.PromptPrice + cacheShare*m.CacheRead
	}
	completion := m.Completion
	if completion == 0 {
		completion = m.PromptPrice * 3
	}
	return prompt + completion
}

// DecideClass seats one class. Returns the SeatDecision and emits a shadow
// DecisionEvent on the contract channel.
func (e *Engine) DecideClass(cs ClassShape) (SeatDecision, error) {
	var cands []Candidate
	for i := range e.Cat.Models {
		m := &e.Cat.Models[i]
		if !capGate(m, cs) {
			continue
		}
		p := e.priors.PriorFor(cs.Role, m.ID)
		ucb := p.Q + UCBAlpha*p.Sigma
		lcb := p.Q - UCBAlpha*p.Sigma
		cands = append(cands, Candidate{
			Model: m, Prior: p, Q: p.Q, Sigma: p.Sigma,
			Cost: seatCost(m, cs), UCB: ucb, LCB: lcb,
		})
	}
	if len(cands) == 0 {
		return SeatDecision{}, fmt.Errorf("class %s: no candidate passes capability gates", cs.ID)
	}
	// credibility floor on UCB
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].UCB != cands[j].UCB {
			return cands[i].UCB > cands[j].UCB
		}
		return cands[i].Cost < cands[j].Cost
	})
	topUCB := cands[0].UCB
	floor := CredibilityFloor * topUCB
	passed := 0
	allInherited := true
	for i := range cands {
		cands[i].Passes = cands[i].UCB >= floor
		if cands[i].Passes {
			passed++
			if cands[i].Prior.Source == "scored" {
				allInherited = false
			}
		}
	}
	// rank on LCB among passers
	sort.Slice(cands, func(i, j int) bool {
		pi, pj := cands[i].Passes, cands[j].Passes
		if pi != pj {
			return pi
		}
		if cands[i].LCB != cands[j].LCB {
			return cands[i].LCB > cands[j].LCB
		}
		return cands[i].Cost < cands[j].Cost
	})
	// vendor rule: the chosen crew must not double up a vendor across the
	// high and worker seats; handled at the crew level in Front.
	chosen := cands[0]
	topLCB := chosen.LCB

	// volume-weighted bill for the class across passers
	bill := 0.0
	n := 0
	for i := range cands {
		if !cands[i].Passes {
			continue
		}
		bill += cands[i].Cost
		n++
	}
	if n > 0 {
		bill = bill * cs.Volume / float64(n)
	}

	ev := e.emitEvent(cs, cands, chosen)
	sd := SeatDecision{
		Class: cs.ID, Role: cs.Role,
		Chosen: chosen.Model.ID, ChosenQ: chosen.Q, ChosenCost: chosen.Cost,
		ChosenSigma: chosen.Sigma, ChosenPrior: chosen.Prior.Source,
		Candidates: len(cands), PassedCount: passed,
		TopLCB: topLCB, TopUCB: topUCB, Bill: bill,
		AllInherited: allInherited, Event: ev,
	}
	return sd, nil
}

func (e *Engine) emitEvent(cs ClassShape, cands []Candidate, chosen Candidate) *contract.DecisionEvent {
	// Probabilities is a map keyed by model id over the candidate set,
	// summing to 1 within tolerance and naming exactly the candidates.
	// Passers get mass proportional to their (clamped) LCB; non-passers 0.
	probs := make(map[string]float64, len(cands))
	for _, c := range cands {
		if c.Passes {
			w := c.LCB
			if w < 0.01 {
				w = 0.01
			}
			probs[c.Model.ID] = w
		} else {
			probs[c.Model.ID] = 0.0
		}
	}
	sum := 0.0
	for _, p := range probs {
		sum += p
	}
	if sum <= 0 {
		for k := range probs {
			probs[k] = 1.0 / float64(len(probs))
		}
	} else {
		for k := range probs {
			probs[k] /= sum
		}
	}
	ev := &contract.DecisionEvent{
		SchemaVersion:  contract.DecisionEventVersion,
		RunID:          "lane-c-shadow",
		NodeID:         cs.ID,
		DecisionTime:   time.Now().UTC(),
		FeatureVersion: "selector-v1",
		Features: map[string]string{
			"role":                cs.Role,
			"task_class":          cs.ID,
			"context_size_tokens": itoa(chosen.Model.ContextLength),
		},
		Candidates:     make([]contract.Candidate, len(cands)),
		Probabilities:  probs,
		SelectedAction: chosen.Model.ID,
		PolicyVersion:  "lane-c/selector-v1",
		CatalogVersion: "model-catalog-2026-10-20",
		BudgetEnvelope: contract.BudgetEnvelope{Currency: "usd"},
	}
	for i, c := range cands {
		ev.Candidates[i] = contract.Candidate{ModelID: c.Model.ID}
	}
	e.eventSink = append(e.eventSink, ev)
	return ev
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// Front returns the quality records for the class, sorted by ascending bill,
// restricted to the passing candidates (front = quality records by ascending
// bill).
type QualityRecord struct {
	Model string
	Q     float64
	Cost  float64
	LCB   float64
	Prior string
}

func (e *Engine) Front(cs ClassShape) []QualityRecord {
	var recs []QualityRecord
	for i := range e.Cat.Models {
		m := &e.Cat.Models[i]
		if !capGate(m, cs) {
			continue
		}
		p := e.priors.PriorFor(cs.Role, m.ID)
		ucb := p.Q + UCBAlpha*p.Sigma
		if ucb < CredibilityFloor*math.Max(0.0001, 1) {
			continue
		}
		if p.Q == 0 && p.Sigma == 0 {
			continue
		}
		recs = append(recs, QualityRecord{
			Model: m.ID, Q: p.Q, Cost: seatCost(m, cs),
			LCB: p.Q - UCBAlpha*p.Sigma, Prior: p.Source,
		})
	}
	sort.Slice(recs, func(i, j int) bool {
		if recs[i].Cost != recs[j].Cost {
			return recs[i].Cost < recs[j].Cost
		}
		return recs[i].Q > recs[j].Q
	})
	return recs
}

// Crew is one fully-seated class: a chosen model per seat role.
type Crew struct {
	Class string
	Seats map[string]string // role -> model id
}

// Presets returns the preset crews: those classes whose seats are all
// verified (no fully-inherited seat). A class whose chosen seat has an
// inherited prior is not preset-worthy.
func (e *Engine) Presets(decisions []SeatDecision) []Crew {
	var crews []Crew
	for _, d := range decisions {
		if d.AllInherited {
			continue
		}
		crews = append(crews, Crew{Class: d.Class, Seats: map[string]string{d.Role: d.Chosen}})
	}
	sort.Slice(crews, func(i, j int) bool { return crews[i].Class < crews[j].Class })
	return crews
}
