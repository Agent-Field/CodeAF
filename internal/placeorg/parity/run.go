package parity

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/placeorg"
	"github.com/Agent-Field/codeaf/internal/placeorg/anchors"
	"github.com/Agent-Field/codeaf/internal/placeorg/audit"
	"github.com/Agent-Field/codeaf/internal/placeorg/community"
	"github.com/Agent-Field/codeaf/internal/placeorg/link"
	"github.com/Agent-Field/codeaf/internal/placeorg/nest"
	"github.com/Agent-Field/codeaf/internal/placeorg/park"
	"github.com/Agent-Field/codeaf/internal/placeorg/review"
	"github.com/Agent-Field/codeaf/internal/placeorg/stream"
)

// Floor is the adjusted Rand index each level must reach. Below it the
// structures are not the same tree up to renaming. The pipeline's own parity
// test uses the same floor.
const Floor = 0.85

// Result is one comparison of a Go run with the saved reference.
type Result struct {
	Chats      int
	ARI        map[string]float64
	Invariants []placeorg.InvariantResult
	Passed     int
	OpCounts   map[string]int
	RefOps     map[string]int
	RefLines   []string
	Wall       time.Duration
	Summary    string
	Space      placeorg.SpaceID
}

// Run builds the tree on the saved embeddings and scores it. graphPath is the
// reference kNN graph.
func Run(ctx context.Context, ref Reference, graphPath string) (Result, error) {
	saved := savedVectors{byID: map[string][]float32{}}
	corpus := &placeorg.Corpus{}
	for i, id := range ref.IDs {
		saved.byID[id] = ref.Vectors[i]
		corpus.Chats = append(corpus.Chats, placeorg.Chat{
			ID: id, Replies: 2, Created: time.Unix(int64(1_700_000_000+i), 0),
		})
	}
	graph, err := loadGraph(graphPath)
	if err != nil {
		return Result{}, err
	}
	capture := &levelCapture{inner: park.New()}
	state := placeorg.NewState()
	stages := placeorg.Stages{
		Dedup: keepAll{}, Kind: keepAll{}, Anchors: anchors.Derived{}, Vectors: saved,
		Neighbours: graph, Communities: community.New(), Nest: nest.New(),
		Audit: audit.Auditor{}, Park: capture, Name: numbered{}, Review: &review.Stage{},
		Link: link.New(state), Stream: stream.New(),
	}
	runner := &placeorg.Runner{Stages: stages, State: state}
	start := time.Now()
	plan, err := runner.Run(ctx, corpus, placeorg.Options{
		Mode: placeorg.ModeRefresh, Seed: placeorg.DefaultSeed,
		// The numbered namer asks no model. A zero budget would still mark the
		// plan provisional the moment the audit asked one question, which is
		// not a difference in the tree.
		Budget: placeorg.Budget{Calls: 10_000, USD: 100},
		Now:    func() time.Time { return start },
	})
	wall := time.Since(start)
	if err != nil {
		return Result{}, err
	}
	out := Result{
		Chats: len(ref.IDs), ARI: map[string]float64{}, Invariants: plan.Invariants,
		OpCounts: map[string]int{}, RefOps: ref.OpCounts, RefLines: ref.OpLines,
		Wall: wall, Summary: plan.Summary, Space: plan.Space,
	}
	for _, res := range plan.Invariants {
		if res.OK {
			out.Passed++
		}
	}
	for _, op := range plan.Ops {
		out.OpCounts[strings.ToUpper(string(op.Kind))]++
	}
	rootSize := map[int]int{}
	for _, id := range ref.IDs {
		rootSize[ref.Levels[id].Root]++
	}
	for level, name := range LevelNames {
		var got, want []string
		for _, id := range ref.IDs {
			c := ref.Levels[id]
			raw := [3]int{c.Root, c.Mid, c.Leaf}
			fold := strconv.Itoa(c.Root)
			if level == 2 && c.Mid >= 0 {
				fold = strconv.Itoa(c.Mid) + "/" + fold
			}
			want = append(want, refLabel(raw[level], rootSize[c.Root] < placeorg.Quorum, fold))
			got = append(got, capture.levels[id][level])
		}
		out.ARI[name] = AdjustedRand(got, want)
	}
	return out, nil
}

// refLabel is the reference's place for a chat at one level. A negative id is
// a place the audit folded, so the chat sits at the place above it. A root
// too small to be a place is Now at every level, which is what dissolving it
// does. The reference's Now flag also covers chats the parking floor sent
// there, whose community is intact, so only that small root counts as Now.
func refLabel(id int, small bool, fold string) string {
	switch {
	case small:
		return "now"
	case id < 0:
		return "up:" + fold
	}
	return strconv.Itoa(id)
}

