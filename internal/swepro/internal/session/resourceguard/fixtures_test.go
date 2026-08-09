package resourceguard_test

import (
	"bufio"
	"encoding/json"
	"os"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/resourceguard"
)

type fixture struct {
	Name     string `json:"name"`
	Fn       string `json:"fn"`
	ArgsJSON string `json:"args_json"`
	OutJSON  string `json:"out_json"`
}

type envSpec struct {
	Present bool   `json:"present"`
	Raw     string `json:"raw"`
}

func loadFixtures(t *testing.T) []fixture {
	t.Helper()
	f, err := os.Open("testdata/fixtures.json")
	if err != nil {
		t.Fatalf("open fixtures: %v", err)
	}
	defer f.Close()

	var fixtures []fixture
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var fx fixture
		if err := json.Unmarshal(scanner.Bytes(), &fx); err != nil {
			t.Fatalf("decode fixture: %v", err)
		}
		fixtures = append(fixtures, fx)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan fixtures: %v", err)
	}
	return fixtures
}

func TestDiskFloorFixtureParity(t *testing.T) {
	fixtures := loadFixtures(t)
	if len(fixtures) < 20 {
		t.Fatalf("expected broad fixture corpus, got %d", len(fixtures))
	}
	for _, fx := range fixtures {
		t.Run(fx.Name, func(t *testing.T) {
			var args []envSpec
			if err := json.Unmarshal([]byte(fx.ArgsJSON), &args); err != nil {
				t.Fatalf("decode args: %v", err)
			}
			before, hadBefore := os.LookupEnv("CODEAF_DISK_FLOOR_GB")
			t.Cleanup(func() {
				if hadBefore {
					_ = os.Setenv("CODEAF_DISK_FLOOR_GB", before)
				} else {
					_ = os.Unsetenv("CODEAF_DISK_FLOOR_GB")
				}
			})
			if args[0].Present {
				if err := os.Setenv("CODEAF_DISK_FLOOR_GB", args[0].Raw); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Unsetenv("CODEAF_DISK_FLOOR_GB"); err != nil {
				t.Fatal(err)
			}

			got, err := jscompat.Stringify(jscompat.JSNumber(resourceguard.DiskFloorGB()))
			if err != nil {
				t.Fatalf("stringify: %v", err)
			}
			if string(got) != fx.OutJSON {
				t.Errorf("args=%s\n got: %s\nwant: %s", fx.ArgsJSON, got, fx.OutJSON)
			}
		})
	}
}
