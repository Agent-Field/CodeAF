package connect

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestCalendarListFormatsEvents(t *testing.T) {
	var query url.Values
	mux := http.NewServeMux()
	mux.HandleFunc("/calendar/v3/calendars/primary/events", func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()
		writeJSON(t, w, map[string]any{
			"items": []map[string]any{
				{
					"summary":  "Standup",
					"location": "Room 2",
					"start":    map[string]string{"dateTime": "2026-08-16T09:00:00Z"},
					"end":      map[string]string{"dateTime": "2026-08-16T09:30:00Z"},
					"attendees": []map[string]any{
						{"email": "a@example.com"},
						{"email": "b@example.com"},
						{"email": "room-2@example.com", "resource": true},
					},
				},
				{
					"summary": "Company holiday",
					"start":   map[string]string{"date": "2026-08-17"},
					"end":     map[string]string{"date": "2026-08-18"},
				},
			},
		})
	})
	fakeService(t, mux)

	out, err := CalendarList(context.Background(), &http.Client{}, "2026-08-16", "2026-08-17")
	if err != nil {
		t.Fatalf("CalendarList: %v", err)
	}

	start, _ := time.Parse(time.RFC3339, "2026-08-16T09:00:00Z")
	finish, _ := time.Parse(time.RFC3339, "2026-08-16T09:30:00Z")
	span := start.Local().Format("2006-01-02 15:04") + "-" + finish.Local().Format("15:04")

	for _, want := range []string{
		"2 events between 2026-08-16 and 2026-08-17.",
		span + " | Standup | 2 people | Room 2",
		"2026-08-17 all day | Company holiday",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the listing is missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "3 people") {
		t.Errorf("a booked room is not a person:\n%s", out)
	}
	if query.Get("singleEvents") != "true" || query.Get("orderBy") != "startTime" {
		t.Errorf("recurring events must be expanded and ordered, got %v", query)
	}
}

// TestCalendarListTreatsABareDateAsAWholeDay holds the law on the window:
// asking for one date must return that date, not nothing.
func TestCalendarListTreatsABareDateAsAWholeDay(t *testing.T) {
	var query url.Values
	mux := http.NewServeMux()
	mux.HandleFunc("/calendar/v3/calendars/primary/events", func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()
		writeJSON(t, w, map[string]any{"items": []any{}})
	})
	fakeService(t, mux)

	out, err := CalendarList(context.Background(), &http.Client{}, "2026-08-16", "2026-08-16")
	if err != nil {
		t.Fatalf("CalendarList: %v", err)
	}

	from, err := time.Parse(time.RFC3339, query.Get("timeMin"))
	if err != nil {
		t.Fatalf("timeMin %q: %v", query.Get("timeMin"), err)
	}
	to, err := time.Parse(time.RFC3339, query.Get("timeMax"))
	if err != nil {
		t.Fatalf("timeMax %q: %v", query.Get("timeMax"), err)
	}
	if got := to.Sub(from); got != 24*time.Hour {
		t.Errorf("one bare date must be a whole day, got %v", got)
	}
	if got := from.Local().Format(dateOnly); got != "2026-08-16" {
		t.Errorf("the window must start on the named day, got %s", got)
	}
	// The window is said back in the caller's own words, not in the
	// resolved form that ends the following morning.
	if out != "No events between 2026-08-16 and 2026-08-16." {
		t.Errorf("got %q", out)
	}
}

func TestCalendarListAcceptsFullTimestamps(t *testing.T) {
	var query url.Values
	mux := http.NewServeMux()
	mux.HandleFunc("/calendar/v3/calendars/primary/events", func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()
		writeJSON(t, w, map[string]any{"items": []any{}})
	})
	fakeService(t, mux)

	if _, err := CalendarList(context.Background(), &http.Client{},
		"2026-08-16T09:00:00Z", "2026-08-16T17:00:00Z"); err != nil {
		t.Fatalf("CalendarList: %v", err)
	}
	from, _ := time.Parse(time.RFC3339, query.Get("timeMin"))
	to, _ := time.Parse(time.RFC3339, query.Get("timeMax"))
	if got := to.Sub(from); got != 8*time.Hour {
		t.Errorf("the window must be the one that was asked for, got %v", got)
	}
}

