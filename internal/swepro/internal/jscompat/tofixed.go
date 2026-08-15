package jscompat

import (
	"math"
	"math/big"
	"strings"
)

// ToFixed reproduces Number.prototype.toFixed(digits) per the ECMAScript
// spec: the EXACT binary value of the double is scaled by 10^digits and
// rounded to the nearest integer, ties picking the LARGER n (i.e. half-up on
// the magnitude, sign applied afterwards). This differs from Go's
// strconv.FormatFloat(f, 'f', digits, 64), which rounds half-to-even:
// (0.125).toFixed(2) is "0.13" in JS but FormatFloat gives "0.12".
// |f| >= 1e21 falls back to ToString, like the spec.
func ToFixed(f float64, digits int) string {
	if math.IsNaN(f) {
		return "NaN"
	}
	if math.IsInf(f, 1) {
		return "Infinity"
	}
	if math.IsInf(f, -1) {
		return "-Infinity"
	}
	if math.Abs(f) >= 1e21 {
		return FormatNumber(f)
	}
	sign := ""
	if f < 0 {
		sign = "-"
	}
	x := math.Abs(f)
	r := new(big.Rat).SetFloat64(x)
	pow := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(digits)), nil)
	r.Mul(r, new(big.Rat).SetInt(pow))
	// n = round(r), ties toward +inf (on the magnitude).
	q, rem := new(big.Int).QuoRem(r.Num(), r.Denom(), new(big.Int))
	rem.Mul(rem, big.NewInt(2))
	if rem.Cmp(r.Denom()) >= 0 {
		q.Add(q, big.NewInt(1))
	}
	s := q.String()
	if digits == 0 {
		return sign + s
	}
	if len(s) <= digits {
		s = strings.Repeat("0", digits-len(s)+1) + s
	}
	return sign + s[:len(s)-digits] + "." + s[len(s)-digits:]
}
