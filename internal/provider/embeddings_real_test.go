//go:build e2e

package provider

import (
	"context"
	"os"
	"testing"
	"time"
)

// This package re-roots the profile in TestMain, so its live key comes from the environment.
func TestRealPlacesEmbeddingDimensions(t *testing.T) {
	key := os.Getenv("OPENROUTER_API_KEY")
	if key == "" {
		t.Skip("no OpenRouter key")
	}
	c, err := NewClient(Config{APIKey: key, BaseURL: "https://openrouter.ai/api/v1", Model: "openai/text-embedding-3-small"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	vectors, _, err := c.Embed(ctx, "openai/text-embedding-3-small", []string{"A synthetic conversation about organising a workspace."}, 256)
	if err != nil {
		t.Fatal(err)
	}
	if len(vectors) != 1 || len(vectors[0]) != 256 {
		t.Fatal("embedding dimensions differ from the requested space")
	}
}
