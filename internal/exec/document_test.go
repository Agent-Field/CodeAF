package exec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/provider"
)

// The document ladder is exercised through the real provider client over an
// in-memory transport: the rung a request lands on is only visible in the
// plugin it names on the wire, so a hand-written fake would assert the
// implementation rather than the contract.
type documentWireStub struct {
	status int
	body   string
}

type documentWireCapture struct {
	calls     int
	bodies    []map[string]any
	responses []documentWireStub
}

func (c *documentWireCapture) client(t *testing.T) *provider.Client {
	t.Helper()
	httpClient := &http.Client{Transport: execRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		payload, _ := io.ReadAll(request.Body)
		var body map[string]any
		if err := json.Unmarshal(payload, &body); err != nil {
			t.Errorf("decode document request: %v", err)
		}
		index := c.calls
		c.calls++
		c.bodies = append(c.bodies, body)
		stub := documentWireStub{status: http.StatusBadRequest, body: `{"error":{"message":"unscripted document call"}}`}
		if index < len(c.responses) {
			stub = c.responses[index]
		} else {
			t.Errorf("unscripted document call %d", index+1)
		}
		status := stub.status
		if status == 0 {
			status = http.StatusOK
		}
		return &http.Response{
			StatusCode: status, Status: http.StatusText(status), Header: make(http.Header),
			Body: io.NopCloser(strings.NewReader(stub.body)), Request: request,
		}, nil
	})}
	client, err := provider.NewClient(provider.Config{
		APIKey: "test-key", BaseURL: "https://provider.example/v1", Model: "work/model", HTTPClient: httpClient,
	})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

// engines names the parser rung each captured request selected, in order.
func (c *documentWireCapture) engines() []string {
	var named []string
	for _, body := range c.bodies {
		plugins, _ := body["plugins"].([]any)
		if len(plugins) == 0 {
			named = append(named, "<none>")
			continue
		}
		plugin, _ := plugins[0].(map[string]any)
		pdf, _ := plugin["pdf"].(map[string]any)
		named = append(named, fmt.Sprint(pdf["engine"]))
	}
	return named
}

func (c *documentWireCapture) prompt(t *testing.T, index int) string {
	t.Helper()
	if index >= len(c.bodies) {
		t.Fatalf("no captured document call %d", index+1)
	}
	messages, _ := c.bodies[index]["messages"].([]any)
	message, _ := messages[0].(map[string]any)
	parts, _ := message["content"].([]any)
	text, _ := parts[0].(map[string]any)
	return fmt.Sprint(text["text"])
}

func documentAnnotationStub(hash, text string, cost float64) documentWireStub {
	payload := map[string]any{
		"choices": []any{map[string]any{"message": map[string]any{
			"role": "assistant", "content": "Received.",
			"annotations": []any{map[string]any{
				"type": "file",
				"file": map[string]any{"hash": hash, "name": "document.pdf", "content": []any{
					map[string]any{"type": "text", "text": text},
				}},
			}},
		}}},
		"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 1, "cost": cost},
	}
	encoded, _ := json.Marshal(payload)
	return documentWireStub{body: string(encoded)}
}

func documentNativeStub(text string, cost float64) documentWireStub {
	payload := map[string]any{
		"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": text}}},
		"usage":   map[string]any{"cost": cost},
	}
	encoded, _ := json.Marshal(payload)
	return documentWireStub{body: string(encoded)}
}

const (
	stubPDFTextEnv  = "AFORGE_TEST_PDFTOTEXT_TEXT"
	stubPDFLogEnv   = "AFORGE_TEST_PDFTOTEXT_LOG"
	stubPDFPagesEnv = "AFORGE_TEST_PDFINFO_PAGES"
)

