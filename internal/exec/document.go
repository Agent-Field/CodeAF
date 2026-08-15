package exec

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	osexec "os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/Agent-Field/aforge-v2/internal/provider"
)

const (
	maxDocumentInputBytes = 25 << 20

	// minimumDocumentTextRunesPerPage distinguishes a genuine selectable text
	// layer from page numbers, scanner watermarks, and other tiny scraps.
	minimumDocumentTextRunesPerPage = 32
	// When pdfinfo is unavailable, 100 KiB/page is deliberately conservative:
	// the rail may slightly overestimate, but it never hides an OCR bill behind
	// an optimistic one-page guess for a large scan.
	estimatedPDFBytesPerPage = 100 << 10
	// OpenRouter's verified Mistral OCR rate is $2 per 1,000 pages.
	mistralOCRUSDPerPage = 2.0 / 1000.0

	documentCommandTimeout = 30 * time.Second
	documentEngineAuto     = "auto"
	documentEngineLocal    = "local"
	documentEngineFree     = "free"
	documentEngineOCR      = "ocr"

	documentCacheHashPrefix  = "<!-- aforge-source-sha256: "
	documentCachePagesPrefix = "<!-- aforge-pages: "
)

var (
	documentPageRangePattern = regexp.MustCompile(`^([1-9][0-9]*)(?:-([1-9][0-9]*))?$`)
	pdfInfoPagesPattern      = regexp.MustCompile(`(?mi)^Pages:\s*([0-9]+)\s*$`)
)

type documentPageRange struct {
	start int
	end   int
	raw   string
}

func (r documentPageRange) count() int {
	if r.start <= 0 {
		return 0
	}
	return r.end - r.start + 1
}

func parseDocumentPageRange(raw string) (documentPageRange, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return documentPageRange{}, nil
	}
	matched := documentPageRangePattern.FindStringSubmatch(raw)
	if len(matched) == 0 {
		return documentPageRange{}, fmt.Errorf("read_document pages must look like 1-5")
	}
	start, _ := strconv.Atoi(matched[1])
	end := start
	if matched[2] != "" {
		end, _ = strconv.Atoi(matched[2])
	}
	if end < start {
		return documentPageRange{}, fmt.Errorf("read_document pages must end at or after page %d", start)
	}
	return documentPageRange{start: start, end: end, raw: raw}, nil
}