func TestCalendarListDefaultsToTheWeekAhead(t *testing.T) {
	var query url.Values
	mux := http.NewServeMux()
	mux.HandleFunc("/calendar/v3/calendars/primary/events", func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()
		writeJSON(t, w, map[string]any{"items": []any{}})
	})
	fakeService(t, mux)

	if _, err := CalendarList(context.Background(), &http.Client{}, "", ""); err != nil {
		t.Fatalf("CalendarList: %v", err)
	}
	from, _ := time.Parse(time.RFC3339, query.Get("timeMin"))
	to, _ := time.Parse(time.RFC3339, query.Get("timeMax"))
	if got := to.Sub(from); got != defaultWindow {
		t.Errorf("naming nothing must mean the week ahead, got %v", got)
	}
	if time.Since(from) > time.Minute {
		t.Errorf("naming no start must mean now, got %v", from)
	}
}

func TestCalendarListRefusesAnUnreadableDate(t *testing.T) {
	fakeService(t, http.NewServeMux())
	_, err := CalendarList(context.Background(), &http.Client{}, "next tuesday", "")
	if err == nil {
		t.Fatal("an unreadable date must be an error")
	}
	if !strings.Contains(err.Error(), "2026-08-16") {
		t.Errorf("the error must show the shape it wants, got %q", err)
	}
}

func TestCalendarListSwapsABackwardsWindow(t *testing.T) {
	var query url.Values
	mux := http.NewServeMux()
	mux.HandleFunc("/calendar/v3/calendars/primary/events", func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()
		writeJSON(t, w, map[string]any{"items": []any{}})
	})
	fakeService(t, mux)

	out, err := CalendarList(context.Background(), &http.Client{}, "2026-08-20", "2026-08-18")
	if err != nil {
		t.Fatalf("CalendarList: %v", err)
	}
	from, _ := time.Parse(time.RFC3339, query.Get("timeMin"))
	to, _ := time.Parse(time.RFC3339, query.Get("timeMax"))
	if !from.Before(to) {
		t.Errorf("a window given backwards must be turned around, got %v to %v", from, to)
	}
	if out != "No events between 2026-08-18 and 2026-08-20." {
		t.Errorf("the summary must follow the window it turned around, got %q", out)
	}
}

func TestCalendarListIsBounded(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/calendar/v3/calendars/primary/events", func(w http.ResponseWriter, r *http.Request) {
		items := make([]map[string]any, 0, maxEvents)
		for i := 0; i < maxEvents; i++ {
			items = append(items, map[string]any{
				"summary":  strings.Repeat("a long meeting title ", 12),
				"location": strings.Repeat("a long room name ", 12),
				"start":    map[string]string{"dateTime": "2026-08-16T09:00:00Z"},
				"end":      map[string]string{"dateTime": "2026-08-16T09:30:00Z"},
			})
		}
		writeJSON(t, w, map[string]any{"items": items})
	})
	fakeService(t, mux)

	out, err := CalendarList(context.Background(), &http.Client{}, "2026-08-16", "2026-08-23")
	if err != nil {
		t.Fatalf("CalendarList: %v", err)
	}
	if len(out) > maxToolText+200 {
		t.Errorf("the answer is %d bytes, over the %d cap", len(out), maxToolText)
	}
	if !strings.Contains(out, "Shortened here") {
		t.Errorf("a cut must be announced")
	}
}

func TestCalendarListReportsARefusal(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/calendar/v3/calendars/primary/events", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"Calendar usage limits exceeded."}}`, http.StatusForbidden)
	})
	fakeService(t, mux)

	if _, err := CalendarList(context.Background(), &http.Client{}, "2026-08-16", "2026-08-17"); err == nil {
		t.Fatal("a refusal must be an error")
	} else if !strings.Contains(err.Error(), "usage limits exceeded") {
		t.Errorf("the error must carry the service's own sentence, got %q", err)
	}
}
