package orclient

// String.prototype.toLowerCase is a FULL Unicode case conversion. Go's
// strings.ToLower uses simple rune mappings, so the two unconditional rules
// from Unicode SpecialCasing.txt have to be applied here as well. Tool-call
// repair runs this operation on provider-controlled names.

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

func jsLowerCase(s string) string {
	ascii := true
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			ascii = false
			break
		}
	}
	if ascii {
		return strings.ToLower(s)
	}

	runes := []rune(s)
	var b strings.Builder
	b.Grow(len(s))
	for i, r := range runes {
		switch {
		case r == 0x0130:
			b.WriteRune('i')
			b.WriteRune(0x0307)
		case r == 0x03A3 && isFinalSigma(runes, i):
			b.WriteRune(0x03C2)
		default:
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

func isFinalSigma(runes []rune, i int) bool {
	j := i - 1
	for j >= 0 && isCaseIgnorable(runes[j]) {
		j--
	}
	if j < 0 || !isCased(runes[j]) {
		return false
	}
	k := i + 1
	for k < len(runes) && isCaseIgnorable(runes[k]) {
		k++
	}
	return k >= len(runes) || !isCased(runes[k])
}

func isCased(r rune) bool {
	return unicode.IsUpper(r) || unicode.IsLower(r) || unicode.IsTitle(r) ||
		unicode.Is(unicode.Other_Lowercase, r) || unicode.Is(unicode.Other_Uppercase, r)
}

func isCaseIgnorable(r rune) bool {
	switch r {
	case '\'', 0x2019, 0x00AD, 0x02B9, 0x0385, 0x1FBF, 0x1FC1, 0x1FCD, 0x1FCE,
		0x1FCF, 0x1FDD, 0x1FDE, 0x1FDF, 0x1FED, 0x1FEE, 0x1FEF, 0x1FFD, 0x1FFE, 0x2027:
		return true
	}
	return unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || unicode.Is(unicode.Cf, r) ||
		unicode.Is(unicode.Lm, r) || unicode.Is(unicode.Sk, r)
}
