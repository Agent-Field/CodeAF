package session

// read_document tests: the OCR rung, without a socket.
//
// The PDFs are the ones tools_pdf_test.go builds byte by byte, for the reason
// that file opens with — a checked-in binary is a fixture nobody can review —
// and the office and image files are deliberately NOT real documents: nothing
// on this path parses them, the parser is scripted, and what the tests are
// actually about is which rung was called with which media type.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── helpers ─────────────────────────────────────────────────────────────────

// scriptedParser is the document rung without a network. It records every
// request so the tests can assert the wire shape, and answers per engine so a
// walk down the rungs can be scripted rung by rung.
type scriptedParser struct {
	mu   sync.Mutex
	seen []provider.DocumentRequest
	// answers is the text each engine returns. An engine that is absent from
	// the map returns nothing, which is how a rung is made to fail its way down
	// to the next one.
	answers map[provider.DocumentParseEngine]string
	usage   *ai.Usage
	err     error
}

func (p *scriptedParser) ParseDocument(_ context.Context, request provider.DocumentRequest) (*provider.DocumentResponse, error) {
	p.mu.Lock()
	p.seen = append(p.seen, request)
	p.mu.Unlock()
	if p.err != nil {
		return nil, p.err
	}
	return &provider.DocumentResponse{Text: p.answers[request.Engine], Usage: p.usage}, nil
}

func (p *scriptedParser) requests() []provider.DocumentRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]provider.DocumentRequest(nil), p.seen...)
}

func (p *scriptedParser) request(t *testing.T, index int) provider.DocumentRequest {
	t.Helper()
	seen := p.requests()
	if index >= len(seen) {
		t.Fatalf("the parser saw %d requests, not %d", len(seen), index+1)
	}
	return seen[index]
}

// newDocumentAgent wires a session whose document rung is the scripted parser.
//
// The factory var is what is swapped, not a config field, because the DEFAULT
// construction is half of what this seam is: a test that could only inject an
// interface would never exercise the fact that the rung is built from the
// session's own key.
func newDocumentAgent(t *testing.T, parser DocumentParser, buildErr error, mutate func(*Config)) (*Agent, string) {
	t.Helper()
	original := newDocClient
	newDocClient = func(Config) (DocumentParser, error) { return parser, buildErr }
	t.Cleanup(func() { newDocClient = original })
	return newTestAgent(t, &scriptedCompleter{}, mutate)
}

func documentTool(t *testing.T, agent *Agent, arguments map[string]any) (string, bool) {
	t.Helper()
	encoded, err := json.Marshal(arguments)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return runTool(t, agent, "read_document", string(encoded))
}

func withEngine(engine string) func(*Config) {
	return func(config *Config) { config.DocumentEngine = engine }
}

// ── (1) the rung the pdf refusal names actually exists ──────────────────────

// The whole bug, in one test. read says "use read_document"; if the belt does
// not carry a read_document, the model does what it did in the field and
// installs an OCR library over bash.
func TestTheScannedPDFRefusalNamesAToolTheBeltCarries(t *testing.T) {
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, nil)
	name := dropFile(t, workspace, "scan.pdf", scannedPDF())

	text, isError := readTool(t, agent, map[string]any{"path": name})
	if isError {
		t.Fatalf("a scanned pdf is information, not a tool failure: %s", text)
	}
	if !strings.Contains(text, "read_document") {
		t.Fatalf("the scanned result should name the rung as a tool: %q", text)
	}
	if strings.Contains(text, "ladder") {
		t.Fatalf("the refusal still names a ladder rather than a hand: %q", text)
	}
	if !hasTool(agent, "read_document") {
		t.Fatalf("read is pointing at a tool the belt does not carry: %v", beltNames(agent))
	}
}

