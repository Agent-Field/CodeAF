package basecontractcheck

import (
	"bufio"
	"encoding/json"
	"os"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

type fixture struct {
	Name     string `json:"name"`
	Fn       string `json:"fn"`
	ArgsJSON string `json:"args_json"`
	OutJSON  string `json:"out_json"`
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
		fx := fx
		t.Run(fx.Name, func(t *testing.T) {
			if fx.Fn != "planContractCopies" {
				t.Fatalf("unknown fn %q", fx.Fn)
			}
			var args []json.RawMessage
			if err := json.Unmarshal([]byte(fx.ArgsJSON), &args); err != nil || len(args) != 1 {
				t.Fatalf("decode one argument: %v", err)
			}
			var input PlanContractCopiesInput
			if err := json.Unmarshal(args[0], &input); err != nil {
				t.Fatalf("decode input: %v", err)
			}
			encoded, err := jscompat.Stringify(PlanContractCopies(input))
			if err != nil {
				t.Fatalf("stringify: %v", err)
			}
			if string(encoded) != fx.OutJSON {
				t.Fatalf("args=%s\n got: %s\nwant: %s", fx.ArgsJSON, encoded, fx.OutJSON)
			}
		})
	}
}
