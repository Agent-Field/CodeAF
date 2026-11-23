package relayserve

import (
	"bytes"
	"strings"
	"testing"
)

// L4-M8c, L4-M8d: help and the first startup line hide protocol and test machinery.
func TestL4RelayWords(t *testing.T) {
	var out bytes.Buffer
	RelayHelp(&out)
	if !strings.HasPrefix(out.String(), "usage: codeaf relay") {
		t.Fatal(out.String())
	}
	for _, bad := range []string{"test hook", "aforge", "min-grace", "sweep-every", "Usage of"} { // legacy-name
		if strings.Contains(out.String(), bad) {
			t.Fatal(out.String())
		}
	}
	if got := relayStartLine("127.0.0.1:8901"); got != "codeaf relay is listening on 127.0.0.1:8901 — on each computer: codeaf pair --via http://127.0.0.1:8901" {
		t.Fatal(got)
	}
}
