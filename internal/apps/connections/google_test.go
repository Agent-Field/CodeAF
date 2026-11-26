package connections

import (
	"bytes"
	"context"

	"errors"
	"github.com/Agent-Field/codeaf/internal/apps/data"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestGoogleGmailMetadata(t *testing.T) {
	recording, err := os.ReadFile("testdata/google/message.json")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/gmail/v1/users/me/messages":
			if req.URL.Query().Get("q") != "is:unread" {
				t.Error("Gmail did not receive the declared query")
			}
			_, _ = w.Write([]byte(`{"messages":[{"id":"m1","threadId":"t1"}],"resultSizeEstimate":1}`))
		case "/gmail/v1/users/me/messages/m1":
			fields := req.URL.Query().Get("fields")
			if fields == "" || strings.Contains(fields, "data") {
				t.Error("metadata request would include the body")
			}
			_, _ = w.Write(recording)
		default:
			t.Error("unexpected request", req.URL.Path)
			http.NotFound(w, req)
		}
	}))
	defer server.Close()
	adapter := googleSource{client: server.Client(), origin: server.URL}
	rows, more, err := adapter.rows(context.Background(), "gmail", map[string]any{"q": "is:unread"}, 10)
	if err != nil || more || len(rows) != 1 {
		t.Fatalf("rows=%v more=%v err=%v", rows, more, err)
	}
	row := rows[0]
	if row["subject"] != "Planning" || row["from"] != "ari@example.com" || row["thread"] != "t1" ||
		!strings.Contains(row["attachments"].(string), "agenda.pdf") {
		t.Fatalf("unexpected message: %v", row)
	}
}

func TestGoogleHistoryHonorsQueryAndPageCursor(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case gmailPath + "/profile":
			_, _ = w.Write([]byte(`{"historyId":"19"}`))
		case gmailPath + "/history":
			q := req.URL.Query()
			if q.Get("startHistoryId") != "19" {
				t.Errorf("cursor = %s", q.Get("startHistoryId"))
			}
			if q.Get("pageToken") == "second" {
				_, _ = w.Write([]byte(`{"historyId":"21"}`))
				return
			}
			_, _ = w.Write([]byte(`{"historyId":"21","nextPageToken":"second","history":[{"messagesAdded":[{"message":{"id":"m1"}},{"message":{"id":"excluded"}}]}]}`))
		case gmailPath + "/messages":
			if q := req.URL.Query().Get("q"); !strings.Contains(q, "from:ari@example.com") {
				t.Errorf("query = %s", q)
			}
			_, _ = w.Write([]byte(`{"messages":[{"id":"m1"},{"id":"old"}]}`))
		default:
			t.Error("unexpected request", req.URL.Path)
			http.NotFound(w, req)
		}
	}))
	defer server.Close()
	adapter := googleSource{client: server.Client(), origin: server.URL}
	ctx := context.Background()
	first, changes, err := adapter.gmailChanges(ctx, GoogleCheckpoint{StartedAt: time.Unix(1790000000, 0)}, "from:ari@example.com")
	if err != nil || first.Cursor != "19" || len(changes) != 0 {
		t.Fatalf("bootstrap %v %v %v", first, changes, err)
	}
	second, changes, err := adapter.gmailChanges(ctx, first, "from:ari@example.com")
	if err != nil || second.Cursor != "19" || second.Page != "second" || len(changes) != 1 || changes[0].id != "m1" {
		t.Fatalf("history page %v %v %v", second, changes, err)
	}
	final, changes, err := adapter.gmailChanges(ctx, second, "from:ari@example.com")
	if err != nil || final.Cursor != "21" || final.Page != "" || len(changes) != 0 {
		t.Fatalf("history checkpoint %v %v %v", final, changes, err)
	}
}

