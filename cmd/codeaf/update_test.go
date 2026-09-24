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

// updateTarget is a path for the binary a test pretends to be running, in a
// temporary directory THAT HAS ALREADY BEEN RESOLVED.
//
// IT IS RESOLVED BECAUSE THE DOOR RESOLVES. [codeupdate.ExecutableTarget] runs
// the path from `os.Executable` through [filepath.EvalSymlinks] on purpose: an
// installer has to replace the real file, and writing through a symlink replaces
// the link or lands somewhere nobody asked for. So a test that hands the door an
// unresolved path and then expects its own spelling back is asserting the
// opposite of the law it is testing.
//
// ON MACOS THAT IS EVERY TEST: `t.TempDir` answers under /var/folders/…, and
// /var is a symlink to /private/var — so the door correctly returned
// /private/var/… while two tests demanded /var/… and failed on every developer's
// laptop while passing in CI on Linux, where the two spellings are one.
//
// The directory is resolved rather than the file, because the file usually does
// not exist yet and [filepath.EvalSymlinks] needs what it is given to exist.
func updateTarget(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolving the temporary directory: %v", err)
	}
	return filepath.Join(dir, "codeaf")
}

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
			stdout, stderr := withUpdateDoor(t, row.revision, client, updateTarget(t))
			if got := updateExit(runUpdate([]string{"--check"})); got != row.want {
				t.Fatalf("exit = %d, want %d; stdout %q stderr %q", got, row.want, stdout.String(), stderr.String())
			}
		})
	}
}

// TestTimeoutContractC2HangingTerminalCheckExitsInsideItsBudget proves C2 for
// the real `codeaf update --check` door and its exit-1 failure road.
func TestTimeoutContractC2HangingTerminalCheckExitsInsideItsBudget(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		<-request.Context().Done()
	}))
	defer server.Close()
	client := &codeupdate.Client{HTTP: server.Client(), APIBase: server.URL, DownloadBase: server.URL}
	stdout, stderr := withUpdateDoor(t, "v0.1.0", client, updateTarget(t))
	started := time.Now()
	if got := updateExit(runUpdate([]string{"--check"})); got != 1 {
		t.Fatalf("exit = %d, want 1; stdout %q stderr %q", got, stdout.String(), stderr.String())
	}
	if elapsed := time.Since(started); elapsed > 6*time.Second {
		t.Fatalf("hanging terminal check took %s, want at most 6s", elapsed)
	}
	if stdout.Len() != 0 || !strings.Contains(stderr.String(), "codeaf: could not check for an update: release API did not answer within 3 s") {
		t.Fatalf("stdout %q stderr %q", stdout.String(), stderr.String())
	}
	if strings.Contains(stderr.String(), "context deadline exceeded") || strings.Contains(stderr.String(), "Client.Timeout") {
		t.Fatalf("terminal check exposed Go's timeout: %q", stderr.String())
	}
}

