package tool

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

func TestRepoOverviewFixtureParity(t *testing.T) {
	fixtures := loadToolFixtures(t, "testdata/repo_overview_fixtures.json")
	if len(fixtures) < 30 {
		t.Fatalf("fixture count = %d, want at least 30", len(fixtures))
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			var args []json.RawMessage
			if err := json.Unmarshal([]byte(fixture.ArgsJSON), &args); err != nil {
				t.Fatal(err)
			}
			var got any
			switch fixture.Fn {
			case "packageManager", "ecosystems", "commonEntrypoints":
				var files []string
				_ = json.Unmarshal(args[0], &files)
				switch fixture.Fn {
				case "packageManager":
					value := RepoPackageManager(files)
					if value == "" {
						got = nil
					} else {
						got = value
					}
				case "ecosystems":
					got = RepoEcosystems(files)
				case "commonEntrypoints":
					got = RepoCommonEntrypoints(files)
				}
			case "packageEntrypoints":
				var source string
				_ = json.Unmarshal(args[0], &source)
				got = packageEntrypoints([]byte(source))
			case "resolveDepth":
				var value *float64
				_ = json.Unmarshal(args[0], &value)
				got = resolveOverviewDepth(value)
			case "structure":
				var tree map[string]any
				var depth int
				_ = json.Unmarshal(args[0], &tree)
				_ = json.Unmarshal(args[1], &depth)
				root := t.TempDir()
				writeOverviewTree(t, root, tree)
				got = BuildRepoStructure(root, depth)
			case "assemble":
				var metadata RepoOverviewMetadata
				var structure RepoStructure
				if err := json.Unmarshal(args[0], &metadata); err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(args[1], &structure); err != nil {
					t.Fatal(err)
				}
				got = AssembleRepoOverview(metadata, structure)
			default:
				t.Fatalf("unknown fixture function %q", fixture.Fn)
			}
			data, err := jscompat.Stringify(got)
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != fixture.OutJSON {
				t.Errorf("args=%s\n got: %s\nwant: %s", fixture.ArgsJSON, data, fixture.OutJSON)
			}
		})
	}
}

func writeOverviewTree(t *testing.T, root string, tree map[string]any) {
	t.Helper()
	for name, value := range tree {
		path := filepath.Join(root, name)
		if kind, ok := value.(string); ok && kind == "file" {
			if err := os.WriteFile(path, nil, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		child, _ := value.(map[string]any)
		if err := os.Mkdir(path, 0o755); err != nil {
			t.Fatal(err)
		}
		writeOverviewTree(t, path, child)
	}
}
