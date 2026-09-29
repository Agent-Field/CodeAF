package session

import (
	"context"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// The screen joins scrollback above the live transcript's floor. A turn that
// finished during the summary must remain on that joined page after each pass.
func TestMidSummaryExchangeAppearsOnceInJoinedHistory(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var summaries atomic.Int32
	model := &summarizer{answer: func(_ int, messages []ai.Message) (*ai.Response, error) {
		if strings.HasPrefix(messageText(messages[0]), "You are compacting") {
			if summaries.Add(1) == 1 {
				close(started)
				<-release
			}
			return textResponse(summaryWords), nil
		}
		return textResponse("MIDFLIGHT ANSWER"), nil
	}}
	agent, _ := newTestAgent(t, model, func(c *Config) {
		c.ContextWindow = 65_536
		c.SessionFile = filepath.Join(t.TempDir(), "session.jsonl")
	})
	personHeavy(agent, 12, 20_000)
	done := make(chan error, 1)
	go func() { done <- agent.Compact(context.Background()) }()
	<-started
	drainTurn(t, agent, "MIDFLIGHT QUESTION")
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	checkJoinedExchange(t, agent)
	personHeavy(agent, 12, 20_000)
	if err := agent.Compact(context.Background()); err != nil {
		t.Fatal(err)
	}
	checkJoinedExchange(t, agent)
}

func checkJoinedExchange(t *testing.T, agent *Agent) {
	t.Helper()
	history, transcript := agent.EarlierHistory(), agent.Transcript()
	joined := append(append([]DisplayEntry(nil), history.Entries...), transcript[min(history.Floor, len(transcript)):]...)
	for _, word := range []string{"MIDFLIGHT QUESTION", "MIDFLIGHT ANSWER"} {
		count := 0
		for _, entry := range joined {
			if strings.Contains(entry.Text, word) {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("%q appears %d times in joined history (floor=%d, earlier=%d, live=%d)", word, count, history.Floor, len(history.Entries), len(transcript))
		}
	}
}
