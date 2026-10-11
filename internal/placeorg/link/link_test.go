package link

import (
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"unsafe"

	"github.com/Agent-Field/codeaf/internal/placeorg"
)

// treeMirror is placeorg.Tree with its fields exported to this package.
// The tree's mutators are unexported and the pipeline helper that will
// build trees does not exist yet, so a test plants one through this mirror.
// The size check fails the day a field is added; a reordering of fields
// would plant into the wrong slot, and the round-trip below catches that.
type treeMirror struct {
	nodes map[string]*placeorg.Node
	roots []string
	home  map[string]string
	links map[string][]string
	inbox map[string]bool
}

func plant(t *testing.T, roots []string, nodes ...*placeorg.Node) *placeorg.Tree {
	t.Helper()
	if unsafe.Sizeof(treeMirror{}) != unsafe.Sizeof(placeorg.Tree{}) {
		t.Fatal("treeMirror no longer matches placeorg.Tree")
	}
	tree := placeorg.NewTree()
	view := (*treeMirror)(unsafe.Pointer(tree))
	view.nodes = map[string]*placeorg.Node{}
	view.home = map[string]string{}
	view.links = map[string][]string{}
	view.inbox = map[string]bool{}
	view.roots = append([]string(nil), roots...)
	for _, n := range nodes {
		view.nodes[n.ID] = n
		for _, chat := range n.Members {
			if _, ok := view.home[chat]; !ok {
				view.home[chat] = n.ID
			}
		}
	}
	if got := tree.Roots(); !reflect.DeepEqual(got, roots) {
		t.Fatalf("planted roots %v, read back %v; the mirror does not match tree.go", roots, got)
	}
	return tree
}

func node(id string, depth int, members []string, children []string, centroid []float32) *placeorg.Node {
	n := &placeorg.Node{ID: id, Depth: depth, Members: members, Children: children}
	if centroid != nil {
		n.Centroids = map[placeorg.SpaceID]placeorg.Centroid{
			"ref": {Sum: append([]float32(nil), centroid...), Count: 1},
		}
	}
	return n
}

func chatsOf(ids ...string) *placeorg.Corpus {
	c := &placeorg.Corpus{}
	for _, id := range ids {
		c.Chats = append(c.Chats, placeorg.Chat{ID: id})
	}
	return c
}

func vectorsOf(vs ...[]float32) [][]float32 { return vs }

func linksOf(ops []placeorg.Op) map[string][]string {
	got := map[string][]string{}
	for _, op := range ops {
		if op.Kind != placeorg.OpLink {
			continue
		}
		got[op.Chat] = append(got[op.Chat], op.Place)
	}
	return got
}

func lensesOf(ops []placeorg.Op) []placeorg.Op {
	var out []placeorg.Op
	for _, op := range ops {
		if op.Kind == placeorg.OpLens {
			out = append(out, op)
		}
	}
	return out
}

func TestShortcutFixtureUsesTheMarginAndIgnoresShallowerPlaces(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "shortcuts.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fix struct {
		Places []struct {
			ID       string    `json:"id"`
			Depth    int       `json:"depth"`
			Centroid []float32 `json:"centroid"`
		} `json:"places"`
		Chats []struct {
			ID        string    `json:"id"`
			Vector    []float32 `json:"vector"`
			Shortcuts []string  `json:"shortcuts"`
		} `json:"chats"`
	}
	if err := json.Unmarshal(raw, &fix); err != nil {
		t.Fatal(err)
	}
	var nodes []*placeorg.Node
	var roots []string
	var children []string
	for _, p := range fix.Places {
		nodes = append(nodes, node(p.ID, p.Depth, nil, nil, p.Centroid))
		if p.Depth == 1 {
			roots = append(roots, p.ID)
		} else {
			children = append(children, p.ID)
		}
	}
	if len(roots) != 1 {
		t.Fatalf("the fixture needs one depth-1 place, has %v", roots)
	}
	// The finer places hang under the root so the forest can reach them.
	// Their depth stays 3, so they are the shortcut candidates. The depth-1
	// place is not scored.
	for i, p := range fix.Places {
		if p.Depth == 1 {
			nodes[i].Children = children
		}
	}
	var ids []string
	var vecs [][]float32
	want := map[string][]string{}
	for _, c := range fix.Chats {
		ids = append(ids, c.ID)
		vecs = append(vecs, c.Vector)
		if len(c.Shortcuts) > 0 {
			want[c.ID] = c.Shortcuts
		}
	}
	ops, err := New(nil).Link(context.Background(), plant(t, roots, nodes...), chatsOf(ids...), vecs)
	if err != nil {
		t.Fatal(err)
	}
	if got := linksOf(ops); !reflect.DeepEqual(got, want) {
		t.Fatalf("shortcuts %v, want %v", got, want)
	}
	for _, op := range ops {
		if op.Kind == placeorg.OpLink && op.Why != whyAlsoIn {
			t.Fatalf("link why %q, want %q", op.Why, whyAlsoIn)
		}
	}
}

