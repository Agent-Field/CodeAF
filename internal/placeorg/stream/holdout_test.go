package stream

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"testing"

	"github.com/Agent-Field/codeaf/internal/placeorg"
)

// The saved reference holdout is the last 15% of live chats by created time:
// 55 arrivals. PLACES.md's "74 arrivals" is an earlier, larger library that
// was not saved in ref/. Under a judge that always falls back, the three
// cross-branch contests (the reference's 2 answers plus 1 "NEW", which this
// port does not offer) keep the broader place, and the other 52 match the
// reference's park.
func TestHoldoutDecisionsMatchTheReferenceUnderAFakeJudge(t *testing.T) {
	ref := os.Getenv("PLACEORG_REF")
	if ref == "" {
		t.Skip("PLACEORG_REF is unset; the owner's embeddings are not in the tree")
	}
	ids, vecs := loadEmbeddings(t, ref)
	rows := loadAssignments(t, ref)
	created := loadCreated(t)
	if len(ids) != len(rows) {
		t.Fatalf("embeddings %d assignments %d", len(ids), len(rows))
	}
	tree, cr, cm, cl := referenceTree(ids, vecs, rows)
	type item struct {
		idx     int
		created string
	}
	var live []item
	for i, id := range ids {
		if rows[id].Inbox {
			continue
		}
		live = append(live, item{i, created[id]})
	}
	// An empty created time sorts first, the same way the reference does.
	// Those chats land in the warm prefix, not in the holdout.
	// Stable, and live was appended in embedding order, which is the order
	// the reference sorts when two chats share a timestamp.
	sort.SliceStable(live, func(i, j int) bool {
		return live[i].created < live[j].created
	})
	hold := live[int(len(live)*85/100):]
	// int(n*0.85) in Python truncates the same way as integer 85/100 for
	// these sizes; 361*0.85 = 306.85, int is 306, and 361*85/100 is 306.
	s := New()
	s.Thresholds = placeorg.Thresholds{Floor: defaultFloor, Margin: defaultMargin}
	j := &fakeJudge{fallback: true}
	var rootN, midN, leafN, judgeN, inboxN int
	for _, h := range hold {
		id := ids[h.idx]
		row := rows[id]
		v := vecs[h.idx]
		level, node, ambiguous := assignedPark(v, row, cr, cm, cl)
		if ambiguous {
			judgeN++
		} else {
			switch level {
			case "root":
				rootN++
			case "mid":
				midN++
			case "leaf":
				leafN++
			default:
				inboxN++
			}
		}
		wantID, wantPark := expectedPlace(v, row, cr, cm, cl, level, node, ambiguous)
		got, err := s.File(context.Background(), tree, placeorg.Arrival{
			Chat: placeorg.Chat{ID: id}, Vector: v,
		}, j)
		if err != nil {
			t.Fatal(err)
		}
		if got.Op.Kind == placeorg.OpBirth {
			t.Fatalf("%s birthed a place", id)
		}
		if ambiguous {
			if !got.Judged || got.Op.Place != wantID || (wantPark && got.Op.Kind != placeorg.OpPark) {
				t.Fatalf("%s contest: got %s %s judged %v, want %s", id, got.Op.Kind, got.Op.Place, got.Judged, wantID)
			}
			continue
		}
		if got.Judged || got.Op.Place != wantID {
			t.Fatalf("%s: got %s %s judged %v, want %s (%s %d)", id, got.Op.Kind, got.Op.Place, got.Judged, wantID, level, node)
		}
		if level == "leaf" && got.Op.Kind != placeorg.OpFile {
			t.Fatalf("%s leaf filed as %s", id, got.Op.Kind)
		}
		if level != "leaf" && level != "inbox" && got.Op.Kind != placeorg.OpPark {
			t.Fatalf("%s %s parked as %s", id, level, got.Op.Kind)
		}
	}
	if rootN != 29 || midN != 3 || leafN != 20 || judgeN != 3 || inboxN != 0 {
		t.Fatalf("holdout %d: root %d mid %d leaf %d judge %d inbox %d", len(hold), rootN, midN, leafN, judgeN, inboxN)
	}
	if len(hold) == 0 || float64(j.calls)/float64(len(hold)) > 0.10 {
		t.Fatalf("judge rate %d/%d", j.calls, len(hold))
	}
	if j.calls != judgeN {
		t.Fatalf("calls %d judge rows %d", j.calls, judgeN)
	}
}

func expectedPlace(v []float32, row assignRow, cr, cm, cl map[int][]float32, level string, node int, ambiguous bool) (string, bool) {
	if !ambiguous {
		return levelID(level, node), level != "leaf"
	}
	top := rankNodes(v, cr, cm, cl)
	a, b := top[0], top[1]
	keep := a
	if b.depth < a.depth || (b.depth == a.depth && b.score > a.score) {
		keep = b
	}
	return keep.id, keep.depth <= 1 || keep.depth < 3
}

