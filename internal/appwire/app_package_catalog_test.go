package appwire

import (
	"encoding/json"
	"testing"
	"testing/fstest"

	"github.com/Agent-Field/codeaf/internal/apps/apppackage"
	"github.com/Agent-Field/codeaf/internal/apps/schema"
)

func TestAppPackageExampleEvidence(t *testing.T) {
	const hash = "sha256:release"
	const name = "example.codeaf-app"
	for _, tc := range []struct {
		name, status, hash string
		rows               int
		want               bool
	}{
		{name: "accepted release", status: "passed", hash: hash, rows: 30, want: true},
		{name: "golden", status: "golden", hash: hash, rows: 31, want: true},
		{name: "held", status: "hold", hash: hash, rows: 31},
		{name: "unreviewed", hash: hash, rows: 31},
		{name: "old acceptance", status: "passed", hash: "sha256:old", rows: 31},
		{name: "missing examples", status: "passed", hash: hash, rows: 29},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows := make([]map[string]any, tc.rows)
			for i := range rows {
				rows[i] = map[string]any{"id": i}
			}
			sample, err := json.Marshal(schema.Sample{Records: map[string][]map[string]any{"example": rows}})
			if err != nil {
				t.Fatal(err)
			}
			companion, err := json.Marshal(map[string]string{"acceptance": tc.status, "accepted_hash": tc.hash})
			if err != nil {
				t.Fatal(err)
			}
			files := fstest.MapFS{name + ".json": {Data: companion}}
			archive := apppackage.Archive{
				Manifest: apppackage.Manifest{Source: apppackage.Source{Hash: hash}},
				Files:    map[string][]byte{schema.SampleFile: sample},
			}
			if got := exampleAccepted(files, name, archive); got != tc.want {
				t.Fatalf("accepted = %t, want %t", got, tc.want)
			}
		})
	}
}
