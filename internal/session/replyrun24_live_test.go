//go:build e2e

package session

// ── THE RUN 24 REPLAYS, AGAINST A REAL READER ───────────────────────────────
//
// replyrun24_test.go proves what reaches the reader and what the guard does
// with the lines it raises. Whether the reader RAISES them is a claim about a
// model, and no stub can answer it. So this file hands the reader model the
// pages of three replayed run 24 turns, with the ask, three times each, and
// counts how often it names the promise each turn broke: "clear of the cup and
// cake", "a row of equal width so the row stays full", "no names or signage".
//
// ── WHAT IT MEASURED ────────────────────────────────────────────────────────
//
// 2026-11-19, deepseek/deepseek-v4.1-flash, five tries per turn:
//
//	                 cup   equal width   signage
//	#2839's ask      0/5   0/5           1/5
//	this ask         4/5   4/5           4/5
//
// The bar below is five of nine, which the old ask cannot reach and the new one
// clears with room; one cheap call is not deterministic, and the bar is set so
// that a pass means the ask works rather than that the dice fell well.
//
// ── HOW TO RUN IT ───────────────────────────────────────────────────────────
//
//	OPENROUTER_API_KEY=… go test -tags e2e ./internal/session/ -run TestRealReaderRaisesRun24Promises -v
//
// About nine calls of a few thousand tokens each on the cheapest tier: a
// fraction of a cent. It skips, green and loudly, with no key. The key is
// resolved as a session resolves it (internal/config's APIKeyAt), but this
// package's tests re-root CODEAF_HOME, so in practice that means the variable.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
)

func TestRealReaderRaisesRun24Promises(t *testing.T) {
	key := strings.TrimSpace(config.APIKeyAt(""))
	if key == "" {
		t.Skip("no OpenRouter key: export OPENROUTER_API_KEY to ask a real reader")
	}
	model := strings.TrimSpace(os.Getenv("CODEAF_REPLY_READER"))
	if model == "" {
		model = "deepseek/deepseek-v4.1-flash"
	}
	cases := map[string]*regexp.Regexp{
		"spring-ad-t2": regexp.MustCompile(`(?i)cup`),
		"pipeline-t2":  regexp.MustCompile(`(?i)equal width|stays full`),
		"cafe-site-t3": regexp.MustCompile(`(?i)signage|no names`),
	}
	const tries = 3
	var (
		mu     sync.Mutex
		raised = map[string]int{}
		wg     sync.WaitGroup
	)
	for name, claim := range cases {
		_, page, _ := run24Page(run24Turn(t, name))
		for range tries {
			wg.Add(1)
			go func() {
				defer wg.Done()
				answer, err := askReader(key, model, page+"\n\n"+replyCheckAsk)
				if err != nil {
					t.Errorf("%s: %v", name, err)
					return
				}
				found := replyCheckFindingsOf(answer)
				for _, line := range append(found.wrong, found.unshown...) {
					if claim.MatchString(line) {
						mu.Lock()
						raised[name]++
						mu.Unlock()
						return
					}
				}
				t.Logf("%s: the reader did not raise it:\n%s", name, answer)
			}()
		}
	}
	wg.Wait()
	total := 0
	for name := range cases {
		t.Logf("%s: raised %d of %d", name, raised[name], tries)
		total += raised[name]
	}
	if want := 5; total < want {
		t.Errorf("the reader raised the broken promise %d times in %d; want at least %d", total, tries*len(cases), want)
	}
}

// askReader is one chat call to the router, the way the role's call is shaped:
// one user message, the page and the ask.
func askReader(key, model, prompt string) (string, error) {
	body, _ := json.Marshal(map[string]any{
		"model":    model,
		"messages": []map[string]string{{"role": "user", "content": prompt}},
	})
	ctx, done := context.WithTimeout(context.Background(), 3*time.Minute)
	defer done()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://openrouter.ai/api/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	request.Header.Set("Authorization", "Bearer "+key)
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(response.Body).Decode(&out); err != nil || len(out.Choices) == 0 {
		return "", err
	}
	return out.Choices[0].Message.Content, nil
}
