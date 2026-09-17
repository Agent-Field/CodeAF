// Package render draws tiny text charts.
package render

import "strings"

// Bar draws one bar of exactly width runes: n filled positions followed by
// dashes to fill the width. More demand than width draws a full bar.
func Bar(n, width int) string {
	if n > width {
		n = width
	}
	if n < 0 {
		n = 0
	}
	return strings.Repeat("#", n) + strings.Repeat("-", width-n+1)
}
