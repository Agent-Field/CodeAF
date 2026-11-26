// Package extensions evaluates app-authored computations with explicit inputs
// and no host capabilities.
package extensions

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"

	"github.com/eliben/watgo"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

const (
	MaxSource     = 8 << 10
	MaxTimeoutMS  = 1000
	MaxPages      = 16
	MaxDateValues = 366
)

var identifier = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,47}$`)

// Origin records the immutable app version that contributed a Kit revision.
type Origin struct {
	App     string `json:"app"`
	Version int    `json:"version"`
	Digest  string `json:"digest"`
}

type Input struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// Step declares the complete interface of a pure WASM function. Effects is
// required and empty so extensions cannot enter the broker through a host call.
type Step struct {
	Version     int      `json:"version"`
	Summary     string   `json:"summary"`
	Reason      string   `json:"reason"`
	Inputs      []Input  `json:"inputs"`
	Output      string   `json:"output"`
	Effects     []string `json:"effects"`
	TimeoutMS   int      `json:"timeout_ms"`
	MemoryPages uint32   `json:"memory_pages"`
	Source      string   `json:"source"`
	Origin      *Origin  `json:"origin,omitempty"`
}

func Parse(raw []byte) (Step, error) {
	if len(raw) > 2*MaxSource {
		return Step{}, errors.New("The custom step definition is too large.")
	}
	var s Step
	if !json.Valid(raw) {
		return s, errors.New("The custom step needs a JSON manifest.")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		return s, err
	}
	return s, s.Check()
}

func (s Step) Digest() string {
	s.Origin = nil
	raw, _ := json.Marshal(s)
	return fmt.Sprintf("%x", sha256.Sum256(raw))
}

func (s Step) Check() error {
	ctx := context.Background()
	r, _, err := s.compile(ctx)
	if r != nil {
		_ = r.Close(ctx)
	}
	return err
}

func (s Step) compile(ctx context.Context) (wazero.Runtime, wazero.CompiledModule, error) {
	if s.Version < 1 || s.Summary == "" || s.Reason == "" || len(s.Inputs) > 12 ||
		len(s.Source) == 0 || len(s.Source) > MaxSource || s.TimeoutMS < 1 || s.TimeoutMS > MaxTimeoutMS ||
		s.MemoryPages < 1 || s.MemoryPages > MaxPages || s.Effects == nil || len(s.Effects) != 0 {
		return nil, nil, errors.New("Give the custom step a version, summary, reason, bounded source, timeout and memory limit, and declare effects: [].")
	}
	if s.Origin != nil && (s.Origin.App == "" || s.Origin.Version < 1 || s.Origin.Digest != s.Digest()) {
		return nil, nil, errors.New("This step differs from the version imported from your Kit.")
	}
	if s.Output != "integer" && s.Output != "bool" {
		return nil, nil, errors.New("A custom step returns integer or bool.")
	}
	seen := map[string]bool{}
	arity, lists := 0, 0
	for _, input := range s.Inputs {
		if !identifier.MatchString(input.Name) || seen[input.Name] {
			return nil, nil, errors.New("Custom step inputs need unique IDs.")
		}
		seen[input.Name] = true
		arity++
		switch input.Type {
		case "date", "integer", "bool":
		case "dates":
			arity++
			lists++
		default:
			return nil, nil, fmt.Errorf("Unknown custom step input type %q.", input.Type)
		}
	}

	module, err := watgo.ParseAndValidateWAT([]byte(s.Source))
	if err != nil {
		return nil, nil, fmt.Errorf("Custom step source: %w", err)
	}
	if len(module.Imports) != 0 || len(module.Tables) != 0 || module.StartFuncIndex != nil {
		return nil, nil, errors.New("A custom step cannot import anything, declare tables or run at load time.")
	}
	wasm, err := watgo.EncodeWASM(module)
	if err != nil {
		return nil, nil, err
	}
	cfg := wazero.NewRuntimeConfigInterpreter().WithCoreFeatures(api.CoreFeaturesV1).
		WithMemoryLimitPages(s.MemoryPages).WithCloseOnContextDone(true)
	r := wazero.NewRuntimeWithConfig(ctx, cfg)
	compiled, err := r.CompileModule(ctx, wasm)
	if err == nil {
		run := compiled.ExportedFunctions()["run"]
		switch {
		case run == nil || len(run.ParamTypes()) != arity || len(run.ResultTypes()) != 1 || run.ResultTypes()[0] != api.ValueTypeI64:
			err = errors.New("Export run with i64 arguments and one i64 result.")
		case lists != 0 && compiled.ExportedMemories()["memory"] == nil:
			err = errors.New("Export memory for date-list arguments.")
		default:
			for _, t := range run.ParamTypes() {
				if t != api.ValueTypeI64 {
					err = errors.New("Every argument to run is i64.")
					break
				}
			}
		}
	}
	if err != nil {
		_ = r.Close(ctx)
		return nil, nil, err
	}
	return r, compiled, nil
}

// Run starts an import-free, fresh instance. Only typed scalar arguments and
// UTC day numbers copied to guest memory cross into it.
func (s Step) Run(ctx context.Context, args map[string]any) (any, error) {
	if len(args) != len(s.Inputs) {
		return nil, errors.New("Supply exactly the custom step's declared inputs.")
	}
	r, compiled, err := s.compile(ctx)
	if err != nil {
		return nil, err
	}
	defer r.Close(ctx)
	ctx, cancel := context.WithTimeout(ctx, time.Duration(s.TimeoutMS)*time.Millisecond)
	defer cancel()
	module, err := r.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithStartFunctions())
	if err != nil {
		return nil, err
	}
	values, err := s.encode(args, module.Memory())
	if err != nil {
		return nil, err
	}
	result, err := module.ExportedFunction("run").Call(ctx, values...)
	if ctx.Err() != nil {
		return nil, fmt.Errorf("Custom step stopped: %w", ctx.Err())
	}
	if err != nil {
		return nil, fmt.Errorf("Custom step could not finish: %w", err)
	}
	if s.Output == "bool" {
		if result[0] > 1 {
			return nil, errors.New("A custom step returning bool must return 0 or 1.")
		}
		return result[0] == 1, nil
	}
	if n := int64(result[0]); n < -maxJSONInteger || n > maxJSONInteger {
		return nil, errors.New("A custom step result must be a safe JSON integer.")
	}
	return int64(result[0]), nil
}

const maxJSONInteger = 1<<53 - 1

func (s Step) encode(args map[string]any, memory api.Memory) ([]uint64, error) {
	var values []uint64
	var offset uint32
	for _, input := range s.Inputs {
		arg, exists := args[input.Name]
		if !exists {
			return nil, fmt.Errorf("Custom step input %q is missing.", input.Name)
		}
		switch input.Type {
		case "date":
			d, err := date(arg)
			if err != nil {
				return nil, err
			}
			values = append(values, uint64(d))
		case "dates":
			items, ok := arg.([]any)
			if !ok || len(items) > MaxDateValues {
				return nil, fmt.Errorf("A dates input takes up to %d dates.", MaxDateValues)
			}
			values = append(values, uint64(offset), uint64(len(items)))
			for _, item := range items {
				d, err := date(item)
				if err != nil {
					return nil, err
				}
				if !memory.WriteUint64Le(offset, uint64(d)) {
					return nil, errors.New("The custom step's date list exceeds its memory.")
				}
				offset += 8
			}
		case "bool":
			b, ok := arg.(bool)
			if !ok {
				return nil, fmt.Errorf("Custom step input %q needs a bool.", input.Name)
			}
			var bit uint64
			if b {
				bit = 1
			}
			values = append(values, bit)
		case "integer":
			n, ok := number(arg)
			if !ok || math.Trunc(n) != n || math.Abs(n) > maxJSONInteger || math.IsNaN(n) {
				return nil, fmt.Errorf("Custom step input %q needs a safe integer.", input.Name)
			}
			values = append(values, uint64(int64(n)))
		}
	}
	return values, nil
}

func number(arg any) (float64, bool) {
	switch v := arg.(type) {
	case json.Number:
		n, err := v.Float64()
		return n, err == nil
	case float64:
		return v, true
	case int64:
		return float64(v), v >= -maxJSONInteger && v <= maxJSONInteger
	case int:
		return number(int64(v))
	}
	return 0, false
}

func date(value any) (int64, error) {
	text, ok := value.(string)
	if !ok {
		return 0, errors.New("A custom step date needs YYYY-MM-DD.")
	}
	t, err := time.Parse(time.DateOnly, text)
	if err != nil {
		t, err = time.Parse(time.RFC3339Nano, text)
		if err != nil {
			return 0, errors.New("A custom step date needs YYYY-MM-DD or an ISO timestamp.")
		}
	}
	utc := t.UTC()
	day := time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC)
	return day.Unix() / 86400, nil
}

// ToolID reserves the local custom namespace before connector dispatch.
func ToolID(tool string) (string, bool) {
	id, ok := strings.CutPrefix(tool, "custom.")
	return id, ok && identifier.MatchString(id)
}
