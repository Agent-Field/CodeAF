package tampercheck

import (
	"bufio"
	"encoding/json"
	"os"
	"regexp"
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
	var fixtures []fixture
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 1<<22)
	for sc.Scan() {
		var fx fixture
		if err := json.Unmarshal(sc.Bytes(), &fx); err != nil {
			t.Fatalf("decode fixture: %v", err)
		}
		fixtures = append(fixtures, fx)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan fixtures: %v", err)
	}
	return fixtures
}

func argsOf(t *testing.T, raw string) []json.RawMessage {
	t.Helper()
	var args []json.RawMessage
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		t.Fatalf("decode args: %v", err)
	}
	return args
}

func decode[T any](t *testing.T, raw json.RawMessage) T {
	t.Helper()
	var value T
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("decode argument: %v", err)
	}
	return value
}

func callFixture(t *testing.T, fx fixture) any {
	t.Helper()
	args := argsOf(t, fx.ArgsJSON)
	switch fx.Fn {
	case "taskTargetsConfig":
		return TaskTargetsConfig(decode[string](t, args[0]), decode[string](t, args[1]))
	case "hunkTouchesVerification":
		var guard *regexp.Regexp
		switch decode[string](t, args[1]) {
		case "py":
			guard = PyVerificationHunkSection
		case "pkg":
			guard = PkgVerificationHunkSection
		default:
			t.Fatal("unknown guard")
		}
		return HunkTouchesVerification(decode[string](t, args[0]), guard)
	case "checkTamper":
		return CheckTamper(decode[TamperInput](t, args[0]))
	case "tamperBlockerDetails":
		return TamperBlockerDetails(decode[TamperVerdict](t, args[0]))
	default:
		t.Fatalf("unknown fixture function %q", fx.Fn)
		return nil
	}
}

func TestFixtureParity(t *testing.T) {
	fixtures := loadFixtures(t)
	if len(fixtures) < 50 {
		t.Fatalf("expected at least 50 fixtures, got %d", len(fixtures))
	}
	for _, fx := range fixtures {
		t.Run(fx.Fn+"/"+fx.Name, func(t *testing.T) {
			got, err := jscompat.Stringify(callFixture(t, fx))
			if err != nil {
				t.Fatalf("stringify: %v", err)
			}
			if string(got) != fx.OutJSON {
				t.Fatalf("args=%s\n got: %s\nwant: %s", fx.ArgsJSON, got, fx.OutJSON)
			}
		})
	}
}

func TestFixtureCoverage(t *testing.T) {
	seen := map[string]int{}
	for _, fx := range loadFixtures(t) {
		seen[fx.Fn]++
	}
	for _, fn := range []string{
		"taskTargetsConfig", "hunkTouchesVerification", "checkTamper", "tamperBlockerDetails",
	} {
		if seen[fn] == 0 {
			t.Errorf("no fixtures for %s", fn)
		}
	}
}
