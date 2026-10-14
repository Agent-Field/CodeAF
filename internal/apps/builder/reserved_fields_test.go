package builder

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

func TestAReservedFieldIDIsRenamedEverywhere(t *testing.T) {
	files := spec.Files{
		spec.AppFile: []byte(`{"records":{"shoot":{"title":"type","fields":{"type":{"type":"enum","values":["Wedding"]},"due":{"type":"date"},
			"rush":{"type":"bool","computed":"type == \"Wedding\" && due < now"}}}}}`),
		"screens/shoots.json": []byte(`{"screen":"shoots","body":{"part":"RecordList","label":"Shoot type: all","bind":{"records":"shoot","query":"type:Wedding sort:type"},
			"props":{"columns":["type","due"],"groupBy":"type"},
			"on":{"click":[{"ask":{"type":"enum"},"records.create":{"type":"shoot","set":{"type":"$input.type"}}}]}}}`),
	}
	changed := renameReservedFields(files)
	if len(changed) != 2 {
		t.Fatalf("changed = %v, want app.json and the screen", changed)
	}
	app, screen := string(files[spec.AppFile]), string(files["screens/shoots.json"])
	for _, want := range []string{`"item_type": {`, `"title": "item_type"`, `item_type == \"Wedding\" && due < now`, `"label": "Type"`, `"type": "enum"`} {
		if !strings.Contains(app, want) {
			t.Errorf("app.json lacks %s:\n%s", want, app)
		}
	}
	for _, want := range []string{`"query": "item_type:Wedding sort:item_type"`, `"groupBy": "item_type"`, `"item_type": "$input.item_type"`, `"item_type": "enum"`, `"type": "shoot"`, `"label": "Shoot type: all"`, `"records": "shoot"`} {
		if !strings.Contains(screen, want) {
			t.Errorf("screen lacks %s:\n%s", want, screen)
		}
	}
	if again := renameReservedFields(files); len(again) != 0 {
		t.Errorf("a second pass changed %v", again)
	}
	if reservedByGrammar("due") {
		t.Error("an ordinary field id was called reserved")
	}
}
