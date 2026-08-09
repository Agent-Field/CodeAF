package isolation_test

import (
	"bufio"
	"encoding/json"
	"math"
	"os"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
	"github.com/Agent-Field/swe-pro-go/internal/session/isolation"
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
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if len(scanner.Bytes()) == 0 {
			continue
		}
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

type jsNum float64

func (n *jsNum) UnmarshalJSON(data []byte) error {
	switch string(data) {
	case `"@@num:NaN"`:
		*n = jsNum(math.NaN())
		return nil
	case `"@@num:Infinity"`:
		*n = jsNum(math.Inf(1))
		return nil
	case `"@@num:-Infinity"`:
		*n = jsNum(math.Inf(-1))
		return nil
	case `"@@num:-0"`:
		*n = jsNum(math.Copysign(0, -1))
		return nil
	}
	var value float64
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	*n = jsNum(value)
	return nil
}

type fixtureReading struct {
	Resource   string `json:"resource"`
	OK         bool   `json:"ok"`
	HeadroomGB jsNum  `json:"headroomGB"`
	FloorGB    jsNum  `json:"floorGB"`
}

func (r fixtureReading) value() isolation.EnvelopeReading {
	return isolation.EnvelopeReading{
		Resource:   r.Resource,
		OK:         r.OK,
		HeadroomGB: jscompat.JSNumber(r.HeadroomGB),
		FloorGB:    jscompat.JSNumber(r.FloorGB),
	}
}

func replay(t *testing.T, fx fixture) any {
	t.Helper()
	var args []json.RawMessage
	if err := json.Unmarshal([]byte(fx.ArgsJSON), &args); err != nil {
		t.Fatalf("%s: decode args: %v", fx.Name, err)
	}

	switch fx.Fn {
	case "resolveIsolationBackend":
		var env map[string]string
		if err := json.Unmarshal(args[0], &env); err != nil {
			t.Fatalf("%s: decode env: %v", fx.Name, err)
		}
		return isolation.ResolveIsolationBackend(env)

	case "decideIsolation":
		var opts isolation.IsolationOptions
		if err := json.Unmarshal(args[0], &opts); err != nil {
			t.Fatalf("%s: decode options: %v", fx.Name, err)
		}
		return isolation.DecideIsolation(opts)

	case "readingToStatus":
		var reading fixtureReading
		if err := json.Unmarshal(args[0], &reading); err != nil {
			t.Fatalf("%s: decode reading: %v", fx.Name, err)
		}
		return isolation.ReadingToStatus(reading.value())

	case "decideDispatchEnvelope":
		var raw []fixtureReading
		if err := json.Unmarshal(args[0], &raw); err != nil {
			t.Fatalf("%s: decode readings: %v", fx.Name, err)
		}
		readings := make([]isolation.EnvelopeReading, len(raw))
		for i := range raw {
			readings[i] = raw[i].value()
		}
		return isolation.DecideDispatchEnvelope(readings)

	default:
		t.Fatalf("unknown fixture function %q", fx.Fn)
		return nil
	}
}

func TestFixtureParity(t *testing.T) {
	fixtures := loadFixtures(t)
	if len(fixtures) < 60 {
		t.Fatalf("expected broad fixture corpus, got %d", len(fixtures))
	}
	for _, fx := range fixtures {
		t.Run(fx.Fn+"/"+fx.Name, func(t *testing.T) {
			got, err := jscompat.Stringify(replay(t, fx))
			if err != nil {
				t.Fatalf("stringify: %v", err)
			}
			if string(got) != fx.OutJSON {
				t.Errorf("args=%s\n got: %s\nwant: %s", fx.ArgsJSON, got, fx.OutJSON)
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
		"resolveIsolationBackend",
		"decideIsolation",
		"readingToStatus",
		"decideDispatchEnvelope",
	} {
		if seen[fn] == 0 {
			t.Errorf("no fixture cases for %s", fn)
		}
	}
}
