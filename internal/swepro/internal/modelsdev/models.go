// Package modelsdev loads the provider/model catalog consumed by the
// TypeScript provider service. The catalog is runtime data: this package does
// not carry a baked model or pricing table.
package modelsdev

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Agent-Field/swe-pro-go/internal/engine/calc"
)

const (
	DefaultSource = "https://models.dev"
	cacheTTL      = 5 * time.Minute
	refreshEvery  = 60 * time.Minute
	fetchTimeout  = 10 * time.Second
	lockTimeout   = 5 * time.Minute
	lockPoll      = 100 * time.Millisecond
)

// Cost is the models.dev cost block. Pointer fields preserve the distinction
// between an omitted optional rate and an explicit zero.
type Cost struct {
	Input           float64  `json:"input"`
	Output          float64  `json:"output"`
	CacheRead       *float64 `json:"cache_read,omitempty"`
	CacheWrite      *float64 `json:"cache_write,omitempty"`
	ContextOver200K *Cost    `json:"context_over_200k,omitempty"`
}

type Limit struct {
	Context float64  `json:"context"`
	Input   *float64 `json:"input,omitempty"`
	Output  float64  `json:"output"`
}

type Modalities struct {
	Input  []string `json:"input"`
	Output []string `json:"output"`
}

type Model struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Attachment  *bool       `json:"attachment,omitempty"`
	Reasoning   *bool       `json:"reasoning,omitempty"`
	Temperature *bool       `json:"temperature,omitempty"`
	ToolCall    *bool       `json:"tool_call,omitempty"`
	Modalities  *Modalities `json:"modalities,omitempty"`
	Cost        *Cost       `json:"cost,omitempty"`
	Limit       Limit       `json:"limit"`
}

type Provider struct {
	ID     string           `json:"id"`
	Name   string           `json:"name"`
	Models map[string]Model `json:"models"`
}

type Catalog map[string]Provider

// Resolve follows provider.ts exactly: provider and model maps are addressed
// by their catalog keys. Model.id is API metadata, not a lookup alias.
func (catalog Catalog) Resolve(providerID, modelID string) (calc.Model, error) {
	provider, ok := catalog[providerID]
	if !ok {
		return calc.Model{}, fmt.Errorf("models.dev: provider %q not found", providerID)
	}
	model, ok := provider.Models[modelID]
	if !ok {
		return calc.Model{}, fmt.Errorf("models.dev: model %q not found for provider %q", modelID, providerID)
	}
	return calc.Model{Cost: projectCost(model.Cost), Limit: calc.ModelLimit{
		Context: model.Limit.Context,
		Input:   model.Limit.Input,
		Output:  model.Limit.Output,
	}, Capabilities: projectCapabilities(model)}, nil
}

func projectCapabilities(model Model) calc.ModelCapabilities {
	capabilities := calc.ModelCapabilities{
		Attachment:  boolOr(model.Attachment, false),
		Reasoning:   boolOr(model.Reasoning, false),
		Temperature: boolOr(model.Temperature, false),
		ToolCall:    boolOr(model.ToolCall, true),
		Input:       modalityMap(nil),
		Output:      modalityMap(nil),
	}
	if model.Modalities != nil {
		capabilities.Input = modalityMap(model.Modalities.Input)
		capabilities.Output = modalityMap(model.Modalities.Output)
	}
	return capabilities
}

