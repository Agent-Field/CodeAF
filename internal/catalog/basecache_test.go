package catalog

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const baseACatalogPayload = `{"data":[{"id":"base-a/model","architecture":{"input_modalities":["text"],"output_modalities":["image"]}}]}`
const baseBCatalogPayload = `{"data":[{"id":"base-b/model","architecture":{"input_modalities":["text"],"output_modalities":["text"]}}]}`

func failedCatalogClient() *http.Client {
	return &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("offline")
	})}
}

func expectedCacheFile(dir, source, normalizedBase string) string {
	key := CacheKey(source, normalizedBase)
	if key == "" {
		return filepath.Join(dir, cacheName)
	}
	extension := filepath.Ext(cacheName)
	stem := strings.TrimSuffix(cacheName, extension)
	return filepath.Join(dir, stem+"-"+key+extension)
}

func assertCatalogHasNoBaseARow(t *testing.T, models *Catalog) {
	t.Helper()
	if _, ok := models.Model("base-a/model"); ok {
		t.Fatal("a catalog for base B served base A's cached row")
	}
}

func TestACatalogCachedForOneBaseIsNeverServedToAnother(t *testing.T) {
	const baseA = "https://catalog-a.example/api/v1"
	const baseB = "https://catalog-b.example/api/v1"
	day := time.Date(2026, 9, 9, 10, 0, 0, 0, time.UTC)

	t.Run("no cache", func(t *testing.T) {
		models := Load(context.Background(), Options{
			BaseURL: baseB, Dir: t.TempDir(),
			HTTPClient: catalogClient(t, http.StatusOK, baseBCatalogPayload, nil),
		})
		assertCatalogHasNoBaseARow(t, models)
	})

	t.Run("fresh cache for base A", func(t *testing.T) {
		dir := t.TempDir()
		Load(context.Background(), Options{
			BaseURL: baseA, Dir: dir, Now: func() time.Time { return day },
			HTTPClient: catalogClient(t, http.StatusOK, baseACatalogPayload, nil),
		})
		models := Load(context.Background(), Options{
			BaseURL: baseB, Dir: dir, Now: func() time.Time { return day.Add(time.Hour) },
			HTTPClient: catalogClient(t, http.StatusOK, baseBCatalogPayload, nil),
		})
		assertCatalogHasNoBaseARow(t, models)
	})

	t.Run("stale cache for base A", func(t *testing.T) {
		dir := t.TempDir()
		Load(context.Background(), Options{
			BaseURL: baseA, Dir: dir, Now: func() time.Time { return day },
			HTTPClient: catalogClient(t, http.StatusOK, baseACatalogPayload, nil),
		})
		models := Load(context.Background(), Options{
			BaseURL: baseB, Dir: dir, Now: func() time.Time { return day.Add(TTL + time.Hour) },
			HTTPClient: catalogClient(t, http.StatusOK, baseBCatalogPayload, nil),
		})
		assertCatalogHasNoBaseARow(t, models)
	})

	t.Run("base B fetch fails beside base A cache", func(t *testing.T) {
		dir := t.TempDir()
		Load(context.Background(), Options{
			BaseURL: baseA, Dir: dir, Now: func() time.Time { return day },
			HTTPClient: catalogClient(t, http.StatusOK, baseACatalogPayload, nil),
		})
		models := Load(context.Background(), Options{
			BaseURL: baseB, Dir: dir, Now: func() time.Time { return day.Add(TTL + time.Hour) },
			HTTPClient: failedCatalogClient(),
		})
		assertCatalogHasNoBaseARow(t, models)
	})

	t.Run("a cache file carrying the wrong base is refused", func(t *testing.T) {
		dir := t.TempDir()
		Load(context.Background(), Options{
			BaseURL: baseA, Dir: dir, Now: func() time.Time { return day },
			HTTPClient: catalogClient(t, http.StatusOK, baseACatalogPayload, nil),
		})
		raw, err := os.ReadFile(expectedCacheFile(dir, "", baseA))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(expectedCacheFile(dir, "", baseB), raw, 0o600); err != nil {
			t.Fatal(err)
		}
		models := Load(context.Background(), Options{
			BaseURL: baseB, Dir: dir, HTTPClient: failedCatalogClient(),
		})
		assertCatalogHasNoBaseARow(t, models)
	})
}

