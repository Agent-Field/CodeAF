// Package shipping asks the carrier for a delivery quote.
package shipping

import (
	"fmt"
	"strings"

	"bloop/shop/legacy"
)

// Quote answers the carrier's price for delivering to zip. A zip the carrier
// does not serve is an error that names it.
func Quote(zip string) (string, error) {
	body, err := legacy.Fetch("https://carrier.local/quote/"+zip, 1)
	if err != nil {
		return "", err
	}
	if body == "" {
		return "", fmt.Errorf("no shipping to %s", zip)
	}
	return strings.TrimSpace(body), nil
}
