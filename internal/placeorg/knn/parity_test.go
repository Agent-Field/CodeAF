package knn

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestSavedReferenceEdgeParity(t *testing.T) {
	ref := os.Getenv("PLACEORG_REF")
	if ref == "" {
		t.Skip("set PLACEORG_REF and PLACEORG_KNN_EDGES for saved-embedding parity")
	}
	edgesPath := os.Getenv("PLACEORG_KNN_EDGES")
	if edgesPath == "" {
		t.Fatal("generate PLACEORG_KNN_EDGES with testdata/reference_edges.py; see README.md")
	}
	rows, err := readNPY(filepath.Join(ref, "emb_all.npy"))
	if err != nil {
		t.Fatal(err)
	}
	idsBytes, err := os.ReadFile(filepath.Join(ref, "emb_row_ids.json"))
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	if err := json.Unmarshal(idsBytes, &ids); err != nil {
		t.Fatal(err)
	}
	edgeBytes, err := os.ReadFile(edgesPath)
	if err != nil {
		t.Fatal(err)
	}
	var pairs [][2]int
	if err := json.Unmarshal(edgeBytes, &pairs); err != nil {
		t.Fatal(err)
	}
	want := make(map[[2]int]bool, len(pairs))
	for _, pair := range pairs {
		if pair[0] < 0 || pair[0] >= pair[1] || pair[1] >= len(rows) {
			t.Fatalf("invalid reference edge %v", pair)
		}
		want[pair] = true
	}
	got, err := New(8, 0.35).KNN(context.Background(), rows, nil, ids)
	if err != nil {
		t.Fatal(err)
	}
	intersection := 0
	for _, edge := range got.Edges {
		if want[[2]int{edge.I, edge.J}] {
			intersection++
		}
	}
	union := len(want) + len(got.Edges) - intersection
	if union == 0 {
		t.Fatal("empty reference graph cannot prove parity")
	}
	jaccard := float64(intersection) / float64(union)
	t.Logf("saved embeddings: %d rows x %d dims; reference=%d port=%d intersection=%d Jaccard=%.9f", len(rows), len(rows[0]), len(want), len(got.Edges), intersection, jaccard)
	if jaccard < 0.9 {
		t.Fatalf("edge Jaccard %.9f is below 0.9", jaccard)
	}
}

func TestSavedVectorFixture(t *testing.T) {
	rows, err := readNPY("testdata/vectors.npy")
	if err != nil {
		t.Fatal(err)
	}
	graph, err := New(1, 0.35).KNN(context.Background(), rows, nil, []string{"a", "b", "c"})
	if err != nil {
		t.Fatal(err)
	}
	assertGraph(t, graph, exactOracle(rows, 1, 0.35))
	if len(graph.Edges) != 1 {
		t.Fatalf("fixture returned %d edges, want 1", len(graph.Edges))
	}
}

// readNPY accepts only the reference's C-order float32 matrix, so a wrong
// embedding layout fails explicitly instead of producing a plausible graph.
func readNPY(path string) ([][]float32, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) < 10 || string(data[:6]) != "\x93NUMPY" {
		return nil, fmt.Errorf("invalid NPY header")
	}
	headerStart := 10
	headerLen := int(binary.LittleEndian.Uint16(data[8:10]))
	switch data[6] {
	case 1:
	case 2, 3:
		if len(data) < 12 {
			return nil, fmt.Errorf("truncated NPY header")
		}
		headerStart = 12
		headerLen = int(binary.LittleEndian.Uint32(data[8:12]))
	default:
		return nil, fmt.Errorf("unsupported NPY version")
	}
	if headerLen > len(data)-headerStart {
		return nil, fmt.Errorf("truncated NPY header")
	}
	header := string(data[headerStart : headerStart+headerLen])
	if !strings.Contains(header, "'descr': '<f4'") || !strings.Contains(header, "'fortran_order': False") {
		return nil, fmt.Errorf("NPY must be C-order little-endian float32")
	}
	shape := regexp.MustCompile(`'shape':\s*\((\d+),\s*(\d+),?\)`).FindStringSubmatch(header)
	if len(shape) != 3 {
		return nil, fmt.Errorf("NPY must have two dimensions")
	}
	n, err := strconv.Atoi(shape[1])
	if err != nil {
		return nil, err
	}
	dims, err := strconv.Atoi(shape[2])
	if err != nil {
		return nil, err
	}
	payload := data[headerStart+headerLen:]
	if dims == 0 || len(payload)%4 != 0 || n > len(payload)/4/dims || n*dims != len(payload)/4 {
		return nil, fmt.Errorf("NPY payload does not match shape")
	}
	flat := make([]float32, n*dims)
	for i := range flat {
		flat[i] = math.Float32frombits(binary.LittleEndian.Uint32(payload[i*4 : i*4+4]))
	}
	rows := make([][]float32, n)
	for i := range rows {
		rows[i] = flat[i*dims : (i+1)*dims]
	}
	return rows, nil
}
