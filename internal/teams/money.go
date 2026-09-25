package teams

import (
	"math"
	"strconv"
	"strings"
)

// ── A CAP'S MONEY, SPELLED ONE WAY EVERYWHERE ───────────────────────────────
//
// A team's cap is a figure a person chose, and it is said in four places: the
// session's refusal and cap packet (`harbor reached its $5 cap today`, `Raise
// to $10`), the teams page's header, the packet's card, and the team's settings
// card. They used to spell it three ways, and below a cent two of them lied:
// `$0.001` read `$0.00` in the packet and `Raise to $0` offered a ceiling that
// lifted nothing. So the spelling and the rounding live here, once, and every
// one of those places calls them.

// moneyMicro is the finest step a figure under a cent keeps: a millionth of a
// dollar, which holds every cap a person can sensibly type and drops the noise
// a float sum leaves in the last digits.
const moneyMicro = 1e6

// RoundMoney is a MEASURED figure (a pool's spend, a share of a cap) rounded
// the way it is kept and said: to the cent from a cent up, and to a millionth
// of a dollar below it, so a positive figure under a cent is never rounded away
// to nothing.
func RoundMoney(usd float64) float64 {
	if usd >= 0.01 || usd <= 0 {
		return math.Round(usd*100) / 100
	}
	if r := microRound(usd); r > 0 {
		return r
	}
	return usd
}

// microRound is usd to the millionth of a dollar, which drops the noise a
// float sum leaves in its last digits and keeps every figure a person types.
func microRound(usd float64) float64 { return math.Round(usd*moneyMicro) / moneyMicro }

// Money is a figure as a person reads it, and it is the figure itself: `$5`
// for whole dollars, `$5.50` for whole cents, `$1,234.50` in the thousands,
// and otherwise the shortest spelling that is still the number, `$0.001`,
// `$0.015`. A positive figure is never spelled `$0.00`. A measured spend goes
// through [RoundMoney] first; a cap is spelled as it was chosen.
func Money(usd float64) string {
	r := microRound(usd)
	if r == 0 && usd > 0 {
		r = usd
	}
	cents := math.Round(r * 100)
	var plain string
	switch {
	case math.Abs(cents-r*100) > 1e-6:
		plain = strconv.FormatFloat(r, 'f', -1, 64)
	case math.Mod(cents, 100) == 0:
		plain = strconv.FormatFloat(cents/100, 'f', 0, 64)
	default:
		plain = strconv.FormatFloat(cents/100, 'f', 2, 64)
	}
	whole, frac, _ := strings.Cut(plain, ".")
	if frac != "" {
		frac = "." + frac
	}
	return "$" + groupThousands(whole) + frac
}

// RaiseTo is the ceiling a cap packet offers for a pool at ceiling: twice it,
// to the millionth, and ALWAYS larger than the ceiling it raises, because a
// raise to the same figure (a sub-cent cap rounded to the cent was `Raise to
// $0`) is a button that lifts nothing.
func RaiseTo(ceiling float64) float64 {
	if r := microRound(ceiling * 2); r > ceiling {
		return r
	}
	return ceiling * 2
}

// groupThousands puts a comma between each group of three digits.
func groupThousands(digits string) string {
	neg := strings.HasPrefix(digits, "-")
	digits = strings.TrimPrefix(digits, "-")
	if len(digits) <= 3 {
		if neg {
			return "-" + digits
		}
		return digits
	}
	var b strings.Builder
	if neg {
		b.WriteByte('-')
	}
	head := len(digits) % 3
	if head > 0 {
		b.WriteString(digits[:head])
	}
	for i := head; i < len(digits); i += 3 {
		if b.Len() > 0 && !(neg && b.Len() == 1) {
			b.WriteByte(',')
		}
		b.WriteString(digits[i : i+3])
	}
	return b.String()
}
