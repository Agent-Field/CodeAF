package provider

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

const (
	// A parser-backed request needs a completion only so OpenRouter returns the
	// parsed annotations. Keeping the acknowledgement tiny prevents the model
	// from echoing a document we already receive losslessly in metadata.
	documentParserMaxTokens = 8
	// Native file handling has no annotation channel, so the model itself must
	// emit the extracted text. This is a ceiling rather than expected spend.
	documentNativeMaxTokens  = 16 << 10
	maxDocumentResponseBytes = 128 << 20
)

// DocumentParseEngine is one verified OpenRouter file-parser engine. Callers
// must always choose one: omitting the plugin would silently select the
// provider's native-then-OCR fallback and make cost an accident.
type DocumentParseEngine string

const (
	DocumentParseCloudflare DocumentParseEngine = "cloudflare-ai"
	DocumentParseMistralOCR DocumentParseEngine = "mistral-ocr"
	DocumentParseNative     DocumentParseEngine = "native"
)

type DocumentRequest struct {
	Model     string
	Filename  string
	MediaType string
	Data      []byte
	Engine    DocumentParseEngine
	Question  string
}

type DocumentResponse struct {
	Text  string
	Hash  string
	Usage *ai.Usage
}

type documentWireFile struct {
	Filename string `json:"filename"`
	FileData string `json:"file_data"`
}

type documentWirePart struct {
	Type string            `json:"type"`
	Text string            `json:"text,omitempty"`
	File *documentWireFile `json:"file,omitempty"`
}

type documentWireMessage struct {
	Role    string             `json:"role"`
	Content []documentWirePart `json:"content"`
}

type documentPlugin struct {
	ID  string `json:"id"`
	PDF struct {
		Engine DocumentParseEngine `json:"engine"`
	} `json:"pdf"`
}

type documentWireRequest struct {
	Model               string                `json:"model"`
	Messages            []documentWireMessage `json:"messages"`
	Plugins             []documentPlugin      `json:"plugins"`
	MaxTokens           *int                  `json:"max_tokens,omitempty"`
	MaxCompletionTokens *int                  `json:"max_completion_tokens,omitempty"`
	Usage               ai.RequestUsage       `json:"usage"`
}

type documentAnnotation struct {
	Type string `json:"type"`
	File struct {
		Hash    string `json:"hash"`
		Name    string `json:"name,omitempty"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text,omitempty"`
		} `json:"content"`
	} `json:"file"`
}

type documentWireResponse struct {
	Choices []struct {
		Message struct {
			Content     json.RawMessage      `json:"content"`
			Annotations []documentAnnotation `json:"annotations"`
		} `json:"message"`
	} `json:"choices"`
	Usage *ai.Usage `json:"usage,omitempty"`
	Error struct {
		Metadata struct {
			FileAnnotations []documentAnnotation `json:"file_annotations"`
		} `json:"metadata"`
	} `json:"error"`
}

