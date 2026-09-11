package standing

import "reflect"

// Runtime receipts are observations, not replacement documents. The ticker is
// serialized by its own lock, while controls and configuration may change during
// the model call. Apply only the observed delta to the current owner document.
func (s *Store) recordRuntime(before, after Item) error {
	_, err := s.mutate(before.ID, func(current *Item) error {
		sameSchedule := reflect.DeepEqual(current.When, before.When)
		current.LastChecked = after.LastChecked
		current.LastCheckLine = after.LastCheckLine
		current.FailedChecks = after.FailedChecks
		current.SpentUSD += after.SpentUSD - before.SpentUSD
		if after.Runs > before.Runs {
			current.Runs += after.Runs - before.Runs
			current.LastFired = after.LastFired
			current.LastOutcome = after.LastOutcome
			current.LastRun = after.LastRun
			current.NeedsPerson = after.NeedsPerson
			current.Previous = after.Previous
			if after.CleanRuns == 0 {
				current.CleanRuns = 0
			} else {
				current.CleanRuns += after.CleanRuns - before.CleanRuns
			}
		}
		// Consume the observed occurrence even when an unrelated edit happened.
		// Only the schedule's own changes can replace its next due or baseline;
		// control and effort edits must not replay an already delivered action.
		if sameSchedule {
			if current.NextDue.Equal(before.NextDue) {
				current.NextDue = after.NextDue
			}
			if current.Fingerprint == before.Fingerprint {
				current.Fingerprint = after.Fingerprint
			}
			mayRetire := after.RetiredWhy != "expired"
			if deadline, has := expiryOf(*current); has && !after.LastChecked.Before(deadline) {
				mayRetire = true
			}
			if mayRetire && after.Status == StatusRetired && current.Status == before.Status {
				current.Status = after.Status
				current.RetiredWhy = after.RetiredWhy
			}
		}
		return nil
	})
	return err
}

// currentAdmission is the last local check before a new action. A pause, stop,
// or edit invalidates an earlier probe's decision, including pause then resume.
// This does not interrupt an action that has already entered its runner.
func (s *Store) currentAdmission(item Item) (bool, error) {
	current, err := s.Get(item.ID)
	if err != nil {
		return false, err
	}
	return current.Status == StatusActive && current.Revision == item.Revision, nil
}
