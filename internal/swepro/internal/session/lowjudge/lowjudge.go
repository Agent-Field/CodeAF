// Package lowjudge ports src/session/low-judge.ts lines 1-89 from swe-pro
// commit 3b25a1a. Zod and ai.generateObject are outside this bundle, so their
// two used operations are represented by the narrow Schema and ObjectGenerator
// interfaces below.
//
// The control behavior remains source-identical: falsy language values fall
// back immediately, model generation gets two tries, every returned object is
// schema-validated, failures are logged with UTF-16 caps, and fallback runs
// exactly once only after the model path is unavailable or exhausted.
package lowjudge

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

const maxTries = 2

var defaultTimeoutMS = readDefaultTimeoutMS()

// SchemaParseResult is the subset of z.SafeParseReturnType observed by
// lowJudge. ErrorMessage corresponds to parsed.error.message.
type SchemaParseResult[T any] struct {
	Success      bool
	Data         T
	ErrorMessage string
}

// Schema is the narrow z.ZodType<T> dependency used by lowJudge.
type Schema[T any] interface {
	SafeParse(value any) SchemaParseResult[T]
}

// AbortSignal is the Go adapter for AbortSignal.timeout. Generators should use
// Context for cancellation; TimeoutMS retains the exact requested delay for
// adapters that need it.
type AbortSignal struct {
	Context   context.Context
	TimeoutMS float64
}

// GenerateParams is the subset passed to ai.generateObject.
type GenerateParams struct {
	Model       any
	Prompt      string
	Schema      any
	AbortSignal *AbortSignal
}

// GeneratedObject mirrors generateObject's `{ object: T }` result without
// trusting T before the schema's safe parse.
type GeneratedObject struct {
	Object any
}

// ObjectGenerator is the narrow ai.generateObject dependency.
type ObjectGenerator interface {
	Generate(params GenerateParams) (GeneratedObject, error)
}

type unavailableGenerator struct{}

func (unavailableGenerator) Generate(GenerateParams) (GeneratedObject, error) {
	return GeneratedObject{}, errors.New("low-judge default generator is not configured")
}

// DefaultGenerator is the integration seam for the out-of-bundle AI SDK.
// Callers may install their process-wide adapter; per-call Generate wins.
var DefaultGenerator ObjectGenerator = unavailableGenerator{}

// ModelProperties lets a Go language-model adapter expose the two JavaScript
// properties lowJudge reads for telemetry.
type ModelProperties interface {
	ModelProperty(name string) (any, bool)
}

// ModelReference is a convenient ModelProperties implementation.
type ModelReference struct {
	ModelID any
	ID      any
}

// ModelProperty implements ModelProperties.
func (m ModelReference) ModelProperty(name string) (any, bool) {
	switch name {
	case "modelId":
		return m.ModelID, m.ModelID != nil
	case "id":
		return m.ID, m.ID != nil
	default:
		return nil, false
	}
}

// LowJudgeInput mirrors the TS interface. TimeoutMS nil is undefined/null and
// therefore selects the module-load environment default. NowMillis and Log are
// Go-only injection points for deterministic fixture replay.
type LowJudgeInput[T any] struct {
	Prompt    string
	Schema    Schema[T]
	Language  any
	Fallback  func() T
	TimeoutMS *float64
	Generate  ObjectGenerator
	NowMillis func() float64
	Log       func(string)
}

// LowJudgeResult mirrors the TS interface.
type LowJudgeResult[T any] struct {
	Value  T      `json:"value"`
	Source string `json:"source"`
}

type undefinedValue struct{}

var jsUndefined = undefinedValue{}

// MarshalJSON preserves JSON.stringify's omission of a value property whose
// schema data is JavaScript undefined. Normal Go nil remains explicit null.
func (r LowJudgeResult[T]) MarshalJSON() ([]byte, error) {
	if _, omitted := any(r.Value).(undefinedValue); omitted {
		return jscompat.Stringify(struct {
			Source string `json:"source"`
		}{Source: r.Source})
	}
	return jscompat.Stringify(struct {
		Value  T      `json:"value"`
		Source string `json:"source"`
	}{Value: r.Value, Source: r.Source})
}

