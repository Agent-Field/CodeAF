package main

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/enginehost"
	codeupdate "github.com/Agent-Field/codeaf/internal/update"
)

func withUpdateDoor(t *testing.T, revision string, client *codeupdate.Client, target string) (*bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	oldOut, oldErr := updateOut, updateErr
	oldExecutable, oldClient := updateExecutable, updateClient
	oldVersion, oldRevision := updateVersionLine, updateRevision
	updateOut, updateErr = stdout, stderr
	updateExecutable = func() (string, error) { return target, nil }
	updateClient = func(string, time.Duration) *codeupdate.Client { return client }
	updateVersionLine = func(string, io.Writer, io.Writer) error { return nil }
	updateRevision = func() string { return revision }
	t.Cleanup(func() {
		updateOut, updateErr = oldOut, oldErr
		updateExecutable, updateClient = oldExecutable, oldClient
		updateVersionLine, updateRevision = oldVersion, oldRevision
	})
	return stdout, stderr
}

func updateExit(err error) int {
	if err == nil {
		return 0
	}
	var status exitStatus
	if errors.As(err, &status) {
		return int(status)
	}
	return 1
}

// TestC9UpdateCheckUsesTheRealDoorAndItsThreeExitCodes proves C9.
func TestC9UpdateCheckUsesTheRealDoorAndItsThreeExitCodes(t *testing.T) {
	for _, row := range []struct {
		name     string
		revision string
		status   int
		body     string
		want     int
	}{
		{"newer", "v0.1.1", http.StatusOK, `{"tag_name":"v0.2.0"}`, 3},
		{"newest", "v0.2.0", http.StatusOK, `{"tag_name":"v0.2.0"}`, 0},
		{"failure", "v0.1.1", http.StatusInternalServerError, "no", 1},
	} {
		t.Run(row.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(row.status)
				fmt.Fprint(w, row.body)
			}))
			defer server.Close()
			client := &codeupdate.Client{HTTP: server.Client(), APIBase: server.URL, DownloadBase: server.URL}
			stdout, stderr := withUpdateDoor(t, row.revision, client, filepath.Join(t.TempDir(), "codeaf"))
			if got := updateExit(runUpdate([]string{"--check"})); got != row.want {
				t.Fatalf("exit = %d, want %d; stdout %q stderr %q", got, row.want, stdout.String(), stderr.String())
			}
		})
	}
}

// TestC9bUpdateCheckNamesTheSelectedTagAndUsesItsChannelRules proves C9b.
func TestC9bUpdateCheckNamesTheSelectedTagAndUsesItsChannelRules(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/releases/latest") {
			fmt.Fprint(w, `{"tag_name":"v0.2.0"}`)
			return
		}
		fmt.Fprint(w, `[
			{"tag_name":"v0.1.1-rc.2","published_at":"2026-09-14T12:00:00Z"},
			{"tag_name":"dev-20260914-bbbbbbbbbbbb","published_at":"2026-09-14T13:00:00Z"},
			{"tag_name":"staging-20260914-cccccccccccc","published_at":"2026-09-14T14:00:00Z"}
		]`)
	}))
	defer server.Close()
	client := &codeupdate.Client{HTTP: server.Client(), APIBase: server.URL, DownloadBase: server.URL}

	for _, row := range []struct {
		name     string
		args     []string
		running  string
		selected string
		want     int
	}{
		{"stable newer", []string{"--check", "--stable"}, "v0.1.0", "v0.2.0", 3},
		{"stable equal", []string{"--check", "--stable"}, "v0.2.0", "v0.2.0", 0},
		{"rc newer", []string{"--check", "--rc"}, "v0.1.1-rc.1", "v0.1.1-rc.2", 3},
		{"rc equal", []string{"--check", "--rc"}, "v0.1.1-rc.2", "v0.1.1-rc.2", 0},
		{"dev different", []string{"--check", "--dev"}, "dev-20260913-aaaaaaaaaaaa", "dev-20260914-bbbbbbbbbbbb", 3},
		{"dev equal", []string{"--check", "--dev"}, "dev-20260914-bbbbbbbbbbbb", "dev-20260914-bbbbbbbbbbbb", 0},
		{"staging different", []string{"--check", "--staging"}, "staging-20260913-dddddddddddd", "staging-20260914-cccccccccccc", 3},
		{"staging equal", []string{"--check", "--staging"}, "staging-20260914-cccccccccccc", "staging-20260914-cccccccccccc", 0},
		{"version different", []string{"--check", "--version", "v0.2.0"}, "v0.3.0", "v0.2.0", 3},
		{"version equal", []string{"--check", "--version", "v0.2.0"}, "v0.2.0", "v0.2.0", 0},
	} {
		t.Run(row.name, func(t *testing.T) {
			stdout, stderr := withUpdateDoor(t, row.running, client, filepath.Join(t.TempDir(), "codeaf"))
			if got := updateExit(runUpdate(row.args)); got != row.want {
				t.Fatalf("exit = %d, want %d; stdout %q stderr %q", got, row.want, stdout.String(), stderr.String())
			}
			if !strings.Contains(stdout.String(), row.selected) {
				t.Fatalf("output does not name selected tag %q: %q", row.selected, stdout.String())
			}
		})
	}
}

