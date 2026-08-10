package jscompat

import (
	"math"
)

// JSNumber is a float64 that marshals the way a JS number does inside
// JSON.stringify: finite values use V8's shortest round-trip decimal form,
// NaN and ±Infinity become null. Unmarshalling JSON null yields NaN so a
// null→null round trip is preserved.
type JSNumber float64

func (n JSNumber) MarshalJSON() ([]byte, error) {
	f := float64(n)
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return []byte("null"), nil
	}
	return []byte(FormatNumber(f)), nil
}

func (n *JSNumber) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		*n = JSNumber(math.NaN())
		return nil
	}
	*n = JSNumber(ToNumber(string(b)))
	return nil
}
