package session

import (
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/home"
)

func TestTeamProposerPinsUsageHome(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	original := UsageLedgerPath()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"choices":[{"index":0,"delta":{"role":"assistant","content":"{\"new\":[],\"add\":[]}"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"cost":0.001}}`)
		fmt.Fprint(w, "\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	proposer, err := NewTeamProposer(TeamProposerConfig{Model: "stub/model", APIKey: "stub", BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(home.EnvVar, t.TempDir())
	other := UsageLedgerPath()
	t.Cleanup(func() { StopUsageWriter(original); StopUsageWriter(other) })
	_, err = proposer.ProposeTeams(t.Context(), TeamProposalInput{Conversations: []TeamProposalConversation{{Key: "one", Title: "one"}, {Key: "two", Title: "two"}}})
	if err != nil {
		t.Fatal(err)
	}
	if !FlushUsage() {
		t.Fatal("flush")
	}
	total := func(path string) float64 {
		rows, err := ReadUsage(path, time.Time{})
		if err != nil {
			t.Fatal(err)
		}
		var sum float64
		for _, row := range rows {
			sum += row.USD
		}
		return sum
	}
	oldTotal, newTotal := total(original), total(other)
	t.Logf("original expected=$0.001000 recorded=$%.6f; other expected=$0 recorded=$%.6f", oldTotal, newTotal)
	if math.Abs(oldTotal-.001) > 1e-9 || newTotal != 0 {
		t.Fatalf("original expected=$0.001000 recorded=$%.6f; other expected=$0 recorded=$%.6f", oldTotal, newTotal)
	}
}
