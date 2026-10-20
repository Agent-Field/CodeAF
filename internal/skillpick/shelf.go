// Package skillpick holds the shelf ordering shared by the terminal and app.
package skillpick

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/Agent-Field/codeaf/internal/skills"
)

// ListLimit is the terminal's active-shelf scan bound.
const ListLimit = 50

// NoShelfWarning explains why a discovered folder cannot be used here.
const NoShelfWarning = "this conversation has no skill shelf, so this cannot be attached"

// Entry is an active shelf entry, before disk discovery fills its origin.
type Entry struct{ Name, Description, Artifact string }

// Row preserves both the terminal's scope word and the original folder.
type Row struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Source      string `json:"source"`
	On          bool   `json:"on"`
	From        string `json:"-"`
	Warning     string `json:"-"`
}

// Rows merges by name, with attachments first in their conflict order. Shelf
// entries own the description; discovery supplies the original root when known.
func Rows(attached []string, shelf []Entry, disk []skills.Skill, readable bool) []Row {
	available := make(map[string]Row)
	var rest []Row
	for _, entry := range shelf {
		key := strings.ToLower(entry.Name)
		if key == "" {
			continue
		}
		if _, exists := available[key]; exists {
			continue
		}
		available[key] = Row{Name: entry.Name, Description: entry.Description, Source: source(entry.Artifact), From: "shelf"}
	}
	for _, skill := range disk {
		if skill.Shadowed || skill.Name == "" {
			continue
		}
		key := strings.ToLower(skill.Name)
		if row, exists := available[key]; exists {
			row.Source = skill.Root
			available[key] = row
			continue
		}
		warning := skill.Warning
		if !readable {
			warning = strings.Trim(strings.Join([]string{warning, NoShelfWarning}, " · "), " ·")
		}
		available[key] = Row{Name: skill.Name, Description: skill.Description, Source: skill.Root, From: skill.Scope, Warning: warning}
	}
	rows := make([]Row, 0, len(available)+len(attached))
	for _, name := range attached {
		key := strings.ToLower(name)
		row := available[key]
		row.Name, row.On = name, true
		if row.Source == "" {
			row.Source = "shelf"
		}
		rows = append(rows, row)
		delete(available, key)
	}
	for _, row := range available {
		rest = append(rest, row)
	}
	sort.SliceStable(rest, func(i, j int) bool {
		if (rest[i].From == skills.ScopeProject) != (rest[j].From == skills.ScopeProject) {
			return rest[i].From == skills.ScopeProject
		}
		return rest[i].Name < rest[j].Name
	})
	return append(rows, rest...)
}

// source names the artifact's parent folder without exposing its login path.
func source(artifact string) string {
	if strings.TrimSpace(artifact) == "" {
		return "shelf"
	}
	parent := filepath.ToSlash(filepath.Dir(artifact))
	for _, marker := range []string{".codeaf/", ".agents/", ".claude/", ".codex/", ".cursor/", ".gemini/", ".opencode/", ".goose/", ".config/"} {
		if at := strings.Index(parent, marker); at >= 0 {
			return parent[at:]
		}
	}
	return parent
}
