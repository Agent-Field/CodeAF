package session

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

func activityMeta(t *testing.T, agent *Agent) Meta {
	t.Helper()
	agent.SettleWrites()
	meta, err := LoadMeta(agent.config.Place.Dir)
	if err != nil || meta.ID == "" {
		t.Fatalf("activity metadata=%+v, %v", meta, err)
	}
	return meta
}

// J1, C6: compaction re-journals the old person line without moving their stamp.
// Rewinding away those copies must not hide a genuine message that follows it
// (the verifier's 15:00 -> copied window -> dropped:2 -> 15:12 sequence).
func TestMetaLastUserAtSurvivesCompactionAndRewind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, placeTranscript)
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Place, c.SessionFile = Place{Dir: dir}, path
	})
	agent.mu.Lock()
	agent.recordUserLocked(userMessage{message: textMessage("user", "finish the report")})
	agent.recordLocked(textMessage("assistant", "the first file is ready"))
	agent.mu.Unlock()
	first := activityMeta(t, agent).LastUserAt
	if first.IsZero() {
		t.Fatal("person did not stamp the metadata")
	}
	replayed, err := replaySessionFile(path)
	if err != nil || len(replayed.messages) != 2 {
		t.Fatalf("original window=%+v, %v", replayed, err)
	}
	window := append([]ai.Message(nil), replayed.messages...)
	agent.mu.Lock()
	agent.file.appendCompaction(compactionPass{at: first.Add(11 * time.Minute)}, 0, window)
	agent.mu.Unlock()
	if got := activityMeta(t, agent).LastUserAt; !got.Equal(first) {
		t.Fatalf("compaction changed person stamp: %v -> %v", first, got)
	}
	lines := readLines(t, path)
	var marker, copied sessionEntry
	if err := json.Unmarshal([]byte(lines[3]), &marker); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(lines[4]), &copied); err != nil {
		t.Fatal(err)
	}
	if marker.Type != "compaction" || marker.Window != 2 || copied.Role != "user" || copied.Content != "finish the report" {
		t.Fatalf("production writer did not copy the two-message window: %+v, %+v", marker, copied)
	}

	removed, err := agent.Rewind()
	if err != nil || len(removed) != 2 {
		t.Fatalf("rewind did not drop the copied turn: removed=%+v, %v", removed, err)
	}
	if got := activityMeta(t, agent).LastUserAt; !got.Equal(first) {
		t.Fatalf("rewind changed person stamp: %v -> %v", first, got)
	}
	// Recovery's promise falls after the old message, before the genuine new
	// direction. Capture the boundary without waiting for a wall-clock interval.
	promise := time.Now()
	agent.mu.Lock()
	agent.recordUserLocked(userMessage{message: textMessage("user", "also check the appendix")})
	agent.mu.Unlock()
	latest := activityMeta(t, agent).LastUserAt
	if !latest.After(promise) || !latest.After(first) {
		t.Fatalf("genuine person did not supersede promise %v: first=%v latest=%v", promise, first, latest)
	}
	lines = readLines(t, path)
	var rewind, person sessionEntry
	if err := json.Unmarshal([]byte(lines[len(lines)-2]), &rewind); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &person); err != nil {
		t.Fatal(err)
	}
	if rewind.Type != "rewind" || rewind.Dropped != 2 || person.Role != "user" || person.Note || person.Content != "also check the appendix" {
		t.Fatalf("missing verifier sequence: rewind=%+v person=%+v", rewind, person)
	}
}

// J1, C6: every runtime-authored road leaves LastUserAt alone, even when it
// reaches the journal with a user role. A fresh crew member keeps the zero
// stamp; an existing conversation retains its person's original instant.
func TestMetaLastUserAtIgnoresSessionAuthoredWork(t *testing.T) {
	for _, spoke := range []bool{false, true} {
		for _, kind := range []string{"session note", "wake", "close resume", "quit resume", "crew brief"} {
			t.Run(map[bool]string{false: "never spoken/", true: "person spoke/"}[spoke]+kind, func(t *testing.T) {
				dir := t.TempDir()
				completer := oneAnswer(1)
				agent, _ := newTestAgent(t, completer, func(c *Config) {
					c.Place, c.SessionFile = Place{Dir: dir}, filepath.Join(dir, placeTranscript)
				})
				if err := SaveMeta(dir, Meta{ID: agent.config.Place.ID()}); err != nil {
					t.Fatal(err)
				}
				if spoke {
					agent.mu.Lock()
					agent.recordUserLocked(userMessage{message: textMessage("user", "original direction")})
					agent.mu.Unlock()
				}
				before := activityMeta(t, agent).LastUserAt
				if before.IsZero() == spoke {
					t.Fatalf("wrong initial stamp: %v", before)
				}
				wakes, stop := agent.WatchWakes()
				defer stop()
				want := "runtime news"
				var accepted bool
				switch kind {
				case "session note":
					agent.mu.Lock()
					agent.landNoteLocked(summaryNotePrefix, want)
					got := agent.lastNoteLocked(summaryNotePrefix)
					agent.mu.Unlock()
					if !strings.Contains(got, want) {
						t.Fatal("session note never reached the request context")
					}
				case "wake":
					accepted = agent.enqueueNote(wakeNote(want))
				case "close resume":
					want = ResumeAfterCloseNote
					accepted = agent.ResumeAfterCloseKey("owed")
				case "quit resume":
					want = ResumeAfterQuitNote
					accepted = agent.ResumeAfterQuit()
				case "crew brief":
					want = "worker instructions"
					if !agent.enqueueNote(briefNote(want)) {
						t.Fatal("crew brief refused")
					}
					accepted = agent.enqueueNote(wakeNote("start the worker"))
				}
				if kind != "session note" {
					if !accepted {
						t.Fatal("session-authored turn refused")
					}
					select {
					case events := <-wakes:
						collect(t, events)
					case <-time.After(time.Second):
						t.Fatal("session-authored turn never started")
					}
					if !strings.Contains(userTextIn(completer.request(0)), want) {
						t.Fatalf("%s did not reach the model", kind)
					}
					found := false
					for _, line := range readLines(t, agent.config.SessionFile) {
						var entry sessionEntry
						if json.Unmarshal([]byte(line), &entry) == nil && entry.Type == "message" && entry.Role == "user" && strings.Contains(entry.Content, want) {
							if !entry.Note {
								t.Fatalf("%s lost its session authorship", kind)
							}
							found = true
						}
					}
					if !found {
						t.Fatalf("%s never reached the journal", kind)
					}
				}
				if got := activityMeta(t, agent).LastUserAt; !got.Equal(before) {
					t.Fatalf("%s moved person's stamp: %v -> %v", kind, before, got)
				}
			})
		}
	}
}
