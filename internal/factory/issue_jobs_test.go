package factory_test

import (
	"context"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/triage"
)

func TestFactoryWorkItemsAreIssuesAndKeepTheirPRObjects(t *testing.T) {
	_, st := openLocal(t)
	issue, err := st.Create(factory.Item{Kind: factory.KindIssue, Repo: "acme/api", Title: "fix", PullRequest: &factory.PullRequest{Number: 65, State: factory.PROpen, URL: "https://github.com/acme/api/pull/65"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []factory.Kind{factory.KindPR, factory.KindCI} {
		legacy, err := st.Create(factory.Item{Kind: kind, Repo: "acme/api", Title: "unsupported"})
		if err != nil {
			t.Fatal(err)
		}
		if triage.Wants(legacy) {
			t.Fatal("paid triage still admits unsupported work")
		}
		if err := run.QueueItem(&legacy, time.Now()); err == nil {
			t.Fatal("unsupported kind can run")
		}
		if _, err := st.Add(context.Background(), factory.Item{Kind: kind, Title: "unsupported"}); err == nil {
			t.Fatal("chat can add unsupported work")
		}
	}
	snap, err := factory.LocalSeam(st, time.Now()).Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Items) != 1 || snap.Items[0].ID != issue.ID {
		t.Fatalf("legacy work leaked: %+v", snap.Items)
	}
	q := api.FromSnapshot(snap, "")
	if len(q.Jobs) != 1 || q.Jobs[0].PR == nil || q.Jobs[0].PR.Number != 65 {
		t.Fatal("issue's linked PR was filtered")
	}
	recipe := factory.DefaultRecipe()
	if len(recipe.ByKind) != 0 {
		t.Fatal("nonissue default pipelines still shipped")
	}
}
