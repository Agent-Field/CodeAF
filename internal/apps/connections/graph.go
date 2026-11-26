package connections

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	pathpkg "path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/connect"
)

const (
	outlookPath      = "/me/mailFolders/inbox/messages/delta"
	eventPath        = "/me/events"
	graphUserFields  = "id,displayName,mail,userPrincipalName"
	graphMailFields  = "id,changeKey,conversationId,receivedDateTime,subject,from,toRecipients,ccRecipients,hasAttachments,isDraft,categories,webLink"
	graphDriveFields = "id,eTag,name,file,deleted,lastModifiedDateTime,webUrl,description,shared"
)

var (
	ErrGraphCursor      = errors.New("the Microsoft 365 sync cursor is invalid or expired; refresh this source")
	ErrGraphThrottle    = errors.New("Microsoft Graph is throttling this source; try again later")
	errGraphMissing     = errors.New("the Microsoft 365 item is missing or unavailable to your account")
	errTeamsGrant       = fmt.Errorf("%w: Microsoft Teams transcripts require an administrator's consent", ErrSourcePermission)
	errTeamsDisabled    = fmt.Errorf("%w: your Microsoft tenant has disabled Graph access to Teams transcripts", ErrSourcePermission)
	errGraphConditional = fmt.Errorf("%w: your organization's Microsoft Conditional Access policy blocked this request", ErrSourcePermission)
	graphID             = regexp.MustCompile(`^[A-Za-z0-9_!=+.%@:-]{1,768}$`)
	domainLabel         = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
)

type graphSource struct {
	client      *http.Client
	origin      string
	transcripts bool
}

type graphPage struct {
	rows             []map[string]any
	removed          []string
	next, checkpoint string
}

type graphCollection[T any] struct {
	Values []T    `json:"value"`
	Next   string `json:"@odata.nextLink"`
	Delta  string `json:"@odata.deltaLink"`
}

type graphEntry struct {
	ID      string
	ETag    string         `json:"@odata.etag"`
	Removed map[string]any `json:"@removed"`
}

func graphIDValid(id string) bool {
	return graphID.MatchString(id) && id != "." && id != ".."
}

func (g graphSource) address(path string, query url.Values) string {
	origin := g.origin
	if origin == "" {
		origin = "https://graph.microsoft.com"
	}
	address := origin + "/v1.0" + path
	if len(query) > 0 {
		address += "?" + query.Encode()
	}
	return address
}

func (g graphSource) continuation(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != "graph.microsoft.com" || u.User != nil ||
		u.Opaque != "" || u.Fragment != "" || !strings.HasPrefix(u.Path, "/v1.0/") || pathpkg.Clean(u.Path) != u.Path ||
		len(raw) > 8192 {
		return "", ErrGraphCursor
	}
	if g.origin != "" {
		return g.origin + u.RequestURI(), nil
	}
	return raw, nil
}

func (g graphSource) request(ctx context.Context, method, address, prefer string, body []byte, version string, max int) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, method, address, bytes.NewReader(body))
	if err != nil {
		return nil, "", ErrRejected
	}
	write := method != http.MethodGet
	if write {
		req.Header.Set("Content-Type", "application/json")
		req.GetBody = nil
		if version != "" {
			req.Header.Set("If-Match", version)
		}
	}
	req.Header.Set("Prefer", `IdType="ImmutableId", outlook.timezone="UTC"`+prefer)
	if strings.Contains(req.URL.Path, "/users") {
		req.Header.Set("ConsistencyLevel", "eventual")
	}
	client := *g.client
	// Graph content redirects are read separately, without the OAuth transport.
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		// An OAuth failure happens before the HTTP transport can send the write.
		if auth := connect.MicrosoftRequestError(err); auth != nil {
			return nil, "", fmt.Errorf("%w: %s", ErrSourcePermission, auth)
		}
		if write {
			return nil, "", ErrUncertain
		}
		return nil, "", ErrUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusFound && !write && strings.HasSuffix(req.URL.Path, "/content") {
		location := resp.Header.Get("Location")
		if location == "" {
			return nil, "", ErrUnavailable
		}
		return nil, location, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || resp.StatusCode == http.StatusAccepted {
		return nil, "", graphStatus(resp, write)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, int64(max)+1))
	if write && (err != nil || len(raw) > max) {
		return nil, "", ErrUncertain
	}
	if err != nil {
		return nil, "", ErrUnavailable
	}
	if len(raw) > max {
		return nil, "", ErrTooLarge
	}
	return raw, "", nil
}

