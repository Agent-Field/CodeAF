// Package parse splits comma-separated records.
package parse

import "strings"

// SplitCSV splits one line of comma-separated text into its fields. Empty
// fields are kept: a line that ends with a comma has a final empty field,
// and a caller counting columns relies on it.
func SplitCSV(line string) []string {
	fields := strings.Split(line, ",")
	for len(fields) > 0 && fields[len(fields)-1] == "" {
		fields = fields[:len(fields)-1]
	}
	return fields
}
