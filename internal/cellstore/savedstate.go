package cellstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Agent-Field/codeaf/internal/cell"
)

// ErrSavedStateChanged is an optimistic comparison that lost to a local write.
// It retains ownership and retries at the quiet cadence rather than backing
// off like a failed relay or engine request.
var ErrSavedStateChanged = errors.New("save check: files changed during comparison")

// Idle certification retains the hold when any proof budget is exceeded.
// These caps apply only to the new idle proof, never to ordinary saves.
const (
	savedStatePaths        = 20_000
	savedStateEntries      = 50_000
	savedStateReplyBytes   = 8 << 20
	savedStateContentBytes = 64 << 20
)

// ErrSavedStateTooLarge refuses partial idle certification and retains the hold.
var ErrSavedStateTooLarge = errors.New("save check: idle comparison budget exceeded")

// SavedState certifies the saved head against current workspace and chat files.
// The native plan supplies the transfer scope, including metadata under .cell.
// File identities and directory membership fence later edits without another
// capture or content read at the final ownership boundary.
type SavedState struct {
	Head    string
	Changes []string
	files   map[string]fs.FileInfo
	members map[string]string
	entries int
}

// SavedState reads and compares outside admission. Extra files are the profile
// carriers that accompany the ordinary snapshot through the vault.
func (e Engine) SavedState(ctx context.Context, c cell.Cell, extra ...string) (*SavedState, error) {
	head, err := Head(c)
	if err != nil || head == nil {
		return nil, err
	}
	if err := e.composeExtra(c); err != nil {
		return nil, err
	}
	// The warm daemon can answer with its incremental metadata scope even
	// without change hints. Certification needs a fresh complete workspace
	// walk; the standalone planner supplies that, while seals keep the daemon.
	scope := e
	if scope.Transport == nil {
		scope.Transport = Spawn{Binary: e.Binary, Run: savedStateRunner(e.Run)}
	}
	raw, err := scope.do(ctx, c, e.cellDir(c), capturePlanOp{})
	if err != nil {
		return nil, err
	}
	if len(raw) > savedStateReplyBytes {
		return nil, ErrSavedStateTooLarge
	}
	var candidates capturePlan
	if err := json.Unmarshal(raw, &candidates); err != nil || candidates.Paths == nil || !candidates.Full {
		return nil, fmt.Errorf("save check: engine did not return a complete file scope")
	}
	if len(candidates.Paths) > savedStatePaths {
		return nil, ErrSavedStateTooLarge
	}
	s := &SavedState{Head: head.Turn.ID, files: map[string]fs.FileInfo{}, members: map[string]string{}}
	tree, state := e.tree(c), stateDir(c)
	for _, physical := range append(candidates.Paths, tree, state) {
		if !filepath.IsAbs(physical) {
			physical = filepath.Join(tree, physical)
		}
		for {
			// The first fingerprint wins; shared ancestors need no more work.
			if _, seen := s.files[physical]; seen {
				break
			}
			if err := s.remember(ctx, physical); err != nil {
				return nil, err
			}
			if physical == tree || physical == state {
				break
			}
			next := filepath.Dir(physical)
			if next == physical {
				return nil, fmt.Errorf("save check: candidate is outside the saved tree")
			}
			physical = next
		}
	}
	for _, physical := range extra {
		if err := s.remember(ctx, physical); err != nil {
			return nil, err
		}
	}
	comparison := e
	comparison.Run = savedStateRunner(e.Run)
	raw, err = comparison.plan(ctx, c, head.Turn.ID)
	if err != nil {
		return nil, err
	}
	if len(raw) > savedStateReplyBytes {
		return nil, ErrSavedStateTooLarge
	}
	var plan struct{ Changes []struct{ Path string } }
	if err := json.Unmarshal(raw, &plan); err != nil {
		return nil, err
	}
	for _, change := range plan.Changes {
		name := path.Clean(change.Path)
		if releaseBookkeeping(name) {
			continue
		}
		s.Changes = append(s.Changes, name)
	}
	stateChanges, err := e.savedChatChanges(ctx, c, head.Turn.ID, candidates.Paths)
	if err != nil {
		return nil, err
	}
	s.Changes = append(s.Changes, stateChanges...)
	if changed := s.changed(ctx); changed != "" {
		return nil, fmt.Errorf("%w: %s", ErrSavedStateChanged, changed)
	}
	return s, nil
}

func releaseBookkeeping(name string) bool {
	return name == cell.StateDir || name == TurnsPath || name == publishedPath || name == adoptedPath || name == cell.CopyOfferedPath || name == cell.CopyReadyPath || name == ReceiptsDir || strings.HasPrefix(name, ReceiptsDir+"/")
}

