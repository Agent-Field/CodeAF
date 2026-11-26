package outputs

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
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

func TestWorksheetCSVComputesFormulasAndEscapesTheirText(t *testing.T) {
	template := schema.OutputTemplate{Format: "csv", Record: "worksheet", Table: "body"}
	rows := []map[string]any{{"body": "Name\tTotal\n\"Northwind, Inc.\"\t=1+2\n\"Line one\nLine two\"\t3"}}
	got, err := Render(template, rows)
	if err != nil {
		t.Fatal(err)
	}
	want := "Name,Total\n\"Northwind, Inc.\",3\n\"Line one\nLine two\",3\n"
	if string(got) != want {
		t.Fatalf("CSV = %q, want %q", got, want)
	}
}
func TestTextOutputRendersTheCurrentDocument(t *testing.T) {
	content, err := Render(schema.OutputTemplate{Label: "Copy text", Format: "txt", Body: "{{title}}\n\n{{body}}"},
		[]map[string]any{{"title": "Review", "body": "Ready to share."}})
	if err != nil || string(content) != "Review\n\nReady to share." {
		t.Fatalf("text output = %q, %v", content, err)
	}
}

func TestPresentationExportKeepsSlideBoundaries(t *testing.T) {
	var template schema.OutputTemplate
	if err := json.Unmarshal([]byte(`{"label":"Pilot","format":"pptx","record":"deck","body":"{{title}}","slides":"deck"}`), &template); err != nil {
		t.Fatal(err)
	}
	got, err := Render(template, []map[string]any{{"title": "Pilot", "deck": `[{"title":"Listening","body":"Interview eight people"},{"title":"Delivery","body":"Ship the pilot","notes":"Internal notes"}]`}})
	if err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(bytes.NewReader(got), int64(len(got)))
	if err != nil {
		t.Fatal(err)
	}
	var slides []string
	for _, file := range archive.File {
		if !strings.HasPrefix(file.Name, "ppt/slides/slide") {
			continue
		}
		r, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		content, err := io.ReadAll(r)
		_ = r.Close()
		if err != nil {
			t.Fatal(err)
		}
		slides = append(slides, string(content))
	}
	if len(slides) != 2 || !strings.Contains(slides[0], "Listening") ||
		!strings.Contains(slides[1], "Delivery") || strings.Contains(slides[1], "Internal notes") {
		t.Fatalf("expected two slides in order without notes; got %d", len(slides))
	}
}
