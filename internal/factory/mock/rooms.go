package mock

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/session"
)

// THE MOCK KEEPS A CONVERSATION FOR WHAT IS RUNNING NOW. A running item's
// current stage, when it is a chat stage, gets a small real transcript on the
// disk the first time the floor is read, and the phase's Chat names it, so
// `enter` on that stage opens a room the way a real stage does. The files are
// SEEDED WITH session.SeedConversation, the writer the talk lane uses, so a
// room opens as any conversation does. They live under the mock's own temp
// home and never under ~/.codeaf.

// home is the mock's temp folder, made on first use. TMPDIR decides where.
func (w *World) homeDir() string {
	if w.home == "" {
		dir, err := os.MkdirTemp("", "codeaf-factory-mock-")
		if err != nil {
			return ""
		}
		w.home = dir
	}
	return w.home
}

// Home is the folder the mock's transcripts are written under, "" until the
// first one is. A test removes it; the tmp reaper does the rest.
func (w *World) Home() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.home
}

// keepRooms gives each running item's current chat stage a conversation.
// Callers hold the lock.
func (w *World) keepRooms() {
	for _, it := range w.items {
		s := it.Stream
		if s == nil || it.State != factory.StateRunning || s.Cur < 0 || s.Cur >= len(s.Phases) {
			continue
		}
		ph := &s.Phases[s.Cur]
		if ph.Chat != "" || ph.State != factory.PhaseRunning {
			continue
		}
		if st := w.stageOf(it, s.Cur); st == nil || (st.Kind != "" && st.Kind != factory.StageChat) || st.Until == factory.UntilGreen || st.Until == factory.UntilProven {
			continue
		}
		home := w.homeDir()
		if home == "" {
			continue
		}
		path := filepath.Join(home, fmt.Sprintf("item-%d-%s", it.ID, strings.ReplaceAll(ph.Name, " ", "-")), "transcript.jsonl")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			continue
		}
		note := fmt.Sprintf("This is the %s stage of %s, %s. %s", ph.Name, it.Ref(), strings.Join(strings.Fields(it.Title), " "), strings.TrimSpace(ph.Note))
		title := it.Ref() + " · " + ph.Name
		if err := session.SeedConversation(path, home, title, note); err != nil {
			continue
		}
		ph.Chat = path
	}
}

// priorityReasons are the why of a priority, five words or fewer. A rank of
// 3 or 4 says little and is left to the read.
var priorityReasons = map[int][]string{
	1: {"money at risk", "blocks the release", "main is red", "a stranger waits on it"},
	2: {"touches a shared path", "asked for twice", "owner's own work"},
	3: {"small and safe", "cheap to clear"},
}

// rank sets the made-up priority: spread over the floor so the column shows
// bars, with a few left unranked as a read sometimes is.
func (w *World) rank(it *factory.Item) {
	p := 0
	switch pickWeighted(w.rng, []string{"0", "1", "2", "3", "4"}, []float64{0.15, 0.15, 0.3, 0.25, 0.15}) {
	case "1":
		p = 1
	case "2":
		p = 2
	case "3":
		p = 3
	case "4":
		p = 4
	}
	it.Triage.Priority = p
	if rs := priorityReasons[p]; len(rs) > 0 {
		it.Triage.Reason = rs[w.rng.Intn(len(rs))]
	}
}
