package park

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/placeorg"
)

// The histogram and the thresholds are read off the saved embeddings
// (emb_all.npy) and the post-audit labels in assignments.json. Parking in
// the reference run happens after collapse, dissolve and demote, so the
// tree handed to Park is that shape, not the pre-audit forest. A fresh
// embedding fetch is not used: two fetches of the same chats move the
// histogram on their own.
func TestParkHistogramAndThresholdsMatchTheReference(t *testing.T) {
	dir := os.Getenv("PLACEORG_REF")
	if dir == "" {
		t.Skip("PLACEORG_REF is unset; the saved embeddings are not in the repository")
	}
	ids := loadStringList(t, filepath.Join(dir, "emb_row_ids.json"))
	vectors := loadNpy(t, filepath.Join(dir, "emb_all.npy"))
	if len(vectors) != len(ids) {
		t.Fatalf("%d vectors for %d ids", len(vectors), len(ids))
	}
	assign := loadParkAssignments(t, filepath.Join(dir, "assignments.json"))
	tree := auditTree(t, ids, assign)
	chow := New()
	corpus := &placeorg.Corpus{Chats: make([]placeorg.Chat, len(ids))}
	for i, id := range ids {
		corpus.Chats[i] = placeorg.Chat{ID: id}
	}
	ops, err := chow.Park(context.Background(), tree, corpus, vectors, placeorg.SpacePrimary)
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) != len(ids) {
		t.Fatalf("ops = %d, chats = %d", len(ops), len(ids))
	}
	got := map[string]int{}
	kept := 0
	for _, op := range ops {
		got[bucket(op)]++
		if op.Kind != placeorg.OpInbox {
			kept++
		}
	}
	// Reference run: root 219, mid 33, leaf 109, inbox 28.
	ref := map[string]int{"root": 219, "mid": 33, "leaf": 109, "inbox": 28}
	for level, want := range ref {
		rel := math.Abs(float64(got[level]-want)) / float64(want)
		if rel > 0.05 {
			t.Errorf("%s = %d, reference %d, which is %.1f%% away (limit 5%%)", level, got[level], want, rel*100)
		}
	}
	th := chow.Thresholds()[placeorg.SpacePrimary]
	if math.Abs(float64(th.Floor-0.45)) > 0.02 {
		t.Errorf("floor = %v, want within 0.02 of 0.45", th.Floor)
	}
	if math.Abs(float64(th.Margin-0.04)) > 0.02 {
		t.Errorf("margin = %v, want within 0.02 of 0.04", th.Margin)
	}
	var rooted float64
	for _, id := range tree.Roots() {
		node, ok := tree.Node(id)
		if !ok {
			t.Fatalf("missing root %s", id)
		}
		rooted += node.Centroids[placeorg.SpacePrimary].Count
	}
	if rooted != float64(kept) {
		t.Fatalf("root positions count %v chats and %d stayed out of Now", rooted, kept)
	}
}

func auditTree(t *testing.T, ids []string, assign map[string]parkAssignment) *placeorg.Tree {
	t.Helper()
	rootSize := map[int]int{}
	for _, id := range ids {
		rootSize[assign[id].Root]++
	}
	nodes := map[string]*placeorg.BuiltNode{}
	homes := map[string]string{}
	var inbox []string
	ensure := func(id, parent string, depth int) *placeorg.BuiltNode {
		if node, ok := nodes[id]; ok {
			return node
		}
		node := &placeorg.BuiltNode{ID: id, Parent: parent, Depth: depth}
		nodes[id] = node
		return node
	}
	for _, id := range ids {
		row := assign[id]
		// A root below quorum was dissolved before parking. Its chats are
		// already in Now and are not scored.
		if rootSize[row.Root] < placeorg.Quorum {
			inbox = append(inbox, id)
			continue
		}
		rootID := fmt.Sprintf("r%d", row.Root)
		ensure(rootID, "", 1)
		home := rootID
		if row.Mid >= 0 {
			midID := fmt.Sprintf("m%d", row.Mid)
			ensure(midID, rootID, 2)
			home = midID
		}
		if row.Leaf >= 0 {
			parent := rootID
			depth := 2
			if row.Mid >= 0 {
				parent = fmt.Sprintf("m%d", row.Mid)
				depth = 3
			}
			leafID := fmt.Sprintf("f%d", row.Leaf)
			ensure(leafID, parent, depth)
			home = leafID
		}
		nodes[home].Members = append(nodes[home].Members, id)
		homes[id] = home
	}
	list := make([]placeorg.BuiltNode, 0, len(nodes))
	for _, node := range nodes {
		list = append(list, *node)
	}
	tree, err := placeorg.Assemble(list, homes, inbox)
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

type parkAssignment struct {
	Root int `json:"root"`
	Mid  int `json:"mid"`
	Leaf int `json:"leaf"`
}

func loadParkAssignments(t *testing.T, path string) map[string]parkAssignment {
	t.Helper()
	var doc struct {
		Assignments map[string]parkAssignment `json:"assignments"`
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return doc.Assignments
}

func loadStringList(t *testing.T, path string) []string {
	t.Helper()
	var ids []string
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &ids); err != nil {
		t.Fatal(err)
	}
	return ids
}

func loadNpy(t *testing.T, path string) [][]float32 {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	magic := make([]byte, 8)
	if _, err := io.ReadFull(f, magic); err != nil {
		t.Fatal(err)
	}
	if string(magic[:6]) != "\x93NUMPY" {
		t.Fatalf("%s is not an npy file", path)
	}
	var headerLen uint32
	if magic[6] == 1 {
		var n uint16
		if err := binary.Read(f, binary.LittleEndian, &n); err != nil {
			t.Fatal(err)
		}
		headerLen = uint32(n)
	} else if err := binary.Read(f, binary.LittleEndian, &headerLen); err != nil {
		t.Fatal(err)
	}
	header := make([]byte, headerLen)
	if _, err := io.ReadFull(f, header); err != nil {
		t.Fatal(err)
	}
	text := string(header)
	mark := "shape': ("
	at := strings.Index(text, mark)
	if at < 0 {
		t.Fatalf("npy header has no shape: %s", text)
	}
	rest := text[at+len(mark):]
	rowText, colsText, _ := strings.Cut(rest, ",")
	rows, err := strconv.Atoi(strings.TrimSpace(rowText))
	if err != nil {
		t.Fatal(err)
	}
	colsText, _, _ = strings.Cut(colsText, ")")
	cols, err := strconv.Atoi(strings.TrimSpace(colsText))
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != rows*cols*4 {
		t.Fatalf("npy payload is %d bytes, want %d", len(data), rows*cols*4)
	}
	out := make([][]float32, rows)
	for r := 0; r < rows; r++ {
		row := make([]float32, cols)
		base := r * cols * 4
		for c := 0; c < cols; c++ {
			row[c] = math.Float32frombits(binary.LittleEndian.Uint32(data[base+c*4:]))
		}
		out[r] = row
	}
	return out
}
