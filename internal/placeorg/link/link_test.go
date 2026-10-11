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
		for _, child := range n.Children {
			for _, c := range nodes {
				if c.ID == child {
					c.Parent = n.ID
				}
			}
		}
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
	// Every place is its own top-level branch: siblings under one parent
	// collapse into one home, and this fixture is about the margin and the
	// depth, not the branch. Depth stays as the fixture says, so only the
	// depth-3 places are scored and the depth-1 place is not.
	for _, p := range fix.Places {
		nodes = append(nodes, node(p.ID, p.Depth, nil, nil, p.Centroid))
		roots = append(roots, p.ID)
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

// TestSiblingLeavesCollapseIntoOneHome: three tied leaves under one parent
// are one branch, so the chat earns no extra home there. Uncapped, four
// distinct branches that all clear the bar are four extra homes.
func TestSiblingLeavesCollapseIntoOneHomeAndDistinctBranchesAreUncapped(t *testing.T) {
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
	if got := linksOf(ops)["chat"]; len(got) != 0 {
		t.Fatalf("siblings in one branch earned %v, want none", got)
	}

	var nodes []*placeorg.Node
	var roots []string
	for _, id := range []string{"a", "b", "c", "d", "e"} {
		roots = append(roots, id)
		nodes = append(nodes, node(id, 1, nil, nil, same))
	}
	linker := New(nil)
	ops, err = linker.Link(context.Background(), plant(t, roots, nodes...), chatsOf("chat"), vectorsOf(same))
	if err != nil {
		t.Fatal(err)
	}
	if got := linksOf(ops)["chat"]; len(got) != 4 {
		t.Fatalf("five tied branches earned %v, want four extra homes", got)
	}
	if got := linker.Homes(); !reflect.DeepEqual(got, map[int]int{4: 1}) {
		t.Fatalf("homes histogram %v, want 4 homes for one chat", got)
	}
	for _, op := range ops {
		if op.Kind == placeorg.OpLink && (len(op.Scores) != 2 || op.Scores[0] < op.Scores[1]) {
			t.Fatalf("link %v does not carry a score that clears its bar", op)
		}
	}
}

// TestADriftedChatEarnsTheHomeItEndedIn: the start vector fits only "a", the
// end vector fits only "b"; both are earned, and "a" is the fit.
func TestADriftedChatEarnsTheHomeItEndedIn(t *testing.T) {
	tree := plant(t, []string{"a", "b"},
		node("a", 1, nil, nil, []float32{1, 0, 0, 0}),
		node("b", 1, nil, nil, []float32{0, 1, 0, 0}),
	)
	linker := New(nil)
	ops, err := linker.Link(context.Background(), tree, chatsOf("chat"), vectorsOf([]float32{1, 0, 0, 0}))
	if err != nil {
		t.Fatal(err)
	}
	if got := linksOf(ops)["chat"]; len(got) != 0 {
		t.Fatalf("without an end anchor got %v, want none", got)
	}
	linker.SetEnd(vectorsOf([]float32{0, 1, 0, 0}))
	ops, err = linker.Link(context.Background(), tree, chatsOf("chat"), vectorsOf([]float32{1, 0, 0, 0}))
	if err != nil {
		t.Fatal(err)
	}
	if got := linksOf(ops)["chat"]; !reflect.DeepEqual(got, []string{"b"}) {
		t.Fatalf("drift earned %v, want b", got)
	}
}

// TestNoHomeInAnAncestorOfWhereTheChatLives: a chat living in a leaf is not
// also listed in that leaf's parent, even when the parent is scored.
func TestNoHomeInAnAncestorOfWhereTheChatLives(t *testing.T) {
	same := []float32{1, 0, 0, 0}
	tree := plant(t, []string{"top", "other"},
		node("top", 1, nil, []string{"leaf"}, same),
		node("leaf", 2, []string{"chat"}, nil, same),
		node("other", 1, nil, nil, same),
	)
	ops, err := New(nil).Link(context.Background(), tree, chatsOf("chat"), vectorsOf(same))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range linksOf(ops)["chat"] {
		if p == "top" || p == "leaf" {
			t.Fatalf("chat linked into %s, which holds or is its home", p)
		}
	}
}

func TestAProbeIsNotAlsoInAPlace(t *testing.T) {
	corpus := chatsOf("probe", "real")
	corpus.Kinds = map[string]placeorg.Kind{"probe": placeorg.KindProbe}
	tree := plant(t, []string{"alpha", "beta"},
		node("alpha", 1, nil, nil, []float32{1, 0, 0, 0}),
		node("beta", 1, nil, nil, []float32{1, 0, 0, 0}),
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