// stubPDFBinaries puts a scripted poppler on an otherwise empty PATH. The local
// rung must be decided by the test rather than by whatever the developer
// happens to have installed, and pdfinfo is only present when the test wants a
// counted page estimate instead of the size heuristic.
func stubPDFBinaries(t *testing.T, text string, pages int) string {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "pdftotext.log")
	scripts := map[string]string{
		"pdftotext": "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$" + stubPDFLogEnv + "\"\nprintf '%s' \"$" + stubPDFTextEnv + "\"\n",
	}
	if pages > 0 {
		scripts["pdfinfo"] = "#!/bin/sh\nprintf 'Pages:          %s\\n' \"$" + stubPDFPagesEnv + "\"\n"
	}
	for name, script := range scripts {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	t.Setenv(stubPDFLogEnv, log)
	t.Setenv(stubPDFTextEnv, text)
	t.Setenv(stubPDFPagesEnv, strconv.Itoa(pages))
	return log
}

// barePATH removes poppler entirely so the absent-local-rung branch is real.
func barePATH(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
}

func localRungRan(t *testing.T, log string) bool {
	t.Helper()
	data, err := os.ReadFile(log)
	return err == nil && strings.TrimSpace(string(data)) != ""
}

func documentToolbox(t *testing.T, wire *documentWireCapture, modalities ModalityCatalog) (*Toolbox, *Workspace) {
	t.Helper()
	space := workspace(t)
	tools := newToolboxWithMedia(space, 11, nil, nil, &MediaTools{
		Catalog: modalities, DocumentClient: wire.client(t), WorkingModel: "work/model",
	})
	return tools, space
}

