package cellstore

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// sameIndex compares staged paths, modes, object ids and conflict stages, not
// Git's expendable stat cache. The saved index is restored only into scratch;
// neither the person's index nor their working files are rewritten (C31.2).
func (e Engine) sameIndex(ctx context.Context, base string, target Target) (bool, error) {
	scratch, err := os.MkdirTemp("", "codeaf-index-")
	if err != nil {
		return false, err
	}
	defer os.RemoveAll(scratch)
	destination := target
	destination.Tree, destination.CellDir, destination.Timeline = scratch, "", "c31-index"
	transport := Spawn{Binary: e.Binary}
	if _, err = transport.Do(ctx, destination, indexScratchOp{}); err != nil {
		return false, err
	}
	defer func() { _, _ = transport.Do(ctx, destination, indexScratchOp{forget: true}) }()
	_, err = transport.Do(ctx, destination, restoreOp{Snapshot: base, Paths: []string{".git/index"}})
	if err != nil {
		return false, err
	}
	saved := filepath.Join(scratch, ".git", "index")
	savedInfo, err := os.Stat(saved)
	if err != nil {
		return false, err
	}
	locate := exec.CommandContext(ctx, "git", "--no-optional-locks", "-C", target.Tree, "rev-parse", "--git-path", "index")
	found, err := locate.Output()
	if err != nil {
		return false, err
	}
	indexPath := strings.TrimSpace(string(found))
	if !filepath.IsAbs(indexPath) {
		indexPath = filepath.Join(target.Tree, indexPath)
	}
	currentInfo, err := os.Stat(indexPath)
	if err != nil {
		return false, err
	}
	if savedInfo.Mode().Perm() != currentInfo.Mode().Perm() {
		return false, nil
	}
	before, err := indexEntries(ctx, target.Tree, saved)
	if err != nil {
		return false, err
	}
	after, err := indexEntries(ctx, target.Tree, "")
	if err != nil {
		return false, err
	}
	return bytes.Equal(before, after), nil
}

func indexEntries(ctx context.Context, root, index string) ([]byte, error) {
	command := exec.CommandContext(ctx, "git", "--no-optional-locks", "-C", root, "ls-files", "--stage", "-v", "-z")
	command.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	if index != "" {
		command.Env = append(command.Env, "GIT_INDEX_FILE="+index)
	}
	return command.Output()
}

// indexScratchOp attaches an empty inspection folder without a watcher and
// drops only that scratch timeline after the comparison.
type indexScratchOp struct{ forget bool }

func (o indexScratchOp) Verb() string {
	if o.forget {
		return "forget"
	}
	return "watch"
}
func (o indexScratchOp) Args() []string {
	if o.forget {
		return []string{"--json", "forget"}
	}
	return []string{"--json", "watch", "--no-daemon"}
}
