package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The fake ssh executes the actual remote command in a separate shell. No
// model or daemon is needed to observe which executable and arguments arrive.
func TestHostLaunchAndRedialRunTheSameProgram(t *testing.T) {
	dir := t.TempDir()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	name := filepath.Base(self)
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body), 0700); err != nil {
			t.Fatal(err)
		}
	}
	write("ssh", "for arg do command=$arg; done\nexec /bin/sh -c \"$command\"\n")
	write(name, "printf 'same-program\\n'; printf '%s\\n' \"$@\"\n")
	write("codeaf", "printf 'wrong-program\\n'\n")
	t.Setenv("PATH", dir)
	t.Setenv("CODEAF_HOME", t.TempDir())
	link := &engineLink{dest: "test-host", workspace: "my app/it's here"}
	for range 2 {
		pipe, err := link.spawn()
		if err != nil {
			t.Fatal(err)
		}
		got, err := io.ReadAll(pipe)
		_ = pipe.Close()
		if err != nil {
			t.Fatal(err)
		}
		_ = link.reap()
		if string(got) != "same-program\nengine\n--workspace\nmy app/it's here\n" {
			t.Fatalf("remote launch ran %q", got)
		}
	}
}

func TestMissingHostCommandIncludesChannelInstall(t *testing.T) {
	got := missingHostCommand("test-host").Error()
	if !strings.Contains(got, "https://agentfield.ai/get/") {
		t.Fatalf("no installation command: %s", got)
	}
}

func TestHostChannelSelectionThroughRemoteShell(t *testing.T) {
	for _, tc := range []struct {
		name            string
		primary, stable bool
		want            string
		notice          bool
		exit            int
	}{
		{"devaf", true, true, "primary", false, 0},
		{"stageaf", true, true, "primary", false, 0},
		{"custom-af", false, true, "stable", true, 0},
		{"devaf", false, false, "", false, 127},
		{"codeaf", false, true, "stable", false, 0},
		{"odd' name", true, true, "primary", false, 0},
	} {
		t.Run(tc.name+tc.want, func(t *testing.T) {
			dir := t.TempDir()
			write := func(name, output string) {
				t.Helper()
				body := "#!/bin/sh\nif [ \"$1\" = version ]; then printf 'rev-123\\n'; else printf '%s\\n' " + shellQuote(output) + " \"$@\"; fi\n"
				if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0700); err != nil {
					t.Fatal(err)
				}
			}
			if tc.primary {
				write(tc.name, "primary")
			}
			if tc.stable {
				write("codeaf", "stable")
			}
			for range 2 {
				cmd := exec.Command("/bin/sh", "-c", hostEngineCommand(tc.name, "test-host", "folder/it's here"))
				cmd.Env = append(os.Environ(), "PATH="+dir)
				var stdout, stderr bytes.Buffer
				cmd.Stdout = &stdout
				cmd.Stderr = &stderr
				err := cmd.Run()
				if tc.exit != 0 {
					var ex *exec.ExitError
					if !errors.As(err, &ex) || ex.ExitCode() != tc.exit {
						t.Fatalf("exit %v", err)
					}
					continue
				}
				if err != nil {
					t.Fatal(err)
				}
				want := tc.want + "\nengine\n--workspace\nfolder/it's here\n"
				if stdout.String() != want {
					t.Fatalf("got %q, want %q", stdout.String(), want)
				}
				if tc.notice {
					if stderr.String() != "test-host has no "+tc.name+"; using its codeaf (rev-123)\n" {
						t.Fatalf("notice %q", stderr.String())
					}
				} else if stderr.Len() != 0 {
					t.Fatalf("unexpected notice %q", stderr.String())
				}
			}
		})
	}
}
