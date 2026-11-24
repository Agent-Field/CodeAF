//go:build decisioneval

package session

// THE DECISION CHECK, MEASURED. Not part of any suite: it spends a few cents
// on a real model. It runs the check's own prompt over live2/E6's twenty
// requests (ten that go against Pantry's decisions, ten that only come near
// one) and counts what it caught and what it cried wolf on. The key is the
// product's (config.APIKeyAt), never printed.
//
//	E6=~/work/workstreams-v5/live2/E6/harness/content MODEL=deepseek/deepseek-v4.1-flash \
//	  go test -tags decisioneval -run TestDecisionCheckEval -count=1 -v ./internal/session/
//
// On 2026-11-16, deepseek/deepseek-v4.1-flash caught 29 of 30 conflicts over
// three runs and raised no false alarm in 30.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/home"
)

func TestDecisionCheckEval(t *testing.T) {
	dir := config.ProfileDir()
	if dir == "" {
		dir = home.InheritedDir()
	}
	key := strings.TrimSpace(config.APIKeyAt(dir))
	if key == "" {
		t.Skip("no key")
	}
	base := os.Getenv("E6")
	raw, _ := os.ReadFile(filepath.Join(base, "decisions.md"))
	var ds []checkDecision
	re := regexp.MustCompile(`^\d+\. (.*)$`)
	for _, l := range strings.Split(string(raw), "\n") {
		if m := re.FindStringSubmatch(l); m != nil {
			ds = append(ds, checkDecision{ID: fmt.Sprint(len(ds) + 1), Text: strings.ReplaceAll(m[1], "**", "")})
		}
	}
	f, _ := os.Open(filepath.Join(base, "requests.tsv"))
	sc := bufio.NewScanner(f)
	model := os.Getenv("MODEL")
	ask := func(ctx context.Context, prompt string) (string, error) {
		body, _ := json.Marshal(map[string]any{"model": model, "temperature": 0, "messages": []map[string]string{{"role": "user", "content": prompt}}})
		req, _ := http.NewRequestWithContext(ctx, "POST", "https://openrouter.ai/api/v1/chat/completions", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		var out struct {
			Choices []struct{ Message struct{ Content string } } `json:"choices"`
			Usage   struct{ Cost float64 }                       `json:"usage"`
		}
		_ = json.Unmarshal(b, &out)
		if len(out.Choices) == 0 {
			return "", fmt.Errorf("%s", b)
		}
		return out.Choices[0].Message.Content, nil
	}
	tp, fn, fp, tn := 0, 0, 0, 0
	for sc.Scan() {
		c := strings.Split(sc.Text(), "\t")
		id, kind, want, req := c[0], c[1], c[3], c[4]
		got, err := checkAgainstDecisions(context.Background(), ask, ds, checkWork{Title: req, Request: req})
		if err != nil {
			t.Fatal(err)
		}
		names := []string{}
		for _, g := range got {
			names = append(names, g.Decision.ID+":"+g.Why)
		}
		hit := false
		for _, g := range got {
			if g.Decision.ID == want {
				hit = true
			}
		}
		switch {
		case kind == "conflict" && hit:
			tp++
		case kind == "conflict":
			fn++
		case len(got) > 0:
			fp++
		default:
			tn++
		}
		t.Logf("%s %s want=%s got=%v", id, kind, want, names)
	}
	t.Logf("caught %d/%d conflicts, false alarms %d/%d", tp, tp+fn, fp, fp+tn)
}
