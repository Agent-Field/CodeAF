package main

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/enginehost"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// `codeaf chat --host` runs `codeaf engine` under a non-interactive ssh shell,
// which rarely carries the CODEAF_SYNC_URL the person exported in their own
// terminal. That launch named no relay, so it must join the host the terminal
// started — idle or busy — rather than retiring it or being refused by it;
// otherwise the two launches take the workspace from each other in turn.
func TestC10ALaunchThatNamesNoRelayJoinsTheHostATerminalStarted(t *testing.T) {
	for _, busy := range []bool{false, true} {
		t.Run(fmt.Sprintf("busy=%t", busy), func(t *testing.T) {
			workspace, _ := environmentTestHome(t)
			t.Setenv(syncsetup.URLVar, "http://127.0.0.1:41001")
			child := environmentChild(t, workspace, os.Getenv(config.APIKeyEnv))
			if busy {
				environmentWindow(t, workspace, "")
			}
			waitEnvironmentHost(t, workspace, func(self remote.HostSelf) bool { return self.Busy == busy })
			t.Setenv(syncsetup.URLVar, "")
			if enginehost.SyncDiffers(workspace) || enginehost.EnvironmentDiffers(workspace) {
				t.Fatal("a launch that named no relay reads the host's own variable as a different setting")
			}
			note, err := clearStaleEngineHost(workspace)
			if err != nil || note != "" {
				t.Fatalf("a launch that named no relay displaced the host: note=%q err=%v", note, err)
			}
			self, err := enginehost.Ask(workspace, remote.WhoIs{})
			if err != nil || self.PID != child.Process.Pid {
				t.Fatalf("the host started by the terminal was replaced: %+v %v", self, err)
			}
		})
	}
}

// Naming no relay is not a way to miss a real change: when the address a
// pairing saved moves, both sides read it from the same home, and an idle
// host still restarts to pick it up.
func TestC10AChangedSavedRelayStillRetiresAnIdleHost(t *testing.T) {
	workspace, _ := environmentTestHome(t)
	t.Setenv(syncsetup.URLVar, "")
	if err := syncsetup.SaveRelayURL(os.Getenv("CODEAF_HOME"), "http://127.0.0.1:41001"); err != nil {
		t.Fatal(err)
	}
	environmentChild(t, workspace, os.Getenv(config.APIKeyEnv))
	waitEnvironmentHost(t, workspace, func(self remote.HostSelf) bool { return !self.Busy })
	if err := syncsetup.SaveRelayURL(os.Getenv("CODEAF_HOME"), "http://127.0.0.1:41002"); err != nil {
		t.Fatal(err)
	}
	note, err := clearStaleEngineHost(workspace)
	if err != nil || !strings.Contains(note, "restarted") || !nothingHolds(workspace) {
		t.Fatalf("an idle host kept a relay the pairing replaced: note=%q err=%v", note, err)
	}
}
