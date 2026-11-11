package skillpick

import (
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/skills"
)

func TestRowsPreserveAttachmentOrderAndOrigins(t *testing.T) {
	rows := Rows([]string{"USER", "missing"}, []Entry{{Name: "user", Description: "shelf description", Artifact: "/home/person/.codex/skills/user"}}, []skills.Skill{
		{Name: "user", Description: "disk description", Root: ".codex/skills", Scope: skills.ScopeUser},
		{Name: "project", Description: "project skill", Root: ".claude/skills", Scope: skills.ScopeProject},
		{Name: "shadow", Shadowed: true},
	}, true)
	names := []string{}
	for _, row := range rows {
		names = append(names, row.Name)
	}
	if !reflect.DeepEqual(names, []string{"USER", "missing", "project"}) {
		t.Fatalf("order: %v", names)
	}
	if !rows[0].On || rows[0].Source != ".codex/skills" || rows[0].Description != "shelf description" {
		t.Fatalf("attached metadata: %+v", rows[0])
	}
	if rows[2].On || rows[2].Source != ".claude/skills" {
		t.Fatalf("discovery: %+v", rows[2])
	}
}

func TestRowsEmptyAndShelfOnly(t *testing.T) {
	if rows := Rows(nil, nil, nil, true); rows == nil || len(rows) != 0 {
		t.Fatalf("empty: %#v", rows)
	}
	rows := Rows(nil, []Entry{{Name: "b", Artifact: "/tmp/custom/b"}, {Name: "a", Artifact: "/home/p/.codeaf/skills/a"}}, nil, true)
	if rows[0].Name != "a" || rows[0].Source != ".codeaf/skills" || rows[1].Source != "/tmp/custom" {
		t.Fatalf("origins: %+v", rows)
	}
}

func TestMergedProjectShelfKeepsProjectPriority(t *testing.T) {
	rows := Rows(nil, []Entry{{Name: "z-project", Description: "shelf words"}}, []skills.Skill{
		{Name: "z-project", Scope: skills.ScopeProject, Root: ".claude/skills"},
		{Name: "a-user", Scope: skills.ScopeUser, Root: ".codex/skills"},
	}, true)
	if len(rows) != 2 || rows[0].Name != "z-project" || rows[0].From != skills.ScopeProject || rows[0].Description != "shelf words" {
		t.Fatalf("project first with shelf description: %+v", rows)
	}
}
