package adaptive

// Correctly-rounded Math.log / Math.cos / Math.pow, plus JavaScriptCore's
// integer-exponent fast path for `**`.
//
// WHY THIS FILE EXISTS — read before "simplifying" it back to math.Log/Cos/Pow.
//
// gauss() and sampleGamma() push Math.log, Math.cos and Math.pow straight into
// the score that every RouteChoice reports, so a 1-ULP difference is a
// byte-parity failure, not a rounding nicety. swe-pro runs on bun, i.e.
// JavaScriptCore, and JSC forwards Math.log/cos/pow to the HOST libm. On the
// reference machine that is glibc. Measured against 400+ bun-captured bit
// patterns:
//
//	Go math.Log  differs from bun on   6.0% of mulberry32 draws
//	Go math.Cos  differs from bun on  29.3%
//	Go math.Pow  differs from bun on  64.9%
//
// Transcribing Sun's fdlibm does NOT help — that is V8's implementation, not
// JSC's; measured, fdlibm log still missed 4.3% of the same samples.
//
// glibc's log/cos/pow are ~99.9% correctly rounded (measured over 20k samples:
// 0.075% / 0.105% / 0.100% of results are 1 ULP off the true value). So the
// closest reachable pure-Go target is CORRECT ROUNDING, which this file
// implements with math/big at 200 bits of working precision. That takes the
// divergence from ~30% down to ~0.1%, and — unlike the TS original, whose
// numbers change with the host libc — makes the Go port's output identical on
// every platform.
//
// The residual ~0.1% is a genuine, irreducible divergence: reproducing it would
// mean transcribing glibc's ~3000 magic table entries for __log_data,
// __pow_log_data, __exp_data and __sincostab. It is registered in the port
// report and demonstrated by TestGlibcNonCorrectlyRoundedCases.
//
// Math.sqrt needs none of this: IEEE-754 mandates a correctly-rounded square
// root, and both Go and JSC emit the hardware instruction.

import (
	"math"
	"math/big"
	"math/bits"
	"sync"
)

// jsPi is Math.PI — the double nearest pi. It is a var, not a const, so that
// `2.0*jsPi*v` in gauss() is evaluated in float64 left to right exactly like
// JS; Go would otherwise fold the constant sub-expression `2.0*math.Pi` at
// arbitrary precision before rounding.
var jsPi = math.Pi

// crPrec is the working precision of the big.Float core. The series below
// converge to ~2^-190 relative error, so the probability that the final
// rounding to float64 picks the wrong neighbour is ~2^-137.
const crPrec = 200

// reducePrec covers exact argument reduction for cos: the worst case needs
// pi to (exponent of the largest double) + crPrec bits.
const reducePrec = 1400

var (
	crOnce   sync.Once
	bigLn2   *big.Float // ln 2
	bigPi    *big.Float // pi, at reducePrec
	bigHalfP *big.Float // pi/2, at reducePrec
	bigHalf  = big.NewFloat(0.5)
	bigOne   = big.NewFloat(1)
	bigTwo   = big.NewFloat(2)
)

func crInit() {
	crOnce.Do(func() {
		// ln 2 = 2 * atanh(1/3): |z| = 1/3 converges at ~3.17 bits/term.
		third := new(big.Float).SetPrec(crPrec+64).Quo(bigOne, big.NewFloat(3))
		bigLn2 = new(big.Float).SetPrec(crPrec+64).Mul(bigTwo, atanhSeries(third, crPrec+64))

		// Machin: pi = 16*atan(1/5) - 4*atan(1/239).
		a := atanSeriesRecip(5, reducePrec+64)
		b := atanSeriesRecip(239, reducePrec+64)
		a.Mul(a, big.NewFloat(16))
		b.Mul(b, big.NewFloat(4))
		bigPi = new(big.Float).SetPrec(reducePrec+64).Sub(a, b)
		bigHalfP = new(big.Float).SetPrec(reducePrec+64).Mul(bigPi, bigHalf)
	})
}

