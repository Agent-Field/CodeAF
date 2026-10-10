package desktopbridge

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

const seenSession = "aaaa000000000001"

const seenIndex = `{"id":"1","title":"a","label":"a","status":"failed","sessionId":"aaaa000000000001","endedAt":"2026-10-01T10:00:00Z"}` + "\n" +
	`{"id":"2","title":"b","label":"b","status":"failed","sessionId":"aaaa000000000001","endedAt":"2026-10-02T10:00:00.25Z"}` + "\n"

// seenFixture is a real sessions root read through the real world reader: one conversation, two landed failures.
func seenFixture(t *testing.T) (*Bridge, string, *WorldFeed) {
	t.Helper()
	root := t.TempDir()
	bucket := filepath.Join(root, "-work-lexer")
	dir := filepath.Join(bucket, seenSession)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	line, _ := json.Marshal(map[string]any{"type": "message", "role": "user", "content": "hello", "timestamp": time.Now().Format(time.RFC3339Nano)})
	if err := os.WriteFile(filepath.Join(dir, "transcript.jsonl"), append(line, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := session.SaveMeta(dir, session.Meta{ID: seenSession, Title: "Lexer", Workspace: "/work/lexer", LastUserAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bucket, "tasks.jsonl"), []byte(seenIndex), 0o600); err != nil {
		t.Fatal(err)
	}
	feed := NewWorldFeed(func() session.World { return session.ReadWorld(root) })
	feed.maxAge = 0
	b := New(worldToken, nil)
	b.UseHistory(&History{Root: root})
	b.UseWorld(feed)
	t.Cleanup(b.Close)
	return b, dir, feed
}

func worldRow(t *testing.T, b *Bridge) WorldRow {
	t.Helper()
	var full struct{ Rows []WorldRow }
	w := requestAs(b, worldToken, "GET", "/api/engine/world", "")
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &full) != nil {
		t.Fatalf("GET /world = %d %s", w.Code, w.Body.String())
	}
	for _, r := range full.Rows {
		if r.Session == seenSession {
			return r
		}
	}
	t.Fatalf("no row for %s in %+v", seenSession, full)
	return WorldRow{}
}

func postSeen(b *Bridge, body string) *httptest.ResponseRecorder {
	return requestAs(b, worldToken, "POST", "/api/engine/world/failures/seen", body)
}

func TestTheWorldNamesTheNewestFailureAndCountsWhatNobodyHasSeen(t *testing.T) {
	b, _, _ := seenFixture(t)
	row := worldRow(t, b)
	if row.Failed != 2 || row.UnseenFailed != 2 || row.Failure == nil || row.Failure.Task != "2" || row.Failure.At != "2026-10-02T10:00:00.25Z" {
		t.Fatalf("row = %+v failure=%+v", row, row.Failure)
	}
}

func TestSeenIsCanonicalAcrossWindowsAndHidesNothingElse(t *testing.T) {
	b, dir, _ := seenFixture(t)
	before := worldRow(t, b)

	w := postSeen(b, `{"session":"`+seenSession+`","at":"`+before.Failure.At+`","task":"2"}`)
	if w.Code != 200 {
		t.Fatalf("mark = %d %s", w.Code, w.Body.String())
	}
	var got struct {
		Changed      bool
		UnseenFailed int
		Through      string
	}
	json.Unmarshal(w.Body.Bytes(), &got)
	if !got.Changed || got.UnseenFailed != 0 || got.Through != before.Failure.At {
		t.Fatalf("answer = %+v", got)
	}
	after := worldRow(t, b)
	if after.UnseenFailed != 0 || after.Failed != before.Failed || after.Tasks != before.Tasks || after.Running != before.Running || after.NeedsYou != before.NeedsYou || after.State != before.State {
		t.Fatalf("seen changed more than the unseen count:\nbefore %+v\nafter  %+v", before, after)
	}
	// A second reader (another window, a restart) sees the same durable fact from the folder alone.
	if meta, _ := session.LoadMeta(dir); meta.FailuresSeen.IsZero() {
		t.Fatal("the mark is not in the conversation's own meta.json")
	}
	fresh := New(worldToken, nil)
	fresh.UseHistory(&History{Root: filepath.Dir(filepath.Dir(dir))})
	fresh.UseWorld(NewWorldFeed(func() session.World { return session.ReadWorld(filepath.Dir(filepath.Dir(dir))) }))
	t.Cleanup(fresh.Close)
	if row := worldRow(t, fresh); row.UnseenFailed != 0 {
		t.Fatalf("a restarted engine forgot the mark: %+v", row)
	}
}

func TestSeenOnTheFeedReachesAnAttachedWindowAsAWorldRecord(t *testing.T) {
	b, _, feed := seenFixture(t)
	clock := &manualClock{}
	feed.newTicker = clock.ticker
	srv := httptest.NewServer(b)
	t.Cleanup(func() { srv.CloseClientConnections(); srv.Close() })
	feed.refresh()
	stream := openStream(t, srv, "0")
	rec, _ := stream.next(t) // the first record replays the world
	_ = rec
	if w := postSeen(b, `{"session":"`+seenSession+`","at":"2026-10-02T10:00:00.25Z"}`); w.Code != 200 {
		t.Fatalf("mark = %d %s", w.Code, w.Body.String())
	}
	deadline := time.After(5 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("the other window was never told")
		default:
		}
		rec, line := stream.next(t)
		if line != "" || rec.Type != "world" {
			continue
		}
		var delta worldDelta
		json.Unmarshal(rec.Payload, &delta)
		for _, r := range delta.Rows {
			if r.Session == seenSession && r.UnseenFailed == 0 {
				return
			}
		}
	}
}

