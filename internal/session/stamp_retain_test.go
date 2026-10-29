package session

import (
	"context"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// Repeated windows use the journal's writer so the fixture follows the same
// marker and re-recording path as a live pass, including occurrence timestamps.
func stampWindowJournal(t *testing.T, passes, width int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	file, _, err := openSessionFile(path, t.TempDir(), "test/model", "")
	if err != nil {
		t.Fatal(err)
	}
	window := make([]ai.Message, width)
	for i := range window {
		role := "assistant"
		if i%2 == 0 {
			role = "user"
		}
		window[i] = textMessage(role, strings.Repeat("long conversation ", 128))
		file.append(window[i], false, nil)
	}
	for range passes {
		file.appendCompaction(compactionPass{folded: 1}, 1000, window)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func assertLiveStamps(t *testing.T, a *Agent) {
	t.Helper()
	a.mu.Lock()
	defer a.mu.Unlock()
	live := map[any]bool{}
	add := func(m ai.Message) { live[stampKey(m)] = true }
	for _, m := range a.messages {
		add(m)
	}
	for _, q := range a.steering {
		add(q.message)
	}
	for _, q := range a.ambient {
		add(q.message)
	}
	for _, q := range a.followups {
		add(q.message.message)
	}
	a.file.mu.Lock()
	stamps := a.file.stamps
	a.file.mu.Unlock()
	stamps.mu.Lock()
	defer stamps.mu.Unlock()
	for key := range stamps.at {
		if !live[key] {
			t.Fatalf("stamp retains a discarded message: %d stamps for %d live identities", len(stamps.at), len(live))
		}
	}
}

func TestResumeRetainsOnlyLiveStamps(t *testing.T) {
	path := stampWindowJournal(t, 40, 32)
	a, _ := newTestAgent(t, &refusingCompleter{t}, func(c *Config) { c.SessionFile = path })
	assertLiveStamps(t, a)
}

func TestResumeHeapDoesNotScaleWithDiscardedWindows(t *testing.T) {
	measure := func(passes int) uint64 {
		path := stampWindowJournal(t, passes, 48)
		runtime.GC()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		a, _ := newTestAgent(t, &refusingCompleter{t}, func(c *Config) { c.SessionFile = path })
		runtime.GC()
		runtime.ReadMemStats(&after)
		runtime.KeepAlive(a)
		if after.HeapAlloc < before.HeapAlloc {
			return 0
		}
		return after.HeapAlloc - before.HeapAlloc
	}
	short, long := measure(6), measure(60)
	const duplicated = 54 * 48 * (len("long conversation ") * 128)
	t.Logf("retained heap growth: 6 passes %d bytes, 60 passes %d bytes; duplicated content %d bytes", short, long, duplicated)
	if int64(long)-int64(short) >= int64(duplicated/4) {
		t.Fatalf("discarded windows retained %d extra bytes; limit %d", int64(long)-int64(short), duplicated/4)
	}
}

func TestLiveCompactionAndRewindRetainOnlyLiveStamps(t *testing.T) {
	a, _ := newTestAgent(t, &refusingCompleter{t}, func(c *Config) {
		c.SessionFile = filepath.Join(t.TempDir(), "session.jsonl")
		c.ContextWindow = 2_000_000
	})
	a.mu.Lock()
	for _, m := range exchanges(6, map[int]string{1: strings.Repeat("large tool result\n", 400)}) {
		a.messages = append(a.messages, m)
		a.file.append(m, false, nil)
	}
	a.mu.Unlock()
	if changed, err := a.compact(context.Background(), nil); !changed || err != nil {
		t.Fatalf("compact = %v, %v", changed, err)
	}
	assertLiveStamps(t, a)
	before := a.Transcript()
	if want := ReadTranscript(a.config.SessionFile).Entries; !reflect.DeepEqual(before, want) {
		t.Fatal("live compaction entries or times differ from the journal")
	}
	if _, err := a.Rewind(); err != nil {
		t.Fatal(err)
	}
	assertLiveStamps(t, a)
	after := a.Transcript()
	if !reflect.DeepEqual(after, before[:len(after)]) {
		t.Fatal("rewind changed the retained entries or their times")
	}
}

func TestResumeStampPrunePreservesShapedEntries(t *testing.T) {
	const at = "2026-10-15T14:00:00Z"
	message := func(role, text string) sessionEntry {
		return sessionEntry{Type: "message", Role: role, Content: text, Timestamp: at}
	}
	tools := []sessionEntry{message("user", "inspect"), {
		Type: "message", Role: "assistant", ToolCalls: []ai.ToolCall{{ID: "c1", Type: "function", Function: ai.ToolCallFunction{Name: "read", Arguments: "{}"}}}, Timestamp: at,
	}, {Type: "message", Role: "tool", ToolCallID: "c1", Content: "result", Timestamp: at}, message("assistant", "done")}
	for name, lines := range map[string][]sessionEntry{
		"empty": nil,
		"short": {message("user", "hello"), message("assistant", "hi")},
		"tools": tools,
		"image and attachment": {{Type: "message", Role: "user", Content: "see image", Timestamp: at,
			Parts:        []journalPart{{Type: journalPartImage, Path: "/tmp/stamp-test.png", MIME: "image/png", SHA256: strings.Repeat("a", 64)}},
			Presentation: &messagePresentation{Audience: audiencePerson, Attachments: []MessageAttachment{{Name: "stamp-test.png", Path: "/tmp/stamp-test.png"}}}}},
		"rewind":      {message("user", "keep"), message("assistant", "kept"), message("user", "drop"), {Type: "rewind", Dropped: 1}, message("user", "replacement")},
		"steer":       {message("user", "start"), message("assistant", "working"), {Type: "message", Role: "user", Content: "correction", Timestamp: at, Steer: &journalSteer{}}},
		"compactions": {message("user", "first"), message("assistant", "old"), {Type: "compaction", Folded: 1, Window: 2, Timestamp: at}, message("user", "first"), message("assistant", "folded"), {Type: "compaction", Folded: 1, Window: 2, Timestamp: at}, message("user", "first"), message("assistant", "folded again")},
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "session.jsonl")
			file, _, err := openSessionFile(path, t.TempDir(), "test/model", "")
			if err != nil {
				t.Fatal(err)
			}
			for _, line := range lines {
				file.writeLine(line)
			}
			if err := file.Close(); err != nil {
				t.Fatal(err)
			}
			// Shape the resume replay before the constructor can prune its indexes.
			file, replayed, err := openSessionFile(path, t.TempDir(), "test/model", "")
			if err != nil {
				t.Fatal(err)
			}
			want := shapeEntries(replayed.messages, file)
			earlier := shapeEntries(replayed.earlier, file)
			floor := len(shapeEntries(replayed.messages[:replayed.overlap], file))
			file.Close()
			a, _ := newTestAgent(t, &refusingCompleter{t}, func(c *Config) {
				c.SessionFile = path
				c.SupportsImages = func(string) bool { return true }
			})
			if !reflect.DeepEqual(a.Transcript(), want) || !reflect.DeepEqual(a.EarlierHistory(), EarlierHistory{Entries: earlier, Floor: floor}) {
				t.Fatal("pruning changed shaped entries, identities, metadata or times")
			}
			for _, entry := range a.Transcript() {
				if entry.At.IsZero() || !entry.At.Equal(lineInstant(at)) {
					t.Fatalf("entry lost its journal instant: %+v", entry)
				}
			}
		})
	}
}

