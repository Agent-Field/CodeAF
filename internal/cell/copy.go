package cell

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/filelock"
)

// copyCheckpoint is a process-death seam; production never pauses construction.
var copyCheckpoint = func(string) {}

// CopyReadyPath marks a legacy copy whose initial turn is durable on this
// computer; snapshots never carry it because it is local bookkeeping.
const CopyReadyPath = StateDir + "/copy-ready"

// CopyOfferedPath is local bookkeeping inside the harness-owned state area.
const CopyOfferedPath = StateDir + "/copy-offered"

// CopyRoot gives an older conversation one stable destination outside the
// folders released builds scan. Its identity never depends on a retry (C29.6).
func CopyRoot(original string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(original)))
	var raw [16]byte
	copy(raw[:], sum[:16])
	return filepath.Join(filepath.Dir(original), ".moving", encode128(raw))
}

// CopyFolderReadme is the note in the folder that holds moved chats. The
// folder's name reads like a transfer in progress, and removing it by hand
// would delete those chats, so the folder says what it is (#2072). The name is
// kept: older builds and copies already made look for it.
const CopyFolderReadme = "README.txt"

const copyFolderNote = `This folder holds your codeaf chats. They are the live copies codeaf writes to,
not a move in progress. Do not delete it or anything in it: that deletes those
chats. The older originals beside it stay untouched for older codeaf builds.
`

// explainCopyFolder writes the note once; an existing note is left as it is.
func explainCopyFolder(dir string) error {
	f, err := os.OpenFile(filepath.Join(dir, CopyFolderReadme), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = f.WriteString(copyFolderNote)
	return errors.Join(err, f.Close())
}

// Copied resolves only a fully prepared, durable copy. A crash before the
// completion marker leaves the original as the only visible conversation.
func Copied(original string) string {
	root := CopyRoot(original)
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(CopyReadyPath))); err == nil {
		if _, err := OpenAt(root, filepath.Base(root)); err == nil {
			return root
		}
	}
	return original
}

// StampCopyReady records on disk that root is a completed `.moving` copy on
// this computer so Home and legacy-open paths resolve it instead of the
// untouched original.
func StampCopyReady(root string) error {
	p := filepath.Join(root, filepath.FromSlash(CopyReadyPath))
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	return errors.Join(f.Sync(), f.Close())
}

// hasSealedTurns reports whether root already holds a cell with at least one
// sealed turn or adopted remote head. Engine.Seal appends to turns.jsonl only
// after composing meta.json and capturing the snapshot, and Materialize writes
// adopted.json only after restoring an incoming head, so either file proves
// root holds real conversation history rather than a half-copied scratch tree.
func hasSealedTurns(root string) bool {
	if raw, err := os.ReadFile(filepath.Join(root, StateDir, "turns.jsonl")); err == nil {
		sc := bufio.NewScanner(bytes.NewReader(raw))
		for sc.Scan() {
			line := bytes.TrimSpace(sc.Bytes())
			if len(line) == 0 {
				continue
			}
			var turn struct {
				ID string `json:"id"`
			}
			if json.Unmarshal(line, &turn) == nil && strings.TrimSpace(turn.ID) != "" {
				return true
			}
		}
	}
	if raw, err := os.ReadFile(filepath.Join(root, StateDir, "adopted.json")); err == nil {
		var adopted struct {
			Head string `json:"head"`
		}
		if json.Unmarshal(raw, &adopted) == nil && strings.TrimSpace(adopted.Head) != "" {
			return true
		}
	}
	return false
}

// CopyLegacy makes a private byte copy and invokes prepare before publishing
// it. It never links files to the original: later writes must stay independent.
// The lock lives outside the original and serializes windows and crash retries.
func CopyLegacy(ctx context.Context, original string, prepare func(context.Context, Cell) error) (Cell, error) {
	original = filepath.Clean(original)
	info, err := os.Lstat(original)
	if err != nil {
		return Cell{}, err
	}
	if !info.IsDir() {
		return Cell{}, errors.New("older chat folder is not a directory")
	}
	root := CopyRoot(original)
	if err := os.MkdirAll(filepath.Dir(root), 0700); err != nil {
		return Cell{}, err
	}
	if err := explainCopyFolder(filepath.Dir(root)); err != nil {
		return Cell{}, err
	}
	lock, err := os.OpenFile(root+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return Cell{}, err
	}
	defer lock.Close()
	if err := filelock.Lock(lock, true, true); err != nil {
		return Cell{}, err
	}
	if Copied(original) != original {
		return OpenAt(root, filepath.Base(root))
	}
	// Never wipe a destination that already holds sealed turns even if an
	// earlier restore or take-back lost its copy-ready marker: reopen and
	// re-stamp it when its cell record is valid, or fail loudly so newer turns
	// cannot be destroyed.
	if hasSealedTurns(root) {
		c, openErr := OpenAt(root, filepath.Base(root))
		if openErr != nil {
			return Cell{}, fmt.Errorf("copy destination %s already holds sealed turns: %w", root, openErr)
		}
		if err := StampCopyReady(root); err != nil {
			return Cell{}, err
		}
		return c, nil
	}
	if err := ctx.Err(); err != nil {
		return Cell{}, err
	}
	if _, err := os.Stat(filepath.Join(original, legacyTranscript)); err != nil {
		return Cell{}, err
	}
	if isCell(original) {
		return Cell{}, errors.New("this chat already moves")
	}
	if err := os.RemoveAll(root); err != nil {
		return Cell{}, err
	}
	source, err := os.Open(filepath.Join(original, legacyTranscript))
	if err != nil {
		return Cell{}, err
	}
	defer source.Close()
	if err := filelock.Lock(source, true, true); err != nil {
		return Cell{}, err
	}
	copyCheckpoint("source-locked")
	if err := copyFiles(ctx, original, root); err != nil {
		return Cell{}, err
	}
	copyCheckpoint("copied")
	// Migration operates solely on the independent copy. No source inode is
	// shared, even for files the migration itself links inside that copy.
	if err := MigrateLegacy(root); err != nil {
		return Cell{}, err
	}
	copyCheckpoint("migrated")
	c, err := OpenAt(root, filepath.Base(root))
	if err != nil {
		return Cell{}, err
	}
	if err := prepare(ctx, c); err != nil {
		return Cell{}, err
	}
	copyCheckpoint("prepared")
	if err := ctx.Err(); err != nil {
		return Cell{}, err
	}
	if err := StampCopyReady(root); err != nil {
		return Cell{}, err
	}
	copyCheckpoint("published")
	return c, nil
}

// copyFiles copies regular files and symlinks without following links outside
// the source. File modes and times are preserved; sockets and locks are local.
func copyFiles(ctx context.Context, source, dest string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		out := filepath.Join(dest, rel)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return os.MkdirAll(out, info.Mode().Perm())
		}
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(target, out)
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		f, err := os.OpenFile(out, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
		if err != nil {
			return err
		}
		copyCheckpoint("file-created")
		_, err = io.Copy(f, in)
		copyCheckpoint("file-written")
		if err = errors.Join(err, f.Sync(), f.Close()); err != nil {
			return err
		}
		return os.Chtimes(out, info.ModTime(), info.ModTime())
	})
}
