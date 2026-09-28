package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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

func releaseClient(server *httptest.Server, revision string) *Client {
	return &Client{
		HTTP: server.Client(), APIBase: server.URL, DownloadBase: server.URL, Revision: revision,
	}
}

// TestC1StableLaunchComparisonAndNotice proves C1.
func TestC1StableLaunchComparisonAndNotice(t *testing.T) {
	for _, row := range []struct {
		running string
		show    bool
	}{{"v0.1.1", true}, {"v0.2.0", false}, {"v0.3.0", false}} {
		t.Run(row.running, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprint(w, `{"tag_name":"v0.2.0"}`)
			}))
			defer server.Close()
			answer, show := CheckLaunch(context.Background(), CheckOptions{
				Running: row.running, ProfileDir: t.TempDir(), Client: releaseClient(server, row.running),
			})
			if show != row.show {
				t.Fatalf("show = %t, want %t", show, row.show)
			}
			if row.show {
				line := answer.Notice()
				for _, want := range []string{"v0.2.0", "v0.1.1", "/update", CurlCommand} {
					if strings.Count(line, want) != 1 {
						t.Fatalf("notice %q does not contain %q exactly once", line, want)
					}
				}
			}
		})
	}
}

// TestC2ReleaseCandidateIsPromptedOnlyWhenItsLineIsStable proves C2.
func TestC2ReleaseCandidateIsPromptedOnlyWhenItsLineIsStable(t *testing.T) {
	for _, row := range []struct {
		latest string
		show   bool
	}{{"v0.2.0", true}, {"v0.1.9", false}} {
		answer := Available{Latest: row.latest, Running: "v0.2.0-rc.3"}
		if got := answer.Newer(); got != row.show {
			t.Fatalf("latest %s: newer = %t, want %t", row.latest, got, row.show)
		}
	}
}

// Source and unstamped launches never reach the release service.
func TestSourceLaunchesNeverReachTheNetwork(t *testing.T) {
	var hits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	defer server.Close()
	for _, running := range []string{"deadbeef", ""} {
		answer, show := CheckLaunch(context.Background(), CheckOptions{
			Running: running, ProfileDir: t.TempDir(), Client: releaseClient(server, running),
		})
		if show || answer != (Available{}) {
			t.Fatalf("%q returned %+v, %t", running, answer, show)
		}
	}
	if hits.Load() != 0 {
		t.Fatalf("server saw %d requests", hits.Load())
	}
}

// V3: Channel builds check their own release list once and show exactly one
// executable-specific notice only when that channel has a newer build.
func TestV3LaunchNoticeForChannelBuilds(t *testing.T) {
	const (
		oldDev   = "dev-20260918-aaaaaaaaaaaa"
		newDev   = "dev-20260921-bbbbbbbbbbbb"
		newStage = "staging-20260921-cccccccccccc"
	)
	list := `[
		{"tag_name":"` + oldDev + `","published_at":"2026-09-18T12:00:00Z"},
		{"tag_name":"` + newDev + `","published_at":"2026-09-21T12:00:00Z"},
		{"tag_name":"` + newStage + `","published_at":"2026-09-21T13:00:00Z"}
	]`
	for _, row := range []struct {
		name, running, executable, wantLatest, wantPath string
		show                                            bool
	}{
		{"older dev", oldDev, "/opt/codeaf/devaf", newDev, "/releases?per_page=100", true},
		{"older dev as codeaf", oldDev, "/opt/codeaf/codeaf", newDev, "/releases?per_page=100", true},
		{"newest dev", newDev, "/opt/codeaf/devaf", newDev, "/releases?per_page=100", false},
		{"staging", "staging-20260918-dddddddddddd", "/opt/codeaf/codeaf", newStage, "/releases?per_page=100", true},
		{"stable", "v0.1.0", "/opt/codeaf/codeaf", "v0.2.0", "/releases/latest", true},
	} {
		t.Run(row.name, func(t *testing.T) {
			var paths []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				paths = append(paths, request.URL.RequestURI())
				if strings.HasSuffix(request.URL.Path, "/releases/latest") {
					fmt.Fprint(w, `{"tag_name":"v0.2.0"}`)
					return
				}
				fmt.Fprint(w, list)
			}))
			defer server.Close()
			answer, show := CheckLaunch(context.Background(), CheckOptions{
				Running: row.running, Executable: row.executable,
				ProfileDir: t.TempDir(), Client: releaseClient(server, row.running),
			})
			if show != row.show || answer.Latest != row.wantLatest {
				t.Fatalf("answer = %+v, show = %t", answer, show)
			}
			if len(paths) != 1 || !strings.HasSuffix(paths[0], row.wantPath) {
				t.Fatalf("requests = %q, want one ending in %q", paths, row.wantPath)
			}
			if row.name == "older dev" {
				want := "codeaf " + newDev + " is out · you have " + oldDev + " · /update installs it and restarts · or: curl -fsSL https://agentfield.ai/get/devaf | bash"
				if got := answer.Notice(); got != want {
					t.Fatalf("notice = %q, want %q", got, want)
				}
			}
			if row.name == "older dev as codeaf" {
				wantEnd := "or: curl -fsSL https://agentfield.ai/get/codeaf/dev | bash"
				if got := answer.Notice(); !strings.HasSuffix(got, wantEnd) {
					t.Fatalf("notice = %q, want suffix %q", got, wantEnd)
				}
			}
		})
	}
}