// The rung is on the belt with nothing wired, unlike web_search and
// generate_image: it rides the session's own credentials, so absence is a
// misconfiguration to report and not a capability to withhold.
func TestReadDocumentIsOnTheBeltUnconditionally(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	if !hasTool(agent, "read_document") {
		t.Fatalf("belt = %v", beltNames(agent))
	}
	tool := beltTool(t, agent, "read_document")
	if !strings.Contains(tool.Description, "scanned PDF") ||
		!strings.Contains(tool.Description, "office document") {
		t.Fatalf("the description should teach WHEN: %q", tool.Description)
	}
}

// ── (2) the wire shape ──────────────────────────────────────────────────────

// What the rung is handed: the file's own name, its bytes, the media type its
// extension claims, and the engine the settings row chose.
func TestReadDocumentSendsTheFileWithItsMediaTypeAndTheConfiguredRung(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		file      string
		bytes     []byte
		engine    string
		wantType  string
		wantRung  provider.DocumentParseEngine
		wantModel string
	}{
		{
			name: "a scan on the ocr rung", file: "scan.pdf", bytes: scannedPDF(), engine: "ocr",
			wantType: "application/pdf", wantRung: provider.DocumentParseMistralOCR, wantModel: "test/model",
		},
		{
			name: "a word document on the free rung", file: "notes.docx", bytes: []byte("PK\x03\x04 docx"), engine: "free",
			wantType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
			wantRung: provider.DocumentParseCloudflare, wantModel: "test/model",
		},
		{
			name: "a photographed page on auto", file: "page.jpg", bytes: []byte("\xff\xd8\xff jpeg"), engine: "",
			wantType: "image/jpeg", wantRung: provider.DocumentParseNative, wantModel: "test/model",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			parser := &scriptedParser{answers: map[provider.DocumentParseEngine]string{
				testCase.wantRung: "INVOICE 4471\nTotal due: 92.00",
			}}
			agent, workspace := newDocumentAgent(t, parser, nil, withEngine(testCase.engine))
			name := dropFile(t, workspace, testCase.file, testCase.bytes)

			text, isError := documentTool(t, agent, map[string]any{"path": name})
			if isError {
				t.Fatalf("read_document failed: %s", text)
			}
			if !strings.Contains(text, "INVOICE 4471") {
				t.Fatalf("the extracted text should be the answer: %q", text)
			}
			if !strings.Contains(text, string(testCase.wantRung)) {
				t.Fatalf("the result should name the rung that read it: %q", text)
			}

			request := parser.request(t, 0)
			if request.Filename != testCase.file {
				t.Fatalf("filename = %q, want %q", request.Filename, testCase.file)
			}
			if request.MediaType != testCase.wantType {
				t.Fatalf("media type = %q, want %q", request.MediaType, testCase.wantType)
			}
			if request.Engine != testCase.wantRung {
				t.Fatalf("engine = %q, want %q", request.Engine, testCase.wantRung)
			}
			if request.Model != testCase.wantModel {
				t.Fatalf("model = %q, want the session's own %q", request.Model, testCase.wantModel)
			}
			if string(request.Data) != string(testCase.bytes) {
				t.Fatalf("the rung was handed %d bytes, the file has %d", len(request.Data), len(testCase.bytes))
			}
		})
	}
}

// Auto walks: the chat model's own file handling first, and down to the paid
// parsers only when the rung above comes back with nothing.
func TestAutoWalksNativeThenFreeThenOCR(t *testing.T) {
	parser := &scriptedParser{answers: map[provider.DocumentParseEngine]string{
		provider.DocumentParseMistralOCR: "PAGE ONE\nthe scanned words, at last",
	}}
	agent, workspace := newDocumentAgent(t, parser, nil, withEngine("auto"))
	name := dropFile(t, workspace, "scan.pdf", scannedPDF())

	text, isError := documentTool(t, agent, map[string]any{"path": name})
	if isError {
		t.Fatalf("read_document failed: %s", text)
	}
	if !strings.Contains(text, "the scanned words") {
		t.Fatalf("the OCR rung's text should be the answer: %q", text)
	}
	walked := parser.requests()
	if len(walked) != 3 {
		t.Fatalf("auto called %d rungs, want native, free, then ocr", len(walked))
	}
	for index, want := range []provider.DocumentParseEngine{
		provider.DocumentParseNative, provider.DocumentParseCloudflare, provider.DocumentParseMistralOCR,
	} {
		if walked[index].Engine != want {
			t.Fatalf("rung %d = %q, want %q", index, walked[index].Engine, want)
		}
	}
}

