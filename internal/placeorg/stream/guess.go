package stream

import (
	"context"
	"math"

	"github.com/Agent-Field/codeaf/internal/placeorg"
)

// Guess scores a draft against every place's local centroid. Nothing is
// written and no model is called. A folder match is the answer. Otherwise
// the closest place is, with a small boost for the place opened within the
// last hour, and no answer at all when nothing reaches the floor.
func (s *Stream) Guess(ctx context.Context, t *placeorg.Tree, draft, workspace, recentPlace string) (placeorg.Guess, error) {
	if err := ctx.Err(); err != nil {
		return placeorg.Guess{}, err
	}
	if s == nil || t == nil {
		return placeorg.Guess{}, nil
	}
	g := placeorg.Guess{}
	if id := s.Folders[workspace]; id != "" {
		if n, ok := t.Node(id); ok {
			g.Place = n.ID
			g.Name = n.Name
			g.Confidence = 100
		}
	}
	// A folder match does not need a vector. With no draft and no folder
	// there is nothing to score, and an absent embedder means no guess
	// beyond the folder.
	if draft == "" || s.Embed == nil {
		return g, nil
	}
	vecs, err := s.Embed.Embed(ctx, []string{draft})
	if err != nil {
		if g.Place != "" {
			return g, nil
		}
		return placeorg.Guess{}, err
	}
	if len(vecs) == 0 || len(vecs[0]) == 0 {
		return g, nil
	}
	_, _, all := scorePlaces(t, placeorg.SpaceLocal, vecs[0])
	for i := range all {
		if all[i].id == recentPlace {
			all[i].score += continuationBoost
		}
	}
	sortPlaces(all)
	floor := s.guessFloor()
	if g.Place == "" {
		if len(all) == 0 || all[0].score < floor {
			return placeorg.Guess{}, nil
		}
		g.Place = all[0].id
		g.Name = all[0].name
		g.Confidence = confidence(all[0].score)
	}
	for _, p := range all {
		if p.id == g.Place || p.score < floor {
			continue
		}
		g.Others = append(g.Others, p.id)
		if len(g.Others) == 2 {
			break
		}
	}
	return g, nil
}

func confidence(score float32) int {
	c := int(math.Round(float64(score) * 100))
	if c < 0 {
		return 0
	}
	if c > 100 {
		return 100
	}
	return c
}
