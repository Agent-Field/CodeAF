// Command fetch puts the pinned furrow release where `go:embed` will find it.
//
// It is the build's half of the no-variance ruling: aforge ships furrow inside
// it, so a build has to have furrow before it can produce an aforge, and this
// is the step that either gets it or stops. It never half-succeeds. Either the
// staged archive is a furrow whose sha256 is the one internal/furrowbin/pin.json
// names, or the command exits non-zero with the sentence that says what to run.
//
//	go run ./internal/furrowbin/cmd/fetch
//	go run ./internal/furrowbin/cmd/fetch -from ~/.agentfield/bin/furrow
//
// A build machine with no network is the case worth spelling out: -from takes
// the artifact off local disk and checks it against the same pin, so an air
// gapped or offline build is a supported road and not a reason to loosen the
// check.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/furrowbin"
)

// downloadTimeout bounds the whole transfer. The artifacts are six or seven
// megabytes, so this is not a speed limit — it is the difference between a
// build that fails with the fetch command in the message and a build that hangs
// on a wedged proxy until somebody notices.
const downloadTimeout = 5 * time.Minute

func main() {
	var (
		cache = flag.String("cache", filepath.Join("third_party", "furrow"), "where verified artifacts are kept between builds")
		stage = flag.String("stage", filepath.Join("internal", "furrowbin", "cache"), "the embedded folder to stage this platform's furrow into")
		from  = flag.String("from", "", "read the artifact from this local file instead of downloading it")
		goos  = flag.String("goos", envOr("GOOS", runtime.GOOS), "the platform being built for")
		arch  = flag.String("goarch", envOr("GOARCH", runtime.GOARCH), "the architecture being built for")
	)
	flag.Parse()

	platform := furrowbin.Platform(*goos, *arch)
	if err := fetch(furrowbin.ReadPin(), platform, *cache, *stage, *from); err != nil {
		fmt.Fprint(os.Stderr, refusal(furrowbin.ReadPin(), platform, err))
		os.Exit(1)
	}
}

// envOr reads a build variable, falling back to the machine's own.
func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

// fetch is the whole job: get this platform's artifact from somewhere, prove it
// is the pinned one, and leave it gzipped in the embedded folder.
func fetch(pin furrowbin.Pin, platform, cache, stage, from string) error {
	artifact, err := pin.ArtifactFor(platform)
	if err != nil {
		return err
	}

	// The cache is keyed by tag as well as by asset name, so upgrading the pin
	// fetches afresh rather than trusting a file that happens to have the right
	// name. The sha check below would catch it anyway; this makes the two
	// versions able to sit side by side while a bisect moves between them.
	cached := filepath.Join(cache, pin.Tag, artifact.Asset)

	binary, err := os.ReadFile(cached)
	if err == nil && digest(binary) == artifact.SHA256 {
		return stageInto(stage, platform, binary)
	}

	switch {
	case from != "":
		binary, err = os.ReadFile(from)
		if err != nil {
			return fmt.Errorf("read %s: %w", from, err)
		}
	default:
		binary, err = download(pin.DownloadURL(artifact))
		if err != nil {
			return err
		}
	}

	// THE HASH IS CHECKED BEFORE THE BYTES ARE KEPT, ANYWHERE. A wrong artifact
	// that reached the cache would be trusted by every later build on this
	// machine, so the one moment to refuse it is before it is written down.
	if got := digest(binary); got != artifact.SHA256 {
		return fmt.Errorf("%s is not the pinned artifact: it hashes to %s, the pin says %s", artifact.Asset, got, artifact.SHA256)
	}

	if err := writeFile(cached, binary, 0o644); err != nil {
		return err
	}
	return stageInto(stage, platform, binary)
}

// stageInto gzips the artifact into the embedded folder under its platform's
// name, and removes any other platform's leftovers first.
//
// ONE PLATFORM AT A TIME IS THE POINT. `//go:embed cache` takes every file in
// the folder, so a developer who built for two platforms this week would
// otherwise ship both — six megabytes of a furrow the binary can never run.
func stageInto(stage, platform string, binary []byte) error {
	if err := os.MkdirAll(stage, 0o755); err != nil {
		return fmt.Errorf("make room in %s: %w", stage, err)
	}
	leftovers, err := filepath.Glob(filepath.Join(stage, "furrow-*.gz"))
	if err != nil {
		return err
	}
	keep := filepath.Join(stage, furrowbin.StagedFileName(platform))
	for _, leftover := range leftovers {
		if leftover == keep {
			continue
		}
		if err := os.Remove(leftover); err != nil {
			return fmt.Errorf("clear %s: %w", leftover, err)
		}
	}

	archive, err := furrowbin.Compress(binary)
	if err != nil {
		return fmt.Errorf("compress furrow: %w", err)
	}

	// An unchanged rewrite leaves the tree clean, the same property
	// internal/packed's packer is built around: `make build` regenerates
	// unconditionally rather than trusting a freshness check, and gzip at a
	// fixed level over fixed bytes is a pure function. It is still written
	// through a temporary file and renamed, because a build interrupted
	// halfway must not leave a truncated archive that compiles.
	if existing, err := os.ReadFile(keep); err == nil && len(existing) == len(archive) && string(existing) == string(archive) {
		return nil
	}
	return writeFile(keep, archive, 0o644)
}

// writeFile writes bytes to a path through a temporary file in the same
// directory, so a reader either sees the old file or the new one.
func writeFile(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("make room in %s: %w", dir, err)
	}
	temporary, err := os.CreateTemp(dir, filepath.Base(path)+".*.partial")
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	defer os.Remove(temporary.Name())
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return os.Rename(temporary.Name(), path)
}

// download pulls one release asset over plain HTTPS. GitHub's release download
// path is public, so this needs no token and no gh CLI — which is the whole
// reason it is a URL and not a shell out: `make build` runs on machines that
// have neither.
func download(url string) ([]byte, error) {
	client := &http.Client{Timeout: downloadTimeout}
	response, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch %s: the server answered %s", url, response.Status)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}
	return body, nil
}

// digest is the hex sha256 the pin is written in.
func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// refusal is the whole of what a failed fetch says, and it is written to be
// read by somebody whose build just stopped for a reason they did not cause.
//
// IT NAMES THE COMMAND. A build that fails with only a cause leaves a person
// guessing at a step they have never run by hand; this one hands them the two
// roads out — the network one and the local-file one — and says where the pin
// lives, because the third possibility is that the pin itself is what needs
// changing.
func refusal(pin furrowbin.Pin, platform string, cause error) string {
	return fmt.Sprintf("\nfurrow %s for %s could not be prepared, so this build stopped.\n\n    %s\n\n",
		pin.Version, platform, cause) +
		"aforge ships furrow inside it, so a binary without it is not a binary\n" +
		"this repository will produce. Get the artifact once and the build carries\n" +
		"on from the cache from then on:\n\n" +
		"    make furrow\n\n" +
		"or, on a machine with no network but with the artifact already on disk:\n\n" +
		"    make furrow FURROW_ARTIFACT=/path/to/" + pinAsset(pin, platform) + "\n\n" +
		"The release, and the sha256 every platform's artifact must have, are in\n" +
		"internal/furrowbin/pin.json.\n\n"
}

// pinAsset names the file a person would go and find, or the platform itself
// when the pin has no entry for it — which is the one failure where naming an
// asset would be inventing one.
func pinAsset(pin furrowbin.Pin, platform string) string {
	artifact, err := pin.ArtifactFor(platform)
	if err != nil {
		return "furrow-" + platform
	}
	return artifact.Asset
}
