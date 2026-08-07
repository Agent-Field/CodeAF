// Package catalog owns OpenRouter model discovery across every modality.
// Callers ask narrow capability questions; fetching, TTLs, and offline
// fallbacks stay behind this seam so chat, graph tools, and future voice input
// do not grow separate model caches.
package catalog

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/guard"
)

const (
	TTL             = 24 * time.Hour
	maxCatalogBytes = 16 << 20
	cacheName       = "model-catalog.json"
)

// Model is the small, durable part of one OpenRouter catalog row. Pricing is
// display-ready economics for the existing picker; architecture is retained
// verbatim for modality queries.
type Model struct {
	ID               string   `json:"id"`
	Name             string   `json:"name,omitempty"`
	PromptPrice      float64  `json:"prompt_price,omitempty"`
	CompletionPrice  float64  `json:"completion_price,omitempty"`
	RequestPrice     float64  `json:"request_price,omitempty"`
	InputModalities  []string `json:"input_modalities,omitempty"`
	OutputModalities []string `json:"output_modalities,omitempty"`
}

type cache struct {
	FetchedAt time.Time `json:"fetched_at"`
	Models    []Model   `json:"models"`
}

// Options describes the one catalog fetch. Dir is the Aforge configuration
// directory (AFORGE_PROFILE_DIR when configured, ~/.aforge otherwise).
type Options struct {
	BaseURL    string
	APIKey     string
	Dir        string
	HTTPClient *http.Client
	Now        func() time.Time
}

// Catalog is immutable once resolved and therefore safe to share among the
// head, executor leaves, and the terminal lens. A lazily loaded catalog holds
// the fetch as a future instead: the value is handed out immediately and the
// first capability question waits, if anything still has to wait at all.
type Catalog struct {
	ready   *rows
	resolve func() *rows
}

// rows is one resolved catalog: the cleaned model list every listing walks,
// beside the index every single-model question is answered from. Building the
// index once turns each Supports call from a scan of the whole catalog into a
// lookup, which matters because the palette asks it per candidate.
type rows struct {
	models []Model
	byID   map[string]Model
}

// Load fetches at most once. A fresh cache avoids I/O; a failed fetch degrades
// to a stale cache, then to a very small set of known modality defaults.
func Load(ctx context.Context, options Options) *Catalog {
	return &Catalog{ready: loadOrFallback(ctx, options)}
}

// loadOrFallback is the only way a catalog is resolved, because a fault in
// discovery must degrade the way a failed fetch does — to the known defaults —
// rather than escape. Inside a sync.OnceValue it would escape twice over: once
// on the warming goroutine, and again on whichever caller first asked a
// capability question, since the future replays the panic to every reader.
func loadOrFallback(ctx context.Context, options Options) (resolved *rows) {
	defer func() {
		if recovered := recover(); recovered != nil {
			_ = guard.Note("catalog/load", recovered)
			resolved = newRows(hardcodedFallbacks())
		}
	}()
	return load(ctx, options)
}

// LoadLazy starts the same discovery immediately but never makes the caller
// wait for it. On a cold cache the fetch is a network round-trip with a
// fifteen-second ceiling, and a launch path that awaits it holds the first
// frame behind a dead terminal. Nothing a catalog answers can be asked before
// the surface is up, so the goroutine warms the value while the caller carries
// on, and only a question that genuinely arrives first ever blocks.
func LoadLazy(ctx context.Context, options Options) *Catalog {
	resolve := sync.OnceValue(func() *rows { return loadOrFallback(ctx, options) })
	guard.Go("catalog/warm", func() { resolve() })
	return &Catalog{resolve: resolve}
}

func load(ctx context.Context, options Options) *rows {
	now := time.Now
	if options.Now != nil {
		now = options.Now
	}
	path := cachePath(options.Dir)
	cached, cachedOK := readCache(path)
	if cachedOK && now().Before(cached.FetchedAt.Add(TTL)) {
		return newRows(cached.Models)
	}

	models, err := fetch(ctx, options)
	if err == nil && len(models) > 0 {
		if path != "" {
			_ = writeCache(path, cache{FetchedAt: now().UTC(), Models: models})
		}
		return newRows(models)
	}
	if cachedOK {
		return newRows(cached.Models)
	}
	return newRows(hardcodedFallbacks())
}

