package cellstore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/inventory"
)

const reasonGitIgnored = "git ignores them"
const ignoredReadFailure = "cannot read which folders Git ignores here"

// gitIgnored keeps a rebuild instruction from naming a lockfile that will not
// arrive. Tracked exceptions are deliberately honored by Git's default check.
// An unreadable Git answer licenses no rebuild; outside Git the old rule holds.
func gitIgnored(tree, path string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "check-ignore", "-q", "--", path) //codeaf:plumbing A rebuild needs the lockfile in the capture.
	cmd.Dir = tree
	_, err := cmd.Output()
	if err == nil {
		return true
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode() != 1 && !strings.Contains(string(exit.Stderr), "not a git repository")
	}
	return true
}

// ignoredDirectories asks Git for collapsed directories, never the files inside
// them. Git keeps tracked exceptions out of that list. Ignored files in ordinary
// directories still pass the secret screen and travel through the vault (D2).
// A failed lookup in a repository must not fall back to screening ignored
// directories: that would send their secret files through the vault (D2).
func ignoredDirectories(tree string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "ls-files", "--others", "--ignored", "--exclude-standard", "--directory", "-z") //codeaf:plumbing Git names ignored directories without opening them.
	cmd.Dir = tree
	raw, err := cmd.Output()
	if err != nil {
		var exit *exec.ExitError
		if (errors.As(err, &exit) && strings.Contains(string(exit.Stderr), "not a git repository")) || !hasGitMarker(tree) {
			return nil, nil
		}
		return nil, fmt.Errorf("%s: %w", ignoredReadFailure, err)
	}
	var candidates []string
	for _, name := range strings.Split(string(raw), "\x00") {
		if strings.HasSuffix(name, "/") {
			candidates = append(candidates, name)
		}
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	// ls-files may collapse a directory whose only child is an ignored file.
	// Pruning that directory would hide config/.env.local from the vault.
	check := exec.CommandContext(ctx, "git", "check-ignore", "--stdin", "-z") //codeaf:plumbing Only an ignored directory itself can bypass the secret screen.
	check.Dir = tree
	check.Stdin = strings.NewReader(strings.Join(candidates, "\x00") + "\x00")
	checked, err := check.Output()
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 1 {
			return nil, fmt.Errorf("%s: %w", ignoredReadFailure, err)
		}
	}
	var dirs []string
	for _, name := range strings.Split(string(checked), "\x00") {
		if name != "" {
			dirs = append(dirs, strings.TrimSuffix(name, "/"))
		}
	}
	return dirs, nil
}

func hasGitMarker(tree string) bool {
	for dir := filepath.Clean(tree); ; dir = filepath.Dir(dir) {
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil || os.IsPermission(err) {
			return true
		}
		if filepath.Dir(dir) == dir {
			return false
		}
	}
}

// ignoredPaths reads only the engine's paths. Older counts and newer summary
// fields are deliberately unknown to the harness (D2).
type ignoredPaths struct {
	Paths []string `json:"paths"`
}

// noteIgnored records the engine's omissions. The first capture discovers them;
// when that changes the record, a state-only capture carries the same facts to
// the next computer before the turn can be published.
func noteIgnored(c cell.Cell, ignored *ignoredPaths) (bool, error) {
	if ignored == nil {
		return false, nil
	}
	store, err := inventory.Open(c.Root)
	if err != nil {
		return false, err
	}
	before := store.Snapshot().Withheld
	var old []string
	for _, w := range before {
		if w.Reason == reasonGitIgnored {
			old = append(old, w.Path)
		}
	}
	if strings.Join(old, "\x00") == strings.Join(ignored.Paths, "\x00") {
		return false, nil
	}
	paths := map[string]bool{}
	var entries []inventory.Withheld
	for _, w := range before {
		if w.Reason == reasonGitIgnored {
			paths[w.Path] = true
		}
	}
	for _, path := range ignored.Paths {
		paths[path] = true
		entries = append(entries, inventory.Withheld{Path: path, Reason: reasonGitIgnored})
	}
	err = inventory.Record(c.Root, func(inv *inventory.Inventory) {
		inv.SetWithheld(func(path string) bool { return paths[path] }, entries)
	})
	return err == nil, err
}