func TestGoogleCalendarSyncAndStartWindow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		q := req.URL.Query()
		if q.Get("syncToken") == "expired" {
			w.WriteHeader(http.StatusGone)
			return
		}
		if q.Get("syncToken") == "calendar-1" {
			_, _ = w.Write([]byte(`{"nextSyncToken":"calendar-2","items":[{"id":"e1","status":"cancelled"}]}`))
			return
		}
		_, _ = w.Write(fixtureCalendar)
	}))
	defer server.Close()
	g := googleSource{client: server.Client(), origin: server.URL}
	ctx := context.Background()
	page, err := g.calendarChanges(ctx, "", "", time.Time{})
	if err != nil || page.NextSyncToken != "calendar-1" || len(page.events()) != 1 {
		t.Fatalf("first page=%+v err=%v", page, err)
	}
	page, err = g.calendarChanges(ctx, page.NextSyncToken, "", time.Time{})
	if err != nil || page.NextSyncToken != "calendar-2" || len(page.events()) != 0 {
		t.Fatalf("cancelled page=%+v err=%v", page, err)
	}
	if _, err := g.calendarChanges(ctx, "expired", "", time.Time{}); !errors.Is(err, ErrCursorExpired) {
		t.Fatalf("expired cursor: %v", err)
	}
	before := time.Date(2026, 11, 26, 15, 59, 59, 0, time.UTC)
	checkpoint, changes, err := g.calendarStarts(ctx, GoogleCheckpoint{}, before, 30)
	if err != nil || len(changes) != 0 || len(checkpoint.Calendar) != 1 {
		t.Fatalf("future event: %+v %v %v", checkpoint, changes, err)
	}
	// A stale client sleeping past the event still applies the incremental
	// cancellation first, so it never starts an already cancelled meeting.
	_, changes, err = g.calendarStarts(ctx, checkpoint, before.Add(time.Hour), 30)
	if err != nil || len(changes) != 0 {
		t.Fatalf("cancelled due event: %+v %v", changes, err)
	}
}

func TestGoogleBodyOnDemandAndDriveACL(t *testing.T) {
	exports := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case gmailPath + "/messages/m1":
			if req.URL.Query().Get("format") != "full" || req.URL.Query().Get("fields") != "" {
				t.Error("body must be fetched separately")
			}
			_, _ = w.Write([]byte(`{"payload":{"mimeType":"text/plain","body":{"data":"UHJpdmF0ZSB0ZXh0"}}}`))
		case drivePath + "/hidden":
			w.WriteHeader(http.StatusNotFound)
		case drivePath + "/limited":
			_, _ = w.Write([]byte(`{"id":"limited","mimeType":"application/vnd.google-apps.document","capabilities":{"canDownload":false}}`))
		case drivePath + "/allowed":
			_, _ = w.Write([]byte(`{"id":"allowed","mimeType":"application/vnd.google-apps.document","capabilities":{"canDownload":true}}`))
		case drivePath + "/allowed/export":
			exports++
			_, _ = w.Write([]byte("Allowed text"))
		default:
			t.Error("unexpected request", req.URL.Path)
			http.NotFound(w, req)
		}
	}))
	defer server.Close()
	g := googleSource{client: server.Client(), origin: server.URL}
	ctx := context.Background()
	body, err := g.content(ctx, "gmail", "m1")
	if err != nil || body != "Private text" {
		t.Fatalf("body=%q err=%v", body, err)
	}
	for _, key := range []string{"hidden", "limited"} {
		if body, err := g.content(ctx, "drive", key); err == nil || body != "" {
			t.Errorf("disallowed %s: %q %v", key, body, err)
		}
	}
	if body, err := g.content(ctx, "drive", "allowed"); err != nil || body != "Allowed text" || exports != 1 {
		t.Fatalf("allowed export %q %v, calls=%d", body, err, exports)
	}
	var service Service
	if _, err := service.ReadGoogleContent(ctx, Query{}, "", 0); err == nil || !strings.Contains(err.Error(), "outside harness") {
		t.Fatalf("outside harness read: %v", err)
	}
}

