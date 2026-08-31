package main

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/furrowbin"
)

// fakeFurrow is the artifact these tests fetch. Nothing here depends on it
// being a real executable — what is under test is the pin check, the cache and
// the staging, all of which are about bytes and names.
const fakeFurrow = "#!/bin/sh\necho furrow 9.9.9\n"

// fakePin is a pin over the fake artifact, with the real pin's shape and the
// fake's real digest. Writing the digest by hand would be writing a test that
// asserts what the test itself computed, so it is taken from the same function
// the fetcher checks against.
func fakePin(platform string) furrowbin.Pin {
	return furrowbin.Pin{
		Repository: "Agent-Field/furrow",
		Tag:        "v9.9.9",
		Version:    "9.9.9",
		Artifacts: map[string]furrowbin.Artifact{
			platform: {Asset: "furrow-" + platform, SHA256: digest([]byte(fakeFurrow))},
		},
	}
}

// artifactOn writes the fake artifact somewhere and returns the path.
func artifactOn(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "furrow")
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// stagedBody reads back what the fetcher staged, which is the whole point of
// the step: the bytes go:embed will carry have to inflate to the artifact.
func stagedBody(t *testing.T, stage, platform string) string {
	t.Helper()
	archive, err := os.ReadFile(filepath.Join(stage, furrowbin.StagedFileName(platform)))
	if err != nil {
		t.Fatalf("nothing was staged: %v", err)
	}
	stream, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		t.Fatalf("what was staged is not a gzip stream: %v", err)
	}
	body, err := io.ReadAll(stream)
	if err != nil {
		t.Fatalf("read the staged archive: %v", err)
	}
	return string(body)
}

func TestALocalArtifactIsCheckedCachedAndStaged(t *testing.T) {
	platform := furrowbin.Platform("linux", "arm64")
	cache, stage := t.TempDir(), t.TempDir()

	if err := fetch(fakePin(platform), platform, cache, stage, artifactOn(t, fakeFurrow)); err != nil {
		t.Fatalf("fetch from a local file: %v", err)
	}
	if got := stagedBody(t, stage, platform); got != fakeFurrow {
		t.Fatalf("staged %q; want the artifact's own bytes", got)
	}

	// The cache is keyed by tag, so a pin that moves fetches afresh rather than
	// trusting a file whose only credential is its name.
	cached := filepath.Join(cache, "v9.9.9", "furrow-"+platform)
	body, err := os.ReadFile(cached)
	if err != nil {
		t.Fatalf("nothing was cached at %s: %v", cached, err)
	}
	if string(body) != fakeFurrow {
		t.Fatalf("cached %q; want the artifact", body)
	}
}

func TestAnArtifactThatIsNotThePinnedOneIsRefusedAndNeverCached(t *testing.T) {
	platform := furrowbin.Platform("linux", "arm64")
	cache, stage := t.TempDir(), t.TempDir()

	err := fetch(fakePin(platform), platform, cache, stage, artifactOn(t, "some other program entirely"))
	if err == nil {
		t.Fatal("an artifact whose sha256 is not the pinned one was accepted; the pin is the only thing making this fetch trustworthy")
	}
	if !strings.Contains(err.Error(), "is not the pinned artifact") {
		t.Fatalf("the refusal says %q; it must name the mismatch", err)
	}

	// NOTHING WRONG REACHES THE CACHE. A bad artifact written down once would
	// be trusted by every later build on this machine, which turns one bad
	// download into a permanently wrong binary.
	if entries, _ := os.ReadDir(filepath.Join(cache, "v9.9.9")); len(entries) != 0 {
		t.Fatalf("a refused artifact left %d files in the cache", len(entries))
	}
	if entries, _ := filepath.Glob(filepath.Join(stage, "furrow-*.gz")); len(entries) != 0 {
		t.Fatalf("a refused artifact was staged anyway: %v", entries)
	}
}

func TestASoundCacheIsUsedWithoutGoingAnywhere(t *testing.T) {
	platform := furrowbin.Platform("linux", "arm64")
	cache, stage := t.TempDir(), t.TempDir()
	cached := filepath.Join(cache, "v9.9.9", "furrow-"+platform)
	if err := os.MkdirAll(filepath.Dir(cached), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cached, []byte(fakeFurrow), 0o644); err != nil {
		t.Fatal(err)
	}

	// The local source names a file that does not exist, so a fetch that
	// reached for it at all would fail. Passing means the cache answered first,
	// which is what makes the second build of the day free and what makes an
	// offline rebuild possible at all.
	if err := fetch(fakePin(platform), platform, cache, stage, filepath.Join(t.TempDir(), "nowhere")); err != nil {
		t.Fatalf("fetch with a sound cache: %v", err)
	}
	if got := stagedBody(t, stage, platform); got != fakeFurrow {
		t.Fatalf("staged %q; want the cached artifact", got)
	}
}

