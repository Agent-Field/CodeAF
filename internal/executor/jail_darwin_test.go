//go:build darwin

package executor

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func jailed(t *testing.T, j Jail, root string, class Class, req ExecRequest) ExecResult {
	t.Helper()
	if !seatbeltWorks() {
		t.Skip("sandbox-exec unavailable")
	}
	res, err := Local{Root: root, Class: class, Jail: j}.Exec(context.Background(), req, nil)
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	return res
}

// loopback returns a local TCP address a confined child can reach only if the
// network is open to it.
func loopback(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("no loopback: %v", err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	return l.Addr().String()
}

func dialer(addr string) ExecRequest {
	host, port, _ := net.SplitHostPort(addr)
	r := sh("exec 3<>/dev/tcp/" + host + "/" + port)
	r.Timeout = 10 * time.Second
	return r
}

func TestSeatbeltProbe(t *testing.T) {
	if !seatbeltWorks() {
		t.Fatal("sandbox-exec does not apply on this macOS")
	}
	if (SeatbeltJail{}).Degraded(ExecRequest{}) {
		t.Fatal("a working seatbelt reports degraded")
	}
}

func TestSandboxedCannotWriteOutsideWorkspace(t *testing.T) {
	root := t.TempDir()
	outsides := []string{
		filepath.Join(os.Getenv("HOME"), ".jail-test-outside"),
		filepath.Join(filepath.Dir(root), "jail-test-sibling"),
	}
	for _, o := range outsides {
		defer os.Remove(o)
	}
	res := jailed(t, SeatbeltJail{}, root, Sandboxed, sh("echo x > "+outsides[0]+"; echo x > "+outsides[1]+"; echo y > inside.txt"))
	for _, o := range outsides {
		if _, err := os.Stat(o); err == nil {
			t.Fatalf("wrote outside the workspace: %s", o)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(root, "inside.txt")); strings.TrimSpace(string(b)) != "y" {
		t.Fatalf("workspace write failed: %s", res.Stderr)
	}
}

func TestSandboxedHasPrivateWritableTmp(t *testing.T) {
	res := jailed(t, SeatbeltJail{}, t.TempDir(), Sandboxed, sh(`echo t > "$TMPDIR/scratch" && cat "$TMPDIR/scratch"`))
	if res.Exit != 0 || strings.TrimSpace(string(res.Stdout)) != "t" {
		t.Fatalf("private tmp unusable: %+v", res)
	}
	if res := jailed(t, SeatbeltJail{}, t.TempDir(), Sandboxed, sh("echo t > /tmp/jail-test-shared")); res.Exit == 0 {
		os.Remove("/tmp/jail-test-shared")
		t.Fatal("wrote the shared /tmp")
	}
}

func TestNetworkDeniedUnlessSetup(t *testing.T) {
	addr := loopback(t)
	if res := jailed(t, SeatbeltJail{}, t.TempDir(), Sandboxed, dialer(addr)); res.Exit == 0 || res.SideEffect != EffectLocal {
		t.Fatalf("a sandboxed call reached a local listener: %+v", res)
	}
	setup := dialer(addr)
	setup.Setup = true
	if res := jailed(t, SeatbeltJail{}, t.TempDir(), Sandboxed, setup); res.Exit != 0 || res.SideEffect != EffectExternal {
		t.Fatalf("a setup call was cut off: %+v", res)
	}
}

func TestCellIsUnreadable(t *testing.T) {
	root := t.TempDir()
	cell := filepath.Join(root, ".cell")
	if err := os.Mkdir(cell, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(cell, "secret"), []byte("s"), 0o644)
	// Assembled at run time: a literal .cell argument is refused before the jail.
	res := jailed(t, SeatbeltJail{}, root, Sandboxed, sh(`d="$(printf '.ce%s' ll)"; cat "$d/secret"; echo z > "$d/new"`))
	if res.Exit == 0 || strings.Contains(string(res.Stdout), "s") {
		t.Fatalf("read .cell inside the jail: %+v", res)
	}
	if _, err := os.Stat(filepath.Join(cell, "new")); err == nil {
		t.Fatal("wrote into .cell inside the jail")
	}
}

func TestHiddenStoreIsUnreachable(t *testing.T) {
	store := t.TempDir()
	os.WriteFile(filepath.Join(store, "obj"), []byte("o"), 0o644)
	res := jailed(t, SeatbeltJail{Hidden: []string{store}}, t.TempDir(), Sandboxed, sh("cat "+filepath.Join(store, "obj")))
	if res.Exit == 0 || strings.Contains(string(res.Stdout), "o") {
		t.Fatalf("read the engine store inside the jail: %+v", res)
	}
}

func TestHostBoundRunsUnconfined(t *testing.T) {
	addr := loopback(t)
	outside := filepath.Join(os.Getenv("HOME"), ".jail-test-host")
	defer os.Remove(outside)
	req := dialer(addr)
	req.Argv[2] += "; echo x > " + outside
	res := jailed(t, SeatbeltJail{}, t.TempDir(), HostBound, req)
	if res.Exit != 0 || res.JailDegraded {
		t.Fatalf("host-bound call was confined: %+v", res)
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatal("host-bound call could not write outside the workspace")
	}
}