func graphStatus(resp *http.Response, write bool) error {
	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		var payload struct {
			Error struct{ Code, Message string }
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		_ = decodeJSON(raw, &payload)
		if strings.EqualFold(payload.Error.Code, "insufficient_claims") ||
			strings.Contains(resp.Header.Get("WWW-Authenticate"), "insufficient_claims") {
			return errGraphConditional
		}
		if payload.Error.Message == "Graph API access to transcripts is disabled for this tenant." {
			return errTeamsDisabled
		}
		return ErrSourcePermission
	case http.StatusConflict, http.StatusPreconditionFailed:
		return ErrConflict
	case http.StatusNotFound:
		if write {
			return ErrConflict
		}
		return errGraphMissing
	case http.StatusGone:
		if !write {
			return ErrGraphCursor
		}
	case http.StatusTooManyRequests:
		return ErrGraphThrottle
	}
	if write {
		if resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != http.StatusRequestTimeout {
			return ErrRejected
		}
		return ErrUncertain
	}
	return ErrUnavailable
}

func (g graphSource) get(ctx context.Context, address, prefer string, out any) error {
	raw, redirect, err := g.request(ctx, http.MethodGet, address, prefer, nil, "", maxBytes)
	if err != nil {
		return err
	}
	if redirect != "" {
		return ErrUnavailable
	}
	return decodeJSON(raw, out)
}

func (g graphSource) rows(ctx context.Context, read Read, cursor, key string, args map[string]any, limit int, detail bool) (graphPage, error) {
	if key != "" {
		return g.point(ctx, read, key, detail)
	}
	prefer := ", odata.maxpagesize=" + strconv.Itoa(limit)
	path, query := "", url.Values{}
	switch read.Table {
	case "outlook":
		path = outlookPath
		query.Set("$select", graphMailFields)
		query.Set("$top", strconv.Itoa(limit))
	case "calendar":
		path = "/me/calendarView/delta"
		start, end, err := graphWindow(read, args)
		if err != nil {
			return graphPage{}, err
		}
		query.Set("startDateTime", start.Format(time.RFC3339Nano))
		query.Set("endDateTime", end.Format(time.RFC3339Nano))
	case "drive":
		path = graphDriveRoot(read.GraphDrive) + "/root/delta"
		query.Set("$select", graphDriveFields)
		query.Set("$top", strconv.Itoa(limit))
	case "person", "company":
		return g.directory(ctx, read.Table, args)
	case "transcripts":
		return g.transcriptList(ctx, args, cursor, prefer)
	default:
		return graphPage{}, ErrRejected
	}
	address := g.address(path, query)
	if cursor != "" {
		var err error
		address, err = g.continuation(cursor)
		if err != nil {
			return graphPage{}, err
		}
	}
	switch read.Table {
	case "outlook":
		return graphList(ctx, g, address, prefer, true, func(item graphMessage) (graphEntry, map[string]any) {
			return item.graphEntry, item.row()
		})
	case "calendar":
		return graphList(ctx, g, address, prefer, true, func(item graphEvent) (graphEntry, map[string]any) {
			if item.IsCancelled {
				item.Removed = map[string]any{}
			}
			return item.graphEntry, item.row()
		})
	default:
		return graphList(ctx, g, address, prefer, true, func(item graphDriveItem) (graphEntry, map[string]any) {
			if item.Deleted != nil {
				item.Removed = item.Deleted
			}
			return item.graphEntry, item.row()
		})
	}
}

