package main

import "testing"

func TestVersionHelpAndManualSkipAdoptionAndTimerCleanup(t *testing.T) {
	for _, args := range [][]string{
		{"--version"}, {"version"}, {"-v"}, {"-h"}, {"--help"}, {"help"}, {"help", "env"},
		{"manual"}, {"manual", "starting-codeaf"}, {"manual", "how do I pair"},
		{"chat", "--help"}, {"devices", "revoke", "--help"}, {"cluster", "lead", "-h"},
		{"plan", "new", "-help"}, {"senior-dev", "help"}, {"senior-dev", "run", "--help"},
	} {
		if startupMaintenanceNeeded(args) {
			t.Errorf("informational invocation %q would adopt the home or remove timers", args)
		}
	}
}

func TestRealStartsKeepAdoptionAndTimerCleanup(t *testing.T) {
	for _, args := range [][]string{
		nil, {"chat"}, {"resume"}, {"devices"}, {"cluster", "status"}, {"cluster", "lead"},
		{"pair"}, {"engine", "--status"}, {"tick"}, {"wake"}, {"update", "--version", "v1.2.3"},
		{"do", "explain help"}, {"do", "--", "--help"}, {"patch", "--", "--help"},
	} {
		if !startupMaintenanceNeeded(args) {
			t.Errorf("real invocation %q skipped startup maintenance", args)
		}
	}
}
