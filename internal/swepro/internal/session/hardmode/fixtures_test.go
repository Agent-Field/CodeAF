package hardmode

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

type scenarioInput struct {
	Hard   *string `json:"hard"`
	Reason *string `json:"reason"`
	Ops    []struct {
		Op     string `json:"op"`
		Reason string `json:"reason"`
	} `json:"ops"`
}

type envSnapshot struct {
	Hard   *string `json:"hard"`
	Reason *string `json:"reason"`
}

type scenarioOutput struct {
	Results []any       `json:"results"`
	Env     envSnapshot `json:"env"`
	Caps    struct {
		Default int `json:"default"`
		Hard    int `json:"hard"`
	} `json:"caps"`
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

func setOptionalEnv(t *testing.T, key string, value *string) {
	t.Helper()
	if value == nil {
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("unset %s: %v", key, err)
		}
		return
	}
	if err := os.Setenv(key, *value); err != nil {
		t.Fatalf("set %s: %v", key, err)
	}
}

func optionalEnv(key string) *string {
	value, ok := os.LookupEnv(key)
	if !ok {
		return nil
	}
	return &value
}

func TestFixtureParity(t *testing.T) {
	fixtures := loadFixtures(t)
	if len(fixtures) < 25 {
		t.Fatalf("expected at least 25 fixtures, got %d", len(fixtures))
	}
	oldHard, hadHard := os.LookupEnv(hardEnv)
	oldReason, hadReason := os.LookupEnv(escalationReasonEnv)
	t.Cleanup(func() {
		if hadHard {
			_ = os.Setenv(hardEnv, oldHard)
		} else {
			_ = os.Unsetenv(hardEnv)
		}
		if hadReason {
			_ = os.Setenv(escalationReasonEnv, oldReason)
		} else {
			_ = os.Unsetenv(escalationReasonEnv)
		}
	})

	for _, fx := range fixtures {
		fx := fx
		t.Run(fx.Name, func(t *testing.T) {
			if fx.Fn != "scenario" {
				t.Fatalf("unknown fn %q", fx.Fn)
			}
			var args []json.RawMessage
			if err := json.Unmarshal([]byte(fx.ArgsJSON), &args); err != nil || len(args) != 1 {
				t.Fatalf("decode one argument: %v", err)
			}
			var input scenarioInput
			if err := json.Unmarshal(args[0], &input); err != nil {
				t.Fatalf("decode scenario: %v", err)
			}
			setOptionalEnv(t, hardEnv, input.Hard)
			setOptionalEnv(t, escalationReasonEnv, input.Reason)

			results := make([]any, 0, len(input.Ops))
			for _, op := range input.Ops {
				switch op.Op {
				case "is":
					results = append(results, IsHardMode())
				case "enable":
					EnableHardMode(op.Reason)
					results = append(results, nil)
				default:
					t.Fatalf("unknown op %q", op.Op)
				}
			}
			output := scenarioOutput{
				Results: results,
				Env: envSnapshot{
					Hard: optionalEnv(hardEnv), Reason: optionalEnv(escalationReasonEnv),
				},
			}
			output.Caps.Default = DefaultAuditFixCycles
			output.Caps.Hard = HardAuditFixCycles
			encoded, err := jscompat.Stringify(output)
			if err != nil {
				t.Fatalf("stringify: %v", err)
			}
			if string(encoded) != fx.OutJSON {
				t.Fatalf("args=%s\n got: %s\nwant: %s", fx.ArgsJSON, encoded, fx.OutJSON)
			}
		})
	}
}
