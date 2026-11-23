//go:build !windows

package celltest_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/cellstore/celltest"
)

func TestEngineDirStopsDaemonsBeforeRemovingFolders(t *testing.T) {
	for _, multiple := range []bool{false, true} {
		name := "one"
		if multiple {
			name = "several"
		}
		t.Run(name, func(t *testing.T) {
			receipt := filepath.Join(t.TempDir(), "stopped")
			// This fallback also reaps the daemon if the nested test fails during setup.
			defer cellstore.StopSpawned()
			var dirs []string
			t.Run("owner", func(t *testing.T) {
				if multiple {
					dirs = append(dirs, t.TempDir())
				}
				dir := celltest.EngineDir(t)
				dirs = append(dirs, dir)
				if multiple {
					dirs = append(dirs, t.TempDir(), celltest.EngineDir(t), t.TempDir())
				}
				// The child records folder existence on SIGTERM. No concurrent removal or
				// delayed write is needed to distinguish the two cleanup orders.
				program := filepath.Join(dir, "engine")
				script := `#!/bin/sh
trap 'receipt=$1
shift
for dir do
 if [ ! -d "$dir" ]; then
  printf missing > "$receipt"
  exit 0
 fi
done
printf present > "$receipt"
exit 0' TERM
`
				script += "set -- '" + receipt + "'"
				for _, folder := range dirs {
					script += " '" + folder + "'"
				}
				script += "\nprintf ready > '" + dir + "/ready'\nwhile :; do sleep 0.01; done\n"
				if err := os.WriteFile(program, []byte(script), 0700); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()
				done := make(chan struct{})
				// The fake never listens, so Do returns when startup is cancelled.
				go func() {
					defer close(done)
					_, _ = (cellstore.Daemon{Binary: program, Socket: filepath.Join(dir, "e.sock")}).Do(ctx, cellstore.Target{}, nil)
				}()
				defer func() { cancel(); <-done }()
				for {
					if _, err := os.Stat(filepath.Join(dir, "ready")); err == nil {
						break
					}
					select {
					case <-ctx.Done():
						t.Fatal("fake daemon did not become ready")
					case <-time.After(10 * time.Millisecond):
					}
				}
			})
			raw, err := os.ReadFile(receipt)
			if err != nil {
				t.Fatal(err)
			}
			if string(raw) != "present" {
				t.Fatalf("daemon stopped after folder removal: %s", raw)
			}
			for _, dir := range dirs {
				if _, err := os.Stat(dir); !os.IsNotExist(err) {
					t.Errorf("temporary folder remains: %v", err)
				}
			}
		})
	}
}
