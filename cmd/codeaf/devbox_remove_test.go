package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/devbox"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/pair"
)

// R5.6: the production removal wiring revokes by saved id across name reuse and renames.
func TestDevboxR56UpDownNameReuseRenameAndMissingDevice(t *testing.T) {
	for _, mode := range []string{"reuse", "renamed", "no-match"} {
		t.Run(mode, func(t *testing.T) {
			stateHome := t.TempDir()
			t.Setenv("CODEAF_HOME", stateHome)
			dir := filepath.Join(t.TempDir(), "box")
			memory := directory.NewMemory(time.Now)
			key := bytes.Repeat([]byte{7}, 32)
			kind := chatsKindOf(memory.For(thisComputer), key, thisComputer, "https://relay.example")
			var out bytes.Buffer
			var reachable atomic.Bool
			joined := 0
			f := devbox.Flow{Home: stateHome, Out: &out, Getenv: func(string) string { return "fixture-token" }, LookPath: func(string) (string, error) { return "fake-terraform", nil }, Workspaces: func(context.Context, string) ([]devbox.Workspace, error) {
				return []devbox.Workspace{{ID: "workspace"}}, nil
			}, Settle: func(context.Context, string, string, string) error { return nil }, Probe: func(context.Context, string) bool { return reachable.Load() },
				Prepare: func(_ context.Context, name string) (devbox.Offer, error) {
					err := pair.MachineBook().Remember(pair.Known{Name: "box-remote", Alias: name, Key: "test-key", Service: "https://relay.example"})
					return devbox.Offer{Via: "https://relay.example", RemoteKey: "fixture-key", RemoteDevices: "fixture-devices"}, err
				},
				Devices: func(context.Context) ([]devbox.Device, error) {
					rows, err := kind.rows()
					var devices []devbox.Device
					for _, r := range rows {
						devices = append(devices, devbox.Device{ID: r.id, Name: r.Name, Removed: r.Stopped})
					}
					return devices, err
				},
				Remove: devboxRemoval(func() (chatsKind, bool) { return kind, true }, pair.MachineBook()),
			}
			f.Pair = func(ctx context.Context, name string, ready func(devbox.Offer)) error {
				joined++
				id := fmt.Sprintf("dev_%032d", joined)
				sealed, err := directory.SealName(kind.key, name)
				if err != nil {
					return err
				}
				if err := memory.For(id).PutDevice(ctx, id, directory.Device{V: 1, Name: sealed}); err != nil {
					return err
				}
				ready(devbox.Offer{Code: "42-123-456", Via: "https://relay.example"})
				<-ctx.Done()
				return ctx.Err()
			}
			f.RunTerraform = func(_ context.Context, _, folder string, args, env []string, stdout, stderr io.Writer) error {
				switch args[0] {
				case "output":
					_, err := io.WriteString(stdout, `{"service_id":{"value":"service"},"environment_id":{"value":"environment"}}`)
					return err
				case "show":
					_, err := io.WriteString(stdout, `{"resource_changes":[]}`)
					return err
				case "apply":
					if strings.Contains(strings.Join(env, " "), "TF_VAR_pair_code=") {
						reachable.Store(true)
					}
					return os.WriteFile(filepath.Join(folder, "terraform.tfstate"), []byte(`{"resources":[{"type":"railway_project","instances":[{}]}]}`), 0o600)
				case "destroy":
					reachable.Store(false)
					return os.WriteFile(filepath.Join(folder, "terraform.tfstate"), []byte(`{"resources":[]}`), 0o600)
				}
				return nil
			}
			rounds := 1
			if mode == "reuse" {
				rounds = 2
			}
			for n := 1; n <= rounds; n++ {
				opts := devbox.Options{Name: "box", Dir: dir, Yes: true}
				if err := f.Run(t.Context(), "up", opts); err != nil {
					t.Fatal(err)
				}
				expected := fmt.Sprintf("dev_%032d", n)
				raw, err := os.ReadFile(filepath.Join(dir, "codeaf.devbox.json"))
				if err != nil || !strings.Contains(string(raw), expected) {
					t.Fatalf("R5.6 missing joined id: %s %v", raw, err)
				}
				info, err := os.Stat(filepath.Join(dir, "codeaf.devbox.json"))
				if err != nil || info.Mode().Perm() != 0o600 {
					t.Fatal("R5.6 receipt is not 0600")
				}
				if mode == "renamed" {
					sealed, err := directory.SealName(kind.key, "new-box-name")
					if err != nil {
						t.Fatal(err)
					}
					if err := memory.For(expected).PutDevice(t.Context(), expected, directory.Device{V: 1, Name: sealed}); err != nil {
						t.Fatal(err)
					}
				}
				// The second removal also proves the active-name fallback ignores old revoked rows.
				if mode == "reuse" && n == 2 {
					if err := os.Remove(filepath.Join(dir, "codeaf.devbox.json")); err != nil {
						t.Fatal(err)
					}
				}
				if mode == "no-match" {
					if err := kind.dir.Revoke(t.Context(), expected); err != nil {
						t.Fatal(err)
					}
				}
				if err := f.Run(t.Context(), "down", opts); err != nil {
					t.Fatalf("R5.6 down %d: %v", n, err)
				}
				listing, err := kind.dir.List(t.Context())
				if err != nil || !listing.Devices[expected].Revoked {
					t.Fatalf("R5.6 did not revoke %s: %v", expected, err)
				}
				if _, found, err := pair.MachineBook().Machine("box"); err != nil || found {
					t.Fatal("R5.6 alias retained")
				}
			}
			if mode == "no-match" && !strings.Contains(out.String(), "No active device matches box; no device was removed.") {
				t.Fatalf("R5.6 no-match hidden: %s", &out)
			}
		})
	}
}
