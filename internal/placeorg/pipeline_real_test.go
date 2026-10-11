package placeorg_test

// The Runner with the real wave-A stages in it. Two kinds of test live here.
// The parity test compares the tree the Go stages build with the owner's
// reference run, on the reference's own saved embeddings, and skips without
// PLACEORG_REF because that corpus is the owner's library and is never
// committed. The scale test builds a synthetic library of ten thousand chats
// and holds the run to the time and memory the architecture promises.

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/placeorg"
	"github.com/Agent-Field/codeaf/internal/placeorg/anchors"
	"github.com/Agent-Field/codeaf/internal/placeorg/audit"
	"github.com/Agent-Field/codeaf/internal/placeorg/community"
	"github.com/Agent-Field/codeaf/internal/placeorg/dedup"
	"github.com/Agent-Field/codeaf/internal/placeorg/kind"
	"github.com/Agent-Field/codeaf/internal/placeorg/knn"
	"github.com/Agent-Field/codeaf/internal/placeorg/link"
	"github.com/Agent-Field/codeaf/internal/placeorg/localembed"
	"github.com/Agent-Field/codeaf/internal/placeorg/name"
	"github.com/Agent-Field/codeaf/internal/placeorg/nest"
	"github.com/Agent-Field/codeaf/internal/placeorg/park"
	"github.com/Agent-Field/codeaf/internal/placeorg/review"
	"github.com/Agent-Field/codeaf/internal/placeorg/stream"
	"github.com/Agent-Field/codeaf/internal/placeorg/vectors"
)

// realStages wires every wave-A package into a registry.
func realStages(state *placeorg.State, dir string, parker placeorg.Parker) placeorg.Stages {
	return placeorg.Stages{
		Dedup: dedup.New(1), Kind: kind.New(), Anchors: anchors.Derived{}, Vectors: vectors.New(dir),
		Neighbours: knn.New(8, 0.35), Communities: community.New(), Nest: nest.New(),
		Audit: audit.Auditor{}, Park: parker, Name: &name.Stage{}, Review: &review.Stage{},
		Link: link.New(state), Stream: stream.New(),
	}
}

// ---- parity ------------------------------------------------------------

type refChat struct{ Root, Mid, Leaf int }

// loadRef reads the reference's assignments and saved embeddings.
func loadRef(t *testing.T, dir string) (ids []string, chats map[string]refChat, emb [][]float32) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "assignments.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Assignments map[string]struct {
			Root int `json:"root"`
			Mid  int `json:"mid"`
			Leaf int `json:"leaf"`
		} `json:"assignments"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	chats = map[string]refChat{}
	for id, a := range doc.Assignments {
		chats[id] = refChat{a.Root, a.Mid, a.Leaf}
	}
	rawIDs, err := os.ReadFile(filepath.Join(dir, "emb_row_ids.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(rawIDs, &ids); err != nil {
		t.Fatal(err)
	}
	return ids, chats, readNpy(t, filepath.Join(dir, "emb_all.npy"), len(ids))
}

var npyShape = regexp.MustCompile(`\((\d+),\s*(\d+)\)`)

// readNpy reads a little-endian float32 matrix in numpy's version 1 format.
func readNpy(t *testing.T, path string, rows int) [][]float32 {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) < 10 || string(raw[1:6]) != "NUMPY" {
		t.Fatalf("%s is not an npy file", path)
	}
	headerLen := int(binary.LittleEndian.Uint16(raw[8:10]))
	header := string(raw[10 : 10+headerLen])
	m := npyShape.FindStringSubmatch(header)
	if m == nil || !strings.Contains(header, "<f4") {
		t.Fatalf("unsupported npy header %q", header)
	}
	r, _ := strconv.Atoi(m[1])
	c, _ := strconv.Atoi(m[2])
	if r != rows {
		t.Fatalf("%d embedding rows for %d ids", r, rows)
	}
	body := raw[10+headerLen:]
	out := make([][]float32, r)
	for i := range out {
		out[i] = make([]float32, c)
		for j := range out[i] {
			out[i][j] = math.Float32frombits(binary.LittleEndian.Uint32(body[(i*c+j)*4:]))
		}
	}
	return out
}

// savedVectors answers the Vectors stage with the reference's own embeddings,
// which removes the provider's run-to-run noise from the comparison.
type savedVectors struct{ byID map[string][]float32 }

func (s savedVectors) Vectorize(_ context.Context, c *placeorg.Corpus, _, _ placeorg.Embedder) (placeorg.SpaceID, [][]float32, bool, error) {
	out := make([][]float32, len(c.Chats))
	for i, ch := range c.Chats {
		out[i] = s.byID[ch.ID]
	}
	return placeorg.SpacePrimary, out, false, nil
}

// referenceGraph answers the Neighbours stage with the kNN graph the reference
// built. That graph carries the owner's entity-anchor boosts, which come from
// chat text the reference corpus does not ship, so the knn stage cannot
// rebuild it here; its own parity test covers the edge set. Everything
// downstream of the graph is what this test is about.
type referenceGraph struct{ g placeorg.Graph }

func loadReferenceGraph(t *testing.T) referenceGraph {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("community", "testdata", "fixture-graph.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		N     int         `json:"n"`
		Edges [][]float64 `json:"edges"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	g := placeorg.Graph{N: doc.N}
	for _, e := range doc.Edges {
		g.Edges = append(g.Edges, placeorg.Edge{I: int(e[0]), J: int(e[1]), W: float32(e[2])})
	}
	return referenceGraph{g}
}

