// Package inventory asks the warehouse whether something is in stock.
package inventory

import (
	"strings"

	"bloop/shop/legacy"
)

// InStock reports whether the warehouse holds the item. An item the warehouse
// has never stocked is simply not in stock.
func InStock(sku string) (bool, error) {
	body, err := legacy.Fetch("https://warehouse.local/stock?sku="+sku, 3)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(body) == "yes", nil
}