func documentFixture(t *testing.T, space *Workspace, name, content string) string {
	t.Helper()
	path := filepath.Join(space.Root(), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

const (
	localLayerText = "Quarterly revenue rose across every region we serve."
	freeParseText  = "# Filing\n\nRevenue grew and the margin held steady through the quarter."
	ocrText        = "Scanned filing text recovered by optical character recognition."
)

func TestReadDocumentIsRegisteredOnlyWithADocumentClient(t *testing.T) {
	wire := &documentWireCapture{}
	tools, space := documentToolbox(t, wire, fakeModalities{})
	definitions := tools.Definitions()
	if len(definitions) != 6 {
		t.Fatalf("definitions = %d, want five base plus read_document", len(definitions))
	}
	found := false
	for _, definition := range definitions {
		if definition.Function.Name != "read_document" {
			continue
		}
		found = true
		encoded, _ := json.Marshal(definition)
		if definition.Function.Description != "Read a PDF into text. Free and local when possible; scanned documents escalate to OCR through the rail. Repeat reads are cached." {
			t.Fatalf("read_document guidance = %q", definition.Function.Description)
		}
		for _, field := range []string{`"path"`, `"pages"`, `"question"`} {
			if !strings.Contains(string(encoded), field) {
				t.Fatalf("read_document is missing %s: %s", field, encoded)
			}
		}
	}
	if !found {
		t.Fatal("read_document was not registered")
	}

	barePATH(t)
	documentFixture(t, space, "orphan.pdf", "%PDF-1.7 orphan")
	tools.media.DocumentClient = nil
	if len(tools.Definitions()) != 5 {
		t.Fatal("read_document survived a missing document client")
	}
	missing := tools.Execute(context.Background(), "read_document", `{"path":"orphan.pdf"}`)
	if !missing.IsError || !strings.Contains(missing.Content, "document parsing is not configured") {
		t.Fatalf("unconfigured read_document = %+v", missing)
	}
}

func TestReadDocumentAutoStopsAtTheLocalTextLayer(t *testing.T) {
	log := stubPDFBinaries(t, localLayerText, 0)
	wire := &documentWireCapture{}
	tools, space := documentToolbox(t, wire, fakeModalities{})
	documentFixture(t, space, "report.pdf", "%PDF-1.7 local layer")
	spendCalls := 0
	tools.media.BeforeSpend = func(context.Context, float64) error { spendCalls++; return nil }

	result := tools.Execute(context.Background(), "read_document", `{"path":"report.pdf"}`)
	if result.IsError || !strings.Contains(result.Content, localLayerText) {
		t.Fatalf("local result = %+v", result)
	}
	// The local rung is free: no provider call, no rail, no recorded usage.
	if wire.calls != 0 || spendCalls != 0 || result.Usage != (Usage{}) {
		t.Fatalf("local rung spent: calls=%d spend=%d usage=%+v", wire.calls, spendCalls, result.Usage)
	}
	if !localRungRan(t, log) {
		t.Fatal("pdftotext was never invoked")
	}
	if _, ok := space.Locate("report.extracted.md"); !ok {
		t.Fatal("extracted cache was not written beside the source")
	}
	// The cache is a real file and the bookkeeping knows about it, but it is
	// the harness's own text dump of the user's PDF — not something the job
	// wrote, and not something to name back as one of its deliverables.
	if artifacts := space.Artifacts(11); len(artifacts) != 0 {
		t.Fatalf("artifacts = %v, want the extraction cache held out of the job's own output", artifacts)
	}
}

func TestReadDocumentAutoFallsFromEmptyLocalToTheFreeParser(t *testing.T) {
	log := stubPDFBinaries(t, "   \f  \n ", 0)
	wire := &documentWireCapture{responses: []documentWireStub{documentAnnotationStub("free-hash", freeParseText, 0)}}
	tools, space := documentToolbox(t, wire, fakeModalities{})
	documentFixture(t, space, "scan.pdf", "%PDF-1.7 no text layer")

	result := tools.Execute(context.Background(), "read_document", `{"path":"scan.pdf"}`)
	if result.IsError || !strings.Contains(result.Content, "the margin held steady") {
		t.Fatalf("free result = %+v", result)
	}
	if !localRungRan(t, log) {
		t.Fatal("auto skipped the local rung")
	}
	if got := wire.engines(); len(got) != 1 || got[0] != string(provider.DocumentParseCloudflare) {
		t.Fatalf("engines = %v, want a single free parse", got)
	}
	if wire.prompt(t, 0) != "Acknowledge receipt." {
		t.Fatalf("free parse asked the model to echo the document: %q", wire.prompt(t, 0))
	}
	if result.Usage.Calls != 1 || result.Usage.PromptTokens != 10 {
		t.Fatalf("free usage = %+v", result.Usage)
	}
}

func TestReadDocumentAutoEscalatesANearEmptyFreeParseToGatedOCR(t *testing.T) {
	stubPDFBinaries(t, "", 7)
	wire := &documentWireCapture{responses: []documentWireStub{
		documentAnnotationStub("free-hash", "page 3", 0),
		documentAnnotationStub("ocr-hash", ocrText, 0.02),
	}}
	tools, space := documentToolbox(t, wire, fakeModalities{})
	documentFixture(t, space, "scanned.pdf", "%PDF-1.7 scanned pages")
	anticipated, gatedAtCall := -1.0, -1
	tools.media.BeforeSpend = func(_ context.Context, amount float64) error {
		anticipated, gatedAtCall = amount, wire.calls
		return nil
	}

	result := tools.Execute(context.Background(), "read_document", `{"path":"scanned.pdf"}`)
	if result.IsError || !strings.Contains(result.Content, ocrText) {
		t.Fatalf("ocr result = %+v", result)
	}
	if got := wire.engines(); len(got) != 2 ||
		got[0] != string(provider.DocumentParseCloudflare) || got[1] != string(provider.DocumentParseMistralOCR) {
		t.Fatalf("engines = %v, want free then OCR", got)
	}
	// pdfinfo counted seven pages, so the rail saw the anticipated OCR bill
	// before the paid call rather than after it.
	if want := 7 * mistralOCRUSDPerPage; anticipated != want || gatedAtCall != 1 {
		t.Fatalf("anticipated = %v (want %v) at call %d", anticipated, want, gatedAtCall)
	}
	if result.Usage.Calls != 2 || result.Usage.Cost != 0.02 {
		t.Fatalf("escalated usage = %+v", result.Usage)
	}
}

func TestReadDocumentOCRRailRefusalIsCalmAndCachesNothing(t *testing.T) {
	stubPDFBinaries(t, "", 7)
	wire := &documentWireCapture{responses: []documentWireStub{documentAnnotationStub("free-hash", "page 3", 0)}}
	tools, space := documentToolbox(t, wire, fakeModalities{})
	documentFixture(t, space, "scanned.pdf", "%PDF-1.7 scanned pages")
	tools.media.BeforeSpend = func(context.Context, float64) error { return errors.New("rail") }

	result := tools.Execute(context.Background(), "read_document", `{"path":"scanned.pdf"}`)
	if !result.IsError || !strings.Contains(result.Content, "paused at the daily budget") ||
		strings.Contains(result.Content, "\n") || result.Usage != (Usage{}) {
		t.Fatalf("gated result = %+v", result)
	}
	if wire.calls != 1 {
		t.Fatalf("OCR escaped the rail: calls = %d", wire.calls)
	}
	if _, ok := space.Locate("scanned.extracted.md"); ok {
		t.Fatal("a refused read left a cache behind")
	}
}

func TestReadDocumentPinnedEnginesSkipTheLadderAndFailHonestly(t *testing.T) {
	t.Run("local without poppler", func(t *testing.T) {
		barePATH(t)
		wire := &documentWireCapture{}
		tools, space := documentToolbox(t, wire, fakeModalities{})
		tools.media.DocumentEngine = documentEngineLocal
		documentFixture(t, space, "pinned.pdf", "%PDF-1.7 pinned")
		result := tools.Execute(context.Background(), "read_document", `{"path":"pinned.pdf"}`)
		if !result.IsError || !strings.Contains(result.Content, "pdftotext (poppler) on PATH") || wire.calls != 0 {
			t.Fatalf("pinned local = %+v calls=%d", result, wire.calls)
		}
	})

	t.Run("local with a text layer", func(t *testing.T) {
		// Three requested pages raise the density threshold threefold, so the
		// fixture has to carry a page's worth of text for each of them.
		log := stubPDFBinaries(t, strings.Repeat(localLayerText+"\f", 3), 0)
		wire := &documentWireCapture{}
		tools, space := documentToolbox(t, wire, fakeModalities{})
		tools.media.DocumentEngine = documentEngineLocal
		documentFixture(t, space, "pinned.pdf", "%PDF-1.7 pinned")
		result := tools.Execute(context.Background(), "read_document", `{"path":"pinned.pdf","pages":"2-4"}`)
		if result.IsError || !strings.Contains(result.Content, localLayerText) || wire.calls != 0 {
			t.Fatalf("pinned local = %+v calls=%d", result, wire.calls)
		}
		data, err := os.ReadFile(log)
		if err != nil || !strings.Contains(string(data), "-f 2 -l 4") {
			t.Fatalf("page range was not passed to pdftotext: %q err=%v", data, err)
		}
	})

	t.Run("local on a scan", func(t *testing.T) {
		stubPDFBinaries(t, "  \f ", 0)
		wire := &documentWireCapture{}
		tools, space := documentToolbox(t, wire, fakeModalities{})
		tools.media.DocumentEngine = documentEngineLocal
		documentFixture(t, space, "pinned.pdf", "%PDF-1.7 pinned")
		result := tools.Execute(context.Background(), "read_document", `{"path":"pinned.pdf"}`)
		if !result.IsError || !strings.Contains(result.Content, "no usable local text layer") || wire.calls != 0 {
			t.Fatalf("pinned local scan = %+v calls=%d", result, wire.calls)
		}
	})

	t.Run("free never escalates", func(t *testing.T) {
		log := stubPDFBinaries(t, localLayerText, 0)
		wire := &documentWireCapture{responses: []documentWireStub{documentAnnotationStub("free-hash", "page 3", 0)}}
		tools, space := documentToolbox(t, wire, fakeModalities{})
		tools.media.DocumentEngine = documentEngineFree
		documentFixture(t, space, "pinned.pdf", "%PDF-1.7 pinned")
		spendCalls := 0
		tools.media.BeforeSpend = func(context.Context, float64) error { spendCalls++; return nil }
		result := tools.Execute(context.Background(), "read_document", `{"path":"pinned.pdf"}`)
		if !result.IsError || !strings.Contains(result.Content, "the free parser found no readable text") {
			t.Fatalf("pinned free = %+v", result)
		}
		if got := wire.engines(); len(got) != 1 || got[0] != string(provider.DocumentParseCloudflare) || spendCalls != 0 {
			t.Fatalf("pinned free engines = %v spend=%d", got, spendCalls)
		}
		if localRungRan(t, log) {
			t.Fatal("a pinned free rung still ran pdftotext")
		}
	})

	t.Run("ocr goes straight to the rail", func(t *testing.T) {
		log := stubPDFBinaries(t, localLayerText, 3)
		wire := &documentWireCapture{responses: []documentWireStub{documentAnnotationStub("ocr-hash", ocrText, 0.006)}}
		tools, space := documentToolbox(t, wire, fakeModalities{})
		tools.media.DocumentEngine = documentEngineOCR
		documentFixture(t, space, "pinned.pdf", "%PDF-1.7 pinned")
		anticipated, gatedAtCall := -1.0, -1
		tools.media.BeforeSpend = func(_ context.Context, amount float64) error {
			anticipated, gatedAtCall = amount, wire.calls
			return nil
		}
		result := tools.Execute(context.Background(), "read_document", `{"path":"pinned.pdf"}`)
		if result.IsError || !strings.Contains(result.Content, ocrText) {
			t.Fatalf("pinned ocr = %+v", result)
		}
		if got := wire.engines(); len(got) != 1 || got[0] != string(provider.DocumentParseMistralOCR) {
			t.Fatalf("pinned ocr engines = %v", got)
		}
		if want := 3 * mistralOCRUSDPerPage; anticipated != want || gatedAtCall != 0 {
			t.Fatalf("anticipated = %v (want %v) at call %d", anticipated, want, gatedAtCall)
		}
		if result.Usage.Calls != 1 || result.Usage.Cost != 0.006 {
			t.Fatalf("pinned ocr usage = %+v", result.Usage)
		}
		if localRungRan(t, log) {
			t.Fatal("a pinned OCR rung still ran pdftotext")
		}
	})

	t.Run("unknown rung", func(t *testing.T) {
		barePATH(t)
		wire := &documentWireCapture{}
		tools, space := documentToolbox(t, wire, fakeModalities{})
		tools.media.DocumentEngine = "tesseract"
		documentFixture(t, space, "pinned.pdf", "%PDF-1.7 pinned")
		result := tools.Execute(context.Background(), "read_document", `{"path":"pinned.pdf"}`)
		if !result.IsError || !strings.Contains(result.Content, `unknown engine "tesseract"`) || wire.calls != 0 {
			t.Fatalf("unknown rung = %+v", result)
		}
	})
}

func TestReadDocumentCacheIsInstantUntilTheSourceHashMoves(t *testing.T) {
	barePATH(t)
	wire := &documentWireCapture{responses: []documentWireStub{
		documentAnnotationStub("hash-1", freeParseText, 0),
		documentAnnotationStub("hash-2", "Revised filing: the margin narrowed sharply this quarter.", 0),
		documentAnnotationStub("hash-3", "Pages two through four of the revised filing text.", 0),
	}}
	tools, space := documentToolbox(t, wire, fakeModalities{})
	tools.media.DocumentEngine = documentEngineFree
	source := documentFixture(t, space, "filing.pdf", "%PDF-1.7 first revision")

	first := tools.Execute(context.Background(), "read_document", `{"path":"filing.pdf"}`)
	if first.IsError || wire.calls != 1 {
		t.Fatalf("first read = %+v calls=%d", first, wire.calls)
	}
	repeat := tools.Execute(context.Background(), "read_document", `{"path":"filing.pdf"}`)
	if repeat.IsError || wire.calls != 1 || repeat.Content != first.Content {
		t.Fatalf("cached read = %+v calls=%d", repeat, wire.calls)
	}
	// A cache hit is free: no parser call means no usage to attribute.
	if repeat.Usage != (Usage{}) {
		t.Fatalf("cache hit recorded usage: %+v", repeat.Usage)
	}

	if err := os.WriteFile(source, []byte("%PDF-1.7 second revision"), 0o644); err != nil {
		t.Fatal(err)
	}
	stale := tools.Execute(context.Background(), "read_document", `{"path":"filing.pdf"}`)
	if stale.IsError || wire.calls != 2 || !strings.Contains(stale.Content, "the margin narrowed sharply") {
		t.Fatalf("stale re-extract = %+v calls=%d", stale, wire.calls)
	}

	// A different page selection is a different extraction, not a cache hit.
	ranged := tools.Execute(context.Background(), "read_document", `{"path":"filing.pdf","pages":"2-4"}`)
	if ranged.IsError || wire.calls != 3 {
		t.Fatalf("page-scoped read = %+v calls=%d", ranged, wire.calls)
	}
}

func TestReadDocumentCapsSizeFormatAndPageRangeBeforeSpending(t *testing.T) {
	barePATH(t)
	wire := &documentWireCapture{}
	tools, space := documentToolbox(t, wire, fakeModalities{})
	spendCalls := 0
	tools.media.BeforeSpend = func(context.Context, float64) error { spendCalls++; return nil }
	documentFixture(t, space, "notes.txt", "plain text")
	documentFixture(t, space, "deck.pptx", "pptx bytes")
	documentFixture(t, space, "small.pdf", "%PDF-1.7 small")
	oversized := filepath.Join(space.Root(), "huge.pdf")
	file, err := os.Create(oversized)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxDocumentInputBytes + 1); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name      string
		arguments string
		want      string
	}{
		{name: "size cap", arguments: `{"path":"huge.pdf"}`, want: "over the 25 MB document limit"},
		{name: "format cap", arguments: `{"path":"notes.txt"}`, want: "supports pdf, docx, and pptx"},
		{name: "missing file", arguments: `{"path":"absent.pdf"}`, want: "could not read absent.pdf"},
		{name: "malformed pages", arguments: `{"path":"small.pdf","pages":"chapter two"}`, want: "pages must look like 1-5"},
		{name: "inverted pages", arguments: `{"path":"small.pdf","pages":"9-2"}`, want: "must end at or after page 9"},
		{name: "pages on office", arguments: `{"path":"deck.pptx","pages":"1-2"}`, want: "page ranges are supported for PDF files"},
		{name: "no path", arguments: `{}`, want: "read_document needs path"},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := tools.Execute(context.Background(), "read_document", test.arguments)
			if !result.IsError || !strings.Contains(result.Content, test.want) {
				t.Fatalf("result = %+v, want %q", result, test.want)
			}
		})
	}
	if wire.calls != 0 || spendCalls != 0 {
		t.Fatalf("a capped read still reached the provider: calls=%d spend=%d", wire.calls, spendCalls)
	}
}

