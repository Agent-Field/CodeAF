package service

// repair.go is the one way a kept-running service changes what it runs: its
// command, its folder or its check, in place, on the same row and under the
// same id. Keeping the changed command instead ([Supervisor.Keep]) makes a
// second row beside the failed one, and the person is left with a service that
// says failed for ever and a new one they never connected to it.
//
// A REPAIR IS A NEW YES, NEVER AN OLD ONE STRETCHED. The yes a person gave the
// old command was for that command in that folder with that check; nothing of
// it carries to the replacement. Repair banks the yes for the NEW spec exactly
// as Keep does, and a caller asks the person first unless that exact spec was
// already approved ([Supervisor.Approved]).
//
// A REPAIR NEVER ENDS A PROCESS. Only a service with no process — failed, gave
// up, not running, stopped — is repaired; one that is running, starting or
// still stopping is refused with nothing written and nothing signalled, and
// the person stops it first. That is what makes a duplicate impossible: the
// replacement can never run beside the process it replaces.

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrDuplicate is a repair whose new command and folder another of the same
// agent's services already runs. Two rows of one owner under one consent key
// would make every later Keep of that spec pick one of them at random.
var ErrDuplicate = errors.New("another kept-running process of this agent already has that command in that folder")

// Repairable answers, with no side effect at all, whether owner's service id
// could be repaired to spec now, with the row as it is. It is what a caller
// asks BEFORE it raises a card, so a refusal costs the person nothing; the
// same refusals are made again by [Supervisor.Repair], because the answer can
// change while the card waits.
//
// An id that is not owner's is [ErrNotFound], exactly as an id that does not
// exist: one agent never learns another's ids.
func (s *Supervisor) Repairable(owner, id string, spec Spec) (Row, error) {
	spec, err := spec.Check()
	if err != nil {
		return Row{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	e, err := s.repairRefusalLocked(owner, id, spec)
	if err != nil {
		return Row{}, err
	}
	return s.rowLocked(e), nil
}

// repairRefusalLocked is every reason owner's service id cannot be repaired
// to spec now, or its entry.
func (s *Supervisor) repairRefusalLocked(owner, id string, spec Spec) (*entry, error) {
	if s.closed {
		return nil, ErrClosed
	}
	e, err := s.findLocked(owner, id)
	if err != nil {
		return nil, err
	}
	if s.fences[owner] > 0 {
		return nil, ErrClosing
	}
	if e.stopping() {
		return nil, ErrStopping
	}
	if e.rec.Identity != nil || e.busy {
		return nil, ErrRunning
	}
	key := spec.ConsentKey()
	for _, other := range s.entries {
		if other != e && other.rec.Owner == owner && other.rec.Spec.ConsentKey() == key {
			return nil, ErrDuplicate
		}
	}
	return e, nil
}

// Repair is a person's yes to a changed spec for one of owner's services that
// has no process, carried out on that same row: the yes is banked for the new
// spec, the row takes the new command, folder, name, check and policy and the
// new provenance, and the new command is started under the row's own id and
// log. When the new spec was already approved with this check, the person's
// policy stands whatever policy says, as for [Supervisor.Keep].
//
// What the row keeps, and why:
//
//   - The id, the log and the output in it. The log is the row's, cut back to
//     its usual bound before the new command writes after it, so what the old
//     command said about its own failure is still there to read.
//   - The open episode, under the same id. The trouble a person was told about
//     is over when the service is healthy again, not when somebody tried
//     something, so the episode closes on the repaired spec's first passing
//     check — and if the repair fails too, it is still the one line, saying
//     what first went wrong.
//
// What it does not keep:
//
//   - The automatic restart budget, when the command, folder or check changed.
//     Those restarts were spent on a spec nobody runs any more; the repaired
//     spec is a new thing a person approved and starts with its whole budget.
//     A repair that changes only the name is the same spec started again, and
//     keeps the budget it had, so asking again is never a way to refill it.
//   - The old check's last result, for the same reason.
//
// NOTHING IS ALLOCATED AND NOTHING IS SIGNALLED. The id counter does not move,
// no row is added, and a row with no process has nothing to end. A write that
// fails before the start puts the row and every banked yes back as they were
// and starts nothing.
func (s *Supervisor) Repair(owner, id string, spec Spec, policy Policy, from Provenance) (Row, error) {
	spec, err := spec.Check()
	if err != nil {
		return Row{}, err
	}
	if owner == "" || len(owner) > maxOwner || strings.TrimSpace(owner) != owner {
		return Row{}, errors.New("a kept-running process needs an owner")
	}
	if !policy.Valid() {
		return Row{}, fmt.Errorf("restart policy is never, on_failure or always, not %q", policy)
	}
	from.Label = oneLine(from.Label, maxProvenance)
	from.CallID = oneLine(from.CallID, maxProvenance)
	now := s.clock.Now()
	from.At = now

	s.mu.Lock()
	e, err := s.repairRefusalLocked(owner, id, spec)
	if err != nil {
		s.mu.Unlock()
		return Row{}, err
	}
	key := spec.ConsentKey()
	prior, hadPrior := s.consent[key]
	if hadPrior && prior.Health == spec.Health {
		policy = prior.Policy
	}
	changed := e.rec.Spec.ConsentKey() != key || e.rec.Spec.Health != spec.Health
	before := e.rec
	s.consent[key] = consent{Policy: policy, Health: spec.Health, At: now}
	evicted := s.trimConsentKeepingLocked(key)
	e.rec.Spec, e.rec.Policy, e.rec.From = spec, policy, from
	e.rec.StoppedBy, e.rec.Reason = "", ""
	e.rec.BackoffUntil = time.Time{}
	if changed {
		e.rec.Restarts, e.rec.Check, e.rec.HealthySince = nil, nil, time.Time{}
	}
	e.gen++
	gen := e.gen
	e.busy = true
	e.before = ""
	e.rec.State = StateStarting
	s.changedLocked(e, "state")
	s.wg.Add(1)
	s.mu.Unlock()
	defer s.wg.Done()

	if err := s.save(); err != nil {
		s.mu.Lock()
		if hadPrior {
			s.consent[key] = prior
		} else {
			delete(s.consent, key)
		}
		for evictedKey, c := range evicted {
			if _, taken := s.consent[evictedKey]; !taken {
				s.consent[evictedKey] = c
			}
		}
		if s.entries[id] == e {
			if e.gen == gen {
				e.busy = false
				e.gen++
				e.rec = before
			} else {
				// A stop overtook the write and its state stands; what this
				// repair put on the row goes back, so the row never names a
				// spec the file and the banked yes do not.
				e.rec.Spec, e.rec.Policy, e.rec.From = before.Spec, before.Policy, before.From
				e.rec.Restarts, e.rec.Check, e.rec.HealthySince = before.Restarts, before.Check, before.HealthySince
			}
			s.changedLocked(e, "state")
		}
		s.mu.Unlock()
		return Row{}, err
	}
	if err := s.startWork(id, gen, StartedByAgent, false); err != nil {
		row, _ := s.Get(owner, id)
		return row, err
	}
	return s.Get(owner, id)
}

// trimConsentKeepingLocked trims the banked yeses to their bound as
// trimConsentLocked does, never evicting keep, and answers what it evicted so
// a write that fails can put them back.
func (s *Supervisor) trimConsentKeepingLocked(keep string) map[string]consent {
	var evicted map[string]consent
	for len(s.consent) > maxConsent {
		var oldest string
		var at time.Time
		for key, c := range s.consent {
			if key == keep {
				continue
			}
			if oldest == "" || c.At.Before(at) {
				oldest, at = key, c.At
			}
		}
		if oldest == "" {
			break
		}
		if evicted == nil {
			evicted = map[string]consent{}
		}
		evicted[oldest] = s.consent[oldest]
		delete(s.consent, oldest)
	}
	return evicted
}
