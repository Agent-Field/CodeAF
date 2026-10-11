package audit

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/placeorg"
)

// TestFixtureLiftsTheFalseParent is the latest tree's false parent: Codeaf
// Spark and Agent Fieldwork under Uwm Japa. The two places share no derived
// anchor and their centroids sit below sepMin, so each one becomes a root.
// The chats filed on the root stay there.
func TestFixtureLiftsTheFalseParent(t *testing.T) {
	tree, corpus, vecs := loadUnrelated(t)
	ops, err := Auditor{}.Audit(context.Background(), tree, corpus, vecs, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) != 1 || ops[0].Kind != placeorg.OpLift {
		t.Fatalf("ops: %+v", ops)
	}
	op := ops[0]
	if !op.Proposed || op.Stage != placeorg.StageAudit || op.Place != "uwm" || op.Parent != "" {
		t.Fatalf("lift op: %+v", op)
	}
	if len(op.Children) != 2 || op.Children[0] != "field" || op.Children[1] != "spark" {
		t.Fatalf("children: %v", op.Children)
	}
	if op.Why != "These places are not about one thing, so each one stands on its own." {
		t.Fatalf("why: %q", op.Why)
	}
	for _, word := range []string{"anchor", "centroid", "embedding", "cosine", "auditor"} {
		if strings.Contains(strings.ToLower(op.Why), word) {
			t.Fatalf("why uses %q: %q", word, op.Why)
		}
	}
	if len(op.Members) != 8 {
		t.Fatalf("members: %v", op.Members)
	}

	f := loadForest(tree)
	if got := (Auditor{}).lift(f, indexCorpus(corpus, vecs)); len(got) != 1 {
		t.Fatalf("direct lift: %+v", got)
	}
	if f.places["spark"].parent != "" || f.places["spark"].depth != 1 {
		t.Fatalf("spark stayed nested: %+v", f.places["spark"])
	}
	if f.places["field"].parent != "" || f.places["field"].depth != 1 {
		t.Fatalf("field stayed nested: %+v", f.places["field"])
	}
	parent := f.places["uwm"]
	if len(parent.children) != 0 || len(parent.members) != 4 {
		t.Fatalf("root chats: children %v members %v", parent.children, parent.members)
	}
}

func TestLiftMovesANestedPlaceUpWithItsParent(t *testing.T) {
	built := falseParent("uwm", "spark", "field", "spark", "field")
	leaf := []string{"l0", "l1", "l2", "l3"}
	built.nodes["spark"].Children = []string{"leaf"}
	built.nodes["leaf"] = placeNode("leaf", "spark", "Leaf", 3, nil, leaf)
	for _, id := range leaf {
		built.anchors[id] = placeorg.AnchorSet{"spark": 1}
		built.vectors[id] = []float32{1, 0}
	}
	tree, corpus, vecs := plantBuilt(built)
	f := loadForest(tree)
	if ops := (Auditor{}).lift(f, indexCorpus(corpus, vecs)); len(ops) != 1 {
		t.Fatalf("ops: %+v", ops)
	}
	if f.places["leaf"].parent != "spark" || f.places["leaf"].depth != 2 {
		t.Fatalf("leaf did not move up with spark: %+v", f.places["leaf"])
	}
	if f.places["spark"].depth != 1 {
		t.Fatalf("spark depth %d", f.places["spark"].depth)
	}
}

func TestLiftKeepsPlacesThatShareAnAnchorOrSitTogether(t *testing.T) {
	shared := falseParent("uwm", "spark", "field", "spark", "field")
	for id := range shared.anchors {
		shared.anchors[id]["both"] = 1
	}
	if ops := liftOf(t, shared); len(ops) != 0 {
		t.Fatalf("a shared anchor was separated: %+v", ops)
	}

	close := falseParent("uwm", "spark", "field", "spark", "field")
	for id, v := range close.vectors {
		if v[0] == 0 && v[1] == 1 {
			close.vectors[id] = []float32{0.9, float32(math.Sqrt(1 - 0.81))}
		}
	}
	if ops := liftOf(t, close); len(ops) != 0 {
		t.Fatalf("places that sit together were separated: %+v", ops)
	}

	one := falseParent("uwm", "spark", "field", "spark", "field")
	delete(one.nodes, "field")
	one.nodes["uwm"].Children = []string{"spark"}
	if ops := liftOf(t, one); len(ops) != 0 {
		t.Fatalf("a single child was separated: %+v", ops)
	}

	blind := falseParent("uwm", "spark", "field", "spark", "field")
	blind.vectors = nil
	if ops := liftOf(t, blind); len(ops) != 0 {
		t.Fatalf("places with no position were separated: %+v", ops)
	}
}