// Native rewind plans describe workspace edits. Compare the composed chat
// truth explicitly too: a metadata-only edit need not appear in that plan.
func (e Engine) savedChatChanges(ctx context.Context, c cell.Cell, head string, candidates []string) ([]string, error) {
	scratch, err := os.MkdirTemp("", "codeaf-saved-state-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(scratch)
	target := e.target(c)
	target.Tree, target.CellDir, target.Timeline = scratch, "", filepath.Base(scratch)
	transport := Spawn{Binary: e.Binary, Run: savedStateRunner(e.Run)}
	if _, err := transport.Do(ctx, target, indexScratchOp{}); err != nil {
		return nil, err
	}
	defer func() { _, _ = transport.Do(ctx, target, indexScratchOp{forget: true}) }()
	if _, err := transport.Do(ctx, target, restoreOp{Snapshot: head, Paths: []string{cell.StateDir}}); err != nil {
		return nil, err
	}
	paths := map[string]bool{}
	for _, physical := range candidates {
		rel, err := filepath.Rel(stateDir(c), physical)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			paths[filepath.ToSlash(rel)] = true
		}
	}
	base := filepath.Join(scratch, cell.StateDir)
	if err := filepath.WalkDir(base, func(physical string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(base, physical)
		paths[filepath.ToSlash(rel)] = true
		if len(paths) > savedStatePaths {
			return ErrSavedStateTooLarge
		}
		return ctx.Err()
	}); err != nil {
		return nil, err
	}
	var changes []string
	remaining := int64(savedStateContentBytes)
	for rel := range paths {
		name := path.Join(cell.StateDir, rel)
		if releaseBookkeeping(name) {
			continue
		}
		current, err := savedFile(ctx, filepath.Join(stateDir(c), filepath.FromSlash(rel)), &remaining)
		if err != nil {
			return nil, err
		}
		saved, err := savedFile(ctx, filepath.Join(base, filepath.FromSlash(rel)), &remaining)
		if err != nil {
			return nil, err
		}
		if current != saved {
			changes = append(changes, name)
		}
	}
	return changes, nil
}

func savedFile(ctx context.Context, physical string, remaining *int64) (string, error) {
	info, err := os.Lstat(physical)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "directory", nil
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(physical)
		return "link:" + target, err
	}
	if info.Size() > *remaining {
		return "", ErrSavedStateTooLarge
	}
	f, err := os.Open(physical)
	if err != nil {
		return "", err
	}
	defer f.Close()
	hash := sha256.New()
	buf := make([]byte, 64<<10)
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		n, err := f.Read(buf)
		*remaining -= int64(n)
		if *remaining < 0 {
			return "", ErrSavedStateTooLarge
		}
		_, _ = hash.Write(buf[:n])
		if err == io.EOF {
			return fmt.Sprintf("%o:%x", info.Mode()&0111, hash.Sum(nil)), nil
		}
		if err != nil {
			return "", err
		}
	}
}

func (s *SavedState) remember(ctx context.Context, physical string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, seen := s.files[physical]; seen {
		return nil
	}
	if len(s.files) >= savedStatePaths {
		return ErrSavedStateTooLarge
	}
	info, err := os.Lstat(physical)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	s.files[physical] = info
	if info != nil && info.IsDir() {
		names, count, err := directoryNames(physical, savedStateEntries-s.entries)
		if err != nil {
			return err
		}
		s.members[physical] = names
		s.entries += count
	}
	return nil
}

func directoryNames(physical string, remaining int) (string, int, error) {
	dir, err := os.Open(physical)
	if err != nil {
		return "", 0, err
	}
	defer dir.Close()
	entries, err := dir.ReadDir(remaining + 1)
	if err != nil && err != io.EOF {
		return "", 0, err
	}
	if len(entries) > remaining {
		return "", 0, ErrSavedStateTooLarge
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	var names strings.Builder
	for _, entry := range entries {
		fmt.Fprintf(&names, "%s\x00%d\x00", entry.Name(), entry.Type())
	}
	return names.String(), len(entries), nil
}

// Unchanged is a bounded metadata recheck; it never captures, reads file
// contents or contacts the engine/relay. A changed parent catches new names.
func (s *SavedState) Unchanged(ctx context.Context) bool {
	return s.changed(ctx) == ""
}

func (s *SavedState) changed(ctx context.Context) string {
	for physical, before := range s.files {
		if ctx.Err() != nil {
			return ctx.Err().Error()
		}
		now, err := os.Lstat(physical)
		if before == nil && os.IsNotExist(err) {
			continue
		}
		if err != nil || before == nil || before.Mode() != now.Mode() || !os.SameFile(before, now) {
			return physical
		}
		if before.Size() != now.Size() || !before.ModTime().Equal(now.ModTime()) {
			// Native comparisons and atomic writers can create then remove
			// scratch names. Directory times alone do not change saved state.
			if !before.IsDir() {
				return physical
			}
			if names, _, err := directoryNames(physical, savedStateEntries); err != nil || names != s.members[physical] {
				return physical
			}
		}
	}
	if ctx.Err() != nil {
		return ctx.Err().Error()
	}
	return ""
}

// The complete planner runs outside the daemon, with a bounded reply. The
// ordinary engine runner and its error handling remain unchanged.
func savedStateRunner(run Runner) Runner {
	return func(ctx context.Context, dir string, environ []string, argv ...string) ([]byte, error) {
		if run != nil {
			out, err := run(ctx, dir, environ, argv...)
			if len(out) > savedStateReplyBytes {
				return nil, ErrSavedStateTooLarge
			}
			return out, err
		}
		cmd := exec.CommandContext(ctx, argv[0], argv[1:]...) //codeaf:plumbing the idle proof drives the embedded engine
		cmd.Dir, cmd.Env = dir, environ
		out, errb := &savedStateReply{}, &savedStateReply{}
		cmd.Stdout, cmd.Stderr = out, errb
		err := cmd.Run()
		if out.overflow || errb.overflow {
			return nil, ErrSavedStateTooLarge
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w: %s", filepath.Base(argv[0]), err, strings.TrimSpace(errb.buffer.String()))
		}
		return out.buffer.Bytes(), nil
	}
}

type savedStateReply struct {
	buffer   bytes.Buffer
	overflow bool
}

func (b *savedStateReply) Write(p []byte) (int, error) {
	if len(p) > savedStateReplyBytes-b.buffer.Len() {
		b.overflow = true
		return 0, ErrSavedStateTooLarge
	}
	return b.buffer.Write(p)
}
