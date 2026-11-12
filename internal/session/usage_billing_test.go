package session

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/roles"
)

// pricedProvider is a real provider client on a fake endpoint that answers
// every completion with a priced usage block, built with the billing statement
// a bound door would give it.
func pricedProvider(t *testing.T, billing provider.BillingFact) *provider.Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var envelope struct {
			Stream bool `json:"stream"`
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &envelope)
		usage := `"usage":{"prompt_tokens":40,"completion_tokens":7,"total_tokens":47,"cost":0.013}`
		if envelope.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "data: {\"id\":\"g-1\",\"model\":\"test/model\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"done\"},\"finish_reason\":\"stop\"}],%s}\n\ndata: [DONE]\n\n", usage)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"id":"g-1","model":"test/model","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"done"}}],%s}`, usage)
	}))
	t.Cleanup(server.Close)
	client, err := provider.NewClient(provider.Config{
		APIKey: "test-key", BaseURL: server.URL, Model: "test/model",
		HTTPClient: server.Client(), Billing: billing,
	})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

// rawLedgerRows reads the ledger back as the JSON objects on disk, so a test
// can see which keys a row carries and not only what the struct decodes to.
func rawLedgerRows(t *testing.T, path string) []map[string]any {
	t.Helper()
	FlushUsage()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	for _, raw := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if raw == "" {
			continue
		}
		var row map[string]any
		if err := json.Unmarshal([]byte(raw), &row); err != nil {
			t.Fatalf("row %q: %v", raw, err)
		}
		rows = append(rows, row)
	}
	return rows
}

// TestATurnsRowCarriesTheRoadItsAnswerCameDown drives a real turn through a
// real provider client: the row names itself, says the plan door stated the
// call was included, and says the money is the provider's own figure.
func TestATurnsRowCarriesTheRoadItsAnswerCameDown(t *testing.T) {
	ledger := filepath.Join(t.TempDir(), UsageLedgerName)
	plan := provider.IncludedDoorFact("z-ai", "coding-plan")
	agent, _ := newTestAgent(t, pricedProvider(t, plan), func(config *Config) {
		config.usageLedger = ledger
		config.SessionFile = filepath.Join(t.TempDir(), "session.jsonl")
	})
	collect(t, mustSubmit(t, agent, "hello"))
	// The turn's own call, and the title errand the first turn starts beside
	// it on its own detached road: both came down the same plan door.
	rows := rawLedgerRows(t, ledger)
	seats := map[any]bool{}
	ids := map[any]bool{}
	for _, row := range rows {
		seats[row["seat"]] = true
		if row["billing"] != "included" || row["billing_basis"] != "door:z-ai/coding-plan" || row["price"] != "provider" {
			t.Fatalf("row = %v, want included on door:z-ai/coding-plan priced by the provider", row)
		}
		id, _ := row["id"].(string)
		if len(id) != 24 || ids[id] {
			t.Fatalf("row id = %v, want a fresh 24-hex row name", row["id"])
		}
		ids[id] = true
		if row["usd"] != 0.013 {
			t.Fatalf("row usd = %v, want the provider's 0.013 unchanged", row["usd"])
		}
	}
	if !seats["talk"] {
		t.Fatalf("no row for the turn's own call: %v", rows)
	}
}

// TestAnUnstatedRoadLeavesTheRowSayingNothing: a client nobody stated a
// billing fact for writes a row with no billing key at all — never "included",
// never "unknown" spelled as though it were a statement.
func TestAnUnstatedRoadLeavesTheRowSayingNothing(t *testing.T) {
	ledger := filepath.Join(t.TempDir(), UsageLedgerName)
	agent, _ := newTestAgent(t, pricedProvider(t, provider.BillingFact{}), func(config *Config) {
		config.usageLedger = ledger
		config.SessionFile = filepath.Join(t.TempDir(), "session.jsonl")
	})
	collect(t, mustSubmit(t, agent, "hello"))
	rows := rawLedgerRows(t, ledger)
	if len(rows) == 0 {
		t.Fatal("the turn wrote no row")
	}
	for _, row := range rows {
		if _, said := row["billing"]; said {
			t.Fatalf("unstated road wrote billing: %v", row)
		}
		if _, said := row["billing_basis"]; said {
			t.Fatalf("unstated road wrote a basis: %v", row)
		}
	}
}

// TestADetachedErrandsRowCarriesItsOwnRoad: an errand run on the real
// transport and banked later through the detached door — the shape a title or
// a task name takes — carries the road its own ladder saw.
func TestADetachedErrandsRowCarriesItsOwnRoad(t *testing.T) {
	ledger := filepath.Join(t.TempDir(), UsageLedgerName)
	meter := provider.MeteredDoorFact("z-ai", "metered")
	agent, _ := newTestAgent(t, pricedProvider(t, meter), func(config *Config) {
		config.usageLedger = ledger
		config.SessionFile = filepath.Join(t.TempDir(), "session.jsonl")
	})
	response, called, err := agent.callRole(t.Context(), roles.RoleTaskName, "test/model",
		[]ai.Message{textMessage("user", "name this")})
	if err != nil || response == nil {
		t.Fatalf("errand = %v, %v", response, err)
	}
	agent.addDetachedUsageAs(response, called, 1, auxRoleTaskName)
	rows := rawLedgerRows(t, ledger)
	if len(rows) != 1 {
		t.Fatalf("ledger rows = %d, want the errand's one", len(rows))
	}
	if rows[0]["billing"] != "metered" || rows[0]["billing_basis"] != "door:z-ai/metered" || rows[0]["price"] != "provider" {
		t.Fatalf("errand row = %v, want metered on door:z-ai/metered priced by the provider", rows[0])
	}
	// AND THE NOTE IS SPENT: the same answer banked again is nobody's word.
	if _, noted := takeAnswerBilling(response); noted {
		t.Fatal("an errand's road was handed out twice")
	}
}

