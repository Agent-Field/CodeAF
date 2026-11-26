package apppackage_test

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/apppackage"
)

func TestPackRefusesAThinFirstPartyExample(t *testing.T) {
	files := map[string]string{"app.json": `{"id":"thin","name":"Thin","version":1,"kit":0,"records":{}}`}
	_, err := apppackage.Pack(files, nil, apppackage.PackOptions{Publisher: "codeaf examples"})
	if err == nil || !strings.Contains(err.Error(), "depth_records") || !strings.Contains(err.Error(), "depth_write_back") {
		t.Fatalf("expected the publisher depth findings, got %v", err)
	}
}
