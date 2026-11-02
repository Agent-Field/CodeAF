package main

import (
	"context"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/tui3"
)

// L4-B11 and stability: a never-paired computer does not gain sync progress,
// and a second quit interrupt keeps status 130 even with a current engine.
func TestL4Fix2SoloQuitKeepsBaseline(t *testing.T) {
	for _, host := range []string{"", "desk-b"} {
		t.Run(map[bool]string{false: "solo computer", true: "paired viewer of solo engine"}[host != ""], func(t *testing.T) {
			local := t.TempDir()
			t.Setenv("CODEAF_HOME", local)
			if _, err := identity.EnsureSolo(local); err != nil {
				t.Fatal(err)
			}
			if host != "" {
				if err := identity.EndSolo(local); err != nil {
					t.Fatal(err)
				}
			}
			t.Cleanup(cell.UseAvailabilityInTests(false))
			oldRun, oldExit := runSurfaceProgram, surfaceForceExit
			defer func() {
				runSurfaceProgram, surfaceForceExit = oldRun, oldExit
				stopFinalSaveProgress()
				takeFinalSaveFailure()
			}()
			restored, code := false, 0
			runSurfaceProgram = func(_ context.Context, o tui3.Options) error {
				if o.SyncService != "" {
					t.Errorf("solo startup advertised sync: %q", o.SyncService)
				}
				o.SavingCompatibility(func() bool { return true })
				o.SavingDurability(func() bool { return true })
				o.ForceQuit()
				restored = true
				return nil
			}
			surfaceForceExit = func(got int) {
				if !restored {
					t.Error("exit before terminal restoration")
				}
				code = got
			}
			if err := runSurface(context.Background(), tui3.Options{ProfileDir: local, Host: host}); err != nil {
				t.Fatal(err)
			}
			if code != 130 {
				t.Fatalf("second interrupt exit=%d", code)
			}
		})
	}
}
