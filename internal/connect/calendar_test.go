package connect

import (
	"context"
	"encoding/json"
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

// ── writing one ─────────────────────────────────────────────────────────────

// createRequest is what one CalendarCreate handed the service: the query it was
// sent with, and the event it carried.
type createRequest struct {
	query url.Values
	event map[string]any
}

// fakeCalendarWriter answers a create the way the service does and records what
// it was asked to make.
func fakeCalendarWriter(t *testing.T) *createRequest {
	t.Helper()
	got := &createRequest{}
	mux := http.NewServeMux()
	mux.HandleFunc("/calendar/v3/calendars/primary/events", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("creating an event must be a write, got %s", r.Method)
		}
		got.query = r.URL.Query()
		if err := json.NewDecoder(r.Body).Decode(&got.event); err != nil {
			t.Errorf("decode the request: %v", err)
		}
		writeJSON(t, w, map[string]any{
			"id":       "ev-1",
			"htmlLink": "https://calendar.example.test/ev-1",
		})
	})
	fakeService(t, mux)
	return got
}

func TestCalendarCreateMakesATimedEvent(t *testing.T) {
	got := fakeCalendarWriter(t)

	out, err := CalendarCreate(context.Background(), &http.Client{},
		"Standup", "2026-08-18T09:00:00Z", "2026-08-18T09:30:00Z",
		" a@example.com , b@example.com ", "Room 2", "The daily one.")
	if err != nil {
		t.Fatalf("CalendarCreate: %v", err)
	}

	if got.event["summary"] != "Standup" || got.event["location"] != "Room 2" || got.event["description"] != "The daily one." {
		t.Errorf("the event does not carry what it was given: %+v", got.event)
	}
	start, _ := got.event["start"].(map[string]any)
	finish, _ := got.event["end"].(map[string]any)
	if start["dateTime"] != "2026-08-18T09:00:00Z" || finish["dateTime"] != "2026-08-18T09:30:00Z" {
		t.Errorf("the span is wrong: %v %v", start, finish)
	}
	guests, _ := got.event["attendees"].([]any)
	if len(guests) != 2 {
		t.Errorf("the guests did not arrive: %v", got.event["attendees"])
	}
	// NAMING SOMEBODY INVITES THEM.
	if got.query.Get("sendUpdates") != "all" {
		t.Errorf("the guests must be told, got %q", got.query.Get("sendUpdates"))
	}
	// The span is said back in the person's own time zone, exactly as a listed
	// event is.
	from, _ := time.Parse(time.RFC3339, "2026-08-18T09:00:00Z")
	to, _ := time.Parse(time.RFC3339, "2026-08-18T09:30:00Z")
	span := from.Local().Format("2006-01-02 15:04") + "-" + to.Local().Format("15:04")
	for _, want := range []string{"Added Standup", span, "Invited a@example.com, b@example.com", "id ev-1", "https://calendar.example.test/ev-1"} {
		if !strings.Contains(out, want) {
			t.Errorf("the answer is missing %q: %q", want, out)
		}
	}
}

// A caller who names no end gets an hour, and a guest list nobody wrote sends
// nothing to anybody.
func TestCalendarCreateFillsInTheEnd(t *testing.T) {
	got := fakeCalendarWriter(t)

	if _, err := CalendarCreate(context.Background(), &http.Client{},
		"Think", "2026-08-18T09:00:00Z", "", "", "", ""); err != nil {
		t.Fatalf("CalendarCreate: %v", err)
	}
	finish, _ := got.event["end"].(map[string]any)
	if finish["dateTime"] != "2026-08-18T10:00:00Z" {
		t.Errorf("an event with no end must run an hour, got %v", finish)
	}
	if got.query.Get("sendUpdates") != "" {
		t.Errorf("an event with nobody invited must tell nobody, got %q", got.query.Get("sendUpdates"))
	}
	// THE EMPTINESS LAW: what was not given is not sent as an empty field.
	for _, absent := range []string{"location", "description", "attendees"} {
		if _, present := got.event[absent]; present {
			t.Errorf("%s was sent though nobody named one: %+v", absent, got.event)
		}
	}
}

// THE DAY YOU NAME IS INCLUDED, even though the service counts the closing date
// as the morning after.
func TestCalendarCreateMakesAWholeDayEvent(t *testing.T) {
	for _, c := range []struct {
		name       string
		start, end string
		wantStart  string
		wantEnd    string
		wantSpoken string
	}{
		{"one day", "2026-08-18", "", "2026-08-18", "2026-08-19", "2026-08-18 all day"},
		{"the same day twice", "2026-08-18", "2026-08-18", "2026-08-18", "2026-08-19", "2026-08-18 all day"},
		{"three days", "2026-08-18", "2026-08-20", "2026-08-18", "2026-08-21", "2026-08-18 to 2026-08-20, all day"},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := fakeCalendarWriter(t)
			out, err := CalendarCreate(context.Background(), &http.Client{}, "Away", c.start, c.end, "", "", "")
			if err != nil {
				t.Fatalf("CalendarCreate: %v", err)
			}
			start, _ := got.event["start"].(map[string]any)
			finish, _ := got.event["end"].(map[string]any)
			if start["date"] != c.wantStart || finish["date"] != c.wantEnd {
				t.Errorf("the days are wrong: %v %v", start, finish)
			}
			if !strings.Contains(out, c.wantSpoken) {
				t.Errorf("the answer must say the days back: %q", out)
			}
		})
	}
}

func TestCalendarCreateNeedsATitleAndAStart(t *testing.T) {
	ctx := context.Background()
	if _, err := CalendarCreate(ctx, &http.Client{}, "  ", "2026-08-18T09:00:00Z", "", "", "", ""); err == nil {
		t.Error("an event with no title must say so")
	}
	if _, err := CalendarCreate(ctx, &http.Client{}, "Standup", "  ", "", "", "", ""); err == nil {
		t.Error("an event with no start must say so")
	}
	if _, err := CalendarCreate(ctx, &http.Client{}, "Standup", "next tuesday", "", "", "", ""); err == nil {
		t.Error("a start nobody can read must say what one looks like")
	} else if !strings.Contains(err.Error(), "2026-08-16") {
		t.Errorf("the error must show the shape it wants, got %q", err)
	}
}

func TestCalendarCreateReportsARefusal(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/calendar/v3/calendars/primary/events", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"code":403,"message":"Request had insufficient authentication scopes."}}`))
	})
	fakeService(t, mux)

	if _, err := CalendarCreate(context.Background(), &http.Client{}, "Standup", "2026-08-18T09:00:00Z", "", "", "", ""); err == nil {
		t.Fatal("a refusal must be an error")
	} else if !strings.Contains(err.Error(), "insufficient authentication scopes") {
		t.Errorf("the error must carry the service's own sentence, got %q", err)
	}
}
