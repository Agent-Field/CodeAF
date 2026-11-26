package connections

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
)

func graphFixture(t *testing.T, w http.ResponseWriter, name string) {
	t.Helper()
	raw, err := os.ReadFile("testdata/graph/" + name)
	if err != nil {
		t.Fatal(err)
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(raw)
}

func TestGraphMailDeltaAndBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Prefer"); !strings.Contains(got, "ImmutableId") {
			t.Errorf("Graph must return immutable Outlook IDs, got %q", got)
		}
		query := r.URL.Query()
		switch {
		case r.URL.Path == "/v1.0/me/messages/AAMkMail1":
			if !strings.Contains(query.Get("$select"), "body") || !strings.Contains(r.Header.Get("Prefer"), "outlook.body-content-type") {
				t.Error("the explicit body read should request plain text")
			}
			graphFixture(t, w, "mail-body.json")
		case r.URL.Path != "/v1.0/me/mailFolders/inbox/messages/delta":
			t.Errorf("unexpected Graph path %q", r.URL.Path)
			http.NotFound(w, r)
		case query.Get("$skiptoken") == "initial-2":
			graphFixture(t, w, "mail-next.json")
		case query.Get("$deltatoken") == "round-1":
			graphFixture(t, w, "mail-delta.json")
		default:
			if selectFields := query.Get("$select"); selectFields == "" || strings.Contains(strings.ToLower(selectFields), "body") {
				t.Errorf("metadata must exclude body and preview, got %q", selectFields)
			}
			graphFixture(t, w, "mail-initial.json")
		}
	}))
	defer server.Close()
	g := graphSource{client: server.Client(), origin: server.URL}
	ctx := context.Background()
	read := Read{Table: "outlook"}
	initial, err := g.rows(ctx, read, "", "", nil, 25, false)
	if err != nil || len(initial.rows) != 1 || len(initial.removed) != 0 || initial.next == "" || initial.checkpoint != "" {
		t.Fatalf("initial page: %+v, %v", initial, err)
	}
	row := initial.rows[0]
	if row["subject"] != "Review the launch brief" || row["from"] != "mira@asterloom.example" || row["body"] != nil {
		t.Fatalf("unexpected normalized message: %v", row)
	}
	last, err := g.rows(ctx, read, initial.next, "", nil, 25, false)
	if err != nil || last.next != "" || last.checkpoint == "" {
		t.Fatalf("last page: %+v, %v", last, err)
	}
	changed, err := g.rows(ctx, read, last.checkpoint, "", nil, 25, false)
	if err != nil || len(changed.removed) != 1 || changed.removed[0] != "AAMkMail1" ||
		len(changed.rows) != 1 || changed.rows[0]["id"] != "AAMkMail2" {
		t.Fatalf("changed page: %+v, %v", changed, err)
	}
	point, err := g.rows(ctx, read, "", "AAMkMail1", nil, 1, true)
	if err != nil || len(point.rows) != 1 || point.rows[0]["body"] != "The launch brief is ready for review." {
		t.Fatalf("body read: %+v, %v", point, err)
	}
	_, err = g.rows(ctx, read, "https://other.example/v1.0/me/messages", "", nil, 25, false)
	if !errors.Is(err, ErrGraphCursor) {
		t.Fatalf("external continuation: %v", err)
	}
}