// TestTimeoutContractC4HangingInstallSelectionNamesTheAPI proves C4 through
// the terminal install door with a short injected API window.
func TestTimeoutContractC4HangingInstallSelectionNamesTheAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		<-request.Context().Done()
	}))
	defer server.Close()
	target := updateTarget(t)
	dir := filepath.Dir(target)
	original := []byte("original codeaf")
	if err := os.WriteFile(target, original, 0o700); err != nil {
		t.Fatal(err)
	}
	client := &codeupdate.Client{
		HTTP: server.Client(), APIBase: server.URL, DownloadBase: server.URL,
		APIWindow: 60 * time.Millisecond,
	}
	_, _ = withUpdateDoor(t, "v0.1.0", client, target)
	started := time.Now()
	err := runUpdate(nil)
	if err == nil || !strings.Contains(err.Error(), "could not select a release: release API did not answer within 60 ms") {
		t.Fatalf("error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("hanging install selection took %s, want at most 1s", elapsed)
	}
	if strings.Contains(err.Error(), "codeaf-") || strings.Contains(err.Error(), "context deadline exceeded") {
		t.Fatalf("API failure named an asset or Go's deadline: %q", err)
	}
	installed, readErr := os.ReadFile(target)
	if readErr != nil || string(installed) != string(original) {
		t.Fatalf("target = %q, error %v; want untouched original", installed, readErr)
	}
	if matches, _ := filepath.Glob(filepath.Join(dir, ".codeaf.tmp.*")); len(matches) != 0 {
		t.Fatalf("temporary files remain: %v", matches)
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
			stdout, stderr := withUpdateDoor(t, row.running, client, updateTarget(t))
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
	target := updateTarget(t)
	if err := os.WriteFile(target, []byte("source"), 0o755); err != nil {
		t.Fatal(err)
	}
	// The updater resolves symlinks, including macOS's temporary-directory alias.
	resolved, err := filepath.EvalSymlinks(target)
	if err != nil {
		t.Fatal(err)
	}
	target = resolved

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
			target := updateTarget(t)
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
	target := updateTarget(t)
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	// The updater resolves symlinks, including macOS's temporary-directory alias.
	resolved, err := filepath.EvalSymlinks(target)
	if err != nil {
		t.Fatal(err)
	}
	target = resolved

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
		target := updateTarget(t)
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
		target := updateTarget(t)
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
	target := updateTarget(t)
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

// V6: codeaf update and --check default to a dev build's own channel; --check
// reports newer and equal dev builds with the documented exit codes.
func TestV6TerminalUpdateDefaultsToTheRunningDevChannel(t *testing.T) {
	const (
		oldDev = "dev-20260918-aaaaaaaaaaaa"
		newDev = "dev-20260921-bbbbbbbbbbbb"
	)
	asset := []byte("new dev executable")
	digest := sha256.Sum256(asset)
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		paths = append(paths, request.URL.RequestURI())
		switch {
		case strings.HasSuffix(request.URL.Path, "/releases"):
			fmt.Fprint(w, `[
				{"tag_name":"`+oldDev+`","published_at":"2026-09-18T12:00:00Z"},
				{"tag_name":"`+newDev+`","published_at":"2026-09-21T12:00:00Z"}
			]`)
		case strings.HasSuffix(request.URL.Path, "/checksums.txt"):
			fmt.Fprintf(w, "%x  codeaf-%s-%s\n", digest, runtime.GOOS, runtime.GOARCH)
		case strings.Contains(request.URL.Path, "/releases/download/"+newDev+"/"):
			_, _ = w.Write(asset)
		default:
			http.NotFound(w, request)
		}
	}))
	defer server.Close()
	client := &codeupdate.Client{HTTP: server.Client(), APIBase: server.URL, DownloadBase: server.URL}

	t.Run("check newer", func(t *testing.T) {
		stdout, stderr := withUpdateDoor(t, oldDev, client, filepath.Join(t.TempDir(), "devaf"))
		if got := updateExit(runUpdate([]string{"--check"})); got != 3 {
			t.Fatalf("exit = %d; stdout %q stderr %q", got, stdout.String(), stderr.String())
		}
	})
	t.Run("check equal", func(t *testing.T) {
		stdout, stderr := withUpdateDoor(t, newDev, client, filepath.Join(t.TempDir(), "devaf"))
		if got := updateExit(runUpdate([]string{"--check"})); got != 0 {
			t.Fatalf("exit = %d; stdout %q stderr %q", got, stdout.String(), stderr.String())
		}
	})
	t.Run("install", func(t *testing.T) {
		target := filepath.Join(t.TempDir(), "devaf")
		if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
			t.Fatal(err)
		}
		stdout, stderr := withUpdateDoor(t, oldDev, client, target)
		if err := runUpdate(nil); err != nil {
			t.Fatalf("update: %v; stdout %q stderr %q", err, stdout.String(), stderr.String())
		}
		if got, err := os.ReadFile(target); err != nil || string(got) != string(asset) {
			t.Fatalf("installed = %q, %v", got, err)
		}
	})
	for _, path := range paths {
		if strings.Contains(path, "/releases/latest") {
			t.Fatalf("dev default asked for stable: %s", path)
		}
	}
}

