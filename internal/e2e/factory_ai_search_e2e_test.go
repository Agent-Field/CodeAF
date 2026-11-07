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
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
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
	nums := map[int]int{}
	for _, j := range q.Jobs {
		titles[j.ID] = j.Title
		nums[j.ID] = j.Num
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
	var scores []aiSearchScore
	for _, question := range questions {
		before := aiSearchKept(t, home)
		run := ask(question)
		runs = append(runs, run)
		score := scoreAISearch(question, run, nums, titles, aiSearchNew(aiSearchKept(t, home), before))
		scores = append(scores, score)
		t.Logf("AISEARCH SCORE %q recall %d/%d precision %d/%d (borderline %v) missed %v",
			question, score.Found, score.Truth, score.Right, score.Matched, score.Borderline, score.Missed)
	}
	again := ask(questions[0])
	if again.USD != 0 || again.Asked != 0 || len(again.Matches) != len(runs[0].Matches) {
		t.Fatalf("asking again cost %.5f over %d calls with %d matches, want nothing and %d", again.USD, again.Asked, len(again.Matches), len(runs[0].Matches))
	}
	report, _ := json.MarshalIndent(map[string]any{"titles": titles, "runs": runs, "again": again, "scores": scores}, "", " ")
	_ = os.WriteFile(filepath.Join(logDir, "aisearch-report.json"), report, 0o600)
	t.Logf("AISEARCH report %s", filepath.Join(logDir, "aisearch-report.json"))
}

// aiSearchTruth is the answer key for the three questions, by factory-demo
// issue number, labelled by hand from the titles and bodies on 2026-11-07.
// Yes is what a person asking the question would want listed; maybe is
// arguable either way, so it is neither demanded nor counted against
// precision. An issue not in the queue (closed, or never imported) is left out
// of the count.
var aiSearchTruth = map[string]struct{ yes, maybe []int }{
	"issues about the ledger total": {
		yes:   []int{1, 3, 5, 49, 51, 53, 58, 64, 68, 78, 80, 91},
		maybe: []int{6, 52, 76, 86, 88},
	},
	"anything about authentication": {
		yes:   []int{43, 70, 90},
		maybe: []int{82},
	},
	"performance problems": {
		yes:   []int{50, 54, 56, 57, 62, 91, 95},
		maybe: []int{77, 84, 71, 94},
	},
}

type aiSearchScore struct {
	Question string `json:"question"`
	// Truth is how many yes issues are in the queue; Found how many of them
	// matched.
	Truth int `json:"truth"`
	Found int `json:"found"`
	// Matched is every match; Right those that are yes or maybe.
	Matched    int   `json:"matched"`
	Right      int   `json:"right"`
	Borderline []int `json:"borderline"`
	Wrong      []int `json:"wrong"`
	// Missed is each yes issue that did not match and which pass said no.
	Missed map[int]string `json:"missed"`
}

func scoreAISearch(question string, run aiSearchRun, nums map[int]int, titles map[int]string, kept map[string]aiSearchEntry) aiSearchScore {
	key := aiSearchTruth[question]
	in := map[int]int{}
	for id, n := range nums {
		in[n] = id
	}
	matched := map[int]bool{}
	for _, m := range run.Matches {
		matched[nums[m.Job]] = true
	}
	yes, maybe := map[int]bool{}, map[int]bool{}
	for _, n := range key.yes {
		yes[n] = true
	}
	for _, n := range key.maybe {
		maybe[n] = true
	}
	s := aiSearchScore{Question: question, Matched: len(run.Matches), Missed: map[int]string{}}
	for _, n := range key.yes {
		if _, ok := in[n]; !ok {
			continue
		}
		s.Truth++
		if matched[n] {
			s.Found++
			continue
		}
		why := "not asked"
		for k, e := range kept {
			if strings.HasSuffix(k, "#"+strconv.Itoa(n)) {
				switch {
				case !e.Screened:
					why = "screen left it out"
				case e.Judged != nil && !e.Judged.Yes:
					why = "judge said no: " + e.Judged.Why
				}
			}
		}
		s.Missed[n] = titles[in[n]] + " — " + why
	}
	for n := range matched {
		switch {
		case yes[n]:
			s.Right++
		case maybe[n]:
			s.Right++
			s.Borderline = append(s.Borderline, n)
		default:
			s.Wrong = append(s.Wrong, n)
		}
	}
	slices.Sort(s.Borderline)
	slices.Sort(s.Wrong)
	return s
}

type aiSearchEntry struct {
	Screened bool `json:"screened"`
	Judged   *struct {
		Yes bool   `json:"yes"`
		Why string `json:"why"`
	} `json:"judged"`
}

// aiSearchKept reads every answer the engine keeps under home.
func aiSearchKept(t *testing.T, home string) map[string]aiSearchEntry {
	t.Helper()
	out := map[string]aiSearchEntry{}
	_ = filepath.WalkDir(home, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Base(filepath.Dir(path)) != "factory-ai-search" {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		var doc struct {
			Answers map[string]aiSearchEntry `json:"answers"`
		}
		if json.Unmarshal(raw, &doc) == nil {
			maps.Copy(out, doc.Answers)
		}
		return nil
	})
	return out
}

// aiSearchNew is the answers in after that were not in before: one question's.
func aiSearchNew(after, before map[string]aiSearchEntry) map[string]aiSearchEntry {
	out := map[string]aiSearchEntry{}
	for k, e := range after {
		if _, ok := before[k]; !ok {
			out[k] = e
		}
	}
	return out
}
