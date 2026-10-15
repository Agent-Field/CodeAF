package main

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

// EVERY agent.changed SAYS WORKING WHILE A TURN RUNS, WITH ITS LIVE LINE,
// whichever road said it: here a rename, which knows nothing of the turn.
func TestAgentChangedIsDressedWithWorkingAndItsLine(t *testing.T) {
	backend, rec, agent := workBackend(t, &fakeConversation{})
	a, _ := backend.find(agent.ID)
	if _, _, err := backend.heldLive(agent.ID); err != nil {
		t.Fatal(err)
	}
	backend.locked(func() {
		a.live.running = true
		a.step = "Reading main.go"
		a.view.State = appwire.StateIdle
	})
	backend.sayAgent(a)
	rec.waitFor(t, 2*time.Second, func(event string, data any) bool {
		c, ok := data.(appwire.AgentChanged)
		return ok && c.Agent.ID == agent.ID && c.Agent.State == appwire.StateWorking && c.Agent.Line == "Reading main.go"
	})
	listed, err := backend.ListAgents("")
	if err != nil {
		t.Fatal(err)
	}
	for _, view := range listed {
		if view.ID == agent.ID && (view.State != appwire.StateWorking || view.Line != "Reading main.go") {
			t.Fatalf("the list says %+v", view)
		}
	}
}

// A PROCESS ID NAMES ONE PROCESS ACROSS RESTARTS: the job's number starts
// again at 1 in a new conversation, and its start time tells them apart.
func TestProcessIDsAreUniqueAcrossRestarts(t *testing.T) {
	first := session.JobNotice{ID: 1, Started: time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)}
	again := session.JobNotice{ID: 1, Started: first.Started.Add(time.Hour)}
	one, two := appProcessID("a", first), appProcessID("a", again)
	if one == two || !strings.HasPrefix(one, "a:p1.") {
		t.Fatalf("ids %q and %q", one, two)
	}
	agent, job, stamp, ok := appProcessJob(one)
	if !ok || agent != "a" || job != 1 || stamp != appProcessStamp(first) {
		t.Fatalf("parsed %q, %d, %q, %v", agent, job, stamp, ok)
	}
	if _, job, stamp, ok := appProcessJob("a:p7"); !ok || job != 7 || stamp != "" {
		t.Fatal("an id without a stamp is still read")
	}
}
