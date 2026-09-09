package provider

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestParseDocumentUsesExplicitEngineTinyCompletionAndHarvestsAnnotations(t *testing.T) {
	var body map[string]any
	client := handlerClient(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v1/chat/completions" {
			t.Fatalf("path = %q", request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer document-key" {
			t.Errorf("auth = %q", request.Header.Get("Authorization"))
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{
			"model":"cheap/model",
			"choices":[{"message":{"role":"assistant","content":"Received.","annotations":[{
				"type":"file","file":{"hash":"parsed-hash-1","name":"doc.pdf","content":[
					{"type":"text","text":"# First section"},
					{"type":"image_url","image_url":{"url":"data:image/png;base64,aW1hZ2U="}},
					{"type":"text","text":"Second section body."}
				]}
			}]}}],
			"usage":{"prompt_tokens":12,"completion_tokens":1,"total_tokens":13,"cost":0.004}
		}`)
	}))
	adapter, err := NewClient(Config{
		APIKey: "document-key", BaseURL: "https://openrouter.ai/api/v1", Model: "cheap/model", HTTPClient: client,
	})
	if err != nil {
		t.Fatal(err)
	}
	response, err := adapter.ParseDocument(context.Background(), DocumentRequest{
		Model: "cheap/model", Filename: "doc.pdf", MediaType: "application/pdf",
		Data: []byte("pdf bytes"), Engine: DocumentParseCloudflare,
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Text != "# First section\n\nSecond section body." || response.Hash != "parsed-hash-1" ||
		response.Usage == nil || response.Usage.Cost == nil || *response.Usage.Cost != 0.004 {
		t.Fatalf("response = %+v", response)
	}
	// NO CEILING TRAVELS ON THIS ROUTE EITHER. It used to send max_tokens 8 —
	// enough for "received" and not for a model that thinks first — and what
	// keeps the acknowledgement small now is the prompt.
	if body["model"] != "cheap/model" {
		t.Fatalf("model = %#v", body)
	}
	if _, capped := body["max_tokens"]; capped {
		t.Fatalf("the parser route sent an output cap: %#v", body)
	}
	plugins, _ := body["plugins"].([]any)
	plugin, _ := plugins[0].(map[string]any)
	pdf, _ := plugin["pdf"].(map[string]any)
	if plugin["id"] != "file-parser" || pdf["engine"] != "cloudflare-ai" {
		t.Fatalf("plugins = %#v", plugins)
	}
	messages, _ := body["messages"].([]any)
	message, _ := messages[0].(map[string]any)
	parts, _ := message["content"].([]any)
	textPart, _ := parts[0].(map[string]any)
	filePart, _ := parts[1].(map[string]any)
	file, _ := filePart["file"].(map[string]any)
	if textPart["text"] != "Acknowledge receipt." || filePart["type"] != "file" || file["filename"] != "doc.pdf" {
		t.Fatalf("content = %#v", parts)
	}
	dataURL, _ := file["file_data"].(string)
	const prefix = "data:application/pdf;base64,"
	if !strings.HasPrefix(dataURL, prefix) {
		t.Fatalf("file_data = %q", dataURL)
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(dataURL, prefix))
	if err != nil || string(decoded) != "pdf bytes" {
		t.Fatalf("decoded file = %q err=%v", decoded, err)
	}
	usage, _ := body["usage"].(map[string]any)
	if include, _ := usage["include"].(bool); !include {
		t.Fatalf("usage = %#v", usage)
	}
}

func TestParseDocumentNativePassesQuestionAndReturnsModelExtraction(t *testing.T) {
	var body map[string]any
	client := handlerClient(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_ = json.NewDecoder(request.Body).Decode(&body)
		_, _ = io.WriteString(writer, `{"choices":[{"message":{"role":"assistant","content":"Native extracted text with enough detail."}}],"usage":{"cost":0.02}}`)
	}))
	adapter, err := NewClient(Config{
		APIKey: "document-key", BaseURL: "https://openrouter.ai/api/v1", Model: "native/model", HTTPClient: client,
	})
	if err != nil {
		t.Fatal(err)
	}
	response, err := adapter.ParseDocument(context.Background(), DocumentRequest{
		Filename: "slides.pptx", MediaType: "application/vnd.openxmlformats-officedocument.presentationml.presentation",
		Data: []byte("slides"), Engine: DocumentParseNative, Question: "Which quarter grew fastest?",
	})
	if err != nil || response.Text != "Native extracted text with enough detail." {
		t.Fatalf("response = %+v err=%v", response, err)
	}
	plugins, _ := body["plugins"].([]any)
	plugin, _ := plugins[0].(map[string]any)
	pdf, _ := plugin["pdf"].(map[string]any)
	// The native route sends no ceiling at all. Extraction has to fit a whole
	// document's text, and the old 16k figure was this file guessing how much of
	// somebody's PDF a model it has never seen would need to write out.
	if pdf["engine"] != "native" {
		t.Fatalf("native body = %#v", body)
	}
	for _, knob := range []string{"max_tokens", "max_completion_tokens"} {
		if _, capped := body[knob]; capped {
			t.Fatalf("the native route sent %s: %#v", knob, body)
		}
	}
	messages, _ := body["messages"].([]any)
	message, _ := messages[0].(map[string]any)
	parts, _ := message["content"].([]any)
	prompt, _ := parts[0].(map[string]any)
	if text := prompt["text"].(string); !strings.Contains(text, "Extract all readable document text") ||
		!strings.Contains(text, "Which quarter grew fastest?") {
		t.Fatalf("native prompt = %q", text)
	}
}

// OpenRouter sometimes returns the parsed file alongside an error envelope
// rather than a choice. The parse succeeded and was billed either way, so the
// annotations are still the answer.
func TestParseDocumentHarvestsAnnotationsFromErrorMetadataAndDeduplicates(t *testing.T) {
	client := handlerClient(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(writer, `{
			"choices":[{"message":{"role":"assistant","content":null,"annotations":[]}}],
			"error":{"metadata":{"file_annotations":[
				{"type":"file","file":{"hash":"h1","content":[{"type":"text","text":"Body text."}]}},
				{"type":"file","file":{"hash":"h1","content":[{"type":"text","text":"Body text."}]}},
				{"type":"file","file":{"hash":"h2","content":[{"type":"text","text":"Second file."}]}}
			]}}
		}`)
	}))
	adapter, err := NewClient(Config{
		APIKey: "document-key", BaseURL: "https://openrouter.ai/api/v1", Model: "cheap/model", HTTPClient: client,
	})
	if err != nil {
		t.Fatal(err)
	}
	response, err := adapter.ParseDocument(context.Background(), DocumentRequest{
		Filename: "doc.pdf", MediaType: "application/pdf", Data: []byte("pdf"), Engine: DocumentParseMistralOCR,
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Text != "Body text.\n\nSecond file." || response.Hash != "h1" {
		t.Fatalf("harvested = %+v", response)
	}
}

func TestParseDocumentRequiresAnExplicitEngineAndRealInput(t *testing.T) {
	client := handlerClient(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("an invalid document request reached the provider")
	}))
	adapter, err := NewClient(Config{
		APIKey: "document-key", BaseURL: "https://openrouter.ai/api/v1", Model: "cheap/model", HTTPClient: client,
	})
	if err != nil {
		t.Fatal(err)
	}
	valid := DocumentRequest{Filename: "doc.pdf", MediaType: "application/pdf", Data: []byte("pdf"), Engine: DocumentParseCloudflare}
	for name, mutate := range map[string]func(DocumentRequest) DocumentRequest{
		"no engine":     func(r DocumentRequest) DocumentRequest { r.Engine = ""; return r },
		"guessed":       func(r DocumentRequest) DocumentRequest { r.Engine = "pdf-text"; return r },
		"no filename":   func(r DocumentRequest) DocumentRequest { r.Filename = " "; return r },
		"no media type": func(r DocumentRequest) DocumentRequest { r.MediaType = ""; return r },
		"no data":       func(r DocumentRequest) DocumentRequest { r.Data = nil; return r },
	} {
		if _, err := adapter.ParseDocument(context.Background(), mutate(valid)); err == nil {
			t.Fatalf("%s was accepted", name)
		}
	}
}

func TestParseDocumentReportsAParserThatReturnedNothing(t *testing.T) {
	client := handlerClient(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(writer, `{"choices":[{"message":{"role":"assistant","content":"Received."}}]}`)
	}))
	adapter, err := NewClient(Config{
		APIKey: "document-key", BaseURL: "https://openrouter.ai/api/v1", Model: "cheap/model", HTTPClient: client,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.ParseDocument(context.Background(), DocumentRequest{
		Filename: "doc.pdf", MediaType: "application/pdf", Data: []byte("pdf"), Engine: DocumentParseCloudflare,
	}); err == nil || !strings.Contains(err.Error(), "no file annotations") {
		t.Fatalf("empty parse error = %v", err)
	}
}