// ParseDocument performs the deliberately-small completion that activates
// OpenRouter's file parser and harvests text from file annotations. Native is
// the sole exception: OpenRouter produces no annotations for native files, so
// the model is explicitly asked to return extracted text.
func (c *Client) ParseDocument(ctx context.Context, request DocumentRequest) (*DocumentResponse, error) {
	if c == nil {
		return nil, fmt.Errorf("document client is nil")
	}
	model := strings.TrimSpace(request.Model)
	if model == "" {
		model = c.config.Model
	}
	if strings.TrimSpace(request.Filename) == "" {
		return nil, fmt.Errorf("document filename is required")
	}
	if strings.TrimSpace(request.MediaType) == "" {
		return nil, fmt.Errorf("document media type is required")
	}
	if len(request.Data) == 0 {
		return nil, fmt.Errorf("document data is empty")
	}
	switch request.Engine {
	case DocumentParseCloudflare, DocumentParseMistralOCR, DocumentParseNative:
	default:
		return nil, fmt.Errorf("unknown document parser engine %q", request.Engine)
	}

	prompt := "Acknowledge receipt."
	maxTokens := documentParserMaxTokens
	if request.Engine == DocumentParseNative {
		prompt = "Extract all readable document text. Preserve headings, lists, and table contents. Return only the extracted text."
		if question := strings.TrimSpace(request.Question); question != "" {
			prompt += "\nThe downstream worker will answer this question from the extraction: " + question
		}
		maxTokens = documentNativeMaxTokens
		if c.config.MaxTokens > 0 {
			maxTokens = c.config.MaxTokens
		}
	}

	plugin := documentPlugin{ID: "file-parser"}
	plugin.PDF.Engine = request.Engine
	wire := documentWireRequest{
		Model: model,
		Messages: []documentWireMessage{{Role: "user", Content: []documentWirePart{
			{Type: "text", Text: prompt},
			{Type: "file", File: &documentWireFile{
				Filename: filepath.Base(request.Filename),
				FileData: "data:" + request.MediaType + ";base64," + base64.StdEncoding.EncodeToString(request.Data),
			}},
		}}},
		Plugins: []documentPlugin{plugin},
		Usage:   ai.RequestUsage{Include: true},
	}
	if needsMaxCompletionTokens(model) && isVouchedRewriteEndpoint(c.config.BaseURL) {
		wire.MaxCompletionTokens = &maxTokens
	} else {
		wire.MaxTokens = &maxTokens
	}
	body, err := json.Marshal(wire)
	if err != nil {
		return nil, fmt.Errorf("marshal document request: %w", err)
	}
	metadata := &ai.Request{Model: model, MaxTokens: &maxTokens}
	httpResponse, err := c.send(ctx, metadata, body, false)
	if err != nil {
		return nil, err
	}
	defer httpResponse.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(httpResponse.Body, maxDocumentResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read document response: %w", err)
	}
	if len(payload) > maxDocumentResponseBytes {
		return nil, fmt.Errorf("document response exceeded %d bytes", maxDocumentResponseBytes)
	}
	var decoded documentWireResponse
	if err := json.Unmarshal(payload, &decoded); err != nil {
		if httpResponse.StatusCode >= 400 {
			return nil, apiError(httpResponse.StatusCode, payload)
		}
		return nil, fmt.Errorf("decode document response: %w", err)
	}

	if request.Engine != DocumentParseNative {
		annotations := make([]documentAnnotation, 0)
		for _, choice := range decoded.Choices {
			annotations = append(annotations, choice.Message.Annotations...)
		}
		annotations = append(annotations, decoded.Error.Metadata.FileAnnotations...)
		text, hash := documentAnnotationText(annotations)
		if strings.TrimSpace(text) != "" {
			return &DocumentResponse{Text: text, Hash: hash, Usage: decoded.Usage}, nil
		}
		if httpResponse.StatusCode >= 400 {
			return nil, apiError(httpResponse.StatusCode, payload)
		}
		return nil, fmt.Errorf("document parser returned no file annotations")
	}

	if httpResponse.StatusCode >= 400 {
		return nil, apiError(httpResponse.StatusCode, payload)
	}
	for _, choice := range decoded.Choices {
		if text := documentMessageText(choice.Message.Content); strings.TrimSpace(text) != "" {
			return &DocumentResponse{Text: text, Usage: decoded.Usage}, nil
		}
	}
	return nil, fmt.Errorf("native document model returned no extracted text")
}

func documentAnnotationText(annotations []documentAnnotation) (string, string) {
	seen := make(map[string]bool)
	var text []string
	firstHash := ""
	for _, annotation := range annotations {
		hash := strings.TrimSpace(annotation.File.Hash)
		if annotation.Type != "file" || hash == "" || seen[hash] {
			continue
		}
		seen[hash] = true
		if firstHash == "" {
			firstHash = hash
		}
		for _, part := range annotation.File.Content {
			if part.Type == "text" && strings.TrimSpace(part.Text) != "" {
				text = append(text, strings.TrimSpace(part.Text))
			}
		}
	}
	return strings.Join(text, "\n\n"), firstHash
}

func documentMessageText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return strings.TrimSpace(text)
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) != nil {
		return ""
	}
	var kept []string
	for _, part := range parts {
		if part.Type == "text" && strings.TrimSpace(part.Text) != "" {
			kept = append(kept, strings.TrimSpace(part.Text))
		}
	}
	return strings.Join(kept, "\n")
}
