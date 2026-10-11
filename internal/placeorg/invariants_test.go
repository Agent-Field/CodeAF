package placeorg

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestCheckPassesAFurnishedTree(t *testing.T) {
	tree, plan, corpus := furnished()
	results, ok := Check(tree, plan, corpus)
	if !ok || len(results) != len(Invariants) {
		t.Fatalf("ok=%v %+v", ok, results)
	}
	for _, r := range results {
		if !r.OK {
			t.Errorf("%s: %s", r.Name, r.Why)
		}
	}
	plan.Mode = ModeAddOn
	if _, ok := Check(tree, plan, corpus); !ok {
		t.Fatal("add-on with only filing should pass")
	}
}

func TestCheckFailsEachBrokenLaw(t *testing.T) {
	cases := []struct {
		law   string
		build func() (*Tree, *Plan, *Corpus)
	}{
		{"depth within 6", brokenDepth},
		{"fan-out within 9", brokenFanout},
		{"every place has at least 4 chats", brokenQuorum},
		{"names unique tree-wide, one or two words", brokenName},
		{"every extra home is earned, and in a branch of its own", brokenLinks},
		{"forest: one parent per place", brokenForest},
		{"every op in the plan is in the journal and replays to this tree", brokenJournal},
		{"a home for every chat, duplicates beside their representative", brokenHome},
		{"no probe is a place or counts toward one", brokenProbe},
		{"the person's own places are intact in add-on mode", brokenAddOn},
		{"no model text became an id, a path or a permission", brokenID},
	}
	if len(cases) != len(Invariants) {
		t.Fatalf("fixtures %d, laws %d", len(cases), len(Invariants))
	}
	for _, tc := range cases {
		t.Run(tc.law, func(t *testing.T) {
			tree, plan, corpus := tc.build()
			results, ok := Check(tree, plan, corpus)
			if ok {
				t.Fatal("check passed")
			}
			for _, r := range results {
				if r.Name == tc.law {
					if r.OK {
						t.Fatalf("the law passed: %+v", results)
					}
					return
				}
			}
			t.Fatalf("law %q missing from %+v", tc.law, results)
		})
	}
}

func TestCheckPassesTheReferenceTree(t *testing.T) {
	dir := os.Getenv("PLACEORG_REF")
	if dir == "" {
		t.Skip("PLACEORG_REF is unset")
	}
	tree, plan, corpus := referenceTree(t, dir)
	results, ok := Check(tree, plan, corpus)
	if len(results) != len(Invariants) {
		t.Fatalf("results %d", len(results))
	}
	if ok {
		return
	}
	for _, r := range results {
		if !r.OK {
			t.Errorf("%s: %s", r.Name, r.Why)
		}
	}
}

func furnished() (*Tree, *Plan, *Corpus) {
	tree := NewTree()
	tree.nodes["shelf"] = &Node{ID: "shelf", Name: "Shelf", Depth: 1, Members: []string{"a", "b", "c", "d", "dup"}}
	tree.nodes["extra"] = &Node{ID: "extra", Name: "Extra", Depth: 1, Members: []string{"e0", "e1", "e2", "e3"}}
	tree.nodes["spare"] = &Node{ID: "spare", Name: "Spare", Depth: 1, Members: []string{"s0", "s1", "s2", "s3"}}
	tree.roots = []string{"shelf", "extra", "spare"}
	homes := map[string]string{
		"a": "shelf", "b": "shelf", "c": "shelf", "d": "shelf", "dup": "shelf",
		"e0": "extra", "e1": "extra", "e2": "extra", "e3": "extra",
		"s0": "spare", "s1": "spare", "s2": "spare", "s3": "spare",
	}
	for id, place := range homes {
		tree.home[id] = place
	}
	tree.links["a"] = []string{"extra", "spare"}
	corpus := &Corpus{
		Kinds:          map[string]Kind{"dup": KindDuplicate, "probe": KindProbe},
		Representative: map[string]string{"dup": "a"},
	}
	var ops []Op
	for id, place := range homes {
		corpus.Chats = append(corpus.Chats, Chat{ID: id})
		ops = append(ops, Op{Kind: OpPark, Stage: "park", Place: place, Chat: id, Proposed: false, Why: "filed"})
	}
	corpus.Chats = append(corpus.Chats, Chat{ID: "probe"})
	sort.Slice(ops, func(i, j int) bool { return ops[i].Chat < ops[j].Chat })
	return tree, &Plan{Mode: ModeRefresh, Ops: ops}, corpus
}

