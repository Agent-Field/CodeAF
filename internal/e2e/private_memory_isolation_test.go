//go:build e2e

package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
)

// privateMemoryIsolation exposes runtime data, never its parent evaluation
// directory. Fresh /proc also closes the host-process-root escape route.
func privateMemoryIsolation(t *testing.T, home, ws, executable string) []string {
	t.Helper()
	wrap, err := exec.LookPath("bwrap")
	if err != nil {
		t.Fatal("private live acceptance requires OS isolation (bwrap)")
	}
	// bwrap preserves inherited descriptors. Close them before entering the
	// namespace so an evaluator's open oracle cannot bypass pathname isolation.
	closeFDs := `for path in /proc/self/fd/*; do fd=${path##*/}; if (( fd > 2 )); then eval "exec ${fd}>&-"; fi; done; exec "$@"`
	args := []string{"/usr/bin/env", "-i", "/bin/bash", "--noprofile", "--norc", "-c", closeFDs, "private-isolation", wrap, "--die-with-parent", "--unshare-pid", "--unshare-ipc", "--unshare-uts", "--cap-drop", "ALL", "--clearenv"}
	// A fresh root admits only system executables/libraries and TLS/DNS data.
	// No alternate user-home, backup, mount or evaluation paths are visible.
	for _, path := range []string{"/usr", "/bin", "/sbin", "/lib", "/lib64", "/etc/ssl", "/etc/resolv.conf", "/etc/hosts", "/etc/nsswitch.conf", "/etc/passwd", "/etc/group", "/etc/localtime", "/etc/ld.so.cache", "/etc/fonts"} {
		if _, err := os.Stat(path); err == nil {
			args = append(args, "--ro-bind", path, path)
		}
	}
	args = append(args, "--tmpfs", "/tmp", "--proc", "/proc", "--dev", "/dev", "--ro-bind", executable, executable,
		"--bind", home, home, "--bind", ws, ws, "--bind", home+".host", home+".host", "--chdir", ws, "--")
	return args
}

func startPrivateMemory(t *testing.T, name, home, ws string, cols, rows int, args ...string) *rig {
	t.Helper()
	// The host guard is the only additional writable mount, and contains fake
	// scheduler commands rather than the developer's actual login state.
	guardHost(t, home)
	prefix := privateMemoryIsolation(t, home, ws, binary(t))
	r := startWithLaunchPrefix(t, prefix, []string{config.APIKeyEnv + "=" + liveKey(t), "PATH=/usr/local/bin:/usr/bin:/bin", "LANG=C.UTF-8", "TMPDIR=/tmp", "CODEAF_VISION_MODEL=" + e2eModel, "IMAGE_MODEL=" + e2eModel, "SPEECH_MODEL=" + e2eModel, "MUSIC_MODEL=" + e2eModel, "VIDEO_MODEL=" + e2eModel, "VOICE_MODEL=" + e2eModel}, name, home, ws, cols, rows, args...)
	r.skipSetup(t)
	return r
}

func TestPrivateMemoryIsolationDeniesOracleReads(t *testing.T) {
	if _, err := exec.LookPath("bwrap"); err != nil {
		t.Skip("bwrap unavailable")
	}
	t.Setenv("CODEAF_PRIVATE_MEMORY_SCENARIOS", "grading-secret-sentinel")
	t.Setenv("CODEAF_PRIVATE_MEMORY_DWELL_PLAN", "grading-secret-sentinel")
	// Keep this under the real user home so the actual mount boundary is tested.
	root, err := os.MkdirTemp(developerHome, ".memory-isolation-canary-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	startupMarker := filepath.Join(root, "startup-leak")
	startupScript := filepath.Join(root, "hostile-bash-env")
	if err := os.WriteFile(startupScript, []byte("touch "+shellQuote(startupMarker)), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BASH_ENV", startupScript)
	home, ws := filepath.Join(root, "runtime"), filepath.Join(root, "workspace")
	for _, dir := range []string{home, ws, home + ".host"} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	oracle := filepath.Join(root, "grading-plan.json")
	if err := os.WriteFile(oracle, []byte("secret grading canary"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(ws, "oracle-link")
	if err := os.Symlink(oracle, link); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "runtime-canary"), []byte("runtime"), 0600); err != nil {
		t.Fatal(err)
	}
	prefix := privateMemoryIsolation(t, home, ws, "/bin/sh")
	// Exercise real child shells, including traversal, symlink and a host PID.
	script := `set -eu
test "$(cat "$1/runtime-canary")" = runtime
printf working > "$2/allowed-write"
for forbidden in "$3" "$2/../grading-plan.json" "$2/oracle-link" "/proc/$4/root$3" "/proc/self/root$3" "/proc/self/fd/3" "/proc/self/fd/64"; do
 if cat "$forbidden" >/dev/null 2>&1; then exit 41; fi
done
test ! -e "$2/../grading-plan.json"
test -z "${CODEAF_PRIVATE_MEMORY_SCENARIOS-}"
test -z "${CODEAF_PRIVATE_MEMORY_DWELL_PLAN-}"
`
	command := append(prefix, "/bin/sh", "-c", script, "canary", home, ws, oracle, strconv.Itoa(os.Getpid()))
	oracleFD, err := os.Open(oracle)
	if err != nil {
		t.Fatal(err)
	}
	defer oracleFD.Close()
	child := exec.Command(command[0], command[1:]...)
	child.ExtraFiles = make([]*os.File, 62)
	child.ExtraFiles[0], child.ExtraFiles[61] = oracleFD, oracleFD
	if output, err := child.CombinedOutput(); err != nil {
		t.Fatal(fmt.Sprintf("OS isolation canary failed: %v (%d diagnostic bytes)", err, len(output)))
	}
	if _, err := os.Stat(filepath.Join(ws, "allowed-write")); err != nil {
		t.Fatal("runtime workspace is not writable")
	}
	if _, err := os.Stat(startupMarker); !os.IsNotExist(err) {
		t.Fatal("host shell evaluated inherited startup instructions")
	}
	// The same mount policy must launch the actual application, not merely a
	// stand-in shell. Version exits without a model call or a copied key.
	app := binary(t)
	appCommand := append(privateMemoryIsolation(t, home, ws, app), "env", "CODEAF_HOME="+home, app, "--version")
	if output, err := exec.Command(appCommand[0], appCommand[1:]...).CombinedOutput(); err != nil {
		t.Fatalf("isolated application launch failed: %v (%d diagnostic bytes)", err, len(output))
	}
}