// rows resolves the catalog, waiting on the future when Load was lazy.
func (c *Catalog) rows() *rows {
	if c == nil {
		return nil
	}
	if c.ready != nil {
		return c.ready
	}
	if c.resolve != nil {
		return c.resolve()
	}
	return nil
}

// ModelsWithInput returns a stable copy of models advertising modality.
func (c *Catalog) ModelsWithInput(modality string) []Model {
	return c.modelsWith("input", modality)
}

// ModelsWithOutput returns a stable copy of models advertising modality.
func (c *Catalog) ModelsWithOutput(modality string) []Model {
	return c.modelsWith("output", modality)
}

// Model returns one catalog row by slug. The returned slices do not alias the
// immutable catalog, so callers may safely retain or amend the result.
func (c *Catalog) Model(modelID string) (Model, bool) {
	resolved := c.rows()
	if resolved == nil {
		return Model{}, false
	}
	model, ok := resolved.byID[normalizeID(modelID)]
	if !ok {
		return Model{}, false
	}
	return cloneModel(model), true
}

// Supports answers whether modelID advertises modality in direction. Unknown
// models and directions calmly return false.
func (c *Catalog) Supports(modelID, direction, modality string) bool {
	resolved := c.rows()
	if resolved == nil {
		return false
	}
	model, ok := resolved.byID[normalizeID(modelID)]
	if !ok {
		return false
	}
	var values []string
	switch strings.ToLower(strings.TrimSpace(direction)) {
	case "input":
		values = model.InputModalities
	case "output":
		values = model.OutputModalities
	default:
		return false
	}
	return hasModality(values, modality)
}

func (c *Catalog) modelsWith(direction, modality string) []Model {
	resolved := c.rows()
	if resolved == nil {
		return nil
	}
	models := make([]Model, 0)
	for _, model := range resolved.models {
		var values []string
		if direction == "input" {
			values = model.InputModalities
		} else {
			values = model.OutputModalities
		}
		if hasModality(values, modality) {
			models = append(models, cloneModel(model))
		}
	}
	return models
}

func hasModality(values []string, requested string) bool {
	requested = strings.ToLower(strings.TrimSpace(requested))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == requested {
			return true
		}
		// OpenRouter currently describes synthesized sound as either audio or
		// speech across model families, while music models may say music or the
		// broader audio. Keep that provider vocabulary behind the catalog seam so
		// callers can ask stable capability questions.
		if requested == "speech" && value == "audio" {
			return true
		}
		if requested == "music" && value == "audio" {
			return true
		}
	}
	return false
}