type ranked struct {
	id    string
	depth int
	score float32
	root  int
}

func rankNodes(v []float32, cr, cm, cl map[int][]float32) []ranked {
	var all []ranked
	for id, c := range cr {
		all = append(all, ranked{id: levelID("root", id), depth: 1, score: dot(unit(c), v), root: id})
	}
	for id, c := range cm {
		all = append(all, ranked{id: levelID("mid", id), depth: 2, score: dot(unit(c), v), root: -1})
	}
	for id, c := range cl {
		all = append(all, ranked{id: levelID("leaf", id), depth: 3, score: dot(unit(c), v), root: -2})
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].score != all[j].score {
			return all[i].score > all[j].score
		}
		return all[i].id < all[j].id
	})
	return all
}

func assignedPark(v []float32, row assignRow, cr, cm, cl map[int][]float32) (string, int, bool) {
	sR, okR := lookup(cr, row.Root, v)
	sM, okM := lookup(cm, row.mid(), v)
	sL, okL := lookup(cl, row.leaf(), v)
	best := sR
	if sM > best {
		best = sM
	}
	if sL > best {
		best = sL
	}
	level, node := "inbox", -1
	if best >= defaultFloor {
		switch {
		case okL && sL > sM+defaultMargin && sL > sR+defaultMargin:
			level, node = "leaf", row.leaf()
		case okM && sM > sR+defaultMargin:
			level, node = "mid", row.mid()
		default:
			level, node = "root", row.Root
		}
	}
	top := rankNodes(v, cr, cm, cl)
	ambiguous := false
	if level != "inbox" && len(top) >= 2 {
		// Different branches: a root id is not a mid or leaf id. The test
		// rebuilds root ownership from the row when comparing the top two,
		// using the maps closed over by rootOf in the caller. Here the
		// ranked nodes carry only depth; branch identity is checked below
		// by rootOfRank.
		ambiguous = top[0].score-top[1].score < defaultMargin && top[0].score >= defaultFloor && differentBranch(top[0], top[1], row, cr, cm, cl)
	}
	_ = okR
	return level, node, ambiguous
}

func differentBranch(a, b ranked, row assignRow, cr, cm, cl map[int][]float32) bool {
	// Recompute roots from the node id prefix and the assignment tables
	// stored on the centroids' keys is not enough. The caller passes the
	// full rows through rootIndex, set before the loop. See rootOf.
	return rootOf(a) != rootOf(b)
}

// rootsOfMid and rootsOfLeaf are filled while the tree is built.
var rootsOfMid = map[int]int{}
var rootsOfLeaf = map[int]int{}

func rootOf(r ranked) int {
	switch {
	case len(r.id) > 0 && r.id[0] == 'r':
		n, _ := strconv.Atoi(r.id[1:])
		return n
	case len(r.id) > 0 && r.id[0] == 'm':
		n, _ := strconv.Atoi(r.id[1:])
		return rootsOfMid[n]
	default:
		n, _ := strconv.Atoi(r.id[1:])
		return rootsOfLeaf[n]
	}
}

func lookup(table map[int][]float32, id int, v []float32) (float32, bool) {
	c, ok := table[id]
	if !ok || id < 0 {
		return -2, false
	}
	return dot(unit(c), v), true
}

func majority(votes map[int]int) int {
	best, n := 0, -1
	for id, c := range votes {
		if c > n || (c == n && id < best) {
			best, n = id, c
		}
	}
	return best
}

func levelID(level string, n int) string {
	switch level {
	case "root":
		return fmt.Sprintf("r%d", n)
	case "mid":
		return fmt.Sprintf("m%d", n)
	case "leaf":
		return fmt.Sprintf("l%d", n)
	default:
		return ""
	}
}

type assignRow struct {
	Root      int     `json:"root"`
	Mid       *int    `json:"mid"`
	Leaf      *int    `json:"leaf"`
	RootName  string  `json:"root_name"`
	MidName   *string `json:"mid_name"`
	LeafName  *string `json:"leaf_name"`
	ParkLevel string  `json:"park_level"`
	ParkNode  int     `json:"park_node"`
	Inbox     bool    `json:"inbox"`
	Live      bool    `json:"live"`
}

func (r assignRow) mid() int {
	if r.Mid == nil {
		return -1
	}
	return *r.Mid
}

func (r assignRow) leaf() int {
	if r.Leaf == nil {
		return -1
	}
	return *r.Leaf
}

