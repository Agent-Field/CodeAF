package calc

import (
	"math"
	"strings"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

// JS `Number(x)` over the JSON value model.
//
// getUsage wraps the entire five-deep provider-metadata `??` chain in
// `Number(...)` (`session.ts:363-374`). The chain's non-numeric arms come from
// `ProviderMetadata` = `Record<string, Record<string, JSONValue>>`, so the
// argument can be any JSON value, and ToNumber's object path (ToPrimitive →
// valueOf → toString) is reachable in principle. Reproduced rather than
// assumed-numeric, because the result feeds `safe()` and a wrong NaN vs 0
// changes `tokens.input`.

// jsNumberOfString is JS `Number(s)`.
func jsNumberOfString(s string) float64 { return jscompat.ToNumber(s) }

// jsNumberOfValue is JS `Number(v)` where v is a decoded JSON value
// (nil | bool | float64 | string | []any | map[string]any).
func jsNumberOfValue(v any) float64 {
	switch typed := v.(type) {
	case nil:
		// Number(null) === 0. (Number(undefined) is NaN, but `undefined` never
		// reaches here — the `??` chain consumes it first.)
		return 0
	case bool:
		if typed {
			return 1
		}
		return 0
	case float64:
		return typed
	case int:
		return float64(typed)
	case string:
		return jscompat.ToNumber(typed)
	case []any:
		// ToPrimitive(array, number) finds no usable valueOf, falls through to
		// Array.prototype.toString === join(","). Number([]) === 0,
		// Number([5]) === 5, Number([5,6]) === NaN.
		return jscompat.ToNumber(jsStringOfValue(typed))
	case map[string]any:
		// "[object Object]" → NaN.
		return math.NaN()
	}
	return math.NaN()
}

// jsStringOfValue is JS `String(v)` for the JSON value model, used only by the
// array branch above (Array.prototype.join stringifies each element, mapping
// null and undefined to the empty string).
func jsStringOfValue(v any) string {
	switch typed := v.(type) {
	case nil:
		return ""
	case bool:
		if typed {
			return "true"
		}
		return "false"
	case float64:
		return jscompat.FormatNumber(typed)
	case int:
		return jscompat.FormatNumber(float64(typed))
	case string:
		return typed
	case []any:
		parts := make([]string, 0, len(typed))
		for _, element := range typed {
			parts = append(parts, jsStringOfValue(element))
		}
		return strings.Join(parts, ",")
	case map[string]any:
		return "[object Object]"
	}
	return ""
}
