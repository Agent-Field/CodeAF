package connect

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// encode packs a body the way a mail service does.
func encode(text string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(text))
}

func TestGmailSearchFormatsHits(t *testing.T) {
	var listQuery url.Values

	mux := http.NewServeMux()
	mux.HandleFunc("/gmail/v1/users/me/messages", func(w http.ResponseWriter, r *http.Request) {
		listQuery = r.URL.Query()
		writeJSON(t, w, map[string]any{
			"messages": []map[string]string{{"id": "m1"}, {"id": "m2"}},
		})
	})
	mux.HandleFunc("/gmail/v1/users/me/messages/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/gmail/v1/users/me/messages/")
		if got := r.URL.Query().Get("format"); got != "metadata" {
			t.Errorf("a listing must fetch summaries, not whole messages, got %q", got)
		}
		subject := map[string]string{"m1": "Lunch tomorrow?", "m2": "Invoice"}[id]
		writeJSON(t, w, map[string]any{
			"id":           id,
			"snippet":      "Hey, are you free at noon?",
			"internalDate": "1786785120000",
			"payload": map[string]any{
				"headers": []map[string]string{
					{"name": "From", "value": "Alice Smith <alice@example.com>"},
					{"name": "Subject", "value": subject},
				},
			},
		})
	})
	fakeService(t, mux)

	out, err := GmailSearch(context.Background(), &http.Client{}, "is:unread", 0)
	if err != nil {
		t.Fatalf("GmailSearch: %v", err)
	}
	when := time.UnixMilli(1786785120000).Local().Format("2006-01-02 15:04")

	for _, want := range []string{
		`2 messages match "is:unread".`,
		"id m1 | " + when + " | Alice Smith <alice@example.com>",
		"Lunch tomorrow?",
		"id m2 | ",
		"Invoice",
		"Hey, are you free at noon?",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the listing is missing %q:\n%s", want, out)
		}
	}
	if strings.Index(out, "id m1") > strings.Index(out, "id m2") {
		t.Errorf("the listing must keep the service's order:\n%s", out)
	}
	if got := listQuery.Get("maxResults"); got != "10" {
		t.Errorf("asking for nothing must mean the default, got %q", got)
	}
}

func TestGmailSearchBoundsWhatItAsksFor(t *testing.T) {
	var listQuery url.Values
	mux := http.NewServeMux()
	mux.HandleFunc("/gmail/v1/users/me/messages", func(w http.ResponseWriter, r *http.Request) {
		listQuery = r.URL.Query()
		writeJSON(t, w, map[string]any{})
	})
	fakeService(t, mux)

	if _, err := GmailSearch(context.Background(), &http.Client{}, "anything", 500); err != nil {
		t.Fatalf("GmailSearch: %v", err)
	}
	if got := listQuery.Get("maxResults"); got != "25" {
		t.Errorf("a caller asking for a thousand must be capped, got %q", got)
	}
}

func TestGmailSearchSaysWhenNothingMatched(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/gmail/v1/users/me/messages", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{"resultSizeEstimate": 0})
	})
	fakeService(t, mux)

	out, err := GmailSearch(context.Background(), &http.Client{}, "from:nobody", 5)
	if err != nil {
		t.Fatalf("GmailSearch: %v", err)
	}
	if out != `No messages match "from:nobody".` {
		t.Errorf("got %q", out)
	}
}

func TestGmailSearchReportsARefusal(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/gmail/v1/users/me/messages", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"code":403,"message":"Request had insufficient authentication scopes."}}`))
	})
	fakeService(t, mux)

	if _, err := GmailSearch(context.Background(), &http.Client{}, "anything", 5); err == nil {
		t.Fatal("a refusal must be an error")
	} else if !strings.Contains(err.Error(), "insufficient authentication scopes") {
		t.Errorf("the error must carry the service's own sentence, got %q", err)
	}
}

