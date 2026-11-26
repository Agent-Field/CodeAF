package builder

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSetRunRejectsAQueryDefaultForOneRecord(t *testing.T) {
	const run = "run: follow-up\nparams:\n  deal: {type: ref, to: deal, default: \"stage:Lead\"}\nsteps:\n  - id: hold\n    gate: {approval: proof}\n"
	for _, typ := range []string{"ref", "refs"} {
		t.Run(typ, func(t *testing.T) {
			text := strings.Replace(run, "type: ref,", "type: "+typ+",", 1)
			args, err := json.Marshal(fileCommand{ID: "follow-up", Text: text})
			if err != nil {
				t.Fatal(err)
			}
			files, err := (authoring{}).edit(t.Context(), "set_run", map[string]string{}, nil, args)
			if typ == "ref" {
				if err == nil || !strings.Contains(err.Error(), "takes a single record") {
					t.Fatalf("single-record default query returned %v", err)
				}
			} else if err != nil || files["runs/follow-up.yaml"] != text {
				t.Fatalf("collection default query was not kept: %v", err)
			}
		})
	}
}
