package parity

import (
	"encoding/binary"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// LevelNames are the three columns of the reference assignments, coarse to fine
// in the tree and fine to coarse in the Leiden resolutions. The report uses
// these words.
var LevelNames = []string{"root", "mid", "leaf"}

// ChatLevels is one chat's place at each of those columns. A negative id is a
// place the reference audit folded away.
type ChatLevels struct {
	Root, Mid, Leaf int
}

// Reference is the saved run: row order, assignments, embeddings, and the
// operation lines the reference printed.
type Reference struct {
	IDs      []string
	Levels   map[string]ChatLevels
	Vectors  [][]float32
	OpLines  []string
	OpCounts map[string]int
}

var npyShape = regexp.MustCompile(`\((\d+),\s*(\d+)\)`)

// Load reads assignments.json, emb_row_ids.json, emb_all.npy and the reference
// run's operation lines. dir is PLACEORG_REF. The embeddings stay here and are
// never fetched again: two fetches of the same texts are not the same vectors.
func Load(dir string) (Reference, error) {
	var ref Reference
	raw, err := os.ReadFile(filepath.Join(dir, "assignments.json"))
	if err != nil {
		return ref, err
	}
	var doc struct {
		Assignments map[string]struct {
			Root int `json:"root"`
			Mid  int `json:"mid"`
			Leaf int `json:"leaf"`
		} `json:"assignments"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return ref, err
	}
	ref.Levels = map[string]ChatLevels{}
	for id, a := range doc.Assignments {
		ref.Levels[id] = ChatLevels{Root: a.Root, Mid: a.Mid, Leaf: a.Leaf}
	}
	rawIDs, err := os.ReadFile(filepath.Join(dir, "emb_row_ids.json"))
	if err != nil {
		return ref, err
	}
	if err := json.Unmarshal(rawIDs, &ref.IDs); err != nil {
		return ref, err
	}
	ref.Vectors, err = readNpy(filepath.Join(dir, "emb_all.npy"), len(ref.IDs))
	if err != nil {
		return ref, err
	}
	opsPath := filepath.Join(dir, "runs", "run1", "places_results.json")
	if rawOps, err := os.ReadFile(opsPath); err == nil {
		var ops struct {
			Ops []string `json:"ops"`
		}
		if err := json.Unmarshal(rawOps, &ops); err != nil {
			return ref, err
		}
		ref.OpLines = ops.Ops
		ref.OpCounts = countLines(ops.Ops)
	}
	return ref, nil
}

// readNpy reads a little-endian float32 matrix in numpy's version 1 format.
func readNpy(path string, rows int) ([][]float32, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(raw) < 10 || string(raw[1:6]) != "NUMPY" {
		return nil, errNotNpy
	}
	headerLen := int(binary.LittleEndian.Uint16(raw[8:10]))
	if 10+headerLen > len(raw) {
		return nil, errNotNpy
	}
	header := string(raw[10 : 10+headerLen])
	m := npyShape.FindStringSubmatch(header)
	if m == nil || !strings.Contains(header, "<f4") {
		return nil, errNotNpy
	}
	r, _ := strconv.Atoi(m[1])
	c, _ := strconv.Atoi(m[2])
	if r != rows {
		return nil, errRowCount
	}
	body := raw[10+headerLen:]
	out := make([][]float32, r)
	for i := range out {
		out[i] = make([]float32, c)
		for j := range out[i] {
			out[i][j] = math.Float32frombits(binary.LittleEndian.Uint32(body[(i*c+j)*4:]))
		}
	}
	return out, nil
}

func countLines(lines []string) map[string]int {
	out := map[string]int{}
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		out[strings.ToUpper(fields[0])]++
	}
	return out
}
