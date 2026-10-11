package audit

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/placeorg"
)

// TestParityMatchesTheReferenceOps compares the op list to the saved run.
// It skips when PLACEORG_REF is unset, so a checkout without that tree still
// builds. The comparison is the set of chats each op moves. Community numbers
// are labels and are not compared; a small top-level place the reference
// folded and then logged a second time is one fold here, then the top-level
// place itself going to Now.
func TestParityMatchesTheReferenceOps(t *testing.T) {
	root := os.Getenv("PLACEORG_REF")
	if root == "" {
		t.Skip("PLACEORG_REF is unset")
	}
	ids := loadStringList(t, filepath.Join(root, "emb_row_ids.json"))
	vecs := loadF32Matrix(t, filepath.Join(root, "emb_all.npy"))
	if len(vecs) != len(ids) {
		t.Fatalf("embeddings %d rows, ids %d", len(vecs), len(ids))
	}
	leaf0, mid0, root0 := loadPartitions(t, filepath.Join(root, "runs", "run1", "consensus_partitions.json"))
	leaf, mid, rootLab := nestPartitions(leaf0, mid0, root0)
	if len(leaf) != len(ids) {
		t.Fatalf("partition length %d, ids %d", len(leaf), len(ids))
	}
	tree, chats := preAuditTree(ids, leaf, mid, rootLab)
	demoted := demotedChats(t, filepath.Join(root, "assignments.json"))
	judge := &replayJudge{activity: demoted}
	ops, err := Auditor{}.Audit(context.Background(), tree, &placeorg.Corpus{Chats: chats}, vecs, judge)
	if err != nil {
		t.Fatal(err)
	}
	got := map[placeorg.OpKind][]stringSet{}
	for _, op := range ops {
		if !op.Proposed || op.Stage != placeorg.StageAudit || op.Why == "" {
			t.Fatalf("op is not a proposed audit line: %+v", op)
		}
		got[op.Kind] = append(got[op.Kind], setOf(op.Members))
	}
	want := referenceOpSets(t, filepath.Join(root, "runs", "run1", "places_results.json"), filepath.Join(root, "assignments.json"))
	for _, kind := range []placeorg.OpKind{placeorg.OpCollapse, placeorg.OpDissolve, placeorg.OpDemote} {
		if !sameSets(got[kind], want[kind]) {
			t.Fatalf("%s sets differ\ngot %d %v\nwant %d %v", kind, len(got[kind]), sizes(got[kind]), len(want[kind]), sizes(want[kind]))
		}
	}
	if len(got[placeorg.OpInterpose]) != 0 || len(got[placeorg.OpSplit]) != 0 {
		t.Fatalf("interpose %d split %d", len(got[placeorg.OpInterpose]), len(got[placeorg.OpSplit]))
	}
	if judge.calls == 0 {
		t.Fatal("the facet question was never asked")
	}
}

type stringSet map[string]struct{}

func setOf(ids []string) stringSet {
	s := stringSet{}
	for _, id := range ids {
		s[id] = struct{}{}
	}
	return s
}

