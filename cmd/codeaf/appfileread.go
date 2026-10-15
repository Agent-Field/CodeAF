package main

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/appwire"
)

// ReadFile answers files.read: what the path is now, resolved canonically, and
// one bounded page of it when it is UTF-8 text no larger than
// appwire.FileReadMax. Reading follows the session's own law (taskoutside.go):
// anything on the machine may be read, so a file outside the workspace is
// answered and marked outside, never refused and never writable. Nothing is
// opened that is not a regular file, and nothing over the bound is read at all.
func (b *appBackend) ReadFile(agent, path string, offset int64, limit int) (appwire.FileRead, error) {
	if offset < 0 || limit < 0 {
		return appwire.FileRead{}, appwire.Errorf(appwire.CodeBadParams, "files.read needs a non-negative offset and page limit")
	}
	limit = min(limit, appwire.FilePageMax)
	a, err := b.find(agent)
	if err != nil {
		return appwire.FileRead{}, err
	}
	var workspace string
	b.locked(func() { workspace = a.view.Workspace })
	full := path
	if !filepath.IsAbs(full) {
		if workspace == "" {
			return appwire.FileRead{}, appwire.Errorf(appwire.CodeBadParams, "this agent has no workspace, so %s needs a full path", path)
		}
		full = filepath.Join(workspace, full)
	}
	full = filepath.Clean(full)
	out := appwire.FileRead{Path: path, Resolved: full, Offset: offset}
	changed := b.changedInChat(a)
	counts := func(keys ...string) {
		for _, k := range keys {
			if c, ok := changed[k]; ok {
				add, del := c.Add, c.Del
				out.Add, out.Del = &add, &del
				return
			}
		}
	}
	counts(full, path)
	root := appCanonical(workspace)

	link, err := os.Lstat(full)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			out.Kind = appwire.ReadMissing
		} else {
			out.Kind = appwire.ReadUnreadable
		}
		out.Outside = !appWithin(root, appCanonical(full))
		return out, nil
	}
	out.Symlink = link.Mode()&fs.ModeSymlink != 0
	resolved, err := filepath.EvalSymlinks(full)
	if err != nil {
		// A link to nothing: what it named is gone.
		out.Kind = appwire.ReadMissing
		out.Outside = !appWithin(root, appCanonical(full))
		return out, nil
	}
	out.Resolved = resolved
	out.Outside = !appWithin(root, resolved)
	counts(resolved)
	info, err := os.Stat(resolved)
	if err != nil {
		out.Kind = appwire.ReadUnreadable
		return out, nil
	}
	out.Modified = info.ModTime().UTC().Format(time.RFC3339Nano)
	switch {
	case info.IsDir():
		out.Kind = appwire.ReadDirectory
		return out, nil
	case !info.Mode().IsRegular():
		// A fifo, socket or device: opening it could block or never end.
		out.Kind = appwire.ReadUnreadable
		return out, nil
	}
	size := info.Size()
	out.Size = &size
	if size > appwire.FileReadMax {
		out.Kind = appwire.ReadTooLarge
		return out, nil
	}
	file, err := os.Open(resolved)
	if err != nil {
		out.Kind = appwire.ReadUnreadable
		return out, nil
	}
	data, err := io.ReadAll(io.LimitReader(file, appwire.FileReadMax+1))
	_ = file.Close()
	if err != nil {
		out.Kind = appwire.ReadUnreadable
		return out, nil
	}
	if len(data) > appwire.FileReadMax {
		// It grew past the bound after it was measured.
		out.Kind = appwire.ReadTooLarge
		return out, nil
	}
	size = int64(len(data))
	out.Size = &size
	if bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
		out.Kind = appwire.ReadBinary
		return out, nil
	}
	out.Kind = appwire.ReadText
	lines := bytes.Count(data, []byte{'\n'})
	if len(data) > 0 && data[len(data)-1] != '\n' {
		lines++
	}
	out.Lines = &lines
	if limit <= 0 {
		return out, nil
	}
	if offset > size || (offset < size && !utf8.RuneStart(data[offset])) {
		return appwire.FileRead{}, appwire.Errorf(appwire.CodeBadParams, "offset %d is not the start of a page of %s", offset, path)
	}
	end := min(size, offset+int64(limit))
	if end < size {
		// Cut after the last whole line, or else at a character boundary.
		if nl := bytes.LastIndexByte(data[offset:end], '\n'); nl >= 0 {
			end = offset + int64(nl) + 1
		} else {
			for end > offset && !utf8.RuneStart(data[end]) {
				end--
			}
		}
		if end == offset {
			return appwire.FileRead{}, appwire.Errorf(appwire.CodeBadParams, "page limit is too small for the next UTF-8 character")
		}
		next := end
		out.Next = &next
	}
	out.Text = string(data[offset:end])
	return out, nil
}

// appCanonical is path with every link followed, or as cleaned when it (or a
// parent) is not there to follow.
func appCanonical(path string) string {
	if path == "" {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	// Follow the deepest parent that exists, so /tmp/gone under a linked /tmp
	// still compares with a linked workspace.
	dir, rest := filepath.Clean(path), ""
	for dir != filepath.Dir(dir) {
		rest = filepath.Join(filepath.Base(dir), rest)
		dir = filepath.Dir(dir)
		if resolved, err := filepath.EvalSymlinks(dir); err == nil {
			return filepath.Join(resolved, rest)
		}
	}
	return filepath.Clean(path)
}

// appWithin says whether path lies inside root (or is root). An empty root has
// nothing inside it.
func appWithin(root, path string) bool {
	if root == "" {
		return false
	}
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

var _ appwire.FileReader = (*appBackend)(nil)
