package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// recordedPost is one POST the relay saw.
type recordedPost struct {
	events      []map[string]any
	schemaVer   float64
	contentType string
	raw         string
}

type relayRecorder struct {
	mu    sync.Mutex
	posts []recordedPost
}

func (r *relayRecorder) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	body, err := io.ReadAll(request.Body)
	if err != nil {
		writer.WriteHeader(http.StatusBadRequest)
		return
	}
	var parsed struct {
		SchemaVersion float64          `json:"schema_version"`
		Events        []map[string]any `json:"events"`
	}
	parseErr := json.Unmarshal(body, &parsed)
	r.mu.Lock()
	r.posts = append(r.posts, recordedPost{
		events:      parsed.Events,
		schemaVer:   parsed.SchemaVersion,
		contentType: request.Header.Get("Content-Type"),
		raw:         string(body),
	})
	r.mu.Unlock()
	if parseErr != nil {
		writer.WriteHeader(http.StatusBadRequest)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (r *relayRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.posts)
}

// newRelay starts a recording relay and points CODEAF_TELEMETRY_ENDPOINT at
// it, the way the endpoint override is meant to be used.
func newRelay(t *testing.T) *relayRecorder {
	t.Helper()
	recorder := &relayRecorder{}
	server := httptest.NewServer(recorder)
	t.Cleanup(server.Close)
	t.Setenv("CODEAF_TELEMETRY_ENDPOINT", server.URL)
	return recorder
}

func TestFlushSendsFiftyPerPostAndRemovesSentLines(t *testing.T) {
	testHome(t)
	recorder := newRelay(t)
	MarkNoticeShown()
	for i := 0; i < 120; i++ {
		if err := SpoolSync(SessionStarted(ModeChat, false, fmt.Sprintf("session-%d", i), freshClock(t))); err != nil {
			t.Fatalf("spooling event %d: %v", i, err)
		}
	}
	if err := Flush(context.Background()); err != nil {
		t.Fatalf("Flush must never fail a run: %v", err)
	}
	if got := recorder.count(); got != 3 {
		t.Fatalf("the relay saw %d posts, want 3 (50 + 50 + 20)", got)
	}
	seen := map[string]bool{}
	for i := range recorder.posts {
		if i < 2 && len(recorder.posts[i].events) != MaxEventsPerPOST {
			t.Errorf("post %d carried %d events, want %d", i, len(recorder.posts[i].events), MaxEventsPerPOST)
		}
		if recorder.posts[i].schemaVer != schemaVersion {
			t.Errorf("post %d carried schema_version %v, want %d", i, recorder.posts[i].schemaVer, schemaVersion)
		}
		if recorder.posts[i].contentType != "application/json" {
			t.Errorf("post %d carried Content-Type %q", i, recorder.posts[i].contentType)
		}
		for _, event := range recorder.posts[i].events {
			if id, ok := event["event_id"].(string); ok {
				seen[id] = true
			}
		}
	}
	if len(seen) != 120 {
		t.Errorf("the relay received %d distinct events, want 120", len(seen))
	}
	if left := len(SpoolContents()); left != 0 {
		t.Errorf("%d lines remain in the spool after everything was confirmed sent, want 0", left)
	}
}

func TestFlushIsSilentUntilTheNoticeWasShown(t *testing.T) {
	testHome(t)
	recorder := newRelay(t)
	if err := SpoolSync(SessionStarted(ModeChat, false, "session-gated", freshClock(t))); err != nil {
		t.Fatal(err)
	}
	if err := Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := recorder.count(); got != 0 {
		t.Fatalf("the relay saw %d posts before the notice was marked shown, want 0", got)
	}
	if left := len(SpoolContents()); left != 1 {
		t.Fatalf("%d lines remain after a gated flush, want 1", left)
	}
	MarkNoticeShown()
	if err := Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := recorder.count(); got != 1 {
		t.Fatalf("after MarkNoticeShown the relay saw %d posts, want 1", got)
	}
}

