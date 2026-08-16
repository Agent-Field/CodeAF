package session

// read_document: the ladder's OCR rung, as a hand the model actually has.
//
// The local rung (tools_pdf.go) already says the true thing about a scan — "no
// text layer, images only" — and then named a LADDER. A ladder is not a tool. A
// model that is told the OCR rungs exist and given no way to call them does the
// one thing left: `bash: pip install easyocr`, on a machine where that is a
// four-minute download, a CUDA warning, and a wrong answer. That is not the
// model being stupid; it is the same fault tools_pdf.go opens with, one rung
// higher. read said it reads files and did not read this file; then the
// refusal said a rung could and there was no rung to call.
//
// So the rung becomes a hand. internal/provider already carries the whole
// engine — ParseDocument, three verified plugins, the annotation harvest — and
// internal/exec already drives it for the workforce (internal/exec/document.go).
// Neither of those is reachable from a conversation. This file is the seam.
//
// FOUR THINGS ARE DELIBERATE.
//
// It is a SECOND TOOL and not a second sense inside read, which is the exact
// opposite of tools_pdf.go's choice, and the difference is who pays. Growing
// read was right when the answer was local, free and instant: one hand, one
// habit, no decision. This answer costs money and seconds, and a model that
// cannot tell "read a file" from "spend two cents OCRing forty pages" will
// spend it on every source file it opens. The split is where the bill is.
//
// It is ALWAYS ON THE BELT, which inverts the conditional law tools_search.go
// states and tools_image.go follows. Those two are optional back ends the
// surface may never have wired, so a tool with nothing behind it is left off
// rather than added and made to refuse. This one rides THE SESSION'S OWN
// CREDENTIALS — the same key and base URL every turn already goes out on — so
// there is nothing to be conditional about: a session that can talk to its
// model can reach the rungs. And read's refusal now names this tool by name; a
// named way out that resolves to nothing is worse than either honest state.
//
// EVERY REFUSAL NAMES A RUNG. "Document parsing failed" tells a model to try
// again; "the mistral-ocr rung failed: 402 insufficient credits" tells it to
// stop, and tells the person reading the transcript which line of settings to
// change.
//
// AND THE ANSWER OBEYS PI'S TRUNCATION LAW, through the same piReadLaw the PDF
// wrapper uses — same 2000 lines, same 50KB, same footer sentences, same
// offset to continue. The offset has to work, and paying for a forty-page OCR
// again to show page two would be the one place this ladder is allowed to rob
// somebody, so the extraction is MEMOIZED per session by content digest: the
// rung runs once, and paging through what it returned is free.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
	"github.com/Agent-Field/aforge-v2/internal/pdfx"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// DocumentParser is the one call read_document makes: a file in, its text out.
//
// It is [provider.Client.ParseDocument]'s signature VERBATIM rather than a
// simplified one of this package's own, for the reason [ImageGenerator] states
// at its own declaration: the seam carries the wire's shape, so nothing between
// here and the provider can drift, and no adapter exists to drift in. It is an
// interface rather than the concrete client so a test drives a scripted parser
// and never opens a socket.
type DocumentParser interface {
	ParseDocument(context.Context, provider.DocumentRequest) (*provider.DocumentResponse, error)
}

// newDocClient builds the parser this session reads documents through.
//
// It is a package-level var so a test can substitute the whole construction —
// the tool holds an interface, but the DEFAULT has to build something real, and
// a test that could not replace the builder would need a live key to reach one
// line of tool logic. It is the same provider.Config New builds the session's
// own client from (agent.go), field for field, because it is the same account
// paying: a document rung is not a second vendor, it is a second endpoint on
// the one this conversation already rides.
//
// A missing key is an ERROR HERE and not a nil client, so the tool's refusal
// can say which thing is missing instead of "not configured".
var newDocClient = func(config Config) (DocumentParser, error) {
	if strings.TrimSpace(config.APIKey) == "" {
		return nil, errors.New("this session has no API key")
	}
	client, err := provider.NewClient(provider.Config{
		APIKey:         config.APIKey,
		BaseURL:        config.BaseURL,
		Model:          config.Model,
		Timeout:        providerTimeout,
		SiteURL:        config.SiteURL,
		SiteName:       config.SiteName,
		SiteCategories: config.SiteCategories,
	})
	if err != nil {
		return nil, err
	}
	return client, nil
}