func fetch(ctx context.Context, options Options) ([]Model, error) {
	endpoint := strings.TrimSuffix(strings.TrimSpace(options.BaseURL), "/") + "/models?output_modalities=all"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	if key := strings.TrimSpace(options.APIKey); key != "" {
		request.Header.Set("Authorization", "Bearer "+key)
	}
	client := options.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4<<10))
		return nil, &statusError{status: response.Status}
	}
	var payload struct {
		Data []struct {
			ID           string `json:"id"`
			Name         string `json:"name"`
			Architecture struct {
				Input  []string `json:"input_modalities"`
				Output []string `json:"output_modalities"`
			} `json:"architecture"`
			Pricing struct {
				Prompt     string `json:"prompt"`
				Completion string `json:"completion"`
				Request    string `json:"request"`
			} `json:"pricing"`
		} `json:"data"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxCatalogBytes))
	if err := decoder.Decode(&payload); err != nil {
		return nil, err
	}
	models := make([]Model, 0, len(payload.Data))
	for _, item := range payload.Data {
		models = append(models, Model{
			ID: strings.TrimSpace(item.ID), Name: strings.TrimSpace(item.Name),
			PromptPrice: parsePrice(item.Pricing.Prompt), CompletionPrice: parsePrice(item.Pricing.Completion),
			RequestPrice:    parsePrice(item.Pricing.Request),
			InputModalities: cleanModalities(item.Architecture.Input), OutputModalities: cleanModalities(item.Architecture.Output),
		})
	}
	// The one cleaning pass for the fetched path; what is cached and what is
	// indexed are the same cleaned rows.
	models = cleanModels(models)
	if len(models) == 0 {
		return nil, &statusError{status: "empty catalog"}
	}
	return models, nil
}

type statusError struct{ status string }

func (e *statusError) Error() string { return "model catalog: " + e.status }

// newRows indexes an already-cleaned model list. Every path into it — the
// fetch, the cache read, the built-in fallbacks — has cleaned its own rows, so
// cleaning runs exactly once per catalog rather than once per hand-off.
//
// The index keeps the first row for each normalized id, which is what a scan
// from the top of the list would have found: cleaning dedupes on the literal
// id, so a slug and its "~" variant can both survive it.
func newRows(models []Model) *rows {
	byID := make(map[string]Model, len(models))
	for _, model := range models {
		id := normalizeID(model.ID)
		if _, seen := byID[id]; !seen {
			byID[id] = model
		}
	}
	return &rows{models: models, byID: byID}
}

func cleanModels(models []Model) []Model {
	seen := make(map[string]bool, len(models))
	cleaned := make([]Model, 0, len(models))
	for _, model := range models {
		model.ID = strings.TrimSpace(model.ID)
		if model.ID == "" || seen[model.ID] {
			continue
		}
		seen[model.ID] = true
		model.InputModalities = cleanModalities(model.InputModalities)
		model.OutputModalities = cleanModalities(model.OutputModalities)
		cleaned = append(cleaned, model)
	}
	return cleaned
}

func cleanModalities(values []string) []string {
	seen := make(map[string]bool, len(values))
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		cleaned = append(cleaned, value)
	}
	return cleaned
}

func cloneModel(model Model) Model {
	model.InputModalities = append([]string(nil), model.InputModalities...)
	model.OutputModalities = append([]string(nil), model.OutputModalities...)
	return model
}

func normalizeID(id string) string { return strings.TrimPrefix(strings.TrimSpace(id), "~") }

func parsePrice(raw string) float64 {
	price, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || price < 0 {
		return 0
	}
	return price
}

func cachePath(dir string) string {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, ".aforge")
	}
	return filepath.Join(dir, cacheName)
}

func readCache(path string) (cache, bool) {
	if path == "" {
		return cache{}, false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return cache{}, false
	}
	var cached cache
	if json.Unmarshal(raw, &cached) != nil || cached.FetchedAt.IsZero() {
		return cache{}, false
	}
	// The one cleaning pass for the cached path — an older cache may predate a
	// vocabulary change, so its rows are normalized here and nowhere else.
	cached.Models = cleanModels(cached.Models)
	return cached, len(cached.Models) > 0
}

func writeCache(path string, cached cache) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(cached, "", "  ")
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if _, err := temporary.Write(raw); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Chmod(name, 0o600); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// hardcodedFallbacks is written already cleaned — unique ids, lowercase
// modalities — so it satisfies newRows without a cleaning pass of its own.
func hardcodedFallbacks() []Model {
	return []Model{
		{ID: "krea/krea-2-medium-turbo", InputModalities: []string{"text", "image"}, OutputModalities: []string{"image"}},
		{ID: "hexgrad/kokoro-82m", InputModalities: []string{"text"}, OutputModalities: []string{"speech"}},
		{ID: "openai/gpt-4o-mini-tts", InputModalities: []string{"text"}, OutputModalities: []string{"speech"}},
		{ID: "google/lyria-3-clip-preview", InputModalities: []string{"text"}, OutputModalities: []string{"music"}, RequestPrice: 0.04},
		{ID: "bytedance/seedance-1-5-pro", InputModalities: []string{"text", "image"}, OutputModalities: []string{"video"}},
	}
}
