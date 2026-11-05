package github

import (
	"context"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	forge "github.com/Agent-Field/codeaf/internal/praf/github"
)

type tierCacheAPI struct {
	*memAPI
	permission string
	calls      int
}

func (a *tierCacheAPI) Permission(context.Context, string, string, string) (string, error) {
	a.calls++
	return a.permission, nil
}

func (a *tierCacheAPI) Issue(context.Context, string, string, int) (forge.Issue, error) {
	return forge.Issue{ID: 77, Number: 7, User: "outside", Title: "outside issue", State: "open"}, nil
}

// Positive permission expires after fifteen minutes.
func TestRefetchRefreshesExpiredWritePermission(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	a := &tierCacheAPI{memAPI: newMemAPI(now), permission: "write"}
	s := New(a, []string{"acme/api"}, func() time.Time { return now })
	base := factory.Item{Repo: "api", Product: "acme", Num: 7, Kind: factory.KindIssue, Origin: factory.OriginForge}
	first, err := s.Refetch(context.Background(), base)
	if err != nil {
		t.Fatal(err)
	}
	if first.Tier != factory.TierCollab {
		t.Fatalf("initial tier %q", first.Tier)
	}
	a.permission = "read"
	now = now.Add(15*time.Minute - time.Nanosecond)
	cached, err := s.Refetch(context.Background(), first)
	if err != nil {
		t.Fatal(err)
	}
	if cached.Tier != factory.TierCollab || a.calls != 1 {
		t.Fatalf("unexpired permission: tier=%q requests=%d", cached.Tier, a.calls)
	}
	now = now.Add(time.Nanosecond)
	fresh, err := s.Refetch(context.Background(), first)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Tier != factory.TierStranger || a.calls != 2 {
		t.Fatalf("expired permission: tier=%q requests=%d", fresh.Tier, a.calls)
	}
}

// A cached refusal keeps the existing lifetime even if permissions later change.
func TestStrangerPermissionStaysCached(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	a := &tierCacheAPI{memAPI: newMemAPI(now), permission: "read"}
	s := New(a, []string{"acme/api"}, func() time.Time { return now })
	base := factory.Item{Repo: "api", Product: "acme", Num: 7, Kind: factory.KindIssue, Origin: factory.OriginForge}
	first, err := s.Refetch(context.Background(), base)
	if err != nil {
		t.Fatal(err)
	}
	a.permission = "write"
	now = now.Add(24 * time.Hour)
	fresh, err := s.Refetch(context.Background(), first)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Tier != factory.TierStranger || a.calls != 1 {
		t.Fatalf("cached refusal: tier=%q requests=%d", fresh.Tier, a.calls)
	}
}
