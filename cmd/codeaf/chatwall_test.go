package main

import (
	"testing"
	"time"
)

// --max-hours ENDS THE PROCESS. Three `codeaf chat --no-host ... --max-hours
// 0.15` windows were alive forty-three hours after their nine-minute cap: the
// wall stopped their work and nothing ever closed the window. The cap now asks
// the process to leave a grace after the wall, and asks again — which the leave
// road answers by exiting at once — if the first ask has not finished it.
func TestTheMaxHoursWallEndsTheProcessAndThenInsists(t *testing.T) {
	wall := 9 * time.Minute
	var at []time.Duration
	var fire []func()
	after := func(d time.Duration, f func()) *time.Timer {
		at = append(at, d)
		fire = append(fire, f)
		return time.NewTimer(time.Hour)
	}
	leaves := 0
	cancel := armLaunchWall(wall, after, func() { leaves++ })
	defer cancel()

	if len(at) != 2 {
		t.Fatalf("the wall armed %d leaves, want the ordinary one and the insisting one", len(at))
	}
	if at[0] != wall+launchWallGrace || at[1] != wall+launchWallGrace+launchWallForce {
		t.Fatalf("the leaves are armed at %v, want %v and %v", at, wall+launchWallGrace, wall+launchWallGrace+launchWallForce)
	}
	// NOTHING THE PROCESS DOES CAN OUTLIVE THIS: the whole overrun is bounded.
	if limit := wall + 5*time.Minute; at[1] > limit {
		t.Fatalf("a capped session may outlive its cap by %v", at[1]-wall)
	}
	for _, f := range fire {
		f()
	}
	if leaves != 2 {
		t.Fatalf("the process was asked to leave %d times, want 2", leaves)
	}
}

// NO WALL, NO CLOCK: a session without --max-hours is the posture every
// session has always had, and nothing is armed.
func TestNoWallArmsNothing(t *testing.T) {
	armed := 0
	after := func(time.Duration, func()) *time.Timer { armed++; return nil }
	armLaunchWall(0, after, func() {})()
	if armed != 0 {
		t.Fatalf("a session with no wall armed %d leaves", armed)
	}
}