// atanhSeries sums z + z^3/3 + z^5/5 + ...
func atanhSeries(z *big.Float, prec uint) *big.Float {
	sum := new(big.Float).SetPrec(prec).Set(z)
	term := new(big.Float).SetPrec(prec).Set(z)
	z2 := new(big.Float).SetPrec(prec).Mul(z, z)
	tmp := new(big.Float).SetPrec(prec)
	den := new(big.Float).SetPrec(prec)
	for k := int64(3); ; k += 2 {
		term.Mul(term, z2)
		den.SetInt64(k)
		tmp.Quo(term, den)
		if tmp.Sign() == 0 || sum.MantExp(nil)-tmp.MantExp(nil) > int(prec)+8 {
			break
		}
		sum.Add(sum, tmp)
	}
	return sum
}

// atanSeriesRecip sums atan(1/n) = 1/n - 1/(3n^3) + 1/(5n^5) - ...
func atanSeriesRecip(n int64, prec uint) *big.Float {
	nf := new(big.Float).SetPrec(prec).SetInt64(n)
	z := new(big.Float).SetPrec(prec).Quo(bigOne, nf)
	z2 := new(big.Float).SetPrec(prec).Mul(z, z)
	sum := new(big.Float).SetPrec(prec).Set(z)
	term := new(big.Float).SetPrec(prec).Set(z)
	tmp := new(big.Float).SetPrec(prec)
	den := new(big.Float).SetPrec(prec)
	for k := int64(3); ; k += 2 {
		term.Mul(term, z2)
		den.SetInt64(k)
		tmp.Quo(term, den)
		if tmp.Sign() == 0 || sum.MantExp(nil)-tmp.MantExp(nil) > int(prec)+8 {
			break
		}
		if (k/2)%2 == 1 {
			sum.Sub(sum, tmp)
		} else {
			sum.Add(sum, tmp)
		}
	}
	return sum
}

// bigLog returns ln(x) for x > 0, accurate to ~prec bits.
func bigLog(x *big.Float, prec uint) *big.Float {
	crInit()
	// x = m * 2^e with m in [0.5, 1)
	m := new(big.Float).SetPrec(prec)
	e := x.MantExp(m)
	// ln(m) = 2*atanh((m-1)/(m+1)); |z| <= 1/3 for m in [0.5, 1).
	num := new(big.Float).SetPrec(prec).Sub(m, bigOne)
	den := new(big.Float).SetPrec(prec).Add(m, bigOne)
	z := new(big.Float).SetPrec(prec).Quo(num, den)
	lnM := new(big.Float).SetPrec(prec).Mul(bigTwo, atanhSeries(z, prec))
	ek := new(big.Float).SetPrec(prec).SetInt64(int64(e))
	ek.Mul(ek, bigLn2)
	return new(big.Float).SetPrec(prec).Add(ek, lnM)
}

// bigExp returns e^x, accurate to ~prec bits. Callers guard |x| against the
// range where the result would blow big.Float's exponent field.
func bigExp(x *big.Float, prec uint) *big.Float {
	crInit()
	// k = round(x / ln2), r = x - k*ln2 with |r| <= ln2/2.
	q := new(big.Float).SetPrec(prec).Quo(x, bigLn2)
	k := roundToInt(q)
	kf := new(big.Float).SetPrec(prec).SetInt(k)
	r := new(big.Float).SetPrec(prec).Mul(kf, bigLn2)
	r.Sub(x, r)

	// e^r = sum r^n / n!
	sum := new(big.Float).SetPrec(prec).SetInt64(1)
	term := new(big.Float).SetPrec(prec).SetInt64(1)
	den := new(big.Float).SetPrec(prec)
	for n := int64(1); ; n++ {
		term.Mul(term, r)
		den.SetInt64(n)
		term.Quo(term, den)
		if term.Sign() == 0 || sum.MantExp(nil)-term.MantExp(nil) > int(prec)+8 {
			break
		}
		sum.Add(sum, term)
	}
	return new(big.Float).SetPrec(prec).SetMantExp(sum, int(k.Int64()))
}

// bigCosSin evaluates cos or sin of |t| <= pi/4 by Taylor series.
func bigCosSin(t *big.Float, prec uint, wantSin bool) *big.Float {
	t2 := new(big.Float).SetPrec(prec).Mul(t, t)
	var sum, term *big.Float
	if wantSin {
		sum = new(big.Float).SetPrec(prec).Set(t)
		term = new(big.Float).SetPrec(prec).Set(t)
	} else {
		sum = new(big.Float).SetPrec(prec).SetInt64(1)
		term = new(big.Float).SetPrec(prec).SetInt64(1)
	}
	den := new(big.Float).SetPrec(prec)
	start := int64(2)
	if wantSin {
		start = 3
	}
	sign := true // next term is subtracted
	for n := start; ; n += 2 {
		term.Mul(term, t2)
		den.SetInt64(n * (n - 1))
		term.Quo(term, den)
		if term.Sign() == 0 || sum.MantExp(nil)-term.MantExp(nil) > int(prec)+8 {
			break
		}
		if sign {
			sum.Sub(sum, term)
		} else {
			sum.Add(sum, term)
		}
		sign = !sign
	}
	return sum
}

