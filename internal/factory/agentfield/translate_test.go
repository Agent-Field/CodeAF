package agentfield

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// ── THE FIXTURES ARE EACH SOURCE'S REAL PAYLOAD ─────────────────────────────
//
// testdata holds what Linear, Sentry, Slack and GitHub actually post (their
// documented webhook bodies), and the normalizers below build the `event` the
// control plane dispatches from them exactly as AgentField's own sources do
// (control-plane/internal/sources/<name>/<name>.go at origin/main c4813968):
// Linear and Sentry wrap the body in their normalized envelope, Slack,
// GitHub, generic_bearer and generic_hmac pass the body through, and cron
// emits {fired_at, expression, timezone}. A translator is tested against what
// reaches it on the wire, not against a shape this package invented.

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// normalizeLinear is linear.go's HandleRequest normalization.
func normalizeLinear(t *testing.T, body []byte, delivery string) json.RawMessage {
	t.Helper()
	var p struct {
		Action           string          `json:"action"`
		Type             string          `json:"type"`
		Actor            json.RawMessage `json:"actor"`
		CreatedAt        string          `json:"createdAt"`
		Data             json.RawMessage `json:"data"`
		URL              string          `json:"url"`
		UpdatedFrom      json.RawMessage `json:"updatedFrom"`
		WebhookID        string          `json:"webhookId"`
		WebhookTimestamp int64           `json:"webhookTimestamp"`
	}
	if err := json.Unmarshal(body, &p); err != nil {
		t.Fatal(err)
	}
	out, _ := json.Marshal(map[string]any{
		"action": p.Action, "type": p.Type, "actor": rawOrNil(p.Actor), "created_at": p.CreatedAt,
		"data": rawOrNil(p.Data), "url": p.URL, "updated_from": rawOrNil(p.UpdatedFrom),
		"webhook_id": p.WebhookID, "webhook_timestamp": p.WebhookTimestamp,
		"linear": map[string]string{"delivery": delivery, "event": p.Type},
	})
	return out
}

// normalizeSentry is sentry.go's HandleRequest normalization, with resource
// the Sentry-Hook-Resource header.
func normalizeSentry(t *testing.T, body []byte, resource, requestID string) json.RawMessage {
	t.Helper()
	var p struct {
		Action       string          `json:"action"`
		Installation json.RawMessage `json:"installation"`
		Data         json.RawMessage `json:"data"`
		Actor        json.RawMessage `json:"actor"`
	}
	if err := json.Unmarshal(body, &p); err != nil {
		t.Fatal(err)
	}
	out, _ := json.Marshal(map[string]any{
		"action": p.Action, "resource": resource, "installation": rawOrNil(p.Installation),
		"data": rawOrNil(p.Data), "actor": rawOrNil(p.Actor),
		"sentry": map[string]string{"request_id": requestID, "timestamp": "1793977445", "resource": resource},
	})
	return out
}

func rawOrNil(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	return raw
}

func TestLinearIssueCreatedIsAnEntry(t *testing.T) {
	event := normalizeLinear(t, fixture(t, "linear_issue_create.json"), "d-1")
	got, err := Translate(Meta{Source: "linear", EventType: "issue.create", IdempotencyKey: "d-1"}, event, "")
	if err != nil || got.File == nil {
		t.Fatalf("Translate = %+v, %v", got, err)
	}
	e := got.File
	if e.Source != "agentfield:linear" || e.ExternalID != "ENG-142" || e.Title != "Ledger needs a Len method" {
		t.Fatalf("entry %+v", e)
	}
	if e.URL != "https://linear.app/acme/issue/ENG-142/ledger-needs-a-len-method" || e.Author != "Ada Lovelace" || e.Priority != "p1" {
		t.Fatalf("entry url %q author %q priority %q", e.URL, e.Author, e.Priority)
	}
	if !slices.Equal(e.Labels, []string{"Bug", "linear-team:ENG"}) || e.Trust != factory.TierCollab {
		t.Fatalf("labels %v trust %q", e.Labels, e.Trust)
	}
	if !strings.Contains(e.Body, "func (l *Ledger) Len() int") {
		t.Fatalf("body %q", e.Body)
	}
}

func TestALinearCommentFilesNothing(t *testing.T) {
	event := normalizeLinear(t, fixture(t, "linear_comment_create.json"), "d-2")
	got, err := Translate(Meta{Source: "linear", EventType: "comment.create"}, event, "")
	if err != nil || got.File != nil || !strings.Contains(got.Ignored, "comment.create") {
		t.Fatalf("Translate = %+v, %v", got, err)
	}
}

