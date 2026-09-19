// Package embed is the RoleEmbed pin and the shipped embeddings adapter.
//
// An embeddings endpoint is not a text tier, so [roles.RoleEmbed] is a pin
// and is never Register'd. Production talks OpenAI-compatible POST
// /embeddings through the existing provider media client — no private HTTP
// client, spend tagged [roles.RoleEmbed], never [roles.RoleAuditor].
//
// When the embedder is down, [DegradedLexical] expands the query for lexical
// retrieval and labels the path degraded / discovery delayed. That is a
// fallback, not a replacement, and it must not claim the workspace was
// checked and must not file.
package embed

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/roles"
)

// The person-facing labels for the expanded-query path. Hybrid search still
// runs; these words are what UI and hits must say when vectors are absent.
const (
	LabelDegraded = "degraded"
	LabelDelayed  = "discovery delayed"
	// Version identifies this adapter's vector generation beside the model id.
	Version = "openai-compat"
)

// Embedder is the contract discover and search bind. Tests may fake it;
// production must not ship a stub that returns success with empty vectors.
type Embedder interface {
	Embed(ctx context.Context, texts []string) (vectors [][]float32, model, version string, dim int, err error)
	Available(ctx context.Context) (model string, ok bool, err error)
}

// Wire is the one provider method this adapter calls.
type Wire interface {
	Embed(ctx context.Context, request provider.EmbeddingRequest) (*provider.EmbeddingResponse, error)
}

// Account folds one embeddings call into spend. Session wires
// addAuxiliaryUsageAs(..., roles.RoleEmbed); a nil Account still embeds.
type Account func(model string, usage *ai.Usage)

// Client is the shipped adapter. Nil, or a client with no model or wire, is
// unavailable — discover reports Delayed rather than binding a stub.
type Client struct {
	wire    Wire
	model   string
	account Account
	secret  string
}

var _ Embedder = (*Client)(nil)

// New builds a production embedder. model must already be the resolved pin
// or catalog slug; this door does not invent one.
func New(wire Wire, model string, account Account) *Client {
	model = strings.TrimSpace(model)
	if wire == nil || model == "" {
		return nil
	}
	return &Client{wire: wire, model: model, account: account}
}

// WithSecret keeps the bearer out of availability errors. Inspect never prints
// the key; this is how a refusal that echoed it is rewritten.
func (c *Client) WithSecret(secret string) *Client {
	if c == nil {
		return nil
	}
	c.secret = strings.TrimSpace(secret)
	return c
}

// ResolveModel is the configurable id: the RoleEmbed pin, then the catalog's
// embeddings rows, then the slug Spark's provider actually served. Nothing
// here is TUI copy.
func ResolveModel(src roles.Source, models *catalog.Catalog) string {
	if pinned, ok := roles.Pinned(src, roles.RoleEmbed); ok {
		if capableEmbed(models, pinned) {
			return pinned
		}
	}
	if id := config.CandidateMediaModel(models, "embeddings"); id != "" {
		return id
	}
	return config.FallbackMediaModel("embeddings")
}

func capableEmbed(models *catalog.Catalog, id string) bool {
	id = strings.TrimSpace(id)
	if id == "" {
		return false
	}
	if config.EmbeddingModelID(id) {
		return true
	}
	if models == nil {
		return false
	}
	return models.Supports(id, "output", "embeddings") || models.Supports(id, "output", "embedding")
}

// Embed posts the passages and returns vectors, or an error. Success with
// empty vectors is refused here as well as on the wire.
func (c *Client) Embed(ctx context.Context, texts []string) ([][]float32, string, string, int, error) {
	if c == nil || c.wire == nil || strings.TrimSpace(c.model) == "" {
		return nil, "", "", 0, fmt.Errorf("embeddings: unavailable")
	}
	if len(texts) == 0 {
		return nil, "", "", 0, fmt.Errorf("embeddings: input is required")
	}
	ctx = provider.WithCallTag(ctx, string(roles.RoleEmbed))
	response, err := c.wire.Embed(ctx, provider.EmbeddingRequest{Model: c.model, Input: texts})
	if err != nil {
		return nil, "", "", 0, redact(err, c.secret)
	}
	if response == nil || len(response.Data) == 0 {
		return nil, "", "", 0, fmt.Errorf("embeddings: provider returned no vectors")
	}
	byIndex := map[int][]float32{}
	dim := 0
	for _, row := range response.Data {
		if len(row.Embedding) == 0 {
			return nil, "", "", 0, fmt.Errorf("embeddings: empty vector at index %d", row.Index)
		}
		if dim == 0 {
			dim = len(row.Embedding)
		}
		byIndex[row.Index] = row.Embedding
	}
	vectors := make([][]float32, len(texts))
	for i := range texts {
		vector, ok := byIndex[i]
		if !ok || len(vector) == 0 {
			return nil, "", "", 0, fmt.Errorf("embeddings: missing vector at index %d", i)
		}
		vectors[i] = vector
	}
	model := strings.TrimSpace(response.Model)
	if model == "" {
		model = c.model
	}
	if c.account != nil {
		c.account(model, response.Usage)
	}
	return vectors, model, Version, dim, nil
}

// Available inspects whether this install can embed, without printing keys.
func (c *Client) Available(ctx context.Context) (string, bool, error) {
	if c == nil || c.wire == nil || strings.TrimSpace(c.model) == "" {
		return "", false, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return c.model, false, redact(ctx.Err(), c.secret)
	}
	return c.model, true, nil
}

func redact(err error, secret string) error {
	if err == nil {
		return nil
	}
	secret = strings.TrimSpace(secret)
	if secret == "" || !strings.Contains(err.Error(), secret) {
		return err
	}
	return fmt.Errorf("%s", strings.ReplaceAll(err.Error(), secret, "[redacted]"))
}

// Retrieval is one search path: hybrid when vectors are present, or the
// labelled lexical fallback when they are not.
type Retrieval struct {
	Mode   string
	Detail string
	Terms  []string
}

// DegradedLexical is the expanded-query fallback. It is degraded capability
// when the embedder is down, not a replacement: no vectors, no "checked",
// and it does not file.
func DegradedLexical(query string) Retrieval {
	return Retrieval{
		Mode:   LabelDegraded,
		Detail: LabelDelayed,
		Terms:  ExpandQuery(query),
	}
}

// ExpandQuery turns one ask into the extra lexical terms a degraded search
// runs. It is word work only — no model, no vectors.
func ExpandQuery(query string) []string {
	seen := map[string]bool{}
	var terms []string
	add := func(word string) {
		word = strings.ToLower(strings.TrimSpace(word))
		if word == "" || stop[word] || seen[word] {
			return
		}
		seen[word] = true
		terms = append(terms, word)
	}
	for _, word := range splitWords(query) {
		add(word)
		if strings.HasSuffix(word, "s") && len(word) > 3 {
			add(strings.TrimSuffix(word, "s"))
		} else if len(word) > 3 {
			add(word + "s")
		}
	}
	return terms
}

func splitWords(query string) []string {
	return strings.FieldsFunc(query, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
}

var stop = map[string]bool{
	"a": true, "an": true, "and": true, "are": true, "as": true, "at": true,
	"be": true, "by": true, "for": true, "from": true, "in": true, "is": true,
	"it": true, "of": true, "on": true, "or": true, "that": true, "the": true,
	"this": true, "to": true, "was": true, "with": true,
}

// Checked is the word the degraded path must never say.
const Checked = "checked"