// roundToInt is floor(f + 1/2).
func roundToInt(f *big.Float) *big.Int {
	t := new(big.Float).SetPrec(f.Prec()).Add(f, bigHalf)
	i, acc := t.Int(nil)
	// Float.Int truncates toward zero; for a negative non-integer that rounds
	// UP, so step down to get a true floor.
	if t.Sign() < 0 && acc == big.Above {
		i.Sub(i, big.NewInt(1))
	}
	return i
}

// ── JS-visible wrappers ──────────────────────────────────────────────────

// jsLog is Math.log.
func jsLog(x float64) float64 {
	switch {
	case math.IsNaN(x) || x < 0:
		return math.NaN()
	case x == 0:
		return math.Inf(-1)
	case x == 1:
		return 0
	case math.IsInf(x, 1):
		return math.Inf(1)
	}
	crInit()
	bx := new(big.Float).SetPrec(crPrec).SetFloat64(x)
	result, _ := bigLog(bx, crPrec).Float64()
	return result
}

// jsCos is Math.cos.
func jsCos(x float64) float64 {
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return math.NaN()
	}
	if x == 0 {
		return 1
	}
	crInit()
	// Reduce to |s| <= pi/4 at reducePrec so the subtraction is exact even for
	// arguments near the top of the double range.
	bx := new(big.Float).SetPrec(reducePrec).SetFloat64(x)
	q := new(big.Float).SetPrec(reducePrec).Quo(bx, bigHalfP)
	n := roundToInt(q)
	nf := new(big.Float).SetPrec(reducePrec).SetInt(n)
	s := new(big.Float).SetPrec(reducePrec).Mul(nf, bigHalfP)
	s.Sub(bx, s)
	s.SetPrec(crPrec)

	quadrant := new(big.Int).Mod(n, big.NewInt(4)).Int64() // Mod is non-negative
	var value *big.Float
	switch quadrant {
	case 0:
		value = bigCosSin(s, crPrec, false)
	case 1:
		value = bigCosSin(s, crPrec, true)
		value.Neg(value)
	case 2:
		value = bigCosSin(s, crPrec, false)
		value.Neg(value)
	default:
		value = bigCosSin(s, crPrec, true)
	}
	result, _ := value.Float64()
	return result
}

// mathPowInteger is JavaScriptCore's exponentiation-by-squaring fast path
// (Source/JavaScriptCore/runtime/MathCommon.h). Empirically it fires for every
// integral exponent in [0, 1000] and nothing else — probed across 300 bases per
// exponent: y in [0,1000] matches this routine 300/300 while y = 1001, 2000 and
// every negative y match the libm result instead.
func mathPowInteger(x float64, yOriginal int32) float64 {
	y := yOriginal
	if y < 0 {
		y = -y
	}
	result := 1.0
	xd := x
	for y != 0 {
		if y&1 != 0 {
			result *= xd
		}
		xd *= xd
		y >>= 1
	}
	if yOriginal < 0 {
		return 1 / result
	}
	return result
}

