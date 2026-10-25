package session

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/taskstorage"
)

const taskStorageGitLock = "codeaf prototype compact task storage"

type taskGitPin struct {
	Root     string `json:"root"`
	Admin    string `json:"admin"`
	Snapshot string `json:"snapshot"`
	Head     string `json:"head"`
	Index    string `json:"index"`
	Owned    bool   `json:"owned"`
}

func taskGitPinPath(manager taskstorage.Manager, path string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(path)))
	return filepath.Join(manager.Root, "git-pins", hex.EncodeToString(sum[:])+".json")
}
func readTaskGitPin(manager taskstorage.Manager, path string) (taskGitPin, error) {
	var pin taskGitPin
	raw, e := os.ReadFile(taskGitPinPath(manager, path))
	if e != nil {
		return pin, e
	}
	e = json.Unmarshal(raw, &pin)
	return pin, e
}
func writeTaskGitPin(manager taskstorage.Manager, path string, pin taskGitPin) error {
	name := taskGitPinPath(manager, path)
	if e := os.MkdirAll(filepath.Dir(name), 0700); e != nil {
		return e
	}
	raw, e := json.Marshal(pin)
	if e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(name), ".pin-")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if e = f.Chmod(0600); e == nil {
		_, e = f.Write(raw)
	}
	if e == nil {
		e = f.Sync()
	}
	closed := f.Close()
	if e == nil {
		e = closed
	}
	if e != nil {
		return e
	}
	if e = os.Rename(f.Name(), name); e != nil {
		return e
	}
	dir, e := os.Open(filepath.Dir(name))
	if e != nil {
		return e
	}
	defer dir.Close()
	return dir.Sync()
}
func taskIndexDigest(admin string) (string, error) {
	body, e := os.ReadFile(filepath.Join(admin, "index"))
	if errors.Is(e, os.ErrNotExist) {
		return "absent", nil
	}
	if e != nil {
		return "", e
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}

// pinTaskGit keeps Git's external administration alive while a linked working
// directory is cold. Its compact backup preserves the index if normal pruning
// or an administrative repair removes it; shared repository objects stay shared.
func pinTaskGit(ctx context.Context, manager taskstorage.Manager, place Place, path string) (retErr error) {
	info, e := os.Lstat(filepath.Join(path, ".git"))
	if e != nil {
		return e
	}
	if info.IsDir() {
		return nil
	}
	if !info.Mode().IsRegular() {
		return errors.New("task Git pointer is not a regular file")
	}
	root, ok := repositoryRoot(path)
	if !ok {
		return errors.New("task Git repository is unavailable")
	}
	defer lockGitRoot(place, root)()
	admin, common, e := taskGitAdministration(path)
	if e != nil {
		return e
	}
	return captureTaskGitPin(ctx, manager, path, admin, common)
}

func taskGitAdministration(path string) (string, string, error) {
	admin, e := git(path, "rev-parse", "--absolute-git-dir")
	if e != nil {
		return "", "", e
	}
	admin = canonicalPath(strings.TrimSpace(admin))
	common, e := git(path, "rev-parse", "--git-common-dir")
	if e != nil {
		return "", "", e
	}
	common = strings.TrimSpace(common)
	if !filepath.IsAbs(common) {
		common = filepath.Join(path, common)
	}
	common = canonicalPath(common)
	rel, e := filepath.Rel(filepath.Join(common, "worktrees"), admin)
	if e != nil || !filepath.IsLocal(rel) || filepath.Dir(rel) != "." {
		return "", "", errors.New("task Git administration is outside its shared repository")
	}
	return admin, common, nil
}

func captureTaskGitPin(ctx context.Context, manager taskstorage.Manager, path, admin, common string) (retErr error) {
	acquired := false
	defer func() {
		if retErr == nil || !acquired {
			return
		}
		if lock, err := os.ReadFile(filepath.Join(admin, "locked")); err == nil && strings.TrimSpace(string(lock)) == taskStorageGitLock {
			_, _ = git(path, "worktree", "unlock", path)
		}
	}()
	pin, e := priorTaskGitPin(manager, path, admin, common)
	if e != nil {
		return e
	}
	if lock, e := os.ReadFile(filepath.Join(admin, "locked")); errors.Is(e, os.ErrNotExist) {
		if _, e = git(path, "worktree", "lock", "--reason", taskStorageGitLock, path); e != nil {
			return e
		}
		pin.Owned = true
		acquired = true
	} else if e != nil {
		return e
	} else if strings.TrimSpace(string(lock)) != taskStorageGitLock {
		pin.Owned = false
	}
	head, e := git(path, "rev-parse", "HEAD")
	if e != nil {
		return e
	}
	pin.Head = strings.TrimSpace(head)
	pin.Index, e = taskIndexDigest(admin)
	if e != nil {
		return e
	}
	pin.Snapshot, e = manager.Vault.Capture(ctx, admin)
	if e != nil {
		return e
	}
	return writeTaskGitPin(manager, path, pin)
}

func priorTaskGitPin(manager taskstorage.Manager, path, admin, common string) (taskGitPin, error) {
	pin := taskGitPin{Root: common, Admin: admin}
	if prior, e := readTaskGitPin(manager, path); e == nil {
		if prior.Admin != admin || prior.Root != common {
			return pin, errors.New("task Git administration changed since its last archive")
		}
		pin.Owned = prior.Owned
	} else if !errors.Is(e, os.ErrNotExist) {
		return pin, e
	}
	return pin, nil
}

func restoreTaskGit(ctx context.Context, manager taskstorage.Manager, path string) error {
	pin, e := readTaskGitPin(manager, path)
	if errors.Is(e, os.ErrNotExist) {
		return nil
	}
	if e != nil {
		return e
	}
	rel, e := filepath.Rel(filepath.Join(pin.Root, "worktrees"), pin.Admin)
	if e != nil || !filepath.IsAbs(pin.Root) || !filepath.IsLocal(rel) || filepath.Dir(rel) != "." || pin.Snapshot == "" {
		return errors.New("invalid task Git administration receipt")
	}
	// A local task archive depends on the shared repository's objects. Missing
	// repositories are refused rather than replaced by a different project's Git.
	if info, e := os.Stat(filepath.Join(pin.Root, "objects")); e != nil || !info.IsDir() {
		return errors.New("the shared Git repository needed by this task is unavailable")
	}
	if _, e = os.Lstat(pin.Admin); errors.Is(e, os.ErrNotExist) {
		if e = manager.Vault.Restore(ctx, pin.Snapshot, pin.Admin); e != nil {
			return e
		}
	} else if e != nil {
		return e
	}
	if e := verifyTaskGitPin(path, pin); e != nil {
		return e
	}
	return releaseTaskGitPin(manager, path, pin)
}

func verifyTaskGitPin(path string, pin taskGitPin) error {
	admin, e := git(path, "rev-parse", "--absolute-git-dir")
	if e != nil {
		return e
	}
	if canonicalPath(strings.TrimSpace(admin)) != pin.Admin {
		return errors.New("restored task points at a different Git administration folder")
	}
	head, e := git(path, "rev-parse", "HEAD")
	if e != nil {
		return e
	}
	index, e := taskIndexDigest(pin.Admin)
	if e != nil {
		return e
	}
	if strings.TrimSpace(head) != pin.Head || index != pin.Index {
		return errors.New("task Git commit or staged index changed while its files were archived")
	}
	return nil
}

func releaseTaskGitPin(manager taskstorage.Manager, path string, pin taskGitPin) error {
	if pin.Owned {
		lock, e := os.ReadFile(filepath.Join(pin.Admin, "locked"))
		if errors.Is(e, os.ErrNotExist) {
			return os.Remove(taskGitPinPath(manager, path))
		}
		if e != nil {
			return e
		}
		if strings.TrimSpace(string(lock)) != taskStorageGitLock {
			return fmt.Errorf("task Git lock is now owned by another writer")
		}
		if _, e = git(path, "worktree", "unlock", path); e != nil {
			return e
		}
	}
	return os.Remove(taskGitPinPath(manager, path))
}
