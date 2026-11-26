package connections

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"
)

func (g graphSource) onlineMeeting(ctx context.Context, eventID string) (string, error) {
	if !g.transcripts {
		return "", errTeamsGrant
	}
	if !graphIDValid(eventID) {
		return "", ErrRejected
	}
	var event graphEvent
	query := url.Values{"$select": {"id,onlineMeeting,isCancelled"}}
	if err := g.get(ctx, g.address(eventPath+"/"+url.PathEscape(eventID), query), "", &event); err != nil {
		return "", err
	}
	join := event.OnlineMeeting.JoinURL
	if event.ID != eventID || event.IsCancelled || join == "" || len(join) > 4096 {
		return "", errGraphMissing
	}
	var meetings graphCollection[graphEntry]
	query = url.Values{"$filter": {"JoinWebUrl eq " + graphLiteral(join)}}
	if err := g.get(ctx, g.address("/me/onlineMeetings", query), "", &meetings); err != nil {
		return "", err
	}
	if len(meetings.Values) != 1 || meetings.Next != "" || !graphIDValid(meetings.Values[0].ID) {
		return "", errGraphMissing
	}
	return meetings.Values[0].ID, nil
}

type graphTranscript struct {
	graphEntry
	MeetingID, CreatedDateTime, EndDateTime string
}

func (t graphTranscript) row(event, meeting string) (graphEntry, map[string]any) {
	entry := t.graphEntry
	if !graphIDValid(t.ID) || t.MeetingID != meeting {
		return graphEntry{}, nil
	}
	entry.ID = transcriptKey(event, meeting, t.ID)
	return entry, map[string]any{"id": entry.ID, "version": graphVersion(t.ETag, t.CreatedDateTime),
		"title": "Teams transcript", "mime_type": "text/vtt", "modified": t.CreatedDateTime, "link": nil,
		"description": nil, "shared": nil, "sharing": nil, "transcript": true, "text": nil, "meeting": nil}
}

func (g graphSource) transcriptList(ctx context.Context, args map[string]any, cursor, prefer string) (graphPage, error) {
	event, _ := args["meeting"].(string)
	meeting, err := g.onlineMeeting(ctx, event)
	if errors.Is(err, errGraphMissing) {
		return graphPage{}, nil
	}
	if err != nil {
		return graphPage{}, err
	}
	path := "/me/onlineMeetings/" + url.PathEscape(meeting) + "/transcripts"
	address := g.address(path, nil)
	if cursor != "" {
		u, err := url.Parse(cursor)
		if err != nil || u.EscapedPath() != "/v1.0"+path {
			return graphPage{}, ErrGraphCursor
		}
		address, err = g.continuation(cursor)
		if err != nil {
			return graphPage{}, err
		}
	}
	return graphList(ctx, g, address, prefer, false, func(item graphTranscript) (graphEntry, map[string]any) {
		return item.row(event, meeting)
	})
}

func (g graphSource) transcriptPoint(ctx context.Context, key string, detail bool) (graphPage, error) {
	var ids []string
	if json.Unmarshal([]byte(key), &ids) != nil || len(ids) != 3 || !graphIDValid(ids[0]) ||
		!graphIDValid(ids[1]) || !graphIDValid(ids[2]) || key != transcriptKey(ids[0], ids[1], ids[2]) {
		return graphPage{}, ErrRejected
	}
	meeting, err := g.onlineMeeting(ctx, ids[0])
	if errors.Is(err, errGraphMissing) {
		return graphPage{}, nil
	}
	if err != nil {
		return graphPage{}, err
	}
	if meeting != ids[1] {
		return graphPage{}, nil
	}
	path := "/me/onlineMeetings/" + url.PathEscape(meeting) + "/transcripts/" + url.PathEscape(ids[2])
	var item graphTranscript
	if err := g.get(ctx, g.address(path, nil), "", &item); errors.Is(err, errGraphMissing) {
		return graphPage{}, nil
	} else if err != nil {
		return graphPage{}, err
	}
	entry, row := item.row(ids[0], meeting)
	if entry.ID != key {
		return graphPage{}, ErrInvalidRows
	}
	if detail {
		raw, redirect, err := g.request(ctx, http.MethodGet, g.address(path+"/content", url.Values{"$format": {"text/vtt"}}), "", nil, "", maxCell)
		if err != nil {
			return graphPage{}, err
		}
		if redirect != "" || !utf8.Valid(raw) || !strings.HasPrefix(string(raw), "WEBVTT") {
			return graphPage{}, ErrInvalidRows
		}
		row["text"] = string(raw)
	}
	return graphPage{rows: []map[string]any{row}}, nil
}

func (g graphSource) driveText(ctx context.Context, path string, item graphDriveItem) (string, error) {
	const docx = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	kind := graphMediaType(item.File.MimeType)
	if !strings.HasPrefix(kind, "text/") && kind != docx {
		return "", errors.New("text is available for plain text and Word documents")
	}
	limit := maxCell
	if kind == docx {
		limit = maxBytes
	}
	raw, redirect, err := g.request(ctx, http.MethodGet, g.address(path, nil), "", nil, "", limit)
	if err != nil {
		return "", err
	}
	if redirect != "" {
		raw, err = graphDownload(ctx, redirect, limit)
		if err != nil {
			return "", err
		}
	}
	if kind == docx {
		return wordText(raw)
	}
	if !utf8.Valid(raw) {
		return "", ErrInvalidRows
	}
	return string(raw), nil
}

func graphDownload(ctx context.Context, address string, limit int) ([]byte, error) {
	u, err := url.Parse(address)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || u.Fragment != "" || len(address) > 16384 {
		return nil, ErrUnavailable
	}
	host := strings.ToLower(u.Hostname())
	if !strings.HasSuffix(host, ".sharepoint.com") && !strings.HasSuffix(host, ".1drv.com") &&
		!strings.HasSuffix(host, ".onedrive.com") {
		return nil, ErrUnavailable
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, ErrUnavailable
	}
	// The provider URL is already authorized. Neither a bearer nor a referer
	// goes to the download origin, and a redirect cannot choose a new host.
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, graphStatus(resp, false)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, int64(limit)+1))
	if err != nil {
		return nil, ErrUnavailable
	}
	if len(raw) > limit {
		return nil, ErrTooLarge
	}
	return raw, nil
}

func wordText(raw []byte) (string, error) {
	document, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return "", ErrInvalidRows
	}
	for _, file := range document.File {
		if file.Name != "word/document.xml" {
			continue
		}
		if file.UncompressedSize64 > maxBytes {
			return "", ErrTooLarge
		}
		part, err := file.Open()
		if err != nil {
			return "", ErrInvalidRows
		}
		defer part.Close()
		dec := xml.NewDecoder(io.LimitReader(part, maxBytes+1))
		var text strings.Builder
		const word = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
		for {
			token, err := dec.Token()
			if err == io.EOF {
				return strings.TrimSpace(text.String()), nil
			}
			if err != nil {
				return "", ErrInvalidRows
			}
			switch v := token.(type) {
			case xml.StartElement:
				if v.Name.Space == word && v.Name.Local == "t" {
					var value string
					if err := dec.DecodeElement(&value, &v); err != nil {
						return "", ErrInvalidRows
					}
					text.WriteString(value)
				}
			case xml.EndElement:
				if v.Name.Space == word && v.Name.Local == "p" {
					text.WriteByte('\n')
				}
			}
			if text.Len() > maxCell {
				return "", ErrTooLarge
			}
		}
	}
	return "", ErrInvalidRows
}
