package main

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
)

// A local job's display and step requests must never point at an unrelated
// GitHub issue. Its saved identity still addresses the same job.
func TestAppFactoryJobWithoutIssueHasAnHonestReference(t *testing.T) {
	s := newLocalSetup(t)
	// The start reads a valid recipe; missing-file budget recovery is unrelated.
	s.backend.local.rules = func(context.Context, string, string) (pipeline.File, error) {
		return pipeline.Defaults(), nil
	}
	keys := config.NewAppCredentials()
	keys.Set("openrouter", "test-key-not-real")
	t.Cleanup(config.UseAppCredentials(keys))
	maker := &factoryrun.FakeMaker{Script: func(ctx context.Context, c *factoryrun.FakeConversation) {
		_ = c.Spec.Stage.Report(ctx, factory.StageResult{Done: true, Output: "Checked the job."})
	}}
	hook := s.backend.local.seamHook
	s.backend.local.seamHook = func(seam factory.Seam) factory.Seam {
		seam = hook(seam)
		seam.Launch = func(id int) error {
			it, err := s.backend.local.host.floor.Get(id)
			if err != nil {
				return err
			}
			for _, name := range []string{"plan", "review", "proof"} {
				_, err = factoryrun.NewChatExecutor(maker).Run(context.Background(), factoryrun.Job{Item: it, Stage: factory.Stage{Name: name, Kind: factory.StageChat, Ask: "Check the job", On: true}, Round: 1})
				if err != nil {
					return err
				}
			}
			return nil
		}
		return seam
	}
	st := s.start()
	floor := s.backend.local.host.floor
	if err := floor.SetCheckout("acme/api", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	var local factory.Item
	for range 21 {
		var err error
		local, err = floor.Create(factory.Item{Tier: factory.TierOwner, Repo: "acme/api", Kind: factory.KindIssue, Origin: factory.OriginForge, Title: "Typed work", State: factory.StateNew})
		if err != nil {
			t.Fatal(err)
		}
	}
	if local.ID != 21 {
		t.Fatalf("fixture job id = %d, want 21", local.ID)
	}
	var got api.Job
	s.must("factory.job", map[string]any{"factory": st.Factory, "job": local.ID}, &got)
	if got.Ref != "job 21" {
		t.Errorf("local ref = %q, want job 21 without a GitHub number", got.Ref)
	}
	if got.JobID != "api#21" || got.ID != 21 {
		t.Errorf("saved identity changed: id=%q job_id=%d", got.JobID, got.ID)
	}
	before := len(s.github.seen())
	msg, detail := (&person{w: s.factoryWire}).refused(t, "factory.issue_comment", map[string]any{"factory": st.Factory, "job": local.ID, "body": "hello"}, appwire.CodeConflict)
	if detail == nil || detail.Outcome != api.CommentRefused || !strings.Contains(msg, "no GitHub issue") || len(s.github.seen()) != before {
		t.Errorf("comment refusal = %q, GitHub calls = %d", msg, len(s.github.seen())-before)
	}
	s.must("factory.job_start", map[string]any{"factory": st.Factory, "job": local.ID, "expect": map[string]any{"version": got.Version}}, &appwire.FactoryJobActed{})
	convs := maker.Opened()
	if len(convs) != 3 {
		t.Fatalf("fake model received %d step requests, want 3", len(convs))
	}
	for _, c := range convs {
		if strings.Contains(c.Spec.Brief, "issue #21") || !strings.Contains(c.Spec.Brief, "no GitHub issue") {
			t.Errorf("%s request misidentifies local job: %s", c.Spec.Name, c.Spec.Brief)
		}
	}
	real, err := floor.Create(factory.Item{Tier: factory.TierOwner, Repo: "acme/api", Kind: factory.KindIssue, Origin: factory.OriginForge, Num: 87, Title: "A GitHub issue", State: factory.StateNew})
	if err != nil {
		t.Fatal(err)
	}
	s.must("factory.job", map[string]any{"factory": st.Factory, "job": real.ID}, &got)
	if got.Ref != "api#87" {
		t.Errorf("real issue ref = %q, want api#87", got.Ref)
	}
	// Old HTTP reference keys still resolve, independently of display text.
	var old api.Job
	err = s.backend.factoryHub().read(context.Background(), st.Factory, "/v1/jobs/"+url.PathEscape("api#21"), appFactoryBody, &old)
	if err != nil || old.ID != 21 {
		t.Fatalf("old reference no longer reads job 21: %+v (%v)", old, err)
	}
}