func boolOr(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

func modalityMap(values []string) map[string]bool {
	result := map[string]bool{
		"text": false, "audio": false, "image": false, "video": false, "pdf": false,
	}
	for _, value := range values {
		if _, ok := result[value]; ok {
			result[value] = true
		}
	}
	return result
}

func projectCost(cost *Cost) *calc.ModelCost {
	result := &calc.ModelCost{Cache: &calc.CacheCost{}}
	if cost == nil {
		return result
	}
	result.Input = cost.Input
	result.Output = cost.Output
	if cost.CacheRead != nil {
		result.Cache.Read = *cost.CacheRead
	}
	if cost.CacheWrite != nil {
		result.Cache.Write = *cost.CacheWrite
	}
	if cost.ContextOver200K != nil {
		over := cost.ContextOver200K
		result.ExperimentalOver200K = &calc.Over200KCost{
			Cache: &calc.CacheCost{}, Input: over.Input, Output: over.Output,
		}
		if over.CacheRead != nil {
			result.ExperimentalOver200K.Cache.Read = *over.CacheRead
		}
		if over.CacheWrite != nil {
			result.ExperimentalOver200K.Cache.Write = *over.CacheWrite
		}
	}
	return result
}

type Options struct {
	Source       string
	CatalogPath  string
	CacheDir     string
	DisableFetch bool
	HTTPClient   *http.Client
	Version      string
	Now          func() time.Time
	Sleep        func(context.Context, time.Duration) error
	LockTimeout  time.Duration
	LockPoll     time.Duration
}

type Client struct {
	options   Options
	cachePath string

	mu      sync.Mutex
	loaded  bool
	catalog Catalog
	loadErr error
}

// New constructs a catalog client. Empty options select the same source and
// XDG cache directory used by the TypeScript Global.Path.cache service.
func New(options Options) (*Client, error) {
	if options.Source == "" {
		options.Source = DefaultSource
	}
	options.Source = strings.TrimRight(options.Source, "/")
	if options.CacheDir == "" {
		base, err := os.UserCacheDir()
		if err != nil {
			return nil, fmt.Errorf("models.dev cache directory: %w", err)
		}
		options.CacheDir = filepath.Join(base, "codeaf")
	}
	if options.HTTPClient == nil {
		options.HTTPClient = http.DefaultClient
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.Sleep == nil {
		options.Sleep = sleepContext
	}
	if options.LockTimeout <= 0 {
		options.LockTimeout = lockTimeout
	}
	if options.LockPoll <= 0 {
		options.LockPoll = lockPoll
	}
	name := "models.json"
	if options.Source != DefaultSource {
		digest := sha1.Sum([]byte(options.Source))
		name = "models-" + hex.EncodeToString(digest[:]) + ".json"
	}
	return &Client{options: options, cachePath: filepath.Join(options.CacheDir, name)}, nil
}

// NewFromEnv projects the three models.ts flags. Only "true" and "1"
// (case-insensitive) enable CODEAF_DISABLE_MODELS_FETCH.
func NewFromEnv(version string) (*Client, error) {
	return New(Options{
		Source:       os.Getenv("CODEAF_MODELS_URL"),
		CatalogPath:  os.Getenv("CODEAF_MODELS_PATH"),
		DisableFetch: truthy(os.Getenv("CODEAF_DISABLE_MODELS_FETCH")),
		Version:      version,
	})
}

func truthy(value string) bool {
	value = strings.ToLower(value)
	return value == "true" || value == "1"
}

// CachePath is exposed for diagnostics and parity tests.
func (client *Client) CachePath() string { return client.cachePath }

// Get is process-lifetime memoized, matching Effect.cachedInvalidateWithTTL
// with Duration.infinity. Population is disk -> no bundled snapshot ->
// disabled-fetch empty catalog -> network.
func (client *Client) Get(ctx context.Context) (Catalog, error) {
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.loaded {
		return client.catalog, client.loadErr
	}
	client.catalog, client.loadErr = client.populate(ctx)
	client.loaded = true
	return client.catalog, client.loadErr
}

func (client *Client) populate(ctx context.Context) (Catalog, error) {
	path := client.cachePath
	if client.options.CatalogPath != "" {
		path = client.options.CatalogPath
	}
	if catalog, err := readCatalog(path); err == nil && catalog != nil {
		return catalog, nil
	}
	// The pinned TypeScript snapshot is an explicit undefined stub.
	if client.options.DisableFetch {
		return Catalog{}, nil
	}
	var catalog Catalog
	err := client.withFileLock(ctx, func() error {
		raw, err := client.fetch(ctx)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(raw, &catalog); err != nil {
			return fmt.Errorf("models.dev decode: %w", err)
		}
		return writeCache(client.cachePath, raw)
	})
	return catalog, err
}

// Refresh mirrors models.ts refresh: freshness is checked on the generated
// cache path (not CODEAF_MODELS_PATH), checked again under the cross-process
// lock, and fetch errors leave the memoized catalog untouched.
func (client *Client) Refresh(ctx context.Context, force bool) error {
	if !force && client.fresh() {
		return nil
	}
	var catalog Catalog
	err := client.withFileLock(ctx, func() error {
		if !force && client.fresh() {
			return nil
		}
		raw, err := client.fetch(ctx)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(raw, &catalog); err != nil {
			return fmt.Errorf("models.dev decode: %w", err)
		}
		return writeCache(client.cachePath, raw)
	})
	if err != nil || catalog == nil {
		return err
	}
	client.mu.Lock()
	client.catalog = catalog
	client.loadErr = nil
	client.loaded = true
	client.mu.Unlock()
	return nil
}

// StartRefresh performs the startup refresh and repeats one hour after each
// completion, as Schedule.spaced does in models.ts.
func (client *Client) StartRefresh(ctx context.Context, report func(error)) {
	if client.options.DisableFetch {
		return
	}
	go func() {
		for {
			if err := client.Refresh(ctx, false); err != nil && report != nil {
				report(err)
			}
			timer := time.NewTimer(refreshEvery)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}()
}

func (client *Client) fresh() bool {
	info, err := os.Stat(client.cachePath)
	if err != nil {
		return false
	}
	return client.options.Now().Sub(info.ModTime()) < cacheTTL
}

func readCatalog(path string) (Catalog, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var catalog Catalog
	if err := json.Unmarshal(raw, &catalog); err != nil {
		return nil, err
	}
	return catalog, nil
}

func writeCache(path string, raw []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}

func (client *Client) fetch(ctx context.Context) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	var last error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			if err := client.options.Sleep(ctx, time.Duration(1<<(attempt-1))*200*time.Millisecond); err != nil {
				return nil, err
			}
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, client.options.Source+"/api.json", nil)
		if err != nil {
			return nil, err
		}
		request.Header.Set("User-Agent", "codeaf/"+client.options.Version)
		response, err := client.options.HTTPClient.Do(request)
		if err != nil {
			last = err
			continue
		}
		raw, readErr := io.ReadAll(response.Body)
		closeErr := response.Body.Close()
		if readErr != nil {
			last = readErr
			continue
		}
		if closeErr != nil {
			last = closeErr
			continue
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			last = fmt.Errorf("models.dev: GET %s: %s", request.URL, response.Status)
			if response.StatusCode != http.StatusRequestTimeout && response.StatusCode != http.StatusTooManyRequests && response.StatusCode < 500 {
				return nil, last
			}
			continue
		}
		return raw, nil
	}
	if last == nil {
		last = errors.New("models.dev: fetch failed")
	}
	return nil, last
}

func (client *Client) withFileLock(ctx context.Context, fn func() error) error {
	if err := os.MkdirAll(filepath.Dir(client.cachePath), 0o755); err != nil {
		return err
	}
	lock, err := os.OpenFile(client.cachePath+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	timer := time.NewTimer(client.options.LockTimeout)
	defer timer.Stop()
	for {
		err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return fmt.Errorf("Timed out waiting for lock: models-dev:%s", client.cachePath)
		case <-time.After(client.options.LockPoll):
		}
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) //nolint:errcheck
	return fn()
}

func sleepContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
