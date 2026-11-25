package outputs

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/schema"
)

func TestOutputGolden(t *testing.T) {
	rows := []map[string]any{{"company": "Northwind, Inc.", "total": 1200}, {"company": "Café & Co.", "total": 450}}
	for _, format := range []string{"csv", "pdf", "docx", "pptx"} {
		t.Run(format, func(t *testing.T) {
			template := schema.OutputTemplate{
				Label: "Pipeline summary", Format: format,
				Body: "{{company}} — {{total}}",
				Columns: []schema.OutputColumn{
					{Label: "Company", Value: "{{company}}"},
					{Label: "Total", Value: "{{total}}"},
				},
			}
			got, err := Render(template, rows)
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(filepath.Join("testdata", "pipeline."+format))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("rendered %s differs from the golden file", format)
			}
		})
	}
}
