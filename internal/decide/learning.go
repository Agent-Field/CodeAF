package decide

import (
	"fmt"
	"time"
)

// GraduateAt is how many of the last RingSize outcomes must be agreements
// before a kind leaves learning and starts deciding. Iteration 2 (I2.8): a
// place graduates at 18 of its last 20, per kind of question. The ring has to
// be full. Eighteen agreements out of eighteen answers is not enough.
const GraduateAt = 18

// KindKey is one kind of question for the learning ring: the ask kind, plus
// the subject class when the kind has one. A permission to read the shell is
// "permission:shell-read"; a choice with no class is "choice". Two classes of
// the same ask kind are different kinds, so an overturn of one does not touch
// the other. The class token is the caller's. This package does not decide
// that a command is a shell read or a git write.
func KindKey(askKind, subjectClass string) string {
	if subjectClass == "" {
		return askKind
	}
	return askKind + ":" + subjectClass
}

// ProposalAnswer is a person answering a proposal: the key the place offered,
// and the key the person actually chose.
type ProposalAnswer struct {
	AskKind      string
	SubjectClass string
	ProposedKey  string
	ChosenKey    string
	At           time.Time
	// DecisionID is optional. A proposal is not yet a ledger decision.
	DecisionID string
}

// RecordProposal writes the answer onto that kind's ring. Agreed means the
// chosen key is the proposed key; any other key is overruled. A full ring with
// at least GraduateAt agreements leaves learning for deciding, in the same
// write as the answer that crossed the line.
//
// A kind that is already deciding, or set to always ask, keeps its mode. Only
// learning graduates, and only an overturn of a decided item sends a deciding
// kind back.
func (s *Store) RecordProposal(a ProposalAnswer) (KindState, error) {
	if a.AskKind == "" || a.ProposedKey == "" || a.ChosenKey == "" {
		return KindState{}, fmt.Errorf("%w: proposal needs an ask kind, a proposed key and a chosen key", ErrInvalid)
	}
	key := KindKey(a.AskKind, a.SubjectClass)
	at := a.At
	if at.IsZero() {
		at = s.now()
	}
	var st KindState
	err := s.update(func(c *doc) error {
		pushOutcome(c, key, Outcome{
			DecisionID: a.DecisionID,
			Agreed:     a.ChosenKey == a.ProposedKey,
			At:         at,
		})
		st = c.Modes[key]
		if st.Mode == ModeLearning && len(st.Recent) >= RingSize && Agreements(st) >= GraduateAt {
			st.Mode = ModeDeciding
			c.Modes[key] = st
		}
		st.Recent = append([]Outcome(nil), st.Recent...)
		return nil
	})
	return st, err
}

// Agreements is how many outcomes in the ring chose the proposed key.
func Agreements(st KindState) int {
	n := 0
	for _, o := range st.Recent {
		if o.Agreed {
			n++
		}
	}
	return n
}

// LearningCount is the tray's count for one kind, "14 of 20". The denominator
// is the graduation window, not how many answers have arrived, so fourteen
// agreements read "14 of 20" on the way to eighteen. Nothing agreed yet is an
// empty string: a count of zero is not drawn.
func LearningCount(st KindState) string {
	n := Agreements(st)
	if n == 0 {
		return ""
	}
	return fmt.Sprintf("%d of %d", n, RingSize)
}

// OverturnDecided marks a decision reversed. When that kind is deciding, it
// puts only that kind back in learning and clears its ring, so the place has
// to re-earn 18 of 20 (P-9). Every other kind is left as it is.
//
// A kind that is still learning, or set to always ask, keeps its ring. The
// drop-back is for a kind that had started deciding. The decision is still
// stamped, and a second call keeps the first time.
//
// This does not go through Store.Overturn. That door notes the reversal on
// the ask-kind ring and leaves the mode, which is the ledger's own record.
// Decision.Subject is the subject class, the same token RecordProposal was
// given, so the kind cleared is the kind that earned the decision.
func (s *Store) OverturnDecided(id string, at time.Time) error {
	if id == "" {
		return fmt.Errorf("%w: empty decision", ErrInvalid)
	}
	if at.IsZero() {
		at = s.now()
	}
	return s.update(func(c *doc) error {
		for i := range c.Decisions {
			d := &c.Decisions[i]
			if d.ID != id {
				continue
			}
			if d.OverturnedAt == nil {
				t := at
				d.OverturnedAt = &t
			}
			key := KindKey(d.AskKind, d.Subject)
			if c.Modes[key].Mode == ModeDeciding {
				st := c.Modes[key]
				st.Mode = ModeLearning
				st.Recent = nil
				c.Modes[key] = st
			}
			return nil
		}
		return ErrNotFound
	})
}
