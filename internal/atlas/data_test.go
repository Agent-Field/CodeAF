package atlas

import (
	"os"
	"path/filepath"
	"testing"
)

// The repo root, two directories up from this package: every Files[].Path in
// the data is relative to it.
const repoRoot = "../.."

// TestEveryFilePathExists holds the diagram to the tree: a file the atlas
// points at must exist, or the map is lying about where a part lives. The
// TypeScript twin (tools/atlas/test/data.test.ts) holds its own copy to the
// same tree, so a path that drifts fails both.
func TestEveryFilePathExists(t *testing.T) {
	for _, n := range Atlas.Nodes {
		for _, f := range n.Files {
			if _, err := os.Stat(filepath.Join(repoRoot, f.Path)); err != nil {
				t.Errorf("node %s names the file %s, which does not exist", n.ID, f.Path)
			}
			for _, s := range f.Symbols {
				raw, readErr := os.ReadFile(filepath.Join(repoRoot, f.Path))
				if readErr != nil {
					t.Errorf("node %s names the file %s: %v", n.ID, f.Path, readErr)
					continue
				}
				if !containsName(string(raw), s) {
					t.Errorf("node %s says %s names %s, which the file never spells", n.ID, f.Path, s)
				}
			}
		}
	}
}

// containsName answers whether text spells name as its own word. The names in
// the data are code identifiers (Approver.Look, FurrowRepository.seal); the
// part after the dot is what has to appear.
func containsName(text, name string) bool {
	want := name
	for i := 0; i < len(name); i++ {
		if name[i] == '.' {
			want = name[i+1:]
		}
	}
	return containsWord(text, want)
}

func containsWord(text, word string) bool {
	for i := 0; i+len(word) <= len(text); i++ {
		if text[i:i+len(word)] != word {
			continue
		}
		before := byte(' ')
		if i > 0 {
			before = text[i-1]
		}
		after := byte(' ')
		if i+len(word) < len(text) {
			after = text[i+len(word)]
		}
		if !isNameByte(before) && !isNameByte(after) {
			return true
		}
	}
	return false
}

func isNameByte(b byte) bool {
	return b == '_' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9'
}

// TestEdgesAndStepsNameRealNodes holds every arrow and every step to the
// boxes: an edge between boxes that do not exist draws nowhere, and a flow
// step on a made-up edge would tell a story about nothing.
func TestEdgesAndStepsNameRealNodes(t *testing.T) {
	ids := map[string]bool{}
	for _, n := range Atlas.Nodes {
		if ids[n.ID] {
			t.Errorf("two nodes are named %s", n.ID)
		}
		ids[n.ID] = true
	}
	edgeIDs := map[string]bool{}
	for _, e := range Atlas.Edges {
		if edgeIDs[e.ID] {
			t.Errorf("two edges are named %s", e.ID)
		}
		edgeIDs[e.ID] = true
		if !ids[e.From] || !ids[e.To] {
			t.Errorf("edge %s joins %s and %s; both have to be nodes", e.ID, e.From, e.To)
		}
	}
	for _, f := range Atlas.Flows {
		if len(f.Steps) == 0 {
			t.Errorf("flow %s has no steps", f.ID)
		}
		for i, s := range f.Steps {
			if !ids[s.From] || !ids[s.To] {
				t.Errorf("flow %s step %d runs from %s to %s; both have to be nodes", f.ID, i+1, s.From, s.To)
			}
			if s.Edge != "" && !edgeIDs[s.Edge] {
				t.Errorf("flow %s step %d travels edge %s, which no overview edge answers to", f.ID, i+1, s.Edge)
			}
		}
	}
}

// TestTheMapCoversItsThreeStories pins what the atlas is for: pairing,
// continuing a chat, and the furrow engine, each a flow a person can step
// through, and every kind of box drawn in its own colour.
func TestTheMapCoversItsThreeStories(t *testing.T) {
	flows := map[string]bool{}
	for _, f := range Atlas.Flows {
		flows[f.ID] = true
	}
	for _, id := range []string{"pairing", "continue", "furrow"} {
		if !flows[id] {
			t.Errorf("the atlas has no flow named %s", id)
		}
	}
	kinds := map[Kind]bool{}
	for _, n := range Atlas.Nodes {
		if n.Summary == "" {
			t.Errorf("node %s says nothing about itself", n.ID)
		}
		if len(n.Files) == 0 {
			t.Errorf("node %s points at no file", n.ID)
		}
		kinds[n.Kind] = true
	}
	for _, k := range []Kind{KindMachine, KindGo, KindService, KindEngine} {
		if !kinds[k] {
			t.Errorf("no node has the kind %s", k)
		}
	}
}
