//go:build e2e

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory"
)

// The paired live probe changes only person_requests, using the public writer
// input and draft checker. It makes exactly two calls, without model fallback.
func TestLiveBriefPersonRequests(t *testing.T) {
	if os.Getenv(liveFactoryOptIn) != "1" {
		t.Skip("set CODEAF_LIVE_FACTORY=1 to run the paired live probe; spend cap $1")
	}
	if liveFactoryKey == "" {
		t.Fatal("BLOCKED: no OpenRouter key")
	}
	t.Setenv("CODEAF_MODEL_POOL", "off")
	recorder := newLiveRecorder(t, config.DefaultBaseURL)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	const words = "also add a second line: see you"
	it := factory.Item{Title: "Farewell", Body: "Add exactly one line saying farewell", State: factory.StateLanded,
		AnsweredQuestions: []factory.Question{{FromStep: "test", Kind: "approve", Answer: &factory.Answer{Text: words, At: time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)}}},
		Change:            &factory.Change{Stat: &factory.ChangeStat{Files: 1, Added: 2}, Files: []factory.ChangedFile{{Path: "farewell.txt", Added: 2}}, Patch: "diff --git a/farewell.txt b/farewell.txt\n--- /dev/null\n+++ b/farewell.txt\n@@ -0,0 +1,2 @@\n+farewell\n+see you\n"},
		Stream: &factory.Stream{Phases: []factory.Phase{
			{Name: "plan", State: factory.PhaseDone, Round: 1, Result: &factory.StageResult{Done: true, Output: "Add exactly one farewell line to farewell.txt."}},
			{Name: "write", State: factory.PhaseDone, Round: 2, Result: &factory.StageResult{Done: true, Output: "farewell.txt now contains two lines:\nfarewell\nsee you"}},
			{Name: "test", State: factory.PhaseDone, Round: 2, Result: &factory.StageResult{Done: true, Output: "Read farewell.txt: farewell followed by see you. Two lines present.", Tests: &factory.TestCounts{Command: "check farewell.txt", Passed: 1}}},
		}},
	}
	in := factory.BriefInputOf(it)
	verdicts := map[string]string{}
	texts := map[string]string{}
	for _, label := range []string{"with_request", "without_request"} {
		if label == "without_request" {
			in.PersonRequests = nil
		}
		input := factory.BriefInputText(in)
		t.Logf("LIVE BRIEF %s input=%s", label, input)
		raw, err := json.Marshal(map[string]any{
			"model": liveFactoryModel, "temperature": 0, "max_tokens": briefMaxTokens, "stream": false,
			"response_format": map[string]string{"type": "json_object"},
			"messages":        []map[string]string{{"role": "system", "content": factory.BriefSystem()}, {"role": "user", "content": input}},
		})
		if err != nil {
			t.Fatal(err)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, recorder.server.URL+"/chat/completions", bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+liveFactoryKey)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: provider status %d", label, resp.StatusCode)
		}
		var reply struct {
			Model   string `json:"model"`
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}
		if err := json.Unmarshal(body, &reply); err != nil {
			t.Fatal(err)
		}
		if reply.Model != liveFactoryModel || len(reply.Choices) != 1 {
			t.Fatalf("unexpected model or choice count: %q, %d", reply.Model, len(reply.Choices))
		}
		draft := reply.Choices[0].Message.Content
		t.Logf("LIVE BRIEF %s draft=%s", label, draft)
		v, drops, fault := factory.CheckBriefDraft(it, []byte(draft))
		t.Logf("LIVE BRIEF %s verdict=%q blocks=%+v drops=%+v fault=%q", label, v.Verdict, v.Blocks, drops, fault)
		if fault != "" || len(v.Blocks) == 0 {
			t.Fatalf("%s: no checked brief", label)
		}
		verdicts[label] = v.Verdict
		checked, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		texts[label] = strings.ToLower(string(checked))
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	t.Logf("LIVE BRIEF calls=%d cost_usd=%.6f models=%v problems=%v", recorder.calls, recorder.cost, recorder.replies, recorder.problems)
	if recorder.cost > 1 || recorder.calls != 2 || len(recorder.problems) != 0 || len(recorder.replies) != 1 || recorder.replies[liveFactoryModel] != 2 {
		t.Fatal("paired probe exceeded its budget or model/call limits")
	}
	if !strings.Contains(strings.ToLower(verdicts["with_request"]), "see you") {
		t.Error("verdict did not name the later request")
	}
	// These checks catch the reported false-fault vocabulary; the paired logs
	// retain the full checked text for the caller to assess the judgment.
	accepted := strings.ToLower(verdicts["with_request"])
	if !strings.Contains(accepted, "asked") && !strings.Contains(accepted, "request") {
		t.Error("verdict did not treat the later request as asked")
	}
	fault := regexp.MustCompile(`(?i)fault|surpris|scope creep|unrequested|unwanted|exceed|mismatch|violat`)
	if fault.MatchString(texts["with_request"]) {
		t.Error("requested work still described as a fault; inspect paired blocks")
	}
	discrepancy := regexp.MustCompile(`(?i)extra|exceed|mismatch|scope|unrequested|unwanted|not asked|only one|exactly one|single.line|one.line|not met|instead of|violat`)
	if !discrepancy.MatchString(texts["without_request"]) {
		t.Error("control did not identify the discrepancy against the issue and plan")
	}
	if verdicts["with_request"] == verdicts["without_request"] {
		t.Error("removing person_requests did not change the verdict; inspect paired blocks")
	}
}
