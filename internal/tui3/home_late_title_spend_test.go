package tui3

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

func TestHomeSearchCardAndCostAgreeAfterALateTitleBill(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_HOME", t.TempDir())
	started, release := make(chan struct{}), make(chan struct{})
	var startOnce, releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		content, cost := "parser fixed", "0.002"
		if strings.Contains(string(raw), "You name conversations.") {
			startOnce.Do(func() { close(started) })
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
			content, cost = "parser migration compatibility investigation", "0.0014"
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `data: {"id":"fixture","choices":[{"index":0,"delta":{"role":"assistant","content":"`+content+`"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15,"cost":`+cost+`}}`+"\n\ndata: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)
	root, workspace := t.TempDir(), t.TempDir()
	place := session.Place{Dir: filepath.Join(root, "project", treeConversation), Workspace: workspace}
	if err := os.MkdirAll(filepath.Join(place.Dir, ".cell"), 0o755); err != nil {
		t.Fatal(err)
	}
	agent, err := session.New(session.Config{
		Workspace: workspace, Place: place, SessionFile: place.Transcript(),
		Model: "fixture/chat", APIKey: "fixture", BaseURL: server.URL,
		System: "Test only.", OneModel: true, DailyBudgetResolved: true,
		ProfileDir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = agent.Close(); session.StopUsageWriter(session.UsageLedgerPath()) })
	lane, leave := agent.WatchTitle()
	defer leave()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	events, err := agent.Submit(ctx, "Investigate parser migration compatibility and reproduce the regression.")
	if err != nil {
		t.Fatal(err)
	}
	for {
		select {
		case event, open := <-events:
			if !open {
				goto sealed
			}
			if event.Kind == session.EventError {
				t.Fatalf("turn failed: %v", event.Err)
			}
		case <-ctx.Done():
			t.Fatal("turn did not seal before the held title")
		}
	}
sealed:
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("title call never started")
	}
	agent.SettleWrites()
	releaseOnce.Do(func() { close(release) })
	select {
	case <-lane:
	case <-ctx.Done():
		t.Fatal("late title did not finish")
	}
	agent.SettleWrites()
	rows := session.ReadWorld(root).Sessions()
	if len(rows) != 1 {
		t.Fatalf("Home has %d conversation rows, want one", len(rows))
	}
	a := newTestApp(agent)
	cost := a.costText()
	card := homeFacts(rows[0], time.Now())
	if !strings.Contains(cost, "$0.0034") || !strings.Contains(card, "spent $0.0034") {
		t.Fatalf("late title bill disagrees across surfaces: /cost=%q Home card=%q", cost, card)
	}
}