// The question is the person's, and it rides the one rung that can hear it. A
// parser rung harvests annotations without a model turn, so a question handed to
// one would be dropped silently — and the result says so instead.
func TestTheQuestionRidesTheNativeRungAndIsDeclaredWhenItCannot(t *testing.T) {
	native := &scriptedParser{answers: map[provider.DocumentParseEngine]string{
		provider.DocumentParseNative: "Total due: 92.00",
	}}
	agent, workspace := newDocumentAgent(t, native, nil, withEngine("native"))
	name := dropFile(t, workspace, "invoice.pdf", scannedPDF())
	if _, isError := documentTool(t, agent, map[string]any{"path": name, "question": "what is the total?"}); isError {
		t.Fatal("read_document failed on the native rung")
	}
	if got := native.request(t, 0).Question; got != "what is the total?" {
		t.Fatalf("native question = %q, want the person's", got)
	}

	parser := &scriptedParser{answers: map[provider.DocumentParseEngine]string{
		provider.DocumentParseMistralOCR: "Total due: 92.00",
	}}
	ocrAgent, ocrWorkspace := newDocumentAgent(t, parser, nil, withEngine("ocr"))
	scan := dropFile(t, ocrWorkspace, "invoice.pdf", scannedPDF())
	text, isError := documentTool(t, ocrAgent, map[string]any{"path": scan, "question": "what is the total?"})
	if isError {
		t.Fatalf("read_document failed: %s", text)
	}
	if got := parser.request(t, 0).Question; got != "" {
		t.Fatalf("the ocr rung was handed a question it cannot hear: %q", got)
	}
	if !strings.Contains(text, "shapes only the native rung") {
		t.Fatalf("a dropped question must be said out loud: %q", text)
	}
}

// ── (3) what it costs, and who it lands on ──────────────────────────────────

// A document is paid for out of the same pocket as a title or a generated
// picture: the session's total, and no turn's ([Agent.addAuxiliaryUsage]).
func TestDocumentUsageFoldsIntoTheSessionTotal(t *testing.T) {
	cost := 0.02
	parser := &scriptedParser{
		answers: map[provider.DocumentParseEngine]string{provider.DocumentParseMistralOCR: "the scanned words"},
		usage:   &ai.Usage{PromptTokens: 40, CompletionTokens: 6, Cost: &cost},
	}
	agent, workspace := newDocumentAgent(t, parser, nil, withEngine("ocr"))
	name := dropFile(t, workspace, "scan.pdf", scannedPDF())

	if _, isError := documentTool(t, agent, map[string]any{"path": name}); isError {
		t.Fatal("read_document failed")
	}
	usage := agent.Usage()
	if usage.Input != 40 || usage.Output != 6 || usage.CostUSD != cost {
		t.Fatalf("session usage = %+v, want the rung's 40/6 tokens and $%.2f", usage, cost)
	}
	if usage.Turns != 0 {
		t.Fatalf("session Turns = %d; reading a document is not a step of the conversation", usage.Turns)
	}
}