func (r referenceGraph) KNN(context.Context, [][]float32, map[string]placeorg.AnchorSet, []string) (placeorg.Graph, error) {
	return r.g, nil
}

// keepAll stands in for dedup and kind: the reference corpus is already
// folded and cleaned.
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

// numbered names every place with a distinct word; the reference's names are
// a model's and are not compared.
type numbered struct{}

func (numbered) Fallbacks() []placeorg.Fallback {
	return []placeorg.Fallback{{When: "naming", Does: "a numbered word"}}
}

func (numbered) Name(_ context.Context, t *placeorg.Tree, _ *placeorg.Corpus, _ placeorg.Judge) ([]placeorg.Op, error) {
	var ids []string
	var walk func(string)
	walk = func(id string) {
		ids = append(ids, id)
		n, _ := t.Node(id)
		for _, c := range n.Children {
			walk(c)
		}
	}
	for _, r := range t.Roots() {
		walk(r)
	}
	var ops []placeorg.Op
	for i, id := range ids {
		ops = append(ops, placeorg.Op{Kind: placeorg.OpName, Stage: placeorg.StageName, Place: id,
			Name: "place" + strings.Repeat("x", i+1), Why: "numbered"})
	}
	return ops, nil
}

// beforePark records, for every chat, where the audited tree puts it, which
// is what the reference's root, mid and leaf columns describe.
type beforePark struct {
	inner  placeorg.Parker
	levels map[string][3]string
}

func (b *beforePark) Park(ctx context.Context, t *placeorg.Tree, c *placeorg.Corpus, v [][]float32, s placeorg.SpaceID) ([]placeorg.Op, error) {
	b.levels = map[string][3]string{}
	for _, ch := range c.Chats {
		home, ok := t.Home(ch.ID)
		if !ok {
			b.levels[ch.ID] = [3]string{"now", "now", "now"}
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
			// A chat that sits above a level is labelled by where it sits,
			// the way the reference's negative ids mark a folded place.
			lv[i] = chain[min(i, len(chain)-1)]
			if i >= len(chain) {
				lv[i] = "up:" + chain[len(chain)-1]
			}
		}
		b.levels[ch.ID] = lv
	}
	return b.inner.Park(ctx, t, c, v, s)
}

