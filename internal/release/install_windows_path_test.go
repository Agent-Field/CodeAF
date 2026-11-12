package release

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Contract 6: the real installer under a Git Bash-shaped environment either
// updates the user PATH or prints the exact command, respecting opt-out.
func TestInstallerWindowsUserPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Git Bash installer is simulated on Unix; run the installer itself natively")
	}
	for _, mode := range []string{"success", "failure", "opt-out"} {
		t.Run(mode, func(t *testing.T) {
			github := newInstallGitHub(t, "v1.2.3")
			github.asset = "codeaf-windows-amd64.exe"
			bin := minimalPath(t, true)
			if err := os.Remove(filepath.Join(bin, "uname")); err != nil {
				t.Fatal(err)
			}
			for name, content := range map[string]string{
				"uname":          "#!/bin/sh\ncase \"$1\" in -s) echo MINGW64_NT;; -m) echo x86_64;; esac\n",
				"cygpath":        "#!/bin/sh\nprintf '%s\\n' \"$TEST_WINDOWS_BIN\"\n",
				"powershell.exe": "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$TEST_PS_RECEIPT\"\nexit \"$TEST_PS_EXIT\"\n",
			} {
				if err := os.WriteFile(filepath.Join(bin, name), []byte(content), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			receipt := filepath.Join(t.TempDir(), "ps.txt")
			code, noModify := "0", "0"
			if mode == "failure" {
				code = "1"
			}
			if mode == "opt-out" {
				noModify = "1"
			}
			native := `C:\Users\Example User\Projects\O'Brien ü\bin`
			run := runInstaller(t, github, nil, "PATH="+bin, "TEST_WINDOWS_BIN="+native, "TEST_PS_RECEIPT="+receipt, "TEST_PS_EXIT="+code, "CODEAF_NO_MODIFY_PATH="+noModify)
			if run.code != 0 {
				t.Fatalf("installer exit %d:\n%s", run.code, run.output)
			}
			raw, err := os.ReadFile(receipt)
			if mode == "opt-out" {
				if !os.IsNotExist(err) {
					t.Fatalf("opt-out ran PowerShell: %s, %v", raw, err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				for _, want := range []string{"-NoProfile", "SetEnvironmentVariable", "'User'", "-ieq", "O''Brien ü"} {
					if !strings.Contains(string(raw), want) {
						t.Errorf("PATH command missing %q: %s", want, raw)
					}
				}
			}
			if mode != "success" && (!strings.Contains(run.output, "powershell.exe -NoProfile -Command") || !strings.Contains(run.output, "new terminal")) {
				t.Fatalf("missing exact recovery command:\n%s", run.output)
			}
			if mode != "success" {
				// The printed line must survive Bash parsing with every path character.
				hint := ""
				for _, line := range strings.Split(run.output, "\n") {
					if candidate := strings.TrimSpace(line); strings.HasPrefix(candidate, "powershell.exe -NoProfile -Command ") {
						hint = candidate
					}
				}
				bash, err := exec.LookPath("bash")
				if err != nil {
					t.Fatal(err)
				}
				command := exec.Command(bash, "-c", hint)
				command.Env = []string{"PATH=" + bin, "TEST_PS_RECEIPT=" + receipt, "TEST_PS_EXIT=0"}
				if output, err := command.CombinedOutput(); err != nil {
					t.Fatalf("recovery command: %v, %s", err, output)
				}
				decoded, err := os.ReadFile(receipt)
				if err != nil || !strings.Contains(string(decoded), "O''Brien ü") || !strings.Contains(string(decoded), "SetEnvironmentVariable") {
					t.Fatalf("recovery command changed the path: %s, %v", decoded, err)
				}
			}
			if mode == "success" && !strings.Contains(run.output, "Windows user PATH") {
				t.Fatalf("missing PATH receipt:\n%s", run.output)
			}
		})
	}
}