func TestAtMostTwoShortcutsAndARaisedFloorDropsTheNeighbour(t *testing.T) {
	same := []float32{1, 0, 0, 0}
	ops, err := New(nil).Link(context.Background(), plant(t, []string{"root"},
		node("root", 1, nil, []string{"a", "b", "c"}, nil),
		node("a", 2, nil, nil, same),
		node("b", 2, nil, nil, same),
		node("c", 2, nil, nil, same),
	), chatsOf("chat"), vectorsOf(same))
	if err != nil {
		t.Fatal(err)
	}
	got := linksOf(ops)["chat"]
	if len(got) != placeorg.MaxLinks {
		t.Fatalf("got %v, want %d shortcuts", got, placeorg.MaxLinks)
	}
	// The tied best is the lowest id, "a", and it is not a shortcut.
	if !reflect.DeepEqual(got, []string{"b", "c"}) {
		t.Fatalf("tied places linked %v, want b and c", got)
	}

	state := &placeorg.State{
		Space:      "ref",
		Thresholds: map[placeorg.SpaceID]placeorg.Thresholds{"ref": {Floor: 0.995, Delta: 0.03}},
	}
	near := []float32{0.99, 0.14106736, 0, 0}
	tree := plant(t, []string{"root"},
		node("root", 1, nil, []string{"alpha", "beta"}, nil),
		node("alpha", 2, nil, nil, []float32{1, 0, 0, 0}),
		node("beta", 2, nil, nil, near),
	)
	ops, err = New(state).Link(context.Background(), tree, chatsOf("chat"), vectorsOf([]float32{1, 0, 0, 0}))
	if err != nil {
		t.Fatal(err)
	}
	if got := linksOf(ops)["chat"]; len(got) != 0 {
		t.Fatalf("a neighbour below the calibrated floor was linked: %v", got)
	}
}

func TestAProbeIsNotAlsoInAPlace(t *testing.T) {
	corpus := chatsOf("probe", "real")
	corpus.Kinds = map[string]placeorg.Kind{"probe": placeorg.KindProbe}
	tree := plant(t, []string{"root"},
		node("root", 1, nil, []string{"alpha", "beta"}, nil),
		node("alpha", 2, nil, nil, []float32{1, 0, 0, 0}),
		node("beta", 2, nil, nil, []float32{1, 0, 0, 0}),
	)
	ops, err := New(nil).Link(context.Background(), tree, corpus, vectorsOf([]float32{1, 0, 0, 0}, []float32{1, 0, 0, 0}))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := linksOf(ops)["probe"]; ok {
		t.Fatal("a probe was given a shortcut")
	}
	if got := linksOf(ops)["real"]; len(got) != 1 {
		t.Fatalf("real chat shortcuts %v, want one", got)
	}
}

