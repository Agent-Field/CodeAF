// Package workspacepath keeps the spellings that provably name one folder.
package workspacepath

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/home"
)

// Canonical is the write spelling. Unresolvable paths keep their old fallback.
func Canonical(path string) string {
	path = filepath.Clean(path)
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return path
}

// Same requires two live citations; absent or broken links prove nothing.
func Same(a, b string) bool {
	if strings.TrimSpace(a) == "" || strings.TrimSpace(b) == "" {
		return false
	}
	a, err := filepath.EvalSymlinks(filepath.Clean(a))
	if err != nil {
		return false
	}
	b, err = filepath.EvalSymlinks(filepath.Clean(b))
	return err == nil && a == b
}

// Aliases returns the canonical write spelling, the incoming spelling, and
// equivalent spellings retained in host/session citations. It never invents an
// alias from a bucket name, follows state-directory links, or changes a record.
func Aliases(workspace string, candidates ...string) []string {
	if strings.TrimSpace(workspace) == "" {
		return nil
	}
	result := []string{Canonical(workspace)}
	seen := map[string]bool{result[0]: true}
	add := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		path = filepath.Clean(path)
		if !seen[path] && Same(workspace, path) {
			result = append(result, path)
			seen[path] = true
		}
	}
	add(workspace)
	for _, path := range candidates {
		add(path)
	}
	hosts, _ := os.ReadDir(home.Join("v3", "hosts"))
	for _, entry := range hosts {
		if !entry.IsDir() {
			continue
		}
		raw, err := os.ReadFile(home.Join("v3", "hosts", entry.Name(), "workspace"))
		if err == nil {
			add(string(raw))
		}
	}
	projects, _ := os.ReadDir(home.Join("v3", "projects"))
	for _, project := range projects {
		if !project.IsDir() {
			continue
		}
		bucket := home.Join("v3", "projects", project.Name())
		sessions, _ := os.ReadDir(bucket)
		for _, entry := range sessions {
			if !entry.IsDir() {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(bucket, entry.Name(), "meta.json"))
			if err != nil {
				continue
			}
			var meta struct {
				ID        string `json:"id"`
				Workspace string `json:"workspace"`
				LaunchDir string `json:"launchDir"`
				Owned     bool   `json:"owned"`
			}
			if json.Unmarshal(raw, &meta) != nil || meta.ID == "" {
				continue
			}
			add(meta.Workspace)
			// A borrowed launch can also cite an alias of the root itself.
			// Same rejects a subdirectory or unrelated launch location.
			add(meta.LaunchDir)
		}
	}
	return result
}
