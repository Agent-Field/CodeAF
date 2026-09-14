package session

// THE BELT'S RESULT CAPS FOLLOW THE MODEL'S WINDOW, and every sentence that
// quotes one is rendered from the pair in force.
//
// The defect this closes was measured rather than reasoned about: a flat 50KB
// cut meant one `read` of one file was 78% of everything a 16k model could
// hold, so the file arrived and there was no room left to think about it. The
// frontier case must not move a byte — the caps are in message[0], and a byte
// there re-prices the whole conversation cold — so a 128k window still lands
// exactly pi's own pair.

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/exec/bare"
)

func TestTheBeltsCapsFollowTheWindow(t *testing.T) {
	// The window nobody named. defaultContextWindow is 128,000 tokens, which is
	// the window pi's caps were measured against, so nothing moves.
	ordinary, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	if got := ordinary.resultCaps(); got != bare.DefaultCaps() {
		t.Fatalf("the default window got %+v, want pi's own %+v", got, bare.DefaultCaps())
	}
	for _, name := range []string{"read", "bash"} {
		description := beltTool(t, ordinary, name).Description
		if !strings.Contains(description, "2000 lines") || !strings.Contains(description, "50KB") {
			t.Errorf("%s stopped quoting pi's caps on a 128k model: %q", name, description)
		}
	}

	small, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.ContextWindow = 16_000
	})
	caps := small.resultCaps()
	if caps.MaxBytes >= bare.DefaultCaps().MaxBytes || caps.MaxLines >= bare.DefaultCaps().MaxLines {
		t.Fatalf("a 16k window got %+v, which is no smaller than pi's", caps)
	}
	// Every hand that cuts a result quotes the pair it is cutting at. read and
	// bash are pi's own; read_document is aforge's and mirrors the same law on
	// extracted text, which is what makes the offset it hands back usable.
	for _, name := range []string{"read", "bash", "read_document"} {
		description := beltTool(t, small, name).Description
		if strings.Contains(description, "2000 lines") || strings.Contains(description, "50KB") {
			t.Errorf("%s still promises pi's caps on a 16k model: %q", name, description)
		}
	}
}

// And the extracted-text path is cut by the same pair as the file path, so a
// PDF and a source file page the same way on the same model.
func TestExtractedTextIsCutAtTheBeltsCaps(t *testing.T) {
	small, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.ContextWindow = 16_000
	})
	caps := small.resultCaps()
	text := strings.Repeat("a line of extracted text\n", caps.MaxLines+50)
	cut := piReadLaw(caps, text, nil, nil)
	if strings.Count(cut, "\n") > caps.MaxLines+3 {
		t.Fatalf("extracted text kept %d lines against a %d-line cap", strings.Count(cut, "\n"), caps.MaxLines)
	}
	if !strings.Contains(cut, "Use offset=") {
		t.Fatalf("the cut lost its continuation: %q", cut[max(0, len(cut)-120):])
	}
}
