package serve

import (
	"bytes"
	"fmt"
	"strconv"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// textPDF is a one-page PDF whose text layer says words, written the way a
// real PDF writer lays it out, with the xref offsets the parser reads by.
func textPDF(words string) []byte {
	stream := "BT /F1 24 Tf 72 700 Td (" + words + ") Tj ET"
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
		"<< /Length " + strconv.Itoa(len(stream)) + " >>\nstream\n" + stream + "\nendstream",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	var out bytes.Buffer
	out.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects))
	for i, body := range objects {
		offsets[i] = out.Len()
		out.WriteString(strconv.Itoa(i+1) + " 0 obj\n" + body + "\nendobj\n")
	}
	xref := out.Len()
	out.WriteString("xref\n0 " + strconv.Itoa(len(objects)+1) + "\n0000000000 65535 f \n")
	for _, o := range offsets {
		out.WriteString(fmt.Sprintf("%010d 00000 n \n", o))
	}
	out.WriteString("trailer\n<< /Size " + strconv.Itoa(len(objects)+1) + " /Root 1 0 R >>\nstartxref\n" + strconv.Itoa(xref) + "\n%%EOF\n")
	return out.Bytes()
}

// SEARCH FINDS AN UPLOAD BY WHAT IT SAYS (live2 E3 F5): a CSV's words and a
// PDF's text layer answer a Files search the way a document's words do, and a
// picture is still found by its name alone.
func TestFilesSearchReadsWhatUploadsSay(t *testing.T) {
	o := newWSOrg(t)
	ws := o.create(t, "santosh", api.WorkstreamNew{Name: "Pantry"})
	path := "/v1/workstreams/" + ws.ID + "/files"
	upload := func(name, mime string, data []byte) {
		o.do(t, "santosh", "POST", path, -1, api.FileNew{Kind: "upload", Name: name, Mime: mime, Data: data}, 201, nil)
	}
	upload("pantry-onboarding-survey-nov.csv", "text/csv", []byte("who,said\nana,loved the photo scan\nben,Couldn't find vegetarian filter\n"))
	upload("Freshbite.pdf", "application/pdf", textPDF("Allergen labels on every recipe"))
	upload("shelf.png", "image/png", []byte{0x89, 'P', 'N', 'G', 0, 0, 'l', 'o', 'v', 'e', 'd'})
	o.do(t, "santosh", "POST", path, -1, api.FileNew{Kind: "document", Name: "Notes", BodyMD: "Vegetarian first."}, 201, nil)

	found := func(q string) []string {
		var out api.WorkstreamFiles
		o.do(t, "santosh", "GET", path+"?q="+q, -1, nil, 200, &out)
		names := []string{}
		for _, f := range out.Files {
			names = append(names, f.Name)
		}
		return names
	}
	for q, want := range map[string]string{
		"loved":      "[pantry-onboarding-survey-nov.csv]",
		"vegetarian": "[Notes pantry-onboarding-survey-nov.csv]",
		"allergen":   "[Freshbite.pdf]",
		"freshbite":  "[Freshbite.pdf]",
		"shelf":      "[shelf.png]",
	} {
		if got := fmt.Sprint(found(q)); got != want {
			t.Errorf("searching %q finds %s, want %s", q, got, want)
		}
	}
}
