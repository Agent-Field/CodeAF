package connections

import (
	"context"
	"errors"
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
	page, err := g.calendarChanges(ctx, "", "")
	if err != nil || page.NextSyncToken != "calendar-1" || len(page.events()) != 1 {
		t.Fatalf("first page=%+v err=%v", page, err)
	}
	page, err = g.calendarChanges(ctx, page.NextSyncToken, "")
	if err != nil || page.NextSyncToken != "calendar-2" || len(page.events()) != 0 {
		t.Fatalf("cancelled page=%+v err=%v", page, err)
	}
	if _, err := g.calendarChanges(ctx, "expired", ""); !errors.Is(err, ErrCursorExpired) {
		t.Fatalf("expired cursor: %v", err)
	}
	for _, tc := range []struct {
		now   string
		count int
	}{
		{"2026-11-26T15:59:59Z", 0}, {"2026-11-26T16:00:00Z", 1}, {"2026-11-26T16:30:01Z", 0},
	} {
		now, _ := time.Parse(time.RFC3339, tc.now)
		_, changes, err := g.upcomingEvents(ctx, GoogleCheckpoint{}, now, 30)
		if err != nil || len(changes) != tc.count {
			t.Errorf("%s: %+v %v", tc.now, changes, err)
		}
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
