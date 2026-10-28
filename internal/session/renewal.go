package session

import "time"

// RenewalState keeps health local to the writer and signals changes while idle.
type RenewalState interface {
	RenewalStatus() (time.Time, time.Duration, bool)
	RenewalChanges() <-chan struct{}
}

// RenewalStatus reads the chat's local renewal state without starting any work.
func (a *Agent) RenewalStatus() (time.Time, time.Duration, bool) {
	if a == nil || a.config.Renewals == nil {
		return time.Time{}, 0, false
	}
	return a.config.Renewals.RenewalStatus()
}

// RenewalChanges wakes the host when idle renewal health needs to cross the wire.
func (a *Agent) RenewalChanges() <-chan struct{} {
	if a == nil || a.config.Renewals == nil {
		return nil
	}
	return a.config.Renewals.RenewalChanges()
}
