package link

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"testing"

	"github.com/Agent-Field/codeaf/internal/placeorg"
)

// TestShortcutHistogramAndByMonthMatchTheReference scores the saved
// embeddings the way the reference run did. PLACEORG_REF points at the
// owner's ref directory (assignments.json, emb_all.npy, emb_row_ids.json).
// Without it the test skips: that corpus is not in this repository.
func TestShortcutHistogramAndByMonthMatchTheReference(t *testing.T) {
	dir := os.Getenv("PLACEORG_REF")
	if dir == "" {
		t.Skip("PLACEORG_REF is unset; the saved embeddings are not in the tree")
	}
	ids := readIDs(t, filepath.Join(dir, "emb_row_ids.json"))
	vectors := readNPY(t, filepath.Join(dir, "emb_all.npy"))
	if len(vectors) != len(ids) {
		t.Fatalf("embeddings %d, ids %d", len(vectors), len(ids))
	}
	assigned := readAssignments(t, filepath.Join(dir, "assignments.json"))

	corpus := &placeorg.Corpus{Chats: make([]placeorg.Chat, len(ids))}
	for i, id := range ids {
		corpus.Chats[i] = placeorg.Chat{ID: id}
	}
	tree, roots := referenceTree(t, ids, assigned, vectors)
	ops, err := New(nil).Link(context.Background(), tree, corpus, vectors)
	if err != nil {
		t.Fatal(err)
	}

	hist := map[int]int{}
	per := map[string]int{}
	nodes := map[string]bool{}
	for _, id := range roots {
		nodes[id] = true
	}
	for _, n := range forest(tree) {
		nodes[n.ID] = true
	}
	for _, op := range ops {
		switch op.Kind {
		case placeorg.OpBirth, placeorg.OpSplit, placeorg.OpInterpose, placeorg.OpCollapse, placeorg.OpDemote:
			t.Fatalf("op %s would change the tree; a lens is not a node", op.Kind)
		}
		if op.Kind == placeorg.OpLens {
			if !nodes[op.Place] {
				t.Fatalf("lens %q is on %q, which is not a place", op.Name, op.Place)
			}
			continue
		}
		if op.Kind == placeorg.OpLink {
			per[op.Chat]++
			if per[op.Chat] > placeorg.MaxLinks {
				t.Fatalf("chat %s has more than %d shortcuts", op.Chat, placeorg.MaxLinks)
			}
		}
	}
	for _, id := range ids {
		hist[per[id]]++
	}
	wantHist := map[int]int{0: 292, 1: 73, 2: 24}
	for k, want := range wantHist {
		if hist[k] != want {
			t.Errorf("shortcut histogram %v, want %v", hist, wantHist)
			break
		}
	}
	mismatches := 0
	for _, id := range ids {
		if per[id] != assigned[id].Shortcuts {
			mismatches++
		}
	}
	if mismatches != 0 {
		t.Errorf("%d chats differ from the reference shortcut count", mismatches)
	}

	month := map[string]bool{}
	for _, op := range ops {
		if op.Kind == placeorg.OpLens && op.Into == axisMonth {
			month[op.Place] = true
			if op.Name != nameByMonth {
				t.Errorf("by-month lens named %q", op.Name)
			}
		}
	}
	counts := map[int]int{}
	for _, id := range ids {
		row := assigned[id]
		if row.Live {
			counts[row.Root]++
		}
	}
	wantMonth := map[string]bool{}
	for root, n := range counts {
		if n >= monthPlaceChats {
			wantMonth["R"+strconv.Itoa(root)] = true
		}
	}
	// The reference run records a by-month view on the 6 roots at or above
	// the cut. A view on a child would show up here as an extra place.
	if !reflect.DeepEqual(month, wantMonth) || len(month) != 6 {
		t.Fatalf("by-month places %v, want %v", month, wantMonth)
	}
}

type refRow struct {
	Root      int    `json:"root"`
	Leaf      int    `json:"leaf"`
	ParkLevel string `json:"park_level"`
	ParkNode  int    `json:"park_node"`
	Live      bool   `json:"live"`
	Shortcuts int    `json:"shortcuts"`
}

func readIDs(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	if err := json.Unmarshal(raw, &ids); err != nil {
		t.Fatal(err)
	}
	return ids
}

