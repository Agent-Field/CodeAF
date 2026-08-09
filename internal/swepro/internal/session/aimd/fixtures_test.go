package aimd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

// Byte-for-byte replay of testdata/fixtures.json, generated from the real
// TypeScript module by tools/fixtures/gen-aimd.ts. The gate is
// jscompat.Stringify(goResult) == out_json exactly.

type fixture struct {
	Name     string `json:"name"`
	Fn       string `json:"fn"`
	ArgsJSON string `json:"args_json"`
	OutJSON  string `json:"out_json"`
}

// Result shapes mirror the gen script's object literals; field declaration
// order is the literal's key order so Stringify reproduces it.

type constructOut struct {
	Error  *string            `json:"error"`
	Window *jscompat.JSNumber `json:"window"`
}

type traceOut struct {
	Error   *string             `json:"error"`
	Windows []jscompat.JSNumber `json:"windows"`
}

type errorInfoOut struct {
	Name    string `json:"name"`
	Message string `json:"message"`
}

// parseNum decodes an options value: a plain JSON number, JSON null (TS null,
// which `??` treats as absent), or the {"$num":"NaN"} sentinel the generator
// uses because JSON.stringify cannot carry non-finite numbers.
func parseNum(raw json.RawMessage) (*float64, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	if raw[0] == '{' {
		var sentinel struct {
			Num *string `json:"$num"`
		}
		if err := json.Unmarshal(raw, &sentinel); err != nil {
			return nil, err
		}
		if sentinel.Num == nil {
			return nil, fmt.Errorf("object option value without $num: %s", raw)
		}
		var v float64
		switch *sentinel.Num {
		case "NaN":
			v = math.NaN()
		case "Infinity":
			v = math.Inf(1)
		case "-Infinity":
			v = math.Inf(-1)
		default:
			return nil, fmt.Errorf("unknown $num sentinel %q", *sentinel.Num)
		}
		return &v, nil
	}
	var v float64
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// parseOptions turns the first fixture argument into *AimdOptions. A missing
// argument or JSON null both mean "no options argument" (TS undefined), which
// is Go nil.
func parseOptions(args []json.RawMessage) (*AimdOptions, error) {
	if len(args) == 0 || string(args[0]) == "null" {
		return nil, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(args[0], &fields); err != nil {
		return nil, err
	}
	options := &AimdOptions{}
	known := []struct {
		key string
		dst **float64
	}{
		{"initialWindow", &options.InitialWindow},
		{"increase", &options.Increase},
		{"decrease", &options.Decrease},
		{"cap", &options.Cap},
		{"floor", &options.Floor},
	}
	seen := 0
	for _, field := range known {
		raw, ok := fields[field.key]
		if !ok {
			continue
		}
		seen++
		value, err := parseNum(raw)
		if err != nil {
			return nil, err
		}
		*field.dst = value
	}
	if seen != len(fields) {
		return nil, fmt.Errorf("options carry unknown keys: %s", args[0])
	}
	return options, nil
}

func parseObservations(raw json.RawMessage) ([]bool, error) {
	var observations []bool
	if err := json.Unmarshal(raw, &observations); err != nil {
		return nil, err
	}
	return observations, nil
}

func errMessage(err error) *string {
	message := err.Error()
	return &message
}

func windowsOf(controller *AimdController, observations []bool) []jscompat.JSNumber {
	windows := []jscompat.JSNumber{jscompat.JSNumber(controller.Window())}
	for _, mergeOk := range observations {
		controller.Observe(mergeOk)
		windows = append(windows, jscompat.JSNumber(controller.Window()))
	}
	return windows
}

func replay(fx fixture) (any, error) {
	var args []json.RawMessage
	if err := json.Unmarshal([]byte(fx.ArgsJSON), &args); err != nil {
		return nil, err
	}

	switch fx.Fn {
	case "construct", "create":
		options, err := parseOptions(args)
		if err != nil {
			return nil, err
		}
		build := NewAimdController
		if fx.Fn == "create" {
			build = CreateAimdController
		}
		controller, buildErr := build(options)
		if buildErr != nil {
			return constructOut{Error: errMessage(buildErr)}, nil
		}
		window := jscompat.JSNumber(controller.Window())
		return constructOut{Window: &window}, nil

	case "trace", "createTrace":
		if len(args) != 2 {
			return nil, fmt.Errorf("%s expects 2 args, got %d", fx.Fn, len(args))
		}
		options, err := parseOptions(args)
		if err != nil {
			return nil, err
		}
		observations, err := parseObservations(args[1])
		if err != nil {
			return nil, err
		}
		build := NewAimdController
		if fx.Fn == "createTrace" {
			build = CreateAimdController
		}
		controller, buildErr := build(options)
		if buildErr != nil {
			return traceOut{Error: errMessage(buildErr)}, nil
		}
		return traceOut{Windows: windowsOf(controller, observations)}, nil

	case "errorInfo":
		options, err := parseOptions(args)
		if err != nil {
			return nil, err
		}
		_, buildErr := NewAimdController(options)
		if buildErr == nil {
			return (*errorInfoOut)(nil), nil
		}
		rangeErr, ok := buildErr.(*RangeError)
		if !ok {
			return nil, fmt.Errorf("expected *RangeError, got %T", buildErr)
		}
		return &errorInfoOut{Name: rangeErr.Name(), Message: rangeErr.Error()}, nil
	}

	return nil, fmt.Errorf("unknown fn %q", fx.Fn)
}

func loadFixtures(t *testing.T) []fixture {
	t.Helper()
	file, err := os.Open("testdata/fixtures.json")
	if err != nil {
		t.Fatalf("open fixtures: %v", err)
	}
	defer file.Close()

	var fixtures []fixture
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var fx fixture
		if err := json.Unmarshal(line, &fx); err != nil {
			t.Fatalf("decode fixture line: %v", err)
		}
		fixtures = append(fixtures, fx)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan fixtures: %v", err)
	}
	if len(fixtures) < 25 {
		t.Fatalf("expected at least 25 fixture cases, got %d", len(fixtures))
	}
	return fixtures
}

func TestFixtureParity(t *testing.T) {
	for _, fx := range loadFixtures(t) {
		t.Run(fx.Name, func(t *testing.T) {
			got, err := replay(fx)
			if err != nil {
				t.Fatalf("replay: %v", err)
			}
			encoded, err := jscompat.Stringify(got)
			if err != nil {
				t.Fatalf("stringify: %v", err)
			}
			if string(encoded) != fx.OutJSON {
				t.Fatalf("fn=%s args=%s\n  go: %s\n  ts: %s", fx.Fn, fx.ArgsJSON, encoded, fx.OutJSON)
			}
		})
	}
}