// TestALateReceiptsRowCarriesTheRoadItWasOwedUnder: the receipt's captured
// road — not the agent's or the client's current one — is what the late row
// says, and its money is marked as the receipt's.
func TestALateReceiptsRowCarriesTheRoadItWasOwedUnder(t *testing.T) {
	ledger := filepath.Join(t.TempDir(), UsageLedgerName)
	agent, _ := ledgerAgent(t, ledger)
	road := provider.BillingFact{Kind: provider.BillingMetered, Basis: "overflow:pay-as-you-go"}
	agent.reconciled(provider.Reconciled{
		Billed: provider.Billed{Model: "test/model", PromptTokens: 30, CompletionTokens: 9, Cost: 0.41, Billing: road},
		Ref:    "generation-late", Found: true,
	})
	rows := rawLedgerRows(t, ledger)
	if len(rows) != 1 {
		t.Fatalf("ledger rows = %d, want the receipt's one", len(rows))
	}
	row := rows[0]
	if row["billing"] != "metered" || row["billing_basis"] != "overflow:pay-as-you-go" || row["price"] != "receipt" || row["reconciled"] != true {
		t.Fatalf("late row = %v, want the overflow road priced by its receipt", row)
	}
}

// TestARowIsNamedOnceAndKeepsItsName: the ledger's one door names a row that
// has no name, never renames one that has, gives two rows two names, and an
// old row with none of the new keys still decodes.
func TestARowIsNamedOnceAndKeepsItsName(t *testing.T) {
	path := filepath.Join(t.TempDir(), UsageLedgerName)
	at := time.Now()
	recordUsage(t, path, UsageLine{At: at, Model: "a", Calls: 1, USD: 0.1})
	recordUsage(t, path, UsageLine{At: at, Model: "b", Calls: 1, USD: 0.2})
	recordUsage(t, path, UsageLine{ID: "kept-by-its-writer", At: at, Model: "c", Calls: 1, USD: 0.3})
	// A row a build before these fields wrote.
	old := `{"at":"2026-09-01T10:00:00Z","day":"2026-09-01","model":"old","calls":1,"usd":0.5}` + "\n"
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(old); err != nil {
		t.Fatal(err)
	}
	_ = file.Close()
	lines, err := ReadUsage(path, time.Time{})
	if err != nil || len(lines) != 4 {
		t.Fatalf("read = %d rows, %v", len(lines), err)
	}
	if lines[0].ID == "" || lines[1].ID == "" || lines[0].ID == lines[1].ID {
		t.Fatalf("ids = %q, %q, want two distinct names", lines[0].ID, lines[1].ID)
	}
	if lines[2].ID != "kept-by-its-writer" {
		t.Fatalf("a named row was renamed to %q", lines[2].ID)
	}
	if lines[3].ID != "" || lines[3].Billing != "" || lines[3].Price != "" || lines[3].USD != 0.5 {
		t.Fatalf("old row = %+v, want its figures and none of the new keys", lines[3])
	}
}

// TestAnErrandNoteGivesWayWithoutTouchingMoney: past the bound, the oldest
// note is forgotten and the answer it belonged to is banked as "nobody said";
// a note still inside the bound is kept.
func TestAnErrandNoteGivesWayWithoutTouchingMoney(t *testing.T) {
	plan := provider.IncludedDoorFact("z-ai", "coding-plan")
	first := &ai.Response{}
	noteAnswerBilling(first, plan)
	var last *ai.Response
	for i := 0; i < answerBillingNotes; i++ {
		last = &ai.Response{}
		noteAnswerBilling(last, plan)
	}
	if _, noted := takeAnswerBilling(first); noted {
		t.Fatal("a note past the bound was still held")
	}
	if fact, noted := takeAnswerBilling(last); !noted || fact != plan {
		t.Fatalf("the newest note = %+v, %t, want %+v", fact, noted, plan)
	}
	for i := range answerBillings.ring {
		if waiting := answerBillings.ring[i].Value(); waiting != nil {
			takeAnswerBilling(waiting)
		}
	}
}

func TestAnUnbankedBillingNoteDoesNotRetainItsAnswer(t *testing.T) {
	collected := func() <-chan struct{} {
		done := make(chan struct{})
		response := &ai.Response{}
		runtime.AddCleanup(response, func(ch chan struct{}) { close(ch) }, done)
		noteAnswerBilling(response, provider.IncludedDoorFact("test", "plan"))
		runtime.KeepAlive(response)
		return done
	}()
	runtime.GC()
	select {
	case <-collected:
	case <-time.After(10 * time.Second):
		t.Fatal("billing metadata retained an unbanked answer")
	}
}