func (t *Toolbox) readDocument(ctx context.Context, args map[string]any) Result {
	path := strings.TrimSpace(stringArg(args, "path"))
	if path == "" {
		return errorf("read_document needs path")
	}
	full, err := t.workspace.Resolve(path)
	if err != nil {
		return errorf("%v", err)
	}
	mediaType, supported := documentMediaType(full)
	if !supported {
		return errorf("read_document supports pdf, docx, and pptx files")
	}
	info, err := os.Stat(full)
	if err != nil || info.IsDir() {
		return errorf("could not read %s", filepath.ToSlash(path))
	}
	if info.Size() > maxDocumentInputBytes {
		return errorf("%s is over the 25 MB document limit", filepath.ToSlash(path))
	}
	pages, err := parseDocumentPageRange(stringArg(args, "pages"))
	if err != nil {
		return errorf("%v", err)
	}
	if pages.raw != "" && strings.ToLower(filepath.Ext(full)) != ".pdf" {
		return errorf("read_document page ranges are supported for PDF files")
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return errorf("could not read %s", filepath.ToSlash(path))
	}
	if len(data) > maxDocumentInputBytes {
		return errorf("%s is over the 25 MB document limit", filepath.ToSlash(path))
	}
	hashBytes := sha256.Sum256(data)
	sourceHash := hex.EncodeToString(hashBytes[:])
	cachePath := extractedDocumentCachePath(full)
	if cached, ok := readDocumentCache(cachePath, sourceHash, pages.raw); ok {
		t.workspace.RecordInternal(t.leaf, cachePath)
		// Handed over whole: a document is read forward, and the turn boundary
		// is what cuts it — from the front, on a line boundary, with the whole
		// text preserved and the exact command to read on. See Toolbox.spill.
		return Result{Content: cached}
	}

	engine := documentEngineAuto
	if t.media != nil && strings.TrimSpace(t.media.DocumentEngine) != "" {
		engine = strings.ToLower(strings.TrimSpace(t.media.DocumentEngine))
	}
	extension := strings.ToLower(filepath.Ext(full))
	if extension != ".pdf" {
		return t.readOfficeDocument(ctx, path, full, cachePath, sourceHash, mediaType, data, pages.raw, engine,
			strings.TrimSpace(stringArg(args, "question")))
	}

	// A question rides only the native engine (office documents). PDF rungs
	// harvest parsed text without a model turn, so a silently dropped question
	// would leave the caller believing it shaped the result.
	note := ""
	if question := strings.TrimSpace(stringArg(args, "question")); question != "" {
		note = "note: question applies only to docx/pptx (native parsing); the extracted text follows\n"
	}
	withNote := func(result Result) Result {
		if note != "" && !result.IsError {
			result.Content = note + result.Content
		}
		return result
	}

	switch engine {
	case documentEngineLocal:
		return withNote(t.readDocumentLocal(ctx, path, full, cachePath, sourceHash, pages))
	case documentEngineFree:
		return withNote(t.readDocumentRemote(ctx, path, full, cachePath, sourceHash, mediaType, data, pages.raw,
			provider.DocumentParseCloudflare, ""))
	case documentEngineOCR:
		return withNote(t.readDocumentOCR(ctx, path, full, cachePath, sourceHash, mediaType, data, pages.raw))
	case documentEngineAuto:
	default:
		return errorf("read_document has unknown engine %q", engine)
	}

	localText, available, localErr := extractLocalPDF(ctx, full, pages)
	if localErr == nil && usableDocumentText(localText, localTextPageCount(localText, pages)) {
		return withNote(t.cacheDocumentResult(cachePath, sourceHash, pages.raw, "", localText, Usage{}))
	}
	_ = available // an absent or failed local rung both fall through in auto

	free, err := t.parseDocument(ctx, path, mediaType, data, provider.DocumentParseCloudflare, "")
	if err != nil {
		return errorf("document free parse failed — %s", oneLine(err.Error(), 180))
	}
	pageCount := estimatePDFPages(ctx, full, info.Size())
	if usableDocumentText(free.Text, pageCount) {
		return withNote(t.cacheDocumentResult(cachePath, sourceHash, pages.raw, free.Hash, free.Text, mediaUsage(free.Usage)))
	}

	if t.media != nil && t.media.BeforeSpend != nil {
		if err := t.media.BeforeSpend(ctx, float64(pageCount)*mistralOCRUSDPerPage); err != nil {
			return errorf("document OCR paused at the daily budget — approve it in chat to continue")
		}
	}
	ocr, err := t.parseDocument(ctx, path, mediaType, data, provider.DocumentParseMistralOCR, "")
	if err != nil {
		return errorf("document OCR failed — %s", oneLine(err.Error(), 180))
	}
	if strings.TrimSpace(ocr.Text) == "" {
		return errorf("document OCR returned no readable text")
	}
	usage := mediaUsage(free.Usage)
	usage.merge(mediaUsage(ocr.Usage))
	return withNote(t.cacheDocumentResult(cachePath, sourceHash, pages.raw, ocr.Hash, ocr.Text, usage))
}

func (t *Toolbox) readDocumentLocal(
	ctx context.Context,
	path, full, cachePath, sourceHash string,
	pages documentPageRange,
) Result {
	text, available, err := extractLocalPDF(ctx, full, pages)
	if !available {
		return errorf("local document extraction needs pdftotext (poppler) on PATH")
	}
	if err != nil {
		return errorf("local document extraction failed — %s", oneLine(err.Error(), 180))
	}
	if !usableDocumentText(text, localTextPageCount(text, pages)) {
		return errorf("%s has no usable local text layer; use auto or ocr for a scanned PDF", filepath.ToSlash(path))
	}
	return t.cacheDocumentResult(cachePath, sourceHash, pages.raw, "", text, Usage{})
}

