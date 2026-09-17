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
	"path/filepath"
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

func TestFlushCapsAtOneThousandLinesDroppingTheOldest(t *testing.T) {
	testHome(t)
	recorder := newRelay(t)
	MarkNoticeShown()
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
	// The cap lives in Flush now, so the 1005 spooled lines stay until one
	// runs: cap then send, in that order.
	if got := len(SpoolContents()); got != 1005 {
		t.Fatalf("the spool holds %d lines before any flush, want 1005", got)
	}
	if err := Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := recorder.count(); got != 20 {
		t.Fatalf("the relay saw %d posts, want 20 (the capped thousand, 50 at a time)", got)
	}
	if got := len(SpoolContents()); got != 0 {
		t.Fatalf("after a flush against no relay the spool holds %d lines, want 0", got)
	}
}

// TestFlushKeepsTheNewestThousandLines pins the cap's direction and order: when
// the relay takes nothing, the thousand lines a flush leaves behind are the
// newest thousand, not the oldest.
func TestFlushKeepsTheNewestThousandLines(t *testing.T) {
	testHome(t)
	relay := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(relay.Close)
	t.Setenv("CODEAF_TELEMETRY_ENDPOINT", relay.URL)
	MarkNoticeShown()
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
	if err := Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	contents := SpoolContents()
	if len(contents) != MaxSpoolLines {
		t.Fatalf("the spool holds %d lines after a failed flush, want %d", len(contents), MaxSpoolLines)
	}
	var oldest, newest map[string]any
	if err := json.Unmarshal(contents[0], &oldest); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(contents[len(contents)-1], &newest); err != nil {
		t.Fatal(err)
	}
	if got := oldest["session_id_hash"]; got != hashHex("session-5") {
		t.Errorf("the oldest kept line is session-5's, got the hash of %q", got)
	}
	if got := newest["session_id_hash"]; got != hashHex("session-1004") {
		t.Errorf("the newest line is %v, want session-1004's", got)
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

// TestFlushPartialFailure pins the counted-confirmed-batches law: when the
// relay accepts the first batch and refuses the second, only the first
// batch's events are deleted, and everything unsent remains in the spool in
// order. The old code marked every batch sent once any batch had gone out.
func TestFlushPartialFailure(t *testing.T) {
	testHome(t)
	request := 0
	var mu sync.Mutex
	flaky := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, r *http.Request) {
		mu.Lock()
		request++
		n := request
		mu.Unlock()
		if n == 1 {
			writer.WriteHeader(http.StatusNoContent)
			return
		}
		writer.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(flaky.Close)
	t.Setenv("CODEAF_TELEMETRY_ENDPOINT", flaky.URL)
	MarkNoticeShown()
	const total = 120 // three batches: 50 + 50 + 20
	ids := make([]string, total)
	for i := 0; i < total; i++ {
		event := SessionStarted(ModeChat, false, fmt.Sprintf("session-%03d", i), freshClock(t))
		ids[i] = event.ID
		if err := SpoolSync(event); err != nil {
			t.Fatalf("spooling event %d: %v", i, err)
		}
	}
	if err := Flush(context.Background()); err != nil {
		t.Fatalf("Flush must never fail a run: %v", err)
	}
	if request != 2 {
		t.Fatalf("the relay saw %d requests, want 2 (the second refused, so the flush stopped)", request)
	}
	left := SpoolContents()
	if len(left) != 70 {
		t.Fatalf("%d lines remain, want 70 (50 + 20 unsent)", len(left))
	}
	// The survivors must be exactly the second and third batches' events,
	// in the order they were spooled.
	for i, raw := range left {
		var row map[string]any
		if err := json.Unmarshal(raw, &row); err != nil {
			t.Fatal(err)
		}
		if row["event_id"] != ids[50+i] {
			t.Errorf("remaining line %d is event %v, want %q (batch 2 onward, in order)", i, row["event_id"], ids[50+i])
			break
		}
	}
	// A retry against a healthy relay: the unsent 70 arrive, and only then
	// is the spool empty.
	healthy := newRelay(t)
	if err := Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := healthy.count(); got != 2 {
		t.Fatalf("the retry made %d posts, want 2 (50 + 20)", got)
	}
	if left := len(SpoolContents()); left != 0 {
		t.Errorf("%d lines remain after the retry, want 0", left)
	}
}

// TestSpoolConcurrent pins the in-process mutex: eight goroutines spooling
// 400 events between them must leave 400 lines on disk — whole lines, not
// interleaved fragments — and every event exactly once. It only means
// something under -race, where the interleavings are shaken out.
func TestSpoolConcurrent(t *testing.T) {
	testHome(t)
	const goroutines = 8
	const perGoroutine = 50
	var group sync.WaitGroup
	group.Add(goroutines)
	for worker := 0; worker < goroutines; worker++ {
		go func(worker int) {
			defer group.Done()
			for i := 0; i < perGoroutine; i++ {
				Spool(SessionStarted(ModeChat, false, fmt.Sprintf("session-%02d-%02d", worker, i), freshClock(t)))
			}
		}(worker)
	}
	group.Wait()
	// Spool returns before its append runs; wait for the lines to land.
	for deadline := time.Now().Add(10 * time.Second); len(SpoolContents()) != goroutines*perGoroutine; {
		if time.Now().After(deadline) {
			t.Fatalf("only %d of %d events were spooled, the rest were lost", len(SpoolContents()), goroutines*perGoroutine)
		}
		time.Sleep(5 * time.Millisecond)
	}
	seen := map[string]bool{}
	for _, raw := range SpoolContents() {
		var row map[string]any
		if err := json.Unmarshal(raw, &row); err != nil {
			t.Fatalf("a concurrent append left a broken line: %v\n%s", err, raw)
		}
		id, _ := row["event_id"].(string)
		if seen[id] {
			t.Fatalf("event %s was spooled twice", id)
		}
		seen[id] = true
	}
	if len(seen) != goroutines*perGoroutine {
		t.Fatalf("%d distinct events were spooled, want %d", len(seen), goroutines*perGoroutine)
	}
}

// TestFlushRacesSpool pins the join: a flush holding the spool renamed away
// while a concurrent Spool appends to it must lose nothing and duplicate
// nothing — the spooler's lines land in spool.jsonl, not in the sending file
// the flush is about to delete.
func TestFlushRacesSpool(t *testing.T) {
	testHome(t)
	recorder := newRelay(t)
	MarkNoticeShown()
	// Warm the spool so the flush has lines to send while the spooler runs.
	for i := 0; i < 30; i++ {
		if err := SpoolSync(SessionStarted(ModeChat, false, fmt.Sprintf("warm-%d", i), freshClock(t))); err != nil {
			t.Fatal(err)
		}
	}
	before := len(SpoolContents())
	var group sync.WaitGroup
	const spoolers = 4
	const perSpooler = 25
	group.Add(1)
	go func() {
		defer group.Done()
		for i := 0; i < perSpooler; i++ {
			Spool(SessionStarted(ModeChat, false, fmt.Sprintf("race-%02d", i), freshClock(t)))
		}
	}()
	group.Add(1)
	go func() {
		defer group.Done()
		_ = Flush(context.Background())
	}()
	group.Add(1)
	go func() {
		defer group.Done()
		// A spooler that waits for the append to land, so the race is with a
		// flush that may already hold the mutex.
		for i := 0; i < perSpooler; i++ {
			if err := SpoolSync(SessionStarted(ModeChat, false, fmt.Sprintf("sync-%02d", i), freshClock(t))); err != nil {
				t.Error(err)
			}
		}
	}()
	// An extra spooler that starts while the flush may be mid-rename.
	group.Add(spoolers)
	for worker := 0; worker < spoolers; worker++ {
		go func(worker int) {
			defer group.Done()
			for i := 0; i < 10; i++ {
				Spool(SessionStarted(ModeChat, false, fmt.Sprintf("late-%d-%d", worker, i), freshClock(t)))
			}
		}(worker)
	}
	group.Wait()
	// Wait out the fire-and-forget appends.
	for deadline := time.Now().Add(10 * time.Second); ; {
		left := len(SpoolContents())
		if left == 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	totalSent := 0
	for i := range recorder.posts {
		totalSent += len(recorder.posts[i].events)
	}
	// Nothing duplicated across the wire: one id, one arrival.
	seen := map[string]bool{}
	for i := range recorder.posts {
		for _, event := range recorder.posts[i].events {
			id, _ := event["event_id"].(string)
			if id == "" {
				continue
			}
			if seen[id] {
				t.Errorf("event %s was sent twice: the flush duplicated it", id)
			}
			seen[id] = true
		}
	}
	left := SpoolContents()
	if got := totalSent + len(left); got != before+spoolers*10+perSpooler*2 {
		t.Fatalf("%d events were sent (%d) plus left (%d), want %d — events were lost or duplicated at the join",
			got, totalSent, len(left), before+spoolers*10+perSpooler*2)
	}
}

// TestFlushAdoptsOrphanedSendingFiles pins the ten-minute rule: a stale
// spool.sending.* file a dead process left behind is folded back into the
// spool at the start of the next flush, and nothing is adopted twice.
func TestFlushAdoptsOrphanedSendingFiles(t *testing.T) {
	testHome(t)
	MarkNoticeShown()
	orphan := filepath.Join(telemetryDir(), "spool.sending.999999.deadbeef")
	if err := os.MkdirAll(telemetryDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	orphanLines := make([]string, 3)
	for i := range orphanLines {
		event := SessionStarted(ModeChat, false, fmt.Sprintf("orphan-%d", i), freshClock(t))
		line, err := jsonMarshal(event)
		if err != nil {
			t.Fatal(err)
		}
		orphanLines[i] = event.ID
		if err := appendLines(orphan, []spoolEntry{{line: string(line)}}); err != nil {
			t.Fatal(err)
		}
	}
	// A fresh sending file — written now — must NOT be adopted.
	fresh := filepath.Join(telemetryDir(), "spool.sending.888888.cafe")
	freshEvent := SessionStarted(ModeChat, false, "fresh-orphan", freshClock(t))
	freshLine, err := jsonMarshal(freshEvent)
	if err != nil {
		t.Fatal(err)
	}
	if err := appendLines(fresh, []spoolEntry{{line: string(freshLine)}}); err != nil {
		t.Fatal(err)
	}
	// Age the orphan past ten minutes by rewriting its mtime.
	stale := time.Now().Add(-11 * time.Minute)
	if err := os.Chtimes(orphan, stale, stale); err != nil {
		t.Fatal(err)
	}
	// Spool something so the flush has a sending cycle at all.
	if err := SpoolSync(SessionStarted(ModeChat, false, "live", freshClock(t))); err != nil {
		t.Fatal(err)
	}
	if err := Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	// The live event was already in spool.jsonl before the flush, and
	// adoption appends the orphan's lines to that same file, so the live
	// line comes first and the three adopted lines follow it. The relay is
	// unreachable — testHome points CODEAF_TELEMETRY_ENDPOINT at a dead
	// loopback address — so every send fails, Flush stays silent, and all
	// four lines are appended back: they remain spooled, in order.
	left := SpoolContents()
	if len(left) != 4 {
		t.Fatalf("%d lines remain after adopting the orphan, want 4 (1 live, 3 adopted)", len(left))
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Errorf("the stale sending file was not removed: %v", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Errorf("a fresh sending file must never be adopted or removed: %v", err)
	}
	var liveRow map[string]any
	if err := json.Unmarshal(left[0], &liveRow); err != nil {
		t.Fatal(err)
	}
	if got := liveRow["session_id_hash"]; got != hashHex("live") {
		t.Errorf("the first remaining line is the live event's, got the hash of %q", got)
	}
	for i := 0; i < 3; i++ {
		var row map[string]any
		if err := json.Unmarshal(left[i+1], &row); err != nil {
			t.Fatal(err)
		}
		if row["event_id"] != orphanLines[i] {
			t.Errorf("adopted line %d is %v, want %q", i, row["event_id"], orphanLines[i])
		}
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
