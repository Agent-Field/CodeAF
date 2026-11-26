package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
)

func TestGoogleDailyBriefQueriesTheFakeBinding(t *testing.T) {
	root := t.TempDir()
	b := &appBackend{profileDir: root}
	b.data.home = root
	defer b.closeData()
	var out bytes.Buffer
	if err := appConnectionsCommand(b, []string{"daily-brief", "--fixture"}, strings.NewReader(""), &out); err != nil {
		t.Fatal(err)
	}
	app := strings.TrimSpace(out.String())
	for _, tc := range []struct{ typ, field, value string }{
		{"message", "subject", "Planning"},
		{"meeting", "title", "Thursday launch planning"},
	} {
		page, err := b.DataQuery(appwire.DataQueryParams{
			DataScope: appwire.DataScope{App: app}, Type: tc.typ, Select: []string{tc.field},
		})
		if err != nil || len(page.Rows) != 1 || page.Rows[0][tc.field] != tc.value || page.Source == nil ||
			page.Source.Residency != "live" {
			t.Fatalf("%s: %+v, %v", tc.typ, page, err)
		}
	}
}
