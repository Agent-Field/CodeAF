package session

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// A source snapshot is the identity under which an observed outcome was earned:
// the exact commit plus a bounded overlay of the changed and untracked files a
// dirty tree carries. It replaces the old all-or-nothing rule that gave a dirty
// tree NO identity at all, which erased every lesson the moment the tree moved.
//
// THE CAPS ARE PART OF THE CONTRACT. A listing that overflows a file, byte or
// time bound, or a tree that moves while it is being read, is recorded as
// unknown — and an unknown snapshot can never certify a current test.
const (
	sourceSnapshotMaxFiles = 512
	sourceSnapshotMaxBytes = 8 << 20
	sourceSnapshotMaxFile  = 1 << 20
	sourceSnapshotTimeout  = 2500 * time.Millisecond
)

var errSnapshotCap = errors.New("source snapshot exceeded its bound")

// sourceSnapshot is the identity of the source tree at capture time. Identity
// is the canonical token stored in evidence and attempt rows; a clean tree
// keeps the bare commit so existing rows are unchanged, a dirty tree appends a
// bounded content hash, and an unknown or truncated capture spells "unknown".
type sourceSnapshot struct {
	Identity  string
	Head      string
	Dirty     bool
	Truncated bool
	Unknown   bool
}

// current reports whether this capture may certify the present source. Only a
// complete, stable listing may; truncated and unknown captures may not.
func (s sourceSnapshot) current() bool { return !s.Unknown && !s.Truncated && s.Head != "" }

