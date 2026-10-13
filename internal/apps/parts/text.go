package parts

import (
	"fmt"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"unicode/utf8"
)

func validateText(text string, _ *bridge.PartExtra) error {
	if !utf8.ValidString(text) {
		return fmt.Errorf("Markdown must be valid UTF-8")
	}
	return nil
}