func onePlace(name string, members []string) (*Tree, *Plan, *Corpus) {
	tree := NewTree()
	id := "p"
	tree.nodes[id] = &Node{ID: id, Name: name, Depth: 1, Members: append([]string(nil), members...)}
	tree.roots = []string{id}
	corpus := &Corpus{Kinds: map[string]Kind{}}
	var ops []Op
	for _, m := range members {
		tree.home[m] = id
		corpus.Chats = append(corpus.Chats, Chat{ID: m})
		ops = append(ops, Op{Kind: OpPark, Stage: "park", Place: id, Chat: m, Proposed: false, Why: "filed"})
	}
	return tree, &Plan{Mode: ModeRefresh, Ops: ops}, corpus
}

func brokenDepth() (*Tree, *Plan, *Corpus) {
	tree := NewTree()
	var parent string
	var id string
	for i := 1; i <= 7; i++ {
		id = fmt.Sprintf("n%d", i)
		n := &Node{ID: id, Name: fmt.Sprintf("D%d", i), Parent: parent, Depth: i}
		if parent != "" {
			tree.nodes[parent].Children = []string{id}
		}
		tree.nodes[id] = n
		parent = id
	}
	tree.roots = []string{"n1"}
	members := []string{"c0", "c1", "c2", "c3"}
	tree.nodes["n7"].Members = members
	corpus := &Corpus{}
	var ops []Op
	for _, m := range members {
		tree.home[m] = "n7"
		corpus.Chats = append(corpus.Chats, Chat{ID: m})
		ops = append(ops, Op{Kind: OpPark, Stage: "park", Place: "n7", Chat: m, Proposed: false, Why: "filed"})
	}
	return tree, &Plan{Mode: ModeRefresh, Ops: ops}, corpus
}

func brokenFanout() (*Tree, *Plan, *Corpus) {
	tree := NewTree()
	corpus := &Corpus{}
	var ops []Op
	for i := 0; i < 10; i++ {
		id := fmt.Sprintf("p%d", i)
		var members []string
		for c := 0; c < 4; c++ {
			m := fmt.Sprintf("p%dc%d", i, c)
			members = append(members, m)
			tree.home[m] = id
			corpus.Chats = append(corpus.Chats, Chat{ID: m})
			ops = append(ops, Op{Kind: OpPark, Stage: "park", Place: id, Chat: m, Proposed: false, Why: "filed"})
		}
		tree.nodes[id] = &Node{ID: id, Name: fmt.Sprintf("P%d", i), Depth: 1, Members: members}
		tree.roots = append(tree.roots, id)
	}
	return tree, &Plan{Mode: ModeRefresh, Ops: ops}, corpus
}

func brokenQuorum() (*Tree, *Plan, *Corpus) {
	return onePlace("Only", []string{"a", "b", "c"})
}

func brokenName() (*Tree, *Plan, *Corpus) {
	return onePlace("One Two Three", []string{"a", "b", "c", "d"})
}

// brokenLinks lists a chat in its own home, which no score can earn.
func brokenLinks() (*Tree, *Plan, *Corpus) {
	tree, plan, corpus := onePlace("Only", []string{"a", "b", "c", "d"})
	tree.links["a"] = []string{tree.home["a"]}
	return tree, plan, corpus
}

func brokenForest() (*Tree, *Plan, *Corpus) {
	tree := NewTree()
	tree.nodes["a"] = &Node{ID: "a", Name: "Alpha", Depth: 1, Children: []string{"c"}, Members: []string{"a0", "a1", "a2", "a3"}}
	tree.nodes["b"] = &Node{ID: "b", Name: "Beta", Depth: 1, Children: []string{"c"}, Members: []string{"b0", "b1", "b2", "b3"}}
	tree.nodes["c"] = &Node{ID: "c", Name: "Child", Parent: "a", Depth: 2, Members: []string{"c0", "c1", "c2", "c3"}}
	tree.roots = []string{"a", "b"}
	corpus := &Corpus{}
	var ops []Op
	homes := map[string]string{}
	for _, id := range []string{"a0", "a1", "a2", "a3"} {
		homes[id] = "a"
	}
	for _, id := range []string{"b0", "b1", "b2", "b3"} {
		homes[id] = "b"
	}
	for _, id := range []string{"c0", "c1", "c2", "c3"} {
		homes[id] = "c"
	}
	for id, place := range homes {
		tree.home[id] = place
		corpus.Chats = append(corpus.Chats, Chat{ID: id})
		ops = append(ops, Op{Kind: OpPark, Stage: "park", Place: place, Chat: id, Proposed: false, Why: "filed"})
	}
	return tree, &Plan{Mode: ModeRefresh, Ops: ops}, corpus
}

