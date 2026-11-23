package shell

import (
	"runtime"
	"strings"
)

// OutputText is the text boundary for captured command output sent to a model.
// Windows normalizes after capture so CRLF split across reads is still one LF.
// Unix preserves the command's original output bytes.
// Lone carriage returns retain their progress-display meaning.
func OutputText(text string) string {
	if runtime.GOOS != "windows" {
		return text
	}
	return strings.ReplaceAll(text, "\r\n", "\n")
}
