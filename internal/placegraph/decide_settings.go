package placegraph

// The deciding settings of a place (design: Places, "Always ask me" and the
// threshold): whether codeaf asks before it decides anything in this place's
// chats, and how sure it must be before it decides without asking.
//
// THESE ARE NOT A POLICY FIELD. Policy conflicts between places are resolved by
// a person's pick (resolve.go); a deciding setting never conflicts, because the
// NEAREST ANCESTOR THAT HAS AN EXPLICIT VALUE answers. A place with no value of
// its own (Place.Decide == nil) simply has no opinion, so files written before
// this existed need no rewrite: they migrate to the default on read.

import (
	"fmt"
	"time"
)

// Decide is one explicit choice, always stored as a pair so "always ask" and the
// threshold travel together and one undo token takes both back.
type Decide struct {
	AlwaysAsk bool `json:"alwaysAsk"`
	Threshold int  `json:"threshold"`
}

// Bounds and the migration default, in percent.
const (
	DecideThresholdDefault = 90
	DecideThresholdMin     = 1
	DecideThresholdMax     = 100
	// DecideLevels caps the walk up the graph, a guard against a damaged file.
	DecideLevels = 64
)

// DefaultDecide is what a place with no explicit ancestor answers.
func DefaultDecide() Decide { return Decide{AlwaysAsk: false, Threshold: DecideThresholdDefault} }

func validateDecide(d Decide) error {
	if d.Threshold < DecideThresholdMin || d.Threshold > DecideThresholdMax {
		return fmt.Errorf("%w: decide threshold %d outside %d–%d", ErrInvalid, d.Threshold, DecideThresholdMin, DecideThresholdMax)
	}
	return nil
}

// SetDecide states a place's own deciding settings. It refuses an unknown or
// archived place and a threshold out of bounds; writing the value already held
// changes nothing and returns a zero Receipt.
func (s *Store) SetDecide(id string, d Decide) (Receipt, error) {
	return s.mutate(func(st *State, _ time.Time) (*change, error) {
		p, err := st.needPlace(id)
		if err != nil {
			return nil, err
		}
		if err := validateDecide(d); err != nil {
			return nil, err
		}
		if p.Decide != nil && *p.Decide == d {
			return nil, nil
		}
		p.Decide = &d
		return &change{ActionDecide, id}, nil
	})
}

// ClearDecide removes a place's own choice so it inherits again.
func (s *Store) ClearDecide(id string) (Receipt, error) {
	return s.mutate(func(st *State, _ time.Time) (*change, error) {
		p, err := st.needPlace(id)
		if err != nil {
			return nil, err
		}
		if p.Decide == nil {
			return nil, nil
		}
		p.Decide = nil
		return &change{ActionDecide, id}, nil
	})
}

// EffectiveDecide answers for a place: its own value, else the nearest ancestor's
// (level by level, parents in order, so the first parent wins a tie), else the
// default. fromID names the place the answer came from, empty for the default, so
// the settings row can say "from Software". Unknown places answer the default.
func (s *Snapshot) EffectiveDecide(id string) (d Decide, fromID string) {
	seen := map[string]bool{id: true}
	frontier := []string{id}
	for level := 0; level <= DecideLevels && len(frontier) > 0; level++ {
		var next []string
		for _, n := range frontier {
			i, ok := s.byID[n]
			if !ok {
				continue
			}
			p := s.Places[i]
			// An archived ancestor lends nothing, like context, but its own
			// parents still count as further up.
			if p.Decide != nil && (level == 0 || !p.Archived) {
				return *p.Decide, p.ID
			}
			for _, par := range p.Parents {
				if !seen[par] {
					seen[par] = true
					next = append(next, par)
				}
			}
		}
		frontier = next
	}
	return DefaultDecide(), ""
}
