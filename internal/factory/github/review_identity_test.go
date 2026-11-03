package github_test

import (
 "context"
 "testing"
 "time"
 "github.com/Agent-Field/codeaf/internal/factory"
 source "github.com/Agent-Field/codeaf/internal/factory/github"
)

func TestReviewRefetchPinsFirstDirectIdentity(t *testing.T) {
 f := newIssueScript(t, 1)
 fresh, err := f.src.Refetch(context.Background(), f.row(1))
 if err != nil { t.Fatal(err) }
 if err := source.Merge(f.st, source.Name, []factory.Item{fresh}); err != nil { t.Fatal(err) }
 if got := f.row(1).GitHubIssueID; got != 1001 { t.Errorf("first direct read lost permanent identity: got %d, want 1001", got) }
 f.identity[1] = 9001
 f.state[1] = "closed"
 f.list(1, "closed")
 f.poll()
 if got := f.row(1); got.State != factory.StateNew { t.Fatalf("repo name reuse finished original row: state=%s pinned_id=%d, want new and id=1001", got.State, got.GitHubIssueID) }
}

func TestReviewRepoReuseListCannotChangePinnedRow(t *testing.T) {
 f := newIssueScript(t, 1)
 f.poll()
 before := f.row(1)
 f.identity[1] = 9001
 f.list(1, "open")
 f.lists[0]["title"] = "replacement issue"
 f.poll()
 if got := f.row(1); got.Title != before.Title { t.Fatalf("repo name reuse changed original row title: got %q, want %q (pinned_id=%d)", got.Title, before.Title, got.GitHubIssueID) }
}

func TestReviewFailuresCannotStarveOldestHealthyNominee(t *testing.T) {
 f := newIssueScript(t, 41)
 for n := 1; n <= 40; n++ { f.status[n] = 404 }
 f.state[41] = "closed"
 for poll := 0; poll < 20; poll++ { f.now = f.now.Add(9 * time.Minute); f.poll() }
 for _, n := range f.reads { if n == 41 { return } }
 t.Fatalf("healthy issue #41 starved for 20 polls: %d direct reads, zero for #41; state=%s", len(f.reads), f.row(41).State)
}

func TestReviewLegacyDisabledStageDoesNotMove(t *testing.T) {
 f := newIssueScript(t, 1)
 f.state[1] = "closed"
 if err := f.st.Annotate(1, func(it *factory.Item) error {
  it.Stages = factory.DefaultRecipe().Stages
  it.Stages[0].On = false
  it.PersonTouched = false
  return nil
 }); err != nil { t.Fatal(err) }
 if err := f.st.Update(1, func(it *factory.Item) error { it.PersonTouched = false; return nil }); err != nil { t.Fatal(err) }
 f.poll()
 if f.row(1).State != factory.StateNew { t.Fatal("legacy person's disabled plan step moved to shipped") }
}

func TestReviewStandingRejectsReplacementIssue(t *testing.T) {
 f := newIssueScript(t, 1)
 f.poll()
 f.identity[1], f.state[1] = 9001, "closed"
 it := f.row(1)
 how, err := source.Standing(context.Background(), f.api, it)
 if err == nil && how == source.StandingClosed { t.Fatal("Standing accepted closed replacement identity: pinned=1001 answer=9001") }
 if factory.ClosedForAutomation(context.Background(), f.st.Root(), it, func(ctx context.Context, row factory.Item) (string, error) { return source.Standing(ctx, f.api, row) }) { t.Fatal("replacement identity stopped automation") }
}
