//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/builder"
	"github.com/Agent-Field/codeaf/internal/apps/schema"
	"github.com/Agent-Field/codeaf/internal/apps/store"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/redact"
)

const fluidBuilderModel = "deepseek/deepseek-v4.1-flash"

func startFluidBuilder(t *testing.T) (*fleEngine, string) {
	t.Helper()
	if os.Getenv("FLUID_BUILDER_E2E") != "1" {
		t.Skip("set FLUID_BUILDER_E2E=1 to build apps on OpenRouter")
	}
	key := liveKey(t)
	home := t.TempDir()
	if err := config.WriteAPIKey(home, key); err != nil {
		t.Fatal("could not copy the profile key to the test home")
	}
	bin := binary(t)
	engine := startFLEEngine(t, bin, home, []string{
		"PATH=" + os.Getenv("PATH"), "HOME=" + home, "TMPDIR=" + home,
		config.ProfileDirEnv + "=" + home, "CODEAF_APPS_BUILDER_MODEL=" + fluidBuilderModel,
	}, filepath.Dir(home))
	t.Cleanup(engine.close)
	return engine, home
}

func TestFluidAppScopedPlanLive(t *testing.T) {
	engine, home := startFluidBuilder(t)
	fluidCall(t, engine, "apps.kit", nil, nil)
	args, _ := json.Marshal(builder.PlanRequest{Ask: "crm for my startup. deals, companies, contacts. remind me about deals nobody touched in 2 weeks and draft a follow up."})
	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Second)
	defer cancel()
	command := guardedCommand(t, ctx, home, []string{
		"PATH=" + os.Getenv("PATH"), "HOME=" + home, "TMPDIR=" + home,
	}, binary(t), "app", "--home", home, "plan", string(args))
	raw, err := command.Output()
	if err != nil {
		var jobs struct{ Builds []builder.Summary }
		fluidCall(t, engine, "apps.list", nil, &jobs)
		for _, summary := range jobs.Builds {
			var state builder.State
			fluidCall(t, engine, "apps.build_state", map[string]string{"draft": summary.Draft}, &state)
			t.Logf("job %s: %s (%v)", state.State, state.Problem, state.Progress)
		}
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			t.Log(redact.Secrets(string(exit.Stderr)))
		}
		t.Fatalf("the app command did not return a plan: %v", err)
	}
	var plan builder.PlanResult
	if err := json.Unmarshal(raw, &plan); err != nil || plan.Draft == "" || !json.Valid(plan.Plan) {
		t.Fatalf("the app command returned an invalid plan: %v", err)
	}
	var state builder.State
	fluidCall(t, engine, "apps.build_state", map[string]string{"draft": plan.Draft}, &state)
	if state.State != builder.Planning || len(state.Screens) != 0 || state.Published || len(state.Checkpoints) != 1 || len(state.Needs) == 0 {
		t.Fatalf("scoped planning didn't leave a reviewable unpublished plan: %+v", state)
	}
	t.Logf("Scoped plan with %d needs and %d checkpoint(s)", len(state.Needs), len(state.Checkpoints))
}

func TestFluidAppBuilderLive(t *testing.T) {
	engine, _ := startFluidBuilder(t)

	var crm store.Meta
	t.Run("founder_crm", func(t *testing.T) {
		draft := fluidPlan(t, engine, builder.PlanRequest{Ask: "crm for my startup. deals, companies, contacts. remind me about deals nobody touched in 2 weeks and draft a follow up."})
		fluidBuild(t, engine, draft.Draft)
		preview := fluidPreview(t, engine, draft.Draft)
		app, err := schema.ParseApp(preview.App)
		if err != nil || len(app.Triggers) == 0 {
			t.Fatalf("CRM needs a scheduled follow-up: %v", err)
		}
		crm = fluidKeep(t, engine, draft.Draft)
	})

	t.Run("hiring_reuses_contacts", func(t *testing.T) {
		if crm.ID == "" {
			t.Skip("the CRM has to be kept before Hiring can share Contacts")
		}
		draft := fluidPlan(t, engine, builder.PlanRequest{Ask: "hiring tracker for my first 5 engineers"})
		if !slices.ContainsFunc(draft.Offers, func(o builder.Offer) bool { return o.Type == "contact" && o.App == crm.ID }) {
			t.Fatal("the plan did not offer the CRM's Contacts")
		}
		fluidPlan(t, engine, builder.PlanRequest{Draft: draft.Draft, Request: "Use my existing Contacts, and link each candidate to its contact."})
		fluidBuild(t, engine, draft.Draft)
		preview := fluidPreview(t, engine, draft.Draft)
		app, err := schema.ParseApp(preview.App)
		if err != nil || !app.Records["contact"].Uses {
			t.Fatalf("Hiring did not share Contacts: %v", err)
		}
		fluidKeep(t, engine, draft.Draft)
	})

	t.Run("personal_finance", func(t *testing.T) {
		t.Parallel()
		draft := fluidPlan(t, engine, builder.PlanRequest{Ask: "track my spending from my bank csv, categories, monthly totals"})
		fluidBuild(t, engine, draft.Draft)
		fluidPreview(t, engine, draft.Draft)
		fluidKeep(t, engine, draft.Draft)
	})
	t.Run("release_tracker", func(t *testing.T) {
		t.Parallel()
		draft := fluidPlan(t, engine, builder.PlanRequest{Ask: "release tracker: releases, the PRs in each, a checklist before shipping, incidents after"})
		fluidBuild(t, engine, draft.Draft)
		fluidPreview(t, engine, draft.Draft)
		fluidKeep(t, engine, draft.Draft)
	})

	t.Run("change_and_revert", func(t *testing.T) {
		if crm.ID == "" {
			t.Skip("the CRM has to be kept before it can be changed and reverted")
		}
		var draft builder.PlanResult
		fluidCall(t, engine, "apps.change", map[string]string{"app": crm.ID, "request": "add a renewal date and a board by owner"}, &draft)
		fluidWait(t, engine, draft.Draft)
		fluidPreview(t, engine, draft.Draft)
		changed := fluidKeep(t, engine, draft.Draft)
		if changed.ID != crm.ID || changed.Version != 2 {
			t.Fatalf("change publishes the next CRM version: %+v", changed)
		}
		var reverted struct{ Meta store.Meta }
		fluidCall(t, engine, "apps.revert", map[string]any{"app": crm.ID, "n": 1}, &reverted)
		if reverted.Meta.ID != crm.ID || reverted.Meta.Version != 3 {
			t.Fatalf("revert publishes a new CRM version: %+v", reverted.Meta)
		}
	})
}