// Paging is free. The rung runs once, and offset walks the extraction it
// returned — because paying a per-page OCR bill again to show line 3 is the one
// place this ladder would be robbing somebody.
func TestPagingReusesTheExtractionRatherThanTheRung(t *testing.T) {
	parser := &scriptedParser{answers: map[provider.DocumentParseEngine]string{
		provider.DocumentParseMistralOCR: "one\ntwo\nthree\nfour",
	}}
	agent, workspace := newDocumentAgent(t, parser, nil, withEngine("ocr"))
	name := dropFile(t, workspace, "scan.pdf", scannedPDF())

	first, isError := documentTool(t, agent, map[string]any{"path": name, "limit": 2})
	if isError {
		t.Fatalf("read_document failed: %s", first)
	}
	if !strings.Contains(first, "one\ntwo") || strings.Contains(first, "three") {
		t.Fatalf("limit should cut the extraction: %q", first)
	}
	if !strings.Contains(first, "offset=3") {
		t.Fatalf("pi's footer should say how to continue: %q", first)
	}
	second, isError := documentTool(t, agent, map[string]any{"path": name, "offset": 3})
	if isError {
		t.Fatalf("read_document failed: %s", second)
	}
	if !strings.Contains(second, "three\nfour") {
		t.Fatalf("offset should page the same extraction: %q", second)
	}
	if calls := len(parser.requests()); calls != 1 {
		t.Fatalf("the rung ran %d times for two pages of one document", calls)
	}
}

// ── (4) the guards against paying for what is free ──────────────────────────

// A text file is read's, and saying so costs nothing. Sending it to a paid rung
// would be the model misreading which hand this is, not finding a limit.
func TestPlainTextIsSentBackToRead(t *testing.T) {
	parser := &scriptedParser{answers: map[provider.DocumentParseEngine]string{
		provider.DocumentParseNative: "should never be reached",
	}}
	agent, workspace := newDocumentAgent(t, parser, nil, nil)
	name := dropFile(t, workspace, "notes.txt", []byte("the quick brown fox\njumped over it\n"))

	text, isError := documentTool(t, agent, map[string]any{"path": name})
	if !isError {
		t.Fatalf("a wrong-hand call is a tool error the model can act on: %q", text)
	}
	if !strings.Contains(text, "the plain read handles this") {
		t.Fatalf("the guard should name the hand that does this free: %q", text)
	}
	if calls := len(parser.requests()); calls != 0 {
		t.Fatalf("a text file reached the paid rungs %d times", calls)
	}
}

// And a PDF that turns out to HAVE a text layer is extracted locally rather
// than parsed remotely: same law, one file type along.
func TestAPDFWithATextLayerNeverReachesAPaidRung(t *testing.T) {
	parser := &scriptedParser{answers: map[provider.DocumentParseEngine]string{
		provider.DocumentParseNative: "should never be reached",
	}}
	agent, workspace := newDocumentAgent(t, parser, nil, withEngine("auto"))
	name := dropFile(t, workspace, "memo.pdf",
		onePageTextPDF("Section 4 begins here and runs for several honest lines of text"))

	text, isError := documentTool(t, agent, map[string]any{"path": name})
	if isError {
		t.Fatalf("read_document failed: %s", text)
	}
	if !strings.Contains(text, "Section 4 begins here") {
		t.Fatalf("the local rung's text should be the answer: %q", text)
	}
	if !strings.Contains(text, "local rung") {
		t.Fatalf("the result should say which rung read it: %q", text)
	}
	if calls := len(parser.requests()); calls != 0 {
		t.Fatalf("a PDF with a text layer reached the paid rungs %d times", calls)
	}
}

// A file no rung reads is refused by name rather than uploaded and answered
// with somebody's 400.
func TestAnUnsupportedFileIsRefusedByName(t *testing.T) {
	parser := &scriptedParser{}
	agent, workspace := newDocumentAgent(t, parser, nil, nil)
	name := dropFile(t, workspace, "archive.zip", []byte("PK\x03\x04\x00\x00binary\x00stuff"))

	text, isError := documentTool(t, agent, map[string]any{"path": name})
	if !isError {
		t.Fatalf("an unreadable format is a tool error: %q", text)
	}
	if !strings.Contains(text, "archive.zip") || !strings.Contains(text, "office documents") {
		t.Fatalf("the refusal should name the file and what is readable: %q", text)
	}
	if calls := len(parser.requests()); calls != 0 {
		t.Fatalf("an unsupported file reached the rungs %d times", calls)
	}
}