// V6: An explicit channel still wins on a dev build, and a check across two
// channels says only what it can: a channel tag and a version number cannot be
// ordered, so the difference itself is the answer.
func TestV6CrossChannelChecksSayOnlyWhatTheyCanOrder(t *testing.T) {
	const devTag = "dev-20260921-bbbbbbbbbbbb"
	for _, row := range []struct {
		name, running, path, want string
		arguments                 []string
		exit                      int
	}{
		{
			name: "a dev build asking for stable", running: "dev-20260918-aaaaaaaaaaaa",
			arguments: []string{"--check", "--stable"}, path: "/releases/latest",
			want: "the newest stable codeaf is v0.3.0 · this codeaf is dev-20260918-aaaaaaaaaaaa\n",
		},
		{
			name: "a stable build asking for dev", running: "v0.3.0",
			arguments: []string{"--check", "--dev"}, path: "/releases", exit: 3,
			want: "codeaf " + devTag + " is available · you have v0.3.0\n",
		},
	} {
		t.Run(row.name, func(t *testing.T) {
			var paths []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				paths = append(paths, request.URL.Path)
				if strings.HasSuffix(request.URL.Path, "/releases/latest") {
					fmt.Fprint(w, `{"tag_name":"v0.3.0"}`)
					return
				}
				fmt.Fprint(w, `[{"tag_name":"`+devTag+`","published_at":"2026-09-21T12:00:00Z"}]`)
			}))
			defer server.Close()
			client := &codeupdate.Client{HTTP: server.Client(), APIBase: server.URL, DownloadBase: server.URL}
			stdout, stderr := withUpdateDoor(t, row.running, client, filepath.Join(t.TempDir(), "devaf"))
			if got := updateExit(runUpdate(row.arguments)); got != row.exit {
				t.Fatalf("exit = %d, want %d; stderr %q", got, row.exit, stderr.String())
			}
			if stdout.String() != row.want {
				t.Fatalf("stdout = %q, want %q", stdout.String(), row.want)
			}
			if len(paths) != 1 || !strings.HasSuffix(paths[0], row.path) {
				t.Fatalf("requests = %q, want one ending in %q", paths, row.path)
			}
		})
	}
}

// D6: a release candidate updates from stable, so a failure on one offers the
// stable road. Handing an rc user the /get/codeaf/rc line would reinstall a
// channel their own /update never selects.
func TestAFailedReleaseCandidateUpdateOffersTheStableRoad(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if strings.HasSuffix(request.URL.Path, "/releases/latest") {
			fmt.Fprint(w, `{"tag_name":"v0.3.0"}`)
			return
		}
		http.NotFound(w, request)
	}))
	defer server.Close()
	client := &codeupdate.Client{HTTP: server.Client(), APIBase: server.URL, DownloadBase: server.URL}
	target := filepath.Join(t.TempDir(), "codeaf")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, _ = withUpdateDoor(t, "v0.3.0-rc.1", client, target)
	err := runUpdate(nil)
	if err == nil || !strings.Contains(err.Error(), "install a release with: "+codeupdate.CurlCommand) {
		t.Fatalf("failure = %v, want the stable road", err)
	}
	if strings.Contains(err.Error(), "/get/codeaf/rc") {
		t.Fatalf("failure offered an rc road: %v", err)
	}
}

// V7 and D4: a publish moment outranks the date written into the tag. A build
// whose tag carries the later day but which was published FIRST is behind, so
// the terminal door installs rather than calling it a downgrade — which it can
// only get right by carrying both moments out of the release list.
func TestV7APublishMomentOutranksTheDateInTheTag(t *testing.T) {
	const (
		running  = "dev-20260921-aaaaaaaaaaaa"
		selected = "dev-20260918-bbbbbbbbbbbb"
	)
	asset := []byte("the later dev build")
	digest := sha256.Sum256(asset)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch {
		case strings.HasSuffix(request.URL.Path, "/releases"):
			fmt.Fprint(w, `[
				{"tag_name":"`+running+`","published_at":"2026-09-18T10:00:00Z"},
				{"tag_name":"`+selected+`","published_at":"2026-09-18T12:00:00Z"}
			]`)
		case strings.HasSuffix(request.URL.Path, "/checksums.txt"):
			fmt.Fprintf(w, "%x  codeaf-%s-%s\n", digest, runtime.GOOS, runtime.GOARCH)
		case strings.Contains(request.URL.Path, "/releases/download/"+selected+"/"):
			_, _ = w.Write(asset)
		default:
			http.NotFound(w, request)
		}
	}))
	defer server.Close()
	client := &codeupdate.Client{HTTP: server.Client(), APIBase: server.URL, DownloadBase: server.URL}
	target := filepath.Join(t.TempDir(), "devaf")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	stdout, stderr := withUpdateDoor(t, running, client, target)
	if err := runUpdate(nil); err != nil {
		t.Fatalf("update: %v; stdout %q stderr %q", err, stdout.String(), stderr.String())
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != string(asset) {
		t.Fatalf("installed = %q, %v", got, err)
	}
}

