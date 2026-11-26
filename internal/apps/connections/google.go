package connections

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/mail"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/connect"
	"golang.org/x/sync/errgroup"
)

const (
	gmailPath    = "/gmail/v1/users/me"
	calendarPath = "/calendar/v3/calendars/primary/events"
	drivePath    = "/drive/v3/files"
	driveFields  = "id,name,mimeType,version,modifiedTime,webViewLink,description,shared,trashed,capabilities(canDownload,canComment),permissions(type,role,emailAddress,domain)"
)

var (
	googleID         = regexp.MustCompile(`^[A-Za-z0-9_@.-]{1,512}$`)
	ErrCursorExpired = errors.New("Google's sync cursor expired; refresh the source")
)

type googleSource struct {
	client  *http.Client
	origin  string
	subject string
}

func (g googleSource) request(ctx context.Context, method, path string, query url.Values, body []byte, version string) ([]byte, int, error) {
	origin := g.origin
	if origin == "" {
		origin = "https://www.googleapis.com"
		if strings.HasPrefix(path, gmailPath) {
			origin = "https://gmail.googleapis.com"
		}
	}
	endpoint := origin + path
	if len(query) != 0 {
		endpoint += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, 0, ErrRejected
	}
	client := *g.client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	write := method != http.MethodGet
	if write {
		req.Header.Set("Content-Type", "application/json")
		req.GetBody = nil
		if version != "" {
			req.Header.Set("If-Match", version)
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		if write {
			return nil, 0, ErrUncertain
		}
		return nil, 0, ErrUnavailable
	}
	defer resp.Body.Close()
	status := resp.StatusCode
	if status < 200 || status >= 300 || status == http.StatusAccepted {
		switch {
		case status == http.StatusPreconditionFailed || status == http.StatusConflict:
			return nil, status, ErrConflict
		case status == http.StatusUnauthorized || status == http.StatusForbidden:
			return nil, status, ErrSourcePermission
		case write && status >= 400 && status < 500 && status != http.StatusRequestTimeout:
			return nil, status, ErrRejected
		case write:
			return nil, status, ErrUncertain
		default:
			return nil, status, ErrUnavailable
		}
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if write && (err != nil || len(raw) > maxBytes) {
		return nil, status, ErrUncertain
	}
	if err != nil {
		return nil, status, ErrUnavailable
	}
	if len(raw) > maxBytes {
		return nil, status, ErrTooLarge
	}
	return raw, status, nil
}

func (g googleSource) get(ctx context.Context, path string, query url.Values, out any) (int, error) {
	raw, status, err := g.request(ctx, http.MethodGet, path, query, nil, "")
	if err != nil {
		return status, err
	}
	return status, decodeJSON(raw, out)
}

func (s Service) workspace(ctx context.Context, c registration, object string, write bool) (googleSource, string, error) {
	if s.Store == nil {
		return googleSource{}, "", ErrPersonCredential
	}
	store, err := s.Store()
	if err != nil {
		return googleSource{}, "", err
	}
	person := s.person(ctx).ID
	if person == "" || person != store.Person() {
		return googleSource{}, "", ErrSourcePermission
	}
	if c.GoogleFixture {
		return googleSource{client: googleFixtureClient(), subject: "fixture"}, "fixture", nil
	}
	if s.Google == nil {
		return googleSource{}, "", ErrPersonCredential
	}
	manager, err := s.Google()
	if err != nil {
		return googleSource{}, "", ErrPersonCredential
	}
	client, grant, subject, err := manager.WorkspaceClient(ctx, googleService(object), write)
	if err != nil {
		return googleSource{}, "", fmt.Errorf("%w: %s", ErrPersonCredential, err)
	}
	return googleSource{client: client, subject: subject}, grant, nil
}

func googleService(object string) string {
	switch object {
	case "gmail", "attachments":
		return "gmail"
	case "calendar":
		return "calendar"
	default:
		return "drive"
	}
}

// rows holds only what the delegated Google account could read in this request.
func (g googleSource) rows(ctx context.Context, object string, args map[string]any, limit int) ([]map[string]any, bool, error) {
	if value, ok := args["id"]; ok {
		id, ok := value.(string)
		if !ok || !googleID.MatchString(id) || id == "." || id == ".." {
			return nil, false, ErrRejected
		}
		return g.point(ctx, object, id)
	}
	switch object {
	case "gmail":
		query, _ := args["q"].(string)
		ids, next, err := g.gmailIDs(ctx, query, "", limit)
		if err != nil {
			return nil, false, err
		}
		rows, err := g.messages(ctx, ids)
		return rows, next != "", err
	case "attachments":
		ids, next, err := g.gmailIDs(ctx, "has:attachment", "", limit)
		if err != nil {
			return nil, false, err
		}
		messages, err := g.messages(ctx, ids)
		if err != nil {
			return nil, false, err
		}
		var docs []map[string]any
		for _, message := range messages {
			items, err := googleAttachments(message)
			if err != nil {
				return nil, false, err
			}
			docs = append(docs, items...)
			if len(docs) > limit {
				return docs[:limit], true, nil
			}
		}
		return docs, next != "", nil
	case "calendar":
		now := time.Now().UTC()
		page, err := g.calendar(ctx, url.Values{"maxResults": {strconv.Itoa(limit)}, "singleEvents": {"true"},
			"timeMin": {now.Format(time.RFC3339)}, "timeMax": {now.AddDate(0, 0, 7).Format(time.RFC3339)}, "orderBy": {"startTime"}})
		return page.events(), page.NextPageToken != "", err
	case "drive", "transcripts":
		q := "trashed = false"
		if object == "transcripts" {
			q += " and mimeType = 'application/vnd.google-apps.document' and name contains 'Transcript'"
		}
		return g.driveRows(ctx, object, q, limit)
	}
	return nil, false, ErrRejected
}

func (g googleSource) driveRows(ctx context.Context, object, search string, limit int) ([]map[string]any, bool, error) {
	var rows []map[string]any
	var token string
	for range 10 {
		var page struct {
			Files      []googleFile `json:"files"`
			Next       string       `json:"nextPageToken"`
			Incomplete bool         `json:"incompleteSearch"`
		}
		query := url.Values{
			"pageSize": {strconv.Itoa(min(limit-len(rows), 100))}, "q": {search},
			"fields":                    {"nextPageToken,incompleteSearch,files(" + driveFields + ")"},
			"includeItemsFromAllDrives": {"true"}, "supportsAllDrives": {"true"},
		}
		if token != "" {
			query.Set("pageToken", token)
		}
		_, err := g.get(ctx, drivePath, query, &page)
		if err != nil {
			return nil, false, err
		}
		if page.Incomplete {
			return nil, false, ErrUnavailable
		}
		for _, file := range page.Files {
			if !file.Trashed && (object != "transcripts" || file.transcript()) {
				rows = append(rows, file.row())
			}
			if len(rows) > limit {
				return rows[:limit], true, nil
			}
		}
		token = page.Next
		if len(rows) == limit || token == "" {
			return rows, token != "", nil
		}
	}
	return nil, false, ErrTooLarge
}

func (g googleSource) point(ctx context.Context, object, id string) ([]map[string]any, bool, error) {
	var status int
	var err error
	var row map[string]any
	switch object {
	case "attachments":
		parent, _, found := strings.Cut(id, ".")
		if !found || !googleID.MatchString(parent) {
			return nil, false, ErrRejected
		}
		messages, _, err := g.point(ctx, "gmail", parent)
		if err != nil || len(messages) == 0 {
			return nil, false, err
		}
		items, err := googleAttachments(messages[0])
		if err != nil {
			return nil, false, err
		}
		for _, item := range items {
			if item["id"] == id {
				return []map[string]any{item}, false, nil
			}
		}
		return nil, false, nil
	case "gmail":
		var msg googleMessage
		status, err = g.get(ctx, gmailPath+"/messages/"+id, gmailMetadataQuery(), &msg)
		row = msg.row()
	case "calendar":
		var event googleEvent
		status, err = g.get(ctx, calendarPath+"/"+id, nil, &event)
		if event.Status != "cancelled" {
			row = event.row()
		}
	case "drive", "transcripts":
		var file googleFile
		status, err = g.get(ctx, drivePath+"/"+id, url.Values{"fields": {driveFields}, "supportsAllDrives": {"true"}}, &file)
		if !file.Trashed && (object != "transcripts" || file.transcript()) {
			row = file.row()
		}
	}
	if status == http.StatusNotFound {
		return nil, false, nil
	}
	if err != nil || row == nil {
		return nil, false, err
	}
	if row["id"] != id {
		return nil, false, ErrInvalidRows
	}
	return []map[string]any{row}, false, nil
}

func (g googleSource) gmailIDs(ctx context.Context, query, token string, limit int) ([]string, string, error) {
	var page struct {
		Messages []struct{ ID string } `json:"messages"`
		Next     string                `json:"nextPageToken"`
	}
	params := url.Values{"maxResults": {strconv.Itoa(limit)}}
	if query != "" {
		params.Set("q", query)
	}
	if token != "" {
		params.Set("pageToken", token)
	}
	_, err := g.get(ctx, gmailPath+"/messages", params, &page)
	var ids []string
	for _, msg := range page.Messages {
		ids = append(ids, msg.ID)
	}
	return ids, page.Next, err
}

func (g googleSource) messages(ctx context.Context, ids []string) ([]map[string]any, error) {
	rows := make([]map[string]any, len(ids))
	group, ctx := errgroup.WithContext(ctx)
	group.SetLimit(5)
	var used atomic.Int64
	for i, id := range ids {
		group.Go(func() error {
			if !googleID.MatchString(id) || id == "." || id == ".." {
				return ErrInvalidRows
			}
			page, _, err := g.point(ctx, "gmail", id)
			if len(page) != 0 {
				size, _ := json.Marshal(page[0])
				if used.Add(int64(len(size))) > maxBytes {
					return ErrTooLarge
				}
				rows[i] = page[0]
			}
			return err
		})
	}
	if err := group.Wait(); err != nil {
		return nil, err
	}
	out := rows[:0]
	for _, row := range rows {
		if row != nil {
			out = append(out, row)
		}
	}
	return out, nil
}

type googleMessage struct {
	ID, ThreadID, HistoryID, InternalDate, Snippet string
	LabelIDs                                       []string
	Payload                                        googleMessagePart
}

type googleMessagePart struct {
	PartID, Filename, MimeType string
	Headers                    []struct{ Name, Value string }
	Body                       struct {
		AttachmentID string
		Size         int
	}
	Parts []googleMessagePart
}

func gmailMetadataQuery() url.Values {
	part := "partId,filename,mimeType,body(attachmentId,size)"
	nested := part
	for range 6 {
		nested = part + ",parts(" + nested + ")"
	}
	return url.Values{"format": {"full"}, "fields": {
		"id,threadId,historyId,internalDate,labelIds,snippet,payload(headers(name,value)," + nested + ")",
	}}
}

func (p googleMessagePart) attachments(out *[]map[string]any) {
	if p.Filename != "" {
		*out = append(*out, map[string]any{"id": p.Body.AttachmentID, "part": p.PartID,
			"name": p.Filename, "mime_type": p.MimeType, "size": p.Body.Size})
	}
	for _, child := range p.Parts {
		child.attachments(out)
	}
}

func googleAttachments(message map[string]any) ([]map[string]any, error) {
	var parts []struct {
		Part     string `json:"part"`
		Name     string `json:"name"`
		MimeType string `json:"mime_type"`
		Size     int    `json:"size"`
	}
	if json.Unmarshal([]byte(googleText(message, "attachments")), &parts) != nil {
		return nil, ErrInvalidRows
	}
	items := make([]map[string]any, 0, len(parts))
	for _, part := range parts {
		if part.Part != "" {
			id := googleText(message, "id") + "." + part.Part
			items = append(items, map[string]any{"id": id, "version": message["version"],
				"title": part.Name, "mime_type": part.MimeType, "size": part.Size})
		}
	}
	return items, nil
}

func (m googleMessage) row() map[string]any {
	headers := map[string]string{}
	for _, h := range m.Payload.Headers {
		headers[strings.ToLower(h.Name)] = h.Value
	}
	subject, err := new(mime.WordDecoder).DecodeHeader(headers["subject"])
	if err != nil {
		subject = headers["subject"]
	}
	var date any
	if ms, err := strconv.ParseInt(m.InternalDate, 10, 64); err == nil {
		date = time.UnixMilli(ms).UTC().Format(time.RFC3339Nano)
	}
	var attachments []map[string]any
	m.Payload.attachments(&attachments)
	return map[string]any{"id": m.ID, "thread": m.ThreadID, "version": m.HistoryID, "date": date,
		"subject": subject, "snippet": m.Snippet, "from": emailList(headers["from"]),
		"to": emailList(headers["to"]), "cc": emailList(headers["cc"]), "labels": strings.Join(m.LabelIDs, ", "),
		"attachments": jsonText(attachments), "sender_domain": emailDomain(emailList(headers["from"]))}
}

func emailList(header string) string {
	addresses, _ := mail.ParseAddressList(header)
	var emails []string
	for _, address := range addresses {
		if strings.Contains(address.Address, "@") {
			emails = append(emails, strings.ToLower(address.Address))
		}
	}
	return strings.Join(emails, ", ")
}

func jsonText(value any) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

type googleEvent struct {
	ID, Etag, Status, Summary, Description, Location, HangoutLink string
	Start, End                                                    struct{ Date, DateTime, TimeZone string }
	Attendees                                                     []struct{ Email string }
	ConferenceData                                                struct {
		EntryPoints []struct{ EntryPointType, URI string }
	}
}

func (e googleEvent) row() map[string]any {
	attendees := make([]string, 0, len(e.Attendees))
	for _, attendee := range e.Attendees {
		if email := emailList(attendee.Email); email != "" {
			attendees = append(attendees, email)
		}
	}
	conference := e.HangoutLink
	for _, entry := range e.ConferenceData.EntryPoints {
		if conference == "" && entry.EntryPointType == "video" {
			conference = entry.URI
		}
	}
	return map[string]any{"id": e.ID, "version": e.Etag, "title": e.Summary, "description": e.Description,
		"location": e.Location, "conference": conference, "start": eventInstant(e.Start), "end": eventInstant(e.End),
		"start_date": e.Start.Date, "end_date": e.End.Date, "attendees": strings.Join(attendees, ", ")}
}

func eventInstant(span struct{ Date, DateTime, TimeZone string }) any {
	if span.DateTime != "" {
		return span.DateTime
	}
	// The all-day civil date remains in a separate field without inventing an instant.
	return nil
}

type googleCalendarPage struct {
	Items                        []googleEvent
	NextPageToken, NextSyncToken string
}

func (g googleSource) calendar(ctx context.Context, q url.Values) (googleCalendarPage, error) {
	var page googleCalendarPage
	status, err := g.get(ctx, calendarPath, q, &page)
	if status == http.StatusGone {
		return page, ErrCursorExpired
	}
	return page, err
}

func (p googleCalendarPage) events() []map[string]any {
	var rows []map[string]any
	for _, event := range p.Items {
		if event.Status != "cancelled" {
			rows = append(rows, event.row())
		}
	}
	return rows
}

type googleFile struct {
	ID, Name, MimeType, Version, CreatedTime, ModifiedTime, WebViewLink, Description string
	Shared, Trashed                                                                  bool
	Capabilities                                                                     struct{ CanDownload, CanComment bool }
	Permissions                                                                      []struct {
		Type         string `json:"type"`
		Role         string `json:"role"`
		EmailAddress string `json:"emailAddress,omitempty"`
		Domain       string `json:"domain,omitempty"`
	}
}

func (f googleFile) transcript() bool {
	return f.MimeType == "application/vnd.google-apps.document" && strings.Contains(strings.ToLower(f.Name), "transcript")
}

func (f googleFile) row() map[string]any {
	return map[string]any{"id": f.ID, "version": f.Version, "title": f.Name, "mime_type": f.MimeType,
		"modified": f.ModifiedTime, "link": f.WebViewLink, "description": f.Description,
		"shared": f.Shared, "sharing": jsonText(f.Permissions), "transcript": f.transcript()}
}

func (g googleSource) content(ctx context.Context, object, id string) (string, error) {
	if object == "gmail" {
		data, _, err := g.request(ctx, http.MethodGet, gmailPath+"/messages/"+id, url.Values{"format": {"full"}}, nil, "")
		if err != nil {
			return "", err
		}
		return connect.GmailBodyText(data)
	}
	var file googleFile
	_, err := g.get(ctx, drivePath+"/"+id, url.Values{"fields": {driveFields}, "supportsAllDrives": {"true"}}, &file)
	if err != nil {
		return "", err
	}
	if file.ID != id || file.Trashed || !file.Capabilities.CanDownload ||
		file.MimeType != "application/vnd.google-apps.document" || object == "transcripts" && !file.transcript() {
		return "", ErrSourcePermission
	}
	raw, _, err := g.request(ctx, http.MethodGet, drivePath+"/"+id+"/export", url.Values{"mimeType": {"text/plain"}}, nil, "")
	if err != nil {
		return "", err
	}
	if !utf8.Valid(raw) {
		return "", ErrInvalidRows
	}
	return string(raw), nil
}

// SetGoogleFixture switches only the operator binding. Retiring its generation
// invalidates record IDs, sync cursors and any already approved write.
func (s Service) SetGoogleFixture(app, id string, fixture bool) error {
	vault := s.vault()
	definitions, err := vault.Definitions()
	if err != nil {
		return err
	}
	var c registration
	previous := definitions[id]
	if decodeJSON(previous, &c) != nil || c.App != app || c.ID != id || c.Kind != Google {
		return ErrRejected
	}
	if c.GoogleFixture == fixture {
		return nil
	}
	c.GoogleFixture = fixture
	c.Binding, err = cell.NewID(time.Now())
	if err != nil {
		return err
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return vault.ChangeDefinition(id, previous, raw)
}
