package format

import (
	"bufio"
	"encoding/json"
	"os"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

type formatFixture struct {
	Name     string `json:"name"`
	Fn       string `json:"fn"`
	ArgsJSON string `json:"args_json"`
	OutJSON  string `json:"out_json"`
}

func loadFormatFixtures(t *testing.T) []formatFixture {
	t.Helper()
	file, err := os.Open("testdata/fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	var fixtures []formatFixture
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var fixture formatFixture
		if err := json.Unmarshal(scanner.Bytes(), &fixture); err != nil {
			t.Fatal(err)
		}
		fixtures = append(fixtures, fixture)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return fixtures
}

func TestFormatFixtureParity(t *testing.T) {
	fixtures := loadFormatFixtures(t)
	if len(fixtures) < 30 {
		t.Fatalf("fixture count = %d", len(fixtures))
	}

	byKey := map[string]Info{}
	for _, item := range Builtins(Dependencies{}) {
		byKey[item.Key] = item
	}
	seen := map[string]int{}
	for _, fixture := range fixtures {
		seen[fixture.Fn]++
		t.Run(fixture.Fn+"/"+fixture.Name, func(t *testing.T) {
			var args []json.RawMessage
			if err := json.Unmarshal([]byte(fixture.ArgsJSON), &args); err != nil {
				t.Fatal(err)
			}
			var out any
			switch fixture.Fn {
			case "metadata":
				var key string
				if err := json.Unmarshal(args[0], &key); err != nil {
					t.Fatal(err)
				}
				item, ok := byKey[key]
				if !ok {
					t.Fatalf("unknown formatter export %q", key)
				}
				out = struct {
					Name        string            `json:"name"`
					Environment map[string]string `json:"environment,omitempty"`
					Extensions  []string          `json:"extensions"`
				}{
					Name: item.Name, Environment: item.Environment, Extensions: item.Extensions,
				}
			case "statusSchema":
				out = acceptsStatus(args[0])
			default:
				t.Fatalf("unknown fixture function %q", fixture.Fn)
			}
			encoded, err := jscompat.Stringify(out)
			if err != nil {
				t.Fatal(err)
			}
			if string(encoded) != fixture.OutJSON {
				t.Fatalf("args=%s\n got %s\nwant %s", fixture.ArgsJSON, encoded, fixture.OutJSON)
			}
		})
	}
	for _, function := range []string{"metadata", "statusSchema"} {
		if seen[function] == 0 {
			t.Errorf("no fixtures for %s", function)
		}
	}
}

func acceptsStatus(raw json.RawMessage) bool {
	if string(raw) == "null" {
		return false
	}
	var value map[string]json.RawMessage
	if err := json.Unmarshal(raw, &value); err != nil {
		return false
	}
	var name string
	if err := json.Unmarshal(value["name"], &name); err != nil {
		return false
	}
	var extensions []json.RawMessage
	if err := json.Unmarshal(value["extensions"], &extensions); err != nil {
		return false
	}
	for _, extension := range extensions {
		var value string
		if err := json.Unmarshal(extension, &value); err != nil {
			return false
		}
	}
	var enabled bool
	return json.Unmarshal(value["enabled"], &enabled) == nil
}
