// Package money is where amounts are formatted.
package money

import "fmt"

// Format renders an amount held in cents: 123456 is "1234.56" and -5 is
// "-0.05".
func Format(cents int64) string {
	sign := ""
	if cents < 0 {
		sign = "-"
		cents = -cents
	}
	return fmt.Sprintf("%s%d.%02d", sign, cents/100, cents%100)
}
