package connections

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"time"
)

const googlePollInterval = 30 * time.Second

// GoogleCheckpoint is stored by the run clock. Page cursors and grants are
// opaque. No source bodies or rows are retained by the run clock.
type GoogleCheckpoint struct {
	Grant     string               `json:"grant,omitempty"`
	Cursor    string               `json:"cursor,omitempty"`
	Page      string               `json:"page,omitempty"`
	Calendar  map[string]time.Time `json:"calendar,omitempty"`
	StartedAt time.Time            `json:"started_at,omitzero"`
	CheckedAt time.Time            `json:"checked_at,omitzero"`
}

type GoogleEvent struct {
	Key, Record, Type string
}

type GoogleTrigger struct {
	App, Connection, Read, Source, Event, Query string
	LeadMinutes                                 int
}

type googleChange struct {
	id, version string
}

// PollGoogle resolves the installed binding before polling the delegated feed.
// It returns only record references so runs cannot transport source PII as
// trigger payloads to an outside harness.
func (s Service) PollGoogle(ctx context.Context, trigger GoogleTrigger, checkpoint GoogleCheckpoint, now time.Time) (GoogleCheckpoint, []GoogleEvent, error) {
	now = now.UTC()
	c, err := s.scoped(trigger.App, trigger.Connection)
	if err != nil {
		return checkpoint, nil, err
	}
	read, ok := c.Reads[trigger.Read]
	if c.Kind != Google || !ok || googleService(read.Table) != trigger.Source ||
		trigger.Event == "transcript.created" && read.Table != "transcripts" ||
		trigger.Event == "message.received" && read.Table != "gmail" {
		return checkpoint, nil, ErrRejected
	}
	if !checkpoint.CheckedAt.IsZero() && now.Before(checkpoint.CheckedAt.Add(googlePollInterval)) && checkpoint.Page == "" {
		return checkpoint, nil, nil
	}
	store, binding, typ, err := s.bind(c.Config, read, false)
	if err != nil {
		return checkpoint, nil, err
	}
	if typ.HasRowPolicy() {
		return checkpoint, nil, ErrSourcePermission
	}
	ctx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()
	source, grant, err := s.workspace(ctx, c, read.Table, false)
	if err != nil {
		return checkpoint, nil, err
	}
	c.googleGrant, c.googleSubject = grant, source.subject
	grantKey := fmt.Sprintf("%x", sha256.Sum256([]byte(c.Binding+"/"+grant+"/"+trigger.Query+"/"+strconv.Itoa(trigger.LeadMinutes))))
	if checkpoint.Grant != grantKey {
		checkpoint = GoogleCheckpoint{Grant: grantKey, StartedAt: now}
	}
	next, changes, err := source.poll(ctx, trigger, checkpoint, now)
	if errors.Is(err, ErrCursorExpired) {
		// Expired authority requires a fresh baseline. Existing remote rows
		// must not be mistaken for new records after that reset.
		since := now
		if trigger.Source == "calendar" && !checkpoint.CheckedAt.IsZero() {
			since = checkpoint.CheckedAt
		}
		next = GoogleCheckpoint{Grant: grantKey, StartedAt: since}
		next, changes, err = source.poll(ctx, trigger, next, now)
	}
	if err != nil {
		return checkpoint, nil, err
	}
	if _, current, err := s.workspace(ctx, c, read.Table, false); err != nil || current != grant {
		return checkpoint, nil, ErrSourcePermission
	}
	current, err := s.scoped(c.App, c.ID)
	if err != nil || current.Binding != c.Binding {
		return checkpoint, nil, ErrConflict
	}
	if _, current, currentType, err := s.bind(c.Config, read, false); err != nil || current != binding || currentType != typ {
		return checkpoint, nil, ErrSourcePermission
	}
	events := make([]GoogleEvent, 0, len(changes))
	for _, change := range changes {
		key, err := encodeSourceKey(change.id)
		if err != nil {
			return checkpoint, nil, err
		}
		id, err := store.LinkIdentity(binding.Common, externalKey(c, read, key), "")
		if err != nil {
			return checkpoint, nil, err
		}
		hash := sha256.Sum256([]byte(grantKey + "/" + id + "/" + change.version))
		events = append(events, GoogleEvent{Key: fmt.Sprintf("%x", hash), Record: id, Type: read.Type})
	}
	return next, events, nil
}