func TestGraphDraftWriteUsesOnlyCreateMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1.0/me/messages" {
			t.Errorf("draft must only POST /me/messages, got %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		var got, want any
		expected, err := os.ReadFile("testdata/graph/draft-request.json")
		if err != nil {
			t.Fatal(err)
		}
		if json.NewDecoder(r.Body).Decode(&got) != nil || json.Unmarshal(expected, &want) != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("draft payload: got %v, want %v", got, want)
		}
		w.WriteHeader(http.StatusCreated)
		graphFixture(t, w, "draft-response.json")
	}))
	defer server.Close()
	g := graphSource{client: server.Client(), origin: server.URL}
	body, err := graphPayload(graphDraftAction, map[string]any{
		"to": "mira@asterloom.example", "subject": "Re: Review the launch brief", "body": "I'll review it today.",
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := g.apply(context.Background(), graphDraftAction, "", "", body, "run-key")
	if err != nil || result.ID != "AAMkDraft1" || result.ETag != `W/"draft-1"` {
		t.Fatalf("draft result: %+v, %v", result, err)
	}
}

func TestGraphPermissionFilteringAndTeamsTenantDenial(t *testing.T) {
	fixtures := graphFake{}
	var contentRequests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/content") {
			contentRequests++
		}
		if strings.HasSuffix(r.URL.Path, "/transcripts") {
			w.WriteHeader(http.StatusForbidden)
			graphFixture(t, w, "teams-denied.json")
			return
		}
		reply, err := fixtures.RoundTrip(r)
		if err != nil {
			t.Error(err)
			w.WriteHeader(500)
			return
		}
		defer reply.Body.Close()
		w.WriteHeader(reply.StatusCode)
		_, _ = io.Copy(w, reply.Body)
	}))
	defer server.Close()
	g := graphSource{client: server.Client(), origin: server.URL, transcripts: true}
	ctx := context.Background()
	if page, err := g.point(ctx, Read{Table: "drive"}, "01OTHER", true); !errors.Is(err, ErrSourcePermission) || len(page.rows) != 0 || contentRequests != 0 {
		t.Fatalf("file ACL: %+v %v, body requests %d", page, err, contentRequests)
	}
	if page, err := g.rows(ctx, Read{Table: "transcripts"}, "", "", map[string]any{"meeting": "AAMkEvent1"}, 25, false); !errors.Is(err, errTeamsDisabled) || len(page.rows) != 0 {
		t.Fatalf("tenant ACL: %+v %v", page, err)
	}
}

func TestGraphCalendarWritesUseTransactionIDAndCompareAndSet(t *testing.T) {
	var transaction string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1.0/me/events":
			current, _ := body["transactionId"].(string)
			if current == "" || transaction != "" && transaction != current {
				t.Errorf("same broker key changed the Graph transaction ID: %q then %q", transaction, current)
			}
			transaction = current
			if body["subject"] != "Daily launch review" || !reflect.DeepEqual(body["start"],
				map[string]any{"dateTime": "2026-11-26T12:30:00", "timeZone": "UTC"}) {
				t.Errorf("unexpected Graph event create payload: %v", body)
			}
			w.WriteHeader(http.StatusCreated)
			graphFixture(t, w, "event-create-response.json")
		case r.Method == http.MethodPatch && r.URL.Path == "/v1.0/me/events/AAMkEvent1":
			if !reflect.DeepEqual(body, map[string]any{"subject": "Daily launch review moved"}) {
				t.Errorf("patch replaced unrelated event fields: %v", body)
			}
			if r.Header.Get("If-Match") != `W/"event-version-1"` {
				w.WriteHeader(http.StatusPreconditionFailed)
				return
			}
			graphFixture(t, w, "event-response.json")
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	g := graphSource{client: server.Client(), origin: server.URL}
	ctx := context.Background()
	create, err := graphPayload(graphEventCreateAction, map[string]any{
		"title": "Daily launch review", "start": "2026-11-26T07:30:00-05:00", "end": "2026-11-26T08:00:00-05:00",
	})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		entry, err := g.apply(ctx, graphEventCreateAction, "", "", create, "stable-broker-key")
		if err != nil || entry.ID != "AAMkEvent2" {
			t.Fatalf("event create: %+v %v", entry, err)
		}
	}
	update, err := graphPayload(graphEventUpdateAction, map[string]any{"title": "Daily launch review moved"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.apply(ctx, graphEventUpdateAction, "AAMkEvent1", `W/"stale"`, update, "update-key"); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale write: %v", err)
	}
	entry, err := g.apply(ctx, graphEventUpdateAction, "AAMkEvent1", `W/"event-version-1"`, update, "update-key")
	if err != nil || entry.ETag != `W/"event-version-2"` {
		t.Fatalf("event update: %+v %v", entry, err)
	}
}
