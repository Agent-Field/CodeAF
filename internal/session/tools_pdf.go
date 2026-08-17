package session

// read, wrapped: the file tool grows a SENSE rather than the belt growing a
// tool.
//
// A person drops a PDF into the conversation and says "what does section 4
// say". Every belt that answers this by adding an extract_pdf tool has made the
// model choose between two hands for one intention — and models choose wrong,
// reliably: what actually happens is `bash: python3 -c "import PyPDF2..."`, on a
// machine where PyPDF2 is not installed, twice, before the model gives up and
// asks the person to paste the text. The failure is not that the model is
// stupid. It is that read said it reads files and then did not read this file.
//
// So the fix goes inside read. The tool keeps its name, its schema, its
// description, its truncation law, and its place in the model's habits, and it
// learns that a PDF is a file whose text lives one decode deeper than the
// bytes. Nothing new to discover, nothing new to choose between, and the
// prompt line that goes with it (system.md) is one clause, not a paragraph
// teaching a new tool.
//
// This is backgroundBash's shape exactly (tools_jobs.go): find pi's tool in the
// belt, wrap it, hand every call it does not claim to the inner tool verbatim.
//
// THE WRAPPER ITSELF NOW LIVES IN tools_sense.go, because the argument above
// generalized. A PDF was the first file whose meaning lived one decode deeper
// than its bytes; a photograph, a recording and a film are the next three, and
// four wrappers chained around one tool would each parse the same arguments,
// resolve the same path, and let their ORDER silently decide which sense claims
// a file two of them could read. So one wrapper sniffs once and routes, and
// this file keeps what is genuinely its own: the local, free, in-binary rung,
// and pi's truncation law that every other rung borrows.
//
// THE ANSWER OBEYS PI'S TRUNCATION LAW, not a second one. Extracted text is
// text, a 400-page manual is a large file, and a model that learned "output is
// truncated to 2000 lines or 50KB, use offset to continue" from read's
// description gets exactly that here, footer wording included — the offset it
// is told to use works, because the next call re-extracts and pages from the
// same text. bare's truncation helpers are unexported, so this file MIRRORS
// them and says so at each constant; the numbers are pinned to
// internal/exec/bare/truncate.go and the sentences to bare's read tool.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
	"github.com/Agent-Field/aforge-v2/internal/pdfx"
)

// pdfSentence is the one sentence the wrapper adds to pi's read description.
// One sentence, because the tool is still read: what changes is which files it
// can answer for, and the second clause is there so a model that gets the
// scanned-PDF result recognizes it as a known limit rather than a bug to retry
// around.
const pdfSentence = " PDF files are read as extracted text (local, fast); scanned PDFs without a text layer cannot be read this way — read_document reads those."

// pdfRead is the belt's seam onto the wrapper (tools.go's one line), kept under
// its old name because that line belongs to another lane this wave and a rename
// that changes no behaviour is not worth a cross-lane edit. It collapses into
// senseRead at the call site when the wave merges.
//
// STUB(media/belt): rename this away once tools.go is one lane's again.
func (a *Agent) pdfRead(inner bare.Tool) bare.Tool { return a.senseRead(inner) }

// pdfSense is the local rung: extract the text layer in-binary and page it by
// pi's law. shown is the path AS THE MODEL WROTE IT, because every sentence
// below quotes it back and a model that asked about `spec.pdf` should not be
// answered about `/home/…/spec.pdf`.
//
// It is only ever called for a file the sniff already claimed and the stat
// already found, so there is no "not a PDF" and no "no such file" branch left
// in it — those are the wrapper's, and bare still owns the wording of the
// second one.
func (a *Agent) pdfSense(shown, absolute string, offset, limit *int) (string, bool, error) {
	text, err := pdfx.Extract(absolute)
	switch {
	case err == nil:
		return piReadLaw(text, offset, limit), false, nil

	case errors.Is(err, pdfx.ErrNoTextLayer):
		// The honest sentence, and the way out in the same breath. A scanned
		// document is not a failure of this tool — it is the point where the
		// ladder's next rung starts, and the model is told which rung rather
		// than left to invent one.
		//
		// It names A TOOL and not a ladder, and that is the whole lesson of
		// that wave: "the document_engine ladder's OCR rungs can read it" was
		// true, unactionable, and answered in the field by `pip install
		// easyocr`. A way out the model cannot call is not a way out
		// (tools_doc.go).
		var scanned *pdfx.NoTextLayerError
		pages := 0
		if errors.As(err, &scanned) {
			pages = scanned.Pages
		}
		return fmt.Sprintf(
			"%s is a scanned PDF with no text layer (%s, images only). No local text to read; use read_document (the OCR rung) or paste a page as an image.",
			shown, pageCount(pages),
		), false, nil

	default:
		// A result, not a harness error: the model reads the sentence and
		// adapts — a different file, a different rung, or asking the person —
		// where a raised error would only end the turn.
		return fmt.Sprintf("could not extract text from %s: %v", shown, err), true, nil
	}
}

