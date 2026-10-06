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

const (
	// sourceSnapshotSubmoduleDepth bounds how deep a nested submodule tree is
	// followed. Beyond it the submodule is UNKNOWN rather than a constant, so
	// two trees can never share an identity the reader could not actually read.
	sourceSnapshotSubmoduleDepth = 4
)

var (
	errSnapshotCap = errors.New("source snapshot exceeded its bound")
	// errSubmoduleUnknown is a submodule directory that IS a git work tree but
	// whose commit, listing or content could not be read. It propagates the whole
	// snapshot to UNKNOWN rather than collapsing to a constant two trees share.
	errSubmoduleUnknown = errors.New("submodule snapshot could not be read")
	// errSnapshotRace is the metadata mismatch between a file's stat and the
	// read of its bytes: the file was rewritten underneath the capture. It is a
	// DISTINCT error so the caller can report the honest unknown rather than
	// hash empty content and certify an identity that never existed.
	errSnapshotRace = errors.New("source snapshot raced a file change")
)

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
	first, err := gitCapture(bounded, root, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--ignore-submodules=none")
	if err != nil {
		return sourceSnapshot{Identity: "unknown", Head: head, Unknown: true}
	}
	if len(first) == 0 {
		return captureCleanSnapshot(bounded, root, head)
	}
	return captureDirtySnapshot(bounded, root, head, first)
}