// V4: Channel ordering uses matching tags, publish moments, tag dates, and the
// API-selected same-day release in that order, including the mirror ahead cases.
func TestV4ChannelOrdering(t *testing.T) {
	oldDay := "dev-20260918-aaaaaaaaaaaa"
	newDay := "dev-20260921-bbbbbbbbbbbb"
	sameDayA := "dev-20260921-aaaaaaaaaaaa"
	sameDayB := "dev-20260921-bbbbbbbbbbbb"
	early := time.Date(2026, 9, 21, 9, 0, 0, 0, time.UTC)
	late := early.Add(time.Hour)
	for _, row := range []struct {
		name, running, selected string
		runningAt, selectedAt   time.Time
		want                    int
	}{
		{"same tag", sameDayA, sameDayA, late, early, 0},
		{"same day selected published later", sameDayA, sameDayB, early, late, -1},
		{"dates disagree with publication", newDay, oldDay, early, late, -1},
		{"running published later", sameDayA, sameDayB, late, early, 1},
		{"selected later date with one moment unknown", oldDay, newDay, early, time.Time{}, -1},
		{"running later date with one moment unknown", newDay, oldDay, time.Time{}, early, 1},
		{"same day unknown moment selects API release", sameDayA, sameDayB, time.Time{}, late, -1},
		{"no moment at all falls to the tag dates", newDay, oldDay, time.Time{}, time.Time{}, 1},
		// TWO RELEASES PUBLISHED IN THE SAME SECOND ARE NOT ORDERED BY THEIR
		// MOMENTS. D4's second rule names a later publication, and neither is
		// later, so the remaining rules answer: the tag date first, then the
		// release the API named.
		{"one moment, twice, falls to the tag dates", newDay, oldDay, early, early, 1},
		{"one moment, twice, on one day gives the API release", sameDayA, sameDayB, early, early, -1},
	} {
		t.Run(row.name, func(t *testing.T) {
			got, ok := CompareChannelBuilds(row.running, row.runningAt, row.selected, row.selectedAt)
			if !ok || got != row.want {
				t.Fatalf("comparison = %d, %t; want %d, true", got, ok, row.want)
			}
			available := Available{
				Latest: row.selected, Running: row.running,
				LatestPublished: row.selectedAt, RunningPublished: row.runningAt,
			}
			if available.Newer() != (row.want < 0) || available.Ahead() != (row.want > 0) {
				t.Fatalf("newer = %t ahead = %t for comparison %d", available.Newer(), available.Ahead(), row.want)
			}
			if available.Ahead() && available.Notice() != "" {
				t.Fatalf("ahead build drew notice %q", available.Notice())
			}
		})
	}
}

// D3 and D6: the channel a build FOLLOWS, which is the one its launch line
// speaks about and the one the road under that line must reinstall. A release
// candidate follows stable; only dev and staging follow themselves.
func TestTheChannelABuildFollowsIsStableUnlessItIsDevOrStaging(t *testing.T) {
	for _, row := range []struct{ running, want string }{
		{"v0.3.0", "stable"},
		{"v0.3.0-rc.1", "stable"},
		{"dev-20260921-aaaaaaaaaaaa", "dev"},
		{"staging-20260921-aaaaaaaaaaaa", "staging"},
		{"deadbeefdead", "stable"},
		{"", "stable"},
	} {
		if got := FollowedChannel(row.running); got != row.want {
			t.Errorf("FollowedChannel(%q) = %q, want %q", row.running, got, row.want)
		}
	}
}

