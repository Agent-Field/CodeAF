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
	"time"
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

// Catalog is immutable after Load and therefore safe to share among the head,
// executor leaves, and the terminal lens.
type Catalog struct {
	models []Model
}

// Load fetches at most once. A fresh cache avoids I/O; a failed fetch degrades
// to a stale cache, then to a very small set of known modality defaults.
func Load(ctx context.Context, options Options) *Catalog {
	now := time.Now
	if options.Now != nil {
		now = options.Now
	}
	path := cachePath(options.Dir)
	cached, cachedOK := readCache(path)
	if cachedOK && now().Before(cached.FetchedAt.Add(TTL)) {
		return newCatalog(cached.Models)
	}

	models, err := fetch(ctx, options)
	if err == nil && len(models) > 0 {
		if path != "" {
			_ = writeCache(path, cache{FetchedAt: now().UTC(), Models: models})
		}
		return newCatalog(models)
	}
	if cachedOK {
		return newCatalog(cached.Models)
	}
	return newCatalog(hardcodedFallbacks())
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
	if c == nil {
		return Model{}, false
	}
	modelID = normalizeID(modelID)
	for _, model := range c.models {
		if normalizeID(model.ID) == modelID {
			return cloneModel(model), true
		}
	}
	return Model{}, false
}

// Supports answers whether modelID advertises modality in direction. Unknown
// models and directions calmly return false.
func (c *Catalog) Supports(modelID, direction, modality string) bool {
	if c == nil {
		return false
	}
	modelID = normalizeID(modelID)
	for _, model := range c.models {
		if normalizeID(model.ID) != modelID {
			continue
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
	return false
}

func (c *Catalog) modelsWith(direction, modality string) []Model {
	if c == nil {
		return nil
	}
	models := make([]Model, 0)
	for _, model := range c.models {
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
	models = cleanModels(models)
	if len(models) == 0 {
		return nil, &statusError{status: "empty catalog"}
	}
	return models, nil
}

type statusError struct{ status string }

func (e *statusError) Error() string { return "model catalog: " + e.status }

func newCatalog(models []Model) *Catalog { return &Catalog{models: cleanModels(models)} }

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

func hardcodedFallbacks() []Model {
	return []Model{
		{ID: "krea/krea-2-medium-turbo", InputModalities: []string{"text", "image"}, OutputModalities: []string{"image"}},
		{ID: "hexgrad/kokoro-82m", InputModalities: []string{"text"}, OutputModalities: []string{"speech"}},
		{ID: "openai/gpt-4o-mini-tts", InputModalities: []string{"text"}, OutputModalities: []string{"speech"}},
		{ID: "google/lyria-3-clip-preview", InputModalities: []string{"text"}, OutputModalities: []string{"music"}, RequestPrice: 0.04},
		{ID: "bytedance/seedance-1-5-pro", InputModalities: []string{"text", "image"}, OutputModalities: []string{"video"}},
	}
}
