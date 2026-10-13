package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

var idPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,47}$`)

func validID(id string) bool { return idPattern.MatchString(id) }

func slug(name string) string {
	var out strings.Builder
	for _, c := range strings.ToLower(name) {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
			out.WriteRune(c)
		default:
			if out.Len() > 0 && !strings.HasSuffix(out.String(), "-") {
				out.WriteByte('-')
			}
		}
	}
	id := strings.Trim(out.String(), "-")
	if id == "" {
		id = "app"
	}
	if id[0] < 'a' || id[0] > 'z' {
		id = "app-" + id
	}
	return strings.TrimRight(id[:min(len(id), 48)], "-")
}

func (s *Service) allocate(base string) (string, error) {
	for n := 1; ; n++ {
		id := base
		if n > 1 {
			suffix := "-" + strconv.Itoa(n)
			id = strings.TrimRight(base[:min(len(base), 48-len(suffix))], "-") + suffix
		}
		err := os.Mkdir(s.appDir(id), 0700)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		return id, err
	}
}

func (s *Service) load(app string) (metadata, error) {
	var m metadata
	// Registry ids cannot redirect definition writes into another app's folder.
	info, err := os.Lstat(s.appDir(app))
	if err != nil {
		return m, err
	}
	if !info.IsDir() {
		return m, bridge.Errorf(bridge.CodeInvalid, "app folder must be a directory")
	}
	name := filepath.Join(s.appDir(app), metadataFile)
	info, err = os.Lstat(name)
	if err != nil {
		return m, err
	}
	if !info.Mode().IsRegular() {
		return m, bridge.Errorf(bridge.CodeInvalid, "app metadata must be a regular file")
	}
	data, err := os.ReadFile(name)
	if err != nil {
		return m, err
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return m, fmt.Errorf("app %s metadata: %w", app, err)
	}
	if m.ID != app || m.Maker != svc.Owner || m.Space != "personal" {
		return m, bridge.Errorf(bridge.CodeInvalid, "app metadata does not match its personal folder")
	}
	return m, nil
}

func (s *Service) save(m *metadata) error {
	m.UpdatedAt = s.timestamp()
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(s.appDir(m.ID), metadataFile), append(data, '\n'))
}

func atomicWrite(name string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(name), ".state-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(data); err != nil {
		return errors.Join(err, f.Close())
	}
	if err := f.Sync(); err != nil {
		return errors.Join(err, f.Close())
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), name)
}

func allowedPath(name string) bool {
	if name == spec.AppFile || name == spec.PlanFile || name == spec.AgentFile {
		return true
	}
	dir, file := path.Split(name)
	ext := path.Ext(file)
	id := strings.TrimSuffix(file, ext)
	return validID(id) && ((dir == "screens/" && ext == ".json") || (dir == "runs/" && ext == ".yaml"))
}

func validatePaths(files spec.Files) error {
	for name := range files {
		if !allowedPath(name) {
			return bridge.Errorf(bridge.CodeInvalid, "not an app definition path: "+name)
		}
	}
	return nil
}

func readFiles(dir string) (spec.Files, error) {
	files := spec.Files{}
	err := filepath.WalkDir(dir, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, name)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return bridge.Errorf(bridge.CodeInvalid, "app definition cannot contain symlinks")
		}
		rel = filepath.ToSlash(rel)
		if !allowedPath(rel) {
			return nil
		}
		data, err := os.ReadFile(name)
		files[rel] = data
		return err
	})
	return files, err
}

func replaceFiles(dir string, files spec.Files) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name() != ".git" {
			if err := os.RemoveAll(filepath.Join(dir, entry.Name())); err != nil {
				return err
			}
		}
	}
	for _, name := range files.Paths() {
		full := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
			return err
		}
		if err := os.WriteFile(full, files[name], 0600); err != nil {
			return err
		}
	}
	return nil
}

// Build the candidate beside the live directory so a failed file write or git
// operation leaves both the current definition and its metadata intact.
func (s *Service) changeDefinition(ctx context.Context, m *metadata, change func(string) error) error {
	dir := s.definitionDir(m.ID)
	stage, err := os.MkdirTemp(s.appDir(m.ID), ".definition-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	if err := copyDefinition(stage, dir); err != nil {
		return err
	}
	if err := change(stage); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	m.UpdatedAt = s.timestamp()
	backup := stage + "-previous"
	if err := os.Rename(dir, backup); err != nil {
		return err
	}
	if err := os.Rename(stage, dir); err != nil {
		return errors.Join(err, os.Rename(backup, dir))
	}
	if err := s.save(m); err != nil {
		return errors.Join(err, os.Rename(dir, stage), os.Rename(backup, dir))
	}
	return os.RemoveAll(backup)
}

func copyDefinition(destination, source string) error {
	// A symlink in .git could make go-git update a repository outside this app.
	err := filepath.WalkDir(source, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return bridge.Errorf(bridge.CodeInvalid, "app definition cannot contain symlinks")
		}
		return nil
	})
	if err != nil {
		return err
	}
	return os.CopyFS(destination, os.DirFS(source))
}
