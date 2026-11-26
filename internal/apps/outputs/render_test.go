package outputs

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/schema"
	"github.com/Agent-Field/codeaf/internal/pdfx"
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

func TestPDFTextAndPagination(t *testing.T) {
	rows := make([]map[string]any, 48)
	for i := range rows {
		rows[i] = map[string]any{"number": i + 1}
	}
	result, err := Render(schema.OutputTemplate{Label: "Café & Co.", Format: "pdf", Body: "Invoice — {{number}}"}, rows)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "invoices.pdf")
	if err := os.WriteFile(path, result, 0o600); err != nil {
		t.Fatal(err)
	}
	text, pages, err := pdfx.ExtractCounted(path)
	if err != nil || pages != 2 || !strings.Contains(text, "Café & Co.") || !strings.Contains(text, "Invoice — 48") {
		t.Fatalf("PDF text layer on %d pages: %q, %v", pages, text, err)
	}
}
