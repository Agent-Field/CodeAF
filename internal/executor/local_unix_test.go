//go:build !windows

// K6 keeps Unix process, lock and mode assertions on the systems that provide them.
package executor

import (
	"context"
	"syscall"
	"testing"
	"time"
)

func TestLocalRecordsALeftRunningChildAsAService(t *testing.T) {
	res := exec1(t, sh("sleep 30 >/dev/null 2>&1 &"), nil)
	if len(res.Services) != 1 {
		t.Fatalf("want one service, got %+v", res.Services)
	}
	pgid := res.Services[0].PGID
	t.Cleanup(func() { _ = syscall.Kill(-pgid, syscall.SIGKILL) })
	if res.Wall > 5*time.Second {
		t.Fatalf("a background child held the call for %v", res.Wall)
	}
}

func TestCommandBuildsAnUnstartedCommandInItsOwnGroup(t *testing.T) {
	cmd, err := Local{Root: t.TempDir()}.Command(context.Background(), sh("true"))
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Process != nil || cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setpgid {
		t.Fatalf("want an unstarted command with Setpgid, got %+v", cmd)
	}
}