// captureCleanSnapshot certifies an empty listing only after HEAD and the
// listing are read a SECOND time and agree, exactly as the dirty path is.
func captureCleanSnapshot(ctx context.Context, root, head string) sourceSnapshot {
	head2, err := gitCapture(ctx, root, "rev-parse", "HEAD")
	if err != nil || strings.TrimSpace(head2) != head {
		return sourceSnapshot{Identity: "unknown", Head: head, Unknown: true}
	}
	second, err := gitCapture(ctx, root, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--ignore-submodules=none")
	if err != nil || len(second) != 0 {
		return sourceSnapshot{Identity: "unknown", Head: head, Unknown: true}
	}
	return sourceSnapshot{Identity: head, Head: head}
}

// captureDirtySnapshot hashes the bounded overlay and refuses to certify a tree
// that moved while it was read.
func captureDirtySnapshot(ctx context.Context, root, head, first string) sourceSnapshot {
	if len(first) > sourceSnapshotMaxBytes {
		return sourceSnapshot{Identity: "unknown", Head: head, Dirty: true, Truncated: true}
	}
	entries, err := snapshotEntries(ctx, first, root, 0)
	if err != nil {
		truncated := errors.Is(err, errSnapshotCap)
		return sourceSnapshot{Identity: "unknown", Head: head, Dirty: true, Truncated: truncated, Unknown: !truncated}
	}
	overlay := contextualHash(strings.Join(entries, "\n"))
	head2, err := gitCapture(ctx, root, "rev-parse", "HEAD")
	if err != nil || strings.TrimSpace(head2) != head {
		return sourceSnapshot{Identity: "unknown", Head: head, Dirty: true, Unknown: true}
	}
	second, err := gitCapture(ctx, root, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--ignore-submodules=none")
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
func snapshotEntries(ctx context.Context, raw string, root string, depth int) ([]string, error) {
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
		first, used, err := snapshotContentDepth(ctx, root, path, status, budget, depth)
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

// snapshotContent hashes the working-tree content of one changed path. It is the
// depth-zero door onto [snapshotContentDepth].
func snapshotContent(ctx context.Context, root, rel, status string, remaining int64) (string, int64, error) {
	return snapshotContentDepth(ctx, root, rel, status, remaining, 0)
}

// snapshotContentDepth hashes one changed path. A symlink contributes its
// target, a directory contributes the submodule identity it can be proven to
// have (or the constant kind only when it is not a work tree at all), a deleted
// tracked path contributes "absent" (its absence is proven by the D status), and
// a regular file is read up to the per-file bound. A path that is missing
// without a deletion status, unreadable from a permission error, or that changes
// size or mtime between its stat and its read is a race and reports an error
// rather than a guess. A submodule whose own tree cannot be read propagates
// UNKNOWN rather than a constant two different trees could share.
func snapshotContentDepth(ctx context.Context, root, rel, status string, remaining int64, depth int) (string, int64, error) {
	if ctx.Err() != nil {
		return "", 0, errSnapshotCap
	}
	path := filepath.Join(root, filepath.FromSlash(rel))
	info, err := os.Lstat(path)
	if err != nil {
		return snapshotMissing(status, err)
	}
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		return snapshotSymlink(path)
	case info.IsDir():
		// A DIRTY GITLINK LOOKS THE SAME WHICHEVER COMMIT IS CHECKED OUT:
		// porcelain stays ` M child`. Reading the submodule's own HEAD and a
		// CONTENT-AWARE listing makes two different trees different source
		// identities; a submodule that is a work tree but unreadable is UNKNOWN.
		sub, err := submoduleIdentity(ctx, path, depth)
		if err != nil {
			return "", 0, err
		}
		if sub == "" {
			return "dir", 0, nil
		}
		return sub, int64(len(sub)), nil
	case !info.Mode().IsRegular():
		return "special", 0, nil
	}
	return snapshotRegularFile(path, info, remaining)
}

// snapshotMissing explains a stat failure: a deleted tracked path's absence is
// proven by its D status, any other absence is a race the caller must not read
// as an empty file.
func snapshotMissing(status string, err error) (string, int64, error) {
	if os.IsNotExist(err) && strings.ContainsAny(status, "D") {
		return "absent", 0, nil
	}
	return "", 0, err
}

// snapshotSymlink hashes a symlink by its target text.
func snapshotSymlink(path string) (string, int64, error) {
	target, err := os.Readlink(path)
	if err != nil {
		return "", 0, err
	}
	return "link:" + target, int64(len(target)), nil
}

// snapshotRegularFile reads a regular file up to the per-file bound and refuses
// a file rewritten between its stat and its read.
func snapshotRegularFile(path string, info os.FileInfo, remaining int64) (string, int64, error) {
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
	// (rewritten between stat and read) is refused, not certified.
	after, err := os.Lstat(path)
	if err != nil {
		return "", 0, errSnapshotRace
	}
	if after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) || after.Mode() != info.Mode() {
		return "", 0, errSnapshotRace
	}
	return contextualHash(string(data)), int64(len(data)), nil
}

// submoduleIdentity reads a submodule directory's own commit and a
// CONTENT-AWARE hash of its dirty overlay, so neither a moved commit nor a
// second edit of an already-dirty file is certified as the same tree. It answers
// "" (with a nil error) only when the directory is not a git work tree at all.
// A directory that IS a work tree but whose commit, listing or changed content
// cannot be read answers errSubmoduleUnknown, so the whole snapshot becomes
// UNKNOWN rather than a constant two different trees could compare equal.
func submoduleIdentity(ctx context.Context, dir string, depth int) (string, error) {
	if depth >= sourceSnapshotSubmoduleDepth {
		return "", errSubmoduleUnknown
	}
	head, err := gitCapture(ctx, dir, "rev-parse", "HEAD")
	head = strings.TrimSpace(head)
	if err != nil || head == "" {
		return "", submoduleUnreadable(dir)
	}
	status, err := gitCapture(ctx, dir, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--ignore-submodules=none")
	if err != nil {
		return "", errSubmoduleUnknown
	}
	if len(status) == 0 {
		return "submodule:" + head, nil
	}
	overlay, err := submoduleOverlay(ctx, status, dir, depth)
	if err != nil {
		return "", err
	}
	return "submodule:" + head + ":" + overlay[:16], nil
}

// submoduleUnreadable distinguishes a directory that is not a work tree at all
// from one that is a work tree whose HEAD could not be read. The latter is
// UNKNOWN; the former keeps the honest constant kind.
func submoduleUnreadable(dir string) error {
	if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
		return errSubmoduleUnknown
	}
	return nil
}

// submoduleOverlay hashes a submodule's own dirty listing the same bounded,
// content-aware way the top-level overlay is hashed, recursing into nested
// submodules. A nested tree that cannot be read fails the whole identity closed.
func submoduleOverlay(ctx context.Context, raw, dir string, depth int) (string, error) {
	entries, err := snapshotEntries(ctx, raw, dir, depth+1)
	if err != nil {
		return "", err
	}
	return contextualHash(strings.Join(entries, "\n")), nil
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
//
// THE LISTING IS READ THROUGH A BYTE-CAPPED BUFFER. `git status` output used to
// be buffered whole and checked against the bound afterwards, so a pathological
// listing could allocate without limit before the check ever ran; the writer
// below stops the read the moment the bound is crossed.
func gitCapture(ctx context.Context, root string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...)
	var buf boundedBuffer
	buf.limit = sourceSnapshotMaxBytes + 1
	command.Stdout = &buf
	if err := command.Run(); err != nil {
		return string(buf.data), err
	}
	return string(buf.data), nil
}

// boundedBuffer is a write target that stops accepting bytes past its limit,
// returning the snapshot-cap error so the read ends instead of growing. The
// bytes already read are kept so a caller can still measure them.
type boundedBuffer struct {
	data  []byte
	limit int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	remaining := b.limit - len(b.data)
	if remaining <= 0 {
		return 0, errSnapshotCap
	}
	if len(p) > remaining {
		b.data = append(b.data, p[:remaining]...)
		return remaining, errSnapshotCap
	}
	b.data = append(b.data, p...)
	return len(p), nil
}
