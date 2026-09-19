package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	lanes "github.com/Agent-Field/codeaf/internal/lane"
	"github.com/Agent-Field/codeaf/internal/roles"
)

// EmbeddingRequest is the OpenAI-compatible POST /embeddings body. Input is
// one string or several; the adapter always sends an array so a single
// passage and a batch are the same shape on the wire.
type EmbeddingRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

// Embedding is one vector in the response, at the index of the passage that
// produced it.
type Embedding struct {
	Embedding []float32 `json:"embedding"`
	Index     int       `json:"index"`
}

// EmbeddingResponse is the OpenAI-compatible embeddings envelope.
type EmbeddingResponse struct {
	Data  []Embedding `json:"data"`
	Model string      `json:"model,omitempty"`
	Usage *ai.Usage   `json:"usage,omitempty"`
}

// Embed posts one OpenAI-compatible /embeddings request on the existing media
// client — same bearer, same base, same attribution, no private HTTP client.
//
// THE CALL MUST BE TAGGED. Spend for this request is [roles.RoleEmbed], and
// the tag rides the context so a caller that forgot still gets the word
// rather than an anonymous row. Empty vectors are an error, never a success:
// a stub that returns ok with nothing is the production lie the contract
// forbids.
func (c *MediaClient) Embed(ctx context.Context, request EmbeddingRequest) (*EmbeddingResponse, error) {
	if strings.TrimSpace(request.Model) == "" {
		return nil, fmt.Errorf("embeddings: model is required")
	}
	if len(request.Input) == 0 {
		return nil, fmt.Errorf("embeddings: input is required")
	}
	for i, passage := range request.Input {
		if strings.TrimSpace(passage) == "" {
			return nil, fmt.Errorf("embeddings: input %d is empty", i)
		}
	}
	if CallTagFrom(ctx) == "" {
		ctx = WithCallTag(ctx, string(roles.RoleEmbed))
	}
	if RoleFrom(ctx) == lanes.RoleUnknown {
		ctx = WithRole(ctx, lanes.RoleAuxiliary)
	}
	var response EmbeddingResponse
	headers, err := c.postJSON(ctx, "/embeddings", request, &response, request.Model)
	if err != nil {
		return nil, redactSecret(err, c.config.APIKey)
	}
	if response.Usage == nil {
		response.Usage = usageFromHeaders(headers)
	}
	if strings.TrimSpace(response.Model) == "" {
		response.Model = request.Model
	}
	if err := checkEmbeddingVectors(response.Data, len(request.Input)); err != nil {
		return nil, err
	}
	return &response, nil
}

func checkEmbeddingVectors(data []Embedding, want int) error {
	if len(data) == 0 {
		return fmt.Errorf("embeddings: provider returned no vectors")
	}
	seen := make(map[int]bool, len(data))
	dim := 0
	for _, row := range data {
		if len(row.Embedding) == 0 {
			return fmt.Errorf("embeddings: empty vector at index %d", row.Index)
		}
		if dim == 0 {
			dim = len(row.Embedding)
		} else if len(row.Embedding) != dim {
			return fmt.Errorf("embeddings: dimension mismatch at index %d", row.Index)
		}
		seen[row.Index] = true
	}
	if want > 0 && len(seen) < want {
		return fmt.Errorf("embeddings: got %d vectors, want %d", len(seen), want)
	}
	return nil
}

// redactSecret strips a bearer from an error so availability inspect and
// logs cannot print a key. A missing key is not rewritten.
func redactSecret(err error, secret string) error {
	if err == nil {
		return nil
	}
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return err
	}
	text := err.Error()
	if !strings.Contains(text, secret) {
		return err
	}
	return fmt.Errorf("%s", strings.ReplaceAll(text, secret, "[redacted]"))
}
