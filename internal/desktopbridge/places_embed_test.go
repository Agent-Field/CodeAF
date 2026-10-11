package desktopbridge

import (
	"context"
	"testing"

	"github.com/Agent-Field/codeaf/internal/placegraph"
	"github.com/Agent-Field/codeaf/internal/placeorg"
	"github.com/Agent-Field/codeaf/internal/session"
)

type embeddingDoor struct{ calls int }

func (*embeddingDoor) AskPlaces(context.Context, placegraph.ModelRequest) (session.PlacesAnswer, error) {
	return session.PlacesAnswer{}, nil
}
func (e *embeddingDoor) EmbedPlaces(ctx context.Context, texts []string) ([][]float32, error) {
	e.calls++
	if _, ok := ctx.Deadline(); !ok {
		panic("embedding lookup must be bounded")
	}
	return [][]float32{{1, 0}}, nil
}

func TestPlacesEmbedderUsesTheEngineDoorLazily(t *testing.T) {
	door := &embeddingDoor{}
	lookups := 0
	embed := (&PlaceAdvice{}).embedThrough(func(context.Context) (placeAskDoor, error) { lookups++; return door, nil })
	if embed.Space() != placeorg.SpacePrimary || embed.Dims() != 256 {
		t.Fatal("wrong embedding space")
	}
	if _, err := embed.Embed(context.Background(), nil); err != nil || lookups != 0 {
		t.Fatal("empty embedding attached an engine")
	}
	vectors, err := embed.Embed(context.Background(), []string{"synthetic"})
	if err != nil || len(vectors) != 1 || door.calls != 1 || lookups != 1 {
		t.Fatalf("vectors %v, error %v, calls %d", vectors, err, door.calls)
	}
	if _, err := (&placesEmbedder{}).Embed(context.Background(), []string{"a"}); err == nil {
		t.Fatal("absent engine accepted embeddings")
	}
}