// ── (5) honest failure ──────────────────────────────────────────────────────

// A rung that fails is a RESULT that names it. "Document parsing failed" tells
// a model to try again; the rung's name and its reason tell it — and the person
// reading the transcript — what to do instead.
func TestAFailedRungIsAResultThatNamesIt(t *testing.T) {
	parser := &scriptedParser{err: errors.New("402 insufficient credits")}
	agent, workspace := newDocumentAgent(t, parser, nil, withEngine("ocr"))
	name := dropFile(t, workspace, "scan.pdf", scannedPDF())

	text, isError := documentTool(t, agent, map[string]any{"path": name})
	if !isError {
		t.Fatalf("a failed rung should be an error result: %q", text)
	}
	for _, want := range []string{"scan.pdf", "mistral-ocr", "insufficient credits"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the failure should say %q: %q", want, text)
		}
	}
}

// Every rung silent is the same shape, and every rung is named in the order it
// was tried.
func TestARungThatReturnsNothingFallsThroughAndIsNamed(t *testing.T) {
	parser := &scriptedParser{answers: map[provider.DocumentParseEngine]string{}}
	agent, workspace := newDocumentAgent(t, parser, nil, withEngine("auto"))
	name := dropFile(t, workspace, "scan.pdf", scannedPDF())

	text, isError := documentTool(t, agent, map[string]any{"path": name})
	if !isError {
		t.Fatalf("no text from any rung is an error result: %q", text)
	}
	for _, want := range []string{"native", "cloudflare-ai", "mistral-ocr", "no readable text"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the failure should name %q: %q", want, text)
		}
	}
}

// A client that could not be built is a sentence about THIS SESSION's
// credentials, because that is the thing that is actually wrong.
func TestAnUnbuildableClientIsAResultAboutTheSessionsKey(t *testing.T) {
	agent, workspace := newDocumentAgent(t, nil, errors.New("this session has no API key"), nil)
	name := dropFile(t, workspace, "scan.pdf", scannedPDF())

	text, isError := documentTool(t, agent, map[string]any{"path": name})
	if !isError {
		t.Fatalf("an unreachable rung is an error result: %q", text)
	}
	if !strings.Contains(text, "no API key") || !strings.Contains(text, "API key and base URL") {
		t.Fatalf("the refusal should point at the session's own credentials: %q", text)
	}
}

// A settings row that switches the rungs off says which row, in the words the
// person set it with.
func TestTheEngineRowsThatHaveNoRungRefuseByName(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		engine string
		file   string
		bytes  []byte
		want   string
	}{
		{"local is the read tool", "local", "scan.pdf", scannedPDF(), "the local rung is the read tool itself"},
		{"ocr does not read images", "ocr", "page.png", []byte("\x89PNG\r\n\x1a\n"), "the OCR parser reads PDFs"},
		{"free does not read images", "free", "page.png", []byte("\x89PNG\r\n\x1a\n"), "rather than images"},
		{"an unknown row", "sideways", "scan.pdf", scannedPDF(), "which is not a rung"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			parser := &scriptedParser{}
			agent, workspace := newDocumentAgent(t, parser, nil, withEngine(testCase.engine))
			name := dropFile(t, workspace, testCase.file, testCase.bytes)

			text, isError := documentTool(t, agent, map[string]any{"path": name})
			if !isError {
				t.Fatalf("a rung that is switched off is an error result: %q", text)
			}
			if !strings.Contains(text, testCase.want) {
				t.Fatalf("result %q does not say %q", text, testCase.want)
			}
			if calls := len(parser.requests()); calls != 0 {
				t.Fatalf("a refused combination still called %d rungs", calls)
			}
		})
	}
}