func TestReadDocumentOfficeFilesUseFreeNativeOrACalmRefusal(t *testing.T) {
	t.Run("free parse", func(t *testing.T) {
		barePATH(t)
		wire := &documentWireCapture{responses: []documentWireStub{documentAnnotationStub("docx-hash", freeParseText, 0)}}
		tools, space := documentToolbox(t, wire, fakeModalities{})
		documentFixture(t, space, "memo.docx", "docx bytes")
		result := tools.Execute(context.Background(), "read_document", `{"path":"memo.docx","question":"What changed?"}`)
		if result.IsError || !strings.Contains(result.Content, "the margin held steady") {
			t.Fatalf("docx free = %+v", result)
		}
		if got := wire.engines(); len(got) != 1 || got[0] != string(provider.DocumentParseCloudflare) {
			t.Fatalf("docx engines = %v", got)
		}
		// The question is a native-only affordance: an annotation harvest must
		// stay a receipt acknowledgement so the document is never echoed back.
		if prompt := wire.prompt(t, 0); prompt != "Acknowledge receipt." {
			t.Fatalf("free parse prompt = %q", prompt)
		}
	})

	t.Run("native carries the question", func(t *testing.T) {
		barePATH(t)
		wire := &documentWireCapture{responses: []documentWireStub{documentNativeStub("Native extraction of the deck body text.", 0.03)}}
		tools, space := documentToolbox(t, wire, fakeModalities{"work/model:input:file": true})
		documentFixture(t, space, "deck.pptx", "pptx bytes")
		gatedAtCall := -1
		tools.media.BeforeSpend = func(_ context.Context, amount float64) error {
			if amount != 0 {
				t.Errorf("native anticipated spend = %v, want token-billed zero", amount)
			}
			gatedAtCall = wire.calls
			return nil
		}
		result := tools.Execute(context.Background(), "read_document", `{"path":"deck.pptx","question":"Which quarter grew fastest?"}`)
		if result.IsError || !strings.Contains(result.Content, "Native extraction") || result.Usage.Cost != 0.03 {
			t.Fatalf("native result = %+v", result)
		}
		if got := wire.engines(); len(got) != 1 || got[0] != string(provider.DocumentParseNative) || gatedAtCall != 0 {
			t.Fatalf("native engines = %v gated_at=%d", got, gatedAtCall)
		}
		if prompt := wire.prompt(t, 0); !strings.Contains(prompt, "Which quarter grew fastest?") {
			t.Fatalf("native prompt dropped the question: %q", prompt)
		}
	})

	t.Run("pinned rungs that cannot serve office files", func(t *testing.T) {
		barePATH(t)
		wire := &documentWireCapture{}
		tools, space := documentToolbox(t, wire, fakeModalities{})
		documentFixture(t, space, "memo.docx", "docx bytes")
		for _, engine := range []string{documentEngineLocal, documentEngineOCR} {
			tools.media.DocumentEngine = engine
			result := tools.Execute(context.Background(), "read_document", `{"path":"memo.docx"}`)
			if !result.IsError || !strings.Contains(result.Content, "only supports PDF files") || wire.calls != 0 {
				t.Fatalf("%s docx = %+v calls=%d", engine, result, wire.calls)
			}
		}
	})

	t.Run("refused by the parser", func(t *testing.T) {
		barePATH(t)
		wire := &documentWireCapture{responses: []documentWireStub{{
			status: http.StatusBadRequest, body: `{"error":{"message":"file type not supported"}}`,
		}}}
		tools, space := documentToolbox(t, wire, fakeModalities{})
		documentFixture(t, space, "memo.docx", "docx bytes")
		result := tools.Execute(context.Background(), "read_document", `{"path":"memo.docx"}`)
		if !result.IsError || !strings.Contains(result.Content, "only PDF is reliably supported") ||
			!strings.Contains(result.Content, "docx") || strings.Contains(result.Content, "\n") {
			t.Fatalf("refused docx = %+v", result)
		}
		if wire.calls != 1 {
			t.Fatalf("refusal retried the parser: calls = %d", wire.calls)
		}
	})
}

