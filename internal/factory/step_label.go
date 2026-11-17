package factory

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// StepLabelMax bounds a label supplied by a structured step edit, in characters.
const StepLabelMax = 200

// CleanStepLabel trims an edit's label. Empty clears it.
func CleanStepLabel(label string) (string, error) {
	label = strings.TrimSpace(label)
	if !utf8.ValidString(label) || utf8.RuneCountInString(label) > StepLabelMax {
		return "", fmt.Errorf("label is valid UTF-8 of at most %d characters", StepLabelMax)
	}
	for _, r := range label {
		if r < 0x20 || r == 0x7f || r == 0x85 || r == 0x2028 || r == 0x2029 || r == 0xfeff {
			return "", fmt.Errorf("label is one line")
		}
	}
	return label, nil
}
