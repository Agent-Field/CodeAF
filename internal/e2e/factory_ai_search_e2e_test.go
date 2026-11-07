//go:build e2e

package e2e

// factory_ai_search_e2e_test.go is the live proof of the jobs search bar's AI
// search (internal/factory/aisearch): the REAL `bin/codeaf app` over a
// throwaway --home, the real santoshkumarradha/factory-demo (about 80 open
// issues) imported as Backlog jobs, and deepseek/deepseek-v4.1-flash.
//
// It WRITES NOTHING TO GITHUB: no issue, branch or pull request. It turns the
// factory's own starting off before it imports, so no job starts, and asks:
//
//  1. the estimate (free), then three real questions, each to the end, with
//     every match, its reason and the cost logged as `AISEARCH MATCH` /
//     `AISEARCH RESULT` lines, and a JSON report in the log folder for a
//     person to grade precision against the issue titles;
//  2. the first question again: every answer is reused and it costs nothing.
//
//	make build
//	FLE2E=1 FLE2E_GH_TOKEN_FILE=~/.fle2e-gh go test -tags e2e -run TestFactoryAISearchE2E -count=1 -timeout 30m -v ./internal/e2e/
//
// FLE2E_HOME keeps the home afterwards (to open the same factory on the
// desktop); by default it is a throwaway.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
)

type aiSearchRun struct {
	Run      string  `json:"run"`
	Question string  `json:"question"`
	Words    string  `json:"words"`
	Phase    string  `json:"phase"`
	Total    int     `json:"total"`
	Read     int     `json:"read"`
	Related  int     `json:"related"`
	Checked  int     `json:"checked"`
	Over     int     `json:"over"`
	Asked    int     `json:"asked"`
	Reused   int     `json:"reused"`
	Failed   int     `json:"failed"`
	USD      float64 `json:"usd"`
	Model    string  `json:"model"`
	Error    string  `json:"error"`
	Matches  []struct {
		Job   int    `json:"job"`
		Ref   string `json:"ref"`
		Title string `json:"title"`
		Why   string `json:"why"`
		Kept  bool   `json:"kept"`
	} `json:"matches"`
}

