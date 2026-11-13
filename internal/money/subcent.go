// Package money holds the shared precision rule for small dollar amounts.
package money

import "fmt"

// Floor is the smallest amount written as a figure, a hundredth of a cent.
const Floor = 0.0001

// SubCent keeps a positive sub-cent amount from rounding down to zero.
func SubCent(usd float64) string {
	if usd < Floor/2 {
		return "<" + fmt.Sprintf("$%.4f", Floor)
	}
	return fmt.Sprintf("$%.4f", usd)
}
