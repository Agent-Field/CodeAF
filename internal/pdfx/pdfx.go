// Package pdfx is the LOCAL RUNG of the document ladder: PDF text, extracted
// in this process, out of this binary, with no service to call and nothing to
// install.
//
// The ladder's shape is the reason this package is one small file. A document
// arrives and something has to read it; the rungs go from cheapest to dearest —
// an embedded text layer here, then OCR, then a vision model looking at the
// page as a picture. The overwhelming majority of PDFs a person hands a coding
// session (a spec, a paper, an invoice, an API reference) carry a text layer,
// and reading it is microseconds of CPU. Paying a vision model to look at a
// picture of text the file already contains is the expensive answer to a
// question nobody had to ask.
//
// So the engine here is github.com/AOShei/go-fast-pdf: pure Go, zero
// dependencies, MIT, compiled straight into the static binary. The rejected
// alternatives were not worse at reading PDFs — they were worse at SHIPPING:
// PDFOxide wants a runtime .so beside the binary and extractous wants Tesseract
// installed, and a rung of the ladder that fails on a machine where nobody ran
// apt-get is not a local rung at all.
//
// The seam is deliberately one function wide. Extract(path) is the entire
// contract this package owes the rest of the program, which is what makes the
// engine swappable: a better pure-Go extractor lands as an edit to this file
// and nothing above it moves.
//
// THREE OUTCOMES, NOT TWO. A caller that only knows "text or error" cannot tell
// a broken file from a scanned one, and those two need opposite responses from
// the model — the first is a dead end, the second is a handoff to the ladder's
// OCR rungs. So a PDF that parses cleanly but carries no text layer gets its
// own sentinel (ErrNoTextLayer, with the page count attached) rather than an
// empty string and a shrug.
package pdfx

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/AOShei/go-fast-pdf/pkg/loader"
	"github.com/AOShei/go-fast-pdf/pkg/model"
)

// concurrentThreshold is the file size above which extraction goes wide. Below
// it the worker pool costs more to set up (a file handle and an xref parse per
// worker) than the pages save; above it a manual or a long paper is the whole
// wait, and the cores are sitting there.
const concurrentThreshold = 2 << 20 // 2MB

// autoWorkers asks the library for one worker per CPU, capped by its own page
// count. The session has no better number than the machine's.
const autoWorkers = 0

// ErrNoTextLayer is the scanned-PDF outcome: the file parsed, it has pages, and
// every one of them is whitespace. Callers word it for their own audience with
// errors.Is; NoTextLayerError carries the page count for the ones that want to
// say how big the document was.
var ErrNoTextLayer = errors.New("pdf has no text layer")

// NoTextLayerError is ErrNoTextLayer with the page count attached.
type NoTextLayerError struct {
	Pages int
}

func (e *NoTextLayerError) Error() string {
	return fmt.Sprintf("%s (%d pages, images only)", ErrNoTextLayer.Error(), e.Pages)
}

// Unwrap makes errors.Is(err, ErrNoTextLayer) the test callers write, so the
// page count is available to whoever wants it and invisible to whoever does
// not.
func (e *NoTextLayerError) Unwrap() error { return ErrNoTextLayer }

// Extract returns the document's text, with a light page marker between pages.
//
// The marker is there because a page boundary is real information — "page 3"
// is how a person and a model both refer to a place in a document — and it is
// omitted on a single-page file because a marker that never varies is noise.
//
// Three returns, one each for the three outcomes: text and nil, "" and an error
// wrapping ErrNoTextLayer for a scanned file, "" and a plain error for a file
// that would not parse.
func Extract(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("%s is a directory", path)
	}

	document, err := load(path, info.Size())
	if err != nil {
		return "", err
	}
	if len(document.Pages) == 0 {
		return "", errors.New("the file parsed but contains no pages")
	}

	var text strings.Builder
	multiPage := len(document.Pages) > 1
	anyText := false
	for index, page := range document.Pages {
		if index > 0 && multiPage {
			text.WriteString("\n── page " + strconv.Itoa(page.PageNumber) + " ──\n")
		}
		content := strings.Trim(page.Content, "\n")
		text.WriteString(content)
		if strings.TrimSpace(content) != "" {
			anyText = true
		}
	}
	if !anyText {
		return "", &NoTextLayerError{Pages: len(document.Pages)}
	}
	return text.String(), nil
}

// load runs the engine, wide or narrow by file size, with the two hazards of
// calling a third-party parser on a stranger's bytes handled in one place.
//
// A PANIC IS AN ERROR HERE. The engine walks attacker-shaped input — a
// truncated xref, a length that lies, an object graph with a cycle — and a
// slice index that goes out of range inside it would otherwise take down a
// whole session over one bad file. A parser that panics has told us the same
// thing a parser that returns an error tells us, in a ruder way.
func load(path string, size int64) (document *model.Document, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			document, err = nil, fmt.Errorf("the pdf parser failed on this file: %v", recovered)
		}
	}()
	return quietly(func() (*model.Document, error) {
		if size > concurrentThreshold {
			return loader.LoadPDFConcurrent(path, autoWorkers, false)
		}
		return loader.LoadPDF(path, false)
	})
}

// quietStreams serializes the stream swap below. It also serializes extraction
// itself, which is a fair trade: extraction is milliseconds, and the file
// handles the engine opens are the real resource.
var quietStreams sync.Mutex

// quietly runs the engine with os.Stdout and os.Stderr pointed at /dev/null.
//
// This is not fastidiousness. The library narrates: loader prints "Processing N
// pages..." and a line per page to stderr, and the reader prints a resolve
// warning to stdout, unconditionally and with no way to turn it off. A session
// runs inside a full-screen TUI, and a library writing to the terminal
// underneath it corrupts the frame the person is looking at — the tool call
// would appear to have scrambled the screen.
//
// It swaps the os.Stdout/os.Stderr VARIABLES, which only affects code that
// dereferences them at call time — the library does, and the surface does not
// (bubbletea captured its writer at startup). The cost is that another
// goroutine writing a line to os.Stderr during these few milliseconds loses it,
// which is the smaller of the two failures.
func quietly(run func() (*model.Document, error)) (*model.Document, error) {
	quietStreams.Lock()
	defer quietStreams.Unlock()

	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		// Nowhere to point them: extract anyway. Noise beats not reading the
		// document at all.
		return run()
	}
	defer devNull.Close()

	savedOut, savedErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = devNull, devNull
	defer func() { os.Stdout, os.Stderr = savedOut, savedErr }()

	return run()
}
