package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestOnlyAStableCodeafMovingToANewerStableIsUpdatedUnasked is the rule that
// keeps the automatic road off every build somebody working ON codeaf runs:
// source builds, devaf and stageaf, a dev build installed as codeaf, a release
// candidate, and any other file name. Each is told what is out and never
// replaced unasked.
func TestOnlyAStableCodeafMovingToANewerStableIsUpdatedUnasked(t *testing.T) {
	for _, row := range []struct {
		name, running, latest, executable string
		want                              bool
	}{
		{"a released codeaf behind the newest stable", "v0.7.0", "v0.7.1", "/home/p/.codeaf/bin/codeaf", true},
		{"a released codeaf on Windows", "v0.7.0", "v0.8.0", "/opt/tools/codeaf.exe", true},
		{"already the newest", "v0.7.1", "v0.7.1", "/home/p/.codeaf/bin/codeaf", false},
		{"ahead of the newest", "v0.8.0", "v0.7.1", "/home/p/.codeaf/bin/codeaf", false},
		{"a source build from make build", "a65a9eed", "v0.7.1", "/src/codeaf/bin/codeaf", false},
		{"an unstamped build", "", "v0.7.1", "/src/codeaf/bin/codeaf", false},
		{"a release candidate", "v0.7.1-rc.2", "v0.7.1", "/home/p/.codeaf/bin/codeaf", false},
		{"a dev build installed as codeaf", "dev-20261001-0123456789ab", "dev-20261013-0123456789ac", "/home/p/.codeaf/bin/codeaf", false},
		{"devaf", "dev-20261001-0123456789ab", "dev-20261013-0123456789ac", "/home/p/.codeaf/bin/devaf", false},
		{"stageaf", "staging-20261001-0123456789ab", "staging-20261009-0123456789ac", "/home/p/.codeaf/bin/stageaf", false},
		{"a stable release under another name", "v0.7.0", "v0.7.1", "/home/p/.codeaf/bin/oldaf", false},
		{"a stable release offered a prerelease", "v0.7.0", "v0.8.0-rc.1", "/home/p/.codeaf/bin/codeaf", false},
		{"no executable to name", "v0.7.0", "v0.7.1", "", false},
	} {
		t.Run(row.name, func(t *testing.T) {
			if got := AutomaticFor(row.running, row.latest, row.executable); got != row.want {
				t.Fatalf("AutomaticFor(%q, %q, %q) = %t, want %t", row.running, row.latest, row.executable, got, row.want)
			}
		})
	}

	// THE NAME IS THE FILE THAT WOULD BE OVERWRITTEN, through every link, so
	// one install answers the same on every platform whatever os.Executable
	// happens to resolve there.
	dir := t.TempDir()
	write := func(name string) string {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("bytes"), 0o755); err != nil {
			t.Fatal(err)
		}
		return path
	}
	link := func(name, target string) string {
		path := filepath.Join(dir, name)
		if err := os.Symlink(target, path); err != nil {
			t.Skipf("this filesystem has no symlinks: %v", err)
		}
		return path
	}
	versioned := write("tools/codeaf-v0.7.0")
	installed := write("home/.codeaf/bin/codeaf")
	for _, row := range []struct {
		name, executable string
		want             bool
	}{
		{"a link named codeaf to a file named otherwise", link("codeaf", versioned), false},
		{"a link named devaf to a file named codeaf", link("devaf", installed), true},
		{"a link to a folder holding codeaf", filepath.Join(link("linked-bin", filepath.Dir(installed)), "codeaf"), true},
	} {
		t.Run(row.name, func(t *testing.T) {
			if got := AutomaticFor("v0.7.0", "v0.7.1", row.executable); got != row.want {
				t.Fatalf("AutomaticFor through %q = %t, want %t", row.executable, got, row.want)
			}
		})
	}
}