// captureSourceSnapshot reads HEAD and a bounded overlay through the one Git
// root reader the session already has. It never stores source bytes: a file is
// read only to hash, and a symlink is hashed by its target text.
//
// A GIT READ THAT FAILS IS NOT A CLEAN TREE. Every command here must succeed
// and agree with a second reading before the tree is certified; a timeout, a
// non-repository root or a listing that changes underneath the first read all
// yield an unknown identity rather than a false clean one.
func (a *Agent) captureSourceSnapshot(ctx context.Context) sourceSnapshot {
	if strings.TrimSpace(a.config.Workspace) == "" {
		return sourceSnapshot{Identity: "unknown", Unknown: true}
	}
	root, ok := repositoryRoot(a.config.Workspace)
	if !ok {
		return sourceSnapshot{Identity: "unknown", Unknown: true}
	}
	bounded, cancel := context.WithTimeout(ctx, sourceSnapshotTimeout)
	defer cancel()
	head, err := gitCapture(bounded, root, "rev-parse", "HEAD")
	head = strings.TrimSpace(head)
	if err != nil || head == "" {
		return sourceSnapshot{Identity: "unknown", Unknown: true}
	}
	first, err := gitCapture(bounded, root, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return sourceSnapshot{Identity: "unknown", Head: head, Unknown: true}
	}
	if len(first) == 0 {
		return sourceSnapshot{Identity: head, Head: head}
	}
	if len(first) > sourceSnapshotMaxBytes {
		return sourceSnapshot{Identity: "unknown", Head: head, Dirty: true, Truncated: true}
	}
	entries, err := snapshotEntries(bounded, first, root)
	if err != nil {
		truncated := errors.Is(err, errSnapshotCap)
		return sourceSnapshot{Identity: "unknown", Head: head, Dirty: true, Truncated: truncated, Unknown: !truncated}
	}
	overlay := contextualHash(strings.Join(entries, "\n"))
	// A TREE THAT MOVES WHILE IT IS READ IS NOT AN IDENTITY. HEAD and the
	// listing are read a second time and must agree; a mismatch is the honest
	// unknown rather than a token certifying a moment that never existed.
	head2, err := gitCapture(bounded, root, "rev-parse", "HEAD")
	if err != nil || strings.TrimSpace(head2) != head {
		return sourceSnapshot{Identity: "unknown", Head: head, Dirty: true, Unknown: true}
	}
	second, err := gitCapture(bounded, root, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil || first != second {
		return sourceSnapshot{Identity: "unknown", Head: head, Dirty: true, Unknown: true}
	}
	return sourceSnapshot{Identity: "dirty:" + head + ":" + overlay[:16], Head: head, Dirty: true}
}

// snapshotEntries parses one porcelain-v1 NUL listing into stable hash lines.
// The fields are joined with NUL rather than a printable separator so a name
// containing the separator cannot forge another entry's identity. Rename and
// copy entries emit a second path field, which is consumed here and hashed as
// its own component.
func snapshotEntries(ctx context.Context, raw string, root string) ([]string, error) {
	fields := strings.Split(raw, "\x00")
	out := make([]string, 0, len(fields))
	budget := int64(sourceSnapshotMaxBytes)
	for i := 0; i < len(fields); i++ {
		field := fields[i]
		if len(field) < 3 {
			continue
		}
		if ctx.Err() != nil {
			return nil, errSnapshotCap
		}
		if len(out) >= sourceSnapshotMaxFiles {
			return nil, errSnapshotCap
		}
		status, path := field[:2], field[3:]
		pair := ""
		if strings.ContainsAny(status, "RC") {
			i++
			if i < len(fields) && fields[i] != "" {
				pair = fields[i]
			}
		}
		first, used, err := snapshotContent(ctx, root, path, status, budget)
		if err != nil {
			return nil, err
		}
		budget -= used
		// A rename or copy names a second path whose old location no longer
		// exists; it contributes its NAME only, hashed as its own NUL-separated
		// component, so an absence here is not a race and no two names can be
		// forged into one.
		out = append(out, status+"\x00"+path+"\x00"+pair+"\x00"+first)
	}
	return out, nil
}

// snapshotContent hashes the working-tree content of one changed path. A
// symlink contributes its target, a directory contributes its kind, a deleted
// tracked path contributes "absent" (its absence is proven by the D status),
// and a regular file is read up to the per-file bound. A path that is missing
// without a deletion status, unreadable from a permission error, or that
// changes size or mtime between its stat and its read is a race and reports an
// error rather than a guess.
func snapshotContent(ctx context.Context, root, rel, status string, remaining int64) (string, int64, error) {
	if ctx.Err() != nil {
		return "", 0, errSnapshotCap
	}
	path := filepath.Join(root, filepath.FromSlash(rel))
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			if strings.ContainsAny(status, "D") {
				return "absent", 0, nil
			}
			return "", 0, err
		}
		return "", 0, err
	}
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(path)
		if err != nil {
			return "", 0, err
		}
		return "link:" + target, int64(len(target)), nil
	case info.IsDir():
		return "dir", 0, nil
	case !info.Mode().IsRegular():
		return "special", 0, nil
	}
	limit := int64(sourceSnapshotMaxFile)
	if remaining < limit {
		limit = remaining
	}
	if info.Size() > limit {
		return "", 0, errSnapshotCap
	}
	data, err := readBounded(path, limit)
	if err != nil {
		return "", 0, err
	}
	if int64(len(data)) > limit {
		return "", 0, errSnapshotCap
	}
	// The file must be the same one that was stat-ed: an equal-listing race
	// (the file rewritten between stat and read) is refused, not certified.
	after, err := os.Lstat(path)
	if err != nil || after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) || after.Mode() != info.Mode() {
		return "", 0, err
	}
	return contextualHash(string(data)), int64(len(data)), nil
}

// readBounded reads at most limit bytes, so a file that grows after its stat
// can never allocate unbounded memory here.
func readBounded(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return io.ReadAll(io.LimitReader(file, limit+1))
}

// gitCapture runs one bounded git command and reports both its output and its
// failure. An empty answer with no error is a real empty answer; a failed or
// timed-out command is an error the caller must not read as clean.
func gitCapture(ctx context.Context, root string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...)
	out, err := command.Output()
	return string(out), err
}