func TestSepMinStaysBelowTheReferenceParents(t *testing.T) {
	// Measured on the saved reference embeddings, after the same collapse
	// and dissolve filter this stage runs first. The least similar places
	// a real parent kept were 0.739.
	const lowestKept = 0.739
	if sepMin >= lowestKept {
		t.Fatalf("sepMin %v is not under the least similar parent the reference kept (%v)", sepMin, lowestKept)
	}
	if sepMin != 0.45 {
		t.Fatalf("sepMin moved from 0.45 to %v without a new measurement", sepMin)
	}
}

func TestLiftKeepsTheParentWhenTheTopCannotHoldThePlaces(t *testing.T) {
	built := fanoutCase(8)
	f := loadForest(built.tree)
	ops := Auditor{}.lift(f, indexCorpus(built.corpus, built.vecs))
	if len(ops) != 1 || ops[0].Kind != placeorg.OpLift || !ops[0].Proposed || ops[0].Place != "uwm" {
		t.Fatalf("proposal: %+v", ops)
	}
	if !strings.Contains(ops[0].Why, "stay inside this one") || !strings.Contains(ops[0].Why, "9") {
		t.Fatalf("why: %q", ops[0].Why)
	}
	if f.places["spark"].parent != "uwm" || f.places["field"].parent != "uwm" {
		t.Fatal("the parent was taken apart even though the top cannot hold the places")
	}

	// Seven roots plus the two places is nine, which the top can hold.
	fits := fanoutCase(6)
	ff := loadForest(fits.tree)
	fitOps := Auditor{}.lift(ff, indexCorpus(fits.corpus, fits.vecs))
	if len(fitOps) != 1 || ff.places["spark"].parent != "" {
		t.Fatalf("a top that can hold the places kept the parent: %+v parent %q", fitOps, ff.places["spark"].parent)
	}
}

func TestLiftStopsOnceTheTopIsFull(t *testing.T) {
	// Two false parents and five other roots. Separating the first puts
	// nine at the top, so the second stays a parent and is still proposed.
	a := falseParent("a", "a-spark", "a-field", "as", "af")
	b := falseParent("b", "b-spark", "b-field", "bs", "bf")
	built := mergeParents([]*builtTree{a, b}, 5)
	f := loadForest(built.tree)
	ops := Auditor{}.lift(f, indexCorpus(built.corpus, built.vecs))
	if len(ops) != 2 {
		t.Fatalf("ops: %+v", ops)
	}
	if f.places["a-spark"].parent != "" || f.places["b-spark"].parent != "b" {
		t.Fatalf("first parent %q, second parent %q", f.places["a-spark"].parent, f.places["b-spark"].parent)
	}
	if !ops[1].Proposed || !strings.Contains(ops[1].Why, "stay inside this one") {
		t.Fatalf("second op: %+v", ops[1])
	}
}

func TestReplayAcceptsAProposedLiftAndAnAppliedOne(t *testing.T) {
	tree, _, _ := loadUnrelated(t)
	op := placeorg.Op{
		Kind: placeorg.OpLift, Stage: placeorg.StageAudit, Place: "uwm",
		Children: []string{"field", "spark"}, Proposed: true,
		Why: "These places are not about one thing, so each one stands on its own.",
	}
	if why := replayWhy(tree, op); why != "" {
		t.Fatal(why)
	}
	op.Proposed = false
	if why := replayWhy(tree, op); why == "" {
		t.Fatal("an applied lift was accepted while the places were still nested")
	}
	lifted := placeorg.TreeFromNodes([]placeorg.Node{
		{ID: "uwm", Name: "Uwm Japa", Depth: 1, Members: []string{"p0", "p1", "p2", "p3"}},
		{ID: "spark", Name: "Codeaf Spark", Depth: 1, Members: []string{"s0", "s1", "s2", "s3"}},
		{ID: "field", Name: "Agent Fieldwork", Depth: 1, Members: []string{"f0", "f1", "f2", "f3"}},
	})
	if why := replayWhy(lifted, op); why != "" {
		t.Fatal(why)
	}
}

type builtTree struct {
	tree    *placeorg.Tree
	corpus  *placeorg.Corpus
	vecs    [][]float32
	anchors map[string]placeorg.AnchorSet
	nodes   map[string]*placeorg.Node
	vectors map[string][]float32
}