// A file that is not there is bare's sentence about a missing file, not a rung's
// about a failed parse.
func TestAMissingFileIsNotARungFailure(t *testing.T) {
	parser := &scriptedParser{}
	agent, _ := newDocumentAgent(t, parser, nil, nil)

	text, isError := documentTool(t, agent, map[string]any{"path": "nowhere.pdf"})
	if !isError {
		t.Fatalf("a missing file is an error result: %q", text)
	}
	if !strings.Contains(text, "could not read nowhere.pdf") {
		t.Fatalf("result %q should name the file", text)
	}
	if calls := len(parser.requests()); calls != 0 {
		t.Fatalf("a missing file reached the rungs %d times", calls)
	}
}

// ── which model reads a picture ─────────────────────────────────────────────
//
// The native rung IS a model's eyes ([documentRungs] gives a photographed page
// exactly one rung), so on a blind chat model it has to be the LOOKING model's
// eyes or it is nobody's. This is the third place that question is asked, and
// all three ask [Agent.visionSeer] (docs/MULTIMODAL.md, Decision 8).

func TestAPictureIsReadByTheLookingModelWhenTheSessionCannotSee(t *testing.T) {
	parser := &scriptedParser{answers: map[provider.DocumentParseEngine]string{
		provider.DocumentParseNative: "INVOICE 3319 — total 412.00",
	}}
	agent, workspace := newDocumentAgent(t, parser, nil, func(config *Config) {
		config.SupportsImages = func(model string) bool { return model == "vendor/slot-eyes" }
		config.MediaModel = func(modality string) string {
			if modality == "vision" {
				return "vendor/slot-eyes"
			}
			return ""
		}
	})
	name := dropFile(t, workspace, "receipt.png", []byte("\x89PNG\r\n\x1a\n"))

	text, isError := documentTool(t, agent, map[string]any{"path": name})
	if isError {
		t.Fatalf("read_document failed on a picture: %q", text)
	}
	if got := parser.request(t, 0).Model; got != "vendor/slot-eyes" {
		t.Fatalf("the picture went to %q, want the looking model", got)
	}
}

// A session that can already see keeps its own model: routing on a capability
// nobody needed would send a picture to a second model for no reason, and the
// answer would be paid for twice as often as it has to be.
func TestAPictureStaysOnTheSessionModelWhenItCanSee(t *testing.T) {
	parser := &scriptedParser{answers: map[provider.DocumentParseEngine]string{
		provider.DocumentParseNative: "INVOICE 3319 — total 412.00",
	}}
	agent, workspace := newDocumentAgent(t, parser, nil, func(config *Config) {
		config.SupportsImages = func(string) bool { return true }
		config.MediaModel = func(string) string { return "vendor/slot-eyes" }
	})
	name := dropFile(t, workspace, "receipt.png", []byte("\x89PNG\r\n\x1a\n"))

	if text, isError := documentTool(t, agent, map[string]any{"path": name}); isError {
		t.Fatalf("read_document failed on a picture: %q", text)
	}
	if got := parser.request(t, 0).Model; got != "test/model" {
		t.Fatalf("the picture went to %q, want the session's own model", got)
	}
}

// And a PDF is a file the parsers read without eyes, so nothing about the
// looking slot reaches it.
func TestADocumentIsNeverRoutedToTheLookingModel(t *testing.T) {
	parser := &scriptedParser{answers: map[provider.DocumentParseEngine]string{
		provider.DocumentParseNative: "PAGE ONE — the quarterly figures, in full sentences",
	}}
	agent, workspace := newDocumentAgent(t, parser, nil, func(config *Config) {
		config.SupportsImages = func(string) bool { return false }
		config.MediaModel = func(string) string { return "vendor/slot-eyes" }
	})
	name := dropFile(t, workspace, "scan.pdf", scannedPDF())

	if text, isError := documentTool(t, agent, map[string]any{"path": name}); isError {
		t.Fatalf("read_document failed on a scan: %q", text)
	}
	if got := parser.request(t, 0).Model; got != "test/model" {
		t.Fatalf("the scan went to %q, want the session's own model", got)
	}
}