func sameSets(got, want []stringSet) bool {
	if len(got) != len(want) {
		return false
	}
	used := make([]bool, len(want))
	for _, g := range got {
		found := false
		for i, w := range want {
			if used[i] || len(g) != len(w) {
				continue
			}
			eq := true
			for id := range g {
				if _, ok := w[id]; !ok {
					eq = false
					break
				}
			}
			if eq {
				used[i] = true
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func sizes(sets []stringSet) []int {
	out := make([]int, len(sets))
	for i, s := range sets {
		out[i] = len(s)
	}
	return out
}

// replayJudge answers ACTIVITY only for the one group the reference turned
// into a view, and THING for every other group. An anchor would have kept
// those other groups without a question; THING keeps them too, so the op
// list does not depend on a word list.
type replayJudge struct {
	activity stringSet
	calls    int
}

func (j *replayJudge) Choose(_ context.Context, q placeorg.Question, _ placeorg.Option) placeorg.Pick {
	j.calls++
	if sameSets([]stringSet{setOf(chatsIn(q.Context))}, []stringSet{j.activity}) {
		return placeorg.Pick{Key: "ACTIVITY"}
	}
	return placeorg.Pick{Key: "THING"}
}

func (j *replayJudge) Write(_ context.Context, _ placeorg.Question, _ placeorg.Validator, fallback string) placeorg.Pick {
	return placeorg.Pick{Written: fallback, FellBack: true}
}

func chatsIn(context string) []string {
	i := strings.LastIndex(context, "chats:")
	if i < 0 {
		return nil
	}
	return strings.Fields(context[i+len("chats:"):])
}

func demotedChats(t *testing.T, path string) stringSet {
	t.Helper()
	asg := loadAssignments(t, path)
	out := stringSet{}
	for id, row := range asg {
		// -8 is the marker the reference wrote when it turned mid 7 into a view.
		if row.Mid == -8 {
			out[id] = struct{}{}
		}
	}
	if len(out) == 0 {
		t.Fatal("the reference has no demoted group")
	}
	return out
}

func referenceOpSets(t *testing.T, opsPath, asgPath string) map[placeorg.OpKind][]stringSet {
	t.Helper()
	raw, err := os.ReadFile(opsPath)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Ops []string `json:"ops"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	asg := loadAssignments(t, asgPath)
	byMid := map[int][]string{}
	byRoot := map[int][]string{}
	for id, row := range asg {
		byMid[row.Mid] = append(byMid[row.Mid], id)
		byRoot[row.Root] = append(byRoot[row.Root], id)
	}
	out := map[placeorg.OpKind][]stringSet{}
	collapseRe := regexp.MustCompile(`^COLLAPSE mid#(\d+) \((\d+)/`)
	dissolveRe := regexp.MustCompile(`^DISSOLVE root#(\d+) \((\d+)`)
	demoteRe := regexp.MustCompile(`^DEMOTE mid#(\d+) \((\d+)\)`)
	for _, line := range doc.Ops {
		if m := collapseRe.FindStringSubmatch(line); m != nil {
			mid, _ := strconv.Atoi(m[1])
			size, _ := strconv.Atoi(m[2])
			ids := byMid[-1-mid]
			if len(ids) != size {
				// A fold that the reference logged again flipped the marker
				// back to the original number. The chats are that number.
				ids = byMid[mid]
			}
			if len(ids) != size {
				t.Fatalf("%s: chat set %d, op says %d", line, len(ids), size)
			}
			out[placeorg.OpCollapse] = append(out[placeorg.OpCollapse], setOf(ids))
			continue
		}
		if m := dissolveRe.FindStringSubmatch(line); m != nil {
			r, _ := strconv.Atoi(m[1])
			size, _ := strconv.Atoi(m[2])
			ids := byRoot[r]
			if len(ids) != size {
				t.Fatalf("%s: chat set %d, op says %d", line, len(ids), size)
			}
			out[placeorg.OpDissolve] = append(out[placeorg.OpDissolve], setOf(ids))
			continue
		}
		if m := demoteRe.FindStringSubmatch(line); m != nil {
			mid, _ := strconv.Atoi(m[1])
			size, _ := strconv.Atoi(m[2])
			ids := byMid[-1-mid]
			if len(ids) != size {
				t.Fatalf("%s: chat set %d, op says %d", line, len(ids), size)
			}
			out[placeorg.OpDemote] = append(out[placeorg.OpDemote], setOf(ids))
		}
	}
	return out
}

type asgRow struct {
	Root int `json:"root"`
	Mid  int `json:"mid"`
	Leaf int `json:"leaf"`
}

func loadAssignments(t *testing.T, path string) map[string]asgRow {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Assignments map[string]asgRow `json:"assignments"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return doc.Assignments
}

func loadStringList(t *testing.T, path string) []string {
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

func loadPartitions(t *testing.T, path string) (leaf, mid, root []int) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string][][]int
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	// Run 0 is the partition the reference built the tree from. The other
	// seven runs in the file are the same partition.
	return doc["2.5"][0], doc["1.0"][0], doc["0.4"][0]
}

// nestPartitions repeats the reference's nesting: each fine community takes
// the coarse label of most of its chats, and a tie takes the smaller label.
func nestPartitions(leaf, mid0, root0 []int) (leafOut, mid, root []int) {
	mo := nest(leaf, mid0)
	ro := nest(mid0, root0)
	mid = make([]int, len(leaf))
	root = make([]int, len(leaf))
	for i, l := range leaf {
		mid[i] = mo[l]
		root[i] = ro[mid[i]]
	}
	return leaf, mid, root
}

func nest(fine, coarse []int) map[int]int {
	groups := map[int][]int{}
	for i, f := range fine {
		groups[f] = append(groups[f], coarse[i])
	}
	out := map[int]int{}
	for cid, vals := range groups {
		counts := map[int]int{}
		for _, v := range vals {
			counts[v]++
		}
		best, bestN := 0, -1
		for label, n := range counts {
			if n > bestN || (n == bestN && label < best) {
				best, bestN = label, n
			}
		}
		out[cid] = best
	}
	return out
}

// preAuditTree puts every chat on its finest place. Middles and top-level
// places hold no chats of their own; a chat belongs to them through the child.
func preAuditTree(ids []string, leaf, mid, root []int) (*placeorg.Tree, []placeorg.Chat) {
	type key struct {
		kind string
		id   int
	}
	nodes := map[string]*placeorg.Node{}
	ensure := func(kind string, id, depth int, parent string) *placeorg.Node {
		nid := kind + strconv.Itoa(id)
		n, ok := nodes[nid]
		if ok {
			return n
		}
		n = placeNode(nid, parent, nid, depth, nil, nil)
		nodes[nid] = n
		return n
	}
	var roots []string
	seenRoot := map[int]bool{}
	chats := make([]placeorg.Chat, len(ids))
	for i, id := range ids {
		chats[i] = placeorg.Chat{ID: id, Title: id}
		r := ensure("r", root[i], 1, "")
		m := ensure("m", mid[i], 2, r.ID)
		l := ensure("l", leaf[i], 3, m.ID)
		l.Members = append(l.Members, id)
		if !contains(m.Children, l.ID) {
			m.Children = append(m.Children, l.ID)
		}
		if !contains(r.Children, m.ID) {
			r.Children = append(r.Children, m.ID)
		}
		if !seenRoot[root[i]] {
			seenRoot[root[i]] = true
			roots = append(roots, r.ID)
		}
	}
	return plantTree(roots, nodes, nil, nil), chats
}

func contains(ids []string, id string) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

func loadF32Matrix(t *testing.T, path string) [][]float32 {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	magic := make([]byte, 6)
	if _, err := f.Read(magic); err != nil {
		t.Fatal(err)
	}
	if string(magic) != "\x93NUMPY" {
		t.Fatalf("not an npy file")
	}
	var ver [2]byte
	if _, err := f.Read(ver[:]); err != nil {
		t.Fatal(err)
	}
	var hlen uint32
	if ver[0] == 1 {
		var n uint16
		if err := binary.Read(f, binary.LittleEndian, &n); err != nil {
			t.Fatal(err)
		}
		hlen = uint32(n)
	} else {
		if err := binary.Read(f, binary.LittleEndian, &hlen); err != nil {
			t.Fatal(err)
		}
	}
	hdr := make([]byte, hlen)
	if _, err := f.Read(hdr); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(hdr), "'descr': '<f4'") || strings.Contains(string(hdr), "'fortran_order': True") {
		t.Fatalf("unexpected npy header %s", hdr)
	}
	shape := regexp.MustCompile(`'shape': \((\d+), (\d+)\)`).FindSubmatch(hdr)
	if shape == nil {
		t.Fatalf("no shape in %s", hdr)
	}
	rows, _ := strconv.Atoi(string(shape[1]))
	cols, _ := strconv.Atoi(string(shape[2]))
	out := make([][]float32, rows)
	buf := make([]byte, cols*4)
	for i := 0; i < rows; i++ {
		if _, err := f.Read(buf); err != nil {
			t.Fatal(err)
		}
		row := make([]float32, cols)
		for c := 0; c < cols; c++ {
			row[c] = math.Float32frombits(binary.LittleEndian.Uint32(buf[c*4 : c*4+4]))
		}
		out[i] = row
	}
	return out
}
