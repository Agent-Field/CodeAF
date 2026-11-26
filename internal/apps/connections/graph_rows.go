package connections

import (
	"context"
	"encoding/json"
	"fmt"
	"net/mail"
	"net/url"
	"strings"
	"time"
)

type graphRecipient struct {
	EmailAddress struct{ Name, Address string }
}

type graphBody struct{ ContentType, Content string }

type graphMessage struct {
	graphEntry
	ChangeKey, ConversationID, ReceivedDateTime, Subject, WebLink string
	From                                                          graphRecipient
	ToRecipients, CcRecipients                                    []graphRecipient
	Categories                                                    []string
	HasAttachments, IsDraft                                       bool
	Body                                                          graphBody
}

func graphRecipients(all []graphRecipient) string {
	addresses := make([]string, 0, len(all))
	for _, r := range all {
		if email := normalizedEmail(r.EmailAddress.Address); email != "" {
			addresses = append(addresses, email)
		}
	}
	return strings.Join(addresses, ", ")
}

func normalizedEmail(value string) string {
	email, err := mail.ParseAddress(strings.TrimSpace(value))
	if err != nil || len(email.Address) > 320 {
		return ""
	}
	return strings.ToLower(email.Address)
}

func (m graphMessage) row() map[string]any {
	return map[string]any{"id": m.ID, "version": graphVersion(m.ETag, m.ChangeKey), "thread": m.ConversationID,
		"date": m.ReceivedDateTime, "subject": m.Subject, "from": normalizedEmail(m.From.EmailAddress.Address),
		"to": graphRecipients(m.ToRecipients), "cc": graphRecipients(m.CcRecipients),
		"labels": strings.Join(m.Categories, ", "), "has_attachments": m.HasAttachments,
		"is_draft": m.IsDraft, "link": m.WebLink, "snippet": nil, "attachments": nil, "body": nil}
}

type graphDate struct{ DateTime, TimeZone string }

type graphEvent struct {
	graphEntry
	ChangeKey, Subject, WebLink string
	IsAllDay, IsCancelled       bool
	Body                        graphBody
	Start, End                  graphDate
	Attendees                   []graphRecipient
	Organizer                   graphRecipient
	Location                    struct{ DisplayName string }
	OnlineMeeting               struct{ JoinURL string }
}

func graphVersion(etag, fallback string) string {
	if etag != "" {
		return etag
	}
	return fallback
}

func graphInstant(d graphDate) any {
	if moment, err := time.Parse(time.RFC3339Nano, d.DateTime); err == nil {
		return moment.UTC().Format(time.RFC3339Nano)
	}
	if d.TimeZone == "" || !strings.EqualFold(d.TimeZone, "UTC") {
		return nil
	}
	if moment, err := time.Parse("2006-01-02T15:04:05", d.DateTime); err == nil {
		return moment.Format(time.RFC3339Nano)
	}
	return nil
}

func (e graphEvent) row() map[string]any {
	start, end := graphInstant(e.Start), graphInstant(e.End)
	var startDate, endDate any
	if e.IsAllDay {
		start, end = nil, nil
		if len(e.Start.DateTime) >= 10 && len(e.End.DateTime) >= 10 {
			startDate, endDate = e.Start.DateTime[:10], e.End.DateTime[:10]
		}
	}
	return map[string]any{"id": e.ID, "version": graphVersion(e.ETag, e.ChangeKey), "title": e.Subject,
		"description": nil, "location": e.Location.DisplayName, "conference": e.OnlineMeeting.JoinURL,
		"link": e.WebLink, "start": start, "end": end, "start_date": startDate, "end_date": endDate,
		"attendees": graphRecipients(e.Attendees), "organizer": normalizedEmail(e.Organizer.EmailAddress.Address)}
}

type graphDriveItem struct {
	graphEntry
	DriveETag                                       string `json:"eTag"`
	Name, LastModifiedDateTime, WebURL, Description string
	File                                            *struct{ MimeType string }
	Deleted, Shared                                 map[string]any
}

func (d graphDriveItem) row() map[string]any {
	if d.File == nil {
		return nil
	}
	return map[string]any{"id": d.ID, "version": graphVersion(d.DriveETag, d.ETag), "title": d.Name,
		"mime_type": d.File.MimeType, "modified": d.LastModifiedDateTime, "link": d.WebURL,
		"description": d.Description, "shared": d.Shared != nil, "sharing": nil, "transcript": false, "text": nil}
}

type graphUser struct{ ID, DisplayName, Mail, UserPrincipalName string }

func (g graphSource) directory(ctx context.Context, object string, args map[string]any) (graphPage, error) {
	query := url.Values{"$select": {graphUserFields}, "$top": {"2"}}
	filter := ""
	if object == "person" {
		value, _ := args["email"].(string)
		address := normalizedEmail(value)
		if address == "" || address != strings.ToLower(strings.TrimSpace(value)) {
			return graphPage{}, ErrRejected
		}
		literal := graphLiteral(address)
		filter = "mail eq " + literal + " or userPrincipalName eq " + literal
	} else {
		value, _ := args["domain"].(string)
		domain := strings.ToLower(strings.TrimSpace(value))
		if !validDomain(domain) {
			return graphPage{}, ErrRejected
		}
		filter = "endsWith(mail," + graphLiteral("@"+domain) + ") or endsWith(userPrincipalName," + graphLiteral("@"+domain) + ")"
		query.Set("$count", "true")
	}
	query.Set("$filter", filter)
	var result graphCollection[graphUser]
	if err := g.get(ctx, g.address("/users", query), "", &result); err != nil {
		return graphPage{}, err
	}
	var rows []map[string]any
	for _, user := range result.Values {
		mail, upn := normalizedEmail(user.Mail), normalizedEmail(user.UserPrincipalName)
		if object == "company" {
			domain := strings.ToLower(strings.TrimSpace(fmt.Sprint(args["domain"])))
			if strings.HasSuffix(mail, "@"+domain) || strings.HasSuffix(upn, "@"+domain) {
				rows = append(rows, map[string]any{"id": domain, "domain": domain, "name": domain})
				break
			}
			continue
		}
		address := strings.ToLower(strings.TrimSpace(fmt.Sprint(args["email"])))
		if mail != address && upn != address {
			continue
		}
		if mail == "" {
			mail = upn
		}
		_, domain, _ := strings.Cut(mail, "@")
		rows = append(rows, map[string]any{"id": user.ID, "email": mail, "upn": upn, "name": user.DisplayName, "domain": domain})
	}
	if object == "person" && (len(rows) > 1 || result.Next != "") {
		return graphPage{}, errorsAmbiguousPerson
	}
	return graphPage{rows: rows}, nil
}

var errorsAmbiguousPerson = fmt.Errorf("Microsoft Graph returned more than one person for this address")

func graphLiteral(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

// Transcript keys retain both provider parent IDs so the delegated meeting
// and transcript must still be accessible at the time of every point read.
func transcriptKey(event, meeting, transcript string) string {
	encoded, _ := json.Marshal([3]string{event, meeting, transcript})
	return string(encoded)
}
