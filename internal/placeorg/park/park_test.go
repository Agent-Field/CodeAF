package park

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/placeorg"
)

func TestQuantileMatchesTheLinearRule(t *testing.T) {
	// numpy.quantile([0, 1, 2, 3], [0, 0.25, 0.5, 0.6, 1]) with the linear method.
	sorted := []float64{0, 1, 2, 3}
	want := []struct {
		q, v float64
	}{{0, 0}, {0.25, 0.75}, {0.5, 1.5}, {0.6, 1.8}, {1, 3}}
	for _, row := range want {
		if got := quantile(sorted, row.q); math.Abs(got-row.v) > 1e-12 {
			t.Errorf("quantile(%v) = %v, want %v", row.q, got, row.v)
		}
	}
}

func TestASpaceWithOneChatKeepsTheMeasuredPriors(t *testing.T) {
	th, err := calibrate(context.Background(), [][]float32{{1, 0}})
	if err != nil {
		t.Fatal(err)
	}
	if th.Floor != priorFloor || th.Margin != priorMargin || th.Delta != priorDelta {
		t.Fatalf("thresholds = %+v", th)
	}
}

func TestTwoSpacesKeepTheirOwnThresholds(t *testing.T) {
	same := [][]float32{{1, 0}, {1, 0}, {1, 0}, {1, 0}}
	spread := [][]float32{{1, 0}, {0, 1}, {-1, 0}}
	chow := New()
	tree := flatTree(t, []string{"a", "b", "c", "d"})
	if _, err := chow.Park(context.Background(), tree, corpusOf("a", "b", "c", "d"), same, placeorg.SpacePrimary); err != nil {
		t.Fatal(err)
	}
	tree = flatTree(t, []string{"a", "b", "c"})
	if _, err := chow.Park(context.Background(), tree, corpusOf("a", "b", "c"), spread, placeorg.SpaceLocal); err != nil {
		t.Fatal(err)
	}
	got := chow.Thresholds()
	if math.Abs(float64(got[placeorg.SpacePrimary].Floor-got[placeorg.SpaceLocal].Floor)) < 1e-3 {
		t.Fatalf("both spaces landed on floor %v", got[placeorg.SpacePrimary].Floor)
	}
	if math.Abs(float64(got[placeorg.SpacePrimary].Floor-1)) > 1e-4 {
		t.Fatalf("identical vectors floor = %v, want 1", got[placeorg.SpacePrimary].Floor)
	}
}

func TestIdenticalChatsParkAtTheTop(t *testing.T) {
	ids := []string{"a", "b", "c", "d"}
	tree := leafUnder(t, ids[:2], "d", "c")
	// Every vector is the same, so no smaller place is closer than the one
	// above it. Everyone who has a home parks at the top.
	vectors := [][]float32{{1, 0}, {1, 0}, {1, 0}, {1, 0}}
	ops, err := New().Park(context.Background(), tree, corpusOf(ids...), vectors, placeorg.SpacePrimary)
	if err != nil {
		t.Fatal(err)
	}
	for _, op := range ops {
		if op.Kind != placeorg.OpPark || op.Level != 1 {
			t.Errorf("%s -> %s level %d, want a park at the top", op.Chat, op.Kind, op.Level)
		}
	}
}

func TestChowFixture(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "chow.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []chowCase `json:"cases"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, tc := range fixture.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			tree := treeFrom(t, tc)
			chow := New()
			chow.UseThresholds(placeorg.Thresholds{Floor: tc.Floor, Margin: tc.Margin, Delta: tc.Margin})
			ops, err := chow.Park(context.Background(), tree, corpusFrom(tc), vectorsFrom(tc), placeorg.SpacePrimary)
			if err != nil {
				t.Fatal(err)
			}
			if len(ops) != len(tc.Chats) {
				t.Fatalf("ops = %d, chats = %d", len(ops), len(tc.Chats))
			}
			for i, op := range ops {
				if got := bucket(op); got != tc.Chats[i].Level {
					t.Errorf("%s -> %s, want %s (kind %s level %d)", op.Chat, got, tc.Chats[i].Level, op.Kind, op.Level)
				}
			}
			for id, want := range tc.Counts {
				node, ok := tree.Node(id)
				if !ok {
					t.Fatalf("missing place %s", id)
				}
				got := node.Centroids[placeorg.SpacePrimary].Count
				if got != float64(want) {
					t.Errorf("place %s counts %v chats, want %d", id, got, want)
				}
			}
			// The second case parks the leaf's chats one level up. They still
			// count in the leaf: a parked chat remains part of the place it
			// was grouped with.
			if tc.Name == "a smaller place that is only as close as the one above it" {
				leaf, _ := tree.Node("f")
				if leaf.Centroids[placeorg.SpacePrimary].Count != 2 {
					t.Fatalf("leaf lost the chats that parked above it: %+v", leaf.Centroids)
				}
			}
		})
	}
}

