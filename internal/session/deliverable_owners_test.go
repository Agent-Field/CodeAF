package session

import (
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestLineNamesSessionMatchesPerOwnerSearch(t *testing.T) {
	line := []byte(`{"path":"/x/\"session\":\"inside\"","session":"real-1","title":"t"}`)
	if !lineNamesSession(line, map[string]bool{"real-1": true}) {
		t.Fatal("missed the session value")
	}
	if lineNamesSession(line, map[string]bool{"real": true, "inside": true, "real-12": true}) {
		t.Fatal("matched a prefix or text inside a string")
	}
}

// An owner id the row's writer escapes (json.Marshal turns & into & and
// escapes a quote and a backslash) is found by both prefilters: the per-owner
// needle of a small set and the by-value read of a large one. Before, either
// searched for the raw bytes and an escaped id never met its own rows.
func TestEscapedOwnersAreFoundByBothPrefilters(t *testing.T) {
	index := filepath.Join(t.TempDir(), "artifacts.jsonl")
	escaped := []string{"a&b", `q"x`, `back\`, "lt<gt>", "line sep", "tab\tid"}
	for _, owner := range escaped {
		fact := DeliverableFact{ID: NewDeliverableID(), At: time.Now(), Origin: DeliverOriginAgent, Authored: true, Run: "r", Turn: "s#1"}
		if !RecordArtifact(index, Artifact{Path: "/w/" + strconv.Itoa(len(owner)) + ".md", Session: owner, Title: "doc.md", Kind: "markdown", Created: time.Now(), Deliver: &fact}) {
			t.Fatal("record")
		}
	}
	// A neighbour whose id is a prefix of an escaped spelling must not match.
	fact := DeliverableFact{ID: NewDeliverableID(), At: time.Now(), Origin: DeliverOriginAgent, Authored: true, Run: "r", Turn: "s#1"}
	RecordArtifact(index, Artifact{Path: "/w/n.md", Session: "a", Title: "n.md", Kind: "markdown", Created: time.Now(), Deliver: &fact})
	for _, owner := range escaped {
		small := map[string]bool{owner: true}
		large := map[string]bool{owner: true}
		for i := 0; i <= deliverNeedleMax; i++ {
			large["pad"+strconv.Itoa(i)] = true
		}
		for name, set := range map[string]map[string]bool{"needle": small, "by value": large} {
			page, err := ListDeliverables(index, DeliverableQuery{Sessions: set}, "", 10, time.Now().Add(5*time.Second), 0, nil)
			if err != nil || len(page.Rows) != 1 || page.Rows[0].Artifact.Session != owner {
				t.Errorf("%s %q: %d rows, err %v", name, owner, len(page.Rows), err)
			}
		}
	}
}

// The by-value prefilter reads an escaped value whole: an escaped quote does
// not end it, so neither the part before it nor a different id matches.
func TestLineNamesSessionReadsEscapedValuesWhole(t *testing.T) {
	line := []byte(`{"session":"q\"x","title":"t"}`)
	if !lineNamesSession(line, sessionValues(map[string]bool{`q"x`: true})) {
		t.Fatal("missed an escaped quote id")
	}
	if lineNamesSession(line, sessionValues(map[string]bool{"q": true, `q\`: true})) {
		t.Fatal("matched a cut escaped value")
	}
	if lineNamesSession([]byte(`{"session":"back\\`), sessionValues(map[string]bool{`back\`: true})) {
		t.Fatal("matched an unterminated value")
	}
}
