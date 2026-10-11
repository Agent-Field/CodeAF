package audit

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/placeorg"
)

func TestCollapseFoldsADominantChildAndAUnaryChain(t *testing.T) {
	big := []string{"c0", "c1", "c2", "c3", "c4", "c5", "c6"}
	small := []string{"c7", "c8", "c9"}
	nodes := map[string]*placeorg.Node{
		"r":     placeNode("r", "", "Root", 1, []string{"big", "small"}, nil),
		"big":   placeNode("big", "r", "Big", 2, nil, big),
		"small": placeNode("small", "r", "Small", 2, nil, small),
	}
	tree := plantTree([]string{"r"}, nodes, nil, nil)
	ops, err := Auditor{}.Audit(context.Background(), tree, corpusOf(append(append([]string{}, big...), small...)...), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var collapsed []string
	for _, op := range ops {
		if op.Kind == placeorg.OpCollapse {
			collapsed = append(collapsed, op.Place)
		}
	}
	if len(collapsed) != 1 || collapsed[0] != "big" {
		t.Fatalf("dominant child: collapsed %v, want big", collapsed)
	}
	if !ops[0].Proposed || ops[0].Stage != placeorg.StageAudit {
		t.Fatalf("collapse should be a proposed audit op: %+v", ops[0])
	}

	only := []string{"a", "b", "c", "d", "e"}
	unary := map[string]*placeorg.Node{
		"r": placeNode("r", "", "Root", 1, []string{"m"}, nil),
		"m": placeNode("m", "r", "Mid", 2, nil, only),
	}
	ops, err = Auditor{}.Audit(context.Background(), plantTree([]string{"r"}, unary, nil, nil), corpusOf(only...), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) != 1 || ops[0].Kind != placeorg.OpCollapse || ops[0].Place != "m" {
		t.Fatalf("unary chain: %+v", ops)
	}

	// Six of ten is under 70%, so neither child folds.
	six := []string{"a0", "a1", "a2", "a3", "a4", "a5"}
	four := []string{"b0", "b1", "b2", "b3"}
	under := map[string]*placeorg.Node{
		"r":    placeNode("r", "", "Root", 1, []string{"six", "four"}, nil),
		"six":  placeNode("six", "r", "Six", 2, nil, six),
		"four": placeNode("four", "r", "Four", 2, nil, four),
	}
	ops, err = Auditor{}.Audit(context.Background(), plantTree([]string{"r"}, under, nil, nil), corpusOf(append(append([]string{}, six...), four...)...), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, op := range ops {
		if op.Kind == placeorg.OpCollapse {
			t.Fatalf("six of ten folded: %+v", op)
		}
	}
}

func TestCollapseLeavesAFinerUnaryChain(t *testing.T) {
	// The finest place holds every chat of its parent, but that parent is
	// only half of the top-level place. The reference folds the middle into
	// the top, and leaves this finer chain alone.
	leaf := []string{"a0", "a1", "a2", "a3", "a4"}
	other := []string{"b0", "b1", "b2", "b3", "b4"}
	nodes := map[string]*placeorg.Node{
		"r":  placeNode("r", "", "Root", 1, []string{"a", "b"}, nil),
		"a":  placeNode("a", "r", "A", 2, []string{"a1"}, nil),
		"a1": placeNode("a1", "a", "A1", 3, nil, leaf),
		"b":  placeNode("b", "r", "B", 2, nil, other),
	}
	ops, err := Auditor{}.Audit(context.Background(), plantTree([]string{"r"}, nodes, nil, nil), corpusOf(append(append([]string{}, leaf...), other...)...), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, op := range ops {
		if op.Kind == placeorg.OpCollapse {
			t.Fatalf("finer chain was folded: %+v", op)
		}
	}
}

func TestDissolveIsBottomUpAndSendsASmallRootToNow(t *testing.T) {
	// The middle place is not the only child of the top one. A chain of one
	// would fold first, and the small place inside it would never be reached.
	tiny := []string{"t0", "t1"}
	ok := []string{"o0", "o1", "o2", "o3"}
	side := []string{"s0", "s1", "s2", "s3"}
	nodes := map[string]*placeorg.Node{
		"r":    placeNode("r", "", "Root", 1, []string{"m", "side"}, nil),
		"m":    placeNode("m", "r", "Mid", 2, []string{"tiny", "ok"}, nil),
		"side": placeNode("side", "r", "Side", 2, nil, side),
		"tiny": placeNode("tiny", "m", "Tiny", 3, nil, tiny),
		"ok":   placeNode("ok", "m", "Ok", 3, nil, ok),
	}
	ids := append(append(append([]string{}, tiny...), ok...), side...)
	ops, err := Auditor{}.Audit(context.Background(), plantTree([]string{"r"}, nodes, nil, nil), corpusOf(ids...), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var dissolved []placeorg.Op
	for _, op := range ops {
		if op.Kind == placeorg.OpDissolve {
			dissolved = append(dissolved, op)
		}
	}
	if len(dissolved) != 1 || dissolved[0].Place != "tiny" || dissolved[0].Parent != "m" || len(dissolved[0].Members) != 2 {
		t.Fatalf("bottom-up dissolve: %+v", dissolved)
	}

	small := []string{"s0", "s1", "s2"}
	root := map[string]*placeorg.Node{
		"r": placeNode("r", "", "Root", 1, nil, small),
	}
	ops, err = Auditor{}.Audit(context.Background(), plantTree([]string{"r"}, root, nil, nil), corpusOf(small...), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) != 1 || ops[0].Kind != placeorg.OpDissolve || ops[0].Parent != "" || ops[0].Place != "r" {
		t.Fatalf("small root: %+v", ops)
	}
}

func TestInterposeRespectsFanoutShareAndSeparability(t *testing.T) {
	// Every place holds at least a quorum, so none is removed before the
	// grouping runs. A place of one chat would be gone already.
	pair := interposeN(t, 10, 4, 4, 0.75)
	if len(pair) != 1 || pair[0].Kind != placeorg.OpInterpose || !pair[0].Proposed {
		t.Fatalf("pair should be one proposed interpose, got %+v", pair)
	}
	if len(pair[0].Children) != 2 || pair[0].Children[0] != "r0" || pair[0].Children[1] != "r1" {
		t.Fatalf("children: %v", pair[0].Children)
	}

	if ops := interposeN(t, 10, 4, 4, 0.95); len(ops) != 0 {
		t.Fatalf("too-alike places were grouped: %+v", ops)
	}
	if ops := interposeN(t, 10, 20, 4, 0.75); len(ops) != 0 {
		t.Fatalf("oversized group was offered: %+v", ops)
	}
	if ops := interposeN(t, 9, 4, 4, 0.75); len(ops) != 0 {
		t.Fatalf("nine places were grouped: %+v", ops)
	}
}

// interposeN builds nRoots top-level places. The first two share cosine sim
// and hold pairChats each. The rest share one distant vector and otherChats.
func interposeN(t *testing.T, nRoots, pairChats, otherChats int, sim float32) []placeorg.Op {
	t.Helper()
	v2 := []float32{sim, float32(math.Sqrt(float64(1 - sim*sim)))}
	far := []float32{-1, 0}
	nodes := map[string]*placeorg.Node{}
	var roots []string
	var ids []string
	var vecs [][]float32
	for r := 0; r < nRoots; r++ {
		id := "r" + itoa(r)
		roots = append(roots, id)
		nChats := otherChats
		vec := far
		if r == 0 {
			nChats = pairChats
			vec = []float32{1, 0}
		}
		if r == 1 {
			nChats = pairChats
			vec = v2
		}
		var members []string
		for c := 0; c < nChats; c++ {
			cid := id + "c" + itoa(c)
			members = append(members, cid)
			ids = append(ids, cid)
			vecs = append(vecs, vec)
		}
		nodes[id] = placeNode(id, "", "P"+itoa(r), 1, nil, members)
	}
	ops, err := Auditor{}.Audit(context.Background(), plantTree(roots, nodes, nil, nil), corpusOf(ids...), vecs, &fixedJudge{key: "THING"})
	if err != nil {
		t.Fatal(err)
	}
	var out []placeorg.Op
	for _, op := range ops {
		if op.Kind == placeorg.OpInterpose {
			out = append(out, op)
		}
	}
	return out
}

func TestFallbackDemotesNothing(t *testing.T) {
	ops := twoMids(t, &fixedJudge{fell: true, key: "ACTIVITY"})
	for _, op := range ops {
		if op.Kind == placeorg.OpDemote {
			t.Fatalf("fallback demoted: %+v", op)
		}
	}
}

func TestActivityDemotesAndThingKeeps(t *testing.T) {
	activity := &fixedJudge{key: "ACTIVITY"}
	ops := twoMids(t, activity)
	n := 0
	for _, op := range ops {
		if op.Kind == placeorg.OpDemote {
			n++
		}
	}
	if n != 2 || activity.calls != 2 {
		t.Fatalf("activity: %d demotes, %d calls", n, activity.calls)
	}
	thing := &fixedJudge{key: "THING"}
	ops = twoMids(t, thing)
	for _, op := range ops {
		if op.Kind == placeorg.OpDemote {
			t.Fatalf("thing demoted: %+v", op)
		}
	}
	if thing.calls != 2 {
		t.Fatalf("thing calls: %d", thing.calls)
	}
}

func TestAnchorDecidesWithoutAJudge(t *testing.T) {
	a := []string{"a0", "a1", "a2", "a3"}
	b := []string{"b0", "b1", "b2", "b3"}
	nodes := map[string]*placeorg.Node{
		"r": placeNode("r", "", "Root", 1, []string{"a", "b"}, nil),
		"a": placeNode("a", "r", "A", 2, nil, a),
		"b": placeNode("b", "r", "B", 2, nil, b),
	}
	c := corpusOf(append(append([]string{}, a...), b...)...)
	c.Anchors = map[string]placeorg.AnchorSet{}
	for _, id := range a {
		c.Anchors[id] = placeorg.AnchorSet{"alpha": 1}
	}
	for _, id := range b {
		c.Anchors[id] = placeorg.AnchorSet{"beta": 1}
	}
	judge := &fixedJudge{key: "ACTIVITY"}
	ops, err := Auditor{}.Audit(context.Background(), plantTree([]string{"r"}, nodes, nil, nil), c, nil, judge)
	if err != nil {
		t.Fatal(err)
	}
	if judge.calls != 0 {
		t.Fatalf("anchor still asked the judge %d times", judge.calls)
	}
	for _, op := range ops {
		if op.Kind == placeorg.OpDemote {
			t.Fatalf("anchored place was demoted: %+v", op)
		}
	}
}

func TestSplitTwoGroupsAndStopsAtDepthAndSize(t *testing.T) {
	ids, vecs := twoBlobs(5, 5)
	ops := splitLeaf(t, 3, ids, vecs, &fixedJudge{key: "THING"})
	n := 0
	for _, op := range ops {
		if op.Kind == placeorg.OpSplit {
			n++
			if !op.Proposed || op.Parent != "leaf" {
				t.Fatalf("split op: %+v", op)
			}
		}
	}
	if n != 2 {
		t.Fatalf("want 2 split ops, got %d from %+v", n, ops)
	}

	if ops := splitLeaf(t, 6, ids, vecs, &fixedJudge{key: "THING"}); hasKind(ops, placeorg.OpSplit) {
		t.Fatalf("depth 6 split: %+v", ops)
	}
	small, smallVecs := twoBlobs(4, 4)
	if ops := splitLeaf(t, 3, small, smallVecs, &fixedJudge{key: "THING"}); hasKind(ops, placeorg.OpSplit) {
		t.Fatalf("under the size floor: %+v", ops)
	}
	one, oneVecs := twoBlobs(7, 3)
	if ops := splitLeaf(t, 3, one, oneVecs, &fixedJudge{key: "THING"}); hasKind(ops, placeorg.OpSplit) {
		t.Fatalf("one quorum-sized group split: %+v", ops)
	}
}

func TestUnaryFixtureCollapses(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "unary.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Roots []string `json:"roots"`
		Nodes []struct {
			ID       string   `json:"id"`
			Parent   string   `json:"parent"`
			Depth    int      `json:"depth"`
			Children []string `json:"children"`
			Members  []string `json:"members"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	nodes := map[string]*placeorg.Node{}
	var ids []string
	for _, n := range doc.Nodes {
		nodes[n.ID] = placeNode(n.ID, n.Parent, n.ID, n.Depth, n.Children, n.Members)
		ids = append(ids, n.Members...)
	}
	ops, err := Auditor{}.Audit(context.Background(), plantTree(doc.Roots, nodes, nil, nil), corpusOf(ids...), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) != 1 || ops[0].Kind != placeorg.OpCollapse || ops[0].Place != "m" {
		t.Fatalf("fixture: %+v", ops)
	}
}

func twoMids(t *testing.T, judge placeorg.Judge) []placeorg.Op {
	t.Helper()
	a := []string{"a0", "a1", "a2", "a3"}
	b := []string{"b0", "b1", "b2", "b3"}
	nodes := map[string]*placeorg.Node{
		"r": placeNode("r", "", "Root", 1, []string{"a", "b"}, nil),
		"a": placeNode("a", "r", "A", 2, nil, a),
		"b": placeNode("b", "r", "B", 2, nil, b),
	}
	ops, err := Auditor{}.Audit(context.Background(), plantTree([]string{"r"}, nodes, nil, nil), corpusOf(append(append([]string{}, a...), b...)...), nil, judge)
	if err != nil {
		t.Fatal(err)
	}
	return ops
}

func splitLeaf(t *testing.T, depth int, ids []string, vecs [][]float32, judge placeorg.Judge) []placeorg.Op {
	t.Helper()
	// A sibling keeps the middle place under 70% of the top one. Without it
	// the chain folds and the leaf is gone before a split is considered.
	side := []string{"s0", "s1", "s2", "s3", "s4", "s5"}
	nodes := map[string]*placeorg.Node{
		"r":    placeNode("r", "", "Root", 1, []string{"m", "side"}, nil),
		"m":    placeNode("m", "r", "Mid", 2, []string{"leaf"}, nil),
		"side": placeNode("side", "r", "Side", 2, nil, side),
		"leaf": placeNode("leaf", "m", "Leaf", depth, nil, ids),
	}
	all := append(append([]string{}, ids...), side...)
	allVecs := append([][]float32{}, vecs...)
	for range side {
		allVecs = append(allVecs, []float32{0.6, 0.8})
	}
	ops, err := Auditor{}.Audit(context.Background(), plantTree([]string{"r"}, nodes, nil, nil), corpusOf(all...), allVecs, judge)
	if err != nil {
		t.Fatal(err)
	}
	return ops
}

func twoBlobs(n, m int) ([]string, [][]float32) {
	var ids []string
	var vecs [][]float32
	for i := 0; i < n; i++ {
		ids = append(ids, "a"+itoa(i))
		vecs = append(vecs, []float32{1, 0})
	}
	for i := 0; i < m; i++ {
		ids = append(ids, "b"+itoa(i))
		vecs = append(vecs, []float32{0, 1})
	}
	return ids, vecs
}

func hasKind(ops []placeorg.Op, kind placeorg.OpKind) bool {
	for _, op := range ops {
		if op.Kind == kind {
			return true
		}
	}
	return false
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [8]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

type fixedJudge struct {
	key   string
	fell  bool
	calls int
}

func (j *fixedJudge) Choose(_ context.Context, _ placeorg.Question, fallback placeorg.Option) placeorg.Pick {
	j.calls++
	if j.fell {
		return placeorg.Pick{Key: fallback.Key, FellBack: true, Why: "test"}
	}
	return placeorg.Pick{Key: j.key}
}

func (j *fixedJudge) Write(_ context.Context, _ placeorg.Question, _ placeorg.Validator, fallback string) placeorg.Pick {
	return placeorg.Pick{Written: fallback, FellBack: true}
}
