package council

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/placegraph"
	"github.com/Agent-Field/codeaf/internal/session"
)

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

type rig struct {
	store    *Store
	places   *placegraph.Store
	clock    *clock
	sessions string
	path     string
}

func newRig(t *testing.T) *rig {
	t.Helper()
	root := t.TempDir()
	clk := &clock{t: time.Date(2026, 10, 10, 15, 0, 0, 0, time.UTC)}
	pg, err := placegraph.Open(placegraph.Options{
		Path: filepath.Join(root, "places.json"),
		Now:  clk.Now,
	})
	if err != nil {
		t.Fatal(err)
	}
	var n int
	var mu sync.Mutex
	next := func(prefix string, width int) string {
		mu.Lock()
		defer mu.Unlock()
		n++
		return fmt.Sprintf("%s%0*x", prefix, width, n)
	}
	path := filepath.Join(root, "state", "councils.json")
	sessions := filepath.Join(root, "sessions")
	st, err := Open(Options{
		Path:        path,
		SessionsDir: sessions,
		Places:      pg,
		Now:         clk.Now,
		NewID:       func() string { return next("cn_", 16) },
		NewChatID:   func() string { return next("", 16) },
	})
	if err != nil {
		t.Fatal(err)
	}
	return &rig{store: st, places: pg, clock: clk, sessions: sessions, path: path}
}