func (t *Toolbox) readDocumentRemote(
	ctx context.Context,
	path, full, cachePath, sourceHash, mediaType string,
	data []byte,
	pages string,
	engine provider.DocumentParseEngine,
	question string,
) Result {
	response, err := t.parseDocument(ctx, path, mediaType, data, engine, question)
	if err != nil {
		if strings.ToLower(filepath.Ext(full)) != ".pdf" {
			return unsupportedOfficeDocument(path)
		}
		return errorf("document parsing failed — %s", oneLine(err.Error(), 180))
	}
	if strings.TrimSpace(response.Text) == "" {
		return errorf("document parser returned no readable text")
	}
	if engine == provider.DocumentParseCloudflare {
		pageCount := estimatePDFPages(ctx, full, int64(len(data)))
		if !usableDocumentText(response.Text, pageCount) {
			return errorf("the free parser found no readable text; use auto or ocr for a scanned PDF")
		}
	}
	return t.cacheDocumentResult(cachePath, sourceHash, pages, response.Hash, response.Text, mediaUsage(response.Usage))
}

func (t *Toolbox) readDocumentOCR(
	ctx context.Context,
	path, full, cachePath, sourceHash, mediaType string,
	data []byte,
	pages string,
) Result {
	pageCount := estimatePDFPages(ctx, full, int64(len(data)))
	if t.media != nil && t.media.BeforeSpend != nil {
		if err := t.media.BeforeSpend(ctx, float64(pageCount)*mistralOCRUSDPerPage); err != nil {
			return errorf("document OCR paused at the daily budget — approve it in chat to continue")
		}
	}
	return t.readDocumentRemote(ctx, path, full, cachePath, sourceHash, mediaType, data, pages,
		provider.DocumentParseMistralOCR, "")
}

func (t *Toolbox) readOfficeDocument(
	ctx context.Context,
	path, full, cachePath, sourceHash, mediaType string,
	data []byte,
	pages, engine, question string,
) Result {
	switch engine {
	case documentEngineLocal, documentEngineOCR:
		return errorf("%s only supports PDF files; use auto or free for %s", engine, strings.TrimPrefix(strings.ToLower(filepath.Ext(full)), "."))
	case documentEngineFree:
		return t.readDocumentRemote(ctx, path, full, cachePath, sourceHash, mediaType, data, pages,
			provider.DocumentParseCloudflare, "")
	case documentEngineAuto:
	default:
		return errorf("read_document has unknown engine %q", engine)
	}

	model := t.documentWorkingModel(ctx)
	if t.media != nil && t.media.Catalog != nil && t.media.Catalog.Supports(model, "input", "file") {
		if t.media.BeforeSpend != nil {
			if err := t.media.BeforeSpend(ctx, 0); err != nil {
				return errorf("native document reading paused at the daily budget — approve it in chat to continue")
			}
		}
		return t.readDocumentRemote(ctx, path, full, cachePath, sourceHash, mediaType, data, pages,
			provider.DocumentParseNative, question)
	}
	return t.readDocumentRemote(ctx, path, full, cachePath, sourceHash, mediaType, data, pages,
		provider.DocumentParseCloudflare, "")
}

func (t *Toolbox) parseDocument(
	ctx context.Context,
	path, mediaType string,
	data []byte,
	engine provider.DocumentParseEngine,
	question string,
) (*provider.DocumentResponse, error) {
	if t.media == nil || t.media.DocumentClient == nil {
		return nil, fmt.Errorf("document parsing is not configured")
	}
	return t.media.DocumentClient.ParseDocument(ctx, provider.DocumentRequest{
		Model: t.documentWorkingModel(ctx), Filename: filepath.Base(path), MediaType: mediaType,
		Data: data, Engine: engine, Question: question,
	})
}

func (t *Toolbox) documentWorkingModel(ctx context.Context) string {
	model := ""
	if t.media != nil {
		model = strings.TrimSpace(t.media.WorkingModel)
	}
	if call := provider.CallFrom(ctx); call != nil && strings.TrimSpace(call.Model()) != "" {
		model = call.Model()
	}
	return model
}

func (t *Toolbox) cacheDocumentResult(cachePath, sourceHash, pages, parserHash, text string, usage Usage) Result {
	text = strings.TrimSpace(text)
	if text == "" {
		return errorf("document extraction returned no readable text")
	}
	if err := writeDocumentCache(cachePath, sourceHash, pages, parserHash, text); err != nil {
		return errorf("could not cache extracted document text")
	}
	t.workspace.RecordInternal(t.leaf, cachePath)
	return Result{Content: text, Usage: usage}
}

