package router

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/home"
)

// catalogTTL is how long a fetched catalog is trusted. Prices and context
// windows move on the scale of weeks, so a day is generous; the point of the
// cache is not freshness but that a run must never fail because a metadata
// endpoint was down.
const catalogTTL = 24 * time.Hour

// Catalog is what the provider says about the panel: prices, context windows,
// and the parameters each model claims to accept.
//
// The claims are recorded and not believed. The probe lab asked all three of its
// models for logprobs, all three advertised support in `supported_parameters`,
// and one of them returned null on every request. So this file is used for
// economics — price is a real number and the cascade's ordering needs it — and
// never as a capability check. What a model can actually do is measured.
type Catalog struct {
	Fetched time.Time               `json:"fetched"`
	Models  map[string]CatalogEntry `json:"models"`
}

// CatalogEntry is one model's advertised economics.
type CatalogEntry struct {
	Slug          string   `json:"slug"`
	PromptPrice   float64  `json:"prompt_price"` // $/M input tokens
	OutputPrice   float64  `json:"output_price"` // $/M output tokens
	ContextLength int      `json:"context_length,omitempty"`
	Parameters    []string `json:"supported_parameters,omitempty"`
}

// LoadCatalog returns the panel's economics, preferring a fetch and falling back
// to whatever was cached.
//
// The order matters and so does the tolerance. A stale price makes the cascade
// order slightly wrong; a hard failure here makes the harness refuse to start
// because a metadata endpoint was slow. So every failure is soft: fetch, fall
// back to the cache however old it is, and fall back again to knowing nothing,
// which the router handles by ordering the panel as the operator wrote it.
func LoadCatalog(dir, baseURL, apiKey string, client *http.Client) *Catalog {
	path, err := statePath(dir, "catalog.json")
	if err != nil {
		path = ""
	}
	cached := readCatalog(path)
	if cached != nil && time.Since(cached.Fetched) < catalogTTL {
		return cached
	}
	fetched, err := fetchCatalog(baseURL, apiKey, client)
	if err != nil {
		return cached
	}
	if path != "" {
		writeCatalog(path, fetched)
	}
	return fetched
}

// Entry looks a model up, tolerating OpenRouter's floating-alias prefix: the
// catalog lists `deepseek/deepseek-v4-flash-latest` while the configured slug is
// `~deepseek/deepseek-v4-flash-latest`, and a lookup that missed on the tilde
// would silently price the model at zero.
func (c *Catalog) Entry(slug string) (CatalogEntry, bool) {
	if c == nil {
		return CatalogEntry{}, false
	}
	if entry, ok := c.Models[slug]; ok {
		return entry, true
	}
	entry, ok := c.Models[strings.TrimPrefix(slug, "~")]
	return entry, ok
}

func fetchCatalog(baseURL, apiKey string, client *http.Client) (*Catalog, error) {
	endpoint := strings.TrimSuffix(strings.TrimSpace(baseURL), "/") + "/models"
	request, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	if apiKey != "" {
		request.Header.Set("Authorization", "Bearer "+apiKey)
	}
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode >= 400 {
		return nil, fmt.Errorf("catalog: HTTP %d", response.StatusCode)
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	var body struct {
		Data []struct {
			ID            string `json:"id"`
			ContextLength int    `json:"context_length"`
			Pricing       struct {
				Prompt     string `json:"prompt"`
				Completion string `json:"completion"`
			} `json:"pricing"`
			SupportedParameters []string `json:"supported_parameters"`
		} `json:"data"`
	}
	if err := json.Unmarshal(payload, &body); err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	catalog := &Catalog{Fetched: time.Now(), Models: make(map[string]CatalogEntry, len(body.Data))}
	for _, item := range body.Data {
		catalog.Models[item.ID] = CatalogEntry{
			Slug:          item.ID,
			PromptPrice:   perMillion(item.Pricing.Prompt),
			OutputPrice:   perMillion(item.Pricing.Completion),
			ContextLength: item.ContextLength,
			Parameters:    item.SupportedParameters,
		}
	}
	if len(catalog.Models) == 0 {
		return nil, errors.New("catalog: no models returned")
	}
	return catalog, nil
}

// perMillion converts the provider's per-token decimal string. An unparseable
// price is zero rather than an error: one bad row must not cost the whole
// catalog, and a zero price is handled downstream as "unknown".
func perMillion(value string) float64 {
	price, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil {
		return 0
	}
	return price * 1e6
}

func readCatalog(path string) *Catalog {
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var catalog Catalog
	if err := json.Unmarshal(data, &catalog); err != nil || len(catalog.Models) == 0 {
		return nil
	}
	return &catalog
}

func writeCatalog(path string, catalog *Catalog) {
	data, err := json.MarshalIndent(catalog, "", "  ")
	if err != nil {
		return
	}
	_ = replaceFile(path, data)
}

// statePath resolves where the router keeps what it has learned. It follows the
// profile package exactly — AFORGE_PROFILE_DIR when set, ~/.aforge otherwise —
// because a run's memory should live in one directory, not two.
func statePath(dir, name string) (string, error) {
	if strings.TrimSpace(dir) == "" {
		dir = home.Dir()
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, name), nil
}

// replaceFile writes through a temporary file and renames over the target, so a
// reader — another aforge process, or this one after a crash — never sees a
// half-written state file. A partial catalog would be read back as a set of
// free models.
func replaceFile(path string, data []byte) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Chmod(name, 0o644); err != nil {
		return err
	}
	return os.Rename(name, path)
}
