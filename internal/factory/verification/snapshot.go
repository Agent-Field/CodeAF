package verification

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/shell"
)

// Snapshot binds a receipt to actual checkout contents, not just a mutable
// branch name. No real index, refs or files are changed to take this reading.
type Snapshot struct {
	Head        string `json:"head,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
}

// The same shell is used at invocation time and by the collector. Only hashes
// leave the pipe; file contents and Git credentials never enter a receipt.
const worktreeStateFunction = `_codeaf_worktree_state() (
 set -o pipefail
 export GIT_OPTIONAL_LOCKS=0
 local _root _head _path
 _root=$(command git rev-parse --show-toplevel 2>/dev/null) || return 4
 cd "$_root" || return 1
 _head=$(command git rev-parse --verify HEAD 2>/dev/null) || return 1
 printf '%s ' "$_head"
 {
  command git diff --binary --no-ext-diff --no-textconv HEAD -- . || exit 1
  command git ls-files --others --exclude-standard -z | while IFS= read -r -d '' _path; do
   printf '%s\0' "$_path"
   if [ -L "$_path" ]; then
    printf 'link\0'; command readlink "$_path" || exit 1
   else
    if [ -x "$_path" ]; then printf 'executable\0'; else printf 'file\0'; fi
    command git hash-object --no-filters -- "$_path" || exit 1
   fi
  done
 } | command git hash-object --stdin
)
`

func snapshotText(raw string) (Snapshot, error) {
	words := strings.Fields(raw)
	if len(words) == 0 {
		return Snapshot{}, nil
	}
	valid := func(s string) bool {
		return (len(s) == 40 || len(s) == 64) && strings.Trim(s, "0123456789abcdef") == ""
	}
	if len(words) != 2 || !valid(words[0]) || !valid(words[1]) {
		return Snapshot{}, fmt.Errorf("invalid verification checkout snapshot")
	}
	return Snapshot{Head: words[0], Fingerprint: words[1]}, nil
}
func readSnapshot(path string) (Snapshot, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Snapshot{}, nil
	}
	if err != nil {
		return Snapshot{}, err
	}
	return snapshotText(string(raw))
}

// SnapshotAt reads the same checkout fingerprint as the shell observer.
func SnapshotAt(dir string) (Snapshot, error) {
	bash, err := shell.Bash()
	if err != nil {
		return Snapshot{}, fmt.Errorf("cannot read verification checkout snapshot: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bash, "-c", worktreeStateFunction+"\n_codeaf_worktree_state")
	cmd.Dir = dir
	raw, err := cmd.Output()
	if err != nil {
		if e, ok := err.(*exec.ExitError); ok && e.ExitCode() == 4 {
			return Snapshot{}, nil
		}
		return Snapshot{}, fmt.Errorf("cannot read verification checkout snapshot: %w", err)
	}
	return snapshotText(string(raw))
}

// Fresh requires a stable check invocation on the checkout being accepted.
func (c Check) Fresh(at Snapshot) bool { return at.Head != "" && c.Before == at && c.After == at }
