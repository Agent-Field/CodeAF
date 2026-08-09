package lowjudge

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

type fixture struct {
	Name     string `json:"name"`
	Fn       string `json:"fn"`
	ArgsJSON string `json:"args_json"`
	OutJSON  string `json:"out_json"`
}

type schemaSpec struct {
	Kind  string `json:"kind"`
	Error string `json:"error"`
}

type fakeSchema struct{ spec schemaSpec }

func (s *fakeSchema) SafeParse(value any) SchemaParseResult[any] {
	fail := func() SchemaParseResult[any] {
		return SchemaParseResult[any]{Success: false, ErrorMessage: s.spec.Error}
	}
	switch s.spec.Kind {
	case "accept":
		return SchemaParseResult[any]{Success: true, Data: value}
	case "string":
		if text, ok := value.(string); ok {
			return SchemaParseResult[any]{Success: true, Data: text}
		}
		return fail()
	case "number":
		if number, ok := value.(float64); ok {
			return SchemaParseResult[any]{Success: true, Data: number}
		}
		return fail()
	case "answer":
		if object, ok := value.(map[string]any); ok {
			if answer, ok := object["answer"].(string); ok {
				return SchemaParseResult[any]{Success: true, Data: answer}
			}
		}
		return fail()
	default:
		return fail()
	}
}

type outcome struct {
	Kind    string `json:"kind"`
	Object  any    `json:"object"`
	Message string `json:"message"`
	Value   any    `json:"value"`
}

type scenarioInput struct {
	Language  any        `json:"language"`
	Prompt    string     `json:"prompt"`
	Schema    schemaSpec `json:"schema"`
	Fallback  any        `json:"fallback"`
	TimeoutMS *float64   `json:"timeoutMs"`
	Outcomes  []outcome  `json:"outcomes"`
}

type thrown struct{ value any }

func (e thrown) Error() string    { return jsString(e.value) }
func (e thrown) ThrownValue() any { return e.value }

type callRecord struct {
	Prompt       string  `json:"prompt"`
	TimeoutMS    float64 `json:"timeoutMs"`
	SchemaPassed bool    `json:"schemaPassed"`
}

type fakeGenerator struct {
	outcomes []outcome
	schema   *fakeSchema
	calls    []callRecord
}

func (g *fakeGenerator) Generate(params GenerateParams) (GeneratedObject, error) {
	g.calls = append(g.calls, callRecord{
		Prompt: params.Prompt, TimeoutMS: params.AbortSignal.TimeoutMS,
		SchemaPassed: params.Schema == g.schema,
	})
	i := len(g.calls) - 1
	if i >= len(g.outcomes) {
		return GeneratedObject{}, errors.New("exhausted")
	}
	next := g.outcomes[i]
	switch next.Kind {
	case "return":
		if next.Object == nil {
			return GeneratedObject{Object: jsUndefined}, nil
		}
		return GeneratedObject{Object: next.Object}, nil
	case "throw":
		return GeneratedObject{}, errors.New(next.Message)
	case "throw-value":
		return GeneratedObject{}, thrown{value: next.Value}
	default:
		return GeneratedObject{}, errors.New("unknown outcome")
	}
}

type scenarioOutput struct {
	Result        LowJudgeResult[any] `json:"result"`
	Logs          []string            `json:"logs"`
	Calls         []callRecord        `json:"calls"`
	FallbackCalls int                 `json:"fallbackCalls"`
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
	sc.Buffer(make([]byte, 0, 1<<20), 1<<22)
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
	if len(fixtures) < 25 {
		t.Fatalf("expected at least 25 fixtures, got %d", len(fixtures))
	}
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
			schema := &fakeSchema{spec: input.Schema}
			generator := &fakeGenerator{outcomes: input.Outcomes, schema: schema, calls: []callRecord{}}
			logs := []string{}
			fallbackCalls := 0
			clock := float64(1000)
			result := LowJudge(LowJudgeInput[any]{
				Prompt: input.Prompt, Schema: schema, Language: input.Language,
				Fallback: func() any {
					fallbackCalls++
					return input.Fallback
				},
				TimeoutMS: input.TimeoutMS, Generate: generator,
				NowMillis: func() float64 {
					value := clock
					clock += 17
					return value
				},
				Log: func(line string) { logs = append(logs, line) },
			})
			output := scenarioOutput{
				Result: result, Logs: logs, Calls: generator.calls, FallbackCalls: fallbackCalls,
			}
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

func TestPanickingGeneratorIsRetried(t *testing.T) {
	schema := &fakeSchema{spec: schemaSpec{Kind: "string", Error: "bad"}}
	logs := []string{}
	result := LowJudge(LowJudgeInput[string]{
		Prompt: "p", Schema: schemaForString{schema}, Language: map[string]any{"id": "x"},
		Fallback: func() string { return "fallback" },
		Generate: panicGenerator{},
		NowMillis: func() func() float64 {
			now := float64(0)
			return func() float64 {
				now++
				return now
			}
		}(),
		Log: func(line string) { logs = append(logs, line) },
	})
	if result.Source != "fallback" || result.Value != "fallback" {
		t.Fatalf("result=%+v", result)
	}
	if len(logs) != 2 || !strings.Contains(logs[0], "panic from generator") {
		t.Fatalf("logs=%v", logs)
	}
}

type schemaForString struct{ inner *fakeSchema }

func (s schemaForString) SafeParse(value any) SchemaParseResult[string] {
	parsed := s.inner.SafeParse(value)
	text, _ := parsed.Data.(string)
	return SchemaParseResult[string]{
		Success: parsed.Success, Data: text, ErrorMessage: parsed.ErrorMessage,
	}
}

type panicGenerator struct{}

func (panicGenerator) Generate(GenerateParams) (GeneratedObject, error) {
	panic("panic from generator")
}
