// Package export writes the ledger out as CSV for the bank's importer.
package export

import (
	"strings"

	"bloop/ledger/money"
)

// Row is one ledger line.
type Row struct {
	Date  string
	Memo  string
	Cents int64
}

// CSV writes one line per row: date, memo, amount.
func CSV(rows []Row) string {
	var b strings.Builder
	for _, row := range rows {
		b.WriteString(row.Date)
		b.WriteString(",")
		b.WriteString(row.Memo)
		b.WriteString(",")
		b.WriteString(money.Format(row.Cents))
		b.WriteString("\n")
	}
	return b.String()
}
