package directory

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/clusterstate"
)

func TestR24SourceCrashHelper(t *testing.T) {
	path := os.Getenv("P11_SOURCE")
	if path == "" {
		return
	}
	s, e := OpenSQLite(path, func() time.Time { return time.UnixMilli(1000) })
	legacyMust(t, e)
	var initial *LegacyGate
	if os.Getenv("P11_STAGE") == "own" {
		initial, e = s.LegacyGate(context.Background())
		legacyMust(t, e)
	}
	if os.Getenv("P11_STAGE") == "freeze" {
		s.clock = func() time.Time { return time.UnixMilli(200000) }
		s.feed.clock = s.clock
		sub, e := s.Feed().Subscribe("a", []Hold{{Cell: "chat", Fence: 1}})
		legacyMust(t, e)
		sub.Ping()
	}
	phase := os.Getenv("P11_PHASE")
	s.migrationCut = func(p string) {
		if p == phase {
			os.Exit(81)
		}
	}
	switch os.Getenv("P11_STAGE") {
	case "freeze":
		e = s.FreezeLegacy(context.Background(), legacyConfig())
	case "inventory":
		_, e = s.InventoryLegacy(context.Background())
	case "own":
		e = s.OwnLegacy(context.Background(), []LegacyReceipt{{Node: 1, Manifest: *initial.Manifest, Applied: 2}, {Node: 2, Manifest: *initial.Manifest, Applied: 2}})
	}
	legacyMust(t, e)
	t.Fatal("source crash cut was not reached")
}

// R24 kills between source SQL stages and either side of each durable commit.
// Every replay uses the same UUID; a committed freeze never reopens the writer.
func TestR24SourceStageCrashesAndResume(t *testing.T) {
	for _, stage := range []string{"freeze", "inventory", "own"} {
		internal := map[string]string{"freeze": "after-freeze-gate", "inventory": "after-inventory-gate", "own": "after-owned-gate"}[stage]
		phases := []string{internal, "before-commit", "after-commit"}
		if stage == "freeze" {
			phases = append(phases, "after-freeze-hold")
		}
		if stage == "inventory" {
			phases = append(phases, "after-inventory-row")
		}
		for _, phase := range phases {
			t.Run(stage+"/"+phase, func(t *testing.T) {
				ctx := context.Background()
				path := filepath.Join(t.TempDir(), "source.db")
				s, _ := legacySeed(t, path)
				if stage != "freeze" {
					legacyMust(t, s.FreezeLegacy(ctx, legacyConfig()))
				}
				if stage == "own" {
					_, e := s.InventoryLegacy(ctx)
					legacyMust(t, e)
				}
				legacyMust(t, s.Close())
				cmd := exec.Command(os.Args[0], "-test.run=^TestR24SourceCrashHelper$", "-test.count=1")
				cmd.Env = append(os.Environ(), "P11_SOURCE="+path, "P11_STAGE="+stage, "P11_PHASE="+phase)
				out, e := cmd.CombinedOutput()
				var exit *exec.ExitError
				if !errors.As(e, &exit) || exit.ExitCode() != 81 {
					t.Fatalf("cut: %v %s", e, out)
				}
				s, e = OpenSQLite(path, func() time.Time { return time.UnixMilli(1000) })
				legacyMust(t, e)
				defer s.Close()
				g, e := s.LegacyGate(ctx)
				legacyMust(t, e)
				// A cut before the first freeze COMMIT is still the old writer; every
				// later cut must leave it permanently blocked, even before ownership.
				frozen := stage != "freeze" || phase == "after-commit"
				if frozen && g == nil {
					t.Fatal("lost freeze")
				}
				if frozen {
					if _, e = s.For("a").Cell(ctx, "chat"); !errors.Is(e, clusterstate.ErrMigration) {
						t.Fatal("old reader reopened", e)
					}
				}
				legacyMust(t, s.FreezeLegacy(ctx, legacyConfig()))
				m, e := s.InventoryLegacy(ctx)
				legacyMust(t, e)
				legacyMust(t, s.OwnLegacy(ctx, []LegacyReceipt{{Node: 1, Manifest: m, Applied: 2}, {Node: 2, Manifest: m, Applied: 2}}))
				g, e = s.LegacyGate(ctx)
				legacyMust(t, e)
				if g.Stage != "raft-owned" || g.Config.Cluster != legacyConfig().Cluster {
					t.Fatal(g)
				}
			})
		}
	}
}