func graphList[T any](ctx context.Context, g graphSource, address, prefer string, delta bool, project func(T) (graphEntry, map[string]any)) (graphPage, error) {
	var collection graphCollection[T]
	if err := g.get(ctx, address, prefer, &collection); err != nil {
		return graphPage{}, err
	}
	if collection.Values == nil || len(collection.Values) > maxRows || delta && (collection.Next == "") == (collection.Delta == "") {
		return graphPage{}, ErrInvalidRows
	}
	p := graphPage{next: collection.Next, checkpoint: collection.Delta}
	for _, continuation := range []string{p.next, p.checkpoint} {
		if continuation != "" {
			if _, err := g.continuation(continuation); err != nil {
				return graphPage{}, err
			}
		}
	}
	// Delta can repeat an item within the same page. The last observation wins.
	type observation struct {
		key     string
		row     map[string]any
		removed bool
	}
	observations := make([]observation, 0, len(collection.Values))
	index := map[string]int{}
	for _, item := range collection.Values {
		entry, row := project(item)
		if entry.ID == "" || len(entry.ID) > 1024 {
			return graphPage{}, ErrInvalidRows
		}
		o := observation{entry.ID, row, entry.Removed != nil}
		if at, ok := index[entry.ID]; ok {
			observations[at] = o
		} else {
			index[entry.ID] = len(observations)
			observations = append(observations, o)
		}
	}
	for _, o := range observations {
		if o.removed {
			p.removed = append(p.removed, o.key)
		} else if o.row != nil {
			p.rows = append(p.rows, o.row)
		}
	}
	return p, nil
}

func graphWindow(read Read, args map[string]any) (time.Time, time.Time, error) {
	from, _ := args["from"].(string)
	to, _ := args["to"].(string)
	if from == "" && to == "" {
		from, to = read.GraphStart, read.GraphEnd
	}
	start, startErr := time.Parse(time.RFC3339Nano, from)
	end, endErr := time.Parse(time.RFC3339Nano, to)
	if startErr != nil || endErr != nil || !end.After(start) || end.Sub(start) > 366*24*time.Hour {
		return time.Time{}, time.Time{}, errors.New("the calendar needs an absolute start and end within one year")
	}
	return start, end, nil
}

func (g graphSource) point(ctx context.Context, read Read, key string, detail bool) (graphPage, error) {
	if read.Table == "transcripts" {
		return g.transcriptPoint(ctx, key, detail)
	}
	if !graphIDValid(key) {
		return graphPage{}, ErrRejected
	}
	path := url.PathEscape(key)
	var row map[string]any
	var err error
	switch read.Table {
	case "outlook":
		fields := graphMailFields
		prefer := ""
		if detail {
			fields += ",body"
			prefer = `, outlook.body-content-type="text"`
		}
		var item graphMessage
		err = g.get(ctx, g.address("/me/messages/"+path, url.Values{"$select": {fields}}), prefer, &item)
		row = item.row()
		if detail {
			if err == nil && !strings.EqualFold(item.Body.ContentType, "text") {
				return graphPage{}, ErrInvalidRows
			}
			row["body"] = item.Body.Content
		}
	case "calendar":
		var item graphEvent
		err = g.get(ctx, g.address(eventPath+"/"+path, nil), "", &item)
		if !item.IsCancelled {
			row = item.row()
		}
	case "drive":
		address := graphDriveRoot(read.GraphDrive) + "/items/" + path
		var item graphDriveItem
		err = g.get(ctx, g.address(address, url.Values{"$select": {graphDriveFields}}), "", &item)
		if item.Deleted == nil {
			row = item.row()
		}
		if detail && err == nil && row != nil && row["id"] == key {
			row["text"], err = g.driveText(ctx, address+"/content", item)
		}
	default:
		return graphPage{}, ErrRejected
	}
	if errors.Is(err, errGraphMissing) {
		return graphPage{}, nil
	}
	if err != nil || row == nil {
		return graphPage{}, err
	}
	if row["id"] != key {
		return graphPage{}, ErrInvalidRows
	}
	return graphPage{rows: []map[string]any{row}}, nil
}

func graphDriveRoot(id string) string {
	if id == "" {
		return "/me/drive"
	}
	return "/drives/" + url.PathEscape(id)
}

func validDomain(domain string) bool {
	parts := strings.Split(domain, ".")
	if len(domain) > 253 || len(parts) < 2 || net.ParseIP(domain) != nil {
		return false
	}
	for _, part := range parts {
		if !domainLabel.MatchString(part) {
			return false
		}
	}
	return true
}

func graphMediaType(value string) string {
	kind, _, _ := mime.ParseMediaType(value)
	return kind
}
