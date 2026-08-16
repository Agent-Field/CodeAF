package patch

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

type outcome struct {
	OK    bool   `json:"ok"`
	Value any    `json:"value,omitempty"`
	Error string `json:"error,omitempty"`
}

type normalizedError struct {
	Error string `json:"error"`
}

func loadFixtures(t *testing.T) []fixture {
	t.Helper()
	file, err := os.Open("testdata/fixtures.json")
	if err != nil {
		t.Fatalf("open fixtures: %v", err)
	}
	defer file.Close()
	out := []fixture{}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		var item fixture
		if err := json.Unmarshal(scanner.Bytes(), &item); err != nil {
			t.Fatalf("decode fixture: %v", err)
		}
		out = append(out, item)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func callFixture(t *testing.T, fixture fixture) outcome {
	t.Helper()
	var args []json.RawMessage
	if err := json.Unmarshal([]byte(fixture.ArgsJSON), &args); err != nil {
		t.Fatalf("decode args: %v", err)
	}
	switch fixture.Fn {
	case "parsePatch":
		var patchText string
		if err := json.Unmarshal(args[0], &patchText); err != nil {
			t.Fatal(err)
		}
		value, err := ParsePatch(patchText)
		if err != nil {
			return outcome{OK: false, Error: err.Error()}
		}
		return outcome{OK: true, Value: value}
	case "maybeParseApplyPatch":
		var argv []string
		if err := json.Unmarshal(args[0], &argv); err != nil {
			t.Fatal(err)
		}
		result := MaybeParseApplyPatch(argv)
		switch result.Type {
		case MaybeBody:
			return outcome{OK: true, Value: struct {
				Type string          `json:"type"`
				Args *ApplyPatchArgs `json:"args"`
			}{result.Type, result.Args}}
		case MaybePatchParseError:
			return outcome{OK: true, Value: struct {
				Type  string          `json:"type"`
				Error normalizedError `json:"error"`
			}{result.Type, normalizedError{result.Err.Error()}}}
		default:
			return outcome{OK: true, Value: struct {
				Type string `json:"type"`
			}{result.Type}}
		}
	case "deriveNewContentsFromChunks":
		var source string
		var chunks []UpdateFileChunk
		if err := json.Unmarshal(args[0], &source); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(args[1], &chunks); err != nil {
			t.Fatal(err)
		}
		const path = "/tmp/swe-pro-go-patch-fixture.txt"
		if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Remove(path) })
		value, err := DeriveNewContentsFromChunks(path, chunks)
		if err != nil {
			return outcome{OK: false, Error: err.Error()}
		}
		return outcome{OK: true, Value: value}
	default:
		t.Fatalf("unknown fn %q", fixture.Fn)
		return outcome{}
	}
}

func TestFixtureParity(t *testing.T) {
	fixtures := loadFixtures(t)
	if len(fixtures) < 29 {
		t.Fatalf("expected at least 29 fixtures, got %d", len(fixtures))
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			got, err := jscompat.Stringify(callFixture(t, fixture))
			if err != nil {
				t.Fatalf("stringify: %v", err)
			}
			if string(got) != fixture.OutJSON {
				t.Fatalf("fn=%s args=%s\n got %s\nwant %s", fixture.Fn, fixture.ArgsJSON, got, fixture.OutJSON)
			}
		})
	}
}

func TestFixtureCoverage(t *testing.T) {
	seen := map[string]bool{}
	for _, fixture := range loadFixtures(t) {
		seen[fixture.Fn] = true
	}
	for _, fn := range []string{"parsePatch", "maybeParseApplyPatch", "deriveNewContentsFromChunks"} {
		if !seen[fn] {
			t.Errorf("missing fixtures for %s", fn)
		}
	}
}