// LowJudge performs the bounded low-tier judgment with deterministic fallback.
func LowJudge[T any](input LowJudgeInput[T]) LowJudgeResult[T] {
	if !jsTruthy(input.Language) {
		return LowJudgeResult[T]{Value: input.Fallback(), Source: "fallback"}
	}
	generator := input.Generate
	if generator == nil {
		generator = DefaultGenerator
	}
	now := input.NowMillis
	if now == nil {
		now = func() float64 { return float64(time.Now().UnixMilli()) }
	}
	logLine := input.Log
	if logLine == nil {
		logLine = func(line string) { _, _ = fmt.Fprintln(os.Stderr, line) }
	}
	modelID := modelIDOf(input.Language)

	for attempt := 1; attempt <= maxTries; attempt++ {
		startedAt := now()
		parsed, mismatch, caught := runAttempt(input, generator)
		if caught != nil {
			elapsed := now() - startedAt
			logLine("[low-judge] modelID=" + modelID +
				" failed in " + jscompat.FormatNumber(elapsed) +
				"ms (attempt " + strconv.Itoa(attempt) + "/" + strconv.Itoa(maxTries) + "): " +
				utf16SliceTo(errorText(caught), 300))
			continue
		}
		if mismatch {
			logLine("[low-judge] modelID=" + modelID +
				" schema mismatch (attempt " + strconv.Itoa(attempt) + "/" + strconv.Itoa(maxTries) + "): " +
				utf16SliceTo(parsed.ErrorMessage, 200))
			continue
		}
		elapsed := now() - startedAt
		logLine("[low-judge] modelID=" + modelID +
			" ok in " + jscompat.FormatNumber(elapsed) +
			"ms (attempt " + strconv.Itoa(attempt) + ")")
		return LowJudgeResult[T]{Value: parsed.Data, Source: "llm"}
	}
	return LowJudgeResult[T]{Value: input.Fallback(), Source: "fallback"}
}

func runAttempt[T any](input LowJudgeInput[T], generator ObjectGenerator) (
	parsed SchemaParseResult[T],
	mismatch bool,
	caught any,
) {
	defer func() {
		if value := recover(); value != nil {
			caught = value
		}
	}()
	timeoutMS := defaultTimeoutMS
	if input.TimeoutMS != nil {
		timeoutMS = *input.TimeoutMS
	}
	signal, cancel, err := timeoutSignal(timeoutMS)
	if err != nil {
		return parsed, false, err
	}
	defer cancel()
	generated, err := generator.Generate(GenerateParams{
		Model: input.Language, Prompt: input.Prompt, Schema: input.Schema, AbortSignal: signal,
	})
	if err != nil {
		return parsed, false, err
	}
	parsed = input.Schema.SafeParse(generated.Object)
	return parsed, !parsed.Success, nil
}

func timeoutSignal(ms float64) (*AbortSignal, context.CancelFunc, error) {
	const maxAbortTimeout = 9007199254740991
	if math.IsNaN(ms) || math.IsInf(ms, 0) || ms < 0 || ms > maxAbortTimeout {
		return nil, func() {}, errors.New(
			"Value " + jscompat.FormatNumber(ms) + " is outside the range [0, 9007199254740991]",
		)
	}
	maxDurationMS := float64(math.MaxInt64) / float64(time.Millisecond)
	if ms > maxDurationMS {
		// AbortSignal accepts values up to Number.MAX_SAFE_INTEGER. Durations
		// above Go's ~292-year timer range cannot expire in a live process, so
		// a cancellable background context is observably equivalent.
		ctx, cancel := context.WithCancel(context.Background())
		return &AbortSignal{Context: ctx, TimeoutMS: ms}, cancel, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(ms*float64(time.Millisecond)))
	return &AbortSignal{Context: ctx, TimeoutMS: ms}, cancel, nil
}

func readDefaultTimeoutMS() float64 {
	raw, ok := os.LookupEnv("CODEAF_LOW_JUDGE_TIMEOUT_MS")
	if !ok {
		raw = "90000"
	}
	trimmed := jscompat.Trim(raw)
	if strings.Contains(trimmed, "_") ||
		(strings.EqualFold(trimmed, "inf") || strings.EqualFold(trimmed, "+inf") || strings.EqualFold(trimmed, "-inf")) ||
		(strings.EqualFold(trimmed, "infinity") && trimmed != "Infinity" && trimmed != "+Infinity" && trimmed != "-Infinity") {
		return math.NaN()
	}
	return jscompat.ToNumber(raw)
}

