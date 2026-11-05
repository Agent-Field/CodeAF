package serve_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/live"
	"github.com/Agent-Field/codeaf/internal/factory/run"
)

func TestSplitFromValidationDoesNotHydrateParent(t *testing.T) {
	f := newCreateExtrasJobs(t)
	parent := f.create(t, factory.Item{Kind: factory.KindIssue, State: factory.StateNew, Num: 7, Author: "author"})
	before, err := os.ReadFile(f.st.ItemPath(parent.ID))
	if err != nil {
		t.Fatal(err)
	}
	var lookups atomic.Int32
	f.st.SetTierResolver(func(context.Context, factory.Item) (factory.Tier, time.Time, error) {
		lookups.Add(1)
		return factory.TierCollab, time.Time{}, nil
	})
	child := createExtrasJob(t, f, "/v1/jobs", api.NewJob{Repo: "acme/api", Title: "Smaller fix", Source: factory.AskApp, SplitFrom: &parent.ID})
	after, err := os.ReadFile(f.st.ItemPath(parent.ID))
	if err != nil || !bytes.Equal(before, after) || lookups.Load() != 0 || child.SplitFrom != parent.ID {
		t.Fatalf("split changed its parent: lookups=%d split_from=%d err=%v", lookups.Load(), child.SplitFrom, err)
	}
	if read, err := f.st.Get(parent.ID); err != nil || read.Tier != factory.TierCollab || lookups.Load() != 1 {
		t.Fatalf("user read did not hydrate: tier=%q lookups=%d err=%v", read.Tier, lookups.Load(), err)
	}
}

func TestNewJobExtrasKeepTierRefusalAndOutcome(t *testing.T) {
	f := newCreateExtrasJobs(t)
	parent := f.create(t, factory.Item{Kind: factory.KindIssue, State: factory.StateNew})
	f.seam.NewJob = func(repo, title, text string) (int, error) {
		it, err := f.st.Create(factory.Item{Repo: repo, Title: title, Body: text, Kind: factory.KindIssue, State: factory.StateNew,
			Origin: factory.OriginForge, Author: "author", Tier: factory.TierStranger,
			Stages: []factory.Stage{{Name: "plan", Kind: factory.StageChat, On: true}}})
		return it.ID, err
	}
	reopenJobs(t, f)
	budget := 10.0
	body, err := json.Marshal(api.NewJob{Repo: "acme/api", Title: "Smaller fix", Source: factory.AskApp,
		Start: "now", Group: "g", Milestone: "release", Budget: &budget, SplitFrom: &parent.ID})
	if err != nil {
		t.Fatal(err)
	}
	w := verbRequest(f.h, http.MethodPost, "/v1/jobs", string(body))
	checkJSON(t, w, http.StatusConflict)
	var refusal struct{ Error string }
	if err := json.Unmarshal(w.Body.Bytes(), &refusal); err != nil || refusal.Error != (run.WriteAccessRefusal{}).Error() {
		t.Fatalf("tier refusal: %s err=%v", w.Body, err)
	}
	jobs, err := f.st.ListSaved()
	if err != nil || len(jobs) != 2 {
		t.Fatalf("created jobs=%d err=%v", len(jobs), err)
	}
	child := jobs[0]
	if child.ID == parent.ID || child.SplitFrom != parent.ID || child.Group != "g" || child.Milestone != "release" || child.Cap != budget || child.Stream != nil || child.Failure == nil {
		t.Fatalf("refused child lost its extras or started: %+v", child)
	}
	zero := int64(0)
	out, err := live.ReadOutcomes(f.st.Root(), &zero, 100)
	if err != nil || out.Reset || len(out.Events) != 1 {
		t.Fatalf("tier outcomes: %+v err=%v", out, err)
	}
	event := out.Events[0]
	if event.Job != child.ID || event.Kind != "failed" || event.Group != "g" || event.Code != factory.CodeToolError || event.Step != "plan" || !event.At.Equal(child.Failure.At) {
		t.Fatalf("tier outcome lost saved facts: %+v", event)
	}
}