func unsupportedOfficeDocument(path string) Result {
	extension := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	return errorf("only PDF is reliably supported; this %s file was not accepted by the configured parser", extension)
}

func documentMediaType(path string) (string, bool) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".pdf":
		return "application/pdf", true
	case ".docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document", true
	case ".pptx":
		return "application/vnd.openxmlformats-officedocument.presentationml.presentation", true
	default:
		return "", false
	}
}

func extractLocalPDF(ctx context.Context, path string, pages documentPageRange) (string, bool, error) {
	binary, err := osexec.LookPath("pdftotext")
	if err != nil {
		return "", false, nil
	}
	args := []string{"-enc", "UTF-8"}
	if pages.start > 0 {
		args = append(args, "-f", strconv.Itoa(pages.start), "-l", strconv.Itoa(pages.end))
	}
	args = append(args, path, "-")
	commandCtx, cancel := context.WithTimeout(ctx, documentCommandTimeout)
	defer cancel()
	output, err := osexec.CommandContext(commandCtx, binary, args...).CombinedOutput()
	if commandCtx.Err() != nil {
		return "", true, commandCtx.Err()
	}
	if err != nil {
		return "", true, fmt.Errorf("%v: %s", err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), true, nil
}

func localTextPageCount(text string, pages documentPageRange) int {
	if count := pages.count(); count > 0 {
		return count
	}
	text = strings.TrimRight(text, "\f\r\n\t ")
	if text == "" {
		return 1
	}
	return strings.Count(text, "\f") + 1
}

func usableDocumentText(text string, pages int) bool {
	if pages < 1 {
		pages = 1
	}
	nonWhitespace := 0
	for _, character := range text {
		if !unicode.IsSpace(character) {
			nonWhitespace++
		}
	}
	return nonWhitespace >= pages*minimumDocumentTextRunesPerPage
}

func estimatePDFPages(ctx context.Context, path string, size int64) int {
	if binary, err := osexec.LookPath("pdfinfo"); err == nil {
		commandCtx, cancel := context.WithTimeout(ctx, documentCommandTimeout)
		output, commandErr := osexec.CommandContext(commandCtx, binary, path).CombinedOutput()
		cancel()
		if commandErr == nil {
			matched := pdfInfoPagesPattern.FindStringSubmatch(string(output))
			if len(matched) > 1 {
				if pages, parseErr := strconv.Atoi(matched[1]); parseErr == nil && pages > 0 {
					return pages
				}
			}
		}
	}
	if size < 1 {
		return 1
	}
	pages := int((size + estimatedPDFBytesPerPage - 1) / estimatedPDFBytesPerPage)
	if pages < 1 {
		return 1
	}
	return pages
}

func extractedDocumentCachePath(source string) string {
	extension := filepath.Ext(source)
	return strings.TrimSuffix(source, extension) + ".extracted.md"
}

func documentCachePages(pages string) string {
	if strings.TrimSpace(pages) == "" {
		return "all"
	}
	return strings.TrimSpace(pages)
}

func readDocumentCache(path, sourceHash, pages string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	lines := strings.SplitN(string(data), "\n", 4)
	if len(lines) < 4 {
		return "", false
	}
	wantHash := documentCacheHashPrefix + sourceHash + " -->"
	wantPages := documentCachePagesPrefix + documentCachePages(pages) + " -->"
	if strings.TrimSpace(lines[0]) != wantHash || strings.TrimSpace(lines[1]) != wantPages {
		return "", false
	}
	text := strings.TrimSpace(lines[3])
	return text, text != ""
}

func writeDocumentCache(path, sourceHash, pages, parserHash, text string) error {
	var body strings.Builder
	body.WriteString(documentCacheHashPrefix)
	body.WriteString(sourceHash)
	body.WriteString(" -->\n")
	body.WriteString(documentCachePagesPrefix)
	body.WriteString(documentCachePages(pages))
	body.WriteString(" -->\n")
	if strings.TrimSpace(parserHash) != "" {
		body.WriteString("<!-- openrouter-file-hash: ")
		body.WriteString(strings.TrimSpace(parserHash))
		body.WriteString(" -->")
	}
	body.WriteString("\n\n")
	body.WriteString(strings.TrimSpace(text))
	body.WriteByte('\n')

	temporary, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o644); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.WriteString(body.String()); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}