func loadUnrelated(t *testing.T) (*placeorg.Tree, *placeorg.Corpus, [][]float32) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "unrelated.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Roots []string `json:"roots"`
		Nodes []struct {
			ID       string   `json:"id"`
			Parent   string   `json:"parent"`
			Name     string   `json:"name"`
			Depth    int      `json:"depth"`
			Children []string `json:"children"`
			Members  []string `json:"members"`
		} `json:"nodes"`
		Anchors map[string]placeorg.AnchorSet `json:"anchors"`
		Vectors map[string][]float32          `json:"vectors"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	nodes := map[string]*placeorg.Node{}
	var ids []string
	var vecs [][]float32
	for _, n := range doc.Nodes {
		nodes[n.ID] = placeNode(n.ID, n.Parent, n.Name, n.Depth, n.Children, n.Members)
		for _, id := range n.Members {
			ids = append(ids, id)
			vecs = append(vecs, doc.Vectors[id])
		}
	}
	corpus := corpusOf(ids...)
	corpus.Anchors = doc.Anchors
	return plantTree(doc.Roots, nodes, nil, nil), corpus, vecs
}

func liftOf(t *testing.T, b *builtTree) []placeorg.Op {
	t.Helper()
	tree, corpus, vecs := plantBuilt(b)
	ops, err := Auditor{}.Audit(context.Background(), tree, corpus, vecs, nil)
	if err != nil {
		t.Fatal(err)
	}
	var out []placeorg.Op
	for _, op := range ops {
		if op.Kind == placeorg.OpLift {
			out = append(out, op)
		}
	}
	return out
}

func plantBuilt(b *builtTree) (*placeorg.Tree, *placeorg.Corpus, [][]float32) {
	var roots []string
	var ids []string
	var vecs [][]float32
	for id, n := range b.nodes {
		if n.Parent == "" {
			roots = append(roots, id)
		}
		for _, m := range n.Members {
			ids = append(ids, m)
			if b.vectors != nil {
				vecs = append(vecs, b.vectors[m])
			}
		}
	}
	corpus := corpusOf(ids...)
	corpus.Anchors = b.anchors
	if b.vectors == nil {
		vecs = nil
	}
	return plantTree(roots, b.nodes, nil, nil), corpus, vecs
}

func fanoutCase(extra int) *builtTree {
	return mergeParents([]*builtTree{falseParent("uwm", "spark", "field", "spark", "field")}, extra)
}

func falseParent(root, spark, field, sparkMark, fieldMark string) *builtTree {
	flat := []string{root + "p0", root + "p1", root + "p2", root + "p3"}
	sm := []string{spark + "0", spark + "1", spark + "2", spark + "3"}
	fm := []string{field + "0", field + "1", field + "2", field + "3"}
	nodes := map[string]*placeorg.Node{
		root:  placeNode(root, "", root, 1, []string{spark, field}, flat),
		spark: placeNode(spark, root, spark, 2, nil, sm),
		field: placeNode(field, root, field, 2, nil, fm),
	}
	anchors := map[string]placeorg.AnchorSet{}
	vectors := map[string][]float32{}
	for _, id := range sm {
		anchors[id] = placeorg.AnchorSet{sparkMark: 1}
		vectors[id] = []float32{1, 0}
	}
	for _, id := range fm {
		anchors[id] = placeorg.AnchorSet{fieldMark: 1}
		vectors[id] = []float32{0, 1}
	}
	for _, id := range flat {
		vectors[id] = []float32{0.6, 0.6}
	}
	return &builtTree{nodes: nodes, anchors: anchors, vectors: vectors}
}

func mergeParents(parents []*builtTree, extra int) *builtTree {
	nodes := map[string]*placeorg.Node{}
	anchors := map[string]placeorg.AnchorSet{}
	vectors := map[string][]float32{}
	var roots []string
	var ids []string
	var vecs [][]float32
	for _, p := range parents {
		for id, n := range p.nodes {
			nodes[id] = n
			if n.Parent == "" {
				roots = append(roots, id)
			}
		}
		for id, set := range p.anchors {
			anchors[id] = set
		}
		for id, v := range p.vectors {
			vectors[id] = v
		}
	}
	for i := 0; i < extra; i++ {
		id := "r" + itoa(i)
		var members []string
		for c := 0; c < placeorg.Quorum; c++ {
			cid := id + "c" + itoa(c)
			members = append(members, cid)
			vectors[cid] = []float32{0, -1}
		}
		nodes[id] = placeNode(id, "", id, 1, nil, members)
		roots = append(roots, id)
	}
	for _, n := range nodes {
		for _, m := range n.Members {
			ids = append(ids, m)
			vecs = append(vecs, vectors[m])
		}
	}
	corpus := corpusOf(ids...)
	corpus.Anchors = anchors
	return &builtTree{
		tree: plantTree(roots, nodes, nil, nil), corpus: corpus, vecs: vecs,
		nodes: nodes, anchors: anchors, vectors: vectors,
	}
}

func replayWhy(tree *placeorg.Tree, op placeorg.Op) string {
	results, _ := placeorg.Check(tree, &placeorg.Plan{Ops: []placeorg.Op{op}}, &placeorg.Corpus{})
	for _, r := range results {
		if strings.Contains(r.Name, "journal") {
			if r.OK {
				return ""
			}
			return r.Why
		}
	}
	return "the journal law was not checked"
}
