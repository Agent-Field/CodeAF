package factorytest

import (
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// WHAT A TEST OUTSIDE THIS PACKAGE NEEDS OF A SCENARIO. cmd/codeaf drives a
// scenario's job through the manager's real door and the app's real press
// (cmd/codeaf's factory_manager_join_test.go). It reaches the service by its
// address, as the app does, and scripts the delegate and the remote through
// the few doors below.

// URL is where the scenario's service listens now. Opening the service again
// ([Scenario.SignIn], [Scenario.RestartService]) moves it.
func (s *Scenario) URL() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.server == nil {
		return ""
	}
	return s.server.URL
}

// HoldRound scripts step's next round to begin and then wait until it is
// released. round is what it does once released. It answers a channel that is
// closed when the round has begun, and the release, which may be called twice.
func (s *Scenario) HoldRound(step string, round Round) (entered <-chan struct{}, release func()) {
	began, hold := make(chan struct{}), make(chan struct{})
	round.entered, round.hold = began, hold
	s.Delegate.Next(step, round)
	released := false
	return began, func() {
		if !released {
			released = true
			close(hold)
		}
	}
}

// Begun waits for a round held by [Scenario.HoldRound] to begin, with the
// service settling meanwhile as an open job page settles it.
func (s *Scenario) Begun(entered <-chan struct{}) {
	s.T.Helper()
	deadline := time.After(30 * time.Second)
	for {
		select {
		case <-entered:
			return
		case <-deadline:
			s.T.Fatalf("the round the scenario holds never began\n%s", s.Story())
		case <-time.After(tickEvery):
			s.Tick()
		}
	}
}

// Branch makes a branch on the scenario's remote at the head of main, for a
// pull request to change its base to.
func (s *Scenario) Branch(name string) {
	s.T.Helper()
	s.git(s.bare, "branch", name, "main")
}

// WaitKey is a job's stored wait in a few words ("you:approve_acts",
// "github:checks"), as a scenario's story prints it.
func WaitKey(it factory.Item) string { return waitKey(it) }

// Sequence is every round the delegate has run so far, oldest first, as
// "write#1 task#1 test#1".
func (d *Delegate) Sequence() string { return strings.Join(d.ran(), " ") }
