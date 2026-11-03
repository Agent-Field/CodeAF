package cellstore

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/cell"
)

// SavedState certifies the saved head against current workspace and chat files.
// The native plan supplies the transfer scope, including metadata under .cell.
// File identities and directory membership fence later edits without another
// capture or content read at the final ownership boundary.
type SavedState struct {
	Head    string
	Changes []string
	files   map[string]fs.FileInfo
	members map[string]string
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
	raw, err := e.do(ctx, c, e.cellDir(c), capturePlanOp{})
	if err != nil {
		return nil, err
	}
	var candidates capturePlan
	if err := json.Unmarshal(raw, &candidates); err != nil || candidates.Paths == nil || !candidates.Full {
		return nil, fmt.Errorf("save check: engine did not return a complete file scope")
	}
	s := &SavedState{Head: head.Turn.ID, files: map[string]fs.FileInfo{}, members: map[string]string{}}
	tree, state := e.tree(c), stateDir(c)
	for _, physical := range append(candidates.Paths, tree, state) {
		if !filepath.IsAbs(physical) {
			physical = filepath.Join(tree, physical)
		}
		for {
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
	raw, err = e.plan(ctx, c, head.Turn.ID)
	if err != nil {
		return nil, err
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
		return nil, fmt.Errorf("save check: files changed during comparison: %s", changed)
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
	transport := Spawn{Binary: e.Binary, Run: e.Run}
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
		return ctx.Err()
	}); err != nil {
		return nil, err
	}
	var changes []string
	for rel := range paths {
		name := path.Join(cell.StateDir, rel)
		if releaseBookkeeping(name) {
			continue
		}
		current, err := savedFile(ctx, filepath.Join(stateDir(c), filepath.FromSlash(rel)))
		if err != nil {
			return nil, err
		}
		saved, err := savedFile(ctx, filepath.Join(base, filepath.FromSlash(rel)))
		if err != nil {
			return nil, err
		}
		if current != saved {
			changes = append(changes, name)
		}
	}
	return changes, nil
}

func savedFile(ctx context.Context, physical string) (string, error) {
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
	info, err := os.Lstat(physical)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	s.files[physical] = info
	if info != nil && info.IsDir() {
		names, err := directoryNames(physical)
		if err != nil {
			return err
		}
		s.members[physical] = names
	}
	return nil
}

func directoryNames(physical string) (string, error) {
	entries, err := os.ReadDir(physical)
	if err != nil {
		return "", err
	}
	var names strings.Builder
	for _, entry := range entries {
		fmt.Fprintf(&names, "%s\x00%d\x00", entry.Name(), entry.Type())
	}
	return names.String(), nil
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
			if names, err := directoryNames(physical); err != nil || names != s.members[physical] {
				return physical
			}
		}
	}
	if ctx.Err() != nil {
		return ctx.Err().Error()
	}
	return ""
}
