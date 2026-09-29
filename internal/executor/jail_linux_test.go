//go:build linux

package executor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func jailed(t *testing.T, root string, req ExecRequest) ExecResult {
	t.Helper()
	if !Probe().UserNS {
		t.Skip("user namespaces unavailable")
	}
	res, err := Local{Root: root, Jail: LinuxJail{}}.Exec(context.Background(), req, nil)
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	return res
}

func needLandlock(t *testing.T) {
	t.Helper()
	if !Probe().Landlock {
		t.Skip("landlock unavailable")
	}
}

func TestProbeReportsCapabilities(t *testing.T) {
	c := Probe()
	t.Logf("capabilities: %+v", c)
	if c != Probe() || c.Landlock != (c.LandlockABI > 0) {
		t.Fatalf("probe unstable or inconsistent: %+v", c)
	}
}

// dial exits 0 when 1.1.1.1:443 is reachable.
var dial = ExecRequest{Argv: []string{"bash", "-c", "exec 3<>/dev/tcp/1.1.1.1/443"}, Timeout: 10 * time.Second}

func TestDenyPolicyBlocksNetwork(t *testing.T) {
	deny := jailed(t, t.TempDir(), dial)
	if deny.Exit == 0 {
		t.Fatal("dial succeeded inside a deny jail")
	}
	open := dial
	open.Setup = true
	if r := jailed(t, t.TempDir(), open); r.Exit != 0 {
		t.Skipf("host itself cannot reach 1.1.1.1: %s", r.Stderr)
	}
}

func TestWriteOutsideWorkspaceFails(t *testing.T) {
	needLandlock(t)
	root := t.TempDir()
	outside := filepath.Join(os.Getenv("HOME"), ".jail-test-outside")
	defer os.Remove(outside)
	res := jailed(t, root, sh("echo x > "+outside+"; echo y > inside.txt"))
	if _, err := os.Stat(outside); err == nil {
		t.Fatal("wrote outside the workspace")
	}
	if b, _ := os.ReadFile(filepath.Join(root, "inside.txt")); strings.TrimSpace(string(b)) != "y" {
		t.Fatalf("workspace write failed: %s", res.Stderr)
	}
	if !res.JailDegraded == !Probe().Landlock {
		t.Fatal("JailDegraded disagrees with the probe")
	}
}

func TestCellIsUnreadable(t *testing.T) {
	root := t.TempDir()
	cell := filepath.Join(root, ".cell")
	if err := os.Mkdir(cell, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(cell, "secret"), []byte("s"), 0o644)
	// The path is assembled at run time: a literal .cell argument is refused by
	// the executor before it reaches the jail, and this tests the jail.
	res := jailed(t, root, sh(`cat "$(printf '.ce%s' ll)/secret"`))
	if res.Exit == 0 || strings.Contains(string(res.Stdout), "s") {
		t.Fatalf("read .cell inside the jail: %+v", res)
	}
	if b, _ := os.ReadFile(filepath.Join(cell, "secret")); string(b) != "s" {
		t.Fatal("host .cell was altered")
	}
}

func TestHostBoundRunsUnjailed(t *testing.T) {
	req := dial
	req.Timeout = 10 * time.Second
	res, err := Local{Root: t.TempDir(), Class: HostBound, Jail: LinuxJail{}}.Exec(context.Background(), req, nil)
	if err != nil || res.JailDegraded {
		t.Fatalf("host-bound call was jailed: %+v %v", res, err)
	}
}
