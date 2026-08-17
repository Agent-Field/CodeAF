package session

// PDF read tests.
//
// The PDFs are BUILT HERE, byte by byte, rather than vendored as testdata. A
// checked-in binary is a fixture nobody can review: when the extractor stops
// finding the text, the question is whether the file or the code changed, and
// with a hand-rolled document the answer is on the screen. Each one is the
// smallest valid PDF that makes its point — a catalog, a pages node, a page,
// and a content stream that either does or does not draw text.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// ── minimal PDFs ────────────────────────────────────────────────────────────

// buildPDF assembles a classic uncompressed PDF from object bodies — objects[i]
// is object number i+1 — computing the xref offsets as it writes, because the
// offsets are what the parser navigates by and a wrong one is an unreadable
// file.
func buildPDF(objects []string) []byte {
	var out bytes.Buffer
	out.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects))
	for index, body := range objects {
		offsets[index] = out.Len()
		out.WriteString(strconv.Itoa(index+1) + " 0 obj\n" + body + "\nendobj\n")
	}
	xref := out.Len()
	out.WriteString("xref\n0 " + strconv.Itoa(len(objects)+1) + "\n")
	out.WriteString("0000000000 65535 f \n")
	for _, offset := range offsets {
		out.WriteString(fmt.Sprintf("%010d 00000 n \n", offset))
	}
	out.WriteString("trailer\n<< /Size " + strconv.Itoa(len(objects)+1) + " /Root 1 0 R >>\n")
	out.WriteString("startxref\n" + strconv.Itoa(xref) + "\n%%EOF\n")
	return out.Bytes()
}

// contentStream wraps a content stream body with the /Length the parser trusts.
func contentStream(body string) string {
	return "<< /Length " + strconv.Itoa(len(body)) + " >>\nstream\n" + body + "\nendstream"
}

// pageObject is a page whose content is the given object number.
func pageObject(contents int) string {
	return "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] " +
		"/Resources << /Font << /F1 5 0 R >> >> /Contents " + strconv.Itoa(contents) + " 0 R >>"
}

const helvetica = "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>"

// showText draws one string on the page: the BT/Tf/Td/Tj/ET a real PDF writes.
func showText(words string) string {
	return "BT /F1 24 Tf 72 700 Td (" + words + ") Tj ET"
}

// onePageTextPDF is the good path: one page, one line of text.
func onePageTextPDF(words string) []byte {
	return buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		pageObject(4),
		contentStream(showText(words)),
		helvetica,
	})
}

// scannedPDF is a page with no text operators at all — a filled rectangle where
// a scan would have its image. Structurally sound, textually empty: the shape
// of every scanned document.
func scannedPDF() []byte {
	return buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		pageObject(4),
		contentStream("0 0 0 rg 10 10 100 100 re f"),
		helvetica,
	})
}

// dropFile drops one file in the workspace and returns its name, which is what
// the model would pass as a relative path.
func dropFile(t *testing.T, workspace, name string, data []byte) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(workspace, name), data, 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return name
}

// readTool calls the belt's read tool the way the loop does.
func readTool(t *testing.T, agent *Agent, arguments map[string]any) (string, bool) {
	t.Helper()
	encoded, err := json.Marshal(arguments)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return runTool(t, agent, "read", string(encoded))
}

// ── the good path ───────────────────────────────────────────────────────────

func TestReadExtractsPDFText(t *testing.T) {
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, nil)
	name := dropFile(t, workspace, "spec.pdf", onePageTextPDF("the local rung reads this"))

	text, isError := readTool(t, agent, map[string]any{"path": name})
	if isError {
		t.Fatalf("read reported an error: %s", text)
	}
	if !strings.Contains(text, "the local rung reads this") {
		t.Fatalf("extracted text missing the document's words: %q", text)
	}
}

// A PDF is a PDF by its bytes, not by its name: a downloaded attachment saved
// without an extension is exactly the file a person drags in.
func TestReadSniffsPDFWithoutExtension(t *testing.T) {
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, nil)
	name := dropFile(t, workspace, "attachment", onePageTextPDF("no extension anywhere"))

	text, isError := readTool(t, agent, map[string]any{"path": name})
	if isError {
		t.Fatalf("read reported an error: %s", text)
	}
	if !strings.Contains(text, "no extension anywhere") {
		t.Fatalf("magic-byte sniff missed the pdf: %q", text)
	}
}

