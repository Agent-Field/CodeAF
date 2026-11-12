package appwire

import (
	"encoding/json"
	"strings"
	"testing"
)

// C3, C6, C12: all three lifecycle notices use the existing footer entry shape,
// retaining their wire kinds when a saved conversation is read again.
func TestAppCloseFooterKindsKeepTheirWireWords(t *testing.T) {
	for kind, expected := range map[string]string{
		FooterResumedAfterClose: "resumed_after_close",
		FooterStoppedByClose:    "stopped_by_close",
		FooterResumedAfterQuit:  "resumed_after_quit",
	} {
		if kind != expected {
			t.Fatalf("footer kind=%q, want %q", kind, expected)
		}
		raw, err := json.Marshal(Entry{Kind: EntryFooter, Footer: kind})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), `"footer_kind":"`+kind+`"`) {
			t.Fatalf("footer=%s", raw)
		}
		var read Entry
		if err := json.Unmarshal(raw, &read); err != nil || read.Kind != EntryFooter || read.Footer != kind {
			t.Fatalf("read=%+v,%v", read, err)
		}
	}
}
