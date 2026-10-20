package main

import (
	"crypto/sha256"
	"encoding/hex"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/enginehost"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/remote"
)

// The installed host predates canonical keys. Construct its directory without
// calling today's identity helpers, then use the ordinary command doors.
func legacyEngineStandIn(t *testing.T, alias string, busy bool) *standInHost {
	t.Helper()
	sum := sha256.Sum256([]byte(filepath.Clean(alias)))
	dir := home.Join("v3", "hosts", hex.EncodeToString(sum[:8]))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "workspace"), []byte(alias+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", filepath.Join(dir, "host.sock"))
	if err != nil {
		t.Fatal(err)
	}
	host := &standInHost{listener: listener, self: remote.HostSelf{
		Version: remote.Version, Build: "two-days-ago", BuiltAt: earlier,
		Workspace: alias, PID: 4242, Busy: busy,
	}}
	t.Cleanup(host.close)
	go host.serve()
	return host
}

func TestLegacyEngineUpgradeDoors(t *testing.T) {
	for _, spelling := range []string{"resolved", "alias"} {
		for _, door := range []string{"dial", "stop", "stop-all", "takeover", "busy-takeover"} {
			t.Run(spelling+"/"+door, func(t *testing.T) {
				shortEngineHome(t)
				target := t.TempDir()
				alias := filepath.Join(t.TempDir(), "alias")
				if err := os.Symlink(target, alias); err != nil {
					t.Fatal(err)
				}
				workspace, err := filepath.EvalSymlinks(target)
				if err != nil {
					t.Fatal(err)
				}
				if spelling == "alias" {
					workspace = alias
				}
				host := legacyEngineStandIn(t, alias, door == "busy-takeover")
				if held, err := enginehost.Held(); err != nil || len(held) != 1 || held[0] != alias {
					t.Fatalf("Held = %v, %v; want the old workspace citation %s", held, err, alias)
				}
				var status strings.Builder
				if err := runEngineStatus(&status, workspace); err != nil || !strings.Contains(status.String(), "is held by an engine") {
					t.Fatalf("status = %q, %v", status.String(), err)
				}
				status.Reset()
				if err := runEngineStatusAll(&status); err != nil || !strings.Contains(status.String(), alias+" is held by an engine") {
					t.Fatalf("status-all = %q, %v", status.String(), err)
				}
				switch door {
				case "dial":
					conn, err := enginehost.Dial(workspace)
					if err != nil {
						t.Fatal(err)
					}
					_ = conn.Close()
					return
				case "stop":
					if stopped, err := enginehost.Stop(workspace); err != nil || !stopped {
						t.Fatalf("Stop = %v, %v", stopped, err)
					}
				case "stop-all":
					if err := runEngineStopAll(); err != nil {
						t.Fatal(err)
					}
				case "takeover", "busy-takeover":
					note, err := clearStaleEngineHostAs(workspace, window("today", later))
					if err != nil {
						t.Fatal(err)
					}
					if door == "busy-takeover" {
						if !strings.Contains(note, "still holding work") || !host.waitingSaid() || host.forced() || !hostAnswers(workspace) {
							t.Fatalf("busy legacy host was not safely joined: %q, asks %+v", note, host.questions())
						}
						return
					}
					if !strings.Contains(note, "replaced the older engine") || host.forced() {
						t.Fatalf("idle legacy host was not safely replaced: %q, asks %+v", note, host.questions())
					}
				}
				if hostAnswers(workspace) {
					t.Fatal("legacy host still answers after retirement")
				}
			})
		}
	}
}
