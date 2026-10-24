package main

// C27.3 requires an older host without moving-chat support to be retired
// before a new window relies on that capability.

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/buildinfo"
	"github.com/Agent-Field/codeaf/internal/enginehost"
	"github.com/Agent-Field/codeaf/internal/remote"
)

func sameBuildHost(cells, busy bool) remote.HostSelf {
	return remote.HostSelf{Version: remote.Version, Build: buildinfo.Identity(), Cells: cells, Busy: busy}
}

// C27.7 exercises this behaviour without an opt-in switch.
func TestAnIdleEngineInTheOtherCellsModeIsRestarted(t *testing.T) {
	t.Setenv("CODEAF_SYNC_URL", "")
	shortEngineHome(t)
	workspace := "/home/somebody/api"
	standIn(t, workspace, sameBuildHost(false, false), false)
	me := thisEngineBuild()
	me.Cells = true

	note, err := clearStaleEngineHostAs(workspace, me)
	if err != nil {
		t.Fatalf("an idle engine in the other mode was refused: %v", err)
	}
	if !strings.Contains(note, "without moving chats") || !strings.Contains(note, "moving chats available now") {
		t.Fatalf("the restart line does not name both modes: %q", note)
	}
	if conn, err := enginehost.Dial(workspace); err == nil {
		_ = conn.Close()
		t.Fatal("the engine in the other mode was still answering")
	}
}

// C27.7 exercises this behaviour without an opt-in switch.
func TestABusyEngineInTheOtherCellsModeIsRefusedAndLeftRunning(t *testing.T) {
	t.Setenv("CODEAF_SYNC_URL", "")
	shortEngineHome(t)
	workspace := "/home/somebody/api"
	standIn(t, workspace, sameBuildHost(false, true), false)
	me := thisEngineBuild()
	me.Cells = true

	_, err := clearStaleEngineHostAs(workspace, me)
	if err == nil {
		t.Fatal("a busy engine in the other mode was attached to in silence")
	}
	for _, want := range []string{"without moving chats", "codeaf engine --stop --workspace " + workspace} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal %q does not say %q", err, want)
		}
	}
	if conn, err := enginehost.Dial(workspace); err != nil {
		t.Fatalf("a busy engine was stopped: %v", err)
	} else {
		_ = conn.Close()
	}
}

// C27.7 exercises this behaviour without an opt-in switch.
func TestAnEngineInTheSameCellsModeIsJoinedWithoutAWord(t *testing.T) {
	t.Setenv("CODEAF_SYNC_URL", "")
	shortEngineHome(t)
	workspace := "/home/somebody/api"
	standIn(t, workspace, sameBuildHost(true, true), false)
	me := thisEngineBuild()
	me.Cells = true

	note, err := clearStaleEngineHostAs(workspace, me)
	if err != nil || note != "" {
		t.Fatalf("a matching engine was not joined quietly: %q, %v", note, err)
	}
}