func modelIDOf(language any) string {
	if value, ok := modelProperty(language, "modelId"); ok && value != nil {
		return jsString(value)
	}
	if value, ok := modelProperty(language, "id"); ok && value != nil {
		return jsString(value)
	}
	return "low-tier"
}

func modelProperty(language any, name string) (any, bool) {
	switch model := language.(type) {
	case map[string]any:
		value, ok := model[name]
		return value, ok
	case ModelProperties:
		return model.ModelProperty(name)
	case ModelReference:
		return model.ModelProperty(name)
	case *ModelReference:
		if model == nil {
			return nil, false
		}
		return model.ModelProperty(name)
	default:
		return nil, false
	}
}

func jsTruthy(value any) bool {
	switch value := value.(type) {
	case nil:
		return false
	case bool:
		return value
	case string:
		return value != ""
	case float64:
		return value != 0 && !math.IsNaN(value)
	case float32:
		return value != 0 && !math.IsNaN(float64(value))
	case int:
		return value != 0
	case int64:
		return value != 0
	}
	return true
}

func jsString(value any) string {
	switch value := value.(type) {
	case nil:
		return "null"
	case string:
		return value
	case bool:
		if value {
			return "true"
		}
		return "false"
	case float64:
		return jscompat.FormatNumber(value)
	case float32:
		return jscompat.FormatNumber(float64(value))
	case int:
		return strconv.Itoa(value)
	case int64:
		return strconv.FormatInt(value, 10)
	case []any:
		parts := make([]string, len(value))
		for i, item := range value {
			if item != nil {
				parts[i] = jsString(item)
			}
		}
		return strings.Join(parts, ",")
	default:
		return "[object Object]"
	}
}

type thrownValue interface {
	ThrownValue() any
}

func errorText(value any) string {
	if thrown, ok := value.(thrownValue); ok {
		raw := thrown.ThrownValue()
		if object, ok := raw.(map[string]any); ok {
			if message, exists := object["message"]; exists && message != nil {
				return jsString(message)
			}
		}
		return jsString(raw)
	}
	if err, ok := value.(error); ok {
		return err.Error()
	}
	return jsString(value)
}

func decodeWTF8(s string, i int) (rune, int) {
	b := s[i]
	switch {
	case b < 0x80:
		return rune(b), 1
	case b&0xe0 == 0xc0:
		if i+1 < len(s) && s[i+1]&0xc0 == 0x80 {
			cp := rune(b&0x1f)<<6 | rune(s[i+1]&0x3f)
			if cp >= 0x80 {
				return cp, 2
			}
		}
	case b&0xf0 == 0xe0:
		if i+2 < len(s) && s[i+1]&0xc0 == 0x80 && s[i+2]&0xc0 == 0x80 {
			cp := rune(b&0x0f)<<12 | rune(s[i+1]&0x3f)<<6 | rune(s[i+2]&0x3f)
			if cp >= 0x800 {
				return cp, 3
			}
		}
	case b&0xf8 == 0xf0:
		if i+3 < len(s) && s[i+1]&0xc0 == 0x80 && s[i+2]&0xc0 == 0x80 && s[i+3]&0xc0 == 0x80 {
			cp := rune(b&0x07)<<18 | rune(s[i+1]&0x3f)<<12 | rune(s[i+2]&0x3f)<<6 | rune(s[i+3]&0x3f)
			if cp >= 0x10000 && cp <= 0x10ffff {
				return cp, 4
			}
		}
	}
	return utf8.RuneError, 1
}

func appendWTF8(dst []byte, cp rune) []byte {
	if cp >= 0xd800 && cp <= 0xdfff {
		return append(dst,
			byte(0xe0|cp>>12),
			byte(0x80|(cp>>6)&0x3f),
			byte(0x80|cp&0x3f))
	}
	return utf8.AppendRune(dst, cp)
}

func utf16SliceTo(s string, n int) string {
	if n <= 0 {
		return ""
	}
	units := 0
	for i := 0; i < len(s); {
		cp, size := decodeWTF8(s, i)
		width := 1
		if cp >= 0x10000 {
			width = 2
		}
		if units+width > n {
			high := rune(0xd800 + ((cp - 0x10000) >> 10))
			return s[:i] + string(appendWTF8(nil, high))
		}
		units += width
		i += size
		if units == n {
			return s[:i]
		}
	}
	return s
}
