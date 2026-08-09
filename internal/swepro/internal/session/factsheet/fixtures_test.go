package factsheet

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

type fixture struct {
	Name     string `json:"name"`
	Fn       string `json:"fn"`
	ArgsJSON string `json:"args_json"`
	OutJSON  string `json:"out_json"`
}

type fixtureScenario struct {
	RootDir string              `json:"rootDir"`
	Hubs    []string            `json:"hubs"`
	Files   map[string]string   `json:"files"`
	Dirs    map[string][]string `json:"dirs"`
}

type mapFS struct {
	files map[string]string
	dirs  map[string][]string
}

func normalizeFSPath(path string) string {
	return strings.ReplaceAll(path, `\`, "/")
}

func newMapFS(files map[string]string, dirs map[string][]string) *mapFS {
	normalizedFiles := map[string]string{}
	for path, content := range files {
		normalizedFiles[normalizeFSPath(path)] = content
	}
	normalizedDirs := map[string][]string{}
	for path, entries := range dirs {
		normalizedDirs[normalizeFSPath(path)] = append([]string{}, entries...)
	}
	return &mapFS{files: normalizedFiles, dirs: normalizedDirs}
}

func (m *mapFS) ReadFile(path string) (string, bool) {
	value, ok := m.files[normalizeFSPath(path)]
	return value, ok
}

func (m *mapFS) Exists(path string) bool {
	key := normalizeFSPath(path)
	if _, ok := m.files[key]; ok {
		return true
	}
	_, ok := m.dirs[key]
	return ok
}

func (m *mapFS) ListDir(path string) []string {
	entries := m.dirs[normalizeFSPath(path)]
	return append([]string{}, entries...)
}

func loadFixtures(t *testing.T) []fixture {
	t.Helper()
	f, err := os.Open("testdata/fixtures.json")
	if err != nil {
		t.Fatalf("open fixtures: %v", err)
	}
	defer f.Close()

	out := []fixture{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 1<<23)
	for sc.Scan() {
		if len(sc.Bytes()) == 0 {
			continue
		}
		var fx fixture
		if err := json.Unmarshal(sc.Bytes(), &fx); err != nil {
			t.Fatalf("decode fixture: %v", err)
		}
		out = append(out, fx)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan fixtures: %v", err)
	}
	return out
}

func TestFixtureParity(t *testing.T) {
	fixtures := loadFixtures(t)
	if len(fixtures) < 30 {
		t.Fatalf("expected at least 30 fixtures, got %d", len(fixtures))
	}
	for _, fx := range fixtures {
		t.Run(fx.Name, func(t *testing.T) {
			if fx.Fn != "generateFactSheet" {
				t.Fatalf("unknown fixture function %q", fx.Fn)
			}
			var args []json.RawMessage
			if err := json.Unmarshal([]byte(fx.ArgsJSON), &args); err != nil {
				t.Fatalf("decode args: %v", err)
			}
			if len(args) != 1 {
				t.Fatalf("generateFactSheet wants 1 arg, got %d", len(args))
			}
			var scenario fixtureScenario
			if err := json.Unmarshal(args[0], &scenario); err != nil {
				t.Fatalf("decode scenario: %v", err)
			}
			result := GenerateFactSheet(GenerateFactSheetOpts{
				RootDir: scenario.RootDir,
				Hubs:    scenario.Hubs,
				FS:      newMapFS(scenario.Files, scenario.Dirs),
			})
			got, err := jscompat.Stringify(result)
			if err != nil {
				t.Fatalf("stringify: %v", err)
			}
			if string(got) != fx.OutJSON {
				t.Errorf("args=%s\n got: %s\nwant: %s", fx.ArgsJSON, got, fx.OutJSON)
			}
		})
	}
}