func TestFlushDropsEventsOlderThanSevenDays(t *testing.T) {
	testHome(t)
	recorder := newRelay(t)
	MarkNoticeShown()

	fresh := SessionStarted(ModeChat, false, "session-fresh", freshClock(t))
	ancient := SessionStarted(ModeChat, false, "session-ancient", freshClock(t))
	ancient.Time = freshClock(t).Add(-8 * 24 * time.Hour).UTC().Format(time.RFC3339)
	// A spooled line just inside the seven days stays.
	border := SessionStarted(ModeChat, false, "session-border", freshClock(t))
	border.Time = freshClock(t).Add(-7*24*time.Hour + time.Minute).UTC().Format(time.RFC3339)

	if err := SpoolSync(ancient); err != nil {
		t.Fatal(err)
	}
	if err := SpoolSync(border); err != nil {
		t.Fatal(err)
	}
	if err := SpoolSync(fresh); err != nil {
		t.Fatal(err)
	}
	if err := Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := recorder.count(); got != 1 {
		t.Fatalf("the relay saw %d posts, want 1", got)
	}
	events := recorder.posts[0].events
	if len(events) != 2 {
		t.Fatalf("the post carried %d events, want 2 (the ancient one is dropped)", len(events))
	}
	ids := map[string]bool{}
	for _, event := range events {
		if id, ok := event["event_id"].(string); ok {
			ids[id] = true
		}
	}
	if !ids[fresh.ID] || !ids[border.ID] || ids[ancient.ID] {
		t.Errorf("the wrong events were sent: fresh %v, border %v, ancient %v", ids[fresh.ID], ids[border.ID], ids[ancient.ID])
	}
	if left := len(SpoolContents()); left != 0 {
		t.Errorf("%d lines remain, want 0", left)
	}
}

func TestFlushRespectsTheCallerDeadline(t *testing.T) {
	testHome(t)
	block := make(chan struct{})
	hanging := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		<-block
		writer.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(func() {
		close(block)
		hanging.Close()
	})
	t.Setenv("CODEAF_TELEMETRY_ENDPOINT", hanging.URL)
	MarkNoticeShown()
	for i := 0; i < 3; i++ {
		if err := SpoolSync(SessionStarted(ModeChat, false, fmt.Sprintf("session-hang-%d", i), freshClock(t))); err != nil {
			t.Fatal(err)
		}
	}
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	err := Flush(ctx)
	cancel()
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Flush returned %v; it must never fail a run", err)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("Flush took %s against a hanging relay; the caller's deadline is hard", elapsed)
	}
	if left := len(SpoolContents()); left != 3 {
		t.Errorf("%d lines remain after an unconfirmed send, want all 3 still spooled", left)
	}
}