func referenceTree(ids []string, vecs [][]float32, rows map[string]assignRow) (*placeorg.Tree, map[int][]float32, map[int][]float32, map[int][]float32) {
	rootsOfMid = map[int]int{}
	rootsOfLeaf = map[int]int{}
	n := len(ids)
	root := make([]int, n)
	mid := make([]int, n)
	leaf := make([]int, n)
	parkLevel := make([]string, n)
	parkNode := make([]int, n)
	inbox := make([]bool, n)
	midVotes := map[int]map[int]int{}
	leafVotes := map[int]map[int]int{}
	for i, id := range ids {
		row := rows[id]
		root[i] = row.Root
		mid[i] = row.mid()
		leaf[i] = row.leaf()
		parkLevel[i] = row.ParkLevel
		parkNode[i] = row.ParkNode
		inbox[i] = row.Inbox
		if row.mid() >= 0 {
			if midVotes[row.mid()] == nil {
				midVotes[row.mid()] = map[int]int{}
			}
			midVotes[row.mid()][row.Root]++
		}
		if row.leaf() >= 0 && row.mid() >= 0 {
			if leafVotes[row.leaf()] == nil {
				leafVotes[row.leaf()] = map[int]int{}
			}
			leafVotes[row.leaf()][row.mid()]++
		}
	}
	for m, votes := range midVotes {
		rootsOfMid[m] = majority(votes)
	}
	for l, votes := range leafVotes {
		rootsOfLeaf[l] = rootsOfMid[majority(votes)]
	}
	cr := centroidLevel(vecs, root, parkLevel, parkNode, inbox, "root")
	cm := centroidLevel(vecs, mid, parkLevel, parkNode, inbox, "mid")
	cl := centroidLevel(vecs, leaf, parkLevel, parkNode, inbox, "leaf")
	midsOf := map[int]map[int]bool{}
	leavesOf := map[int]map[int]bool{}
	names := map[string]string{}
	for _, id := range ids {
		row := rows[id]
		names[levelID("root", row.Root)] = lastSegment(row.RootName)
		if row.mid() >= 0 && cm[row.mid()] != nil && cr[row.Root] != nil {
			midsOf[row.Root] = midsOf[row.Root]
			if midsOf[row.Root] == nil {
				midsOf[row.Root] = map[int]bool{}
			}
			midsOf[row.Root][row.mid()] = true
			if row.MidName != nil {
				names[levelID("mid", row.mid())] = lastSegment(*row.MidName)
			}
		}
		if row.leaf() >= 0 && row.mid() >= 0 && cl[row.leaf()] != nil {
			if leavesOf[row.mid()] == nil {
				leavesOf[row.mid()] = map[int]bool{}
			}
			leavesOf[row.mid()][row.leaf()] = true
			if row.LeafName != nil {
				names[levelID("leaf", row.leaf())] = lastSegment(*row.LeafName)
			}
		}
	}
	var nodes []placeorg.Node
	var roots []string
	for id, c := range cr {
		rid := levelID("root", id)
		var children []string
		for m := range midsOf[id] {
			children = append(children, levelID("mid", m))
		}
		sort.Strings(children)
		nodes = append(nodes, placeorg.Node{
			ID: rid, Name: names[rid], Depth: 1, Children: children,
			Centroids: map[placeorg.SpaceID]placeorg.Centroid{placeorg.SpacePrimary: {Sum: c, Count: 1}},
		})
		roots = append(roots, rid)
	}
	sort.Strings(roots)
	for id, c := range cm {
		midID := levelID("mid", id)
		var children []string
		for l := range leavesOf[id] {
			children = append(children, levelID("leaf", l))
		}
		sort.Strings(children)
		nodes = append(nodes, placeorg.Node{
			ID: midID, Name: names[midID], Parent: levelID("root", rootsOfMid[id]), Depth: 2, Children: children,
			Centroids: map[placeorg.SpaceID]placeorg.Centroid{placeorg.SpacePrimary: {Sum: c, Count: 1}},
		})
	}
	for id, c := range cl {
		leafID := levelID("leaf", id)
		parent := ""
		for m, leaves := range leavesOf {
			if leaves[id] {
				parent = levelID("mid", m)
				break
			}
		}
		nodes = append(nodes, placeorg.Node{
			ID: leafID, Name: names[leafID], Parent: parent, Depth: 3,
			Centroids: map[placeorg.SpaceID]placeorg.Centroid{placeorg.SpacePrimary: {Sum: c, Count: 1}},
		})
	}
	return putTree(nodes, roots), cr, cm, cl
}