// pageCount renders the page count so the sentence reads like English on a
// one-page fax.
func pageCount(pages int) string {
	if pages == 1 {
		return "1 page"
	}
	return strconv.Itoa(pages) + " pages"
}

// resolveInWorkspace mirrors bare's resolveToCwd (internal/exec/bare/tools.go),
// which is unexported: expand ~, strip a leading @, resolve against the
// workspace. The wrapper must land on the SAME file bare's read would, or a
// path the model wrote one way would be sniffed as one file and read as
// another.
//
// The unicode-space normalization bare also does is deliberately not mirrored:
// it changes which file is opened only for paths a model essentially never
// writes, and a divergence there costs one fall-through to bare — the inner
// tool then reads the PDF as bytes and reports it, which is the pre-wrapper
// behaviour rather than a wrong file.
func resolveInWorkspace(path, workspace string) string {
	trimmed := strings.TrimPrefix(path, "@")
	if home, err := os.UserHomeDir(); err == nil {
		if trimmed == "~" {
			trimmed = home
		} else if strings.HasPrefix(trimmed, "~/") {
			trimmed = filepath.Join(home, trimmed[2:])
		}
	}
	if filepath.IsAbs(trimmed) {
		return filepath.Clean(trimmed)
	}
	return filepath.Clean(filepath.Join(workspace, trimmed))
}

// ── pi's read law, mirrored ─────────────────────────────────────────────────

// The numbers are pi's, pinned to internal/exec/bare/truncate.go. They are
// mirrored rather than imported because bare's copies are unexported and bare
// is frozen: a divergence would show up as a read whose footer promises one
// budget and whose body carries another.
const (
	pdfMaxLines = 2000
	pdfMaxBytes = 50 * 1024
)

// piReadLaw applies bare read's offset/limit/truncation pipeline to extracted
// text, with bare's footer sentences verbatim.
//
// One deviation, and it is forced. bare's first-line-exceeds-limit branch tells
// the model to run `sed -n '1p' file | head -c 51200` — sound advice for a text
// file and nonsense for a PDF, where sed would print binary. Extracted PDF text
// really can arrive as one enormous line (a page whose content stream never
// emits a line break), so this path is reachable, and the useful answer there
// is the first 50KB of it plus a sentence saying that is what happened.
func piReadLaw(text string, offset, limit *int) string {
	allLines := strings.Split(text, "\n")
	totalFileLines := len(allLines)

	startLine := 0
	if offset != nil {
		startLine = *offset - 1
		if startLine < 0 {
			startLine = 0
		}
	}
	startLineDisplay := startLine + 1

	if startLine >= len(allLines) {
		requested := startLineDisplay
		if offset != nil {
			requested = *offset
		}
		return fmt.Sprintf("Offset %d is beyond end of file (%d lines total)", requested, len(allLines))
	}

	var selected string
	userLimitedLines := -1
	if limit != nil {
		endLine := startLine + *limit
		if endLine > len(allLines) {
			endLine = len(allLines)
		}
		selected = strings.Join(allLines[startLine:endLine], "\n")
		userLimitedLines = endLine - startLine
	} else {
		selected = strings.Join(allLines[startLine:], "\n")
	}

	content, truncated, truncatedBy, outputLines := truncateExtracted(selected)

	if truncatedBy == "first-line" {
		return content + fmt.Sprintf(
			"\n\n[Line %d is %s, exceeds %s limit. Showing its first %s.]",
			startLineDisplay, sizeLabel(len(allLines[startLine])), sizeLabel(pdfMaxBytes), sizeLabel(len(content)),
		)
	}

	if truncated {
		endLineDisplay := startLineDisplay + outputLines - 1
		nextOffset := endLineDisplay + 1
		if truncatedBy == "lines" {
			return content + fmt.Sprintf("\n\n[Showing lines %d-%d of %d. Use offset=%d to continue.]", startLineDisplay, endLineDisplay, totalFileLines, nextOffset)
		}
		return content + fmt.Sprintf("\n\n[Showing lines %d-%d of %d (%s limit). Use offset=%d to continue.]", startLineDisplay, endLineDisplay, totalFileLines, sizeLabel(pdfMaxBytes), nextOffset)
	}

	if userLimitedLines >= 0 && startLine+userLimitedLines < len(allLines) {
		remaining := len(allLines) - (startLine + userLimitedLines)
		nextOffset := startLine + userLimitedLines + 1
		return content + fmt.Sprintf("\n\n[%d more lines in file. Use offset=%d to continue.]", remaining, nextOffset)
	}

	return content
}

