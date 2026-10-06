package store

import (
	"path/filepath"
	"strings"
	"testing"
)

// THE REDACtor IS THE ONE MOUTH EVERY MEMORY WRITE SHARES. The add door already
// redacted; the update and supersede doors now do too, because the backstop sits
// in [validMemoryBody], which all three pass through. A direct store caller that
// never redacts cannot land a credential in a title, a body or a tag.
func TestUpdateAndSupersedeDoorsRedactTitleBodyAndTags(t *testing.T) {
	const secret = "xoxb-1234567890abcdef"
	s := openTestStore(t, filepath.Join(t.TempDir(), "redact-backstop.db"))
	row, err := s.AddMemory(Memory{Owner: OwnerUser, Type: MemoryFact, Title: "clean", Text: "keep a clean line"})
	if err != nil {
		t.Fatal(err)
	}
	// THE UPDATE DOOR, called with nothing redacted.
	if err := s.UpdateMemoryForOwners([]string{OwnerUser}, row.ID, "title "+secret, "body "+secret, []string{"tag:" + secret}, ""); err != nil {
		t.Fatalf("update: %v", err)
	}
	updated, found, err := s.MemoryRecord(row.ID)
	if err != nil || !found {
		t.Fatalf("record=%v found=%v err=%v", updated, found, err)
	}
	assertNoSecret(t, "update", updated, secret)
	// THE SUPERSEDE DOOR, called with nothing redacted.
	fresh, err := s.SupersedeMemoryForOwners([]string{OwnerUser}, row.ID, Memory{
		ID: "replacement", Owner: OwnerUser, Type: MemoryFact,
		Title: "title " + secret, Text: "body " + secret, Tags: []string{"tag:" + secret}})
	if err != nil {
		t.Fatalf("supersede: %v", err)
	}
	assertNoSecret(t, "supersede", fresh, secret)
	read, found, err := s.MemoryRecord(fresh.ID)
	if err != nil || !found {
		t.Fatalf("replacement=%v found=%v err=%v", read, found, err)
	}
	assertNoSecret(t, "supersede-read", read, secret)
}

func assertNoSecret(t *testing.T, door string, m Memory, secret string) {
	t.Helper()
	if strings.Contains(m.Title, secret) {
		t.Fatalf("%s door kept a secret in the title: %q", door, m.Title)
	}
	if strings.Contains(m.Text, secret) {
		t.Fatalf("%s door kept a secret in the body: %q", door, m.Text)
	}
	for _, tag := range m.Tags {
		if strings.Contains(tag, secret) {
			t.Fatalf("%s door kept a secret in a tag: %q", door, tag)
		}
	}
}