// TestALaunchHearsTheBuildsHalfFromTheCheck proves both launch roads get the
// rule's answer from the check, about the resolved file, and only when a
// release is newer.
func TestALaunchHearsTheBuildsHalfFromTheCheck(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	file := filepath.Join(dir, "codeaf")
	if err := os.WriteFile(file, []byte("bytes"), 0o755); err != nil {
		t.Fatal(err)
	}
	devaf := filepath.Join(dir, "devaf")
	if err := os.Symlink(file, devaf); err != nil {
		t.Skipf("this filesystem has no symlinks: %v", err)
	}
	for _, row := range []struct {
		name, running, executable string
		want                      bool
	}{
		{"a stable codeaf behind", "v0.7.0", file, true},
		{"devaf linked to that codeaf", "v0.7.0", devaf, true},
		{"a release candidate", "v0.7.1-rc.1", file, false},
	} {
		t.Run(row.name, func(t *testing.T) {
			profile := t.TempDir()
			if err := saveCheckCache(filepath.Join(profile, "update-check.json"), checkCache{CheckedAt: now.Add(-time.Hour), Latest: "v0.7.1", Running: row.running}); err != nil {
				t.Fatal(err)
			}
			answer, newer := CheckLaunch(context.Background(), CheckOptions{
				Running: row.running, ProfileDir: profile, Executable: row.executable, Now: func() time.Time { return now },
			})
			if !newer || answer.Automatic != row.want {
				t.Fatalf("answer = %+v, newer %t; want Automatic %t", answer, newer, row.want)
			}
		})
	}
}

// TestASlowLaunchIsRememberedForThatReleaseOnly proves the slow-download memory
// answers one release, for a day, and that a success clears it.
func TestASlowLaunchIsRememberedForThatReleaseOnly(t *testing.T) {
	profile := t.TempDir()
	AutoSlow(profile, "v0.7.1")
	state := LoadAutoState(profile)
	now := time.Now()
	if !state.SlowRecently("v0.7.1", now) {
		t.Fatalf("the slow release was not remembered: %+v", state)
	}
	if state.SlowRecently("v0.7.2", now) {
		t.Fatal("a later release was treated as the slow one")
	}
	if state.SlowRecently("v0.7.1", now.Add(slowInstallQuiet+time.Minute)) {
		t.Fatal("the slow release was remembered past its quiet period")
	}
	if state.Failures != 0 {
		t.Fatalf("a slow download counted as a failure: %+v", state)
	}
	AutoSuccess(profile)
	if LoadAutoState(profile).SlowRecently("v0.7.1", now) {
		t.Fatal("a success left the slow memory behind")
	}
}

// TestANamedTagPinsTheFileAndOnlyAPersonUnpinsIt proves the pin end to end
// through the one installer: a named tag pins, the automatic road is refused
// while it holds, and a person's plain install moves the file and lets it go.
// Naming the release already on disk pins it in place without a download, and
// a plain install of that same release unpins it the same way.
func TestANamedTagPinsTheFileAndOnlyAPersonUnpinsIt(t *testing.T) {
	asset := []byte("codeaf release bytes")
	digest := sha256.Sum256(asset)
	server, requests := servedRelease(t, asset, hex.EncodeToString(digest[:]))
	defer server.Close()
	target := filepath.Join(t.TempDir(), "codeaf")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	install := func(tag string, named, automatic bool) (InstallResult, error) {
		return Install(context.Background(), InstallOptions{
			Client: releaseClient(server, "v0.7.1"), Release: Release{Tag: tag, Repository: primaryRepository},
			Target: target, Curl: CurlCommand, AllowDowngrade: named, Automatic: automatic,
		})
	}

	pinned, err := install("v0.6.3", true, false)
	if err != nil || !pinned.Pinned {
		t.Fatalf("named install = %+v, %v; want it pinned", pinned, err)
	}
	if got := PinnedRelease(target); got != "v0.6.3" {
		t.Fatalf("PinnedRelease = %q, want v0.6.3", got)
	}
	_, err = install("v0.7.1", false, true)
	var refusal *RefusalError
	if !errors.As(err, &refusal) || !strings.Contains(refusal.Reason, "codeaf v0.6.3 is pinned") {
		t.Fatalf("automatic install over a pin = %v, want the pinned refusal", err)
	}
	if PinnedRelease(target) != "v0.6.3" {
		t.Fatal("a refused automatic install disturbed the pin")
	}

	moved, err := install("v0.7.1", false, false)
	if err != nil || moved.Already || moved.Pinned {
		t.Fatalf("plain install = %+v, %v; want a move that unpins", moved, err)
	}
	if got := PinnedRelease(target); got != "" {
		t.Fatalf("PinnedRelease after a plain install = %q, want nothing", got)
	}

	before := requests.Load()
	inPlace, err := install("v0.7.1", true, false)
	if err != nil || !inPlace.Already || !inPlace.Pinned || PinnedRelease(target) != "v0.7.1" {
		t.Fatalf("naming the release on disk = %+v, %v, pin %q; want it pinned in place", inPlace, err, PinnedRelease(target))
	}
	released, err := install("v0.7.1", false, false)
	if err != nil || !released.Already || released.Pinned || PinnedRelease(target) != "" {
		t.Fatalf("a plain install of the release on disk = %+v, %v, pin %q; want it unpinned", released, err, PinnedRelease(target))
	}
	if requests.Load() != before {
		t.Fatalf("pinning in place downloaded %d times", requests.Load()-before)
	}
	// The automatic road never writes a pin, even when it finds the release
	// already there.
	if quiet, err := install("v0.7.1", true, true); err != nil || quiet.Pinned || PinnedRelease(target) != "" {
		t.Fatalf("an automatic install of the release on disk = %+v, %v, pin %q", quiet, err, PinnedRelease(target))
	}
}

