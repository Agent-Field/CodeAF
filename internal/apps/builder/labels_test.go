package builder

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

func TestHumanLabel(t *testing.T) {
	tests := []struct{ label, id, want string }{
		{"", "publish_on", "Publish on"},
		{"publish_on", "publish_on", "Publish on"},
		{"stage", "stage", "Stage"},
		{"due-date", "due", "Due date"},
		{"Go-live date", "golive", "Go-live date"},
		{"Publish date", "publish_on", "Publish date"},
	}
	for _, tt := range tests {
		if got := humanLabel(tt.label, tt.id); got != tt.want {
			t.Errorf("humanLabel(%q, %q) = %q, want %q", tt.label, tt.id, got, tt.want)
		}
	}
}

func TestPolishLabelsWritesLabelsAndAIMarks(t *testing.T) {
	files := spec.Files{
		spec.AppFile: []byte(`{"records":{"post":{"title":"name","fields":{
			"name":{"type":"text","label":"Post name"},
			"publish_on":{"type":"date"},
			"summary":{"type":"longtext","label":"Summary","ai":"Summarise"}}}},
			"commands":[{"id":"rank_posts","label":"rank_posts","do":[{"ai.sort":{}}]}]}`),
		"screens/calendar.json": []byte(`{"screen":"calendar","title":"calendar","body":{"part":"RecordList",
			"props":{"views":["List","Calendar by publish_on"]},
			"children":[{"part":"Button","label":"Draft posts","on":{"click":[{"agent.ask":{}}]}}]}}`),
	}
	changed := polishLabels(files)
	if len(changed) != 2 {
		t.Fatalf("changed = %v", changed)
	}
	app := string(files[spec.AppFile])
	for _, want := range []string{`"label": "Post"`, `"label": "Post name"`, `"label": "Publish on"`, `"label": "Summary · AI"`, `"label": "Rank posts · AI"`} {
		if !strings.Contains(app, want) {
			t.Errorf("app.json lacks %s:\n%s", want, app)
		}
	}
	screen := string(files["screens/calendar.json"])
	for _, want := range []string{`"title": "Calendar"`, `"Calendar by publish on"`, `"label": "Draft posts · AI"`} {
		if !strings.Contains(screen, want) {
			t.Errorf("screen lacks %s:\n%s", want, screen)
		}
	}
	var check map[string]any
	if json.Unmarshal(files[spec.AppFile], &check) != nil {
		t.Fatal("app.json no longer JSON")
	}
	if again := polishLabels(files); len(again) != 0 {
		t.Errorf("second pass changed %v; polish must be idempotent", again)
	}
}