// TestC9SourceBuildRefusesBeforeTheNetwork proves C9.
func TestC9SourceBuildRefusesBeforeTheNetwork(t *testing.T) {
	target := filepath.Join(t.TempDir(), "codeaf")
	if err := os.WriteFile(target, []byte("source"), 0o755); err != nil {
		t.Fatal(err)
	}
	client := &codeupdate.Client{HTTP: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("source install reached the network")
		return nil, nil
	})}}
	_, stderr := withUpdateDoor(t, "deadbeef", client, target)
	if code := updateExit(runUpdate(nil)); code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	for _, want := range []string{"this codeaf was built from source (deadbeef) at " + target, "rebuild with make build", codeupdate.CurlCommand} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr does not contain %q: %q", want, stderr.String())
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

// TestC9ExactVersionAndNewestDevSelectionInstallTheChosenTag proves C9.
func TestC9ExactVersionAndNewestDevSelectionInstallTheChosenTag(t *testing.T) {
	for _, row := range []struct {
		name string
		args []string
		tag  string
	}{
		{"exact", []string{"--version", "v0.1.1-rc.1"}, "v0.1.1-rc.1"},
		{"dev", []string{"--dev"}, "dev-20260915-bbbbbbbbbbbb"},
	} {
		t.Run(row.name, func(t *testing.T) {
			asset := []byte("installed " + row.tag)
			digest := sha256.Sum256(asset)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.Contains(r.URL.Path, "/releases") && !strings.Contains(r.URL.Path, "/download/"):
					fmt.Fprint(w, `[{"tag_name":"dev-20260915-aaaaaaaaaaaa","published_at":"2026-09-15T15:00:00Z"},{"tag_name":"dev-20260915-bbbbbbbbbbbb","published_at":"2026-09-15T16:00:00Z"}]`)
				case strings.HasSuffix(r.URL.Path, "/checksums.txt"):
					fmt.Fprintf(w, "%x  codeaf-%s-%s\n", digest, runtime.GOOS, runtime.GOARCH)
				case strings.Contains(r.URL.Path, "/releases/download/"+row.tag+"/"):
					_, _ = w.Write(asset)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			target := filepath.Join(t.TempDir(), "codeaf")
			if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
				t.Fatal(err)
			}
			client := &codeupdate.Client{HTTP: server.Client(), APIBase: server.URL, DownloadBase: server.URL}
			stdout, _ := withUpdateDoor(t, "v0.1.0", client, target)
			if err := runUpdate(row.args); err != nil {
				t.Fatal(err)
			}
			got, _ := os.ReadFile(target)
			if string(got) != string(asset) || !strings.Contains(stdout.String(), "installed "+row.tag) {
				t.Fatalf("target %q stdout %q", got, stdout.String())
			}
		})
	}
}