func TestGmailReadPrefersPlainText(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/gmail/v1/users/me/messages/", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("format"); got != "full" {
			t.Errorf("reading one message must ask for all of it, got %q", got)
		}
		writeJSON(t, w, map[string]any{
			"id":           "m1",
			"internalDate": "1786785120000",
			"payload": map[string]any{
				"mimeType": "multipart/alternative",
				"headers": []map[string]string{
					{"name": "From", "value": "Alice <alice@example.com>"},
					{"name": "To", "value": "Me <me@example.com>"},
					{"name": "Subject", "value": "Lunch tomorrow?"},
				},
				"parts": []map[string]any{
					{"mimeType": "text/html", "body": map[string]string{"data": encode("<p>markup version</p>")}},
					{"mimeType": "text/plain", "body": map[string]string{"data": encode("Hey,\n\n\n\nare you free at noon?\n")}},
					{"mimeType": "application/pdf", "filename": "menu.pdf", "body": map[string]string{"data": encode("never mind me")}},
				},
			},
		})
	})
	fakeService(t, mux)

	out, err := GmailRead(context.Background(), &http.Client{}, "m1")
	if err != nil {
		t.Fatalf("GmailRead: %v", err)
	}
	for _, want := range []string{
		"From: Alice <alice@example.com>",
		"To: Me <me@example.com>",
		"Subject: Lunch tomorrow?",
		"Hey,\n\nare you free at noon?",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the message is missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "markup version") {
		t.Errorf("plain text must win over markup:\n%s", out)
	}
	if strings.Contains(out, "Cc:") {
		t.Errorf("a header the message does not carry must not be printed:\n%s", out)
	}
}

func TestGmailReadFallsBackToMarkup(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/gmail/v1/users/me/messages/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{
			"id": "m1",
			"payload": map[string]any{
				"mimeType": "text/html",
				"headers":  []map[string]string{{"name": "Subject", "value": "Newsletter"}},
				"body": map[string]string{"data": encode(
					`<html><head><style>p{color:red}</style></head><body>` +
						`<p>First&nbsp;line.</p><script>alert(1)</script><div>Second line.</div></body></html>`)},
			},
		})
	})
	fakeService(t, mux)

	out, err := GmailRead(context.Background(), &http.Client{}, "m1")
	if err != nil {
		t.Fatalf("GmailRead: %v", err)
	}
	if !strings.Contains(out, "First line.") || !strings.Contains(out, "Second line.") {
		t.Errorf("the sentences must survive:\n%s", out)
	}
	if strings.Contains(out, "alert(1)") || strings.Contains(out, "color:red") || strings.Contains(out, "<") {
		t.Errorf("only the sentences may survive:\n%s", out)
	}
}

func TestGmailReadIsBounded(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/gmail/v1/users/me/messages/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{
			"id": "m1",
			"payload": map[string]any{
				"mimeType": "text/plain",
				"headers":  []map[string]string{{"name": "Subject", "value": "A very long thread"}},
				"body":     map[string]string{"data": encode(strings.Repeat("a long line of quoted history\n", 3000))},
			},
		})
	})
	fakeService(t, mux)

	out, err := GmailRead(context.Background(), &http.Client{}, "m1")
	if err != nil {
		t.Fatalf("GmailRead: %v", err)
	}
	if len(out) > maxToolText+200 {
		t.Errorf("the answer is %d bytes, over the %d cap", len(out), maxToolText)
	}
	if !strings.Contains(out, "Shortened here") {
		t.Errorf("a cut must be announced:\n%s", out[len(out)-200:])
	}
}

func TestGmailReadNeedsAMessage(t *testing.T) {
	if _, err := GmailRead(context.Background(), &http.Client{}, "  "); err == nil {
		t.Error("reading nothing must say so")
	}
}

func TestHelpersRefuseAClientlessCall(t *testing.T) {
	ctx := context.Background()
	if _, err := GmailSearch(ctx, nil, "anything", 5); err == nil {
		t.Error("GmailSearch with no connected account must fail")
	}
	if _, err := GmailRead(ctx, nil, "m1"); err == nil {
		t.Error("GmailRead with no connected account must fail")
	}
	if _, err := CalendarList(ctx, nil, "", ""); err == nil {
		t.Error("CalendarList with no connected account must fail")
	}
}

func TestBoundAnnouncesTheCut(t *testing.T) {
	short := "a line\n"
	if got := bound(short); got != short {
		t.Errorf("short text must pass through untouched, got %q", got)
	}
	long := strings.Repeat("x", maxToolText*2)
	got := bound(long)
	if len(got) > maxToolText+200 {
		t.Errorf("bound left %d bytes", len(got))
	}
	if !strings.Contains(got, "Shortened here") {
		t.Errorf("the cut must be announced: %q", got[len(got)-80:])
	}
}