// V7: A terminal dev update refuses an implicit downgrade, --check calls it
// equal-or-ahead with exit 0, and --version still installs the named release.
func TestV7TerminalUpdateRefusesAnAheadDevUnlessTheTagIsNamed(t *testing.T) {
	const (
		running = "dev-20260921-bbbbbbbbbbbb"
		newest  = "dev-20260918-aaaaaaaaaaaa"
	)
	asset := []byte("named older dev")
	digest := sha256.Sum256(asset)
	downloads := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch {
		case strings.HasSuffix(request.URL.Path, "/releases"):
			fmt.Fprint(w, `[{"tag_name":"`+newest+`","published_at":"2026-09-18T12:00:00Z"}]`)
		case strings.HasSuffix(request.URL.Path, "/checksums.txt"):
			downloads++
			fmt.Fprintf(w, "%x  codeaf-%s-%s\n", digest, runtime.GOOS, runtime.GOARCH)
		case strings.Contains(request.URL.Path, "/releases/download/"+newest+"/"):
			downloads++
			_, _ = w.Write(asset)
		default:
			http.NotFound(w, request)
		}
	}))
	defer server.Close()
	client := &codeupdate.Client{HTTP: server.Client(), APIBase: server.URL, DownloadBase: server.URL}

	target := filepath.Join(t.TempDir(), "devaf")
	if err := os.WriteFile(target, []byte("ahead"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, stderr := withUpdateDoor(t, running, client, target)
	if got := updateExit(runUpdate(nil)); got != 2 {
		t.Fatalf("implicit exit = %d, stderr %q", got, stderr.String())
	}
	want := "this codeaf is " + running + ", ahead of the newest dev " + newest + " — pass --version " + newest + " to install it anyway\n"
	if stderr.String() != want || downloads != 0 {
		t.Fatalf("stderr = %q, downloads = %d", stderr.String(), downloads)
	}

	stdout, stderr := withUpdateDoor(t, running, client, target)
	if got := updateExit(runUpdate([]string{"--check"})); got != 0 {
		t.Fatalf("check exit = %d, stderr %q", got, stderr.String())
	}
	checkLine := "the newest dev codeaf is " + newest + " · this codeaf is " + running + "\n"
	if stdout.String() != checkLine {
		t.Fatalf("check = %q, want %q", stdout.String(), checkLine)
	}

	stdout, stderr = withUpdateDoor(t, running, client, target)
	if err := runUpdate([]string{"--version", newest}); err != nil {
		t.Fatalf("named update: %v; stderr %q", err, stderr.String())
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != string(asset) {
		t.Fatalf("named install = %q, %v", got, err)
	}
}

// V8: Terminal update failures use the curl road for the running executable
// and channel instead of silently handing a devaf user the stable codeaf line.
func TestV8TerminalFailureUsesTheRunningFilesCurlLine(t *testing.T) {
	const running = "dev-20260918-aaaaaaaaaaaa"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if strings.HasSuffix(request.URL.Path, "/releases") {
			fmt.Fprint(w, `[{"tag_name":"dev-20260921-bbbbbbbbbbbb","published_at":"2026-09-21T12:00:00Z"}]`)
			return
		}
		http.NotFound(w, request)
	}))
	defer server.Close()
	target := filepath.Join(t.TempDir(), "devaf")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	client := &codeupdate.Client{HTTP: server.Client(), APIBase: server.URL, DownloadBase: server.URL}
	_, _ = withUpdateDoor(t, running, client, target)
	err := runUpdate(nil)
	const want = "install a release with: curl -fsSL https://agentfield.ai/get/devaf | bash"
	if err == nil || !strings.Contains(err.Error(), want) || strings.Contains(err.Error(), "/get/codeaf/dev") {
		t.Fatalf("failure = %v", err)
	}
}

// C22: A staging build installed as stageaf offers the same installer on a failed update.
func TestC22StageafFailureUsesTheStageafCurlLine(t *testing.T) {
	const running = "staging-20260918-aaaaaaaaaaaa"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if strings.HasSuffix(request.URL.Path, "/releases") {
			fmt.Fprint(w, `[{"tag_name":"staging-20260921-bbbbbbbbbbbb","published_at":"2026-09-21T12:00:00Z"}]`)
			return
		}
		http.NotFound(w, request)
	}))
	defer server.Close()
	target := filepath.Join(t.TempDir(), "stageaf")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	client := &codeupdate.Client{HTTP: server.Client(), APIBase: server.URL, DownloadBase: server.URL}
	_, _ = withUpdateDoor(t, running, client, target)
	err := runUpdate(nil)
	const want = "install a release with: curl -fsSL https://agentfield.ai/get/stageaf | bash"
	if err == nil || !strings.Contains(err.Error(), want) || strings.Contains(err.Error(), "/get/codeaf/staging") {
		t.Fatalf("failure = %v", err)
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
