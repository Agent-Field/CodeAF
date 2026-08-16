package store

import (
	"path/filepath"
	"strings"
	"testing"
)

// Everything else in this database is searchable. The conversation was not, so
// anything the person only ever SAID — never spliced into a job — was
// unreachable by every read in the product, and the head answered "remember that
// pricing analysis from January?" out of a twenty-message window that did not
// contain January.
func TestConversationIsFoundByItsWordsMonthsLater(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "graph.db"))

	old, err := graph.PostMessage(Message{SessionID: "s1", Role: RoleUser,
		Body: "the pricing analysis said the enterprise ladder beats per-seat above forty users"})
	if err != nil {
		t.Fatal(err)
	}
	for index := range 40 {
		if _, err := graph.PostMessage(Message{SessionID: "s1", Role: RoleAgent,
			Body: "ordinary chatter number " + string(rune('a'+index%26))}); err != nil {
			t.Fatal(err)
		}
	}

	// Not the original sentence: the words a person reaches for months later.
	hits, err := graph.SearchMessages("what did we decide about enterprise pricing", "", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("the conversation index found nothing")
	}
	if hits[0].Seq != old.Seq {
		t.Fatalf("top hit was #%d (%q), want the pricing line #%d", hits[0].Seq, hits[0].Body, old.Seq)
	}
	if hits[0].Role != RoleUser || hits[0].SessionID != "s1" || hits[0].Age == "" {
		t.Fatalf("a hit could not say who said it or when: %+v", hits[0])
	}

	// Scoped to a thread that never held it, the honest answer is nothing.
	scoped, err := graph.SearchMessages("enterprise pricing", "s2", 5)
	if err != nil || len(scoped) != 0 {
		t.Fatalf("session-scoped search leaked another thread: %+v err=%v", scoped, err)
	}
}

// The index is a materialized view of the journal like every other one here, so
// a rebuild must reproduce it exactly rather than quietly losing the past.
func TestConversationIndexSurvivesRebuild(t *testing.T) {
	path := filepath.Join(t.TempDir(), "graph.db")
	graph := openTestStore(t, path)
	if _, err := graph.PostMessage(Message{SessionID: "s1", Role: RoleUser,
		Body: "let's call the migration cutover the beacon window"}); err != nil {
		t.Fatal(err)
	}
	before, err := graph.SearchMessages("beacon window cutover", "", 5)
	if err != nil || len(before) != 1 {
		t.Fatalf("before rebuild = %+v err=%v", before, err)
	}
	if err := graph.Rebuild(); err != nil {
		t.Fatal(err)
	}
	after, err := graph.SearchMessages("beacon window cutover", "", 5)
	if err != nil || len(after) != 1 || after[0].Seq != before[0].Seq {
		t.Fatalf("after rebuild = %+v err=%v, want the same single hit", after, err)
	}
}

// A database written before the index existed is exactly the one worth
// searching. An index that only covered what was said after the upgrade would
// be honest about nothing.
func TestTheConversationIndexBackfillsAnOlderDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	graph := openTestStore(t, path)
	said, err := graph.PostMessage(Message{SessionID: "s1", Role: RoleUser,
		Body: "we settled on the seat ladder for the january pricing analysis"})
	if err != nil {
		t.Fatal(err)
	}
	// Rewind the database to before this index existed.
	if _, err := graph.db.Exec(`DROP TABLE messages_fts`); err != nil {
		t.Fatal(err)
	}
	if err := graph.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	hits, err := reopened.SearchMessages("january pricing analysis", "", 5)
	if err != nil || len(hits) != 1 || hits[0].Seq != said.Seq {
		t.Fatalf("backfilled search = %+v err=%v", hits, err)
	}
	if !strings.Contains(hits[0].Body, "seat ladder") {
		t.Fatalf("backfilled hit lost its body: %+v", hits[0])
	}
}