func TestSentryIssueCreatedIsAnEntry(t *testing.T) {
	event := normalizeSentry(t, fixture(t, "sentry_issue_created.json"), "issue", "req-9")
	got, err := Translate(Meta{Source: "sentry", EventType: "issue.created", IdempotencyKey: "req-9"}, event, "")
	if err != nil || got.File == nil {
		t.Fatalf("Translate = %+v, %v", got, err)
	}
	e := got.File
	if e.Source != "agentfield:sentry" || e.ExternalID != "FACTORY-DEMO-7" || !strings.HasPrefix(e.Title, "panic: runtime error") {
		t.Fatalf("entry %+v", e)
	}
	if e.URL != "https://acme.sentry.io/issues/1170820242/" || e.Priority != "p1" {
		t.Fatalf("url %q priority %q", e.URL, e.Priority)
	}
	if !slices.Contains(e.Labels, "sentry-project:factory-demo") || !slices.Contains(e.Labels, "sentry-level:error") {
		t.Fatalf("labels %v", e.Labels)
	}
	if !strings.Contains(e.Body, "In: ledger.(*Ledger).At") || !strings.Contains(e.Body, "Events: 3") {
		t.Fatalf("body %q", e.Body)
	}
}

func TestASentryResolutionFilesNothing(t *testing.T) {
	body := strings.Replace(string(fixture(t, "sentry_issue_created.json")), `"action": "created"`, `"action": "resolved"`, 1)
	got, err := Translate(Meta{Source: "sentry"}, normalizeSentry(t, []byte(body), "issue", "r"), "")
	if err != nil || got.File != nil || got.Ignored == "" {
		t.Fatalf("Translate = %+v, %v", got, err)
	}
}

func TestSlackMentionIsAnEntryAndABotIsNot(t *testing.T) {
	got, err := Translate(Meta{Source: "slack", EventType: "app_mention", IdempotencyKey: "Ev0LAN670R"}, fixture(t, "slack_app_mention.json"), "")
	if err != nil || got.File == nil {
		t.Fatalf("Translate = %+v, %v", got, err)
	}
	e := got.File
	if e.Source != "agentfield:slack" || e.ExternalID != "C0LAN2Q65:1515449522.000016" || e.Title != "add a Len method to Ledger in factory-demo" || e.Author != "U061F7AUR" {
		t.Fatalf("entry %+v", e)
	}
	if !slices.Equal(e.Labels, []string{"slack-channel:C0LAN2Q65"}) {
		t.Fatalf("labels %v", e.Labels)
	}
	bot, err := Translate(Meta{Source: "slack"}, fixture(t, "slack_bot_message.json"), "")
	if err != nil || bot.File != nil || bot.Ignored == "" {
		t.Fatalf("a bot's message: %+v, %v", bot, err)
	}
	verify, _ := Translate(Meta{Source: "slack"}, json.RawMessage(`{"type":"url_verification","challenge":"x"}`), "")
	if verify.File != nil || verify.Ignored == "" {
		t.Fatalf("url verification: %+v", verify)
	}
}

// A generic source's body already is an entry. Its source is the control
// plane's, never the sender's own word: this fixture says github, and a
// sender who could claim github would be taken for the forge.
func TestAGenericBearerEventIsAnEntryUnderItsOwnSource(t *testing.T) {
	got, err := Translate(Meta{Source: "generic_bearer", IdempotencyKey: "k-1"}, fixture(t, "generic_entry.json"), factory.TierCollab)
	if err != nil || got.File == nil {
		t.Fatalf("Translate = %+v, %v", got, err)
	}
	e := got.File
	if e.Source != "agentfield:generic-bearer" || e.ExternalID != "ops-77" || e.Repo != "santoshkumarradha/factory-demo" || e.Priority != "p1" {
		t.Fatalf("entry %+v", e)
	}
	if e.Trust != factory.TierCollab {
		t.Fatalf("trust %q: an owner entry from a collaborator control plane is a collaborator's", e.Trust)
	}
	hmac, err := Translate(Meta{Source: "generic_hmac", IdempotencyKey: "k-2"}, json.RawMessage(`{"entry":{"title":"Fix it"}}`), factory.TierOwner)
	if err != nil || hmac.File == nil || hmac.File.Source != "agentfield:generic-hmac" || hmac.File.ExternalID != "k-2" || hmac.File.Trust != factory.TierOwner {
		t.Fatalf("an {entry} under generic_hmac with the delivery's key as its id: %+v, %v", hmac.File, err)
	}
}