func TestEstimatePDFPagesPrefersPdfinfoAndFallsBackToSize(t *testing.T) {
	stubPDFBinaries(t, "", 12)
	if got := estimatePDFPages(context.Background(), "any.pdf", 1); got != 12 {
		t.Fatalf("counted pages = %d, want the pdfinfo answer", got)
	}
	barePATH(t)
	for _, test := range []struct {
		size int64
		want int
	}{
		{size: 0, want: 1},
		{size: 1, want: 1},
		{size: estimatedPDFBytesPerPage, want: 1},
		{size: estimatedPDFBytesPerPage + 1, want: 2},
		{size: 10 * estimatedPDFBytesPerPage, want: 10},
	} {
		if got := estimatePDFPages(context.Background(), "any.pdf", test.size); got != test.want {
			t.Fatalf("estimate(%d bytes) = %d, want %d", test.size, got, test.want)
		}
	}
}

// A question on a PDF cannot shape the harvest (only office documents route it
// natively); the result must say so rather than silently ignoring it.
func TestReadDocumentPDFQuestionGetsHonestNote(t *testing.T) {
	stubPDFBinaries(t, localLayerText, 0)
	wire := &documentWireCapture{}
	tools, space := documentToolbox(t, wire, fakeModalities{})
	documentFixture(t, space, "guide.pdf", "%PDF-1.7 guide")
	result := tools.Execute(context.Background(), "read_document",
		`{"path":"guide.pdf","question":"what is the total?"}`)
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "question applies only to docx/pptx") {
		t.Fatalf("ignored question must be declared:\n%s", result.Content)
	}
	plain := tools.Execute(context.Background(), "read_document", `{"path":"guide.pdf"}`)
	if plain.IsError || strings.Contains(plain.Content, "question applies") {
		t.Fatalf("note leaked without a question:\n%s", plain.Content)
	}
}
