package atlas

import (
	"os"
	"path/filepath"
	"testing"
)

// The repo root, two directories up from this package: every Files[].Path in
// the data is relative to it.
const repoRoot = "../.."

// TestEveryFilePathExists holds every registered map to the tree: a file a
// map points at must exist, or the map is lying about where a part lives.
func TestEveryFilePathExists(t *testing.T) {
	for _, mp := range Maps {
		for _, n := range mp.Nodes {
			for _, f := range n.Files {
				if _, err := os.Stat(filepath.Join(repoRoot, f.Path)); err != nil {
					t.Errorf("%s: node %s names the file %s, which does not exist", mp.Name, n.ID, f.Path)
				}
				for _, s := range f.Symbols {
					raw, readErr := os.ReadFile(filepath.Join(repoRoot, f.Path))
					if readErr != nil {
						t.Errorf("%s: node %s names the file %s: %v", mp.Name, n.ID, f.Path, readErr)
						continue
					}
					if !containsName(string(raw), s) {
						t.Errorf("%s: node %s says %s names %s, which the file never spells", mp.Name, n.ID, f.Path, s)
					}
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

// TestEdgesAndStepsNameRealNodes holds every arrow and every step of every
// map to the boxes: an edge between boxes that do not exist draws nowhere,
// and a flow step on a made-up edge would tell a story about nothing.
func TestEdgesAndStepsNameRealNodes(t *testing.T) {
	for _, mp := range Maps {
		ids := map[string]bool{}
		for _, n := range mp.Nodes {
			if ids[n.ID] {
				t.Errorf("%s: two nodes are named %s", mp.Name, n.ID)
			}
			ids[n.ID] = true
		}
		edgeIDs := map[string]bool{}
		for _, e := range mp.Edges {
			if edgeIDs[e.ID] {
				t.Errorf("%s: two edges are named %s", mp.Name, e.ID)
			}
			edgeIDs[e.ID] = true
			if !ids[e.From] || !ids[e.To] {
				t.Errorf("%s: edge %s joins %s and %s; both have to be nodes", mp.Name, e.ID, e.From, e.To)
			}
		}
		for _, f := range mp.Flows {
			if len(f.Steps) == 0 {
				t.Errorf("%s: flow %s has no steps", mp.Name, f.ID)
			}
			for i, s := range f.Steps {
				if !ids[s.From] || !ids[s.To] {
					t.Errorf("%s: flow %s step %d runs from %s to %s; both have to be nodes", mp.Name, f.ID, i+1, s.From, s.To)
				}
				if s.Edge != "" && !edgeIDs[s.Edge] {
					t.Errorf("%s: flow %s step %d travels edge %s, which no overview edge answers to", mp.Name, f.ID, i+1, s.Edge)
				}
			}
		}
	}
}

// TestMapNamesAreUnique keeps the registry honest: a person types a map's
// name at `codeaf atlas`, so two maps answering to one name would leave the
// second one unreachable.
func TestMapNamesAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, mp := range Maps {
		if mp.Name == "" {
			t.Error("a registered map has no name")
		}
		if seen[mp.Name] {
			t.Errorf("two maps are named %s", mp.Name)
		}
		seen[mp.Name] = true
	}
}

// TestRegistryIsEmptyWithoutMaps keeps the entry points safe: the picker and
// the CLI both read Maps, and an empty registry would render nothing at all.
func TestRegistryIsEmptyWithoutMaps(t *testing.T) {
	if len(Maps) == 0 {
		t.Fatal("no map is registered")
	}
	for _, mp := range Maps {
		if mp.Title == "" || mp.Description == "" {
			t.Errorf("map %s has no title or no one-line description for the picker", mp.Name)
		}
	}
}

// TestTheMapCoversItsThreeStories pins what the atlas is for: pairing,
// continuing a chat, and the furrow engine, each a flow a person can step
// through, and every kind of box drawn in its own colour.
func TestTheMapCoversItsThreeStories(t *testing.T) {
	pairing, ok := ByName("pairing")
	if !ok {
		t.Fatal("no map is named pairing")
	}
	flows := map[string]bool{}
	for _, f := range pairing.Flows {
		flows[f.ID] = true
	}
	for _, id := range []string{"pairing", "continue", "furrow"} {
		if !flows[id] {
			t.Errorf("the pairing map has no flow named %s", id)
		}
	}
	kinds := map[Kind]bool{}
	for _, n := range pairing.Nodes {
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
