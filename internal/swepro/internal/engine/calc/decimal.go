package calc

import (
	"math"
	"math/big"
	"strconv"
	"strings"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

// A minimal image of decimal.js 10.5.0 at its DEFAULT configuration, covering
// exactly the four operations `Session.getUsage` performs on the cost chain
// (`session.ts:401-411`): construct-from-number, `mul`, `div` (always by
// 1_000_000), `add`, and `toNumber`.
//
// ENGINE-DESIGN §3.5 requires `math/big.Rat`, but treating the whole expression
// as one exact Rat would be the one thing decimal.js is not. decimal.js
// finalises every arithmetic result to `Decimal.precision` = 20 SIGNIFICANT
// digits using `Decimal.rounding` = 4 = ROUND_HALF_UP (ties away from zero);
// nothing in swe-pro calls `Decimal.set`/`Decimal.config`, so those defaults
// stand (decimal.mjs:39, :56). Verified against the real module:
//
//	new Decimal(999999).mul(0.12345678901234567)
//	  exact:  123456.66555555664765434    (23 significant digits)
//	  actual: 123456.66555555664765       (rounded to 20, HALF_UP)
//
// Finite operations below therefore use Rat, convert each terminating result
// back to a decimal coefficient, and round it to 20 significant digits before
// the next operation. This preserves decimal.js's expression shape and is
// pinned by gen-calc.ts's exact `costDecimalString` cases.

// dec is a decimal.js value: sign · unscaled · 10^-scale, or one of the three
// non-finite specials. Zero is (unscaled=0); decimal.js keeps a sign on zero
// but every path out of this package goes through jscompat.FormatNumber, which
// prints -0 as "0", so the sign of zero is not tracked.
type dec struct {
	special  uint8 // decFinite | decNaN | decPosInf | decNegInf
	unscaled *big.Int
	scale    int
}

const (
	decFinite uint8 = iota
	decNaN
	decPosInf
	decNegInf
)

// decPrecision is Decimal.precision (decimal.mjs:39). ROUND_HALF_UP is
// Decimal.rounding = 4 (decimal.mjs:56) and is baked into roundToPrecision.
const decPrecision = 20

// decToExpNeg / decToExpPos are Decimal.toExpNeg / Decimal.toExpPos
// (decimal.mjs:45, :51) — the window outside which toString goes exponential.
const (
	decToExpNeg = -7
	decToExpPos = 21
)

var bigTen = big.NewInt(10)

func decNaNValue() dec { return dec{special: decNaN} }

func decZero() dec { return dec{unscaled: new(big.Int)} }

// decFromFloat is `new Decimal(v)` for a JS number. The number path ends at
// `parseDecimal(x, v.toString())` (decimal.mjs:4373), so the decimal value is
// the SHORTEST round-trip decimal form of the float — jscompat.FormatNumber.
func decFromFloat(v float64) dec {
	if math.IsNaN(v) {
		return dec{special: decNaN}
	}
	if math.IsInf(v, 1) {
		return dec{special: decPosInf}
	}
	if math.IsInf(v, -1) {
		return dec{special: decNegInf}
	}
	return decParse(jscompat.FormatNumber(v))
}

// decParse reads the decimal grammar jscompat.FormatNumber can emit:
// an optional sign, digits with an optional fraction, and an optional
// JS-style exponent ("1e-7", "1e+21"). It is not a general-purpose parser.
func decParse(s string) dec {
	negative := false
	if strings.HasPrefix(s, "-") {
		negative, s = true, s[1:]
	} else if strings.HasPrefix(s, "+") {
		s = s[1:]
	}
	exponent := 0
	if i := strings.IndexAny(s, "eE"); i >= 0 {
		parsed, err := strconv.Atoi(s[i+1:])
		if err != nil {
			return dec{special: decNaN}
		}
		exponent, s = parsed, s[:i]
	}
	scale := 0
	if i := strings.IndexByte(s, '.'); i >= 0 {
		scale = len(s) - i - 1
		s = s[:i] + s[i+1:]
	}
	unscaled, ok := new(big.Int).SetString(s, 10)
	if !ok {
		return dec{special: decNaN}
	}
	if negative {
		unscaled.Neg(unscaled)
	}
	return dec{unscaled: unscaled, scale: scale - exponent}
}

func (d dec) isZero() bool {
	return d.special == decFinite && d.unscaled.Sign() == 0
}

// mul is P.times: a rational product, then finalise to 20 significant digits.
func (d dec) mul(other dec) dec {
	if d.special != decFinite || other.special != decFinite {
		return mulSpecial(d, other)
	}
	product := new(big.Rat).Mul(d.rat(), other.rat())
	return decFromRat(product).roundToPrecision()
}

// divPow10 is P.div by a power of ten. In the cost chain the divisor is always
// the literal 1_000_000, so the quotient is a pure exponent shift and the
// finalise that follows can only be a no-op — but it is applied anyway,
// because decimal.js's `divide()` does compute to `Decimal.precision` digits.
func (d dec) divPow10(power int) dec {
	if d.special != decFinite {
		// ±Infinity / 1e6 is ±Infinity; NaN / 1e6 is NaN.
		return d
	}
	divisor := new(big.Int).Exp(bigTen, big.NewInt(int64(power)), nil)
	quotient := new(big.Rat).Quo(d.rat(), new(big.Rat).SetInt(divisor))
	return decFromRat(quotient).roundToPrecision()
}

// add is P.plus: a rational sum, then finalise.
func (d dec) add(other dec) dec {
	if d.special != decFinite || other.special != decFinite {
		return addSpecial(d, other)
	}
	sum := new(big.Rat).Add(d.rat(), other.rat())
	return decFromRat(sum).roundToPrecision()
}

// rat returns the exact rational value of a finite decimal.
func (d dec) rat() *big.Rat {
	if d.scale <= 0 {
		factor := new(big.Int).Exp(bigTen, big.NewInt(int64(-d.scale)), nil)
		return new(big.Rat).SetInt(new(big.Int).Mul(d.unscaled, factor))
	}
	denominator := new(big.Int).Exp(bigTen, big.NewInt(int64(d.scale)), nil)
	return new(big.Rat).SetFrac(new(big.Int).Set(d.unscaled), denominator)
}

// decFromRat converts a terminating rational to coefficient × 10^-scale.
// These operations start with decimal values and divide only by powers of ten,
// so a reduced denominator can contain no prime factors besides 2 and 5.
func decFromRat(value *big.Rat) dec {
	numerator := new(big.Int).Set(value.Num())
	denominator := new(big.Int).Set(value.Denom())
	twos := factorCount(denominator, 2)
	fives := factorCount(denominator, 5)
	if denominator.Cmp(big.NewInt(1)) != 0 {
		return decNaNValue()
	}

	scale := twos
	if fives > scale {
		scale = fives
	}
	if twos < scale {
		factor := new(big.Int).Exp(big.NewInt(2), big.NewInt(int64(scale-twos)), nil)
		numerator.Mul(numerator, factor)
	}
	if fives < scale {
		factor := new(big.Int).Exp(big.NewInt(5), big.NewInt(int64(scale-fives)), nil)
		numerator.Mul(numerator, factor)
	}
	return dec{unscaled: numerator, scale: scale}
}

func factorCount(value *big.Int, factor int64) int {
	divisor := big.NewInt(factor)
	quotient := new(big.Int)
	remainder := new(big.Int)
	count := 0
	for {
		quotient.QuoRem(value, divisor, remainder)
		if remainder.Sign() != 0 {
			return count
		}
		value.Set(quotient)
		count++
	}
}

// mulSpecial reproduces decimal.js's non-finite multiplication: 0 × ±Infinity
// is NaN, anything else with a NaN operand is NaN, otherwise ±Infinity with the
// product of the signs.
func mulSpecial(a, b dec) dec {
	if a.special == decNaN || b.special == decNaN {
		return decNaNValue()
	}
	if (a.special != decFinite && b.isZero()) || (b.special != decFinite && a.isZero()) {
		return decNaNValue()
	}
	if specialSign(a)*specialSign(b) < 0 {
		return dec{special: decNegInf}
	}
	return dec{special: decPosInf}
}

// addSpecial reproduces decimal.js's non-finite addition: NaN is contagious and
// (+Infinity) + (−Infinity) is NaN.
func addSpecial(a, b dec) dec {
	if a.special == decNaN || b.special == decNaN {
		return decNaNValue()
	}
	if a.special != decFinite && b.special != decFinite {
		if a.special != b.special {
			return decNaNValue()
		}
		return a
	}
	if a.special != decFinite {
		return a
	}
	return b
}

// specialSign is the operand's sign for the ±Infinity bookkeeping above.
func specialSign(d dec) int {
	switch d.special {
	case decPosInf:
		return 1
	case decNegInf:
		return -1
	}
	if d.unscaled.Sign() < 0 {
		return -1
	}
	return 1
}

// roundToPrecision is decimal.js's `finalise(x, Ctor.precision, Ctor.rounding)`
// with precision 20 and rounding 4 (ROUND_HALF_UP — ties away from zero).
//
// decimal.js counts SIGNIFICANT digits (trailing zeros of the coefficient are
// normalised away first); this counts the decimal digits of the unscaled
// integer, which includes trailing zeros. The two agree on the resulting VALUE:
// the extra digits dropped here are exactly the trailing zeros, which divide
// out with zero remainder, and the half-up comparison scales identically on
// both sides (dropping k extra zeros multiplies both the remainder and the
// threshold by 10^k).
func (d dec) roundToPrecision() dec {
	if d.special != decFinite || d.unscaled.Sign() == 0 {
		return d
	}
	digits := len(new(big.Int).Abs(d.unscaled).String())
	if digits <= decPrecision {
		return d
	}
	drop := digits - decPrecision
	divisor := new(big.Int).Exp(bigTen, big.NewInt(int64(drop)), nil)
	quotient, remainder := new(big.Int).QuoRem(d.unscaled, divisor, new(big.Int))
	// ROUND_HALF_UP: away from zero when |remainder| ≥ divisor/2, i.e.
	// 2·|remainder| ≥ divisor.
	doubled := new(big.Int).Abs(remainder)
	doubled.Lsh(doubled, 1)
	if doubled.Cmp(divisor) >= 0 {
		if d.unscaled.Sign() < 0 {
			quotient.Sub(quotient, big.NewInt(1))
		} else {
			quotient.Add(quotient, big.NewInt(1))
		}
	}
	return dec{unscaled: quotient, scale: d.scale - drop}
}

// normalized strips trailing zeros from the coefficient, which is what
// decimal.js's base-1e7 digit array does implicitly: `new Decimal("1.20")`
// prints "1.2" and reports e === 0. Value-preserving.
func (d dec) normalized() dec {
	if d.special != decFinite || d.unscaled.Sign() == 0 {
		return d
	}
	unscaled := new(big.Int).Set(d.unscaled)
	scale := d.scale
	quotient, remainder := new(big.Int), new(big.Int)
	for {
		quotient.QuoRem(unscaled, bigTen, remainder)
		if remainder.Sign() != 0 {
			break
		}
		unscaled.Set(quotient)
		scale--
	}
	return dec{unscaled: unscaled, scale: scale}
}

// String is decimal.js's `P.toString()` (decimal.mjs:2434-2440) via
// `finiteToString` (:3108-3138): the normalised coefficient rendered in
// positional notation, switching to exponential when the base-10 exponent
// satisfies `e <= toExpNeg (-7) || e >= toExpPos (21)`. A zero prints "0" with
// no sign, matching `x.isNeg() && !x.isZero()`.
func (d dec) String() string { return d.decString() }

func (d dec) decString() string {
	switch d.special {
	case decNaN:
		return "NaN"
	case decPosInf:
		return "Infinity"
	case decNegInf:
		return "-Infinity"
	}
	normal := d.normalized()
	if normal.unscaled.Sign() == 0 {
		return "0"
	}
	sign := ""
	if normal.unscaled.Sign() < 0 {
		sign = "-"
	}
	digits := new(big.Int).Abs(normal.unscaled).String()
	// value = 0.<digits> × 10^(len-scale) = <d>.<igits> × 10^exponent
	exponent := len(digits) - 1 - normal.scale

	if exponent <= decToExpNeg || exponent >= decToExpPos {
		mantissa := digits[:1]
		if len(digits) > 1 {
			mantissa += "." + digits[1:]
		}
		if exponent < 0 {
			return sign + mantissa + "e" + strconv.Itoa(exponent)
		}
		return sign + mantissa + "e+" + strconv.Itoa(exponent)
	}
	switch {
	case exponent < 0:
		return sign + "0." + strings.Repeat("0", -exponent-1) + digits
	case exponent >= len(digits):
		return sign + digits + strings.Repeat("0", exponent+1-len(digits))
	case exponent+1 < len(digits):
		// finiteToString's `if ((k = e + 1) < len)` guard — without it an
		// integer whose exponent lands on the last digit gains a trailing dot.
		return sign + digits[:exponent+1] + "." + digits[exponent+1:]
	default:
		return sign + digits
	}
}

// toNumber is P.toNumber === `+this` (decimal.mjs:2200): Number(toString()).
// strconv.ParseFloat is correctly rounded (ties to even), which is what
// JS's string→number conversion is, so the two agree bit for bit. A magnitude
// past the float64 range yields ±Inf with ErrRange — JS's `Number("1e400")`
// is Infinity too, so the error is deliberately ignored.
func (d dec) toNumber() float64 {
	switch d.special {
	case decNaN:
		return math.NaN()
	case decPosInf:
		return math.Inf(1)
	case decNegInf:
		return math.Inf(-1)
	}
	if d.unscaled.Sign() == 0 {
		return 0
	}
	value, _ := strconv.ParseFloat(new(big.Int).Set(d.unscaled).String()+"e"+strconv.Itoa(-d.scale), 64)
	return value
}