// truncateExtracted mirrors bare's truncateHead: keep the first whole lines
// that fit both caps, counting the newline that joins each line to the previous
// one, because that byte is real output the model pays for. The one addition is
// the "first-line" verdict, which bare signals with a flag and this returns as
// a reason, carrying the first 50KB of the line instead of nothing.
func truncateExtracted(content string) (out string, truncated bool, by string, outputLines int) {
	totalBytes := len(content)
	lines := splitLinesForCounting(content)
	if len(lines) <= pdfMaxLines && totalBytes <= pdfMaxBytes {
		return content, false, "", len(lines)
	}

	if len(lines) > 0 && len(lines[0]) > pdfMaxBytes {
		return truncateToBytes(lines[0], pdfMaxBytes), true, "first-line", 1
	}

	var kept []string
	keptBytes := 0
	by = "lines"
	for index := range min(len(lines), pdfMaxLines) {
		lineBytes := len(lines[index])
		if index > 0 {
			lineBytes++ // the newline separator
		}
		if keptBytes+lineBytes > pdfMaxBytes {
			by = "bytes"
			break
		}
		kept = append(kept, lines[index])
		keptBytes += lineBytes
	}
	if len(kept) >= pdfMaxLines && keptBytes <= pdfMaxBytes {
		by = "lines"
	}
	return strings.Join(kept, "\n"), true, by, len(kept)
}

// splitLinesForCounting mirrors bare's helper of the same name: a trailing
// newline terminates the last line rather than starting an empty one, which is
// what makes the footer's line numbers agree with wc -l.
func splitLinesForCounting(content string) []string {
	if content == "" {
		return nil
	}
	lines := strings.Split(content, "\n")
	if content[len(content)-1] == '\n' && len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// truncateToBytes keeps the first maxBytes bytes, backing off to a rune
// boundary so the result is still valid UTF-8 — extracted text is full of
// multi-byte punctuation (curly quotes, dashes, ligatures) and a cut through
// one of them would ride the wire as a replacement character.
func truncateToBytes(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	end := maxBytes
	for end > 0 && (s[end]&0xc0) == 0x80 {
		end--
	}
	return s[:end]
}

// sizeLabel mirrors bare's formatSize, whose rendered strings the footers
// interpolate: bytes under 1KB plain, then one decimal place, JS toFixed(1)
// style, so "50.0KB" reads the same here as it does from pi's read.
func sizeLabel(bytes int) string {
	if bytes < 1024 {
		return strconv.Itoa(bytes) + "B"
	}
	if bytes < 1024*1024 {
		return strconv.FormatFloat(float64(bytes)/1024, 'f', 1, 64) + "KB"
	}
	return strconv.FormatFloat(float64(bytes)/(1024*1024), 'f', 1, 64) + "MB"
}