func (r *rig) place(t *testing.T, name string) placegraph.Place {
	t.Helper()
	p, _, err := r.places.CreatePlace(placegraph.NewPlace{Name: name})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestBeginFilesAnOrdinaryChatInBothPlaces(t *testing.T) {
	r := newRig(t)
	marketing := r.place(t, "Marketing")
	software := r.place(t, "Software")

	c, err := r.store.Begin(marketing.ID, software.ID, "  Which draft is accurate? ")
	if err != nil {
		t.Fatal(err)
	}
	if c.Topic != "which draft is accurate?" {
		t.Fatalf("topic key %q", c.Topic)
	}
	if c.Label != "Marketing with Software" {
		t.Fatalf("label %q", c.Label)
	}
	if c.State != StateRunning || c.Turns != 0 || c.Spend != 0 || c.Outcome != "" {
		t.Fatalf("fresh council %+v", c)
	}
	if c.Cap != TurnCap || c.CapUSD != SpendCapUSD || TurnCap != 6 || SpendCapUSD != 0.25 {
		t.Fatalf("caps %+v", c)
	}
	if c.Places != [2]string{marketing.ID, software.ID} {
		t.Fatalf("places %+v", c.Places)
	}
	if !c.ClosedAt.IsZero() || !c.OpenedAt.Equal(r.clock.Now()) {
		t.Fatalf("times opened %s closed %s", c.OpenedAt, c.ClosedAt)
	}

	dir := r.store.SessionDir(c.ChatID)
	meta, err := session.LoadMeta(dir)
	if err != nil {
		t.Fatal(err)
	}
	if meta.ID != c.ChatID || meta.Title != "Marketing with Software" || !meta.Owned {
		t.Fatalf("session meta %+v", meta)
	}
	if meta.Workspace != filepath.Join(dir, "work") {
		t.Fatalf("workspace %q", meta.Workspace)
	}
	if _, err := os.Stat(meta.Workspace); err != nil {
		t.Fatal(err)
	}
	spoken, sure := session.SpokeIn(session.Place{Dir: dir}.Transcript())
	if spoken || !sure {
		t.Fatalf("spoken %v sure %v — a new council chat has a title and no person has spoken", spoken, sure)
	}
	if got := journalTitle(t, session.Place{Dir: dir}.Transcript()); got != c.Label {
		t.Fatalf("journal title %q", got)
	}

	snap, err := r.places.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	filed := snap.PlacesOf(c.ChatID)
	if len(filed) != 2 {
		t.Fatalf("memberships %+v", filed)
	}
	for i, want := range []string{marketing.ID, software.ID} {
		if filed[i].PlaceID != want || filed[i].AddedBy != placegraph.AddedByAI || filed[i].ChatID != c.ChatID {
			t.Fatalf("membership %d %+v", i, filed[i])
		}
	}

	raw, err := os.ReadFile(r.path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	rows, _ := doc["councils"].([]any)
	if len(rows) != 1 {
		t.Fatalf("rows %#v", doc["councils"])
	}
	row := rows[0].(map[string]any)
	for _, key := range []string{"id", "places", "topic", "chatId", "turns", "cap", "spend", "capUSD", "state", "label", "openedAt"} {
		if _, ok := row[key]; !ok {
			t.Errorf("missing %s in %v", key, row)
		}
	}
	if _, ok := row["outcome"]; ok {
		t.Errorf("an open discussion stored an outcome: %v", row["outcome"])
	}
	if row["cap"] != float64(6) || row["capUSD"] != 0.25 || row["state"] != "running" {
		t.Fatalf("record %v", row)
	}
	places, _ := row["places"].([]any)
	if len(places) != 2 {
		t.Fatalf("places %v", row["places"])
	}

	again, err := Open(Options{Path: r.path, SessionsDir: r.sessions, Places: r.places, Now: r.clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	got, err := again.Get(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ChatID != c.ChatID || got.Label != c.Label || got.Topic != c.Topic {
		t.Fatalf("reloaded %+v", got)
	}
}

func TestCooldownIsTheUnorderedPairAndTheTopicKey(t *testing.T) {
	r := newRig(t)
	marketing := r.place(t, "Marketing")
	software := r.place(t, "Software")
	design := r.place(t, "Design")

	first, err := r.store.Begin(marketing.ID, software.ID, "Which draft is accurate?")
	if err != nil {
		t.Fatal(err)
	}
	// Still open after the hour: a running or paused discussion is not a
	// cooldown, it is the same discussion.
	r.clock.Advance(2 * time.Hour)
	_, err = r.store.Begin(software.ID, marketing.ID, "  WHICH   DRAFT is accurate? ")
	if !errors.Is(err, ErrOpen) {
		t.Fatalf("swapped pair while running: %v", err)
	}
	if _, err := r.store.Record(first.ID, 2, 0.01, StatePaused, ""); err != nil {
		t.Fatal(err)
	}
	_, err = r.store.Begin(marketing.ID, software.ID, "which draft is accurate?")
	if !errors.Is(err, ErrOpen) {
		t.Fatalf("paused: %v", err)
	}
	if _, err := r.store.Record(first.ID, 2, 0.01, StateDecided, "promise it for v2.4.1"); err != nil {
		t.Fatal(err)
	}
	// It opened two hours ago, so the hour has passed.
	second, err := r.store.Begin(software.ID, marketing.ID, "which draft is accurate?")
	if err != nil {
		t.Fatalf("after the hour: %v", err)
	}
	if second.ID == first.ID || second.ChatID == first.ChatID {
		t.Fatal("a reopen reused the first chat")
	}
	if second.Label != "Software with Marketing" {
		t.Fatalf("label follows the order given, got %q", second.Label)
	}
	if _, err := r.store.Record(second.ID, 1, 0.02, StateEscalated, ""); err != nil {
		t.Fatal(err)
	}
	_, err = r.store.Begin(marketing.ID, software.ID, "Which draft is accurate?")
	if !errors.Is(err, ErrCooldown) {
		t.Fatalf("inside the hour after it ended: %v", err)
	}
	other, err := r.store.Begin(marketing.ID, design.ID, "Which draft is accurate?")
	if err != nil {
		t.Fatalf("a different pair: %v", err)
	}
	if other.Label != "Marketing with Design" {
		t.Fatalf("label %q", other.Label)
	}
	topic, err := r.store.Begin(marketing.ID, software.ID, "A different question")
	if err != nil {
		t.Fatalf("a different topic: %v", err)
	}
	if topic.Topic != "a different question" {
		t.Fatalf("topic %q", topic.Topic)
	}

	// Exactly one hour after the second discussion opened, the pair may go again.
	r.clock.Advance(time.Hour)
	third, err := r.store.Begin(marketing.ID, software.ID, "which draft is accurate?")
	if err != nil {
		t.Fatalf("at the hour: %v", err)
	}
	if third.ChatID == second.ChatID {
		t.Fatal("reused chat")
	}
}

func TestRecordMovesStateAndKeepsTheCaps(t *testing.T) {
	r := newRig(t)
	a := r.place(t, "Marketing")
	b := r.place(t, "Config parser")
	c, err := r.store.Begin(a.ID, b.ID, "Does the port hold?")
	if err != nil {
		t.Fatal(err)
	}
	if c.Label != "Marketing with Config parser" {
		t.Fatalf("label %q", c.Label)
	}
	paused, err := r.store.Record(c.ID, 1, 0.04, StatePaused, "")
	if err != nil || paused.State != StatePaused || paused.Turns != 1 {
		t.Fatalf("pause %+v %v", paused, err)
	}
	if _, err := r.store.Record(c.ID, 0, 0.04, StateRunning, ""); err == nil {
		t.Fatal("turns went backwards")
	}
	if _, err := r.store.Record(c.ID, 1, 0.01, StateRunning, ""); err == nil {
		t.Fatal("spend went backwards")
	}
	if _, err := r.store.Record(c.ID, 7, 0.04, StateRunning, ""); err == nil {
		t.Fatal("a seventh turn was recorded")
	}
	if _, err := r.store.Record(c.ID, 1, 0.04, StatePaused, "too soon"); err == nil {
		t.Fatal("an open discussion stored an outcome")
	}
	running, err := r.store.Record(c.ID, 6, 0.30, StateRunning, "")
	if err != nil {
		t.Fatal(err)
	}
	if running.Spend != 0.30 || running.Turns != TurnCap || running.CapUSD != SpendCapUSD {
		t.Fatalf("crossing the dollar cap is still this discussion's cost: %+v", running)
	}
	decided, err := r.store.Record(c.ID, 6, 0.30, StateDecided, "promise it for v2.4.1")
	if err != nil {
		t.Fatal(err)
	}
	if decided.Outcome != "promise it for v2.4.1" || decided.ClosedAt.IsZero() || !decided.State.Ended() {
		t.Fatalf("decided %+v", decided)
	}
	again, err := r.store.Record(c.ID, 6, 0.30, StateDecided, "promise it for v2.4.1")
	if err != nil || again.Outcome != decided.Outcome {
		t.Fatalf("repeat %+v %v", again, err)
	}
	if _, err := r.store.Record(c.ID, 6, 0.31, StateDecided, "promise it for v2.4.1"); !errors.Is(err, ErrEnded) {
		t.Fatalf("ended discussion changed: %v", err)
	}

	listed, err := r.store.InPlace(b.ID)
	if err != nil || len(listed) != 1 || listed[0].ID != c.ID {
		t.Fatalf("in place %+v %v", listed, err)
	}
}

func TestBeginRefusesACouncilThatCannotBeFiled(t *testing.T) {
	r := newRig(t)
	marketing := r.place(t, "Marketing")
	software := r.place(t, "Software")
	if _, err := r.places.Archive(software.ID); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name  string
		a, b  string
		topic string
		want  error
	}{
		{"one place", marketing.ID, marketing.ID, "topic", ErrInvalid},
		{"empty topic", marketing.ID, software.ID, "   ", ErrInvalid},
		{"control character", marketing.ID, software.ID, "hello\x07", ErrInvalid},
		{"missing", marketing.ID, "pl_missing", "topic", placegraph.ErrNotFound},
		{"archived", marketing.ID, software.ID, "topic", placegraph.ErrArchived},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := r.store.Begin(tc.a, tc.b, tc.topic)
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
	entries, err := os.ReadDir(r.sessions)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("a refused council left sessions: %v", entries)
	}
	snap, err := r.places.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Memberships) != 0 {
		t.Fatalf("a refused council filed chats: %+v", snap.Memberships)
	}
	if _, err := os.Stat(r.path); !os.IsNotExist(err) {
		t.Fatal("a refused council wrote the document")
	}
	if r.store.SessionDir("../etc") != "" || r.store.SessionDir("") != "" {
		t.Fatal("a bad chat id walked out of the sessions bucket")
	}
}

func TestADamagedFileIsLeftAlone(t *testing.T) {
	r := newRig(t)
	if err := os.WriteFile(r.path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Open(Options{Path: r.path, SessionsDir: r.sessions, Places: r.places, Now: r.clock.Now})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("corrupt: %v", err)
	}
	if _, err := os.ReadFile(r.path); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(r.path)
	if string(body) != "{" {
		t.Fatalf("corrupt file was rewritten: %q", body)
	}

	newer := []byte(`{"version":99,"councils":[]}`)
	if err := os.WriteFile(r.path, newer, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = Open(Options{Path: r.path, SessionsDir: r.sessions, Places: r.places, Now: r.clock.Now})
	if !errors.Is(err, ErrNewerFile) {
		t.Fatalf("newer: %v", err)
	}
	body, _ = os.ReadFile(r.path)
	if string(body) != string(newer) {
		t.Fatalf("newer file was rewritten: %q", body)
	}
}

func TestTwoOpensOfDifferentTopicsBothFile(t *testing.T) {
	r := newRig(t)
	a := r.place(t, "Marketing")
	b := r.place(t, "Software")
	var wg sync.WaitGroup
	errCh := make(chan error, 2)
	for _, topic := range []string{"first question", "second question"} {
		wg.Add(1)
		go func(topic string) {
			defer wg.Done()
			_, err := r.store.Begin(a.ID, b.ID, topic)
			errCh <- err
		}(topic)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	list, err := r.store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("got %d councils", len(list))
	}
}

func journalTitle(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for line := range bytesLines(raw) {
		var row struct {
			Type  string `json:"type"`
			Title string `json:"title"`
		}
		if err := json.Unmarshal(line, &row); err != nil {
			t.Fatal(err)
		}
		if row.Type == "title" {
			return row.Title
		}
	}
	t.Fatal("no title line")
	return ""
}

func bytesLines(raw []byte) func(func([]byte) bool) {
	return func(yield func([]byte) bool) {
		start := 0
		for i, b := range raw {
			if b != '\n' {
				continue
			}
			if !yield(raw[start:i]) {
				return
			}
			start = i + 1
		}
		if start < len(raw) {
			yield(raw[start:])
		}
	}
}