func readAssignments(t *testing.T, path string) map[string]refRow {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Assignments map[string]refRow `json:"assignments"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return doc.Assignments
}

// referenceTree builds the finest places from the reference leaf labels and
// the top-level places from the root labels. Centroids are the unit mean of
// the live chats in that leaf, which is the matrix the reference dotted
// against. Depth 3 is the fine level and depth 1 the root, so a flat root
// is not a shortcut candidate.
func referenceTree(t *testing.T, ids []string, rows map[string]refRow, vectors [][]float32) (*placeorg.Tree, []string) {
	t.Helper()
	type acc struct {
		sum     []float64
		root    int
		rootSet bool
		members []string
	}
	leaves := map[int]*acc{}
	rootMembers := map[int][]string{}
	add := func(leaf, root, i int, id string) {
		if leaf < 0 {
			return
		}
		a := leaves[leaf]
		if a == nil {
			a = &acc{sum: make([]float64, len(vectors[i]))}
			leaves[leaf] = a
		}
		if !a.rootSet {
			a.root = root
			a.rootSet = true
		} else if a.root != root {
			t.Fatalf("leaf %d sits under roots %d and %d", leaf, a.root, root)
		}
		for d, v := range vectors[i] {
			a.sum[d] += float64(v)
		}
		a.members = append(a.members, id)
	}
	for i, id := range ids {
		row, ok := rows[id]
		if !ok {
			t.Fatalf("no assignment for %s", id)
		}
		if !row.Live {
			continue
		}
		rootMembers[row.Root] = append(rootMembers[row.Root], id)
		// The reference centroid is every live chat with that leaf label,
		// plus any chat parked on that leaf under a different label.
		if row.Leaf >= 0 {
			add(row.Leaf, row.Root, i, id)
		}
		if row.ParkLevel == "leaf" && row.ParkNode >= 0 && row.ParkNode != row.Leaf {
			add(row.ParkNode, row.Root, i, id)
		}
	}

	var rootIDs []string
	var nodes []*placeorg.Node
	childrenOf := map[int][]string{}
	for leaf, a := range leaves {
		var ss float64
		for _, v := range a.sum {
			ss += v * v
		}
		inv := 1 / math.Sqrt(ss)
		sum := make([]float32, len(a.sum))
		for d, v := range a.sum {
			sum[d] = float32(v * inv)
		}
		id := "L" + strconv.Itoa(leaf)
		nodes = append(nodes, &placeorg.Node{
			ID:      id,
			Depth:   3,
			Members: a.members,
			Centroids: map[placeorg.SpaceID]placeorg.Centroid{
				"ref": {Sum: sum, Count: 1},
			},
		})
		childrenOf[a.root] = append(childrenOf[a.root], id)
	}
	for root, members := range rootMembers {
		id := "R" + strconv.Itoa(root)
		rootIDs = append(rootIDs, id)
		nodes = append(nodes, &placeorg.Node{
			ID:       id,
			Depth:    1,
			Members:  members,
			Children: childrenOf[root],
		})
	}
	sort.Strings(rootIDs)
	return plant(t, rootIDs, nodes...), rootIDs
}

var npyShape = regexp.MustCompile(`shape': \((\d+),\s*(\d+)\)`)

// readNPY reads a C-order float32 matrix. The reference file is that and
// nothing else; a different dtype is a different embedding and must not be
// scored as if it were this one.
func readNPY(t *testing.T, path string) [][]float32 {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) < 10 || string(b[:6]) != "\x93NUMPY" {
		t.Fatalf("%s is not an npy file", path)
	}
	major := b[6]
	var headerLen int
	var dataAt int
	switch major {
	case 1:
		headerLen = int(binary.LittleEndian.Uint16(b[8:10]))
		dataAt = 10 + headerLen
	case 2, 3:
		if len(b) < 12 {
			t.Fatal("truncated npy header")
		}
		headerLen = int(binary.LittleEndian.Uint32(b[8:12]))
		dataAt = 12 + headerLen
	default:
		t.Fatalf("npy version %d", major)
	}
	header := string(b[dataAt-headerLen : dataAt])
	if !regexp.MustCompile(`descr': '<f4'`).MatchString(header) || regexp.MustCompile(`fortran_order': True`).MatchString(header) {
		t.Fatalf("want a little-endian float32 C-order matrix, header %s", header)
	}
	m := npyShape.FindStringSubmatch(header)
	if m == nil {
		t.Fatalf("no shape in %s", header)
	}
	rows, _ := strconv.Atoi(m[1])
	cols, _ := strconv.Atoi(m[2])
	need := rows * cols * 4
	if dataAt+need > len(b) {
		t.Fatalf("npy data is %d bytes, need %d", len(b)-dataAt, need)
	}
	out := make([][]float32, rows)
	off := dataAt
	for i := 0; i < rows; i++ {
		row := make([]float32, cols)
		for j := 0; j < cols; j++ {
			row[j] = math.Float32frombits(binary.LittleEndian.Uint32(b[off : off+4]))
			off += 4
		}
		out[i] = row
	}
	return out
}
