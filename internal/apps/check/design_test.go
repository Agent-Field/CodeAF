package check

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

func designBundle(t *testing.T, screen string) *spec.Bundle {
	t.Helper()
	var s spec.Screen
	if err := json.Unmarshal([]byte(screen), &s); err != nil {
		t.Fatal(err)
	}
	return &spec.Bundle{App: &spec.App{Commands: []spec.Command{{ID: "c", Label: "What publishes this month · AI"}}},
		Screens: map[string]*spec.Screen{"home": &s}}
}

func TestDesignWarnings(t *testing.T) {
	b := designBundle(t, `{"screen":"home","title":"Home","body":{"part":"Stack","children":[
		{"part":"Button","label":"Add post","props":{"kind":"primary"}},
		{"part":"Button","label":"Should we post?","props":{"kind":"primary"}},
		{"part":"RecordList","bind":{"records":"post"},"props":{"query":"status:todo"}},
		{"part":"Text","props":{"text":"Lorem ipsum TODO"}},
		{"part":"RecordList","bind":{"records":"post"},"slots":{"empty":{"part":"Empty","label":"No posts yet"}}}]}}`)
	got := map[string]int{}
	var all []string
	for _, issue := range DesignWarnings(b) {
		got[issue.Rule]++
		all = append(all, issue.String()+" | "+issue.FixHint)
	}
	want := map[string]int{"design/primary": 1, "design/empty": 1, "design/placeholder": 1, "design/action-label": 2}
	for rule, n := range want {
		if got[rule] != n {
			t.Errorf("%s: %d findings, want %d\n%s", rule, got[rule], n, strings.Join(all, "\n"))
		}
	}
	if len(got) != len(want) {
		t.Errorf("unexpected rules: %v", got)
	}
}
