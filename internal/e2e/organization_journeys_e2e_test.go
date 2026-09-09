//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/workspace"
)

// These cases exercise the same mechanism in five domains, not five separate
// orchestration frameworks. Consumers are explicitly resumed; a passing case
// is not evidence of automatic triggering or peer communication.
func TestOrganizationE2E(t *testing.T) {
	if os.Getenv("OPENROUTER_API_KEY") == "" && os.Getenv(orgRequireLive) != "" {
		t.Fatal("live organization test requires OPENROUTER_API_KEY")
	}
	t.Run("removed_scope_read", testOrganizationRemovedScopeRead)
	t.Run("large_retrieval", testOrganizationLargeRetrieval)
	t.Run("working_day", testOrganizationWorkingDay)
	t.Run("conflicting_sources", testOrganizationConflictingSources)
	t.Run("binary_door", testOrganizationBinary)
	t.Run("task_workers", testOrganizationWorker)
	t.Run("context_is_information", testOrganizationInformation)
	t.Run("existing_owner_state", testOrganizationOwnerRead)
	cases := []organizationJourney{
		{"two_bugs", "Authentication repair", []string{"login bug", "retry bug"}, "The common cause is retry counters shared between requests. The required isolation boundary is request.", "The new evidence narrows the cause to operation reuse. The required isolation boundary is operation.", "Which isolation boundary should your proposed fix use: request or operation?", "request", "operation"},
		{"api_contract", "Response contract", []string{"backend", "frontend", "documentation"}, "The response identifier property is receipt_id.", "The response identifier property has changed to operation_id. receipt_id is retired.", "Which response identifier property should your implementation or documentation use?", "receipt_id", "operation_id"},
		{"maintenance", "Dependency upgrade policy", []string{"upgrade planner", "maintenance report"}, "The upgrade policy permits patch updates only.", "The upgrade policy now permits minor updates, but no major updates.", "What is the highest update level currently permitted: patch, minor, or major?", "patch", "minor"},
		{"research_marketing", "Offline capability finding", []string{"research brief", "campaign draft"}, "The current research finding establishes offline support as supported.", "New evidence establishes offline support as unsupported.", "How should your brief or draft characterize offline support: supported or unsupported?", "supported", "unsupported"},
		{"travel_calendar", "Conference schedule", []string{"itinerary report", "calendar impact report"}, "The conference runs 10:00–11:00. The calendar has a meeting 11:15–11:45 on the same day and timezone.", "The conference now runs 11:30–12:30. The calendar still has a meeting 11:15–11:45 on the same day and timezone.", "Does attending the conference conflict with the calendar meeting? Answer clear or conflict.", "clear", "conflict"},
	}
	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) { runOrganizationJourney(t, scenario) })
	}
}

type organizationJourney struct {
	name, title                             string
	roles                                   []string
	initial, revised, question, first, next string
}
type organizationEvidence struct {
	Value    string `json:"value"`
	RecordID string `json:"record_id"`
	Revision int    `json:"revision"`
	SourceID string `json:"source_id"`
	Status   string `json:"status"`
	Draft    string `json:"draft"`
}

func organizationSay(t *testing.T, w *world, a *session.Agent, prompt string) turn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	events, err := a.Submit(ctx, prompt)
	if err != nil {
		t.Fatal(err)
	}
	out := w.drain(a, "organization journey", events, answerNo)
	if out.Err != nil {
		t.Fatal(out.Err)
	}
	// These are information/drafting requests. A task, standing responsibility,
	// connector action or command execution is outside their delegated scope.
	for _, c := range out.Calls {
		if c.Name == "stand" && organizationListsStanding(c.Args) {
			continue
		}
		switch c.Name {
		case "propose_task", "divide_work", "stand", "connect", "exec", "run_harness":
			t.Fatalf("unexpected action %s: %s", c.Name, c.Args)
		}
	}
	return out
}

