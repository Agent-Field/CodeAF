package nest

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/placeorg"
)

// The parent map is compared on the saved run: the consensus partitions and
// the per-chat root and mid in assignments.json, both produced from
// emb_all.npy. The embeddings are not read as numbers here. Nesting does
// not look at a vector. The file's presence and its row count are what tie
// this check to that run rather than to a fresh fetch, which moves leaves
// on its own.
func TestParentMapMatchesTheReference(t *testing.T) {
	dir := os.Getenv("PLACEORG_REF")
	if dir == "" {
		t.Skip("PLACEORG_REF is unset; the saved embeddings are not in the repository")
	}
	ids := loadIDs(t, filepath.Join(dir, "emb_row_ids.json"))
	rows := npyRows(t, filepath.Join(dir, "emb_all.npy"))
	if rows != len(ids) {
		t.Fatalf("emb_all.npy has %d rows and emb_row_ids.json has %d ids", rows, len(ids))
	}
	parts := loadPartitions(t, filepath.Join(dir, "runs", "run1", "consensus_partitions.json"))
	assign := loadAssignments(t, filepath.Join(dir, "assignments.json"))
	if len(parts["2.5"]) == 0 || len(parts["2.5"][0]) != len(ids) {
		t.Fatalf("fine partition length %d, ids %d", len(parts["2.5"][0]), len(ids))
	}
	// The reference run's eight partitions at each resolution are identical,
	// and the final cut keeps those communities. The first one is the input
	// the communities stage would hand over for that run.
	fine, mid, coarse := parts["2.5"][0], parts["1.0"][0], parts["0.4"][0]
	corpus := &placeorg.Corpus{Chats: make([]placeorg.Chat, len(ids))}
	for i, id := range ids {
		corpus.Chats[i] = placeorg.Chat{ID: id}
	}
	tree, err := New().Nest(context.Background(), corpus, fine, mid, coarse)
	if err != nil {
		t.Fatal(err)
	}
	gotRoot, gotMid, gotFine := map[string]string{}, map[string]string{}, map[string]string{}
	wantRoot, wantMid := map[string]string{}, map[string]string{}
	wantFine := map[string]string{}
	for i, id := range ids {
		chain := chainOf(t, tree, id)
		if len(chain) != 3 {
			t.Fatalf("%s path = %v", id, chain)
		}
		gotRoot[id], gotMid[id], gotFine[id] = chain[0], chain[1], chain[2]
		row, ok := assign[id]
		if !ok {
			t.Fatalf("no assignment for %s", id)
		}
		wantRoot[id] = strconv.Itoa(row.Root)
		wantMid[id] = strconv.Itoa(row.Mid)
		wantFine[id] = strconv.Itoa(fine[i])
	}
	if !samePartition(gotRoot, wantRoot) {
		t.Fatal("root parent map differs from the reference")
	}
	if !samePartition(gotMid, wantMid) {
		t.Fatal("mid parent map differs from the reference")
	}
	if !samePartition(gotFine, wantFine) {
		t.Fatal("fine communities were merged or split")
	}
	depths := map[int]int{}
	var walk func(string)
	walk = func(id string) {
		node, ok := tree.Node(id)
		if !ok {
			t.Fatalf("missing %s", id)
		}
		if node.Name != "" {
			t.Fatalf("place %s has a name", id)
		}
		depths[node.Depth]++
		for _, child := range node.Children {
			walk(child)
		}
	}
	for _, root := range tree.Roots() {
		walk(root)
	}
	// The reference run prints "14 roots 17 mids 26 leaves" before groups are trimmed.
	if depths[1] != 14 || depths[2] != 17 || depths[3] != 26 {
		t.Fatalf("places by depth = %v, want 14 roots, 17 mids, 26 leaves", depths)
	}
}

type assignmentRow struct {
	Root int `json:"root"`
	Mid  int `json:"mid"`
	Leaf int `json:"leaf"`
}

func loadIDs(t *testing.T, path string) []string {
	t.Helper()
	var ids []string
	readJSON(t, path, &ids)
	return ids
}

func loadAssignments(t *testing.T, path string) map[string]assignmentRow {
	t.Helper()
	var doc struct {
		Assignments map[string]assignmentRow `json:"assignments"`
	}
	readJSON(t, path, &doc)
	return doc.Assignments
}

func loadPartitions(t *testing.T, path string) map[string][][]int {
	t.Helper()
	var parts map[string][][]int
	readJSON(t, path, &parts)
	return parts
}

func readJSON(t *testing.T, path string, dest any) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, dest); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

func npyRows(t *testing.T, path string) int {
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
	} else {
		if err := binary.Read(f, binary.LittleEndian, &headerLen); err != nil {
			t.Fatal(err)
		}
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
	rowText, _, _ := strings.Cut(rest, ",")
	rows, err := strconv.Atoi(strings.TrimSpace(rowText))
	if err != nil {
		t.Fatal(err)
	}
	return rows
}