type chowCase struct {
	Name   string  `json:"name"`
	Floor  float32 `json:"floor"`
	Margin float32 `json:"margin"`
	Places []struct {
		ID      string   `json:"id"`
		Parent  string   `json:"parent"`
		Depth   int      `json:"depth"`
		Members []string `json:"members"`
	} `json:"places"`
	Chats []struct {
		ID     string    `json:"id"`
		Vector []float32 `json:"vector"`
		Level  string    `json:"level"`
	} `json:"chats"`
	Counts map[string]int `json:"counts"`
}

func treeFrom(t *testing.T, tc chowCase) *placeorg.Tree {
	t.Helper()
	nodes := make([]placeorg.BuiltNode, len(tc.Places))
	homes := map[string]string{}
	for i, place := range tc.Places {
		nodes[i] = placeorg.BuiltNode{ID: place.ID, Parent: place.Parent, Depth: place.Depth, Members: place.Members}
		for _, member := range place.Members {
			homes[member] = place.ID
		}
	}
	tree, err := placeorg.Assemble(nodes, homes, nil)
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

func corpusFrom(tc chowCase) *placeorg.Corpus {
	corpus := &placeorg.Corpus{Chats: make([]placeorg.Chat, len(tc.Chats))}
	for i, chat := range tc.Chats {
		corpus.Chats[i] = placeorg.Chat{ID: chat.ID}
	}
	return corpus
}

func vectorsFrom(tc chowCase) [][]float32 {
	out := make([][]float32, len(tc.Chats))
	for i, chat := range tc.Chats {
		out[i] = chat.Vector
	}
	return out
}

func bucket(op placeorg.Op) string {
	if op.Kind == placeorg.OpInbox {
		return "inbox"
	}
	switch op.Level {
	case 1:
		return "root"
	case 2:
		return "mid"
	default:
		if op.Level >= 3 && op.Kind == placeorg.OpFile {
			return "leaf"
		}
	}
	return "bad"
}

func corpusOf(ids ...string) *placeorg.Corpus {
	corpus := &placeorg.Corpus{Chats: make([]placeorg.Chat, len(ids))}
	for i, id := range ids {
		corpus.Chats[i] = placeorg.Chat{ID: id}
	}
	return corpus
}

func flatTree(t *testing.T, ids []string) *placeorg.Tree {
	t.Helper()
	tree, err := placeorg.Assemble([]placeorg.BuiltNode{{
		ID: "r", Depth: 1, Members: ids,
	}}, homesAt("r", ids), nil)
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

// leafUnder builds root / mid / leaf. The first two ids are the leaf, the
// third is homed at the mid, and the fourth at the root.
func leafUnder(t *testing.T, leaf []string, midChat, rootChat string) *placeorg.Tree {
	t.Helper()
	homes := homesAt("f", leaf)
	homes[midChat] = "m"
	homes[rootChat] = "r"
	tree, err := placeorg.Assemble([]placeorg.BuiltNode{
		{ID: "r", Depth: 1, Members: []string{rootChat}},
		{ID: "m", Parent: "r", Depth: 2, Members: []string{midChat}},
		{ID: "f", Parent: "m", Depth: 3, Members: leaf},
	}, homes, nil)
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

func homesAt(id string, chats []string) map[string]string {
	out := make(map[string]string, len(chats))
	for _, chat := range chats {
		out[chat] = id
	}
	return out
}
