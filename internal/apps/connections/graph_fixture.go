package connections

import (
	"bytes"
	"embed"
	"io"
	"net/http"
	"strings"
)

//go:embed testdata/graph/*
var graphFixtures embed.FS

type graphFake struct{}

func graphFixtureClient() *http.Client { return &http.Client{Transport: graphFake{}} }

func (graphFake) RoundTrip(req *http.Request) (*http.Response, error) {
	path := strings.TrimPrefix(req.URL.Path, "/v1.0")
	query := req.URL.Query()
	status, file := http.StatusOK, ""
	switch {
	case req.Method == http.MethodPost && path == "/me/messages":
		status, file = http.StatusCreated, "draft-response.json"
	case req.Method == http.MethodPost && path == eventPath:
		status, file = http.StatusCreated, "event-response.json"
	case req.Method == http.MethodPatch && path == eventPath+"/AAMkEvent1":
		file = "event-response.json"
	case req.Method != http.MethodGet:
		status = http.StatusMethodNotAllowed
	case path == outlookPath:
		switch {
		case query.Get("$skiptoken") != "":
			file = "mail-next.json"
		case query.Get("$deltatoken") != "":
			file = "mail-delta.json"
		default:
			file = "mail-initial.json"
		}
	case path == "/me/messages/AAMkMail1":
		file = "mail-body.json"
	case path == eventPath+"/AAMkEvent1":
		file = "calendar-event.json"
	case path == "/me/calendarView/delta":
		file = "calendar-initial.json"
		if query.Get("$deltatoken") != "" {
			file = "calendar-delta.json"
		}
	case strings.HasSuffix(path, "/root/delta"):
		file = "drive-initial.json"
		if query.Get("$deltatoken") != "" {
			file = "drive-delta.json"
		}
	case strings.HasSuffix(path, "/items/01DOC"):
		file = "drive-item.json"
	case strings.HasSuffix(path, "/items/01DOC/content"):
		file = "drive-text.txt"
	case strings.Contains(path, "/items/"):
		status, file = http.StatusForbidden, "denied.json"
	case path == "/me/onlineMeetings":
		file = "online-meeting.json"
	case path == "/me/onlineMeetings/MSpkLaunchReview/transcripts":
		file = "transcript-list.json"
	case path == "/me/onlineMeetings/MSpkLaunchReview/transcripts/MSMjLaunchTranscript":
		file = "transcript.json"
	case path == "/me/onlineMeetings/MSpkLaunchReview/transcripts/MSMjLaunchTranscript/content":
		file = "transcript.vtt"
	case path == "/users":
		file = "user.json"
	case path == "/me":
		file = "me.json"
	default:
		status = http.StatusNotFound
	}
	var body []byte
	if file != "" {
		var err error
		body, err = graphFixtures.ReadFile("testdata/graph/" + file)
		if err != nil {
			return nil, err
		}
	}
	return &http.Response{StatusCode: status, Body: io.NopCloser(bytes.NewReader(body)), Request: req, Header: make(http.Header)}, nil
}
