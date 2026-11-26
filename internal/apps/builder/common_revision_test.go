package builder

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/schema"
)

func TestMeetingRevisionKeepsCommonBindings(t *testing.T) {
	fixture, err := os.ReadFile("../data/testdata/meeting-revision/app.json")
	if err != nil {
		t.Fatal(err)
	}
	author := authoring{}
	files := map[string]string{schema.AppFile: string(fixture)}
	types := schema.TypeSet{"meeting": {Common: "event"}, "transcript": {Common: "document"}}
	for _, name := range []string{"meeting", "transcript"} {
		args := json.RawMessage(`{"name":"` + name + `","definition":{"title":"title","uses":true,"fields":{}}}`)
		files, err = author.edit(t.Context(), "add_type", files, types, args)
		if err != nil {
			t.Fatal(err)
		}
	}
	app, err := schema.ParseApp([]byte(files[schema.AppFile]))
	if err != nil || app.Records["meeting"].Common != "event" || app.Records["transcript"].Common != "document" {
		t.Fatalf("revision lost canonical bindings: %v (%v)", app.Records, err)
	}
}