func (g googleSource) poll(ctx context.Context, t GoogleTrigger, c GoogleCheckpoint, now time.Time) (GoogleCheckpoint, []googleChange, error) {
	c.CheckedAt = now
	switch t.Source + "." + t.Event {
	case "gmail.message.received":
		return g.gmailChanges(ctx, c, t.Query)
	case "calendar.event.starts":
		return g.calendarStarts(ctx, c, now, t.LeadMinutes)
	case "drive.transcript.created":
		return g.driveChanges(ctx, c)
	default:
		return c, nil, ErrRejected
	}
}

func (g googleSource) gmailChanges(ctx context.Context, c GoogleCheckpoint, query string) (GoogleCheckpoint, []googleChange, error) {
	if c.Cursor == "" {
		var profile struct{ HistoryID string }
		if _, err := g.get(ctx, gmailPath+"/profile", nil, &profile); err != nil {
			return c, nil, err
		}
		if profile.HistoryID == "" {
			return c, nil, ErrInvalidRows
		}
		c.Cursor = profile.HistoryID
		return c, nil, nil
	}
	var page struct {
		HistoryID, NextPageToken string
		History                  []struct {
			MessagesAdded []struct{ Message struct{ ID string } }
		}
	}
	q := url.Values{"startHistoryId": {c.Cursor}, "maxResults": {"50"}, "historyTypes": {"messageAdded"}}
	if c.Page != "" {
		q.Set("pageToken", c.Page)
	}
	status, err := g.get(ctx, gmailPath+"/history", q, &page)
	if status == http.StatusNotFound {
		return c, nil, ErrCursorExpired
	}
	if err != nil {
		return c, nil, err
	}
	pending := map[string]bool{}
	for _, event := range page.History {
		for _, added := range event.MessagesAdded {
			pending[added.Message.ID] = true
			if len(pending) > maxRows {
				return c, nil, ErrTooLarge
			}
		}
	}
	var matched []googleChange
	if len(pending) > 0 {
		// Ask Gmail to interpret its own query. Only ids also present in the
		// new-message history can start a run, even for a broad query.
		var after string
		for range 5 {
			ids, next, err := g.gmailIDs(ctx, query, after, maxRows)
			if err != nil {
				return c, nil, err
			}
			for _, id := range ids {
				if pending[id] {
					matched = append(matched, googleChange{id: id})
					delete(pending, id)
				}
			}
			after = next
			if after == "" || len(pending) == 0 {
				break
			}
		}
		if after != "" && len(pending) > 0 {
			return c, nil, ErrTooLarge
		}
	}
	c.Page = page.NextPageToken
	if c.Page == "" {
		if page.HistoryID == "" {
			return c, nil, ErrInvalidRows
		}
		c.Cursor = page.HistoryID
	}
	return c, matched, nil
}

const maxTrackedCalendarEvents = 5000