func TestFactoryAISearchE2E(t *testing.T) {
	if os.Getenv("FLE2E") != "1" {
		t.Skip("opt-in live lane: set FLE2E=1 (spends cents on a real model and reads " + fleRepo + ")")
	}
	key := liveKey(t)
	ghToken := fleGHToken(t)
	if ghToken == "" {
		t.Skip("no GitHub token: set GH_TOKEN or FLE2E_GH_TOKEN_FILE")
	}
	bin := binary(t)
	base := t.TempDir()
	if keep := strings.TrimSpace(os.Getenv("FLE2E_HOME")); keep != "" {
		if err := os.MkdirAll(keep, 0o700); err != nil {
			t.Fatal(err)
		}
		base = keep
	}
	root, _ := filepath.EvalSymlinks(base)
	root = filepath.Join(root, "aisearch")
	home := filepath.Join(root, "codeaf")
	logDir := filepath.Join(root, "logs")
	for _, d := range []string{home, logDir, filepath.Join(root, "code")} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	rows := map[string]any{
		config.KeyAPIKey: key, config.KeyChatModel: fleModel, config.KeyTaskModel: fleModel,
		config.KeyTierWorkerModel: fleModel, config.KeyTierLowModel: fleModel, config.KeyTierHighModel: fleModel,
		config.KeyTierReflexModel: fleModel, config.KeyTierMastermindModel: fleModel, config.KeyKnowledgeModel: fleModel,
		config.KeyModelFallbacks: "", config.KeyModelRoles: "", config.KeyMemoryEnabled: "off",
	}
	raw, _ := json.MarshalIndent(rows, "", " ")
	if err := os.WriteFile(filepath.Join(home, "config.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	var env []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		switch name {
		case "OPENROUTER_API_KEY", "OPENAI_API_KEY", "GH_TOKEN", "GITHUB_TOKEN", "CODEAF_HOME", "CODEAF_PROFILE_DIR":
			continue
		}
		env = append(env, kv)
	}
	env = append(slices.Clone(env), "GH_TOKEN="+ghToken, "CODEAF_MODEL_POOL=off")
	eng := startFLEEngine(t, bin, home, env, logDir)
	t.Cleanup(eng.close)
	w := &fleWorld{t: t, eng: eng, bin: bin, home: home, root: root, logDir: logDir}
	t.Logf("AISEARCH home=%s logs=%s", home, logDir)

	// The factory on this machine, factory-demo imported, and nothing that
	// starts by itself.
	var local struct {
		Factory string `json:"factory"`
	}
	w.decode("factory.local_start", map[string]any{"caller": fleCaller}, &local)
	w.factory = local.Factory
	w.must("factory.setup_connect", map[string]any{"caller": fleCaller, "source": "gh"})
	w.must("factory.setup_select", map[string]any{"caller": fleCaller, "repos": []string{fleRepo}})
	var view struct {
		State string `json:"state"`
		Repos []struct {
			ID, Read, Clone string
		} `json:"repos"`
	}
	fleEventually(t, "the setup to read "+fleRepo, 5*time.Minute, 3*time.Second, func() bool {
		w.decode("factory.setup_read", map[string]any{"caller": fleCaller}, &view)
		if view.State == "reading" {
			for _, r := range view.Repos {
				if r.Read == "done" || r.Read == "empty" || r.Read == "skipped" {
					_, _ = eng.call("factory.setup_continue", map[string]any{"caller": fleCaller}, time.Minute)
				}
			}
		}
		return view.State == "checkouts" || view.State == "done"
	})
	if view.State == "checkouts" {
		_, _ = eng.call("factory.setup_done", map[string]any{"caller": fleCaller}, time.Minute)
	}
	w.must("factory.rules_set", map[string]any{"factory": w.factory, "rules": []string{}, "ask_first": []string{}, "never": []string{}, "start_on_its_own": false, "start_labels": []string{}})
	w.must("factory.spending_set", map[string]any{"factory": w.factory, "default_model": fleModel})

	var q struct {
		Jobs []struct {
			ID    int    `json:"document_id"`
			Num   int    `json:"number"`
			Title string `json:"title"`
		} `json:"jobs"`
	}
	fleEventually(t, "factory-demo's issues to arrive as jobs", 5*time.Minute, 5*time.Second, func() bool {
		w.decode("factory.queue", map[string]any{"factory": w.factory}, &q)
		return len(q.Jobs) >= 40
	})
	titles := map[int]string{}
	for _, j := range q.Jobs {
		titles[j.ID] = j.Title
	}
	t.Logf("AISEARCH queue: %d jobs", len(q.Jobs))

	ask := func(question string) aiSearchRun {
		var est map[string]any
		w.decode("factory.ai_search_estimate", map[string]any{"factory": w.factory, "question": question}, &est)
		t.Logf("AISEARCH ESTIMATE %q %v", question, est)
		var started struct {
			Run string `json:"run"`
		}
		w.decode("factory.ai_search", map[string]any{"factory": w.factory, "question": question}, &started)
		var run aiSearchRun
		last := ""
		fleEventually(t, "the search "+question, 10*time.Minute, 700*time.Millisecond, func() bool {
			w.decode("factory.ai_search_status", map[string]any{"run": started.Run}, &run)
			if run.Words != last {
				t.Logf("AISEARCH PROGRESS %q: %s", question, run.Words)
				last = run.Words
			}
			return run.Phase == "done" || run.Phase == "failed" || run.Phase == "stopped"
		})
		for _, m := range run.Matches {
			t.Logf("AISEARCH MATCH %q #%d %q — %s", question, m.Job, m.Title, m.Why)
		}
		t.Logf("AISEARCH RESULT %q phase=%s total=%d related=%d checked=%d over=%d matches=%d asked=%d reused=%d failed=%d usd=%.5f model=%s",
			question, run.Phase, run.Total, run.Related, run.Checked, run.Over, len(run.Matches), run.Asked, run.Reused, run.Failed, run.USD, run.Model)
		if run.Phase != "done" {
			t.Fatalf("search %q ended %s: %s", question, run.Phase, run.Error)
		}
		return run
	}
	questions := []string{"issues about the ledger total", "anything about authentication", "performance problems"}
	var runs []aiSearchRun
	for _, question := range questions {
		runs = append(runs, ask(question))
	}
	again := ask(questions[0])
	if again.USD != 0 || again.Asked != 0 || len(again.Matches) != len(runs[0].Matches) {
		t.Fatalf("asking again cost %.5f over %d calls with %d matches, want nothing and %d", again.USD, again.Asked, len(again.Matches), len(runs[0].Matches))
	}
	report, _ := json.MarshalIndent(map[string]any{"titles": titles, "runs": runs, "again": again}, "", " ")
	_ = os.WriteFile(filepath.Join(logDir, "aisearch-report.json"), report, 0o600)
	t.Logf("AISEARCH report %s", filepath.Join(logDir, "aisearch-report.json"))
}
