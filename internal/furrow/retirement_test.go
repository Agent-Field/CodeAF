package furrow

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDropForkChecksIdentityAndRemovalReceipt(t *testing.T) {
	for _, mode := range []string{"ok", "mismatch", "missing", "symlink", "json-error", "retained", "invalid", "absent"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			destination := filepath.Join(root, "child")
			if mode != "missing" {
				if err := os.Mkdir(destination, 0700); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "symlink" {
				os.Remove(destination)
				if err := os.Symlink(t.TempDir(), destination); err != nil {
					t.Fatal(err)
				}
			}
			listed := destination
			if mode == "mismatch" {
				listed = t.TempDir()
			}
			row, _ := json.Marshal([]map[string]string{{"name": "task", "destination": listed}})
			if mode == "absent" {
				row = []byte("[]")
			}
			t.Setenv("RETIREMENT_ROWS", string(row))
			t.Setenv("RETIREMENT_MODE", mode)
			binary := filepath.Join(root, "furrow")
			script := `#!/bin/sh
case "$4" in
forks) printf '%s\n' "$RETIREMENT_ROWS" ;;
fork-rm)
 printf '%s\n' "$*" > "$2/called"
 case "$RETIREMENT_MODE" in
 json-error) echo '{"error":"refused"}'; exit 1 ;;
 retained) echo '{"files_removed":false}' ;;
 invalid) echo '{}' ;;
 *) echo '{"files_removed":true}' ;;
 esac
 ;;
esac
`
			if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			workspace := &Workspace{root: root, binary: binary}
			err := workspace.DropFork(context.Background(), "task", destination)
			wantError := mode != "ok" && mode != "absent"
			if (err != nil) != wantError {
				t.Fatalf("DropFork error = %v, want error %v", err, wantError)
			}
			call, readErr := os.ReadFile(filepath.Join(root, "called"))
			if mode == "mismatch" || mode == "missing" || mode == "symlink" || mode == "absent" {
				if !os.IsNotExist(readErr) {
					t.Fatalf("unsafe removal was called: %s", call)
				}
			} else if readErr != nil || strings.Contains(string(call), "--keep-files") {
				t.Fatalf("retirement command = %q: %v", call, readErr)
			}
		})
	}
}
