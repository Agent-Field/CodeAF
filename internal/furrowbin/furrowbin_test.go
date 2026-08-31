package furrowbin

import (
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// archiveOf is a stand-in for the real six megabytes: the extraction laws are
// about names, modes and renames, and none of them care what the bytes are.
func archiveOf(t *testing.T, body string) []byte {
	t.Helper()
	archive, err := Compress([]byte(body))
	if err != nil {
		t.Fatalf("compress the fake furrow: %v", err)
	}
	return archive
}

func TestAFreshStateRootGetsTheBinaryStampedWithItsVersion(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "bin")

	path, err := extractInto(dir, "1.2.3", archiveOf(t, "I am furrow"))
	if err != nil {
		t.Fatalf("extract into a fresh root: %v", err)
	}
	if want := filepath.Join(dir, "furrow-1.2.3"); path != want {
		t.Fatalf("extracted to %s; want %s — the name carries the version or an upgrade overwrites a running binary", path, want)
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read what was extracted: %v", err)
	}
	if string(body) != "I am furrow" {
		t.Fatalf("extracted %q; want the archive's own bytes back", body)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat what was extracted: %v", err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("extracted furrow has mode %v; a binary aforge cannot execute is a binary it did not extract", info.Mode())
	}

	// Nothing half-written is left behind. A .partial that survived would be
	// swept up by the next glob somebody writes and, worse, would look like a
	// furrow to anything that only checked the prefix.
	leftovers, err := filepath.Glob(filepath.Join(dir, "*.partial"))
	if err != nil {
		t.Fatal(err)
	}
	if len(leftovers) != 0 {
		t.Fatalf("extraction left %v behind; the temporary file is renamed or removed, never kept", leftovers)
	}
}

func TestTheVERSIONALREADYTHEREIsLeftExactlyAsItIs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "furrow-1.2.3")
	if err := os.WriteFile(path, []byte("the furrow that is already here"), 0o755); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	got, err := extractInto(dir, "1.2.3", archiveOf(t, "different bytes entirely"))
	if err != nil {
		t.Fatalf("extract with the version already there: %v", err)
	}
	if got != path {
		t.Fatalf("returned %s; want the file that was already there at %s", got, path)
	}

	// THE STEADY STATE IS ONE STAT. Rewriting here would be a six-megabyte
	// write on every boot, and — the sharper half — a write over a path a
	// furrow started a second ago may be running out of.
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "the furrow that is already here" {
		t.Fatalf("the existing binary was rewritten; it now holds %q", body)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Fatal("the existing binary was touched; a version already on disk is not extracted again")
	}
}

func TestAnOlderVersionIsLeftBesideTheNewOneAndNeverWrittenOver(t *testing.T) {
	dir := t.TempDir()
	stale := filepath.Join(dir, "furrow-0.0.1")
	if err := os.WriteFile(stale, []byte("yesterday's furrow, possibly running right now"), 0o755); err != nil {
		t.Fatal(err)
	}

	fresh, err := extractInto(dir, "1.2.3", archiveOf(t, "today's furrow"))
	if err != nil {
		t.Fatalf("extract beside a stale version: %v", err)
	}
	if fresh == stale {
		t.Fatal("the new version took the old one's path; that is the macOS Killed: 9 bug, written on purpose")
	}

	body, err := os.ReadFile(stale)
	if err != nil {
		t.Fatalf("the stale binary is gone: %v", err)
	}
	if string(body) != "yesterday's furrow, possibly running right now" {
		t.Fatalf("the stale binary was rewritten; it now holds %q", body)
	}
}

func TestAStateRootThatCannotBeWrittenToIsAnErrorAndNotAHalfBinary(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes into a read-only directory, so there is nothing to refuse")
	}
	root := t.TempDir()
	if err := os.Chmod(root, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(root, 0o700) })

	path, err := extractInto(filepath.Join(root, "bin"), "1.2.3", archiveOf(t, "I am furrow"))
	if err == nil {
		t.Fatalf("extracting into a read-only root returned %s; want an error the seam can treat as absence", path)
	}
	if path != "" {
		t.Fatalf("a failed extraction returned the path %s; a caller must never be handed a binary that is not there", path)
	}
}

