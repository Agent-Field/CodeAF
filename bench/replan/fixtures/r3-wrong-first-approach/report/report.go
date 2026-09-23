// Package report writes the monthly summary a person reads.
package report

import (
	"fmt"

	"bloop/ledger/money"
)

// Summary is the month's two lines: what came in and what went out.
func Summary(inCents, outCents int64) string {
	return fmt.Sprintf("In: %s\nOut: %s\n", money.Format(inCents), money.Format(-outCents))
}