func TestGoogleMailIndexRefreshesChangedMetadata(t *testing.T) {
	var metadataReads int
	var changed bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case gmailPath + "/profile":
			_, _ = w.Write([]byte(`{"historyId":"19"}`))
		case gmailPath + "/history":
			if changed {
				_, _ = w.Write([]byte(`{"historyId":"20","history":[{"labelsRemoved":[{"message":{"id":"m1"},"labelIds":["UNREAD"]}]}]}`))
			} else {
				_, _ = w.Write([]byte(`{"historyId":"19"}`))
			}
		case gmailPath + "/messages":
			_, _ = w.Write([]byte(`{"messages":[{"id":"m1"}]}`))
		case gmailPath + "/messages/m1":
			metadataReads++
			raw := fixtureMessage
			if changed {
				raw = bytes.ReplaceAll(raw, []byte(`, "UNREAD"`), nil)
			}
			_, _ = w.Write(raw)
		default:
			t.Errorf("unexpected URL %s", req.URL.Path)
			http.NotFound(w, req)
		}
	}))
	defer server.Close()
	place := data.NewPlace(t.TempDir(), data.Options{NoRefresh: true})
	defer place.Close()
	db, err := place.Main()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.PublishApp(data.AppPublication{App: "brief", Version: 1, Metadata: []byte(`{"digest":"brief"}`),
		Definition: []byte(`{"id":"brief","name":"Brief","version":1,"kit":0}`)}, 0); err != nil {
		t.Fatal(err)
	}
	adapter := googleSource{client: server.Client(), origin: server.URL}
	ctx := context.Background()
	read := func(want string, wantReads int) {
		t.Helper()
		index, key, prev, err := adapter.loadMailIndex(ctx, db, "brief", "google", "messages", "grant")
		if err != nil {
			t.Fatal(err)
		}
		rows, _, err := adapter.indexedMail(ctx, &index, "", 10)
		if err != nil || len(rows) != 1 || rows[0]["labels"] != want || metadataReads != wantReads {
			t.Fatalf("rows=%+v metadata reads=%d err=%v", rows, metadataReads, err)
		}
		if err := saveMailIndex(db, "brief", "google", key, prev, index); err != nil {
			t.Fatal(err)
		}
	}
	read("INBOX, UNREAD", 1)
	read("INBOX, UNREAD", 1)
	changed = true
	read("INBOX", 2)
}

func TestGoogleDriveChangesRequireCurrentFileAccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/drive/v3/changes/startPageToken":
			_, _ = w.Write([]byte(`{"startPageToken":"drive-1"}`))
		case "/drive/v3/changes":
			_, _ = w.Write([]byte(`{"newStartPageToken":"drive-2","changes":[
				{"fileId":"hidden","file":{"id":"hidden","name":"Meet Transcript","mimeType":"application/vnd.google-apps.document","createdTime":"2026-11-26T10:01:00Z"}},
				{"fileId":"visible","file":{"id":"visible","name":"Meet Transcript","mimeType":"application/vnd.google-apps.document","createdTime":"2026-11-26T10:01:00Z"}},
				{"fileId":"removed","removed":true}
			]}`))
		case drivePath + "/hidden":
			w.WriteHeader(http.StatusNotFound)
		case drivePath + "/visible":
			_, _ = w.Write([]byte(`{"id":"visible","name":"Meet Transcript","mimeType":"application/vnd.google-apps.document"}`))
		default:
			t.Errorf("unexpected URL %s", req.URL.Path)
			http.NotFound(w, req)
		}
	}))
	defer server.Close()
	adapter := googleSource{client: server.Client(), origin: server.URL}
	ctx := context.Background()
	state, changes, err := adapter.driveChanges(ctx, GoogleCheckpoint{StartedAt: time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC)})
	if err != nil || state.Cursor != "drive-1" || len(changes) != 0 {
		t.Fatalf("baseline=%+v %v %v", state, changes, err)
	}
	state, changes, err = adapter.driveChanges(ctx, state)
	if err != nil || state.Cursor != "drive-2" || len(changes) != 1 || changes[0].id != "visible" {
		t.Fatalf("changes=%+v %v %v", state, changes, err)
	}
}