func TestACatalogFetchedForOneServiceIsNeverServedToAnother(t *testing.T) {
	const base = "https://shared.example/v1"
	day := time.Date(2026, 9, 9, 10, 0, 0, 0, time.UTC)

	t.Run("fresh cache", func(t *testing.T) {
		dir := t.TempDir()
		Load(context.Background(), Options{Source: "deepseek", BaseURL: base, Dir: dir, Now: func() time.Time { return day }, HTTPClient: catalogClient(t, http.StatusOK, baseACatalogPayload, nil)})
		models := Load(context.Background(), Options{Source: "z-ai", BaseURL: base, Dir: dir, Now: func() time.Time { return day.Add(time.Hour) }, HTTPClient: catalogClient(t, http.StatusOK, baseBCatalogPayload, nil)})
		assertCatalogHasNoBaseARow(t, models)
	})

	t.Run("successful fetch", func(t *testing.T) {
		dir := t.TempDir()
		Load(context.Background(), Options{Source: "deepseek", BaseURL: base, Dir: dir, HTTPClient: catalogClient(t, http.StatusOK, baseACatalogPayload, nil)})
		foreignPath := expectedCacheFile(dir, "deepseek", base)
		foreignBefore, err := os.ReadFile(foreignPath)
		if err != nil {
			t.Fatal(err)
		}
		models := Load(context.Background(), Options{Source: "z-ai", BaseURL: base, Dir: dir, HTTPClient: catalogClient(t, http.StatusOK, baseBCatalogPayload, nil)})
		assertCatalogHasNoBaseARow(t, models)
		foreignAfter, err := os.ReadFile(foreignPath)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(foreignAfter, foreignBefore) {
			t.Fatal("a successful fetch overwrote the foreign service's artifact")
		}
		if _, err := os.Stat(expectedCacheFile(dir, "z-ai", base)); err != nil {
			t.Fatalf("successful fetch did not write its own service artifact: %v", err)
		}
	})

	t.Run("stale foreign cache after failed fetch", func(t *testing.T) {
		dir := t.TempDir()
		Load(context.Background(), Options{Source: "deepseek", BaseURL: base, Dir: dir, Now: func() time.Time { return day }, HTTPClient: catalogClient(t, http.StatusOK, baseACatalogPayload, nil)})
		models := Load(context.Background(), Options{Source: "z-ai", BaseURL: base, Dir: dir, Now: func() time.Time { return day.Add(TTL + time.Hour) }, HTTPClient: failedCatalogClient()})
		assertCatalogHasNoBaseARow(t, models)
	})

	t.Run("no cache has no foreign fallbacks", func(t *testing.T) {
		models := Load(context.Background(), Options{Source: "deepseek", BaseURL: DefaultBaseURL, Dir: t.TempDir(), HTTPClient: failedCatalogClient()})
		if len(models.ModelsNow()) != 0 {
			t.Fatalf("non-default service got %d built-in rows", len(models.ModelsNow()))
		}
	})

	t.Run("panic recovery has no foreign fallbacks", func(t *testing.T) {
		models := Load(context.Background(), Options{Source: "deepseek", BaseURL: DefaultBaseURL, Dir: t.TempDir(), Now: func() time.Time { panic("clock") }, HTTPClient: catalogClient(t, http.StatusOK, baseACatalogPayload, nil)})
		if len(models.ModelsNow()) != 0 {
			t.Fatalf("non-default service panic got %d built-in rows", len(models.ModelsNow()))
		}
		defaults := Load(context.Background(), Options{BaseURL: DefaultBaseURL, Dir: t.TempDir(), Now: func() time.Time { panic("clock") }, HTTPClient: catalogClient(t, http.StatusOK, baseACatalogPayload, nil)})
		if len(defaults.ModelsNow()) != 11 {
			t.Fatalf("default service panic got %d built-in rows, want 11", len(defaults.ModelsNow()))
		}
	})
}

func TestTheDefaultBaseKeepsItsCacheAndOfflineFallbackBehavior(t *testing.T) {
	day := time.Date(2026, 9, 9, 10, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	Load(context.Background(), Options{
		BaseURL: DefaultBaseURL, Dir: dir, Now: func() time.Time { return day },
		HTTPClient: catalogClient(t, http.StatusOK, baseACatalogPayload, nil),
	})

	var fetches atomic.Int32
	offline := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		fetches.Add(1)
		return nil, errors.New("offline")
	})}
	fresh := Load(context.Background(), Options{
		BaseURL: DefaultBaseURL, Dir: dir, Now: func() time.Time { return day.Add(time.Hour) }, HTTPClient: offline,
	})
	if _, ok := fresh.Model("base-a/model"); !ok {
		t.Fatal("the default base did not serve its fresh cache")
	}
	if fetches.Load() != 0 {
		t.Fatalf("the default base's fresh cache performed %d fetches, want none", fetches.Load())
	}

	stale := Load(context.Background(), Options{
		BaseURL: DefaultBaseURL, Dir: dir, Now: func() time.Time { return day.Add(TTL + time.Hour) }, HTTPClient: offline,
	})
	if _, ok := stale.Model("base-a/model"); !ok {
		t.Fatal("the default base did not serve its stale cache after a failed fetch")
	}
	if !stale.FetchedAt().Equal(day) {
		t.Fatalf("stale cache date = %v, want %v", stale.FetchedAt(), day)
	}

	builtIn := Load(context.Background(), Options{
		BaseURL: DefaultBaseURL, Dir: t.TempDir(), HTTPClient: failedCatalogClient(),
	})
	if len(builtIn.ModelsNow()) != 11 {
		t.Fatalf("default-base built-in rows = %d, want 11", len(builtIn.ModelsNow()))
	}
	for _, modality := range []string{"image", "speech", "music", "video"} {
		if len(builtIn.ModelsWithOutput(modality)) == 0 {
			t.Errorf("default-base built-in rows have no %s output", modality)
		}
	}
}