func TestStampRetainKeepsQueueTimes(t *testing.T) {
	a, _ := newTestAgent(t, &refusingCompleter{t}, func(c *Config) { c.SessionFile = filepath.Join(t.TempDir(), "session.jsonl") })
	// A queued message can already be journaled; its stamp must survive a live
	// boundary before the message joins the model's transcript.
	a.mu.Lock()
	defer a.mu.Unlock()
	messages := []ai.Message{textMessage("user", "steering"), textMessage("user", "ambient"), textMessage("user", "followup")}
	ats := make([]time.Time, len(messages))
	for i, m := range messages {
		a.file.append(m, false, nil)
		ats[i] = a.file.written(m)
	}
	a.steering = append(a.steering, userMessage{message: messages[0]})
	a.ambient = append(a.ambient, userMessage{message: messages[1]})
	a.followups = append(a.followups, followUp{message: userMessage{message: messages[2]}})
	defer func() {
		a.steering, a.ambient, a.followups = nil, nil, nil
	}()
	a.retainPresentationLocked()
	for i, m := range messages {
		if got := a.file.written(m); got.IsZero() || !got.Equal(ats[i]) {
			t.Fatalf("queued instant = %v, want %v", got, ats[i])
		}
	}
}

func TestStampRetainKeepsOnlyRequestedInstants(t *testing.T) {
	var absent *stampIndex
	absent.retain(nil)
	var noFile *sessionFile
	noFile.retainStamps(nil)
	s := &stampIndex{}
	s.retain(nil)
	at := time.Date(2026, 10, 15, 14, 0, 0, 0, time.UTC)
	word := textMessage("user", "keep")
	call := ai.Message{Role: "assistant", ToolCalls: []ai.ToolCall{{ID: "call-only"}}}
	dropped := textMessage("assistant", "gone")
	for _, m := range []ai.Message{word, call, dropped} {
		s.remember(m, at)
	}
	s.retain([]ai.Message{word}, []ai.Message{call, {}})
	if len(s.at) != 2 || !s.of(word).Equal(at) || !s.of(call).Equal(at) || !s.of(dropped).IsZero() {
		t.Fatal("retain lost a live instant or kept a discarded one")
	}
	s.retain()
	if len(s.at) != 0 {
		t.Fatal("retain with no messages kept stamps")
	}
}