// savedVectors answers with the reference embeddings, so the provider's
// run-to-run noise is not part of the comparison.
type savedVectors struct{ byID map[string][]float32 }

func (s savedVectors) Vectorize(_ context.Context, c *placeorg.Corpus, _, _ placeorg.Embedder) (placeorg.SpaceID, [][]float32, bool, error) {
	out := make([][]float32, len(c.Chats))
	for i, ch := range c.Chats {
		out[i] = s.byID[ch.ID]
	}
	return placeorg.SpacePrimary, out, false, nil
}

// keepAll stands in for dedup and kind. The reference rows are already folded
// and already have the probes taken out.
type keepAll struct{}

func (keepAll) Dedup(context.Context, []placeorg.Chat) (map[string]string, error) {
	return map[string]string{}, nil
}

func (keepAll) Kinds(_ context.Context, chats []placeorg.Chat, _ map[string]string) (map[string]placeorg.Kind, error) {
	out := map[string]placeorg.Kind{}
	for _, c := range chats {
		out[c.ID] = placeorg.KindConversation
	}
	return out, nil
}

// numbered names every place with a distinct word. The reference's names are
// a model's and are not compared. It asks no model; the signature still
// takes a Judge because that is the Namer interface, and the fallback is
// the numbered word.
type numbered struct{}

var _ placeorg.Judged = numbered{}

func (numbered) Fallbacks() []placeorg.Fallback {
	return []placeorg.Fallback{{When: "naming", Does: "a numbered word"}}
}

func (numbered) Name(_ context.Context, t *placeorg.Tree, _ *placeorg.Corpus, _ placeorg.Judge) ([]placeorg.Op, error) {
	var ids []string
	var walk func(string)
	walk = func(id string) {
		ids = append(ids, id)
		n, ok := t.Node(id)
		if !ok {
			return
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	for _, r := range t.Roots() {
		walk(r)
	}
	var ops []placeorg.Op
	for i, id := range ids {
		ops = append(ops, placeorg.Op{
			Kind: placeorg.OpName, Stage: placeorg.StageName, Place: id,
			Name: "place" + strings.Repeat("x", i+1), Why: "numbered",
		})
	}
	return ops, nil
}

// levelCapture records, before parking, where the audited tree puts each
// chat. That is the root, mid and leaf the reference columns describe.
type levelCapture struct {
	inner  *park.Chow
	levels map[string][3]string
}

func (c *levelCapture) Park(ctx context.Context, t *placeorg.Tree, corpus *placeorg.Corpus, v [][]float32, s placeorg.SpaceID) ([]placeorg.Op, error) {
	c.levels = map[string][3]string{}
	for _, ch := range corpus.Chats {
		home, ok := t.Home(ch.ID)
		if !ok {
			c.levels[ch.ID] = [3]string{"now", "now", "now"}
			continue
		}
		var chain []string
		for id := home; id != ""; {
			chain = append([]string{id}, chain...)
			n, _ := t.Node(id)
			id = n.Parent
		}
		var lv [3]string
		for i := range lv {
			lv[i] = chain[min(i, len(chain)-1)]
			if i >= len(chain) {
				lv[i] = "up:" + chain[len(chain)-1]
			}
		}
		c.levels[ch.ID] = lv
	}
	return c.inner.Park(ctx, t, corpus, v, s)
}

func (c *levelCapture) Thresholds() map[placeorg.SpaceID]placeorg.Thresholds {
	return c.inner.Thresholds()
}

// mustGraph answers with the reference kNN graph. The file is the community
// package's export of that graph.
type fixedGraph struct{ g placeorg.Graph }

func (f fixedGraph) KNN(context.Context, [][]float32, map[string]placeorg.AnchorSet, []string) (placeorg.Graph, error) {
	return f.g, nil
}

func loadGraph(path string) (fixedGraph, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fixedGraph{}, fmt.Errorf("parity: reference graph: %w", err)
	}
	var doc struct {
		N     int         `json:"n"`
		Edges [][]float64 `json:"edges"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return fixedGraph{}, fmt.Errorf("parity: reference graph: %w", err)
	}
	if doc.N == 0 {
		return fixedGraph{}, fmt.Errorf("parity: reference graph %s has no chats", path)
	}
	g := placeorg.Graph{N: doc.N}
	for _, e := range doc.Edges {
		if len(e) < 3 {
			continue
		}
		g.Edges = append(g.Edges, placeorg.Edge{I: int(e[0]), J: int(e[1]), W: float32(e[2])})
	}
	return fixedGraph{g}, nil
}

// GraphBesideParity is the reference graph exported next to this package.
func GraphBesideParity() string {
	return filepath.Join("..", "community", "testdata", "fixture-graph.json")
}