func (b *beforePark) Thresholds() map[placeorg.SpaceID]placeorg.Thresholds {
	return b.inner.(interface {
		Thresholds() map[placeorg.SpaceID]placeorg.Thresholds
	}).Thresholds()
}

// ari is the adjusted Rand index of two labellings of the same items.
func ari(a, b []string) float64 {
	n := len(a)
	pair := map[[2]string]int{}
	rows, cols := map[string]int{}, map[string]int{}
	for i := range a {
		pair[[2]string{a[i], b[i]}]++
		rows[a[i]]++
		cols[b[i]]++
	}
	c2 := func(x int) float64 { return float64(x*(x-1)) / 2 }
	var sumPair, sumRow, sumCol float64
	for _, v := range pair {
		sumPair += c2(v)
	}
	for _, v := range rows {
		sumRow += c2(v)
	}
	for _, v := range cols {
		sumCol += c2(v)
	}
	expected := sumRow * sumCol / c2(n)
	max := (sumRow + sumCol) / 2
	if max == expected {
		return 1
	}
	return (sumPair - expected) / (max - expected)
}

// refLabel is the reference's place for a chat at one level. A negative id is
// a place the audit folded, so the chat sits at the place above it. The
// reference's Now flag also covers chats the parking floor sent there, whose
// community is intact, so only a root too small to be a place counts as Now
// at every level, which is what dissolving it does.
func refLabel(id int, small bool, fold string) string {
	switch {
	case small:
		return "now"
	case id < 0:
		return "up:" + fold
	}
	return strconv.Itoa(id)
}

func TestTheTreeMatchesTheOwnersReferenceOnItsSavedEmbeddings(t *testing.T) {
	dir := os.Getenv("PLACEORG_REF")
	if dir == "" {
		t.Skip("PLACEORG_REF names the owner's reference directory; this test needs it")
	}
	ids, ref, emb := loadRef(t, dir)
	saved := savedVectors{byID: map[string][]float32{}}
	corpus := &placeorg.Corpus{}
	for i, id := range ids {
		saved.byID[id] = emb[i]
		corpus.Chats = append(corpus.Chats, placeorg.Chat{ID: id, Replies: 2, Created: time.Unix(int64(1_700_000_000+i), 0)})
	}
	parker := &beforePark{inner: park.New()}
	stages := realStages(placeorg.NewState(), t.TempDir(), parker)
	stages.Dedup, stages.Kind, stages.Vectors, stages.Name = keepAll{}, keepAll{}, saved, numbered{}
	stages.Neighbours = loadReferenceGraph(t)
	r := &placeorg.Runner{Stages: stages}
	plan, err := r.Run(context.Background(), corpus, placeorg.Options{Mode: placeorg.ModeRefresh, Seed: 42, Now: time.Now})
	if err != nil {
		t.Fatal(err)
	}
	for _, res := range plan.Invariants {
		if !res.OK {
			t.Errorf("law %q failed: %s", res.Name, res.Why)
		}
	}
	rootSize := map[int]int{}
	for _, c := range ref {
		rootSize[c.Root]++
	}
	for level, name := range []string{"root", "mid", "leaf"} {
		var got, want []string
		for _, id := range ids {
			c := ref[id]
			raw := [3]int{c.Root, c.Mid, c.Leaf}
			fold := strconv.Itoa(c.Root)
			if level == 2 {
				fold = strconv.Itoa(c.Mid) + "/" + fold
			}
			want = append(want, refLabel(raw[level], rootSize[c.Root] < placeorg.Quorum, fold))
			got = append(got, parker.levels[id][level])
		}
		score := ari(got, want)
		t.Logf("ARI %s = %.3f", name, score)
		if score < 0.85 {
			t.Errorf("%s level: ARI %.3f against the reference, want at least 0.85", name, score)
		}
	}
}

