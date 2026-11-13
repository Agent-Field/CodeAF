package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/pool/poolcfg"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestAppFactoryModelPoolRequiresOptIn(t *testing.T) {
	for _, tc := range []struct {
		name, saved, env, ci string
		want                 poolcfg.Mode
		source               string
	}{
		{name: "never chose", want: poolcfg.Off, source: "default"},
		{name: "CI never chose", ci: "true", want: poolcfg.Off, source: "default"},
		{name: "saved on", saved: "on", want: poolcfg.On, source: "setting"},
		{name: "saved off", saved: "off", want: poolcfg.Off, source: "setting"},
		{name: "env on", env: "on", saved: "off", want: poolcfg.On, source: "env"},
		{name: "env off", env: "off", saved: "on", want: poolcfg.Off, source: "env"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			profile := t.TempDir()
			if tc.saved != "" {
				raw, _ := json.Marshal(map[string]string{config.KeyModelPool: tc.saved})
				if err := os.WriteFile(filepath.Join(profile, "config.json"), raw, 0600); err != nil {
					t.Fatal(err)
				}
			}
			got := config.ModelPoolResolved(profile, func(key string) (string, bool) {
				value, ok := map[string]string{"CODEAF_MODEL_POOL": tc.env, "CI": tc.ci}[key]
				return value, ok
			})
			if got.Mode != tc.want || got.Source.Mode != tc.source {
				t.Fatalf("pool mode=%s source=%s; want %s from %s", got.Mode, got.Source.Mode, tc.want, tc.source)
			}
			if tc.want == poolcfg.Off && (got.CanRead() || got.CanSend()) {
				t.Fatal("off pool permits routing, judging or records")
			}
		})
	}
	t.Run("configured provider answers and is recorded", func(t *testing.T) {
		s := newLocalSetup(t)
		t.Setenv("CODEAF_MODEL_POOL", "")
		t.Setenv("CI", "")
		const model = "deepseek/deepseek-v4.1-flash"
		var calls atomic.Int64
		endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				fmt.Fprint(w, `{"data":[]}`)
				return
			}
			var req struct {
				Model string `json:"model"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Error(err)
			}
			if req.Model != model {
				t.Errorf("requested model=%q, want %q; pool must not swap vendors", req.Model, model)
			}
			calls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"model": model, "choices": []any{map[string]any{"message": map[string]any{"content": briefDraft}, "finish_reason": "stop"}}, "usage": map[string]any{"prompt_tokens": 20, "completion_tokens": 5, "cost": .01}})
		}))
		t.Cleanup(endpoint.Close)
		profile := v3Profile(t, map[string]any{
			"model.talk": model, "models.tiers.low": model,
			"models.tiers.reflex": model, "models.tiers.worker": model,
			"models.tiers.high": model, "models.tiers.mastermind": model,
		})
		t.Setenv(config.ProfileDirEnv, profile)
		t.Setenv("OPENROUTER_API_KEY", "fixture")
		t.Setenv("CODEAF_BASE_URL", endpoint.URL)
		t.Setenv("CODEAF_MODEL_POOL_URL", endpoint.URL+"/index.json")
		t.Setenv("CODEAF_MODEL_POOL_MIRROR_URL", "")
		t.Setenv("CODEAF_MODEL_POOL_SUBMIT_URL", endpoint.URL+"/rows")
		freshEngineProcess(t)
		s.backend.open = appOpenInProcess
		st := s.start()
		floor := s.backend.local.host.floor
		it, err := floor.Create(factory.Item{Tier: factory.TierOwner, Model: model, Repo: "acme/api", Num: 7, Title: "Fix retries", Kind: factory.KindIssue, State: factory.StateNeedsYou, Cap: 1, Stream: &factory.Stream{Phases: []factory.Phase{{Name: "test", Kind: factory.StageCheck, State: factory.PhaseDone, Round: 1, Result: &factory.StageResult{Done: true, Tests: &factory.TestCounts{Passed: 3}}}}}})
		if err != nil {
			t.Fatal(err)
		}
		var answer appwire.FactoryJobBriefWritten
		s.must("factory.job_brief_write", map[string]any{"factory": st.Factory, "job": it.ID}, &answer)
		if !answer.Kept || answer.Model != model || calls.Load() != 1 {
			t.Fatalf("answer=%+v provider calls=%d", answer, calls.Load())
		}
		if !session.FlushUsage() {
			t.Fatal("usage did not flush")
		}
		rows, err := session.ReadUsage(session.UsageLedgerPath(), time.Time{})
		if err != nil {
			t.Fatal(err)
		}
		recorded := 0
		for _, row := range rows {
			if row.FactoryJob == factory.LedgerName(it) {
				recorded += row.Calls
				if row.Model != model || row.Seat == session.SeatJudge {
					t.Errorf("unexpected recorded model or judge: %+v", row)
				}
			}
		}
		if recorded != 1 {
			t.Fatalf("recorded calls=%d, want 1", recorded)
		}
		// Join the production startup sweep before checking that it wrote nothing.
		closeEngineProcess()
		if _, err := os.Stat(config.ProfilePath(profile, "pool")); !os.IsNotExist(err) {
			t.Fatalf("off pool created records: %v", err)
		}
	})
}