func TestACacheThatDoesNotMatchThePinIsIgnoredRatherThanTrusted(t *testing.T) {
	platform := furrowbin.Platform("linux", "arm64")
	cache, stage := t.TempDir(), t.TempDir()
	cached := filepath.Join(cache, "v9.9.9", "furrow-"+platform)
	if err := os.MkdirAll(filepath.Dir(cached), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cached, []byte("a truncated download from last week"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := fetch(fakePin(platform), platform, cache, stage, artifactOn(t, fakeFurrow)); err != nil {
		t.Fatalf("fetch over a bad cache entry: %v", err)
	}
	if got := stagedBody(t, stage, platform); got != fakeFurrow {
		t.Fatalf("staged %q; a cache entry is trusted only when it hashes to the pin", got)
	}
	body, err := os.ReadFile(cached)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != fakeFurrow {
		t.Fatal("the bad cache entry was left in place; the next build would read it again")
	}
}

func TestOnlyOnePlatformIsEverStaged(t *testing.T) {
	// `//go:embed cache` takes every file in the folder, so a developer who
	// cross-compiled yesterday would otherwise ship two furrows and run one.
	platform := furrowbin.Platform("linux", "arm64")
	cache, stage := t.TempDir(), t.TempDir()
	leftover := filepath.Join(stage, furrowbin.StagedFileName("darwin-arm64"))
	if err := os.WriteFile(leftover, []byte("yesterday's cross-compile"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := fetch(fakePin(platform), platform, cache, stage, artifactOn(t, fakeFurrow)); err != nil {
		t.Fatalf("fetch beside a leftover: %v", err)
	}
	staged, err := filepath.Glob(filepath.Join(stage, "furrow-*.gz"))
	if err != nil {
		t.Fatal(err)
	}
	if len(staged) != 1 || filepath.Base(staged[0]) != furrowbin.StagedFileName(platform) {
		t.Fatalf("the staged folder holds %v; want only this platform's furrow", staged)
	}
}

func TestStagingTwiceRewritesNothingAndLeavesTheTreeClean(t *testing.T) {
	// `make build` runs the fetch unconditionally, the same way it regenerates
	// the packed corpora unconditionally, and that is only affordable because
	// an unchanged run touches nothing a person would have to explain in
	// `git status`.
	platform := furrowbin.Platform("linux", "arm64")
	cache, stage := t.TempDir(), t.TempDir()
	artifact := artifactOn(t, fakeFurrow)
	if err := fetch(fakePin(platform), platform, cache, stage, artifact); err != nil {
		t.Fatal(err)
	}
	staged := filepath.Join(stage, furrowbin.StagedFileName(platform))
	before, err := os.Stat(staged)
	if err != nil {
		t.Fatal(err)
	}

	if err := fetch(fakePin(platform), platform, cache, stage, artifact); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(staged)
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Fatal("an unchanged fetch rewrote the staged archive; every build would then dirty the tree")
	}
}

func TestAPlatformThePinDoesNotCoverStopsTheBuildByName(t *testing.T) {
	cache, stage := t.TempDir(), t.TempDir()
	err := fetch(fakePin("linux-arm64"), "plan9-riscv64", cache, stage, "")
	if err == nil {
		t.Fatal("a platform with no pinned artifact built happily; that is an aforge without furrow")
	}

	// THE REFUSAL IS THE PRODUCT HERE. A build that stops has to hand back the
	// command that gets it going again, or the person is left guessing at a
	// step they have never run by hand.
	said := refusal(fakePin("linux-arm64"), "plan9-riscv64", err)
	for _, phrase := range []string{"make furrow", "FURROW_ARTIFACT=", "internal/furrowbin/pin.json", "plan9-riscv64"} {
		if !strings.Contains(said, phrase) {
			t.Fatalf("the refusal never says %q:\n%s", phrase, said)
		}
	}
}

func TestADownloadThatDidNotArriveIsAnErrorAndNotAnEmptyFile(t *testing.T) {
	missing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Not Found", http.StatusNotFound)
	}))
	defer missing.Close()
	if _, err := download(missing.URL + "/furrow-linux-arm64"); err == nil {
		t.Fatal("a 404 downloaded happily; the body of an error page would then be hashed and refused for the wrong reason")
	}

	serving := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, fakeFurrow)
	}))
	defer serving.Close()
	body, err := download(serving.URL + "/furrow-linux-arm64")
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	if string(body) != fakeFurrow {
		t.Fatalf("downloaded %q; want the artifact", body)
	}
}
