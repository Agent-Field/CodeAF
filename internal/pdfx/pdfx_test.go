package pdfx

// The seam's own tests cover the outcomes that need no PDF at all — a path that
// is not a file, and bytes that are not a document. The three real outcomes
// (text, page markers, no text layer) are tested through the tool that uses
// them, in internal/session/tools_pdf_test.go, where the hand-rolled documents
// live; a second copy of that builder here would be a second thing to keep
// true.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractRejectsMissingFile(t *testing.T) {
	if _, err := Extract(filepath.Join(t.TempDir(), "nothing.pdf")); err == nil {
		t.Fatal("a missing file should be an error")
	}
}

func TestExtractRejectsDirectory(t *testing.T) {
	directory := t.TempDir()
	_, err := Extract(directory)
	if err == nil || !strings.Contains(err.Error(), "is a directory") {
		t.Fatalf("a directory should say so: %v", err)
	}
}

// Bytes that are not a PDF are an error, not a panic and not an empty string:
// the parser walks stranger-shaped input, and the contract is that Extract
// survives it.
func TestExtractReportsGarbage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "garbage.pdf")
	if err := os.WriteFile(path, []byte("%PDF-1.7\nthis is not a pdf at all"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	text, err := Extract(path)
	if err == nil {
		t.Fatalf("garbage should not parse, got %q", text)
	}
	if errors.Is(err, ErrNoTextLayer) {
		t.Fatalf("a broken file is not a scanned one: %v", err)
	}
}

// The scanned outcome is a sentinel because callers must word it differently
// from a parse failure — one is a dead end, the other is a handoff to the
// ladder's OCR rungs — and the page count rides along for the ones that say
// how big the document was.
func TestNoTextLayerCarriesPageCount(t *testing.T) {
	err := error(&NoTextLayerError{Pages: 12})
	if !errors.Is(err, ErrNoTextLayer) {
		t.Fatal("NoTextLayerError should satisfy errors.Is(ErrNoTextLayer)")
	}
	var scanned *NoTextLayerError
	if !errors.As(err, &scanned) || scanned.Pages != 12 {
		t.Fatalf("page count should survive the error: %v", err)
	}
}