func TestADirectCallIsAnEntryAndABadOneIsRefused(t *testing.T) {
	got, err := Translate(Meta{EventID: "exec-1"}, json.RawMessage(`{"title":"Fix it","external_id":"x-1"}`), "")
	if err != nil || got.File == nil || got.File.Source != "agentfield:execute" || got.File.ExternalID != "x-1" {
		t.Fatalf("Translate = %+v, %v", got.File, err)
	}
	if _, err := Translate(Meta{EventID: "exec-2"}, json.RawMessage(`{"title":"Fix it","priority":"p9"}`), ""); err == nil || !strings.Contains(err.Error(), "priority") {
		t.Fatalf("a bad priority is refused in the inbox's words, got %v", err)
	}
}

// A cron tick carries no work, so it files nothing; one replayed with an
// entry in it files that entry.
func TestACronTickFilesNothingUnlessItCarriesAnEntry(t *testing.T) {
	tick := json.RawMessage(`{"fired_at":"2026-11-06T15:05:00Z","expression":"* * * * *","timezone":"UTC"}`)
	got, err := Translate(Meta{Source: "cron", EventType: "tick", IdempotencyKey: "* * * * *@2026-11-06T15:05Z"}, tick, "")
	if err != nil || got.File != nil || !strings.Contains(got.Ignored, "cron tick") {
		t.Fatalf("a bare tick: %+v, %v", got, err)
	}
	with, err := Translate(Meta{Source: "cron", EventType: "tick", IdempotencyKey: "k"}, json.RawMessage(`{"fired_at":"x","entry":{"title":"Nightly tidy","external_id":"nightly"}}`), "")
	if err != nil || with.File == nil || with.File.Source != "agentfield:cron" {
		t.Fatalf("a tick with an entry: %+v, %v", with, err)
	}
}

// GITHUB STAYS NATIVE: an AgentField github event is a nudge, never an entry.
func TestAGitHubEventIsANudgeNeverAnEntry(t *testing.T) {
	got, err := Translate(Meta{Source: "github", EventType: "issues"}, fixture(t, "github_issues_opened.json"), factory.TierOwner)
	if err != nil || got.File != nil || got.Nudge != "santoshkumarradha/factory-demo" {
		t.Fatalf("Translate = %+v, %v", got, err)
	}
}

func TestTrustIsCappedByTheCeiling(t *testing.T) {
	for _, c := range []struct{ said, ceiling, want factory.Tier }{
		{"", "", factory.TierCollab},
		{"owner", "", factory.TierCollab},
		{"owner", "owner", factory.TierOwner},
		{"stranger", "owner", factory.TierStranger},
		{"collaborator", "stranger", factory.TierStranger},
		{"boss", "owner", factory.TierStranger},
	} {
		if got := capTrust(c.said, c.ceiling); got != c.want {
			t.Errorf("capTrust(%q, %q) = %q, want %q", c.said, c.ceiling, got, c.want)
		}
	}
}

// AN EVENT THAT NAMES A REASONER FILES A JOB THAT REASONER RUNS: run_on
// survives the translation and the inbox's own check.
func TestAnEventWithRunOnFilesARunOnJob(t *testing.T) {
	out, err := Translate(Meta{Source: "generic_bearer", EventID: "ev1"}, json.RawMessage(`{"title":"ledger total wrong","body":"refund -500 printed 0.-50","run_on":"workaf-support.handle_ticket"}`), factory.TierCollab)
	if err != nil {
		t.Fatal(err)
	}
	if out.File == nil || out.File.RunOn != "workaf-support.handle_ticket" || out.File.Title != "ledger total wrong" {
		t.Fatalf("filed %+v", out.File)
	}
}

// An event's labels and tags both become the job's labels, each once, with
// nothing for the sender to do but send them.
func TestAnEventsLabelsAndTagsAreItsLabels(t *testing.T) {
	out, err := Translate(Meta{Source: "generic_bearer", EventID: "ev2"}, json.RawMessage(`{"title":"refund failed","labels":["billing"],"tags":["urgent","billing"," "],"run_on":"support.triage"}`), factory.TierCollab)
	if err != nil {
		t.Fatal(err)
	}
	if out.File == nil || strings.Join(out.File.Labels, ",") != "billing,urgent" {
		t.Fatalf("filed labels %v", out.File.Labels)
	}
}

// The run's page on the control plane's own UI.
func TestARunsPageIsOnTheControlPlanesUI(t *testing.T) {
	c := &Client{URL: "http://cp:18741/"}
	if got := c.RunURL("run_20261107_1"); got != "http://cp:18741/ui/runs/run_20261107_1" {
		t.Fatalf("RunURL is %q", got)
	}
	if (&Client{}).RunURL("run_1") != "" || c.RunURL("") != "" {
		t.Fatal("a client with no address, or no run, names a page")
	}
}
