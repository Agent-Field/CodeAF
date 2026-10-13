package svc

import "testing"

func TestParseDraftApp(t *testing.T) {
	for _, tt := range []struct {
		app, id string
		draft   bool
	}{
		{"crm", "crm", false}, {"crm@draft", "crm", true},
		{"crm@draft-extra", "crm@draft-extra", false}, {"", "", false},
		{"@draft", "", true}, {"crm@draft@draft", "crm@draft", true},
	} {
		t.Run(tt.app, func(t *testing.T) {
			id, draft := ParseDraftApp(tt.app)
			if id != tt.id || draft != tt.draft {
				t.Fatalf("got (%q, %v), want (%q, %v)", id, draft, tt.id, tt.draft)
			}
		})
	}
}