// calendarStarts applies a page of events and keeps only their start times.
// A change or cancellation replaces prior timing before any due event fires.
func (g googleSource) calendarStarts(ctx context.Context, c GoogleCheckpoint, now time.Time, leadMinutes int) (GoogleCheckpoint, []googleChange, error) {
	if leadMinutes < 1 || leadMinutes > 1440 {
		return c, nil, ErrRejected
	}
	if c.StartedAt.IsZero() {
		c.StartedAt = now
	}
	page, err := g.calendarChanges(ctx, c.Cursor, c.Page, c.StartedAt)
	if err != nil {
		return c, nil, err
	}
	tracked := maps.Clone(c.Calendar)
	if tracked == nil {
		tracked = make(map[string]time.Time)
	}
	for _, event := range page.Items {
		at, err := time.Parse(time.RFC3339, event.Start.DateTime)
		if err != nil || event.Status == "cancelled" || at.Before(c.StartedAt) {
			delete(tracked, event.ID)
		} else {
			tracked[event.ID] = at
		}
	}
	if len(tracked) > maxTrackedCalendarEvents {
		return c, nil, ErrTooLarge
	}
	c.Calendar, c.Page = tracked, page.NextPageToken
	if c.Page != "" {
		// A later page can cancel an event in this batch. Wait for the
		// final page before evaluating which events are due.
		return c, nil, nil
	}
	if page.NextSyncToken == "" {
		return c, nil, ErrInvalidRows
	}
	c.Cursor = page.NextSyncToken
	due := now.Add(time.Duration(leadMinutes) * time.Minute)
	var changes []googleChange
	for _, id := range slices.Sorted(maps.Keys(tracked)) {
		if at := tracked[id]; !at.After(due) {
			changes = append(changes, googleChange{id: id, version: at.UTC().Format(time.RFC3339Nano)})
			delete(tracked, id)
		}
	}
	return c, changes, nil
}

// calendarChanges keeps the initial query frozen until its last page;
// Google rejects timeMin on incremental requests.
func (g googleSource) calendarChanges(ctx context.Context, syncToken, pageToken string, since time.Time) (googleCalendarPage, error) {
	q := url.Values{"singleEvents": {"true"}, "showDeleted": {"true"}, "maxResults": {"100"}}
	if syncToken != "" {
		q.Set("syncToken", syncToken)
	} else {
		q.Set("timeMin", since.UTC().Format(time.RFC3339))
	}
	if pageToken != "" {
		q.Set("pageToken", pageToken)
	}
	return g.calendar(ctx, q)
}

func (g googleSource) driveChanges(ctx context.Context, c GoogleCheckpoint) (GoogleCheckpoint, []googleChange, error) {
	const changesPath = "/drive/v3/changes"
	if c.Cursor == "" {
		var start struct{ StartPageToken string }
		if _, err := g.get(ctx, changesPath+"/startPageToken", url.Values{"supportsAllDrives": {"true"}}, &start); err != nil {
			return c, nil, err
		}
		if start.StartPageToken == "" {
			return c, nil, ErrInvalidRows
		}
		c.Cursor = start.StartPageToken
		return c, nil, nil
	}
	query := url.Values{"pageToken": {c.Cursor}, "pageSize": {"100"}, "includeRemoved": {"true"},
		"fields":                    {"nextPageToken,newStartPageToken,changes(fileId,removed,file(id,name,mimeType,trashed,createdTime))"},
		"includeItemsFromAllDrives": {"true"}, "supportsAllDrives": {"true"}}
	if c.Page != "" {
		query.Set("pageToken", c.Page)
	}
	var page struct {
		NextPageToken, NewStartPageToken string
		Changes                          []struct {
			FileID  string
			Removed bool
			File    googleFile
		}
	}
	status, err := g.get(ctx, changesPath, query, &page)
	if status == http.StatusGone {
		return c, nil, ErrCursorExpired
	}
	if err != nil {
		return c, nil, err
	}
	var matched []googleChange
	for _, ch := range page.Changes {
		created, err := time.Parse(time.RFC3339, ch.File.CreatedTime)
		if !ch.Removed && ch.FileID == ch.File.ID && !ch.File.Trashed && ch.File.transcript() && err == nil && !created.Before(c.StartedAt) {
			// The current file read also rechecks access after the feed page.
			rows, _, err := g.point(ctx, "transcripts", ch.FileID)
			if errors.Is(err, ErrSourcePermission) {
				continue
			}
			if err != nil {
				return c, nil, err
			}
			if len(rows) == 1 {
				matched = append(matched, googleChange{id: ch.FileID})
			}
		}
	}
	c.Page = page.NextPageToken
	if c.Page == "" {
		if page.NewStartPageToken == "" {
			return c, nil, ErrInvalidRows
		}
		c.Cursor = page.NewStartPageToken
	}
	return c, matched, nil
}
