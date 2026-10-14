package schemas

import (
	"fmt"
	"strings"
)

// A negative consistency verdict claims a verified disagreement at real code
// locations. Its optional schema fields are defaults for holds=true, not a
// license to publish a title without the supporting finding.
func validateConsistency(dimension, title, body, path string, start, end int) error {
	if !strings.EqualFold(strings.TrimSpace(dimension), "consistency-verify") {
		return nil
	}
	if strings.TrimSpace(title) == "" || strings.TrimSpace(body) == "" ||
		strings.TrimSpace(path) == "" || start <= 0 || (end != 0 && end < start) {
		return fmt.Errorf("invalid consistency finding: a defect needs a title, explanation and code location with a valid line range")
	}
	return nil
}

func (f ReviewFinding) ValidateConsistency() error {
	return validateConsistency(f.DimensionID, f.Title, f.Body, f.FilePath, f.LineStart, f.LineEnd)
}

func (f ScoredFinding) ValidateConsistency() error {
	return validateConsistency(f.DimensionID, f.Title, f.Body, f.FilePath, f.LineStart, f.LineEnd)
}