// D6: a release candidate is told about the stable release ahead of it, so the
// road under that notice installs STABLE — an rc road would install something
// the notice never named. The file's own name still chooses the address.
func TestAReleaseCandidateNoticeOffersTheStableRoad(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"tag_name":"v0.3.0"}`)
	}))
	defer server.Close()
	for _, row := range []struct{ name, executable, want string }{
		{"as codeaf", "/opt/codeaf/codeaf", CurlCommand},
		{"as devaf", "/opt/codeaf/devaf", "curl -fsSL https://agentfield.ai/get/devaf | bash"},
	} {
		t.Run(row.name, func(t *testing.T) {
			answer, show := CheckLaunch(context.Background(), CheckOptions{
				Running: "v0.3.0-rc.1", Executable: row.executable,
				ProfileDir: t.TempDir(), Client: releaseClient(server, "v0.3.0-rc.1"),
			})
			if !show {
				t.Fatalf("an rc behind its stable line drew no notice: %+v", answer)
			}
			if got := answer.Notice(); !strings.HasSuffix(got, "or: "+row.want) {
				t.Fatalf("notice = %q, want it to end in %q", got, row.want)
			}
			if strings.Contains(answer.Notice(), "/get/codeaf/rc") {
				t.Fatalf("the rc notice offered an rc road: %q", answer.Notice())
			}
		})
	}
}

// V4: A pair the channel law cannot rank — two different channels, a channel
// tag beside a version number, or a tag that names no release at all — is
// neither newer nor ahead, and draws no launch line.
func TestV4UnrankablePairsAreNeitherNewerNorAhead(t *testing.T) {
	for _, row := range []struct{ running, selected string }{
		{"dev-20260921-aaaaaaaaaaaa", "staging-20260921-bbbbbbbbbbbb"},
		{"dev-20260921-aaaaaaaaaaaa", "v0.3.0"},
		{"dev-20260921-aaaaaaaaaaaa", "not-a-tag"},
		{"staging-20260921-aaaaaaaaaaaa", "dev-20260921-bbbbbbbbbbbb"},
	} {
		if _, ok := CompareChannelBuilds(row.running, time.Time{}, row.selected, time.Time{}); ok {
			t.Errorf("CompareChannelBuilds(%q, %q) claimed an order", row.running, row.selected)
		}
		available := Available{Latest: row.selected, Running: row.running}
		if available.Newer() || available.Ahead() || available.Notice() != "" {
			t.Errorf("%q against %q: newer = %t, ahead = %t, notice = %q",
				row.running, row.selected, available.Newer(), available.Ahead(), available.Notice())
		}
	}
}

// V8: An install that cannot replace the running file offers the road back to
// THAT file. A devaf told to reinstall from /get/codeaf would come back as a
// stable codeaf, which is the surprise this whole change exists to remove.
func TestV8InstallFailureCarriesTheCallersCurlLine(t *testing.T) {
	asset := []byte("new codeaf")
	digest := sha256.Sum256(asset)
	server, _ := servedRelease(t, asset, hex.EncodeToString(digest[:]))
	defer server.Close()
	devafCurl := CurlLine("/home/x/.codeaf/bin/devaf", "dev")
	unreachable := filepath.Join(t.TempDir(), "gone", "devaf")
	_, err := Install(context.Background(), InstallOptions{
		Client:  releaseClient(server, "dev-20260918-aaaaaaaaaaaa"),
		Release: Release{Tag: "dev-20260921-bbbbbbbbbbbb", Repository: primaryRepository},
		Target:  unreachable, Curl: devafCurl,
	})
	if err == nil || !strings.Contains(err.Error(), "cannot replace "+unreachable) ||
		!strings.Contains(err.Error(), devafCurl) || strings.Contains(err.Error(), "get/codeaf") {
		t.Fatalf("error = %v, want the devaf road", err)
	}
	// With no road supplied the stable constant is still the answer.
	if _, bare := Install(context.Background(), InstallOptions{
		Client:  releaseClient(server, "v0.1.1"),
		Release: Release{Tag: "v0.2.0", Repository: primaryRepository},
		Target:  unreachable,
	}); bare == nil || !strings.Contains(bare.Error(), CurlCommand) {
		t.Fatalf("bare error = %v", bare)
	}
}

// V5: Stable and channel builds use separate cache files and retain their
// unchanged 24-hour and one-hour lifetimes when launches alternate.
func TestV5ChannelCacheFilesAndLifetimes(t *testing.T) {
	const runningDev = "dev-20260918-aaaaaaaaaaaa"
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	var hits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		hits.Add(1)
		if strings.HasSuffix(request.URL.Path, "/releases/latest") {
			fmt.Fprint(w, `{"tag_name":"v0.2.0"}`)
			return
		}
		fmt.Fprint(w, `[
			{"tag_name":"`+runningDev+`","published_at":"2026-09-18T12:00:00Z"},
			{"tag_name":"dev-20260921-bbbbbbbbbbbb","published_at":"2026-09-21T12:00:00Z"}
		]`)
	}))
	defer server.Close()
	profile := t.TempDir()
	client := releaseClient(server, runningDev)
	clock := func() time.Time { return now }
	stable := CheckOptions{Running: "v0.1.0", ProfileDir: profile, Client: client, Now: clock}
	dev := CheckOptions{Running: runningDev, ProfileDir: profile, Client: client, Now: clock}
	devOnly := t.TempDir()
	CheckLaunch(context.Background(), CheckOptions{Running: runningDev, ProfileDir: devOnly, Client: client, Now: clock})
	if _, err := os.Stat(filepath.Join(devOnly, "update-check.dev.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(devOnly, "update-check.json")); !os.IsNotExist(err) {
		t.Fatalf("a dev launch wrote the stable cache: %v", err)
	}
	hits.Store(0)
	CheckLaunch(context.Background(), stable)
	CheckLaunch(context.Background(), dev)
	CheckLaunch(context.Background(), stable)
	CheckLaunch(context.Background(), dev)
	if hits.Load() != 2 {
		t.Fatalf("alternating launches made %d requests, want one per channel", hits.Load())
	}
	for _, name := range []string{"update-check.json", "update-check.dev.json"} {
		if _, err := os.Stat(filepath.Join(profile, name)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(profile, "update-check.staging.json")); !os.IsNotExist(err) {
		t.Fatalf("unexpected staging cache: %v", err)
	}

	now = now.Add(59 * time.Minute)
	CheckLaunch(context.Background(), dev)
	if hits.Load() != 2 {
		t.Fatalf("fresh dev cache made %d requests", hits.Load())
	}
	now = now.Add(2 * time.Minute)
	CheckLaunch(context.Background(), dev)
	if hits.Load() != 3 {
		t.Fatalf("stale dev cache made %d requests, want 3 total", hits.Load())
	}
	now = now.Add(22 * time.Hour)
	CheckLaunch(context.Background(), stable)
	if hits.Load() != 3 {
		t.Fatalf("fresh stable cache made %d requests", hits.Load())
	}
	now = now.Add(2 * time.Hour)
	CheckLaunch(context.Background(), stable)
	if hits.Load() != 4 {
		t.Fatalf("stale stable cache made %d requests, want 4 total", hits.Load())
	}
}

// V8: CurlLine preserves the stable road, follows named codeaf channels, gives
// devaf its proxy, and carries arbitrary executable names through the installer.
func TestV8CurlLineTable(t *testing.T) {
	for _, row := range []struct{ executable, channel, want string }{
		{"codeaf", "stable", CurlCommand},
		{"codeaf.exe", "stable", CurlCommand},
		{"codeaf", "dev", "curl -fsSL https://agentfield.ai/get/codeaf/dev | bash"},
		{"codeaf", "staging", "curl -fsSL https://agentfield.ai/get/codeaf/staging | bash"},
		{"codeaf", "rc", "curl -fsSL https://agentfield.ai/get/codeaf/rc | bash"},
		{"devaf", "dev", "curl -fsSL https://agentfield.ai/get/devaf | bash"},
		{"devaf", "stable", "curl -fsSL https://agentfield.ai/get/devaf | bash"},
		{"devaf.exe", "dev", "curl -fsSL https://agentfield.ai/get/devaf | bash"},
		{"stageaf", "staging", "curl -fsSL https://agentfield.ai/get/stageaf | bash"},
		{"stageaf", "stable", "curl -fsSL https://agentfield.ai/get/stageaf | bash"},
		{"stageaf", "rc", "curl -fsSL https://agentfield.ai/get/stageaf | bash"},
		{"stageaf.exe", "dev", "curl -fsSL https://agentfield.ai/get/stageaf | bash"},
		{"mine", "dev", "curl -fsSL https://agentfield.ai/get/codeaf/dev | CODEAF_INSTALL_NAME=mine bash"},
		{"mine", "stable", "curl -fsSL https://agentfield.ai/get/codeaf | CODEAF_INSTALL_NAME=mine bash"},
		{"", "dev", "curl -fsSL https://agentfield.ai/get/codeaf/dev | bash"},
		{"codeaf", "other", CurlCommand},
	} {
		if got := CurlLine(row.executable, row.channel); got != row.want {
			t.Errorf("CurlLine(%q, %q) = %q, want %q", row.executable, row.channel, got, row.want)
		}
	}
}

// TestC4OptOutAndFreshCacheAvoidRequestsWhileStaleFactsRefresh proves C4.
func TestC4OptOutAndFreshCacheAvoidRequestsWhileStaleFactsRefresh(t *testing.T) {
	base := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	var hits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		fmt.Fprint(w, `{"tag_name":"v0.2.0"}`)
	}))
	defer server.Close()
	client := releaseClient(server, "v0.1.1")

	t.Setenv(NoUpdateCheckEnv, "1")
	CheckLaunch(context.Background(), CheckOptions{Running: "v0.1.1", ProfileDir: t.TempDir(), Client: client})
	if hits.Load() != 0 {
		t.Fatal("the opt-out reached the server")
	}
	t.Setenv(NoUpdateCheckEnv, "")

	fresh := t.TempDir()
	path := filepath.Join(fresh, "update-check.json")
	if err := saveCheckCache(path, checkCache{CheckedAt: base.Add(-23 * time.Hour), Latest: "v0.2.0", Running: "v0.1.1"}); err != nil {
		t.Fatal(err)
	}
	answer, show := CheckLaunch(context.Background(), CheckOptions{Running: "v0.1.1", ProfileDir: fresh, Client: client, Now: func() time.Time { return base }})
	if !show || answer.Latest != "v0.2.0" || hits.Load() != 0 {
		t.Fatalf("fresh cache = %+v, %t, hits %d", answer, show, hits.Load())
	}

	for _, cached := range []checkCache{
		{CheckedAt: base.Add(-25 * time.Hour), Latest: "v0.2.0", Running: "v0.1.1"},
		{CheckedAt: base.Add(-time.Hour), Latest: "v0.2.0", Running: "v0.1.0"},
	} {
		dir := t.TempDir()
		if err := saveCheckCache(filepath.Join(dir, "update-check.json"), cached); err != nil {
			t.Fatal(err)
		}
		CheckLaunch(context.Background(), CheckOptions{Running: "v0.1.1", ProfileDir: dir, Client: client, Now: func() time.Time { return base }})
	}
	if hits.Load() != 2 {
		t.Fatalf("stale facts made %d requests, want 2", hits.Load())
	}
}

// TestC5LaunchFailuresAreSilent proves C5.
func TestC5LaunchFailuresAreSilent(t *testing.T) {
	tests := map[string]http.Handler{
		"403":       http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "no", http.StatusForbidden) }),
		"404":       http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { http.NotFound(w, nil) }),
		"500":       http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "no", http.StatusInternalServerError) }),
		"malformed": http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "{") }),
		"timeout":   http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { time.Sleep(50 * time.Millisecond) }),
	}
	for name, handler := range tests {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(handler)
			defer server.Close()
			client := releaseClient(server, "v0.1.1")
			if name == "timeout" {
				client.HTTP.Timeout = time.Millisecond
			}
			answer, show := CheckLaunch(context.Background(), CheckOptions{Running: "v0.1.1", ProfileDir: t.TempDir(), Client: client})
			if show || answer != (Available{}) {
				t.Fatalf("failure drew %+v, %t", answer, show)
			}
		})
	}
	client := &Client{HTTP: &http.Client{Timeout: 10 * time.Millisecond}, APIBase: "http://127.0.0.1:1", Revision: "v0.1.1"}
	if answer, show := CheckLaunch(context.Background(), CheckOptions{Running: "v0.1.1", ProfileDir: t.TempDir(), Client: client}); show || answer != (Available{}) {
		t.Fatalf("unreachable server drew %+v, %t", answer, show)
	}
}

func servedRelease(t *testing.T, asset []byte, checksum string) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	requests := &atomic.Int64{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		switch {
		case strings.HasSuffix(r.URL.Path, "/codeaf-"+runtime.GOOS+"-"+runtime.GOARCH):
			_, _ = w.Write(asset)
		case strings.HasSuffix(r.URL.Path, "/checksums.txt"):
			fmt.Fprintf(w, "%s  codeaf-%s-%s\n", checksum, runtime.GOOS, runtime.GOARCH)
		default:
			http.NotFound(w, r)
		}
	}))
	return server, requests
}

// TestTimeoutContractC1SlowAssetOutlivesTheCheckClock proves C1: the check
// clock cannot cut off an asset that continues to deliver bytes.
func TestTimeoutContractC1SlowAssetOutlivesTheCheckClock(t *testing.T) {
	asset := make([]byte, 1<<20)
	for index := range asset {
		asset[index] = byte(index)
	}
	digest := sha256.Sum256(asset)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch {
		case strings.HasSuffix(request.URL.Path, "/checksums.txt"):
			fmt.Fprintf(w, "%x  codeaf-%s-%s\n", digest, runtime.GOOS, runtime.GOARCH)
		case strings.HasSuffix(request.URL.Path, "/codeaf-"+runtime.GOOS+"-"+runtime.GOARCH):
			flusher := w.(http.Flusher)
			const chunks = 13
			for chunk := 0; chunk < chunks; chunk++ {
				select {
				case <-request.Context().Done():
					return
				case <-time.After(500 * time.Millisecond):
				}
				start := len(asset) * chunk / chunks
				end := len(asset) * (chunk + 1) / chunks
				if _, err := w.Write(asset[start:end]); err != nil {
					return
				}
				flusher.Flush()
			}
		default:
			http.NotFound(w, request)
		}
	}))
	defer server.Close()

	dir := t.TempDir()
	target := filepath.Join(dir, "codeaf")
	if err := os.WriteFile(target, []byte("old codeaf"), 0o700); err != nil {
		t.Fatal(err)
	}
	client := releaseClient(server, "v0.1.0")
	client.HTTP.Timeout = CheckTimeout
	result, err := Install(context.Background(), InstallOptions{
		Client: client, Release: Release{Tag: "v0.2.0", Repository: primaryRepository}, Target: target,
	})
	if err != nil {
		t.Fatal(err)
	}
	installed, err := os.ReadFile(target)
	if err != nil || string(installed) != string(asset) {
		t.Fatalf("installed bytes = %d, error %v; want %d matching bytes", len(installed), err, len(asset))
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 || result.Path != target {
		t.Fatalf("result = %+v mode = %v", result, info.Mode().Perm())
	}
	if matches, _ := filepath.Glob(filepath.Join(dir, ".codeaf.tmp.*")); len(matches) != 0 {
		t.Fatalf("temporary files remain: %v", matches)
	}
}

// TestTimeoutContractC2HangingLaunchCheckEndsInsideItsBudget proves C2 for the
// silent launch door with a server that accepts the request and never answers.
func TestTimeoutContractC2HangingLaunchCheckEndsInsideItsBudget(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		<-request.Context().Done()
	}))
	defer server.Close()
	started := time.Now()
	answer, show := CheckLaunch(context.Background(), CheckOptions{
		Running: "v0.1.0", ProfileDir: t.TempDir(), Client: releaseClient(server, "v0.1.0"),
	})
	if elapsed := time.Since(started); elapsed > 6*time.Second {
		t.Fatalf("hanging launch check took %s, want at most 6s", elapsed)
	}
	if show || answer != (Available{}) {
		t.Fatalf("hanging launch check returned %+v, show %t", answer, show)
	}
}

// TestTimeoutContractC3StallAndCeilingPreserveTheOriginal proves C3 with two
// real response bodies: one sends no bytes and one never stops trickling them.
func TestTimeoutContractC3StallAndCeilingPreserveTheOriginal(t *testing.T) {
	assetName := "codeaf-" + runtime.GOOS + "-" + runtime.GOARCH
	if got, want := downloadStallError(assetName, downloadStallWindow).Error(), "downloading "+assetName+" stalled — no bytes for 30 s"; got != want {
		t.Fatalf("default stall sentence = %q, want %q", got, want)
	}
	if got, want := downloadCeilingError(assetName, downloadCeiling).Error(), "downloading "+assetName+" took longer than 15 minutes"; got != want {
		t.Fatalf("default ceiling sentence = %q, want %q", got, want)
	}

	for _, row := range []struct {
		name        string
		stall       time.Duration
		ceiling     time.Duration
		serve       func(http.ResponseWriter, *http.Request)
		want        string
		upperMargin time.Duration
	}{
		{
			name: "stalled body", stall: 60 * time.Millisecond, ceiling: 2 * time.Second,
			serve: func(w http.ResponseWriter, request *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.(http.Flusher).Flush()
				<-request.Context().Done()
			},
			want: downloadStallError(assetName, 60*time.Millisecond).Error(), upperMargin: time.Second,
		},
		{
			name: "endless trickle", stall: 100 * time.Millisecond, ceiling: 140 * time.Millisecond,
			serve: func(w http.ResponseWriter, request *http.Request) {
				w.WriteHeader(http.StatusOK)
				flusher := w.(http.Flusher)
				flusher.Flush()
				ticker := time.NewTicker(15 * time.Millisecond)
				defer ticker.Stop()
				for {
					select {
					case <-request.Context().Done():
						return
					case <-ticker.C:
						if _, err := w.Write([]byte("x")); err != nil {
							return
						}
						flusher.Flush()
					}
				}
			},
			want: downloadCeilingError(assetName, 140*time.Millisecond).Error(), upperMargin: time.Second,
		},
	} {
		t.Run(row.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				if strings.HasSuffix(request.URL.Path, "/"+assetName) {
					row.serve(w, request)
					return
				}
				http.NotFound(w, request)
			}))
			defer server.Close()
			dir := t.TempDir()
			target := filepath.Join(dir, "codeaf")
			original := []byte("original codeaf")
			if err := os.WriteFile(target, original, 0o700); err != nil {
				t.Fatal(err)
			}
			client := releaseClient(server, "v0.1.0")
			client.StallWindow = row.stall
			client.DownloadCeiling = row.ceiling
			started := time.Now()
			_, err := Install(context.Background(), InstallOptions{
				Client: client, Release: Release{Tag: "v0.2.0", Repository: primaryRepository}, Target: target,
			})
			if err == nil || err.Error() != row.want {
				t.Fatalf("error = %v, want %q", err, row.want)
			}
			if elapsed := time.Since(started); elapsed > row.upperMargin {
				t.Fatalf("failure took %s, want at most %s", elapsed, row.upperMargin)
			}
			installed, readErr := os.ReadFile(target)
			if readErr != nil || string(installed) != string(original) {
				t.Fatalf("target = %q, error %v; want untouched original", installed, readErr)
			}
			info, statErr := os.Stat(target)
			if statErr != nil {
				t.Fatal(statErr)
			}
			if info.Mode().Perm() != 0o700 {
				t.Fatalf("original mode = %v, want 0700", info.Mode().Perm())
			}
			if matches, _ := filepath.Glob(filepath.Join(dir, ".codeaf.tmp.*")); len(matches) != 0 {
				t.Fatalf("temporary files remain: %v", matches)
			}
		})
	}
}

// TestTimeoutContractC4HangingReleaseAPINamesTheAPI proves C4 with an injected
// API window, so the test exercises the real deadline without waiting ten seconds.
func TestTimeoutContractC4HangingReleaseAPINamesTheAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		<-request.Context().Done()
	}))
	defer server.Close()
	client := releaseClient(server, "v0.1.0")
	client.APIWindow = 60 * time.Millisecond
	started := time.Now()
	_, err := client.Select(context.Background(), Choice{Channel: "stable"})
	if err == nil || err.Error() != "release API did not answer within 60 ms" {
		t.Fatalf("error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("hanging API took %s, want at most 1s", elapsed)
	}
	if strings.Contains(err.Error(), "codeaf-") || strings.Contains(err.Error(), "context deadline exceeded") {
		t.Fatalf("API failure named an asset or Go's deadline: %q", err)
	}
}

// TestTimeoutContractD1AndD3DefaultTransportHasNoWholeBodyClock proves the
// default client's transport clocks and the exact delayed-header sentence.
func TestTimeoutContractD1AndD3DefaultTransportHasNoWholeBodyClock(t *testing.T) {
	client := NewClient("v0.1.0", CheckTimeout)
	if client.HTTP.Timeout != 0 {
		t.Fatalf("Client.Timeout = %s, want zero", client.HTTP.Timeout)
	}
	transport, ok := client.HTTP.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport = %T, want *http.Transport", client.HTTP.Transport)
	}
	if transport.TLSHandshakeTimeout != networkSetupTimeout || transport.ResponseHeaderTimeout != apiRequestTimeout {
		t.Fatalf("transport TLS %s header %s", transport.TLSHandshakeTimeout, transport.ResponseHeaderTimeout)
	}

	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		<-request.Context().Done()
	}))
	defer server.Close()
	testTransport := server.Client().Transport.(*http.Transport).Clone()
	testTransport.ResponseHeaderTimeout = 50 * time.Millisecond
	client.HTTP = &http.Client{Transport: testTransport}
	client.DownloadBase = server.URL
	client.DownloadCeiling = time.Second
	target := filepath.Join(t.TempDir(), "codeaf")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := Install(context.Background(), InstallOptions{
		Client: client, Release: Release{Tag: "v0.2.0", Repository: primaryRepository}, Target: target,
	})
	want := assetHeaderTimeoutSentence(runtime.GOOS, runtime.GOARCH)
	if err == nil || err.Error() != want {
		t.Fatalf("error = %v, want %q", err, want)
	}
}

func assetHeaderTimeoutSentence(goos, goarch string) string {
	return fmt.Sprintf("codeaf-%s-%s did not start arriving within 10 s", goos, goarch)
}

// TestC7CheckedInstallReplacesAtomicallyAndMismatchPreservesTheOriginal proves C7.
func TestC7CheckedInstallReplacesAtomicallyAndMismatchPreservesTheOriginal(t *testing.T) {
	asset := []byte("new codeaf")
	digest := sha256.Sum256(asset)
	for _, row := range []struct {
		name     string
		checksum string
		ok       bool
	}{{"matching", hex.EncodeToString(digest[:]), true}, {"wrong", strings.Repeat("0", 64), false}} {
		t.Run(row.name, func(t *testing.T) {
			server, _ := servedRelease(t, asset, row.checksum)
			defer server.Close()
			dir := t.TempDir()
			target := filepath.Join(dir, "codeaf")
			if err := os.WriteFile(target, []byte("old codeaf"), 0o700); err != nil {
				t.Fatal(err)
			}
			result, err := Install(context.Background(), InstallOptions{
				Client: releaseClient(server, "v0.1.1"), Release: Release{Tag: "v0.2.0", Repository: primaryRepository}, Target: target,
			})
			if (err == nil) != row.ok {
				t.Fatalf("error = %v, want success %t", err, row.ok)
			}
			got, readErr := os.ReadFile(target)
			if readErr != nil {
				t.Fatal(readErr)
			}
			want := []byte("old codeaf")
			if row.ok {
				want = asset
				info, _ := os.Stat(target)
				if info.Mode().Perm() != 0o755 || result.Path != target || result.Release.Tag != "v0.2.0" {
					t.Fatalf("result = %+v mode %o", result, info.Mode().Perm())
				}
			} else if !strings.Contains(err.Error(), "checksum") {
				t.Fatalf("mismatch error = %v", err)
			}
			if string(got) != string(want) {
				t.Fatalf("target = %q, want %q", got, want)
			}
			matches, _ := filepath.Glob(filepath.Join(dir, ".codeaf.tmp.*"))
			if len(matches) != 0 {
				t.Fatalf("temporary files remain: %v", matches)
			}
		})
	}
}

// TestC10TokenReachesOnlyTheAPIHost proves C10.
func TestC10TokenReachesOnlyTheAPIHost(t *testing.T) {
	var apiAuthorization, apiAccept, apiAgent string
	var downloadAuthorization, downloadAccept, downloadAgent string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiAuthorization = r.Header.Get("Authorization")
		apiAccept = r.Header.Get("Accept")
		apiAgent = r.Header.Get("User-Agent")
		fmt.Fprint(w, `{"tag_name":"v0.2.0"}`)
	}))
	defer api.Close()
	asset := []byte("release")
	digest := sha256.Sum256(asset)
	download := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		downloadAuthorization = r.Header.Get("Authorization")
		downloadAccept = r.Header.Get("Accept")
		downloadAgent = r.Header.Get("User-Agent")
		if strings.HasSuffix(r.URL.Path, "checksums.txt") {
			fmt.Fprintf(w, "%x  codeaf-%s-%s\n", digest, runtime.GOOS, runtime.GOARCH)
			return
		}
		_, _ = w.Write(asset)
	}))
	defer download.Close()
	client := &Client{HTTP: api.Client(), APIBase: api.URL, DownloadBase: download.URL, Token: "secret", Revision: "v0.1.1"}
	release, err := client.Select(context.Background(), Choice{Channel: "stable"})
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "codeaf")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(context.Background(), InstallOptions{Client: client, Release: release, Target: target}); err != nil {
		t.Fatal(err)
	}
	if apiAuthorization != "Bearer secret" || downloadAuthorization != "" {
		t.Fatalf("authorization: api %q download %q", apiAuthorization, downloadAuthorization)
	}
	if apiAccept != "application/vnd.github+json" || downloadAccept != "application/octet-stream" {
		t.Fatalf("accept: api %q download %q", apiAccept, downloadAccept)
	}
	if apiAgent != "codeaf/v0.1.1" || downloadAgent != "codeaf/v0.1.1" {
		t.Fatalf("user agent: api %q download %q", apiAgent, downloadAgent)
	}
}

// TestC10RedirectsNeverCarryTheAPITokenToAnotherHost proves the redirect case
// with two real servers that share a hostname and differ only by port.
func TestC10RedirectsNeverCarryTheAPITokenToAnotherHost(t *testing.T) {
	var redirectedAuthorization string
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		redirectedAuthorization = request.Header.Get("Authorization")
		fmt.Fprint(w, `{"tag_name":"v0.2.0"}`)
	}))
	defer destination.Close()
	var apiAuthorization string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		apiAuthorization = request.Header.Get("Authorization")
		http.Redirect(w, request, destination.URL+request.URL.Path, http.StatusFound)
	}))
	defer api.Close()
	client := &Client{HTTP: api.Client(), APIBase: api.URL, Token: "secret", Revision: "v0.1.1"}
	if _, err := client.Select(context.Background(), Choice{Channel: "stable"}); err != nil {
		t.Fatal(err)
	}
	if apiAuthorization != "Bearer secret" || redirectedAuthorization != "" {
		t.Fatalf("authorization: original host %q redirected host %q", apiAuthorization, redirectedAuthorization)
	}
}

// TestC11RepositoryAndAssetFallbacksKeepOldReleasesInstallable proves C11.
func TestC11RepositoryAndAssetFallbacksKeepOldReleasesInstallable(t *testing.T) {
	asset := []byte("old named release")
	digest := sha256.Sum256(asset)
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		switch {
		case strings.Contains(r.URL.Path, "/repos/"+primaryRepository+"/"):
			http.NotFound(w, r)
		case strings.Contains(r.URL.Path, "/repos/"+legacyRepository+"/"):
			fmt.Fprint(w, `{"tag_name":"v0.2.0"}`)
		case strings.HasSuffix(r.URL.Path, "/aforge-"+runtime.GOOS+"-"+runtime.GOARCH): // legacy-name
			_, _ = w.Write(asset)
		case strings.HasSuffix(r.URL.Path, "/checksums.txt"):
			fmt.Fprintf(w, "%x *aforge-%s-%s\n", digest, runtime.GOOS, runtime.GOARCH) // legacy-name
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := releaseClient(server, "v0.1.1")
	release, err := client.Select(context.Background(), Choice{Channel: "stable"})
	if err != nil || release.Repository != legacyRepository {
		t.Fatalf("release = %+v, error %v", release, err)
	}
	target := filepath.Join(t.TempDir(), "codeaf")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(context.Background(), InstallOptions{Client: client, Release: release, Target: target}); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(paths, "\n")
	for _, want := range []string{"/repos/" + primaryRepository, "/repos/" + legacyRepository, "/codeaf-", "/aforge-"} { // legacy-name
		if !strings.Contains(joined, want) {
			t.Fatalf("requests do not contain %q:\n%s", want, joined)
		}
	}
}

// TestAssetFailureNamesTheCurrentAssetAndStatusWithoutExposingFallbacks proves D9.
func TestAssetFailureNamesTheCurrentAssetAndStatusWithoutExposingFallbacks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()
	target := filepath.Join(t.TempDir(), "codeaf")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := Install(context.Background(), InstallOptions{
		Client:  releaseClient(server, "v0.1.0"),
		Release: Release{Tag: "v0.2.0", Repository: primaryRepository},
		Target:  target,
	})
	if err == nil {
		t.Fatal("missing asset returned no error")
	}
	want := fmt.Sprintf("codeaf-%s-%s answered HTTP 404", runtime.GOOS, runtime.GOARCH)
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err, want)
	}
	if strings.Contains(err.Error(), server.URL) || strings.Contains(err.Error(), "aforge") { // legacy-name
		t.Fatalf("person-facing error exposes a fallback: %q", err)
	}
}

// TestC14RestartArgumentsReopenTheSameConversation proves C14.
func TestC14RestartArgumentsReopenTheSameConversation(t *testing.T) {
	file := "/tmp/conversation.jsonl"
	for _, row := range []struct {
		name string
		args []string
		want []string
	}{
		{"bare", nil, []string{"chat", "--session", file}},
		{"chat", []string{"chat", "--model", "x"}, []string{"chat", "--model", "x", "--session", file}},
		{"resume", []string{"resume", "--session", file}, []string{"resume", "--session", file}},
	} {
		t.Run(row.name, func(t *testing.T) {
			got := RestartArgs(row.args, file)
			if strings.Join(got, "\x00") != strings.Join(row.want, "\x00") {
				t.Fatalf("args = %q, want %q", got, row.want)
			}
		})
	}
}