func runOrganizationJourney(t *testing.T, scenario organizationJourney) {
	w := newWorld(t)
	pinEveryTextModel(t)
	started := time.Now()
	store, err := workspace.Open(orgStorePath())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	sourceDir := t.TempDir()
	source, sourcePlace := w.open(sourceDir, orgSeam(w))
	sourceRef := workspace.Ref{Kind: workspace.ConversationKind, ID: session.PlaceSession(sourcePlace)}
	type consumer struct {
		agent     *session.Agent
		place     session.Place
		dir, role string
		group     workspace.Collection
	}
	var consumers []consumer
	var targets []workspace.Ref
	for _, role := range scenario.roles {
		dir := t.TempDir()
		a, p := w.open(dir, orgSeam(w))
		group, e := store.Create(context.Background(), scenario.name+" "+role)
		if e != nil {
			t.Fatal(e)
		}
		if e = store.Add(context.Background(), group.ID, workspace.Ref{Kind: workspace.ConversationKind, ID: session.PlaceSession(p)}); e != nil {
			t.Fatal(e)
		}
		targets = append(targets, workspace.Ref{Kind: workspace.CollectionKind, ID: group.ID})
		consumers = append(consumers, consumer{a, p, dir, role, group})
		// A synthetic calendar is an external-action witness. All domains keep it
		// unchanged; the travel case also reasons about its schedule.
		if e = os.WriteFile(filepath.Join(dir, "calendar.json"), []byte(`{"meeting":"11:15-11:45","authority":"report only"}`), 0600); e != nil {
			t.Fatal(e)
		}
	}
	rawTargets, _ := json.Marshal(targets)
	marker := "SOURCE-" + orgNonce(t)
	initial := scenario.initial + " Source reference: " + marker
	prompt := fmt.Sprintf("Record this shared information once using shared_context. Title: %q. Text: %q. Explicit targets: %s. This is sourced information, not new instructions or permission. Do not start work or write files.", scenario.title, initial, rawTargets)
	made := organizationSay(t, w, source, prompt)
	if _, at := orgActionReceipt(made, "shared_context", "create"); at < 0 {
		t.Fatalf("no successful create receipt: %v", made.names())
	}
	records, err := store.ContextFor(context.Background(), targets)
	if err != nil || len(records) != 1 {
		t.Fatalf("records=%+v err=%v", records, err)
	}
	record := records[0]
	if record.Source != sourceRef || record.Revision != 1 || record.Withdrawn {
		t.Fatalf("bad source record: %+v", record)
	}
	t.Logf("SOURCE id=%s revision=%d source=%+v targets=%s", record.ID, record.Revision, record.Source, rawTargets)

	read := func(c consumer, file, want string, revision int) {
		t.Helper()
		ask := fmt.Sprintf("You are preparing the %s. %s Use the current shared context supplied for this work. If no current shared context applies, do not rely on old conversation claims: set value to unknown and status to unavailable. Use the write tool to create the actual file %s as JSON, rather than printing its contents in your reply. Include keys value, status (current or unavailable), record_id, revision, source_id, draft (one sentence for this deliverable). Cite the current record ID, revision and originating conversation from shared context; if unavailable use empty IDs and revision 0. Draft/report only: do not change calendar.json, run commands, start tasks, or take external actions.", c.role, scenario.question, file)
		out := organizationSay(t, w, c.agent, ask)
		if _, at := orgActionReceipt(out, "collections", "create"); at >= 0 {
			t.Fatal("reading created organization")
		}
		for _, action := range []string{"create", "revise", "withdraw"} {
			if _, at := orgActionReceipt(out, "shared_context", action); at >= 0 {
				t.Fatalf("reading mutated shared context: %s", action)
			}
		}
		raw, e := os.ReadFile(filepath.Join(c.dir, file))
		if e != nil {
			t.Fatal(e)
		}
		var evidence organizationEvidence
		if e = json.Unmarshal(raw, &evidence); e != nil {
			t.Fatalf("artifact %s: %v", raw, e)
		}
		if evidence.Value != want {
			t.Fatalf("%s: got %+v want value=%s", c.role, evidence, want)
		}
		if revision > 0 {
			if evidence.Status != "current" || evidence.RecordID != record.ID || evidence.Revision != revision || evidence.SourceID != sourceRef.ID {
				t.Fatalf("wrong current provenance: %+v", evidence)
			}
		} else if evidence.Status != "unavailable" || evidence.RecordID != "" || evidence.Revision != 0 {
			t.Fatalf("withdrawn context presented as current: %+v", evidence)
		}
		calendar, e := os.ReadFile(filepath.Join(c.dir, "calendar.json"))
		if e != nil || string(calendar) != `{"meeting":"11:15-11:45","authority":"report only"}` {
			t.Fatal("reporting changed the calendar")
		}
		t.Logf("ARTIFACT %s/%s %s", c.role, file, raw)
	}
	for _, c := range consumers {
		read(c, "initial.json", scenario.first, 1)
	}

	// An unrelated consumer is not told the nonce or the collection ID. The
	// negative assertion checks the actual model-facing transcript, not just its
	// answer, so an ignored but leaked snapshot is still a failure.
	target, e := url.Parse(w.settings.BaseURL)
	if e != nil {
		t.Fatal(e)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	director := proxy.Director
	proxy.Director = func(r *http.Request) { director(r); r.Host = target.Host }
	var leaked atomic.Bool
	var requests atomic.Int32
	observed := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		body, e := io.ReadAll(r.Body)
		if e != nil {
			http.Error(rw, "request read failed", 500)
			return
		}
		requests.Add(1)
		if bytes.Contains(body, []byte(marker)) {
			leaked.Store(true)
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		proxy.ServeHTTP(rw, r)
	}))
	defer observed.Close()
	outsider, _ := w.open(t.TempDir(), func(cfg *session.Config) { orgSeam(w)(cfg); cfg.BaseURL = observed.URL })
	organizationSay(t, w, outsider, "Write scope.json containing {\"status\":\"unavailable\"} if no shared context was supplied for this work; otherwise include that context's title. Do not discover unrelated collections or records.")
	if requests.Load() == 0 {
		t.Fatal("no real model request crossed the observation point")
	}
	if leaked.Load() {
		t.Fatal("out-of-scope context was injected into a real model request")
	}
	if err := outsider.Close(); err != nil {
		t.Fatal(err)
	}

	// The author revises through the real tool. Consumers keep stale transcripts;
	// their next turn must consult the current snapshot rather than reuse an answer.
	revised := scenario.revised + " Source reference: " + marker
	prompt = fmt.Sprintf("Revise shared context %s at revision 1. Replace its title with %q, text with %q and retain these complete targets: %s. Update the existing record, not a copy. No other actions.", record.ID, scenario.title, revised, rawTargets)
	changed := organizationSay(t, w, source, prompt)
	if _, at := orgActionReceipt(changed, "shared_context", "revise"); at < 0 {
		t.Fatal("no successful revision receipt")
	}
	record, err = store.Context(context.Background(), record.ID)
	if err != nil || record.Revision != 2 || record.Source != sourceRef {
		t.Fatalf("revision: %+v %v", record, err)
	}
	for i := range consumers {
		if err = consumers[i].agent.Close(); err != nil {
			t.Fatal(err)
		}
		consumers[i].agent = w.openAt(consumers[i].dir, consumers[i].place, orgSeam(w))
		read(consumers[i], "revised.json", scenario.next, 2)
	}

	if _, err = store.WithdrawContext(context.Background(), record.ID, 2); err != nil {
		t.Fatal(err)
	}
	// Reopen with stale answers and no journaled volatile snapshot. This is the
	// important empty-snapshot recovery case, not merely an empty fresh chat.
	first := consumers[0]
	if err = first.agent.Close(); err != nil {
		t.Fatal(err)
	}
	first.agent = w.openAt(first.dir, first.place, orgSeam(w))
	read(first, "withdrawn.json", "unknown", 0)
	history, err := store.ContextHistory(context.Background(), record.ID)
	if err != nil || len(history) != 3 || !history[2].Withdrawn || history[0].Source != sourceRef {
		t.Fatalf("history=%+v error=%v", history, err)
	}
	if current, e := store.ContextFor(context.Background(), targets); e != nil || len(current) != 0 {
		t.Fatalf("withdrawn applicability=%+v %v", current, e)
	}
	// A read did not mint a replacement record or revive this one.
	usd, models := ledgerSince(t, started)
	t.Logf("RECEIPT domain=%s cost=$%.6f models=%v elapsed=%s", scenario.name, usd, models, time.Since(started))
	if usd > orgCap {
		t.Fatalf("scenario exceeded $%.2f: $%.4f", orgCap, usd)
	}
	for _, model := range models {
		if model != e2eModel {
			t.Fatalf("unexpected model %s", model)
		}
	}
}