func TestGoogleReferenceProjectionResolvesLinkedPeopleAndAttachments(t *testing.T) {
	ctx := context.Background()
	place := data.NewPlace(t.TempDir(), data.Options{NoRefresh: true})
	defer place.Close()
	db, err := place.Main()
	if err != nil {
		t.Fatal(err)
	}
	const manifest = `{"id":"brief","name":"Brief","kit":0,"version":1,
		"records":{
			"message":{"title":"subject","fields":{
				"subject":{"type":"text"},"sender":{"type":"ref","to":"person"},
				"attachment":{"type":"ref","to":"document"}
			}},
			"person":{"title":"name","fields":{"name":{"type":"text"}}},
			"document":{"title":"title","fields":{"title":{"type":"text"}}}
		},
		"capabilities":{"records":{"message":"r","person":"r","document":"r"}}}`
	if err := db.PublishApp(data.AppPublication{App: "brief", Version: 1,
		Metadata: []byte(`{"digest":"refs"}`), Definition: []byte(manifest)}, 0); err != nil {
		t.Fatal(err)
	}
	for common, typ := range map[data.CommonTypeKey]string{data.CommonMessage: "message", data.CommonPerson: "person", data.CommonDocument: "document"} {
		if err := db.BindCommonType(common, typ); err != nil {
			t.Fatal(err)
		}
	}
	service := Service{Profile: t.TempDir(), Store: func() (*data.Store, error) { return db, nil }}
	if err := service.Register(Config{ID: "source", App: "brief", Kind: Google, CredentialMode: CredentialPerson, GoogleFixture: true,
		Reads: map[string]Read{
			"messages": {Type: "message", Table: "gmail", Key: "id", GoogleLinkIdentities: true,
				Fields: map[string]string{"subject": "subject", "sender": "from", "attachment": "attachments"}},
			"docs": {Type: "document", Table: "attachments", Key: "id", Fields: map[string]string{"title": "title"}},
		},
	}, ""); err != nil {
		t.Fatal(err)
	}
	query := Query{App: "brief", Connection: "source", Read: "messages"}
	page, err := service.Read(ctx, query)
	if err != nil || len(page.Rows) != 1 || page.Rows[0]["sender"] != nil {
		t.Fatalf("unlinked sender %+v %v", page, err)
	}
	attachmentID, _ := page.Rows[0]["attachment"].(string)
	if attachmentID == "" {
		t.Fatal("message did not link to its document")
	}
	doc, _, err := service.GetRecord(ctx, "brief", "document", attachmentID, false)
	if err != nil || doc["title"] != "agenda.pdf" {
		t.Fatalf("attachment %+v %v", doc, err)
	}
	if err := service.LinkGoogleIdentities(ctx, query); !errors.Is(err, errGoogleFixtureIdentity) {
		t.Fatalf("fixture identities must not enter the live crosswalk: %v", err)
	}
	if _, found, err := db.LookupIdentity(googleIdentityKey("email", "ari@example.com")); err != nil || found {
		t.Fatalf("the test read created an identity: found=%v err=%v", found, err)
	}
	if _, err := db.LinkIdentity(data.CommonPerson, googleIdentityKey("email", "ari@example.com"), ""); err != nil {
		t.Fatal(err)
	}
	page, err = service.Read(ctx, query)
	if err != nil || len(page.Rows) != 1 || page.Rows[0]["sender"] == nil {
		t.Fatalf("linked sender %+v %v", page, err)
	}
	_, info, _, err := service.PlanQuery(ctx, "brief", data.QueryIR{Type: "message", Select: []string{"attachment.title"}})
	if err != nil || len(info.Contributors) != 1 || info.Contributors[0].Read != "docs" {
		t.Fatalf("related Google source %+v %v", info, err)
	}
}

func TestGooglePartialWindowListsRowsButRefusesTotals(t *testing.T) {
	ctx := context.Background()
	place := data.NewPlace(t.TempDir(), data.Options{NoRefresh: true})
	defer place.Close()
	db, err := place.Main()
	if err != nil {
		t.Fatal(err)
	}
	const manifest = `{"id":"brief","name":"Brief","kit":0,"version":1,
		"records":{"message":{"title":"subject","fields":{"subject":{"type":"text"}}}},
		"capabilities":{"records":{"message":"r"}}}`
	if err := db.PublishApp(data.AppPublication{App: "brief", Version: 1,
		Metadata: []byte(`{"digest":"window"}`), Definition: []byte(manifest)}, 0); err != nil {
		t.Fatal(err)
	}
	if err := db.BindCommonType(data.CommonMessage, "message"); err != nil {
		t.Fatal(err)
	}
	svc := Service{Profile: t.TempDir(), Store: func() (*data.Store, error) { return db, nil }}
	if err := svc.Register(Config{ID: "work", App: "brief", Kind: Google, CredentialMode: CredentialPerson, GoogleFixture: true,
		Reads: map[string]Read{"messages": {Type: "message", Table: "gmail", Key: "id", GoogleLimit: 1,
			GoogleQuery: "label:planning", Fields: map[string]string{"subject": "subject"}}},
	}, ""); err != nil {
		t.Fatal(err)
	}
	ir := data.QueryIR{Type: "message"}
	planned, source, _, err := svc.PlanQuery(ctx, "brief", ir)
	if err != nil || !source.Partial || source.Window != 1 {
		t.Fatalf("source window = %+v, err = %v", source, err)
	}
	page, err := db.QueryRows(data.Scope{App: "brief"}, &planned, "", 10)
	if err != nil || len(page.Rows) != 1 || page.Rows[0]["subject"] != "Planning" {
		t.Fatalf("rows in the partial window = %+v, err = %v", page, err)
	}
	ir.Aggregate = []data.Agg{{Fn: "count"}}
	if _, _, _, err := svc.PlanQuery(ctx, "brief", ir); !errors.Is(err, ErrPartialWindow) {
		t.Fatalf("partial total: %v", err)
	}
}
