package serve

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// Owner decision 4 (2026-11-17), E3 F6: documents keep History with Restore,
// and the agent may speak of an earlier copy only when History really holds
// one — in plain words, never a number. E3 heard Afi say "(page version 2)".
// ws_file_read hands it exactly the copies the page held before its current
// words: when, and who by name.
func TestTheAgentHearsOnlyRealEarlierCopiesInPlainWords(t *testing.T) {
	name := func(h string) string { return map[string]string{"lena": "Lena Ortiz", "priya": "Priya Raman"}[h] }
	if got := earlierCopies([]store.PageVersion{{Version: 1, At: "2026-11-17T09:00:00Z", By: "priya", Reason: "manual"}}, name); len(got) != 0 {
		t.Fatalf("a page with only its current words has earlier copies %v", got)
	}
	list := []store.PageVersion{
		{Version: 3, At: "2026-11-17T11:00:00Z", By: "priya", Reason: "manual"},
		{Version: 2, At: "2026-11-17T10:00:00Z", By: "priya", Reason: versionReasonKept},
		{Version: 1, At: "2026-11-17T09:00:00Z", By: "lena", Reason: "manual"},
	}
	got := earlierCopies(list, name)
	if len(got) != 1 || got[0]["by"] != "Lena Ortiz" || got[0]["when"] != "2026-11-17T09:00:00Z" {
		t.Fatalf("earlier copies %v; want Lena's first words alone", got)
	}
	for _, c := range got {
		for k := range c {
			if k != "when" && k != "by" {
				t.Fatalf("an earlier copy carries %q, which the agent could repeat", k)
			}
		}
	}
}
