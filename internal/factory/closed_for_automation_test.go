package factory

import (
	"context"
	"testing"
	"time"
)

func TestAReopenedIssueIsWorkedAgainWithinTheKeep(t *testing.T) {
	now := time.Now()
	automaticNow = func() time.Time { return now }
	t.Cleanup(func() { automaticNow = time.Now })
	how := "closed"
	calls := 0
	standing := func(context.Context, Item) (SourceStanding, error) { calls++; return SourceStanding{How: how}, nil }
	it := Item{Origin: OriginForge, Repo: "acme/api", Num: 7}
	root := t.TempDir()
	if !ClosedForAutomation(context.Background(), root, it, standing) {
		t.Fatal("a closed issue should stop automatic work")
	}
	how = "open"
	if !ClosedForAutomation(context.Background(), root, it, standing) || calls != 1 {
		t.Fatalf("inside the keep: calls=%d, want the remembered answer", calls)
	}
	now = now.Add(automaticClosedKeep + time.Second)
	if ClosedForAutomation(context.Background(), root, it, standing) {
		t.Fatal("an issue reopened on GitHub stayed skipped")
	}
	if calls != 2 {
		t.Fatalf("calls=%d, want one fresh read", calls)
	}
}