func brokenJournal() (*Tree, *Plan, *Corpus) {
	tree, plan, corpus := onePlace("Only", []string{"a", "b", "c", "d"})
	plan.Ops[0].Stage = ""
	return tree, plan, corpus
}

func brokenHome() (*Tree, *Plan, *Corpus) {
	tree, plan, corpus := onePlace("Only", []string{"a", "b", "c", "d"})
	corpus.Chats = append(corpus.Chats, Chat{ID: "loose"})
	return tree, plan, corpus
}

func brokenProbe() (*Tree, *Plan, *Corpus) {
	tree, plan, corpus := onePlace("Only", []string{"a", "b", "c", "d", "probe"})
	corpus.Kinds = map[string]Kind{"probe": KindProbe}
	delete(tree.home, "probe")
	plan.Ops = plan.Ops[:len(plan.Ops)-1]
	return tree, plan, corpus
}

func brokenAddOn() (*Tree, *Plan, *Corpus) {
	tree, plan, corpus := onePlace("Only", []string{"a", "b", "c", "d"})
	plan.Mode = ModeAddOn
	plan.Ops = append(plan.Ops, Op{
		Kind: OpDissolve, Stage: "audit", Place: "old", Proposed: false, Why: "removed",
	})
	return tree, plan, corpus
}

func brokenID() (*Tree, *Plan, *Corpus) {
	tree, plan, corpus := onePlace("Only", []string{"a", "b", "c", "d"})
	n := tree.nodes["p"]
	delete(tree.nodes, "p")
	n.ID = "bad id"
	tree.nodes[n.ID] = n
	tree.roots = []string{n.ID}
	for id := range tree.home {
		tree.home[id] = n.ID
	}
	for i := range plan.Ops {
		plan.Ops[i].Place = n.ID
	}
	return tree, plan, corpus
}