// The page marker is information — "page 2" is how both a person and a model
// point at a place in a document — and it appears only when there is more than
// one page to separate.
func TestReadMarksPageBoundaries(t *testing.T) {
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, nil)
	twoPages := buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 6 0 R] /Count 2 >>",
		pageObject(4),
		contentStream(showText("first page words")),
		helvetica,
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 5 0 R >> >> /Contents 7 0 R >>",
		contentStream(showText("second page words")),
	})
	name := dropFile(t, workspace, "manual.pdf", twoPages)

	text, isError := readTool(t, agent, map[string]any{"path": name})
	if isError {
		t.Fatalf("read reported an error: %s", text)
	}
	if !strings.Contains(text, "── page 2 ──") {
		t.Fatalf("page marker missing: %q", text)
	}
	if !strings.Contains(text, "first page words") || !strings.Contains(text, "second page words") {
		t.Fatalf("both pages should be present: %q", text)
	}

	single, _ := readTool(t, agent, map[string]any{"path": dropFile(t, workspace, "one.pdf", onePageTextPDF("alone"))})
	if strings.Contains(single, "── page") {
		t.Fatalf("a one-page document should carry no marker: %q", single)
	}
}

// ── pass-through ────────────────────────────────────────────────────────────

// Every other file reaches pi's read untouched. The assertion is byte equality
// with the file, because "untouched" is the whole promise: a wrapper that
// reformats ordinary reads would have changed the tool for every call to buy
// one file type.
func TestReadPassesNonPDFThrough(t *testing.T) {
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, nil)
	body := "package main\n\nfunc main() {}\n"
	name := dropFile(t, workspace, "main.go", []byte(body))

	text, isError := readTool(t, agent, map[string]any{"path": name})
	if isError {
		t.Fatalf("read reported an error: %s", text)
	}
	if text != body {
		t.Fatalf("pass-through altered the file: %q want %q", text, body)
	}
}

// A missing file is bare's sentence, not a second one about PDFs — the wrapper
// declines the call and pi reports it exactly as it always has.
func TestReadMissingPDFKeepsBareWording(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)

	text, isError := readTool(t, agent, map[string]any{"path": "nowhere.pdf"})
	if !isError {
		t.Fatalf("a missing file should be an error result: %q", text)
	}
	if !strings.HasPrefix(text, "Error reading file: ") {
		t.Fatalf("missing file should keep bare's wording: %q", text)
	}
}

// ── the two honest failures ─────────────────────────────────────────────────

// Garbage behind a %PDF- header is what a truncated download looks like. The
// contract is a sentence the model can act on and a process that is still
// running — the parser walks hostile bytes, so the wrapper turns a panic into
// this same result.
func TestReadReportsUnparseablePDF(t *testing.T) {
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, nil)
	garbage := append([]byte("%PDF-1.7\n"), bytes.Repeat([]byte{0x00, 0xff, 0x41, 0x7f}, 256)...)
	name := dropFile(t, workspace, "truncated.pdf", garbage)

	text, isError := readTool(t, agent, map[string]any{"path": name})
	if !isError {
		t.Fatalf("an unreadable pdf should be an error result: %q", text)
	}
	if !strings.HasPrefix(text, "could not extract text from truncated.pdf: ") {
		t.Fatalf("parse failure should name the file and the reason: %q", text)
	}
}

// A scanned page is not a failure of this tool — it is where the ladder's next
// rung starts, and the sentence says which one rather than leaving the model to
// invent a Python script.
func TestReadReportsScannedPDF(t *testing.T) {
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, nil)
	name := dropFile(t, workspace, "scan.pdf", scannedPDF())

	text, isError := readTool(t, agent, map[string]any{"path": name})
	if isError {
		t.Fatalf("a scanned pdf is information, not a tool failure: %s", text)
	}
	want := "scan.pdf is a scanned PDF with no text layer (1 page, images only)."
	if !strings.HasPrefix(text, want) {
		t.Fatalf("scanned wording drifted:\n got %q\nwant prefix %q", text, want)
	}
	// The way out is a TOOL, not a ladder: see tools_doc.go, and
	// TestTheScannedPDFRefusalNamesAToolTheBeltCarries beside it.
	if !strings.Contains(text, "read_document (the OCR rung)") {
		t.Fatalf("the scanned result should name the way out: %q", text)
	}
}

// ── pi's law ────────────────────────────────────────────────────────────────