func fluidCall(t *testing.T, engine *fleEngine, method string, args, into any) {
	t.Helper()
	raw, err := engine.call(method, args, 300*time.Second)
	if err != nil {
		t.Fatalf("%s: %v", method, err)
	}
	if into != nil {
		if err := json.Unmarshal(raw, into); err != nil {
			t.Fatalf("%s returned invalid output: %v", method, err)
		}
	}
}

func fluidPlan(t *testing.T, engine *fleEngine, args builder.PlanRequest) builder.PlanResult {
	t.Helper()
	var result builder.PlanResult
	fluidCall(t, engine, "apps.plan", args, &result)
	if result.Draft == "" || !json.Valid(result.Plan) {
		t.Fatal("the Builder did not return a plan")
	}
	return result
}

func fluidBuild(t *testing.T, engine *fleEngine, draft string) {
	t.Helper()
	fluidCall(t, engine, "apps.build", map[string]string{"draft": draft}, nil)
	fluidWait(t, engine, draft)
}

func fluidWait(t *testing.T, engine *fleEngine, draft string) {
	t.Helper()
	timer := time.NewTimer(7 * time.Minute)
	defer timer.Stop()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		var state builder.State
		fluidCall(t, engine, "apps.build_state", map[string]string{"draft": draft}, &state)
		round := 0
		var report *schema.Report
		for _, event := range state.Progress {
			round = max(round, event.Round)
			if event.Report != nil {
				report = event.Report
			}
		}
		switch state.State {
		case builder.Ready:
			if round < 1 || round > 3 || report == nil || !report.OK() {
				t.Fatalf("invalid completion: rounds=%d report=%v", round, report)
			}
			t.Logf("Check passed in %d round(s)", round)
			return
		case builder.Failed, builder.NeedsYou:
			fail := state.Problem
			if report != nil {
				fail = fmt.Sprintf("%s (%v)", fail, report.Problems)
			}
			t.Fatalf("Check ended at %s after %d round(s): %s", state.State, round, fail)
		}
		select {
		case <-timer.C:
			t.Fatalf("app still %s at round %d", state.State, round)
		case <-ticker.C:
		}
	}
}

func fluidPreview(t *testing.T, engine *fleEngine, draft string) builder.Preview {
	t.Helper()
	var preview builder.Preview
	fluidCall(t, engine, "apps.preview", builder.PreviewRequest{Draft: draft}, &preview)
	if !preview.Sample || preview.Scope == "" || len(preview.Screens) == 0 {
		t.Fatal("a checked app had no sample preview")
	}
	for _, screen := range preview.Screens {
		var response struct{ Screen json.RawMessage }
		fluidCall(t, engine, "apps.screen", map[string]string{"screen": screen, "draft": preview.Scope}, &response)
		parsed, err := schema.ParseScreen(response.Screen)
		if err != nil || parsed.Screen != screen {
			t.Fatalf("preview screen %q is invalid: %v", screen, err)
		}
	}
	return preview
}

func fluidKeep(t *testing.T, engine *fleEngine, draft string) store.Meta {
	t.Helper()
	var result struct{ Meta store.Meta }
	fluidCall(t, engine, "apps.apply", map[string]string{"draft": draft}, &result)
	return result.Meta
}