// documentRung is the tool's whole state: the client, built once, and what the
// rungs have already returned this session.
//
// It is built ON FIRST USE rather than at construction, by [Agent.state]'s
// precedent (session.go): the belt closes over the agent, so a tool can reach a
// thing construction never made, and the once is what keeps a session that
// never reads a document from building a client it will never call.
type documentRung struct {
	once   sync.Once
	client DocumentParser
	err    error

	// mu guards memo, whose writers are tool calls running in parallel inside
	// one batch (loop.go) — two calls for two pages of the same scan race here
	// by design, and the loser simply re-stores what the winner stored.
	mu   sync.Mutex
	memo map[string]documentExtraction
}

// documentExtraction is one document, read once: the text and the rung that
// produced it, so page two says the same thing about its provenance as page one.
type documentExtraction struct {
	rung string
	text string
}

// documentMemoLimit bounds what one session holds in memory. Extracted
// documents are large — a forty-page scan is a megabyte of text — and the memo
// exists to make PAGING free, not to be a cache. Four is enough for a
// conversation working through a couple of documents at once; the fifth clears
// the map rather than evicting cleverly, because the cost of being wrong is one
// re-extraction and the cost of a heap policy is a heap policy.
const documentMemoLimit = 4

// documentMaxBytes is the input ceiling, pinned to internal/exec/document.go's
// maxDocumentInputBytes so the same file is accepted or refused the same way
// whichever door it arrives at. It is enforced against the stat BEFORE the read
// for the reason image.go's is: a limit checked after the read already pulled
// the file into memory to discover it was too big.
const documentMaxBytes = 25 << 20

// documentMinimumRunes is how little text makes an answer THIN — a page number,
// a scanner watermark, a stray glyph where a document should be. It mirrors
// internal/exec/document.go's minimumDocumentTextRunesPerPage, flattened from
// per-page to per-document because this seam has no page count to multiply by
// (exec runs pdfinfo; a conversation should not shell out to decide whether to
// try the next rung).
//
// IT DECIDES WHETHER TO CLIMB, NOT WHETHER TO ANSWER. A thin answer from a rung
// with another rung beneath it is a reason to try the next one; the same thin
// answer from the LAST rung is the answer, because a photographed receipt
// really does extract to four words and "could not read it" would be a refusal
// invented by a threshold. Emptiness is the only thing refused outright, and
// the provider refuses most of that itself (provider/document.go returns an
// error for a parser that harvested no annotations at all).
const documentMinimumRunes = 32

// The settings vocabulary (config.DocumentEngines: auto, local, free, ocr) and
// the wire vocabulary (provider's native, cloudflare-ai, mistral-ocr) are two
// different words for overlapping things, and both reach this file — the first
// off the settings sheet, the second from anybody who read the provider. Both
// are accepted and normalized here, once, so the rung plan below is written in
// one language.
const (
	docEngineAuto   = "auto"
	docEngineLocal  = "local"
	docEngineFree   = "free"
	docEngineOCR    = "ocr"
	docEngineNative = "native"
)

// documentKind is what sort of file this is, which is what decides which rungs
// could possibly read it. The paid parsers are FILE parsers — OpenRouter's
// plugin is keyed on the PDF pipeline — so a photograph has exactly one rung
// (the model's own eyes) and an unreachable one is refused by name rather than
// sent and answered with a provider error about a content part.
type documentKind int

const (
	documentPDF documentKind = iota
	documentOffice
	documentImage
)

// documentMediaTypes is the accepted set, by extension. The office three are
// internal/exec/document.go's plus xlsx; the image five are image.go's, because
// a page photographed with a phone is the commonest scan there is and refusing
// it here would send the person back to the ladder they could not climb.
var documentMediaTypes = map[string]struct {
	mediaType string
	kind      documentKind
}{
	".pdf":  {"application/pdf", documentPDF},
	".docx": {"application/vnd.openxmlformats-officedocument.wordprocessingml.document", documentOffice},
	".xlsx": {"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", documentOffice},
	".pptx": {"application/vnd.openxmlformats-officedocument.presentationml.presentation", documentOffice},
	".png":  {"image/png", documentImage},
	".jpg":  {"image/jpeg", documentImage},
	".jpeg": {"image/jpeg", documentImage},
	".webp": {"image/webp", documentImage},
	".gif":  {"image/gif", documentImage},
}

const readDocumentDescription = "Read a document the plain read tool cannot turn into text: a scanned PDF with no text layer, an image of a page or a receipt, or an office document (docx, xlsx, pptx). It sends the file to the document rungs — the chat model's own file handling first, then the parsers that cost money — and returns the extracted text, truncated to 2000 lines or 50KB with an offset to continue like read's. Use read instead for plain text, source code, and PDFs that already have a text layer: those are local, instant and free, and read tells you when it cannot read a file. Ask a question to point the extraction at what you need."

