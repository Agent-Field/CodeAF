package data

import (
	"fmt"
	"net/url"
	"time"
)

type MeetingStarting struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// MeetingsStarting only presents a prompt for a scheduled conference. The
// desk owns the separate, affirmative action that opens audio devices.
func (s *Store) MeetingsStarting(now time.Time) ([]MeetingStarting, error) {
	result := []MeetingStarting{}
	t := s.schema.Load().common[CommonEvent]
	if t == nil || t.field("conference") == nil || t.field("starts_at") == nil || t.field("ends_at") == nil {
		return result, nil
	}
	if mode, err := s.Residency(Scope{}, t.Name); err != nil || mode.Mode == "live" {
		return result, err
	}
	window := now.Add(-2 * time.Minute).UTC().Format(time.RFC3339)
	page, err := s.Query(Scope{}, t.Name, fmt.Sprintf("starts_at>=%s starts_at<=%s ends_at>%s has:conference sort:starts_at",
		window, now.UTC().Format(time.RFC3339), now.UTC().Format(time.RFC3339)), "", 12)
	if err != nil {
		return nil, err
	}
	for _, row := range page.Rows {
		conference, err := url.Parse(asText(row["conference"]))
		if err != nil || conference.Scheme != "https" || conference.Hostname() == "" {
			continue
		}
		title := asText(row["title"])
		if title == "" || title == masked {
			title = "Calendar meeting"
		}
		result = append(result, MeetingStarting{ID: row.ID(), Title: title})
	}
	return result, nil
}

// MeetingAttendees resolves the calendar links through the person's visible
// Person rows. Unnamed or masked attendees remain unassigned in model output.
func (s *Store) MeetingAttendees(eventID string) (map[string]string, error) {
	out := map[string]string{}
	sch := s.schema.Load()
	event, person := sch.common[CommonEvent], sch.common[CommonPerson]
	if eventID == "" || event == nil || person == nil {
		return out, nil
	}
	row, err := s.Get(Scope{}, event.Name, eventID, false)
	if err != nil {
		return nil, err
	}
	for _, id := range asStrings(row["attendees"]) {
		attendee, err := s.Get(Scope{}, person.Name, id, false)
		if err != nil {
			continue
		}
		if name := asText(attendee["name"]); name != "" && name != masked {
			out[id] = name
		}
	}
	return out, nil
}
