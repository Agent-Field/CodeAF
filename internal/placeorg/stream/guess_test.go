package stream

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/placeorg"
)

func TestAFolderMatchIsTheGuess(t *testing.T) {
	t.Parallel()
	tree := localPlaces(0.9, 0.5)
	s := New()
	s.Embed = axisEmbed{2}
	s.Folders = map[string]string{"/work/beta": "b"}
	got, err := s.Guess(context.Background(), tree, "a draft about alpha", "/work/beta", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Place != "b" || got.Name != "Beta" || got.Confidence != 100 {
		t.Fatalf("guess %+v", got)
	}
}

func TestTheRecentPlaceTipsACloseGuess(t *testing.T) {
	t.Parallel()
	// Alpha is slightly closer. The place opened in the last hour is Beta,
	// and the boost is enough to tip it.
	tree := putTree([]placeorg.Node{
		{ID: "a", Name: "Alpha", Depth: 1, Centroids: centroid(placeorg.SpaceLocal, toward(0.62, 2))},
		{ID: "b", Name: "Beta", Depth: 1, Centroids: centroid(placeorg.SpaceLocal, toward(0.60, 2))},
	}, []string{"a", "b"})
	s := New()
	s.Embed = axisEmbed{2}
	got, err := s.Guess(context.Background(), tree, "draft", "", "b")
	if err != nil {
		t.Fatal(err)
	}
	if got.Place != "b" || got.Name != "Beta" {
		t.Fatalf("guess %+v", got)
	}
	if len(got.Others) != 1 || got.Others[0] != "a" {
		t.Fatalf("others %v", got.Others)
	}
}

func TestAFolderMatchSurvivesADeadEmbedder(t *testing.T) {
	t.Parallel()
	tree := localPlaces(0.9, 0.5)
	s := New()
	s.Embed = errEmbed{}
	s.Folders = map[string]string{"/work/beta": "b"}
	got, err := s.Guess(context.Background(), tree, "draft", "/work/beta", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Place != "b" || got.Confidence != 100 {
		t.Fatalf("guess %+v", got)
	}
}

func TestAWeakGuessIsNothing(t *testing.T) {
	t.Parallel()
	tree := localPlaces(0.2, 0.1)
	s := New()
	s.Embed = axisEmbed{2}
	got, err := s.Guess(context.Background(), tree, "draft", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Place != "" || got.Name != "" || len(got.Others) != 0 {
		t.Fatalf("guess %+v", got)
	}
}

func TestGuessStaysInsideItsBudgetAtFiveHundredPlaces(t *testing.T) {
	nodes := make([]placeorg.Node, 500)
	roots := make([]string, 500)
	for i := range nodes {
		id := "g" + itoa(i)
		nodes[i] = placeorg.Node{
			ID: id, Name: id, Depth: 1,
			Centroids: centroid(placeorg.SpaceLocal, toward(float32(i)/500-0.5, 256)),
		}
		roots[i] = id
	}
	tree := putTree(nodes, roots)
	s := New()
	s.Embed = axisEmbed{256}
	if _, err := s.Guess(context.Background(), tree, "draft", "", ""); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if _, err := s.Guess(context.Background(), tree, "draft", "", ""); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed >= placeorg.GuessBudget {
		t.Fatalf("Guess took %s, budget %s", elapsed, placeorg.GuessBudget)
	}
}

func localPlaces(a, b float32) *placeorg.Tree {
	return putTree([]placeorg.Node{
		{ID: "a", Name: "Alpha", Depth: 1, Centroids: centroid(placeorg.SpaceLocal, toward(a, 2))},
		{ID: "b", Name: "Beta", Depth: 1, Centroids: centroid(placeorg.SpaceLocal, toward(b, 2))},
	}, []string{"a", "b"})
}

// axisEmbed returns the first axis at dim, so a place built with toward
// scores at the cosine that toward was given. It stands in for the local
// table; the budget test is about the scoring, not a model.
type axisEmbed struct{ dim int }

func (a axisEmbed) Space() placeorg.SpaceID { return placeorg.SpaceLocal }
func (a axisEmbed) Dims() int               { return a.dim }
func (a axisEmbed) Embed(context.Context, []string) ([][]float32, error) {
	v := make([]float32, a.dim)
	v[0] = 1
	return [][]float32{v}, nil
}

type errEmbed struct{}

func (errEmbed) Space() placeorg.SpaceID { return placeorg.SpaceLocal }
func (errEmbed) Dims() int               { return 2 }
func (errEmbed) Embed(context.Context, []string) ([][]float32, error) {
	return nil, errors.New("down")
}
