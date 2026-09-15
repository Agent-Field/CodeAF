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

// TestC3SourceAndChannelLaunchesNeverReachTheNetwork proves C3.
func TestC3SourceAndChannelLaunchesNeverReachTheNetwork(t *testing.T) {
	var hits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	defer server.Close()
	for _, running := range []string{"dev-20260915-abcdefabcdef", "staging-20260915-abcdefabcdef", "deadbeef", ""} {
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
