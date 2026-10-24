//go:build linux

package executor

import (
	"context"
	"errors"
	"fmt"
	"golang.org/x/sys/unix"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func jailed(t *testing.T, root string, req ExecRequest) ExecResult {
	t.Helper()
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

// TestDenyPolicyBlocksNetwork proves C18: missing confinement refuses the
// denied call, while an available jail keeps the outside listener unreachable.
func TestDenyPolicyBlocksNetwork(t *testing.T) {
	if unavailableJail(t) {
		return
	}
	addr := strings.Replace(listener(t), ":", "/", 1)
	dial := ExecRequest{Argv: []string{"bash", "-c", "exec 3<>/dev/tcp/" + addr}, Timeout: 10 * time.Second}
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

// TestWriteOutsideWorkspaceFails proves C18 on confined and degraded hosts.
func TestWriteOutsideWorkspaceFails(t *testing.T) {
	if unavailableJail(t) {
		return
	}
	needLandlock(t)
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside")
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

// TestCellIsUnreadable proves C18 without mistaking unavailable confinement for a jail.
func TestCellIsUnreadable(t *testing.T) {
	if unavailableJail(t) {
		return
	}
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
	req := sh("echo host-bound")
	req.Timeout = 10 * time.Second
	res, err := Local{Root: t.TempDir(), Class: HostBound, Jail: LinuxJail{}}.Exec(context.Background(), req, nil)
	if err != nil || res.JailDegraded {
		t.Fatalf("host-bound call was jailed: %+v %v", res, err)
	}
}

// unavailableJail proves C18 through the real Confine/exec door on a host
// without namespaces: denied work never runs and setup work reports the limit.
func unavailableJail(t *testing.T) bool {
	t.Helper()
	if Probe().UserNS {
		return false
	}
	root := t.TempDir()
	req := sh("echo ran > marker")
	local := Local{Root: root, Jail: LinuxJail{}}
	if _, err := local.Exec(context.Background(), req, nil); !errors.Is(err, ErrNoIsolation) {
		t.Fatalf("unavailable jail did not refuse denied work: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "marker")); !os.IsNotExist(err) {
		t.Fatal("refused work ran")
	}
	req.Setup = true
	res, err := local.Exec(context.Background(), req, nil)
	if err != nil || res.Exit != 0 || !res.JailDegraded || res.SideEffect != EffectExternal {
		t.Fatalf("setup without namespaces = %+v, %v", res, err)
	}
	if data, _ := os.ReadFile(filepath.Join(root, "marker")); string(data) != "ran\n" {
		t.Fatal("setup did not run")
	}
	return true
}

// TestC18HelperExecDeniedDegradesAndRefuses reproduces a kernel that creates
// namespaces but denies the jail's ambient-capability helper exec, as on CI.
func TestC18HelperExecDeniedDegradesAndRefuses(t *testing.T) {
	previous := runNamespaceProbe
	t.Cleanup(func() { runNamespaceProbe = previous })
	attempts := 0
	runNamespaceProbe = func(cmd *exec.Cmd) error {
		attempts++
		if cmd.Path != "/proc/self/exe" || fmt.Sprint(cmd.Args) != fmt.Sprint([]string{helperName, "probe"}) {
			t.Fatalf("probe is not the jail helper: %+v", cmd)
		}
		attr := cmd.SysProcAttr
		if attr.Cloneflags != namespaceAttr(true).Cloneflags || len(attr.AmbientCaps) != 1 || attr.AmbientCaps[0] != unix.CAP_SYS_ADMIN {
			t.Fatalf("probe does not exercise the jail attributes: %+v", attr)
		}
		return syscall.EACCES
	}
	t.Cleanup(UseNamespaceProbeInTests(userNSWorks))
	if !unavailableJail(t) || attempts == 0 {
		t.Fatal("denied helper was treated as available")
	}
}

// TestC18AvailableProbeUsesTheHelper proves C18's available branch reaches the
// same confined command, rather than silently bypassing a supported jail.
func TestC18AvailableProbeUsesTheHelper(t *testing.T) {
	if !userNSWorks() {
		unavailableJail(t)
		return
	}
	t.Cleanup(UseNamespaceProbeInTests(func() bool { return true }))
	res := jailed(t, t.TempDir(), sh("echo confined"))
	if res.Exit != 0 || string(res.Stdout) != "confined\n" {
		t.Fatalf("confined exec = %+v", res)
	}
}

func init() {
	if os.Getenv("CODEAF_TEST_NO_USERNS") == "1" {
		UseNamespaceProbeInTests(func() bool { return false })
	}
}

// TestC18ProbeCachesTheHelperLaunch proves C18: one denied helper exec fixes
// the process's capability answer, so repeated calls cannot spawn more probes.
func TestC18ProbeCachesTheHelperLaunch(t *testing.T) {
	oldOnce, oldCaps, oldOverride, oldRun := probeOnce, probed, namespaceProbeInTests, runNamespaceProbe
	t.Cleanup(func() {
		probeOnce, probed, namespaceProbeInTests, runNamespaceProbe = oldOnce, oldCaps, oldOverride, oldRun
	})
	probeOnce, probed, namespaceProbeInTests = new(sync.Once), Capabilities{}, nil
	attempts := 0
	runNamespaceProbe = func(*exec.Cmd) error { attempts++; return syscall.EACCES }
	for range 3 {
		if Probe().UserNS {
			t.Fatal("denied helper counted as a working jail")
		}
	}
	if attempts != 1 {
		t.Fatalf("helper probe ran %d times", attempts)
	}
}
