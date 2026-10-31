package devbox

import (
	"context"
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// R6.2: the first install pins its release, retries unpinned, and later boots follow the channel.
func TestDevboxR62BootInstallVersion(t *testing.T) {
	const version = "dev-20261030-aaaaaaaaaaaa"
	for _, row := range []struct {
		name, version, failure, want string
		existing                     bool
		backoff                      bool
	}{
		{name: "first-install-pins", version: version, want: version + "\n"},
		{name: "failed-pin-retries-unpinned", version: version, failure: "pinned", want: version + "\n<unset>\n"},
		{name: "failed-both-back-off-once", version: version, failure: "all", want: version + "\n<unset>\n", backoff: true},
		{name: "existing-binary-refreshes-unpinned", version: version, existing: true, want: "<unset>\n"},
		{name: "failed-refresh-keeps-earlier-binary", version: version, existing: true, failure: "all", want: "<unset>\n"},
		{name: "no-version-installs-unpinned", want: "<unset>\n"},
		{name: "no-version-failure-backs-off", failure: "all", want: "<unset>\n", backoff: true},
	} {
		t.Run(row.name, func(t *testing.T) {
			data := t.TempDir()
			bin := filepath.Join(data, "fake-bin")
			if err := os.Mkdir(bin, 0o700); err != nil {
				t.Fatal(err)
			}
			for name, script := range map[string]string{
				"curl": `#!/bin/bash
cat <<'INSTALLER'
set -eu
printf '%s\n' "${VERSION-<unset>}" >> "$CODEAF_DEVBOX_DATA_ROOT/versions"
if [[ "$INSTALL_FAILURE" == all || ( "$INSTALL_FAILURE" == pinned && -n "${VERSION:-}" ) ]]; then exit 22; fi
mkdir -p "$CODEAF_INSTALL_DIR"
cp "$CODEAF_DEVBOX_DATA_ROOT/fake-bin/codeaf" "$CODEAF_INSTALL_DIR/$CODEAF_INSTALL_NAME"
INSTALLER
`,
				"sleep":  "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$CODEAF_DEVBOX_DATA_ROOT/sleeps\"\n",
				"codeaf": "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$CODEAF_DEVBOX_DATA_ROOT/calls\"\n",
			} {
				if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			if row.existing {
				installed := filepath.Join(data, "home", ".codeaf", "bin")
				if err := os.MkdirAll(installed, 0o700); err != nil {
					t.Fatal(err)
				}
				fake, err := os.ReadFile(filepath.Join(bin, "codeaf"))
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(installed, "codeaf"), fake, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			raw, err := module.ReadFile("terraform/boot.sh")
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "bash")
			cmd.Stdin = strings.NewReader(string(raw))
			cmd.Env = append(os.Environ(), "CODEAF_TELEMETRY=off", "DO_NOT_TRACK=1", "PATH="+bin+":"+os.Getenv("PATH"), "CODEAF_DEVBOX_DATA_ROOT="+data, "CODEAF_DEVBOX_CODE=42-123-456", "CODEAF_DEVBOX_NAME=box", "CODEAF_DEVBOX_BINARY=codeaf", "CODEAF_DEVBOX_INSTALL_URL=https://installer.example/codeaf", "CODEAF_DEVBOX_VIA=https://relay.example", "CODEAF_DEVBOX_VERSION="+row.version, "VERSION=stray-environment-version", "INSTALL_FAILURE="+row.failure, "CODEAF_DEVBOX_REMOTE_KEY=", "CODEAF_DEVBOX_REMOTE_DEVICES=")
			if row.version == "" {
				cmd.Env = slices.DeleteFunc(cmd.Env, func(entry string) bool { return strings.HasPrefix(entry, "CODEAF_DEVBOX_VERSION=") })
			}
			out, err := cmd.CombinedOutput()
			versions, readErr := os.ReadFile(filepath.Join(data, "versions"))
			if readErr != nil || string(versions) != row.want {
				t.Fatalf("R6.2 installer versions = %q (%v), want %q; boot: %v %s", versions, readErr, row.want, err, out)
			}
			sleeps, _ := os.ReadFile(filepath.Join(data, "sleeps"))
			calls, _ := os.ReadFile(filepath.Join(data, "calls"))
			if row.backoff {
				if err == nil || string(sleeps) != "60\n" || len(calls) != 0 || !strings.Contains(string(out), "installation failed and no earlier binary is available") {
					t.Fatalf("R6.2 backoff: %v, output %q, sleeps %q, calls %q", err, out, sleeps, calls)
				}
			} else if err != nil || len(sleeps) != 0 || !strings.Contains(string(calls), "serve --workspace "+filepath.Join(data, "home", "work")) {
				t.Fatalf("R6.2 boot: %v, output %q, sleeps %q, calls %q", err, out, sleeps, calls)
			}
		})
	}
}

// Contracts 3 and 4: the real boot script pairs once, persists HOME and serves through the same relay.
func TestDevboxR35BootPairsOnceAcrossRestarts(t *testing.T) {
	for _, firstFailure := range []bool{false, true} {
		t.Run(map[bool]string{true: "retry", false: "restart"}[firstFailure], func(t *testing.T) {
			data := t.TempDir()
			bin := filepath.Join(data, "home", ".codeaf", "bin")
			if err := os.MkdirAll(bin, 0o700); err != nil {
				t.Fatal(err)
			}
			fake := `#!/bin/bash
set -eu
printf '%s|%s\n' "$HOME" "$*" >> "$CODEAF_DEVBOX_DATA_ROOT/calls"
if [[ "$1" == pair ]]; then
 [[ "$2" == 42-123-456 && "$3" == --name && "$4" == box && "$5" == --via && "$6" == https://relay.example ]]
 [[ -z "${FAIL_PAIR:-}" ]]
else
 [[ -z "${CODEAF_DEVBOX_CODE+x}" && -z "${CODEAF_DEVBOX_REMOTE_KEY+x}" && -z "${CODEAF_DEVBOX_REMOTE_DEVICES+x}" ]]
 cmp "$CODEAF_HOME/v3/remote/device.key" "$CODEAF_DEVBOX_DATA_ROOT/expected-key"
 cmp "$CODEAF_HOME/v3/remote/devices.json" "$CODEAF_DEVBOX_DATA_ROOT/expected-devices"
 [[ "$1" == serve && "$2" == --workspace && "$3" == "$HOME/work" && "$4" == --via && "$5" == https://relay.example ]]
fi
`
			if err := os.WriteFile(filepath.Join(bin, "codeaf"), []byte(fake), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(bin, "sleep"), []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$CODEAF_DEVBOX_DATA_ROOT/sleeps\"\n"), 0o700); err != nil {
				t.Fatal(err)
			}
			// A failed refresh must fall back to the existing binary on the volume.
			if err := os.WriteFile(filepath.Join(bin, "curl"), []byte("#!/bin/sh\nexit 22\n"), 0o700); err != nil {
				t.Fatal(err)
			}
			script, err := module.ReadFile("terraform/boot.sh")
			if err != nil {
				t.Fatal(err)
			}
			run := func(code string, fail bool) (string, error) {
				cmd := exec.Command("bash")
				cmd.Stdin = strings.NewReader(string(script))
				cmd.Env = append(os.Environ(), "HOME="+filepath.Join(data, "home"), "CODEAF_TELEMETRY=off", "DO_NOT_TRACK=1", "CODEAF_DEVBOX_DATA_ROOT="+data, "CODEAF_DEVBOX_BINARY=codeaf", "CODEAF_DEVBOX_NAME=box", "CODEAF_DEVBOX_VIA=https://relay.example", "CODEAF_DEVBOX_INSTALL_URL=https://installer.example/codeaf", "CODEAF_DEVBOX_CODE="+code, "FAIL_PAIR="+map[bool]string{true: "1", false: ""}[fail])
				out, err := cmd.CombinedOutput()
				return string(out), err
			}
			key := "key file bytes\n"
			devices := "admission file bytes\n"
			for name, raw := range map[string]string{"expected-key": key, "expected-devices": devices} {
				if err := os.WriteFile(filepath.Join(data, name), []byte(raw), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("CODEAF_DEVBOX_REMOTE_KEY", base64.StdEncoding.EncodeToString([]byte(key)))
			t.Setenv("CODEAF_DEVBOX_REMOTE_DEVICES", base64.StdEncoding.EncodeToString([]byte(devices)))
			marker := filepath.Join(data, ".codeaf-devbox-joined")
			if firstFailure {
				if out, err := run("42-123-456", true); err != nil {
					t.Fatalf("failed join did not wait: %v %s", err, out)
				}
				if sleeps, _ := os.ReadFile(filepath.Join(data, "sleeps")); string(sleeps) != "infinity\n" {
					t.Fatalf("R3.5 failed pair did not sleep: %q", sleeps)
				}
				if _, err := os.Stat(marker); !os.IsNotExist(err) {
					t.Fatal("failed join wrote marker")
				}
			}
			if out, err := run("42-123-456", false); err != nil {
				t.Fatalf("boot: %v %s", err, out)
			}
			if _, err := os.Stat(marker); err != nil {
				t.Fatal(err)
			}
			// Each release channel syncs through its own relay, so the box must
			// sync through the one the laptop paired it through, whatever its
			// installed build's own default is.
			savedRelay := filepath.Join(data, "home", ".codeaf", syncsetup.SavedURLFile)
			if raw, err := os.ReadFile(savedRelay); err != nil || string(raw) != "https://relay.example\n" {
				t.Fatalf("the box's saved relay = %q (%v); want the laptop's relay", raw, err)
			}
			remote := filepath.Join(data, "home", ".codeaf", "v3", "remote")
			for path, mode := range map[string]os.FileMode{remote: 0o700, filepath.Join(remote, "device.key"): 0o600, filepath.Join(remote, "devices.json"): 0o600} {
				info, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				if info.Mode().Perm() != mode {
					t.Fatalf("%s mode = %o, want %o", path, info.Mode().Perm(), mode)
				}
			}
			// Later admissions and revocations must survive restarts, even with stale service variables.
			devices = "changed admissions\n"
			if err := os.WriteFile(filepath.Join(data, "home", ".codeaf", "v3", "remote", "devices.json"), []byte(devices), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(data, "expected-devices"), []byte(devices), 0o600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("CODEAF_DEVBOX_REMOTE_KEY", base64.StdEncoding.EncodeToString([]byte("different key")))
			// A relay a later pairing saved belongs to the volume, like its admissions.
			if err := os.WriteFile(savedRelay, []byte("https://later.example\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(filepath.Join(data, "calls"))
			if out, err := run("", false); err != nil {
				t.Fatalf("restart: %v %s", err, out)
			}
			after, _ := os.ReadFile(filepath.Join(data, "calls"))
			added := strings.TrimPrefix(string(after), string(before))
			if strings.Contains(added, "pair") || !strings.Contains(added, "serve --workspace "+filepath.Join(data, "home", "work")) {
				t.Fatalf("restart: %s", added)
			}
			if raw, _ := os.ReadFile(savedRelay); string(raw) != "https://later.example\n" {
				t.Fatalf("a restart replaced the saved relay with %q", raw)
			}
			// An existing admission book is retained even when the key alone needs seeding.
			if err := os.Remove(filepath.Join(data, "home", ".codeaf", "v3", "remote", "device.key")); err != nil {
				t.Fatal(err)
			}
			t.Setenv("CODEAF_DEVBOX_REMOTE_KEY", base64.StdEncoding.EncodeToString([]byte(key)))
			if out, err := run("", false); err != nil {
				t.Fatalf("key-only seed: %v %s", err, out)
			}
			t.Setenv("CODEAF_DEVBOX_REMOTE_KEY", "")
			t.Setenv("CODEAF_DEVBOX_REMOTE_DEVICES", "")
			if out, err := run("", false); err != nil {
				t.Fatalf("unseeded restart: %v %s", err, out)
			}
		})
	}
}

// R3.5: a missing code waits before downloading, a failed pair waits, and an install failure backs off.
func TestDevboxR35BootWaitsWithoutCodeAndBacksOffFailedInstall(t *testing.T) {
	for _, mode := range []string{"no-code", "install-failure"} {
		t.Run(mode, func(t *testing.T) {
			data := t.TempDir()
			bin := filepath.Join(data, "fake-bin")
			if err := os.Mkdir(bin, 0o700); err != nil {
				t.Fatal(err)
			}
			for name, script := range map[string]string{
				"curl":  "#!/bin/sh\necho download >> \"$CODEAF_DEVBOX_DATA_ROOT/calls\"\nexit 22\n",
				"sleep": "#!/bin/sh\necho \"$*\" >> \"$CODEAF_DEVBOX_DATA_ROOT/sleeps\"\n",
			} {
				if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			raw, _ := module.ReadFile("terraform/boot.sh")
			cmd := exec.Command("bash")
			cmd.Stdin = strings.NewReader(string(raw))
			cmd.Env = append(os.Environ(), "HOME="+filepath.Join(data, "home"), "CODEAF_TELEMETRY=off", "DO_NOT_TRACK=1", "PATH="+bin+":"+os.Getenv("PATH"), "CODEAF_DEVBOX_DATA_ROOT="+data, "CODEAF_DEVBOX_CODE=", "CODEAF_DEVBOX_BINARY=", "CODEAF_DEVBOX_INSTALL_URL=")
			if mode == "install-failure" {
				cmd.Env = append(cmd.Env, "CODEAF_DEVBOX_CODE=42-123-456", "CODEAF_DEVBOX_BINARY=codeaf", "CODEAF_DEVBOX_INSTALL_URL=https://installer.example/codeaf")
			}
			out, err := cmd.CombinedOutput()
			sleeps, _ := os.ReadFile(filepath.Join(data, "sleeps"))
			calls, _ := os.ReadFile(filepath.Join(data, "calls"))
			if mode == "no-code" {
				if err != nil || string(sleeps) != "infinity\n" || len(calls) != 0 || string(out) != "codeaf devbox: waiting for a pairing code from codeaf devbox up\n" {
					t.Fatalf("wait: %v, output %q, sleeps %q, calls %q", err, out, sleeps, calls)
				}
			} else if err == nil || string(sleeps) != "60\n" || string(calls) != "download\n" {
				t.Fatalf("backoff: %v, output %q, sleeps %q, calls %q", err, out, sleeps, calls)
			}
			for _, path := range []string{".codeaf-devbox-joined", "home/.codeaf/v3/remote/device.key"} {
				if _, err := os.Stat(filepath.Join(data, path)); !os.IsNotExist(err) {
					t.Fatalf("created %s", path)
				}
			}
		})
	}
}

// R5.5: each absent seed is atomic and independent; existing admissions belong to the volume.
func TestDevboxR55BootSeedsIndependentlyAndAtomically(t *testing.T) {
	for _, mode := range []string{"key-present", "book-present", "bad-key", "bad-book"} {
		t.Run(mode, func(t *testing.T) {
			data := t.TempDir()
			remote := filepath.Join(data, "home", ".codeaf", "v3", "remote")
			bin := filepath.Join(data, "home", ".codeaf", "bin")
			for _, dir := range []string{remote, bin} {
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			for name, body := range map[string]string{"curl": "exit 22", "codeaf": "exit 0"} {
				if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"+body+"\n"), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(data, ".codeaf-devbox-joined"), nil, 0o600); err != nil {
				t.Fatal(err)
			}
			key, book := base64.StdEncoding.EncodeToString([]byte("new-key")), base64.StdEncoding.EncodeToString([]byte("new-book"))
			if mode == "key-present" {
				if err := os.WriteFile(filepath.Join(remote, "device.key"), []byte("existing-key"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "book-present" {
				if err := os.WriteFile(filepath.Join(remote, "devices.json"), []byte("existing-book"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "bad-key" {
				key = "cGFydGlhbA==!"
			}
			if mode == "bad-book" {
				book = "cGFydGlhbA==!"
			}
			raw, err := module.ReadFile("terraform/boot.sh")
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("bash")
			cmd.Stdin = strings.NewReader(string(raw))
			cmd.Env = append(os.Environ(), "HOME="+filepath.Join(data, "home"), "CODEAF_TELEMETRY=off", "DO_NOT_TRACK=1", "CODEAF_DEVBOX_DATA_ROOT="+data, "CODEAF_DEVBOX_BINARY=codeaf", "CODEAF_DEVBOX_INSTALL_URL=https://installer.example/codeaf", "CODEAF_DEVBOX_VIA=https://relay.example", "CODEAF_DEVBOX_REMOTE_KEY="+key, "CODEAF_DEVBOX_REMOTE_DEVICES="+book)
			out, err := cmd.CombinedOutput()
			if strings.HasPrefix(mode, "bad-") {
				if err == nil {
					t.Fatalf("R5.5 accepted failed decode: %s", out)
				}
				name := "device.key"
				if mode == "bad-book" {
					name = "devices.json"
				}
				if _, err := os.Stat(filepath.Join(remote, name)); !os.IsNotExist(err) {
					t.Fatalf("R5.5 partial final %s remains", name)
				}
				files, err := os.ReadDir(remote)
				if err != nil {
					t.Fatal(err)
				}
				for _, file := range files {
					if strings.HasPrefix(file.Name(), ".seed-") {
						t.Fatal("R5.5 temporary seed remains")
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("R5.5 boot: %v %s", err, out)
			}
			for name, want := range map[string]string{"device.key": "new-key", "devices.json": "new-book"} {
				if mode == "key-present" && name == "device.key" {
					want = "existing-key"
				}
				if mode == "book-present" && name == "devices.json" {
					want = "existing-book"
				}
				got, err := os.ReadFile(filepath.Join(remote, name))
				if err != nil || string(got) != want {
					t.Fatalf("R5.5 %s = %q, %v", name, got, err)
				}
			}
		})
	}
}

// Contract 3 and stability: the box's serve, and only the box's, asks to keep
// the identity's keys and presence in step, spelled with the one constant.
func TestDevboxBootAsksServeToKeepStateInStep(t *testing.T) {
	script, err := module.ReadFile("terraform/boot.sh")
	if err != nil {
		t.Fatal(err)
	}
	export := "export " + syncsetup.ServeStateVar + "=1\n"
	at := strings.Index(string(script), export)
	if at < 0 || at > strings.Index(string(script), "serve --workspace") {
		t.Fatalf("boot.sh must export %q before it starts serve", strings.TrimSpace(export))
	}
}