const readDocumentSchemaJSON = `{"type":"object","properties":{"path":{"type":"string","description":"The file to read, relative to the workspace or absolute"},"question":{"type":"string","description":"What you need from the document, asked of the page itself (default: read the whole thing). It shapes the extraction only on the native rung; the parser rungs return the full text either way."},"offset":{"type":"number","description":"The line of the extracted text to start from (1-based). The extraction is reused, so paging costs nothing."},"limit":{"type":"number","description":"How many lines of the extracted text to return"}},"required":["path"],"additionalProperties":false}`

// documentTool is the OCR rung on the belt. See this file's opening for why it
// is unconditional where tools_search.go and tools_image.go are not.
func (a *Agent) documentTool() bare.Tool {
	return bare.Tool{
		Name:        "read_document",
		Description: readDocumentDescription,
		Schema:      json.RawMessage(readDocumentSchemaJSON),
		Execute: func(ctx context.Context, args json.RawMessage) (string, bool, error) {
			var parsed struct {
				Path     string `json:"path"`
				Question string `json:"question"`
				Offset   *int   `json:"offset"`
				Limit    *int   `json:"limit"`
			}
			if err := json.Unmarshal(args, &parsed); err != nil {
				return "Invalid arguments: " + err.Error(), true, nil
			}
			path := strings.TrimSpace(parsed.Path)
			if path == "" {
				return "Invalid arguments: path is required", true, nil
			}
			return a.readDocument(ctx, path, strings.TrimSpace(parsed.Question), parsed.Offset, parsed.Limit)
		},
	}
}

// readDocument is the tool's body, split out so every early answer is a return
// of the same three values the belt expects and none of them is a Go error: a
// missing file, an unreadable format, a rung that failed and a rung that is
// switched off are all things the MODEL can act on — read the file another way,
// ask the person to change a setting, give up out loud — and none of them is a
// reason to end the turn (tools_search.go's rule).
func (a *Agent) readDocument(ctx context.Context, path, question string, offset, limit *int) (string, bool, error) {
	absolute := resolveInWorkspace(path, a.config.Workspace)
	shown := filepath.ToSlash(path)

	info, err := os.Stat(absolute)
	if err != nil || info.IsDir() {
		return fmt.Sprintf("could not read %s", shown), true, nil
	}
	if info.Size() > documentMaxBytes {
		return fmt.Sprintf("%s is over the %dMB document limit", shown, documentMaxBytes>>20), true, nil
	}

	entry, supported := documentMediaTypes[strings.ToLower(filepath.Ext(absolute))]
	if !supported {
		// The guard against being charged for what read does free. A .txt, a
		// .go, a .md — the plain read opens all of them, locally, in a
		// millisecond, and a model that sent one here has misread which hand
		// this is rather than found a limit.
		if looksLikePlainText(absolute) {
			return fmt.Sprintf("the plain read handles this: %s is plain text — call read, which opens it locally and free", shown), true, nil
		}
		return fmt.Sprintf("read_document reads PDFs, images (png, jpeg, webp, gif) and office documents (docx, xlsx, pptx); %s is none of those", shown), true, nil
	}

	setting := normalizeDocumentEngine(a.config.DocumentEngine)
	rungs, refusal := documentRungs(setting, entry.kind)
	if refusal != "" {
		return refusal, true, nil
	}

	// The local rung, tried first and only when nobody pinned an engine. This
	// tool exists for what read could not do, but the model is guessing when it
	// comes here — a PDF that turns out to HAVE a text layer must not be sent to
	// a parser that bills per page for text the binary can extract itself. Same
	// law as the plain-text guard above, one file type along.
	if entry.kind == documentPDF && setting == docEngineAuto {
		if text, err := pdfx.Extract(absolute); err == nil && !documentThin(text) {
			return documentNote("local", question, false) + piReadLaw(strings.TrimSpace(text), offset, limit), false, nil
		}
	}

	data, err := os.ReadFile(absolute)
	if err != nil {
		return fmt.Sprintf("could not read %s", shown), true, nil
	}
	// Checked again: the file could have grown between the stat and the read.
	if len(data) > documentMaxBytes {
		return fmt.Sprintf("%s is over the %dMB document limit", shown, documentMaxBytes>>20), true, nil
	}

	digest := sha256.Sum256(data)
	key := hex.EncodeToString(digest[:]) + "|" + setting + "|" + question
	if cached, ok := a.documentMemo(key); ok {
		return documentNote(cached.rung, question, cached.rung != docEngineNative) + piReadLaw(cached.text, offset, limit), false, nil
	}

	client, err := a.documentParser()
	if err != nil {
		return fmt.Sprintf("the document rungs are out of reach: %v — read_document rides this session's own API key and base URL", err), true, nil
	}

	model := a.Model()
	filename := filepath.Base(absolute)
	var failures []string
	for index, rung := range rungs {
		last := index == len(rungs)-1
		// The question rides the NATIVE rung alone, by internal/exec's law: the
		// parser rungs harvest annotations without a model turn, so a question
		// handed to one is a question silently dropped, and a caller who
		// believes it shaped the result is worse off than one told it did not.
		asked := ""
		if rung == provider.DocumentParseNative {
			asked = question
		}
		response, err := client.ParseDocument(ctx, provider.DocumentRequest{
			Model:     model,
			Filename:  filename,
			MediaType: entry.mediaType,
			Data:      data,
			Engine:    rung,
			Question:  asked,
		})
		// Accounted BEFORE the answer is judged, and folded into the SESSION
		// total rather than the turn's ([Agent.addAuxiliaryUsage]): a rung that
		// billed for an unusable answer still billed, and no turn of the
		// person's ran on that endpoint (the same treatment the title, the
		// compaction summary and a generated picture get).
		if response != nil {
			a.addAuxiliaryUsage(&ai.Response{Usage: response.Usage})
		}
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %s", rung, oneLineReason(err.Error())))
			continue
		}
		text := strings.TrimSpace(response.Text)
		if text == "" {
			failures = append(failures, fmt.Sprintf("%s: no readable text", rung))
			continue
		}
		// Thin, with somewhere left to climb: a page number is not the page.
		// See [documentMinimumRunes] for why the last rung is exempt.
		if documentThin(text) && !last {
			failures = append(failures, fmt.Sprintf("%s: only %s", rung, oneLineReason(text)))
			continue
		}
		a.storeDocumentMemo(key, documentExtraction{rung: string(rung), text: text})
		return documentNote(string(rung), question, rung != provider.DocumentParseNative) + piReadLaw(text, offset, limit), false, nil
	}

	// Every rung named, in the order they were tried, because "which one broke"
	// is the whole difference between a model that retries forever and a person
	// who knows whether to add credit, change document_engine, or scan again.
	return fmt.Sprintf("could not read %s — %s", shown, strings.Join(failures, "; ")), true, nil
}

