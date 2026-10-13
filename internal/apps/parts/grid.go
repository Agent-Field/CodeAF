package parts

import (
	"encoding/csv"
	"fmt"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"regexp"
	"strings"
	"unicode/utf8"
)

var cellAddress = regexp.MustCompile(`^[A-Z]+[1-9][0-9]*$`)

func validateGrid(text string, extra *bridge.PartExtra) error {
	if !utf8.ValidString(text) {
		return fmt.Errorf("CSV must be valid UTF-8")
	}
	if _, err := csv.NewReader(strings.NewReader(text)).ReadAll(); err != nil {
		return fmt.Errorf("invalid CSV: %w", err)
	}
	if extra == nil {
		return nil
	}
	for cell, formula := range extra.Formulas {
		if !cellAddress.MatchString(cell) || !strings.HasPrefix(formula, "=") {
			return fmt.Errorf("formulas must map cell addresses to formulas starting with =")
		}
	}
	return nil
}