// TestC9AnInstallEndsWithTheNewBinarysVersionLine proves C9 and D10's
// last-line contract through the command door.
func TestC9AnInstallEndsWithTheNewBinarysVersionLine(t *testing.T) {
	asset := []byte("installed release")
	digest := sha256.Sum256(asset)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/checksums.txt"):
			fmt.Fprintf(w, "%x  codeaf-%s-%s\n", digest, runtime.GOOS, runtime.GOARCH)
		case strings.Contains(r.URL.Path, "/releases/download/v0.2.0/"):
			_, _ = w.Write(asset)
		default:
			fmt.Fprint(w, `{"tag_name":"v0.2.0"}`)
		}
	}))
	defer server.Close()
	target := filepath.Join(t.TempDir(), "codeaf")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	client := &codeupdate.Client{HTTP: server.Client(), APIBase: server.URL, DownloadBase: server.URL}
	stdout, _ := withUpdateDoor(t, "v0.1.0", client, target)
	updateVersionLine = func(path string, output, _ io.Writer) error {
		if path != target {
			t.Fatalf("version path = %q, want %q", path, target)
		}
		_, err := fmt.Fprintln(output, "codeaf v0.2.0 built 2026-09-15 12:00")
		return err
	}
	if err := runUpdate(nil); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if got := lines[len(lines)-1]; got != "codeaf v0.2.0 built 2026-09-15 12:00" {
		t.Fatalf("last line = %q; all output:\n%s", got, stdout.String())
	}
}

// TestC15TerminalUpdateRefusesAnImplicitDowngradeAndAnExactTagInstalls proves C15.
func TestC15TerminalUpdateRefusesAnImplicitDowngradeAndAnExactTagInstalls(t *testing.T) {
	asset := []byte("selected older release")
	digest := sha256.Sum256(asset)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/releases/latest"):
			fmt.Fprint(w, `{"tag_name":"v0.2.0"}`)
		case strings.HasSuffix(r.URL.Path, "/checksums.txt"):
			fmt.Fprintf(w, "%x  codeaf-%s-%s\n", digest, runtime.GOOS, runtime.GOARCH)
		case strings.Contains(r.URL.Path, "/releases/download/v0.2.0/"):
			_, _ = w.Write(asset)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := &codeupdate.Client{HTTP: server.Client(), APIBase: server.URL, DownloadBase: server.URL}

	t.Run("implicit stable", func(t *testing.T) {
		target := filepath.Join(t.TempDir(), "codeaf")
		if err := os.WriteFile(target, []byte("ahead release"), 0o755); err != nil {
			t.Fatal(err)
		}
		stdout, stderr := withUpdateDoor(t, "v0.3.0", client, target)
		if got := updateExit(runUpdate(nil)); got != 2 {
			t.Fatalf("exit = %d, want 2; stdout %q stderr %q", got, stdout.String(), stderr.String())
		}
		want := "this codeaf is v0.3.0, ahead of the newest stable v0.2.0 — pass --version v0.2.0 to install it anyway\n"
		if stderr.String() != want || stdout.Len() != 0 {
			t.Fatalf("stdout %q stderr %q, want stderr %q", stdout.String(), stderr.String(), want)
		}
		if got, err := os.ReadFile(target); err != nil || string(got) != "ahead release" {
			t.Fatalf("target = %q, %v", got, err)
		}
	})

	t.Run("exact tag", func(t *testing.T) {
		target := filepath.Join(t.TempDir(), "codeaf")
		if err := os.WriteFile(target, []byte("ahead release"), 0o755); err != nil {
			t.Fatal(err)
		}
		stdout, _ := withUpdateDoor(t, "v0.3.0", client, target)
		if err := runUpdate([]string{"--version", "v0.2.0"}); err != nil {
			t.Fatal(err)
		}
		if got, err := os.ReadFile(target); err != nil || string(got) != string(asset) {
			t.Fatalf("target = %q, %v", got, err)
		}
		if !strings.Contains(stdout.String(), "installed v0.2.0") {
			t.Fatalf("stdout = %q", stdout.String())
		}
	})
}