func TestByMonthOnlyOnALargeTopLevelPlace(t *testing.T) {
	wide := ids("w", monthPlaceChats)
	tiny := ids("t", 4)
	big := ids("b", 20)
	tree := plant(t, []string{"wide", "tiny"},
		node("wide", 1, wide, nil, nil),
		node("tiny", 1, tiny, []string{"big"}, nil),
		node("big", 2, big, nil, nil),
	)
	ops, err := New(nil).Link(context.Background(), tree, &placeorg.Corpus{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var places []string
	for _, op := range lensesOf(ops) {
		if op.Into != axisMonth || op.Name != nameByMonth || op.Why != whyByMonth {
			t.Fatalf("month lens %+v", op)
		}
		if op.Place == "big" {
			t.Fatal("a child place was given the by-month view")
		}
		places = append(places, op.Place)
	}
	// tiny's own list is 4 chats, and its child holds 20. The view is on
	// the top-level place, whose list includes the child, so tiny crosses
	// the cut. big itself does not get a view.
	if !sameSet(places, []string{"wide", "tiny"}) {
		t.Fatalf("by-month places %v, want wide and tiny", places)
	}
}

func TestAMonthAxisAddsTheViewBelowTheCut(t *testing.T) {
	members := ids("c", 3)
	tree := plant(t, []string{"small"}, node("small", 1, members, nil, nil))
	state := &placeorg.State{Axes: map[string]placeorg.Axis{
		"small": {Kind: placeorg.AxisMonth},
	}}
	ops, err := New(state).Link(context.Background(), tree, &placeorg.Corpus{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	lenses := lensesOf(ops)
	if len(lenses) != 1 || lenses[0].Place != "small" || lenses[0].Into != axisMonth {
		t.Fatalf("month axis lenses %+v", lenses)
	}
}

func TestProjectAxisListsOnlyChatsThatCarryTheValue(t *testing.T) {
	tree := plant(t, []string{"desk"}, node("desk", 1, []string{"one", "two"}, nil, nil))
	corpus := &placeorg.Corpus{
		Chats: []placeorg.Chat{
			{ID: "one", Workspace: "alpha"},
			{ID: "two", Workspace: "beta"},
		},
		Anchors: map[string]placeorg.AnchorSet{"one": {"alpha": 1}},
	}
	state := &placeorg.State{Axes: map[string]placeorg.Axis{
		"desk":    {Kind: placeorg.AxisProject, Values: []string{"alpha", "missing"}},
		"nowhere": {Kind: placeorg.AxisProject, Values: []string{"alpha"}},
		"empty":   {Kind: placeorg.AxisWorkspace},
	}}
	ops, err := New(state).Link(context.Background(), tree, corpus, nil)
	if err != nil {
		t.Fatal(err)
	}
	lenses := lensesOf(ops)
	if len(lenses) != 1 {
		t.Fatalf("lenses %+v, want the one value a chat carries", lenses)
	}
	got := lenses[0]
	if got.Place != "desk" || got.Name != "alpha" || got.Into != axisAnchor || got.Why != whyProject {
		t.Fatalf("axis lens %+v", got)
	}
	if !reflect.DeepEqual(got.Members, []string{"one"}) {
		t.Fatalf("members %v", got.Members)
	}
}

func TestADemotedGroupIsALensOnItsParentAndNeverANode(t *testing.T) {
	members := []string{"a", "b", "c", "d"}
	// Six further chats keep "parcel" under the 60% document-frequency
	// cut. Without them the word is in most of the corpus and is not a name.
	chats := []placeorg.Chat{
		{ID: "a", Text: "parcel parcel notes"},
		{ID: "b", Text: "parcel parcel notes"},
		{ID: "c", Text: "parcel parcel notes"},
		{ID: "d", Text: "parcel parcel notes"},
	}
	for i := 0; i < 6; i++ {
		chats = append(chats, placeorg.Chat{ID: "f" + string(rune('0'+i)), Text: "unrelated words here"})
	}
	corpus := &placeorg.Corpus{Chats: chats}
	tree := plant(t, []string{"parent"}, node("parent", 1, members, nil, nil))
	linker := New(nil)
	linker.Demoted = []placeorg.Op{{
		Kind:    placeorg.OpDemote,
		Place:   "gone",
		Parent:  "parent",
		Members: append([]string(nil), members...),
	}}
	before := map[string]bool{"parent": true}
	ops, err := linker.Link(context.Background(), tree, corpus, nil)
	if err != nil {
		t.Fatal(err)
	}
	lenses := lensesOf(ops)
	if len(lenses) != 1 {
		t.Fatalf("lenses %+v", lenses)
	}
	got := lenses[0]
	if got.Place != "parent" || got.Into != axisGroup || got.Name != "parcel" || got.Why != whyGroup {
		t.Fatalf("demoted lens %+v", got)
	}
	if !reflect.DeepEqual(got.Members, []string{"a", "b", "c", "d"}) {
		t.Fatalf("members %v", got.Members)
	}
	assertNoLensIsANode(t, ops, before)
	if _, ok := tree.Node("gone"); ok {
		t.Fatal("the demoted group was planted as a node")
	}
	if _, ok := tree.Node("parcel"); ok {
		t.Fatal("the view name was turned into a node")
	}
}

func TestCancelledLinkReturnsTheError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tree := plant(t, []string{"root"}, node("root", 1, nil, nil, nil))
	ops, err := New(nil).Link(ctx, tree, chatsOf("a"), nil)
	if err == nil || ops != nil {
		t.Fatalf("ops %v err %v, want the context error and no ops", ops, err)
	}
}

func TestNoSubjectListInLiterals(t *testing.T) {
	// The owner's anchor list. None of these words may be a string in this
	// package: a view is named from the chats it is given, not from a list
	// of one person's projects. Import paths are skipped; the module path
	// contains the product name and is not a subject list.
	banned := []string{
		"carnatic", "wisp", "cloudflare", "pareto", "telegram", "openrouter",
		"codex", "sonnet", "toronto",
	}
	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			s := strings.ToLower(lit.Value)
			if strings.Contains(s, "/") {
				return true
			}
			for _, word := range banned {
				if strings.Contains(s, word) {
					t.Errorf("%s: literal contains %q", path, word)
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func assertNoLensIsANode(t *testing.T, ops []placeorg.Op, nodes map[string]bool) {
	t.Helper()
	for _, op := range ops {
		switch op.Kind {
		case placeorg.OpBirth, placeorg.OpSplit, placeorg.OpInterpose, placeorg.OpCollapse:
			t.Fatalf("op %s creates or removes a node; a view must not", op.Kind)
		}
		if op.Kind != placeorg.OpLens {
			continue
		}
		if !nodes[op.Place] {
			t.Fatalf("lens %q hangs on %q, which is not a place", op.Name, op.Place)
		}
		if nodes[op.Name] && op.Name != op.Place {
			t.Fatalf("lens name %q is a node", op.Name)
		}
	}
}

func ids(prefix string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = prefix + string(rune('0'+i/10)) + string(rune('0'+i%10))
	}
	return out
}

func sameSet(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	g := append([]string(nil), got...)
	w := append([]string(nil), want...)
	sort.Strings(g)
	sort.Strings(w)
	return reflect.DeepEqual(g, w)
}
