package main

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

func TestSeparateJobTitleIsLiteral(t *testing.T) {
	s := newLocalSetup(t)
	st := s.start()
	for _, tc := range []struct{ title, text string }{
		{"  c43: add the word hello to README  ", "Append one line with the word hello to README.md"},
		{"security two review rounds cheaper plan first ship it", "Literal details"},
		{"Fix auto-save $5", "  security, two review rounds, cheaper; ship it; plan first\nKeep  these words.\n"},
	} {
		t.Run(tc.title, func(t *testing.T) {
			var made appwire.FactoryJobActed
			s.must("factory.job_create", map[string]any{"factory": st.Factory, "repo": "acme/api", "title": tc.title, "text": tc.text}, &made)
			var got api.Job
			s.must("factory.job", map[string]any{"factory": st.Factory, "job": made.Job}, &got)
			want := strings.TrimSpace(tc.title)
			if got.Title != want {
				t.Errorf("title = %q; want %q", got.Title, want)
			}
			if got.Ask == nil || got.Ask.Text != tc.text {
				t.Errorf("ask = %+v; want text %q", got.Ask, tc.text)
			}
			if got.Budget != 0 {
				t.Errorf("budget = %v; want 0", got.Budget)
			}
			approvals := 0
			for _, step := range got.Steps {
				if step.Kind == "gate" {
					approvals++
				}
			}
			if approvals == 0 {
				t.Error("job lost its approve steps")
			}
		})
	}
}
