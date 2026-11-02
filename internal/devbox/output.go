package devbox

import (
	"io"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Terraform's apply hint names a temporary file that codeaf applies and then deletes.
// Buffer lines so split writes and color sequences cannot leave half of that hint behind.
type terraformOutput struct {
	out        io.Writer
	stripColor bool
	pending    string
	applyHint  bool
}

func (w *terraformOutput) Write(p []byte) (int, error) {
	w.pending += string(p)
	for {
		line, rest, found := strings.Cut(w.pending, "\n")
		if !found {
			break
		}
		w.pending = rest
		if err := w.line(line, "\n"); err != nil {
			return 0, err
		}
	}
	return len(p), nil
}

func (w *terraformOutput) line(line, ending string) error {
	plain := ansi.Strip(line)
	clean := strings.TrimSpace(plain)
	if strings.HasPrefix(clean, "To perform exactly these actions, run the following command to apply:") {
		w.applyHint = true
		return nil
	}
	if w.applyHint {
		if clean == "" {
			return nil
		}
		w.applyHint = false
		if strings.HasPrefix(clean, "terraform apply ") {
			return nil
		}
	}
	if w.stripColor {
		line = plain
	}
	_, err := io.WriteString(w.out, line+ending)
	return err
}

func (w *terraformOutput) Flush() {
	if w.pending != "" {
		_ = w.line(w.pending, "")
		w.pending = ""
	}
}
