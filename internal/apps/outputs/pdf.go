package outputs

import (
	"io"
	"strings"
	"time"

	"github.com/phpdave11/gofpdf"
	"golang.org/x/image/font/gofont/goregular"
)

func writePDF(w io.Writer, title string, sections []string) error {
	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(18, 18, 18)
	pdf.SetAutoPageBreak(true, 18)
	pdf.SetCatalogSort(true)
	// Provenance belongs to the saved file record. Fixed metadata makes the
	// same inputs produce the same bytes, including after a retry.
	epoch := time.Unix(0, 0).UTC()
	pdf.SetCreationDate(epoch)
	pdf.SetModificationDate(epoch)
	pdf.SetTitle(title, true)
	pdf.AddUTF8FontFromBytes("Go", "", goregular.TTF)
	pdf.AddPage()
	pdf.SetFont("Go", "", 18)
	pdf.MultiCell(0, 9, pdfText(title), "", "L", false)
	pdf.Ln(4)
	pdf.SetFont("Go", "", 11)
	for _, section := range sections {
		pdf.MultiCell(0, 5.5, pdfText(section), "", "L", false)
		pdf.Ln(3)
	}
	return pdf.Output(w)
}

func pdfText(text string) string {
	// The PDF writer's embedded font map is limited to the BMP.
	return strings.Map(func(r rune) rune {
		if r > 0xffff || r < ' ' && r != '\n' && r != '\t' {
			return '\uFFFD'
		}
		return r
	}, strings.ReplaceAll(text, "\t", "    "))
}