// documentParser builds the client once, on first use, and hands back the same
// error every time it could not be built.
func (a *Agent) documentParser() (DocumentParser, error) {
	a.docs.once.Do(func() { a.docs.client, a.docs.err = newDocClient(a.config) })
	if a.docs.err != nil {
		return nil, a.docs.err
	}
	if a.docs.client == nil {
		return nil, errors.New("no document client")
	}
	return a.docs.client, nil
}

func (a *Agent) documentMemo(key string) (documentExtraction, bool) {
	a.docs.mu.Lock()
	defer a.docs.mu.Unlock()
	entry, ok := a.docs.memo[key]
	return entry, ok
}

func (a *Agent) storeDocumentMemo(key string, entry documentExtraction) {
	a.docs.mu.Lock()
	defer a.docs.mu.Unlock()
	if a.docs.memo == nil {
		a.docs.memo = make(map[string]documentExtraction, documentMemoLimit)
	}
	if len(a.docs.memo) >= documentMemoLimit {
		a.docs.memo = make(map[string]documentExtraction, documentMemoLimit)
	}
	a.docs.memo[key] = entry
}

// documentNote is the one line above the text: which rung read this, and — when
// a question was asked of a rung that cannot hear one — that the question did
// not shape it.
//
// It sits OUTSIDE the paged body on purpose. The footer's line numbers are the
// DOCUMENT's, so "use offset=451 to continue" means line 451 of the extraction
// and keeps meaning that whether or not this line is above it.
func documentNote(rung, question string, questionDropped bool) string {
	note := "[read_document — " + rung + " rung]"
	if question != "" && questionDropped {
		note += " the question shapes only the native rung; this is the full extracted text"
	}
	return note + "\n"
}