func TestACorruptArchiveIsAnErrorRatherThanAFileNobodyCanRun(t *testing.T) {
	dir := t.TempDir()
	if _, err := extractInto(dir, "1.2.3", []byte("this is not a gzip stream")); err == nil {
		t.Fatal("a corrupt archive extracted happily; it must fail before anything reaches disk")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("a corrupt archive left %d files behind; want nothing written at all", len(entries))
	}
}

func TestEnsureAgreesWithWhatThisBuildActuallyCarries(t *testing.T) {
	// This test is honest on both kinds of build. A plain `go build ./...` in a
	// fresh clone carries nothing and must say so through the sentinel the seam
	// branches on; a build after `make furrow` carries the real furrow and must
	// put it under the state root and nowhere else.
	t.Setenv("AFORGE_HOME", t.TempDir())
	ensureOnce = sync.Once{}

	path, err := Ensure()
	if !Embedded() {
		if !errors.Is(err, ErrNotEmbedded) {
			t.Fatalf("a build carrying no furrow answered (%q, %v); want ErrNotEmbedded", path, err)
		}
		return
	}
	if err != nil {
		t.Fatalf("a build carrying furrow could not extract it: %v", err)
	}
	if base := filepath.Base(path); base != "furrow-"+Version() {
		t.Fatalf("extracted as %s; want furrow-%s, the version the pin names", base, Version())
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("the extracted furrow is not there: %v", err)
	}
	if info.Size() == 0 || info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("the extracted furrow is %d bytes with mode %v; want an executable file", info.Size(), info.Mode())
	}
}

func TestTheStagedNameIsThisMachinesAndNoOthers(t *testing.T) {
	// The whole reason the staged file carries a platform is that a cache left
	// over from a cross-compile must read as "nothing embedded" rather than as
	// a binary this machine will try to run.
	want := "cache/furrow-" + runtime.GOOS + "-" + runtime.GOARCH + ".gz"
	if got := stagedName(); got != want {
		t.Fatalf("this build looks for %s; want %s", got, want)
	}
	if got := StagedFileName(Platform("linux", "arm64")); got != "furrow-linux-arm64.gz" {
		t.Fatalf("the fetcher would stage %s, which is not what stagedName reads", got)
	}
}

func TestThePinIsWholeAndIsTheOnlyPlaceTheVersionIsWritten(t *testing.T) {
	pin := ReadPin()
	if pin.Repository == "" || pin.Tag == "" || pin.Version == "" {
		t.Fatalf("pin.json is missing something: %+v", pin)
	}
	if !strings.Contains(pin.Tag, pin.Version) {
		t.Fatalf("the tag %q and the version %q have drifted apart", pin.Tag, pin.Version)
	}
	if len(pin.Artifacts) == 0 {
		t.Fatal("pin.json names no artifacts, so no platform can be built")
	}

	// EVERY BUILD PLATFORM THIS REPOSITORY SHIPS FOR MUST BE IN THE PIN, or
	// `make build` on it stops with a refusal nobody can act on. These four are
	// what the release publishes and what CI and the two developer machines
	// need.
	for _, platform := range []string{"darwin-arm64", "darwin-amd64", "linux-amd64", "linux-arm64"} {
		artifact, err := ReadPin().ArtifactFor(platform)
		if err != nil {
			t.Fatalf("%s: %v", platform, err)
		}
		if artifact.Asset == "" {
			t.Fatalf("%s names no asset", platform)
		}
		if len(artifact.SHA256) != 64 {
			t.Fatalf("%s has a %d-character sha256; want 64 hex characters", platform, len(artifact.SHA256))
		}
		if _, err := hex.DecodeString(artifact.SHA256); err != nil {
			t.Fatalf("%s has a sha256 that is not hex: %v", platform, err)
		}
		url := pin.DownloadURL(artifact)
		if !regexp.MustCompile(`^https://github\.com/[^/]+/[^/]+/releases/download/[^/]+/[^/]+$`).MatchString(url) {
			t.Fatalf("%s downloads from %s, which is not a release asset URL", platform, url)
		}
	}

	if _, err := pin.ArtifactFor("plan9-riscv64"); err == nil {
		t.Fatal("a platform with no artifact answered happily; the build must refuse it by name")
	}
}
