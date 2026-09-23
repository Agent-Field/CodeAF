// Package catalog reads product titles from the catalog service.
package catalog

import "bloop/shop/legacy"

// Title answers the product's title. A product the catalog does not carry is
// shown as "unknown item".
func Title(sku string) (string, error) {
	body, err := legacy.Fetch("https://catalog.local/items/"+sku+"/title", 1)
	if err != nil {
		return "", err
	}
	if body == "" {
		return "unknown item", nil
	}
	return body, nil
}
