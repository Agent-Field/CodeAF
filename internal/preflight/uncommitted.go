package preflight

import (
	"bytes"
	"context"
	"os/exec"
	"time"
)

// gitLookBound bounds the one look at a folder's uncommitted files: a takeover
// does not wait on a slow disk to say what it found.
const gitLookBound = 2 * time.Second

// Uncommitted names the files in a folder that differ from its last commit, in
// the order git lists them. A folder that is not a repository, or a look that
// fails, has none to name.
func Uncommitted(dir string) []string {
	ctx, cancel := context.WithTimeout(context.Background(), gitLookBound)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "status", "--porcelain", "-z").Output() //codeaf:plumbing read-only status look at the folder a chat arrived in
	if err != nil {
		return nil
	}
	return changedNames(out)
}

// changedNames reads the paths out of `status --porcelain -z`: each entry is two
// status letters, a space and the path, and a rename is followed by its source.
func changedNames(out []byte) []string {
	var names []string
	parts := bytes.Split(out, []byte{0})
	for i := 0; i < len(parts); i++ {
		entry := parts[i]
		if len(entry) < 4 {
			continue
		}
		names = append(names, string(entry[3:]))
		if entry[0] == 'R' || entry[0] == 'C' {
			i++
		}
	}
	return names
}
