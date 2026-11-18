package session

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/connect"
)

// The connector read for a sheet: read-only by construction, refused never cut.

func newCSVReader(t *testing.T, hub *fakeHub, steps ...step) (*Agent, *scriptedCompleter) {
	t.Helper()
	completer := &scriptedCompleter{steps: steps}
	agent, _ := newTestAgent(t, completer, func(config *Config) { config.connectHub = hub })
	return agent, completer
}

func csvStep(text string) step {
	return func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse(text), nil }
}

func csvRefusal(t *testing.T, err error, code, want string) {
	t.Helper()
	var refusal *ConnectorCSVError
	if !errors.As(err, &refusal) || refusal.Code != code || !strings.Contains(refusal.Why, want) {
		t.Fatalf("err = %v, want a %s refusal saying %q", err, code, want)
	}
}

// A served account brings only the tools it said only look; the one that acts
// is not on the belt, and a call to it made anyway never reaches the account.
func TestAConnectorReadArmsOnlyTheToolsThatLook(t *testing.T) {
	hub := servedHub(servedList())
	agent, _ := newCSVReader(t, hub,
		toolStep("notion_create_page", `{"title":"x"}`),
		toolStep(servedName("notion", "Search Pages"), `{"query":"deals"}`),
		csvStep("Deal ID,Amount\nD-2,300\nD-3,75"),
	)
	tools, err := agent.connectorReadTools(context.Background(), notionStatus(true))
	if err != nil || len(tools) != 1 || tools[0].Name != servedName("notion", "Search Pages") {
		t.Fatalf("read belt = %v (%v), want the one tool that only looks", toolNames(tools), err)
	}

	csv, err := agent.readConnectorCSV(context.Background(), "notion", "deals this quarter", 1<<20)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if csv != "Deal ID,Amount\nD-2,300\nD-3,75\n" {
		t.Fatalf("csv = %q", csv)
	}
	made := hub.servedMade()
	if len(made) != 1 || !strings.HasPrefix(made[0], "notion Search Pages ") {
		t.Fatalf("calls that reached the account = %v, want the one search", made)
	}
}

// A key account's raw call runs `get` and nothing else.
func TestAConnectorReadRunsOnlyGetOnAKeyAccount(t *testing.T) {
	hub := &fakeHub{keyService: true, keyConnected: true}
	agent, _ := newCSVReader(t, hub,
		toolStep("stripe_request", `{"method":"post","path":"/v1/customers","body":"{}"}`),
		toolStep("stripe_request", `{"path":"/v1/customers","query":"limit=3"}`),
		csvStep("```csv\nid,email\ncus_1,a@example.test\n```"),
	)
	csv, err := agent.readConnectorCSV(context.Background(), "stripe", "", 0)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if csv != "id,email\ncus_1,a@example.test\n" {
		t.Fatalf("the fence was not taken off: %q", csv)
	}
	// An empty method is the raw call's own default, get; the post never left.
	if calls := hub.rawCalls(); len(calls) != 1 || calls[0] != " /v1/customers" {
		t.Fatalf("raw calls = %v, want the one get", calls)
	}
}

// An account whose reading the person turned off has nothing to read with,
// and is refused before any model is asked.
func TestAConnectorWithReadingOffIsRefusedBeforeAnyModelIsAsked(t *testing.T) {
	hub := servedHub(servedList())
	hub.says("notion", connect.CapabilityRead, connect.StateOff)
	agent, completer := newCSVReader(t, hub)
	_, err := agent.readConnectorCSV(context.Background(), "notion", "deals", 0)
	csvRefusal(t, err, ConnectorCSVRefused, "nothing turned on that only reads")
	if len(completer.seen) != 0 {
		t.Fatalf("a refused read still asked the model %d times", len(completer.seen))
	}
}

// Every way the answer cannot become a sheet is a refusal with a reason.
func TestAConnectorAnswerIsAWholeCSVOrARefusal(t *testing.T) {
	for _, testCase := range []struct {
		name, answer, code, want string
		max                      int64
	}{
		{"the model says it cannot", "CANNOT: that needs a write to the account", ConnectorCSVRefused, "that needs a write to the account", 0},
		{"rows of different widths", "a,b\n1,2,3\n", ConnectorCSVRefused, "not a CSV a sheet can take (row 2)", 0},
		{"nothing", "   ", ConnectorCSVRefused, "nothing to answer with", 0},
		{"over the ceiling", "a,b\n" + strings.Repeat("1,2\n", 600), ConnectorCSVTooLarge, "more than 1 KB", 1024},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := connectorCSVAnswer(testCase.answer, maxOr(testCase.max))
			csvRefusal(t, err, testCase.code, testCase.want)
		})
	}

	signedOut := servedHub(servedList())
	signedOut.servedConnected = false
	agent, _ := newCSVReader(t, signedOut)
	_, err := agent.readConnectorCSV(context.Background(), "notion", "", 0)
	csvRefusal(t, err, ConnectorCSVSignedOut, "Notion is signed out")
}

func maxOr(n int64) int64 {
	if n == 0 {
		return ConnectorCSVMaxBytes
	}
	return n
}