func TestTheSameSeedTwiceMakesTheSamePlanWithTheRealStages(t *testing.T) {
	corpus := synthLibrary(900, 6)
	hashed := localembed.NewHashed()
	var plans [2]*placeorg.Plan
	for i := range plans {
		stages := realStages(placeorg.NewState(), t.TempDir(), park.New())
		stages.Name = numbered{}
		r := &placeorg.Runner{Stages: stages, Primary: hashed, Local: hashed}
		var err error
		plans[i], err = r.Run(context.Background(), cloneCorpus(corpus), placeorg.Options{Mode: placeorg.ModeRefresh, Seed: 5, Now: fixedNow})
		if err != nil {
			t.Fatal(err)
		}
	}
	a, b := plans[0], plans[1]
	if len(a.Ops) != len(b.Ops) || a.Summary != b.Summary {
		t.Fatalf("two runs differ: %d ops %q, then %d ops %q", len(a.Ops), a.Summary, len(b.Ops), b.Summary)
	}
	for i := range a.Ops {
		if !reflect.DeepEqual(a.Ops[i], b.Ops[i]) {
			t.Fatalf("op %d differs:\n%+v\n%+v", i, a.Ops[i], b.Ops[i])
		}
	}
}

func cloneCorpus(c *placeorg.Corpus) *placeorg.Corpus {
	return &placeorg.Corpus{Chats: append([]placeorg.Chat(nil), c.Chats...)}
}

func fixedNow() time.Time { return time.Date(2026, 10, 10, 9, 0, 0, 0, time.UTC) }

// ---- scale -------------------------------------------------------------

// synthLibrary builds n chats in topics, each with a few topic words and some
// noise, so the real stages have structure to find.
func synthLibrary(n, topics int) *placeorg.Corpus {
	rng := rand.New(rand.NewSource(1))
	c := &placeorg.Corpus{Chats: make([]placeorg.Chat, n)}
	for i := range c.Chats {
		topic := rng.Intn(topics)
		var words []string
		for w := 0; w < 12; w++ {
			if rng.Intn(3) == 0 {
				words = append(words, fmt.Sprintf("noise%d", rng.Intn(400)))
			} else {
				words = append(words, fmt.Sprintf("topic%dword%d", topic, rng.Intn(20)))
			}
		}
		c.Chats[i] = placeorg.Chat{
			ID: fmt.Sprintf("c%05d", i), Title: strings.Join(words[:4], " "), Text: strings.Join(words, " "),
			Replies: 3, Created: time.Unix(int64(1_700_000_000+i*600), 0),
		}
	}
	return c
}

func TestTenThousandChatsFitInTwoMinutesAndAHundredAndFiftyMegabytes(t *testing.T) {
	if testing.Short() {
		t.Skip("the scale run takes about a minute")
	}
	corpus := synthLibrary(10_000, 8)
	hashed := localembed.NewHashed()
	stages := realStages(placeorg.NewState(), t.TempDir(), park.New())
	stages.Name = numbered{}
	r := &placeorg.Runner{Stages: stages, Primary: hashed, Local: hashed}

	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	var peak uint64
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		var m runtime.MemStats
		for {
			select {
			case <-stop:
				return
			case <-time.After(100 * time.Millisecond):
				runtime.ReadMemStats(&m)
				if m.HeapAlloc > peak {
					peak = m.HeapAlloc
				}
			}
		}
	}()
	start := time.Now()
	plan, err := r.Run(context.Background(), corpus, placeorg.Options{Mode: placeorg.ModeRefresh, Seed: 1, Now: time.Now})
	elapsed := time.Since(start)
	close(stop)
	<-done
	if err != nil {
		t.Fatal(err)
	}
	grew := int64(peak) - int64(before.HeapAlloc)
	t.Logf("10k chats: %v, heap grew %d MB, %d ops, %q", elapsed.Round(time.Second), grew>>20, len(plan.Ops), plan.Summary)
	if elapsed > 2*time.Minute {
		t.Errorf("took %v, want under 2 minutes", elapsed)
	}
	if grew > 150<<20 {
		t.Errorf("the heap grew %d MB, want under 150", grew>>20)
	}
}

var _ = sort.Strings
