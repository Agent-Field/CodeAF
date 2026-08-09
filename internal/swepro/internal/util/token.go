// Token estimator — port of src/util/token.ts:1-5 (swe-pro 3b25a1a).
package util

import "unicode/utf16"

func EstimateTokens(input string) int {
	units := len(utf16.Encode([]rune(input)))
	return (units + 2) / 4
}