func TestSpoolCapsAtOneThousandLinesDroppingTheOldest(t *testing.T) {
	testHome(t)
	if err := ensureDir(); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	for i := 0; i < 1005; i++ {
		line, err := jsonMarshal(SessionStarted(ModeChat, false, fmt.Sprintf("session-%d", i), freshClock(t)))
		if err != nil {
			t.Fatal(err)
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}
	if err := os.WriteFile(spoolPath(), buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SpoolSync(SessionStarted(ModeChat, false, "the-newest", freshClock(t))); err != nil {
		t.Fatal(err)
	}
	contents := SpoolContents()
	if len(contents) != MaxSpoolLines {
		t.Fatalf("the spool holds %d lines, want %d", len(contents), MaxSpoolLines)
	}
	var oldest, newest map[string]any
	if err := json.Unmarshal(contents[0], &oldest); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(contents[len(contents)-1], &newest); err != nil {
		t.Fatal(err)
	}
	if got := oldest["session_id_hash"]; got != hashHex("session-6") {
		t.Errorf("the oldest kept line is session-6's, got the hash of %q", got)
	}
	if got := newest["session_id_hash"]; got != hashHex("the-newest") {
		t.Errorf("the newest line is %v, want the-newest", got)
	}
}

func TestFlushDropsPropKeysAndEventNamesTheContractDoesNotAllow(t *testing.T) {
	testHome(t)
	recorder := newRelay(t)
	MarkNoticeShown()

	// A spool line carrying a prop key the contract does not name — written by
	// an older build, or by hand. The key must not reach the wire.
	edited := SessionStarted(ModeChat, false, "session-edited", freshClock(t))
	edited.Props["prompt"] = sentinels[5]
	if err := SpoolSync(edited); err != nil {
		t.Fatal(err)
	}
	if err := Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := recorder.count(); got != 1 {
		t.Fatalf("the relay saw %d posts, want 1", got)
	}
	if strings.Contains(recorder.posts[0].raw, "prompt") || strings.Contains(recorder.posts[0].raw, sentinels[5]) {
		t.Error("an unallowlisted prop key reached the wire")
	}

	// An event whose name the contract does not define can never be sent
	// validly, so it is discarded rather than posted or kept forever.
	stray := fmt.Sprintf(`{"event_name":"git_remote_probe","event_id":%q,"install_id_hash":%q,"event_time":%q,"props":{"remote":"git@github.com:Agent-Field/secret.git"}}`,
		randomHex(16), InstallIDHash(), roundTime(freshClock(t)))
	file, err := os.OpenFile(spoolPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(stray + "\n"); err != nil {
		t.Fatal(err)
	}
	file.Close()
	if err := Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := recorder.count(); got != 1 {
		t.Errorf("the relay saw %d posts after an unsendable line was spooled, want 1", got)
	}
	for _, post := range recorder.posts {
		if strings.Contains(post.raw, "git_remote_probe") {
			t.Error("an event name the contract does not define reached the wire")
		}
	}
	if left := len(SpoolContents()); left != 0 {
		t.Errorf("%d lines remain after the flush, want 0", left)
	}
}

func TestAFailingRelayKeepsTheSpoolAndStaysSilent(t *testing.T) {
	testHome(t)
	// Nothing listens here.
	t.Setenv("CODEAF_TELEMETRY_ENDPOINT", "http://127.0.0.1:1/telemetry")
	MarkNoticeShown()
	if err := SpoolSync(SessionStarted(ModeChat, false, "session-lost", freshClock(t))); err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = writer
	captured := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, reader)
		captured <- buf.String()
	}()
	flushErr := Flush(context.Background())
	os.Stderr = old
	writer.Close()
	if flushErr != nil {
		t.Fatalf("Flush returned %v against a dead relay; it must never fail a run", flushErr)
	}
	if noise := <-captured; strings.TrimSpace(noise) != "" {
		t.Errorf("a failed flush must stay silent, wrote %q to stderr", noise)
	}
	if left := len(SpoolContents()); left != 1 {
		t.Errorf("%d lines remain after a failed send, want 1", left)
	}
}

func TestShowReturnsTheSpoolAsPrettyJSON(t *testing.T) {
	testHome(t)
	if got := Show(); got != "[]" {
		t.Fatalf("an empty spool shows %q, want []", got)
	}
	event := SessionStarted(ModeTask, false, "session-shown", freshClock(t))
	if err := SpoolSync(event); err != nil {
		t.Fatal(err)
	}
	var shown []map[string]any
	if err := json.Unmarshal([]byte(Show()), &shown); err != nil {
		t.Fatalf("Show must print valid JSON: %v", err)
	}
	if len(shown) != 1 {
		t.Fatalf("Show printed %d events, want 1", len(shown))
	}
	if shown[0]["event_id"] != event.ID {
		t.Errorf("Show printed event_id %v, want %q", shown[0]["event_id"], event.ID)
	}
}
