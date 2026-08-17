package connect

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// maxEvents is the ceiling on one calendar answer. A window wide enough to
// return more than this is a window the caller should narrow, and the honest
// way to say so is to fill the answer and mark it.
const maxEvents = 100

// defaultWindow is how far ahead a caller who named no end is looking. A week
// is what "what's coming up" almost always means.
const defaultWindow = 7 * 24 * time.Hour

// dateOnly is the shorthand a caller may use instead of a full timestamp.
const dateOnly = "2006-01-02"

// event is the part of a calendar's answer this package reads.
type event struct {
	Summary   string   `json:"summary"`
	Location  string   `json:"location"`
	Start     when     `json:"start"`
	End       when     `json:"end"`
	Attendees []person `json:"attendees"`
}

// when is a calendar's two ways of saying a moment: a timestamp for an ordinary
// event, a bare date for one that takes the whole day.
type when struct {
	DateTime string `json:"dateTime"`
	Date     string `json:"date"`
}

type person struct {
	Email    string `json:"email"`
	Resource bool   `json:"resource"`
}

// CalendarList answers with the events on the person's main calendar between
// two moments, one line each: when, what, how many people, and where.
//
// from and to may each be a full timestamp or a bare YYYY-MM-DD date. A BARE
// DATE MEANS THE WHOLE OF THAT DAY, local time — from starts at its first
// moment and to ends at its last, so asking for the same date twice returns
// that day rather than nothing at all. An empty from means now; an empty to
// means a week after from.
func CalendarList(ctx context.Context, client *http.Client, from, to string) (string, error) {
	start, err := moment(from, false, time.Now())
	if err != nil {
		return "", fmt.Errorf("list events: %w", err)
	}
	finish, err := moment(to, true, start.Add(defaultWindow))
	if err != nil {
		return "", fmt.Errorf("list events: %w", err)
	}
	// The window is echoed back in the caller's own words where they used
	// any, so that asking for one bare date does not come back describing a
	// range that ends the following morning.
	opening, closing := echo(from, start), echo(to, finish)
	if finish.Before(start) {
		start, finish = finish, start
		opening, closing = closing, opening
	}

	parameters := url.Values{}
	parameters.Set("timeMin", start.Format(time.RFC3339))
	parameters.Set("timeMax", finish.Format(time.RFC3339))
	// Recurring events are expanded into the individual occurrences that
	// actually fall in the window, which is what "what is on my calendar"
	// means; the alternative is a rule the reader would have to evaluate.
	parameters.Set("singleEvents", "true")
	parameters.Set("orderBy", "startTime")
	parameters.Set("maxResults", strconv.Itoa(maxEvents))

	var listing struct {
		Items []event `json:"items"`
	}
	address := googleCalendarURL + "/calendars/primary/events?" + parameters.Encode()
	if err := getJSON(ctx, client, address, &listing); err != nil {
		return "", fmt.Errorf("list events: %w", err)
	}
	if len(listing.Items) == 0 {
		return fmt.Sprintf("No events between %s and %s.", opening, closing), nil
	}

	var builder strings.Builder
	noun := "events"
	if len(listing.Items) == 1 {
		noun = "event"
	}
	fmt.Fprintf(&builder, "%d %s between %s and %s.\n\n",
		len(listing.Items), noun, opening, closing)
	for _, item := range listing.Items {
		builder.WriteString(eventLine(item))
		builder.WriteString("\n")
	}
	return bound(strings.TrimRight(builder.String(), "\n")), nil
}

// eventLine renders one event. Fields the event does not carry are left out
// rather than printed empty, so a bare lunch entry is one short line and a
// meeting with a room and a guest list is a longer one.
func eventLine(e event) string {
	fields := []string{eventTime(e)}
	if title := collapse(e.Summary); title != "" {
		fields = append(fields, clip(title, 120))
	}
	if people := attendeeCount(e); people > 0 {
		noun := "people"
		if people == 1 {
			noun = "person"
		}
		fields = append(fields, fmt.Sprintf("%d %s", people, noun))
	}
	if place := collapse(e.Location); place != "" {
		fields = append(fields, clip(place, 80))
	}
	return strings.Join(fields, " | ")
}

// eventTime is the left column: a day and a span for an ordinary event, a day
// and "all day" for one that has no hours.
func eventTime(e event) string {
	if date := strings.TrimSpace(e.Start.Date); date != "" {
		return date + " all day"
	}
	start, err := time.Parse(time.RFC3339, strings.TrimSpace(e.Start.DateTime))
	if err != nil {
		return collapse(e.Start.DateTime)
	}
	start = start.Local()
	finish, err := time.Parse(time.RFC3339, strings.TrimSpace(e.End.DateTime))
	if err != nil {
		return start.Format("2006-01-02 15:04")
	}
	return start.Format("2006-01-02 15:04") + "-" + finish.Local().Format("15:04")
}

// attendeeCount counts the people invited, not the rooms and projectors that
// were booked alongside them.
func attendeeCount(e event) int {
	count := 0
	for _, guest := range e.Attendees {
		if !guest.Resource {
			count++
		}
	}
	return count
}

// moment reads one end of the window. endOfDay decides what a bare date means
// there: the first moment of that day at the start of a window, the first
// moment of the next day at the end of one, so that the named day is included
// whole.
func moment(raw string, endOfDay bool, fallback time.Time) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback, nil
	}
	if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
		return parsed, nil
	}
	if parsed, err := time.ParseInLocation(dateOnly, raw, time.Local); err == nil {
		if endOfDay {
			return parsed.AddDate(0, 0, 1), nil
		}
		return parsed, nil
	}
	return time.Time{}, fmt.Errorf("want a date like 2026-08-16 or a full timestamp, got %q", raw)
}

// echo says a window boundary back the way the caller wrote it, falling back to
// the resolved moment for a boundary the caller left to the defaults.
func echo(raw string, resolved time.Time) string {
	if trimmed := strings.TrimSpace(raw); trimmed != "" {
		return trimmed
	}
	return stamp(resolved)
}

// stamp renders a boundary nobody named: the day alone when it sits on
// midnight, the day and the time otherwise.
func stamp(t time.Time) string {
	local := t.Local()
	if local.Hour() == 0 && local.Minute() == 0 && local.Second() == 0 {
		return local.Format(dateOnly)
	}
	return local.Format("2006-01-02 15:04")
}
