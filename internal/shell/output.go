package shell

import "strings"

// OutputText is the text boundary for captured command output sent to a model.
// Normalize after capture so a CRLF split across pipe reads is still one LF.
// Lone carriage returns retain their progress-display meaning.
func OutputText(text string) string {
	return strings.ReplaceAll(text, "\r\n", "\n")
}