// TestAPinDiesWithTheBytesItWasWrittenFor proves a codeaf replaced by curl or a
// package manager is not held at a release it no longer is.
func TestAPinDiesWithTheBytesItWasWrittenFor(t *testing.T) {
	asset := []byte("pinned bytes")
	digest := sha256.Sum256(asset)
	server, _ := servedRelease(t, asset, hex.EncodeToString(digest[:]))
	defer server.Close()
	target := filepath.Join(t.TempDir(), "codeaf")
	if _, err := Install(context.Background(), InstallOptions{
		Client: releaseClient(server, "v0.7.1"), Release: Release{Tag: "v0.6.3", Repository: primaryRepository},
		Target: target, AllowDowngrade: true,
	}); err != nil {
		t.Fatal(err)
	}
	if PinnedRelease(target) != "v0.6.3" {
		t.Fatal("the named install did not pin")
	}
	if err := os.WriteFile(target, []byte("installed by curl"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := PinnedRelease(target); got != "" {
		t.Fatalf("a replaced file is still pinned at %q", got)
	}
	if got := PinnedRelease(filepath.Join(t.TempDir(), "codeaf")); got != "" {
		t.Fatalf("a file never installed is pinned at %q", got)
	}
}

// TestALaunchHearsThePinWithTheNewerRelease proves the check hands both launch
// roads the pin they need to decide with, and only when something is newer.
func TestALaunchHearsThePinWithTheNewerRelease(t *testing.T) {
	asset := []byte("pinned bytes")
	digest := sha256.Sum256(asset)
	server, _ := servedRelease(t, asset, hex.EncodeToString(digest[:]))
	defer server.Close()
	target := filepath.Join(t.TempDir(), "codeaf")
	if _, err := Install(context.Background(), InstallOptions{
		Client: releaseClient(server, "v0.6.3"), Release: Release{Tag: "v0.6.3", Repository: primaryRepository},
		Target: target, AllowDowngrade: true,
	}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	profile := t.TempDir()
	if err := saveCheckCache(filepath.Join(profile, "update-check.json"), checkCache{CheckedAt: now.Add(-time.Hour), Latest: "v0.7.1", Running: "v0.6.3"}); err != nil {
		t.Fatal(err)
	}
	answer, newer := CheckLaunch(context.Background(), CheckOptions{
		Running: "v0.6.3", ProfileDir: profile, Executable: target, Now: func() time.Time { return now },
	})
	if !newer || answer.Pinned != "v0.6.3" {
		t.Fatalf("launch answer = %+v, %t; want v0.7.1 with the pin", answer, newer)
	}
}

// TestALaunchThatGotNoAnswerDoesNotWaitAgainForAnHour proves the prelaunch look
// never costs a person the same wait twice in a row: a request that got no
// answer is written down, the next launch inside the hour makes none, the one
// after the hour asks again, and an answer clears it. The chat's own look
// behind a drawn frame is never held back by it.
func TestALaunchThatGotNoAnswerDoesNotWaitAgainForAnHour(t *testing.T) {
	var hits atomic.Int64
	var answer atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		if !answer.Load() {
			http.Error(w, "unreachable", http.StatusBadGateway)
			return
		}
		fmt.Fprint(w, `{"tag_name":"v0.7.1"}`)
	}))
	defer server.Close()
	client := releaseClient(server, "v0.7.0")
	profile := t.TempDir()
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	look := func(prelaunch bool) (Available, bool) {
		return CheckLaunch(context.Background(), CheckOptions{
			Running: "v0.7.0", ProfileDir: profile, Client: client, Prelaunch: prelaunch,
			Now: func() time.Time { return now },
		})
	}
	if _, newer := look(true); newer || hits.Load() != 1 {
		t.Fatalf("first launch: newer %t, hits %d", newer, hits.Load())
	}
	if _, err := os.Stat(filepath.Join(profile, prelaunchMissName)); err != nil {
		t.Fatalf("the miss was not written down: %v", err)
	}
	now = now.Add(59 * time.Minute)
	if _, newer := look(true); newer || hits.Load() != 1 {
		t.Fatalf("a launch inside the hour asked again: newer %t, hits %d", newer, hits.Load())
	}
	look(false)
	if hits.Load() != 2 {
		t.Fatalf("the chat's own look was held back by the miss: hits %d", hits.Load())
	}
	now = now.Add(2 * time.Minute)
	answer.Store(true)
	if available, newer := look(true); !newer || available.Latest != "v0.7.1" || hits.Load() != 3 {
		t.Fatalf("a launch after the hour = %+v, %t, hits %d", available, newer, hits.Load())
	}
	if _, err := os.Stat(filepath.Join(profile, prelaunchMissName)); !os.IsNotExist(err) {
		t.Fatalf("an answer did not clear the miss: %v", err)
	}
}

