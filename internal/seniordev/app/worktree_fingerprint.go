//go:build !windows

package app

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

const (
	worktreeFingerprintMaxFiles = 4096
	worktreeFingerprintMaxBytes = 8 * 1024 * 1024
	worktreeFingerprintTimeout  = 2 * time.Second
)

type worktreeFileFingerprint struct {
	Mode        os.FileMode
	Size        int64
	ModTimeNano int64
	Missing     bool
	ContentHash string
}

type fingerprintStatus uint8

const (
	fingerprintOK fingerprintStatus = iota
	fingerprintFailed
	fingerprintOverBudget
)

type worktreeFingerprinter struct {
	runner *pipeline
	parent context.Context
	ctx    context.Context
	cancel context.CancelFunc
}

func newWorktreeFingerprinter(
	runner *pipeline, parent context.Context,
) *worktreeFingerprinter {
	ctx, cancel := context.WithTimeout(parent, worktreeFingerprintTimeout)
	return &worktreeFingerprinter{
		runner: runner, parent: parent, ctx: ctx, cancel: cancel,
	}
}

func (fingerprinter *worktreeFingerprinter) fingerprint() (string, bool) {
	defer fingerprinter.cancel()
	paths, remaining, status := fingerprinter.listPaths()
	if status != fingerprintOK {
		return fingerprinter.result(status)
	}
	digest := sha256.New()
	nextFiles := make(map[string]worktreeFileFingerprint, len(paths))
	for _, relative := range paths {
		if status = fingerprinter.contextStatus(); status != fingerprintOK {
			return fingerprinter.result(status)
		}
		state, consumed, status := fingerprinter.fingerprintPath(relative, remaining)
		if status != fingerprintOK {
			return fingerprinter.result(status)
		}
		remaining -= consumed
		nextFiles[relative] = state
		writeFingerprintState(digest, relative, state)
	}
	fingerprinter.runner.fingerprintFiles = nextFiles
	return fmt.Sprintf("%x", digest.Sum(nil)), true
}

func (fingerprinter *worktreeFingerprinter) result(
	status fingerprintStatus,
) (string, bool) {
	if status == fingerprintOverBudget {
		return fingerprinter.runner.overBudgetFingerprint(), true
	}
	return "", false
}

func (fingerprinter *worktreeFingerprinter) contextStatus() fingerprintStatus {
	if fingerprinter.ctx.Err() == nil {
		return fingerprintOK
	}
	if fingerprinter.parent.Err() == nil {
		return fingerprintOverBudget
	}
	return fingerprintFailed
}

func (fingerprinter *worktreeFingerprinter) listPaths() (
	[]string, int64, fingerprintStatus,
) {
	paths, consumed, overBudget, err := fingerprinter.runner.recorder.ListPaths(
		fingerprinter.ctx, worktreeFingerprintMaxBytes,
	)
	if overBudget {
		return nil, 0, fingerprintOverBudget
	}
	if status := fingerprinter.contextStatus(); status != fingerprintOK {
		return nil, 0, status
	}
	if err != nil {
		return nil, 0, fingerprintFailed
	}
	if len(paths) > worktreeFingerprintMaxFiles {
		return nil, 0, fingerprintOverBudget
	}
	return paths, int64(worktreeFingerprintMaxBytes - consumed), fingerprintOK
}

func (fingerprinter *worktreeFingerprinter) fingerprintPath(
	relative string, remaining int64,
) (worktreeFileFingerprint, int64, fingerprintStatus) {
	path := filepath.Join(
		fingerprinter.runner.workspace, filepath.FromSlash(relative),
	)
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return worktreeFileFingerprint{Missing: true}, 0, fingerprintOK
	}
	if err != nil {
		return worktreeFileFingerprint{}, 0, fingerprintFailed
	}
	state := worktreeFileFingerprint{
		Mode: info.Mode(), Size: info.Size(), ModTimeNano: info.ModTime().UnixNano(),
	}
	cached, ok := fingerprinter.runner.fingerprintFiles[relative]
	metadataUnchanged := ok && !cached.Missing && cached.Mode == state.Mode &&
		cached.Size == state.Size && cached.ModTimeNano == state.ModTimeNano
	if metadataUnchanged {
		state.ContentHash = cached.ContentHash
		return state, 0, fingerprintOK
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fingerprintSymlink(path, state, remaining)
	}
	if info.Mode().IsRegular() {
		return fingerprintRegularFile(fingerprinter.ctx, path, state, remaining)
	}
	return state, 0, fingerprintOK
}

func fingerprintSymlink(
	path string, state worktreeFileFingerprint, remaining int64,
) (worktreeFileFingerprint, int64, fingerprintStatus) {
	target, err := os.Readlink(path)
	if err != nil {
		return worktreeFileFingerprint{}, 0, fingerprintFailed
	}
	consumed := int64(len(target))
	if consumed > remaining {
		return worktreeFileFingerprint{}, 0, fingerprintOverBudget
	}
	sum := sha256.Sum256([]byte(target))
	state.ContentHash = fmt.Sprintf("%x", sum[:])
	return state, consumed, fingerprintOK
}

func fingerprintRegularFile(
	ctx context.Context, path string, state worktreeFileFingerprint, remaining int64,
) (worktreeFileFingerprint, int64, fingerprintStatus) {
	contentHash, consumed, ok := boundedFileHash(ctx, path, remaining)
	if !ok {
		return worktreeFileFingerprint{}, 0, fingerprintOverBudget
	}
	state.ContentHash = contentHash
	return state, consumed, fingerprintOK
}

func writeFingerprintState(
	digest io.Writer, relative string, state worktreeFileFingerprint,
) {
	_, _ = digest.Write([]byte(relative + "\x00"))
	if state.Missing {
		_, _ = digest.Write([]byte("missing\x00" + state.ContentHash + "\x00"))
		return
	}
	_, _ = digest.Write([]byte(fmt.Sprintf(
		"%s\x00%d\x00%d\x00%s\x00",
		state.Mode, state.Size, state.ModTimeNano, state.ContentHash,
	)))
}

func boundedFileHash(
	ctx context.Context, path string, remaining int64,
) (string, int64, bool) {
	if remaining < 0 {
		return "", 0, false
	}
	file, err := os.Open(path)
	if err != nil {
		return "", 0, false
	}
	defer file.Close()
	digest := sha256.New()
	buffer := make([]byte, 64*1024)
	limited := io.LimitReader(file, remaining+1)
	consumed := int64(0)
	for {
		if ctx.Err() != nil {
			return "", consumed, false
		}
		read, readErr := limited.Read(buffer)
		if read > 0 {
			consumed += int64(read)
			_, _ = digest.Write(buffer[:read])
			if consumed > remaining {
				return "", consumed, false
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return "", consumed, false
		}
	}
	return fmt.Sprintf("%x", digest.Sum(nil)), consumed, true
}