// jsPow is Math.pow.
func jsPow(x float64, y float64) float64 {
	// ── ECMAScript Number::exponentiate special cases ──
	if math.IsNaN(y) {
		return math.NaN()
	}
	if y == 0 {
		return 1 // including NaN ** 0
	}
	if math.IsNaN(x) {
		return math.NaN()
	}
	if math.IsInf(y, 0) {
		ax := math.Abs(x)
		switch {
		case ax == 1:
			return math.NaN()
		case ax > 1:
			if math.IsInf(y, 1) {
				return math.Inf(1)
			}
			return 0
		default:
			if math.IsInf(y, 1) {
				return 0
			}
			return math.Inf(1)
		}
	}

	// JSC's integer fast path. It runs BEFORE the libm call and therefore
	// before the remaining special cases, exactly as it does in the engine —
	// which is harmless because repeated squaring already yields the
	// spec-mandated answer for +-0, +-Inf and negative bases.
	if yInt, ok := exactInt32(y); ok && yInt >= 0 && yInt <= 1000 {
		return mathPowInteger(x, yInt)
	}

	yIsOddInt := false
	yIsInt := y == math.Trunc(y)
	if yIsInt && math.Abs(y) < 1e18 {
		yIsOddInt = math.Mod(math.Abs(y), 2) == 1
	}

	if math.IsInf(x, 0) {
		if math.IsInf(x, 1) {
			if y > 0 {
				return math.Inf(1)
			}
			return 0
		}
		if y > 0 {
			if yIsOddInt {
				return math.Inf(-1)
			}
			return math.Inf(1)
		}
		if yIsOddInt {
			return math.Copysign(0, -1)
		}
		return 0
	}
	if x == 0 {
		negZero := math.Signbit(x)
		if y > 0 {
			if negZero && yIsOddInt {
				return math.Copysign(0, -1)
			}
			return 0
		}
		if negZero && yIsOddInt {
			return math.Inf(-1)
		}
		return math.Inf(1)
	}
	if x < 0 && !yIsInt {
		return math.NaN()
	}

	sign := 1.0
	if x < 0 && yIsOddInt {
		sign = -1.0
	}
	ax := math.Abs(x)

	// JSC's other two libm bypasses, confirmed against bun on 400 bases each:
	// y == 0.5 becomes sqrt(x) and y == -0.5 becomes 1/sqrt(x). The latter is
	// observably NOT the correctly-rounded power — pow(0.5, -0.5) is
	// 1.414213562373095 in bun but sqrt(2) == 1.4142135623730951 — because the
	// reciprocal rounds a second time. They sit here, after the +-Inf and +-0
	// bases, since Math.pow(-Infinity, 0.5) is +Infinity while sqrt(-Infinity)
	// is NaN.
	if y == 0.5 {
		return math.Sqrt(x)
	}
	if y == -0.5 {
		return 1 / math.Sqrt(x)
	}

	// An exact power-of-two base with an integer exponent yields an exact power
	// of two. The series core cannot be trusted on the resulting rounding TIES:
	// pow(-2, -1075) is exactly 2**-1075, halfway between -0 and the smallest
	// denormal, so ties-to-even must produce -0.
	if yIsInt {
		if k, ok := exactPowerOfTwo(ax); ok {
			e := float64(k) * y
			switch {
			case e > 1e6:
				return sign * math.Inf(1)
			case e < -1e6:
				return sign * 0
			}
			return sign * math.Ldexp(1, int(e))
		}
	}

	// ── general case: sign * exp(y * ln|x|), correctly rounded ──
	crInit()
	lnx := bigLog(new(big.Float).SetPrec(crPrec).SetFloat64(ax), crPrec)
	t := new(big.Float).SetPrec(crPrec).Mul(new(big.Float).SetPrec(crPrec).SetFloat64(y), lnx)
	// Guard big.Float's exponent field before calling exp; anything past these
	// bounds is an unambiguous overflow/underflow for a float64 result.
	if approx, _ := t.Float64(); approx > 1e6 {
		return sign * math.Inf(1)
	} else if approx < -1e6 {
		return sign * 0
	}
	result, _ := bigExp(t, crPrec).Float64()
	return sign * result
}

// exactPowerOfTwo reports the k with ax == 2**k, when ax is exactly a power of
// two (normal or subnormal).
func exactPowerOfTwo(ax float64) (int, bool) {
	raw := math.Float64bits(ax)
	frac := raw & ((1 << 52) - 1)
	expo := int((raw >> 52) & 0x7ff)
	switch expo {
	case 0:
		if frac == 0 || frac&(frac-1) != 0 {
			return 0, false
		}
		return bits.TrailingZeros64(frac) - 1074, true
	case 0x7ff:
		return 0, false
	}
	if frac != 0 {
		return 0, false
	}
	return expo - 1023, true
}

// exactInt32 reports whether f is an integer representable in int32.
func exactInt32(f float64) (int32, bool) {
	if f != math.Trunc(f) || f < -2147483648 || f > 2147483647 {
		return 0, false
	}
	return int32(f), true
}