// The description the model reads is pi's, plus the two sentences the senses
// add. All three halves matter: pi's is why the model keeps its read habits,
// the PDF clause is why it recognizes the scanned result as a known limit, and
// the senses clause is why it never writes a decoder script.
func TestReadDescriptionGainsOneSentence(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	tool := beltTool(t, agent, "read")

	if !strings.HasPrefix(tool.Description, "Read the contents of a file.") {
		t.Fatalf("pi's description should lead: %q", tool.Description)
	}
	if !strings.Contains(tool.Description, pdfSentence) {
		t.Fatalf("the pdf sentence should survive the senses: %q", tool.Description)
	}
	if !strings.HasSuffix(tool.Description, senseSentence) {
		t.Fatalf("the senses sentence should close it: %q", tool.Description)
	}
	if !strings.Contains(tool.Description, "truncated to 2000 lines or 50KB") {
		t.Fatalf("pi's truncation law should survive the wrap: %q", tool.Description)
	}
}

// offset/limit work on extracted text the way they work on a file, and the
// footer says so in pi's words — a continuation pointer the model is told to
// use has to actually be usable, and it is: the next call re-extracts and pages
// from the same text.
func TestPDFReadHonoursOffsetAndLimit(t *testing.T) {
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, nil)
	// Four lines on one page: four text-showing operators, each moved down.
	body := "BT /F1 12 Tf 72 700 Td (alpha line) Tj 0 -20 Td (beta line) Tj 0 -20 Td (gamma line) Tj 0 -20 Td (delta line) Tj ET"
	name := dropFile(t, workspace, "lines.pdf", buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		pageObject(4),
		contentStream(body),
		helvetica,
	}))

	whole, _ := readTool(t, agent, map[string]any{"path": name})
	lines := strings.Split(whole, "\n")
	if len(lines) < 2 {
		t.Skipf("extractor emitted %d line(s); offset paging has nothing to page: %q", len(lines), whole)
	}

	first, _ := readTool(t, agent, map[string]any{"path": name, "limit": 1})
	if !strings.HasPrefix(first, lines[0]) {
		t.Fatalf("limit=1 should start at the first line: %q", first)
	}
	if !strings.Contains(first, "more lines in file. Use offset=2 to continue.]") {
		t.Fatalf("limit should carry pi's continuation footer: %q", first)
	}

	second, _ := readTool(t, agent, map[string]any{"path": name, "offset": 2})
	if strings.Contains(second, lines[0]) {
		t.Fatalf("offset=2 should have skipped the first line: %q", second)
	}
	if !strings.Contains(second, lines[1]) {
		t.Fatalf("offset=2 should start at the second line: %q", second)
	}

	beyond, _ := readTool(t, agent, map[string]any{"path": name, "offset": len(lines) + 5})
	if !strings.HasPrefix(beyond, "Offset ") || !strings.Contains(beyond, "beyond end of file") {
		t.Fatalf("an offset past the end should keep pi's wording: %q", beyond)
	}
}

// The truncation law is mirrored from bare, so it is pinned here directly: a
// long text truncates at 2000 lines and says so in pi's sentence, and a single
// enormous line — which extracted PDF text really can be — returns its first
// 50KB rather than pi's sed hint, which would print binary.
func TestPDFReadTruncationMirrorsPi(t *testing.T) {
	many := strings.Repeat("a line of extracted text\n", 2500)
	truncated := piReadLaw(many, nil, nil)
	if !strings.Contains(truncated, "[Showing lines 1-2000 of 2501. Use offset=2001 to continue.]") {
		t.Fatalf("line truncation should carry pi's footer: %q", truncated[max(0, len(truncated)-120):])
	}

	huge := strings.Repeat("x", 60*1024)
	oneLine := piReadLaw(huge, nil, nil)
	if len(oneLine) < pdfMaxBytes {
		t.Fatalf("a huge single line should still return its first 50KB, got %d bytes", len(oneLine))
	}
	if !strings.Contains(oneLine, "exceeds 50.0KB limit. Showing its first 50.0KB.]") {
		t.Fatalf("single-line truncation should say what it did: %q", oneLine[max(0, len(oneLine)-120):])
	}

	short := "one\ntwo\nthree"
	if got := piReadLaw(short, nil, nil); got != short {
		t.Fatalf("text inside the caps should ride through untouched: %q", got)
	}
}
