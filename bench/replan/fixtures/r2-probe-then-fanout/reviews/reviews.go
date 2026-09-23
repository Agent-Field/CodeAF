// Package reviews counts the reviews a product has.
package reviews

import (
	"strconv"
	"strings"

	"bloop/shop/legacy"
)

// Count answers how many reviews the product has. A product nobody has
// reviewed has none.
func Count(sku string) (int, error) {
	body, err := legacy.Fetch("https://reviews.local/count/"+sku, 2)
	if err != nil {
		return 0, err
	}
	if body == "" {
		return 0, nil
	}
	return strconv.Atoi(strings.TrimSpace(body))
}
