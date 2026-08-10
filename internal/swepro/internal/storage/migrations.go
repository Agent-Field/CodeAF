// Flat-file migration chain — port of src/storage/storage.ts:88-218
// (swe-pro 3b25a1a).
package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func (s *Store) migration1() error {
	projectRoot := filepath.Clean(filepath.Join(s.Dir, "..", "project"))
	info, err := os.Stat(projectRoot)
	if err != nil || !info.IsDir() {
		return nil
	}
	projectDirs, err := filepath.Glob(filepath.Join(projectRoot, "*"))
	if err != nil {
		return err
	}
	for _, full := range projectDirs {
		info, err := os.Stat(full)
		if err != nil || !info.IsDir() {
			continue
		}
		projectID := filepath.Base(full)
		worktree := "/"
		if projectID == "global" {
			continue
		}
		messageFiles, _ := filepath.Glob(filepath.Join(full, "storage", "session", "message", "*", "*.json"))
		for _, messageFile := range messageFiles {
			value, err := readOrderedFile(messageFile)
			if err != nil {
				return err
			}
			root, ok := rootPath(value)
			if !ok || root == "" {
				continue
			}
			worktree = root
			break
		}
		// The TS initializes worktree to "/" and then tests `if (!worktree)`;
		// therefore a project with no usable message probes the filesystem root.
		if info, err := os.Stat(worktree); err != nil || !info.IsDir() {
			continue
		}
		if s.git == nil {
			continue
		}
		out, err := s.git.Run([]string{"rev-list", "--max-parents=0", "--all"}, worktree)
		if err != nil {
			return err
		}
		lines := []string{}
		for _, line := range strings.Split(string(out), "\n") {
			if line == "" {
				continue
			}
			lines = append(lines, strings.TrimSpace(line))
		}
		sort.Strings(lines)
		if len(lines) == 0 || lines[0] == "" {
			continue
		}
		id := lines[0]
		projectID = id
		project := NewObject()
		project.Set("id", id)
		project.Set("vcs", "git")
		project.Set("worktree", worktree)
		times := NewObject()
		times.Set("created", float64(s.now().UnixMilli()))
		times.Set("initialized", float64(s.now().UnixMilli()))
		project.Set("time", times)
		if err := writeJSON(filepath.Join(s.Dir, "project", projectID+".json"), project); err != nil {
			return err
		}

		sessionFiles, _ := filepath.Glob(filepath.Join(full, "storage", "session", "info", "*.json"))
		for _, sessionFile := range sessionFiles {
			session, err := readOrderedFile(sessionFile)
			if err != nil {
				return err
			}
			if err := writeJSON(filepath.Join(s.Dir, "session", projectID, filepath.Base(sessionFile)), session); err != nil {
				return err
			}
			sessionObject, ok := session.(*Object)
			if !ok {
				continue
			}
			sessionID, ok := objectString(sessionObject, "id")
			if !ok {
				continue
			}
			messageFiles, _ := filepath.Glob(filepath.Join(full, "storage", "session", "message", sessionID, "*.json"))
			for _, messageFile := range messageFiles {
				message, err := readOrderedFile(messageFile)
				if err != nil {
					return err
				}
				if err := writeJSON(filepath.Join(s.Dir, "message", sessionID, filepath.Base(messageFile)), message); err != nil {
					return err
				}
				messageObject, ok := message.(*Object)
				if !ok {
					continue
				}
				messageID, ok := objectString(messageObject, "id")
				if !ok {
					continue
				}
				partFiles, _ := filepath.Glob(filepath.Join(full, "storage", "session", "part", sessionID, messageID, "*.json"))
				for _, partFile := range partFiles {
					part, err := readOrderedFile(partFile)
					if err != nil {
						return err
					}
					if err := writeJSON(filepath.Join(s.Dir, "part", messageID, filepath.Base(partFile)), part); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

func (s *Store) migration2() error {
	files, err := filepath.Glob(filepath.Join(s.Dir, "session", "*", "*.json"))
	if err != nil {
		return err
	}
	for _, file := range files {
		value, err := readOrderedFile(file)
		if err != nil {
			return err
		}
		session, ok := value.(*Object)
		if !ok {
			continue
		}
		id, idOK := objectString(session, "id")
		projectID, projectOK := objectString(session, "projectID")
		summary, summaryOK := objectObject(session, "summary")
		diffs, diffsOK := objectArray(summary, "diffs")
		if !idOK || !projectOK || !summaryOK || !diffsOK || !validDiffs(diffs) {
			continue
		}
		if err := writeJSON(filepath.Join(s.Dir, "session_diff", id+".json"), diffs); err != nil {
			return err
		}
		var additions float64
		var deletions float64
		for _, value := range diffs {
			diff := value.(*Object)
			a, _ := diff.Get("additions")
			d, _ := diff.Get("deletions")
			additions += a.(float64)
			deletions += d.(float64)
		}
		nextSummary := NewObject()
		nextSummary.Set("additions", additions)
		nextSummary.Set("deletions", deletions)
		session.Set("summary", nextSummary)
		if err := writeJSON(filepath.Join(s.Dir, "session", projectID, id+".json"), session); err != nil {
			return err
		}
	}
	return nil
}

func rootPath(value any) (string, bool) {
	root, ok := value.(*Object)
	if !ok {
		return "", false
	}
	path, ok := objectObject(root, "path")
	if !ok {
		return "", false
	}
	return objectString(path, "root")
}

func validDiffs(diffs []any) bool {
	for _, value := range diffs {
		diff, ok := value.(*Object)
		if !ok {
			return false
		}
		for _, key := range []string{"additions", "deletions"} {
			value, ok := diff.Get(key)
			n, valid := value.(float64)
			if !ok || !valid || n < 0 || n != float64(int64(n)) {
				return false
			}
		}
	}
	return true
}

func readOrderedFile(path string) (any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	value, err := parseJSON(data)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return value, nil
}
