package desktopbridge

import (
	"context"
	"errors"

	"github.com/Agent-Field/codeaf/internal/placeorg"
)

// placesEmbedder adapts the engine door without opening a provider in the bridge.
// Door lookup is lazy so a warm vector cache never attaches a conversation.
type placesEmbedder struct{ find askDoor }

var _ placeorg.Embedder = (*placesEmbedder)(nil)

func (*placesEmbedder) Space() placeorg.SpaceID { return placeorg.SpacePrimary }
func (*placesEmbedder) Dims() int               { return placeorg.PrimaryDims }

func (e *placesEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	if e.find == nil {
		return nil, errors.New("no engine for places embeddings")
	}
	ctx, cancel := context.WithTimeout(ctx, askWithin)
	defer cancel()
	door, err := e.find(ctx)
	if err != nil {
		return nil, err
	}
	embed, ok := door.(interface {
		EmbedPlaces(context.Context, []string) ([][]float32, error)
	})
	if !ok {
		return nil, errors.New("this engine cannot embed places")
	}
	return embed.EmbedPlaces(ctx, texts)
}

// embedThrough is the primary-space counterpart of askThrough for the organiser.
func (a *PlaceAdvice) embedThrough(find askDoor) placeorg.Embedder {
	return &placesEmbedder{find: find}
}