// TestAStoppedLookIsNotANetworkThatFailed proves ctrl+c during the look writes
// nothing down, so the next launch still asks.
func TestAStoppedLookIsNotANetworkThatFailed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()
	profile := t.TempDir()
	stopped, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	CheckLaunch(stopped, CheckOptions{Running: "v0.7.0", ProfileDir: profile, Client: releaseClient(server, "v0.7.0"), Prelaunch: true})
	if _, err := os.Stat(filepath.Join(profile, prelaunchMissName)); !os.IsNotExist(err) {
		t.Fatalf("a stopped look was written down as a miss: %v", err)
	}
}

// TestAnInstallReportsTheReleaseArriving proves the progress hook hears the
// asset with the size the server announced — and -1, never a guess, when the
// server announced none.
func TestAnInstallReportsTheReleaseArriving(t *testing.T) {
	asset := []byte(strings.Repeat("codeaf", 40_000))
	digest := sha256.Sum256(asset)
	checksum := hex.EncodeToString(digest[:])
	for _, row := range []struct {
		name     string
		announce bool
		want     int64
	}{{"announced", true, int64(len(asset))}, {"chunked", false, -1}} {
		t.Run(row.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/checksums.txt"):
					fmt.Fprintf(w, "%s  %s\n", checksum, assetName())
				default:
					if row.announce {
						w.Header().Set("Content-Length", fmt.Sprint(len(asset)))
					}
					_, _ = w.Write(asset)
				}
			}))
			defer server.Close()
			var last, total int64
			calls := 0
			_, err := Install(context.Background(), InstallOptions{
				Client: releaseClient(server, "v0.7.0"), Release: Release{Tag: "v0.7.1", Repository: primaryRepository},
				Target: filepath.Join(t.TempDir(), "codeaf"),
				Progress: func(received, announced int64) {
					calls++
					last, total = received, announced
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			if calls == 0 || last != int64(len(asset)) || total != row.want {
				t.Fatalf("progress heard %d calls ending at %d of %d, want %d of %d", calls, last, total, len(asset), row.want)
			}
		})
	}
}

// assetName is the release asset this machine downloads.
func assetName() string {
	return "codeaf-" + runtime.GOOS + "-" + runtime.GOARCH
}