func referenceTree(t *testing.T, dir string) (*Tree, *Plan, *Corpus) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "assignments.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		ProbeIDs    []string                 `json:"probe_ids"`
		Assignments map[string]refAssignment `json:"assignments"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	tree := NewTree()
	type slot struct {
		kind string
		id   int
	}
	ensure := func(kind string, id, depth int, parent, name string) *Node {
		nid := fmt.Sprintf("%s%d", kind, id)
		n, ok := tree.nodes[nid]
		if ok {
			if n.Name == "" && name != "" {
				n.Name = name
			}
			return n
		}
		n = &Node{ID: nid, Parent: parent, Name: name, Depth: depth}
		tree.nodes[nid] = n
		return n
	}
	seenRoot := map[string]bool{}
	corpus := &Corpus{Kinds: map[string]Kind{}}
	var ops []Op
	var placeIDs []string
	for id, row := range doc.Assignments {
		corpus.Chats = append(corpus.Chats, Chat{ID: id})
		corpus.Kinds[id] = KindConversation
		if row.Inbox {
			tree.inbox[id] = true
			ops = append(ops, Op{Kind: OpInbox, Stage: "park", Chat: id, Proposed: false, Why: "now"})
			continue
		}
		rname := lastSegment(row.RootName)
		mname := lastSegment(row.MidName)
		lname := lastSegment(row.LeafName)
		root := ensure("r", row.Root, 1, "", rname)
		if !seenRoot[root.ID] {
			seenRoot[root.ID] = true
			tree.roots = append(tree.roots, root.ID)
		}
		home := root
		if row.Mid >= 0 {
			mid := ensure("m", row.Mid, 2, root.ID, mname)
			if !containsStr(root.Children, mid.ID) {
				root.Children = append(root.Children, mid.ID)
			}
			home = mid
			if row.Leaf >= 0 {
				leaf := ensure("l", row.Leaf, 3, mid.ID, lname)
				if !containsStr(mid.Children, leaf.ID) {
					mid.Children = append(mid.Children, leaf.ID)
				}
				leaf.Members = append(leaf.Members, id)
				home = leaf
			} else {
				mid.Members = append(mid.Members, id)
			}
		} else {
			root.Members = append(root.Members, id)
		}
		switch row.ParkLevel {
		case "leaf":
			home = tree.nodes[fmt.Sprintf("l%d", row.ParkNode)]
		case "mid":
			home = tree.nodes[fmt.Sprintf("m%d", row.ParkNode)]
		case "root":
			home = tree.nodes[fmt.Sprintf("r%d", row.ParkNode)]
		}
		if home == nil {
			t.Fatalf("%s parks at %s %d, which is not a place", id, row.ParkLevel, row.ParkNode)
		}
		tree.home[id] = home.ID
		ops = append(ops, Op{Kind: OpPark, Stage: "park", Place: home.ID, Chat: id, Proposed: false, Why: "filed"})
	}
	for id := range tree.nodes {
		placeIDs = append(placeIDs, id)
	}
	sort.Strings(placeIDs)
	for id, row := range doc.Assignments {
		if row.Inbox || row.Shortcuts == 0 {
			continue
		}
		n := row.Shortcuts
		var links []string
		for _, pid := range placeIDs {
			if pid == tree.home[id] {
				continue
			}
			links = append(links, pid)
			if len(links) == n {
				break
			}
		}
		tree.links[id] = links
	}
	for _, id := range doc.ProbeIDs {
		corpus.Chats = append(corpus.Chats, Chat{ID: id})
		corpus.Kinds[id] = KindProbe
	}
	return tree, &Plan{Mode: ModeRefresh, Ops: ops}, corpus
}

type refAssignment struct {
	Root      int    `json:"root"`
	Mid       int    `json:"mid"`
	Leaf      int    `json:"leaf"`
	RootName  string `json:"root_name"`
	MidName   string `json:"mid_name"`
	LeafName  string `json:"leaf_name"`
	ParkLevel string `json:"park_level"`
	ParkNode  int    `json:"park_node"`
	Inbox     bool   `json:"inbox"`
	Shortcuts int    `json:"shortcuts"`
}

func lastSegment(name string) string {
	if i := strings.LastIndex(name, "/"); i >= 0 {
		return name[i+1:]
	}
	return name
}

func containsStr(ids []string, id string) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

// TestHomesInvariantFailsOnWhatAnEarnedHomeCannotBe pins the three ways the
// law can fail, and that many earned homes in distinct branches pass: there
// is no cap to trip.
func TestHomesInvariantFailsOnWhatAnEarnedHomeCannotBe(t *testing.T) {
	forest := func() (*Tree, *Plan, *Corpus) {
		tree := NewTree()
		tree.nodes["p"] = &Node{ID: "p", Name: "Parent", Depth: 1, Children: []string{"l1", "l2"}}
		tree.nodes["l1"] = &Node{ID: "l1", Name: "Leafone", Parent: "p", Depth: 2, Members: []string{"c"}}
		tree.nodes["l2"] = &Node{ID: "l2", Name: "Leaftwo", Parent: "p", Depth: 2}
		for _, id := range []string{"x", "y", "z", "w"} {
			tree.nodes[id] = &Node{ID: id, Name: "Top" + id, Depth: 1}
			tree.roots = append(tree.roots, id)
		}
		tree.roots = append(tree.roots, "p")
		tree.home["c"] = "l1"
		return tree, &Plan{}, &Corpus{}
	}
	earned := Op{Kind: OpLink, Chat: "c", Scores: []float32{0.6, 0.5}}
	cases := map[string]func(*Tree, *Plan){
		"four homes in distinct branches": func(tr *Tree, p *Plan) {
			tr.links["c"] = []string{"x", "y", "z", "w"}
		},
		"sibling of home is the same branch": func(tr *Tree, p *Plan) { tr.links["c"] = []string{"l2"} },
		"ancestor of home":                   func(tr *Tree, p *Plan) { tr.links["c"] = []string{"p"} },
		"score under its bar": func(tr *Tree, p *Plan) {
			p.Ops = []Op{{Kind: OpLink, Chat: "c", Place: "x", Scores: []float32{0.4, 0.5}}}
		},
		"no score at all": func(tr *Tree, p *Plan) { p.Ops = []Op{{Kind: OpLink, Chat: "c", Place: "x"}} },
	}
	for name, mutate := range cases {
		tree, plan, corpus := forest()
		mutate(tree, plan)
		err := checkLinks(tree, plan, corpus)
		if name == "four homes in distinct branches" {
			if err != nil {
				t.Errorf("%s: %v", name, err)
			}
			continue
		}
		if err == nil {
			t.Errorf("%s passed", name)
		}
	}
	tree, plan, corpus := forest()
	tree.links["c"] = []string{"x"}
	earned.Place = "x"
	plan.Ops = []Op{earned}
	if err := checkLinks(tree, plan, corpus); err != nil {
		t.Errorf("an earned home failed: %v", err)
	}
}