func TestTheBuiltInRowsAreTheDefaultBasesAndNobodyElses(t *testing.T) {
	models := Load(context.Background(), Options{
		BaseURL: "https://another-provider.example/v1", Dir: t.TempDir(), HTTPClient: failedCatalogClient(),
	})
	if got := len(models.ModelsNow()); got != 0 {
		t.Fatalf("non-default offline catalog has %d models, want none", got)
	}
	for _, modality := range []string{"image", "speech", "music", "video"} {
		if got := models.ModelsWithOutput(modality); len(got) != 0 {
			t.Errorf("non-default offline catalog invented %s models: %+v", modality, got)
		}
	}
}

func TestALegacyCacheFileIsTheDefaultBasesCache(t *testing.T) {
	dir := t.TempDir()
	day := time.Date(2026, 9, 9, 10, 0, 0, 0, time.UTC)
	legacy, err := json.Marshal(struct {
		FetchedAt time.Time `json:"fetched_at"`
		Models    []Model   `json:"models"`
	}{FetchedAt: day, Models: []Model{{ID: "legacy/model", OutputModalities: []string{"image"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, cacheName), legacy, 0o600); err != nil {
		t.Fatal(err)
	}

	// THE CLOCK IS THE FIXTURE'S, NOT THE MACHINE'S. "Fresh" here means inside
	// [TTL] of the moment this cache says it was fetched, and a test that let
	// the wall clock answer that would pass on the day it was written and fail
	// every day after — which is exactly what it did.
	anHourLater := func() time.Time { return day.Add(time.Hour) }

	neverFetch := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("a fresh legacy cache performed a fetch")
		return nil, errors.New("offline")
	})}
	for _, base := range []string{DefaultBaseURL, ""} {
		models := Load(context.Background(), Options{BaseURL: base, Dir: dir, Now: anHourLater, HTTPClient: neverFetch})
		if _, ok := models.Model("legacy/model"); !ok {
			t.Errorf("legacy cache was not served for base %q", base)
		}
	}

	custom := Load(context.Background(), Options{
		BaseURL: "https://another-provider.example/v1", Dir: dir, Now: anHourLater, HTTPClient: failedCatalogClient(),
	})
	if _, ok := custom.Model("legacy/model"); ok {
		t.Fatal("legacy default-base cache was served to a custom base")
	}
}

func TestACacheRecordsItsNormalizedBase(t *testing.T) {
	dir := t.TempDir()
	const normalized = "https://vendor.example/Case/v1"
	Load(context.Background(), Options{
		BaseURL: "  HTTPS://VENDOR.EXAMPLE/Case/v1///  ", Dir: dir,
		HTTPClient: catalogClient(t, http.StatusOK, baseACatalogPayload, nil),
	})
	raw, err := os.ReadFile(expectedCacheFile(dir, "", normalized))
	if err != nil {
		t.Fatal(err)
	}
	var cached struct {
		Base string `json:"base"`
	}
	if err := json.Unmarshal(raw, &cached); err != nil {
		t.Fatal(err)
	}
	if cached.Base != normalized {
		t.Fatalf("cache base = %q, want %q", cached.Base, normalized)
	}

	var fetches atomic.Int32
	fromCache := Load(context.Background(), Options{
		BaseURL: normalized + "/", Dir: dir,
		HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			fetches.Add(1)
			return nil, errors.New("offline")
		})},
	})
	if _, ok := fromCache.Model("base-a/model"); !ok {
		t.Fatal("equivalent normalized base did not find its cache")
	}
	if fetches.Load() != 0 {
		t.Fatalf("equivalent normalized base performed %d fetches, want none", fetches.Load())
	}
	if got := filepath.Base(expectedCacheFile(dir, "", DefaultBaseURL)); got != cacheName {
		t.Fatalf("default cache name = %q, want %q", got, cacheName)
	}
}