func TestSeenIsIdempotentAndRefusesWhatWasNeverShown(t *testing.T) {
	b, dir, _ := seenFixture(t)
	body := `{"session":"` + seenSession + `","at":"2026-10-01T10:00:00Z"}`
	first := postSeen(b, body)
	second := postSeen(b, body)
	if first.Code != 200 || second.Code != 200 || !strings.Contains(first.Body.String(), `"changed":true`) || !strings.Contains(second.Body.String(), `"changed":false`) {
		t.Fatalf("first %d %s second %d %s", first.Code, first.Body.String(), second.Code, second.Body.String())
	}
	if !strings.Contains(second.Body.String(), `"unseenFailed":1`) {
		t.Fatalf("marking the older failure covered the newer: %s", second.Body.String())
	}
	for name, c := range map[string]struct {
		body string
		want int
	}{
		"not json":          {`nope`, 400},
		"no session":        {`{"at":"2026-10-01T10:00:00Z"}`, 400},
		"bad time":          {`{"session":"` + seenSession + `","at":"yesterday"}`, 400},
		"unknown":           {`{"session":"ffff000000000000","at":"2026-10-01T10:00:00Z"}`, 404},
		"path traversal":    {`{"session":"../x","at":"2026-10-01T10:00:00Z"}`, 404},
		"a future failure":  {`{"session":"` + seenSession + `","at":"2030-01-01T00:00:00Z"}`, 409},
		"between failures":  {`{"session":"` + seenSession + `","at":"2026-10-01T12:00:00Z"}`, 409},
		"wrong task for at": {`{"session":"` + seenSession + `","at":"2026-10-02T10:00:00.25Z","task":"1"}`, 409},
	} {
		if w := postSeen(b, c.body); w.Code != c.want {
			t.Fatalf("%s = %d %s", name, w.Code, w.Body.String())
		}
	}
	if meta, _ := session.LoadMeta(dir); !meta.FailuresSeen.Equal(time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("a refused mark moved the watermark to %v", meta.FailuresSeen)
	}
	if w := requestAs(b, worldToken, "GET", "/api/engine/world/failures/seen", ""); w.Code != 405 {
		t.Fatalf("GET = %d", w.Code)
	}
}

func TestSeenNeedsTheEngineToken(t *testing.T) {
	b, dir, _ := seenFixture(t)
	r := httptest.NewRequest("POST", "/api/engine/world/failures/seen", strings.NewReader(`{"session":"`+seenSession+`","at":"2026-10-01T10:00:00Z"}`))
	r.Host = "127.0.0.1:1420"
	w := httptest.NewRecorder()
	b.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatalf("unauthenticated mark = %d", w.Code)
	}
	if meta, _ := session.LoadMeta(dir); !meta.FailuresSeen.IsZero() {
		t.Fatal("an unauthenticated request wrote a mark")
	}
}

func requestAs(b *Bridge, token, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Host = "127.0.0.1:1420"
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	b.ServeHTTP(w, r)
	return w
}