// documentRungs is the plan: which engines, in which order, for this setting and
// this kind of file. A second return that is non-empty is a REFUSAL — a
// combination with no rung behind it — worded so the person knows which row of
// settings says no.
func documentRungs(setting string, kind documentKind) ([]provider.DocumentParseEngine, string) {
	switch setting {
	case docEngineLocal:
		// Not a rung this tool has: "local" IS the read tool (in-binary PDF
		// extraction, tools_pdf.go), which is the thing that already failed if
		// the model is standing here.
		return nil, "document_engine is set to local, and the local rung is the read tool itself — in-binary PDF text extraction, which does not read scans. Set document_engine to auto, free or ocr to reach a rung that can."

	case docEngineNative:
		return []provider.DocumentParseEngine{provider.DocumentParseNative}, ""

	case docEngineFree:
		if kind == documentImage {
			return nil, "document_engine is set to free, and the free parser reads PDFs and office documents rather than images. Set document_engine to auto or native to read a picture of a page."
		}
		return []provider.DocumentParseEngine{provider.DocumentParseCloudflare}, ""

	case docEngineOCR:
		if kind != documentPDF {
			return nil, "document_engine is set to ocr, and the OCR parser reads PDFs. Set document_engine to auto or native for images and office documents."
		}
		return []provider.DocumentParseEngine{provider.DocumentParseMistralOCR}, ""

	case docEngineAuto:
		// Auto walks native first — the chat model's own file handling, paid for
		// as ordinary tokens rather than per page — and only falls to the
		// parsers when it comes back with nothing. An image stops there: the
		// parser rungs are file parsers, and there is no cheaper rung below a
		// model that can already see.
		switch kind {
		case documentImage:
			return []provider.DocumentParseEngine{provider.DocumentParseNative}, ""
		case documentOffice:
			return []provider.DocumentParseEngine{provider.DocumentParseNative, provider.DocumentParseCloudflare}, ""
		default:
			return []provider.DocumentParseEngine{
				provider.DocumentParseNative,
				provider.DocumentParseCloudflare,
				provider.DocumentParseMistralOCR,
			}, ""
		}
	}
	return nil, fmt.Sprintf("document_engine is set to %q, which is not a rung (auto, local, free, ocr)", setting)
}

// normalizeDocumentEngine folds the settings sheet's words and the provider's
// into one vocabulary. Empty is auto, which is config.DefaultDocumentEngine and
// also what an unwired surface hands over.
func normalizeDocumentEngine(setting string) string {
	value := strings.ToLower(strings.TrimSpace(setting))
	switch value {
	case "":
		return docEngineAuto
	case string(provider.DocumentParseCloudflare):
		return docEngineFree
	case string(provider.DocumentParseMistralOCR):
		return docEngineOCR
	}
	return value
}

// documentThin answers whether what came back is too little to be the document,
// which is a reason to climb rather than a reason to refuse. See
// [documentMinimumRunes].
func documentThin(text string) bool {
	runes := 0
	for _, character := range text {
		if !unicode.IsSpace(character) {
			runes++
			if runes >= documentMinimumRunes {
				return false
			}
		}
	}
	return true
}

// looksLikePlainText decides whether the file the model sent here is one read
// would have opened: valid UTF-8 with no NUL bytes in its first few KB.
//
// The prefix is enough and the whole file is not read, because the question is
// "did the model pick the wrong hand", not "is every byte of this text": a file
// that opens with 8KB of clean UTF-8 and turns binary on page nine is one read
// will still show, truncated, which is the outcome this sentence points at.
func looksLikePlainText(absolute string) bool {
	file, err := os.Open(absolute)
	if err != nil {
		return false
	}
	defer file.Close()
	prefix := make([]byte, 8<<10)
	read, err := file.Read(prefix)
	if read <= 0 {
		// An empty file is read's to describe, not this tool's.
		return err == nil || errors.Is(err, os.ErrClosed)
	}
	prefix = prefix[:read]
	// A NUL byte is the oldest and most reliable "this is not text" signal there
	// is, and it is what every scanned document, image and office archive here
	// carries within its first bytes.
	if bytes.IndexByte(prefix, 0) >= 0 {
		return false
	}
	// A cut through a multi-byte rune at the 8KB boundary is not a binary file,
	// so the last partial rune is dropped before the check rather than counted
	// against it.
	for len(prefix) > 0 && !utf8.Valid(prefix) && (prefix[len(prefix)-1]&0xc0) == 0x80 {
		prefix = prefix[:len(prefix)-1]
	}
	if len(prefix) > 0 && !utf8.Valid(prefix) {
		return false
	}
	return true
}

// oneLineReason flattens a provider error onto one line and bounds it, mirroring
// internal/exec/document.go's oneLine: an API error can carry a whole HTML page,
// and a tool result that is mostly somebody's error template teaches nothing.
func oneLineReason(message string) string {
	message = strings.Join(strings.Fields(message), " ")
	if len(message) > 180 {
		message = truncateToBytes(message, 177) + "…"
	}
	return message
}