func centroidLevel(vecs [][]float32, labels []int, parkLevel []string, parkNode []int, inbox []bool, level string) map[int][]float32 {
	// Members are the live chats carrying this label, plus any live chat
	// the reference parked on this node. A parked chat is a citizen of the
	// place it stopped at even when a finer label still names it.
	groups := map[int]map[int]bool{}
	add := func(id, i int) {
		if id < 0 || inbox[i] {
			return
		}
		if groups[id] == nil {
			groups[id] = map[int]bool{}
		}
		groups[id][i] = true
	}
	for i, lab := range labels {
		add(lab, i)
	}
	for i := range parkLevel {
		if parkLevel[i] == level {
			add(parkNode[i], i)
		}
	}
	out := map[int][]float32{}
	for id, set := range groups {
		idxs := make([]int, 0, len(set))
		for i := range set {
			idxs = append(idxs, i)
		}
		out[id] = meanUnit(vecs, idxs)
	}
	return out
}

func meanUnit(vecs [][]float32, idxs []int) []float32 {
	if len(idxs) == 0 {
		return nil
	}
	dim := len(vecs[idxs[0]])
	sum := make([]float64, dim)
	for _, i := range idxs {
		for d, x := range vecs[i] {
			sum[d] += float64(x)
		}
	}
	var n float64
	for _, x := range sum {
		n += x * x
	}
	if n == 0 {
		return nil
	}
	inv := 1 / math.Sqrt(n)
	out := make([]float32, dim)
	for i, x := range sum {
		out[i] = float32(x * inv)
	}
	return out
}

func lastSegment(name string) string {
	for i := len(name) - 1; i >= 0; i-- {
		if name[i] == '/' {
			return name[i+1:]
		}
	}
	return name
}

func loadEmbeddings(t *testing.T, ref string) ([]string, [][]float32) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(ref, "emb_row_ids.json"))
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	if err := json.Unmarshal(raw, &ids); err != nil {
		t.Fatal(err)
	}
	vecs, err := loadNpy(filepath.Join(ref, "emb_all.npy"))
	if err != nil {
		t.Fatal(err)
	}
	if len(vecs) != len(ids) {
		t.Fatalf("rows %d ids %d", len(vecs), len(ids))
	}
	return ids, vecs
}

func loadAssignments(t *testing.T, ref string) map[string]assignRow {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(ref, "assignments.json"))
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Assignments map[string]assignRow `json:"assignments"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatal(err)
	}
	return file.Assignments
}

func loadCreated(t *testing.T) map[string]string {
	t.Helper()
	dir := os.Getenv("PLACEORG_CORPUS")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, "places-lab")
	}
	out := map[string]string{}
	found := false
	for _, name := range []string{"corpus.jsonl", "corpus-cc.jsonl"} {
		f, err := os.Open(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		found = true
		dec := json.NewDecoder(f)
		for dec.More() {
			var row struct {
				ID      string `json:"id"`
				Created string `json:"created"`
			}
			if err := dec.Decode(&row); err != nil {
				f.Close()
				t.Fatal(err)
			}
			out[row.ID] = row.Created
		}
		f.Close()
	}
	if !found {
		t.Skip("corpus jsonl is not next to PLACEORG_REF; created times order the holdout")
	}
	return out
}

var (
	descrRE = regexp.MustCompile(`'descr':\s*'([^']+)'`)
	shapeRE = regexp.MustCompile(`'shape':\s*\((\d+),\s*(\d+)\)`)
)

func loadNpy(path string) ([][]float32, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(b) < 10 || string(b[:6]) != "\x93NUMPY" {
		return nil, fmt.Errorf("not an npy file")
	}
	major := b[6]
	var headerLen, off int
	if major == 1 {
		headerLen = int(binary.LittleEndian.Uint16(b[8:10]))
		off = 10 + headerLen
	} else {
		headerLen = int(binary.LittleEndian.Uint32(b[8:12]))
		off = 12 + headerLen
	}
	header := string(b[off-headerLen : off])
	if regexp.MustCompile(`'fortran_order':\s*True`).MatchString(header) {
		return nil, fmt.Errorf("fortran order")
	}
	dm := descrRE.FindStringSubmatch(header)
	sm := shapeRE.FindStringSubmatch(header)
	if dm == nil || sm == nil || dm[1] != "<f4" {
		return nil, fmt.Errorf("unexpected npy header %s", header)
	}
	rows, _ := strconv.Atoi(sm[1])
	cols, _ := strconv.Atoi(sm[2])
	need := rows * cols * 4
	if off+need > len(b) {
		return nil, fmt.Errorf("npy truncated")
	}
	out := make([][]float32, rows)
	for r := 0; r < rows; r++ {
		row := make([]float32, cols)
		for c := 0; c < cols; c++ {
			bits := binary.LittleEndian.Uint32(b[off+(r*cols+c)*4:])
			row[c] = math.Float32frombits(bits)
		}
		out[r] = row
	}
	return out, nil
}
