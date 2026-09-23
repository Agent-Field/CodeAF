// Package billing reads invoice totals from the billing service.
package billing

import (
	"strings"

	"bloop/shop/legacy"
)

// InvoiceTotal answers an invoice's total as the service spells it. An
// invoice the service has never heard of totals 0.00.
func InvoiceTotal(id string) (string, error) {
	body, err := legacy.Fetch("https://billing.local/invoices/"+id, 2)
	if err != nil {
		return "", err
	}
	if body == "" {
		return "0.00", nil
	}
	return strings.TrimSpace(body), nil
}
