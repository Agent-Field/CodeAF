// Package furrowbin carries furrow inside aforge and puts it on disk the first
// time anything wants it.
//
// THE RULING THIS PACKAGE EXISTS FOR IS "NO VARIANCE": every aforge is an
// aforge with furrow. Before this, furrow was a program the person went and
// installed, so the four workspace verbs — byte-exact forks that carry the
// dirty tree, the sealed timeline, the restore that puts a .env back — were a
// capability some machines had. Half a product is worse than either half: the
// model cannot plan around a verb that exists on the author's laptop and not on
// the reader's, and the pages that described it had to hedge every sentence. So
// the binary rides along, and the only thing that still varies is whether a
// particular folder has been attached with `furrow watch`.
//
// The bytes get here in three moves, and each one is somewhere else:
//
//  1. pin.json, committed, names the release and the sha256 of every
//     platform's artifact. It is the auditable half and the one source of the
//     version.
//  2. `make build` runs cmd/fetch, which downloads that platform's artifact,
//     checks it against the pin, and stages it gzipped into cache/. The
//     artifact is gitignored — six megabytes of binary in git is six megabytes
//     in every clone forever — and a fetch that cannot happen fails the build
//     out loud, naming the command to run, rather than quietly producing an
//     aforge without furrow.
//  3. This package embeds cache/ and, on first need, writes the binary out
//     under the state root.
//
// [Ensure] is the whole runtime surface, and internal/furrow is its only
// caller. NOTHING ELSE IN THE TREE KNOWS THE BINARY WAS EMBEDDED: the seam
// stays where it was, a caller still asks internal/furrow whether furrow can
// act on a folder, and the answer's new reason for being yes is not the
// caller's business.
package furrowbin

import (
	"bytes"
	"compress/gzip"
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/Agent-Field/aforge-v2/internal/home"
)

// staged holds whatever the build put in cache/ — in a shipped build, exactly
// one gzipped furrow for the platform being built for, and in a fresh clone
// nothing but the README that keeps the folder in git.
//
// THE STAGED FILE IS NAMED FOR ITS PLATFORM ON PURPOSE. A cross-compiled build
// whose cache still holds yesterday's darwin furrow embeds a file this linux
// binary will never ask for, so the mismatch reads as "no furrow embedded" —
// which falls through to PATH — instead of as a binary that confidently
// extracts a Mach-O executable onto a Linux machine and hands the person an
// exec-format error from a program they never installed.
//
//go:embed cache
var staged embed.FS

// ErrNotEmbedded is what [Ensure] answers on a build that carries no furrow for
// this platform — a plain `go build ./...` in a fresh clone, which is a real
// and useful thing to be able to do. It is not the shipped state: `make build`
// fetches first and fails rather than produce one.
var ErrNotEmbedded = errors.New("this aforge was not built with furrow inside it")

// stagedName is where the build put this platform's furrow, and it is derived
// rather than written down so that the fetcher and the reader cannot disagree.
func stagedName() string {
	return "cache/furrow-" + Platform(runtime.GOOS, runtime.GOARCH) + ".gz"
}

// Embedded reports whether this build carries furrow for the machine it is
// running on. It reads the embedded directory and decompresses nothing, so
// asking is free.
func Embedded() bool {
	_, err := fs.Stat(staged, stagedName())
	return err == nil
}

var (
	ensureOnce sync.Once
	ensurePath string
	ensureErr  error
)

// Ensure puts the embedded furrow on disk if it is not already there and
// returns the path to it.
//
// It is memoised for the life of the process because it sits in front of belt
// construction, where it is asked once per session and would otherwise cost a
// stat each time; the first call in a fresh state root costs one decompression
// and one six-megabyte write, and every call after that on every later boot
// costs one stat.
func Ensure() (string, error) {
	ensureOnce.Do(func() { ensurePath, ensureErr = extract() })
	return ensurePath, ensureErr
}

