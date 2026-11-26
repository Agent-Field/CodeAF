package connections

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

//go:embed testdata/google/message.json
var fixtureMessage []byte

//go:embed testdata/google/calendar.json
var fixtureCalendar []byte

//go:embed testdata/google/drive.json
var fixtureDrive []byte

//go:embed testdata/google/draft.json
var fixtureDraft []byte

// googleFixtureClient presents provider-shaped responses without any network
// or account. Both bindings exercise the same mapping and policy.
func googleFixtureClient() *http.Client {
	return &http.Client{Transport: googleFixtureTransport{}}
}

type googleFixtureTransport struct{}

func (googleFixtureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	path := req.URL.Path
	raw := []byte(nil)
	status := http.StatusOK
	switch {
	case req.Method == http.MethodPost && path == gmailPath+"/drafts":
		var draft struct{ Message struct{ Raw string } }
		if err := json.NewDecoder(req.Body).Decode(&draft); err != nil || draft.Message.Raw == "" {
			status = http.StatusBadRequest
		} else {
			raw = fixtureDraft
		}
	case req.Method == http.MethodPost && path == calendarPath,
		req.Method == http.MethodPatch && path == calendarPath+"/e1":
		raw = []byte(`{"id":"e1","etag":"\"2\""}`)
	case req.Method == http.MethodPost && path == drivePath+"/doc1/comments":
		raw = []byte(`{"id":"c1"}`)
	case req.Method != http.MethodGet:
		status = http.StatusForbidden
	case path == gmailPath+"/messages" && req.URL.Query().Get("q") == "label:planning" &&
		req.URL.Query().Get("pageToken") == "planning-2":
		raw = []byte(`{"messages":[{"id":"m2","threadId":"t2"}],"resultSizeEstimate":2}`)
	case path == gmailPath+"/messages" && req.URL.Query().Get("q") == "label:planning":
		raw = []byte(`{"messages":[{"id":"m1","threadId":"t1"}],"nextPageToken":"planning-2","resultSizeEstimate":2}`)
	case path == gmailPath+"/messages/m2":
		raw = []byte(`{"id":"m2","threadId":"t2","historyId":"18","labelIds":["PLANNING"],"payload":{"headers":[{"name":"Subject","value":"Quarterly planning"}]}}`)
	case path == gmailPath+"/messages":
		raw = []byte(`{"messages":[{"id":"m1","threadId":"t1"}],"resultSizeEstimate":1}`)
	case path == gmailPath+"/messages/m1" && req.URL.Query().Get("fields") != "":
		raw = fixtureMessage
	case path == gmailPath+"/messages/m1":
		raw = []byte(`{"id":"m1","payload":{"mimeType":"text/plain","body":{"data":"U2VlIHlvdSBhdCBwbGFubmluZy4"}}}`)
	case path == gmailPath+"/profile":
		raw = []byte(`{"emailAddress":"jo@example.com","historyId":"19"}`)
	case path == gmailPath+"/history":
		raw = []byte(`{"historyId":"19"}`)
	case path == calendarPath:
		raw = fixtureCalendar
	case path == calendarPath+"/e1":
		var page struct{ Items []json.RawMessage }
		_ = json.Unmarshal(fixtureCalendar, &page)
		raw = page.Items[0]
	case path == drivePath:
		raw = fixtureDrive
	case path == drivePath+"/doc1":
		var page struct{ Files []json.RawMessage }
		_ = json.Unmarshal(fixtureDrive, &page)
		raw = page.Files[0]
	case path == "/drive/v3/changes/startPageToken":
		raw = []byte(`{"startPageToken":"drive-1"}`)
	case path == "/drive/v3/changes":
		raw = []byte(`{"newStartPageToken":"drive-1","changes":[]}`)
	case path == drivePath+"/doc1/export":
		raw = []byte("Ari: Let's review the launch plan.\nJo: I'll share the agenda.")
	case strings.HasPrefix(path, drivePath+"/"):
		status = http.StatusNotFound
	default:
		status = http.StatusNotFound
	}
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(raw)), Request: req}, nil
}
