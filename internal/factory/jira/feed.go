package jira

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/inbox"
)

// Source is the name every entry from Jira carries, and the name its
// [Replier] is registered under.
const Source = "jira"

// ProjectLabel prefixes the label every entry carries naming its Jira
// project, so a routing rule can send one project's tickets to one
// repository: a ticket ENG-142 carries jira-project:ENG.
const ProjectLabel = "jira-project:"

// CursorLayout is how a cursor is spelled: JQL's own minute format, which
// Jira reads in the connected account's time zone.
const CursorLayout = "2006-01-02 15:04"

// CursorOverlap is how far before the newest change the next read begins.
// JQL's dates stop at the minute, so a ticket changed later in that same
// minute is read again rather than missed; [inbox.Add] makes the repeat
// harmless.
const CursorOverlap = time.Minute

// UnknownZoneSlack is the widest gap between UTC and any zone's clock, the
// extra overlap a cursor carries when the account's zone is not known.
const UnknownZoneSlack = 14 * time.Hour

// MaxPages bounds one Fetch. Results come oldest change first, so a read cut
// at the bound resumes where it stopped on the next pass.
const MaxPages = 10

// priorities is THE ONE TABLE from Jira's priority names to the factory's.
// A name it does not hold (a site's own scheme) is no priority at all.
var priorities = map[string]string{
	"highest": "p0",
	"high":    "p1",
	"medium":  "p2",
	"low":     "p3",
	"lowest":  "",
}

// Priority is the factory priority for a Jira priority name, or "".
func Priority(name string) string {
	return priorities[strings.ToLower(strings.TrimSpace(name))]
}

// Feed reads the tickets a person's JQL finds, as [inbox.Entry] values. It is
// an [inbox.Feed]: [inbox.Poll] drives it and [inbox.Add] files what it
// returns.
type Feed struct {
	Client *Client
	// JQL is the person's query, as they wrote it.
	JQL string
	// Zone is the connected account's time zone. NIL IS "NOT KNOWN", not
	// UTC: the cursor is then written in UTC and begins [UnknownZoneSlack]
	// earlier, because a JQL date read in a zone behind UTC would otherwise
	// skip the hours between them.
	Zone *time.Location
	// Observe, when set, hears the outcome of every Fetch (nil for a good
	// read), so a connection can say when it last read and what went wrong.
	// It is told, never asked: it cannot change what Fetch returns.
	Observe func(err error)
}

var _ inbox.Feed = (*Feed)(nil)

// Name is [Source].
func (f *Feed) Name() string { return Source }

// Fetch runs the person's JQL, narrowed to tickets changed since cursor when
// there is one, oldest change first, and answers each as an entry, with the
// next cursor: the newest change seen less [CursorOverlap], never earlier
// than cursor itself.
func (f *Feed) Fetch(ctx context.Context, cursor string) ([]inbox.Entry, string, error) {
	entries, next, err := f.fetch(ctx, cursor)
	if f.Observe != nil {
		f.Observe(err)
	}
	return entries, next, err
}

func (f *Feed) fetch(ctx context.Context, cursor string) ([]inbox.Entry, string, error) {
	if f.Client == nil {
		return nil, cursor, errors.New("Jira is not connected")
	}
	jql, err := Query(f.JQL, cursor)
	if err != nil {
		return nil, cursor, err
	}
	var (
		entries []inbox.Entry
		newest  time.Time
		token   string
	)
	for range MaxPages {
		page, err := f.Client.Search(ctx, jql, token)
		if err != nil {
			// What was read before the failure is kept by nobody: the next
			// pass reads it again from the same cursor.
			return nil, cursor, err
		}
		for _, is := range page.Issues {
			if !ValidKey(is.Key) {
				continue
			}
			entries = append(entries, EntryOf(f.Client.Site, is))
			if is.Fields.Updated.After(newest) {
				newest = is.Fields.Updated.Time
			}
		}
		token = page.NextPageToken
		if page.IsLast || token == "" {
			break
		}
	}
	return entries, nextCursor(cursor, newest, f.Zone), nil
}

// nextCursor is newest less the overlap, in zone and JQL's minute format,
// and never earlier than the cursor it follows.
func nextCursor(cursor string, newest time.Time, zone *time.Location) string {
	if newest.IsZero() {
		return cursor
	}
	overlap := CursorOverlap
	if zone == nil {
		zone, overlap = time.UTC, CursorOverlap+UnknownZoneSlack
	}
	next := newest.Add(-overlap).In(zone)
	if cursor != "" {
		if was, err := time.ParseInLocation(CursorLayout, cursor, zone); err == nil && next.Before(was) {
			return cursor
		}
	}
	return next.Format(CursorLayout)
}

// reOrderBy finds a JQL's ORDER BY clause, which must stay last when the
// query is narrowed.
var reOrderBy = regexp.MustCompile(`(?i)\border\s+by\b`)

// Query is the JQL one read sends: the person's conditions in parentheses,
// `AND updated >= "<cursor>"` when there is a cursor, and ordered by change,
// oldest first. The person's own ORDER BY is dropped, because the cursor
// depends on that order. A query with no conditions is refused: Jira's search
// refuses an unbounded one too, and the factory must never read a whole site.
func Query(jql, cursor string) (string, error) {
	where := strings.TrimSpace(jql)
	if loc := reOrderBy.FindStringIndex(where); loc != nil {
		where = strings.TrimSpace(where[:loc[0]])
	}
	if where == "" {
		return "", errors.New("the JQL needs at least one condition, such as project = ENG")
	}
	q := "(" + where + ")"
	if cursor != "" {
		if _, err := time.Parse(CursorLayout, cursor); err != nil {
			return "", errors.New("the Jira read position is unreadable")
		}
		q += ` AND updated >= "` + cursor + `"`
	}
	return q + " ORDER BY updated ASC", nil
}

// EntryOf is one ticket as an entry: its key as the identity, its browse
// link, its summary and description, its reporter, its labels with its
// project's [ProjectLabel], and its priority through the one table.
func EntryOf(site string, is Issue) inbox.Entry {
	labels := make([]string, 0, len(is.Fields.Labels)+1)
	for _, l := range is.Fields.Labels {
		if l = strings.TrimSpace(l); l != "" {
			labels = append(labels, l)
		}
	}
	labels = append(labels, ProjectLabel+ProjectOf(is.Key))
	e := inbox.Entry{
		Source:     Source,
		ExternalID: is.Key,
		URL:        strings.TrimRight(site, "/") + "/browse/" + is.Key,
		Title:      strings.TrimSpace(is.Fields.Summary),
		Body:       PlainText(is.Fields.Description),
		Author:     author(is.Fields.Reporter),
		Labels:     labels,
	}
	if is.Fields.Priority != nil {
		e.Priority = Priority(is.Fields.Priority.Name)
	}
	if e.Title == "" {
		e.Title = is.Key
	}
	return e
}

// ProjectOf is the project key of a ticket key: ENG of ENG-142.
func ProjectOf(key string) string {
	if i := strings.LastIndex(key, "-"); i > 0 {
		return key[:i]
	}
	return key
}

// author is how a reporter is named: "Ada Lovelace <ada@example.com>", or
// whichever half Jira shared (an account's email is often private).
func author(u *User) string {
	if u == nil {
		return ""
	}
	name, mail := strings.TrimSpace(u.DisplayName), strings.TrimSpace(u.EmailAddress)
	switch {
	case name != "" && mail != "":
		return name + " <" + mail + ">"
	case name != "":
		return name
	}
	return mail
}