// TestTerminalDownloadFailureEndsWithTheCurlFallback proves D9's failure line.
func TestTerminalDownloadFailureEndsWithTheCurlFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/releases/latest") {
			fmt.Fprint(w, `{"tag_name":"v0.2.0"}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	target := filepath.Join(t.TempDir(), "codeaf")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	client := &codeupdate.Client{HTTP: server.Client(), APIBase: server.URL, DownloadBase: server.URL}
	_, _ = withUpdateDoor(t, "v0.1.0", client, target)
	err := runUpdate(nil)
	if err == nil {
		t.Fatal("download failure returned no error")
	}
	wantEnd := "; install a release with: " + codeupdate.CurlCommand
	if !strings.HasSuffix(err.Error(), wantEnd) {
		t.Fatalf("failure does not end with the curl fallback: %q", err)
	}
}

// TestC14RestartArgumentsPassTheRealChatFlagParser proves C14.
func TestC14RestartArgumentsPassTheRealChatFlagParser(t *testing.T) {
	for _, original := range [][]string{nil, {"chat", "--model", "x"}, {"resume", "--session", "/tmp/this.jsonl"}} {
		args := codeupdate.RestartArgs(original, "/tmp/this.jsonl")
		name := args[0]
		if name != "chat" && name != "resume" {
			t.Fatalf("restart command = %q", name)
		}
		// An invalid reasoning value stops the real door immediately after its
		// private flag set has parsed, before it can open a profile or session.
		parsed, _, err := watchParses(t, func() error {
			withStop := append(append([]string(nil), args[1:]...), "--reasoning", "not-a-level")
			return openChatV3(name, withStop, name == "resume")
		})
		if err == nil || len(parsed) != 1 {
			t.Fatalf("%q reached %d parses and error %v", args, len(parsed), err)
		}
		if got := parsed[0].values["session"]; got != "/tmp/this.jsonl" {
			t.Fatalf("%q parsed --session as %q", args, got)
		}
		if name == "chat" && len(original) > 1 && original[1] == "--model" && parsed[0].values["model"] != "x" {
			t.Fatalf("%q lost --model: %+v", args, parsed[0].values)
		}
	}
}

// TestC7TheDoorWaitsOutItsOwnDetachBeforeRestarting proves the local-engine
// half of C7 and D8 without sleeping or replacing the test process.
func TestC7TheDoorWaitsOutItsOwnDetachBeforeRestarting(t *testing.T) {
	oldRetire, oldRestart := retireChatEngine, restartUpdatedChat
	oldNow, oldPause := updateRestartNow, updateRestartPause
	t.Cleanup(func() {
		retireChatEngine, restartUpdatedChat = oldRetire, oldRestart
		updateRestartNow, updateRestartPause = oldNow, oldPause
	})
	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	updateRestartNow = func() time.Time { return now }
	updateRestartPause = func(wait time.Duration) { now = now.Add(wait) }
	var attempts int
	retireChatEngine = func(workspace string, anyway bool) error {
		attempts++
		if workspace != "/tmp/lab" || anyway {
			t.Fatalf("retire(%q, %t)", workspace, anyway)
		}
		if attempts < 3 {
			return enginehost.ErrHostBusy
		}
		return nil
	}
	want := codeupdate.Plan{Path: "/tmp/codeaf", Args: []string{"chat", "--session", "/tmp/this.jsonl"}}
	restarted := false
	restartUpdatedChat = func(got codeupdate.Plan) error {
		restarted = true
		if got.Path != want.Path || strings.Join(got.Args, "\x00") != strings.Join(want.Args, "\x00") {
			t.Fatalf("restart = %+v, want %+v", got, want)
		}
		return nil
	}
	if err := finishChatRestart(nil, &want, "/tmp/lab"); err != nil {
		t.Fatal(err)
	}
	if attempts != 3 || !restarted {
		t.Fatalf("retire attempts = %d, restarted = %t", attempts, restarted)
	}
}
