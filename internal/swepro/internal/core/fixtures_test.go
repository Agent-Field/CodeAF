package core

import (
	"bufio"
	"encoding/json"
	"os"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

type coreFixture struct {
	Name     string `json:"name"`
	Fn       string `json:"fn"`
	ArgsJSON string `json:"args_json"`
	OutJSON  string `json:"out_json"`
}

func loadCoreFixtures(t *testing.T) []coreFixture {
	t.Helper()
	file, err := os.Open("testdata/fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	out := []coreFixture{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var fixture coreFixture
		if err := json.Unmarshal(scanner.Bytes(), &fixture); err != nil {
			t.Fatal(err)
		}
		out = append(out, fixture)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestCoreFixtureParity(t *testing.T) {
	fixtures := loadCoreFixtures(t)
	if len(fixtures) < 50 {
		t.Fatalf("fixture count = %d", len(fixtures))
	}
	seen := map[string]int{}
	for _, fixture := range fixtures {
		seen[fixture.Fn]++
		t.Run(fixture.Fn+"/"+fixture.Name, func(t *testing.T) {
			var args []json.RawMessage
			if err := json.Unmarshal([]byte(fixture.ArgsJSON), &args); err != nil {
				t.Fatal(err)
			}
			stringArg := func(index int) string {
				var value string
				if err := json.Unmarshal(args[index], &value); err != nil {
					t.Fatal(err)
				}
				return value
			}
			var out any
			switch fixture.Fn {
			case "mimeType":
				out = MimeType(stringArg(0))
			case "normalizePath":
				out = NormalizePath(stringArg(0))
			case "normalizePathPattern":
				out = NormalizePathPattern(stringArg(0))
			case "resolve":
				value, err := Resolve(stringArg(0))
				if err != nil {
					t.Fatal(err)
				}
				out = value
			case "windowsPath":
				out = WindowsPath(stringArg(0))
			case "overlaps":
				out = Overlaps(stringArg(0), stringArg(1))
			case "contains":
				out = Contains(stringArg(0), stringArg(1))
			case "sanitize":
				out = Sanitize(stringArg(0))
			default:
				t.Fatalf("unknown fn %q", fixture.Fn)
			}
			bytes, err := jscompat.Stringify(out)
			if err != nil {
				t.Fatal(err)
			}
			if string(bytes) != fixture.OutJSON {
				t.Fatalf("args=%s\n got %s\nwant %s", fixture.ArgsJSON, bytes, fixture.OutJSON)
			}
		})
	}
	for _, fn := range []string{
		"mimeType", "normalizePath", "normalizePathPattern", "resolve",
		"windowsPath", "overlaps", "contains", "sanitize",
	} {
		if seen[fn] == 0 {
			t.Errorf("no fixtures for %s", fn)
		}
	}
}