// extract is Ensure's one-time body: find the carried bytes, then put them
// where the state root keeps them.
func extract() (string, error) {
	archive, err := staged.ReadFile(stagedName())
	if err != nil {
		return "", ErrNotEmbedded
	}
	return extractInto(home.Join("bin"), Version(), archive)
}

// extractInto is the part with the laws in it, and it takes its directory and
// its version rather than reading them so that the tests can drive every case
// the real one has exactly one of — a fresh state root, a version already
// there, an older version beside it, a root that cannot be written to.
func extractInto(dir, version string, archive []byte) (string, error) {
	// THE PATH IS STAMPED WITH THE VERSION, AND THAT IS WHAT MAKES REPLACING A
	// RUNNING BINARY IMPOSSIBLE RATHER THAN CAREFUL. An aforge that upgrades
	// its pinned furrow writes a file with a new name; the old one keeps its
	// inode and any furrow still running out of it keeps running. This repo has
	// paid the other bill twice — a binary written over in place is a process
	// killed with signal 9 on macOS the moment it next pages in — and the
	// lesson generalises past aforge's own binary to any binary it writes.
	path := filepath.Join(dir, "furrow-"+version)

	// One stat is the steady state. A file at this path is complete by
	// construction — it got there by rename, below, and a rename either
	// happened or did not — so its existence, its being an ordinary file and
	// its executable bit are the whole check. Hashing six megabytes on every
	// boot to re-learn what the build already verified against the pin would
	// be paying twice for one fact.
	if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 && info.Size() > 0 {
		return path, nil
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("make room for furrow in %s: %w", dir, err)
	}

	binary, err := decompress(archive)
	if err != nil {
		return "", err
	}

	// Write somewhere else and move it into place. Two aforges starting at the
	// same moment each write their own temporary file and each rename it over
	// the same name; the rename is atomic, the loser's bytes are identical to
	// the winner's, and neither ever sees a half-written furrow.
	temporary, err := os.CreateTemp(dir, "furrow-*.partial")
	if err != nil {
		return "", fmt.Errorf("write furrow into %s: %w", dir, err)
	}
	defer os.Remove(temporary.Name())
	if _, err := temporary.Write(binary); err != nil {
		temporary.Close()
		return "", fmt.Errorf("write furrow into %s: %w", dir, err)
	}
	if err := temporary.Chmod(0o755); err != nil {
		temporary.Close()
		return "", fmt.Errorf("make furrow executable: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return "", fmt.Errorf("write furrow into %s: %w", dir, err)
	}
	if err := os.Rename(temporary.Name(), path); err != nil {
		return "", fmt.Errorf("put furrow at %s: %w", path, err)
	}
	return path, nil
}

// decompress inflates the staged archive. It is a plain gzip stream of the
// artifact's own bytes and nothing else — no container format, because there is
// exactly one file in it and a format would be a second thing to keep in step.
func decompress(archive []byte) ([]byte, error) {
	stream, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("read the embedded furrow: %w", err)
	}
	binary, err := io.ReadAll(stream)
	if err != nil {
		return nil, fmt.Errorf("read the embedded furrow: %w", err)
	}
	if err := stream.Close(); err != nil {
		return nil, fmt.Errorf("read the embedded furrow: %w", err)
	}
	return binary, nil
}

// Compress is what the fetcher stages with, and it lives here rather than in
// the fetcher so that the two halves of the format — who writes it and who
// reads it — are the same twenty lines apart.
func Compress(binary []byte) ([]byte, error) {
	var archive bytes.Buffer
	stream, err := gzip.NewWriterLevel(&archive, gzip.BestCompression)
	if err != nil {
		return nil, err
	}
	if _, err := stream.Write(binary); err != nil {
		return nil, err
	}
	if err := stream.Close(); err != nil {
		return nil, err
	}
	return archive.Bytes(), nil
}

// StagedFileName is the name the fetcher writes inside cache/ for one platform.
// The build needs it for a platform it is cross-compiling to, which is why it
// takes the platform rather than reading runtime's.
func StagedFileName(platform string) string { return "furrow-" + platform + ".gz" }
