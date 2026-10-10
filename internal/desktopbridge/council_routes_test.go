package desktopbridge

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/council"
	"github.com/Agent-Field/codeaf/internal/placegraph"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/teams"
)

// TestHomeLiveIncludesCouncilsAndHistoryListsThem is the home feed and the
// history seam together. The session folders sit under the history root on
// purpose: the world walk skips a named chat nobody has typed in, and History
// still has to list it.
func TestHomeLiveIncludesCouncilsAndHistoryListsThem(t *testing.T) {
	rig := newPlacesRig(t)
	release := rig.mk("Release")
	marketing := rig.mk("Marketing", release)
	software := rig.mk("Software")
	elsewhere := rig.mk("Elsewhere")
	docs := rig.mk("Docs")

	root := t.TempDir()
	now := placesEpoch
	var chats, ids int
	store, err := council.Open(council.Options{
		Path:        filepath.Join(root, "councils.json"),
		SessionsDir: filepath.Join(root, "council-sessions"),
		Places:      rig.p.Store,
		Now:         func() time.Time { return now },
		NewID: func() string {
			ids++
			return "cn_" + padHex(ids)
		},
		NewChatID: func() string {
			chats++
			return padHex(chats)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	run, err := council.NewRunner(store, rig.p.Store, refuseCouncilAsk, journalSink{store: store, now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	rig.b.UseCouncils(store, run)
	rig.b.UseHistory(&History{Root: root})

	launch := mustBegin(t, store, marketing, software, "Launch date")
	now = now.Add(time.Minute)
	banner := mustBegin(t, store, marketing, software, "Banner copy")
	if _, err := store.Record(banner.ID, 2, 0.02, council.StateRunning, ""); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	headline := mustBegin(t, store, marketing, software, "The headline")
	if _, err := store.Record(headline.ID, 1, 0.01, council.StatePaused, ""); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	pricing := mustBegin(t, store, marketing, software, "Pricing")
	if _, err := store.Record(pricing.ID, 2, 0.01, council.StateDecided, "promise it for v2.4.1"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	legal := mustBegin(t, store, marketing, software, "Legal review")
	if _, err := store.Record(legal.ID, 6, 0.25, council.StateEscalated, ""); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	other := mustBegin(t, store, elsewhere, docs, "Other work")
	if _, err := store.Record(other.ID, 1, 0.01, council.StateDecided, "leave it"); err != nil {
		t.Fatal(err)
	}

	started := placesEpoch.Add(-time.Hour)
	openRun := working(row("open1", "Publish draft", placesEpoch), "t-open", "Publish the draft", started)
	openRun.Open = true
	closedRun := working(row("shut1", "Lexer chat", placesEpoch.Add(-2*time.Hour)), "t-shut", "Fix the lexer", started.Add(-time.Hour))
	// The council chat is also a running task in the world. Home must not
	// list it twice: the discussion row is that chat.
	dup := working(row(launch.ChatID, launch.Label, placesEpoch.Add(-time.Minute)), "t-dup", "Should not list", started)
	dup.Open = true
	need := waiting(row("need1", "Launch", placesEpoch.Add(-3*time.Hour)), "Publish the launch post?")
	quiet := row("quiet1", "Notes", placesEpoch.Add(-4*time.Hour))
	rig.setRows(openRun, closedRun, dup, need, quiet)
	for _, id := range []string{"open1", "shut1", "need1", "quiet1"} {
		if code := rig.do(http.MethodPost, "/places/"+marketing+"/members", map[string]any{"chats": []string{id}}, nil); code != 200 {
			t.Fatalf("file %s: %d", id, code)
		}
	}

	walk := map[string]bool{}
	for _, row := range session.ReadWorld(root).Sessions() {
		walk[row.ID] = true
	}
	for _, id := range []string{launch.ChatID, banner.ChatID, headline.ChatID, pricing.ChatID, legal.ChatID, other.ChatID} {
		if walk[id] {
			t.Fatalf("the world walk listed %s before anyone spoke", id)
		}
	}

	home := getHome(t, rig, marketing)
	if home.Decided != 1 {
		t.Fatalf("marketing decided = %d, want 1", home.Decided)
	}
	wantLive(t, home.Live,
		liveWant{kind: liveKindDiscussion, title: "Marketing with Software", detail: "banner copy", turn: 2, of: 6, council: banner.ID},
		liveWant{kind: liveKindDiscussion, title: "Marketing with Software", detail: "launch date", turn: 0, of: 6, council: launch.ID},
		liveWant{kind: liveKindDiscussion, title: "Marketing with Software", detail: "the headline", turn: 1, of: 6, council: headline.ID},
		liveWant{kind: liveKindRunning, title: "Publish the draft", detail: "Publish draft", task: "t-open"},
		liveWant{kind: liveKindClosed, title: "Fix the lexer", detail: "Lexer chat", task: "t-shut"},
		liveWant{kind: liveKindNeedsYou, title: "Publish the launch post?"},
	)
	for _, row := range home.Live {
		if row.Title == "Should not list" || row.Title == "Notes" || row.CouncilID == pricing.ID || row.CouncilID == legal.ID || row.CouncilID == other.ID {
			t.Fatalf("live listed %s (%s)", row.Title, row.Kind)
		}
		if row.Kind == liveKindDiscussion && row.SessionFile == "" {
			t.Fatalf("discussion %s has no session file", row.CouncilID)
		}
	}

	releaseHome := getHome(t, rig, release)
	if releaseHome.Decided != 1 || !liveHas(releaseHome.Live, banner.ID) {
		t.Fatalf("release decided=%d live=%v", releaseHome.Decided, titlesOf(releaseHome.Live))
	}
	softHome := getHome(t, rig, software)
	if softHome.Decided != 1 || len(softHome.Live) != 3 {
		t.Fatalf("software decided=%d live=%d %v", softHome.Decided, len(softHome.Live), titlesOf(softHome.Live))
	}
	elseHome := getHome(t, rig, elsewhere)
	if elseHome.Decided != 1 || len(elseHome.Live) != 0 {
		t.Fatalf("elsewhere decided=%d live=%v", elseHome.Decided, titlesOf(elseHome.Live))
	}
	rootHome := getHome(t, rig, placegraph.RootID)
	if rootHome.Decided != 2 || !liveHas(rootHome.Live, banner.ID) || liveHas(rootHome.Live, other.ID) {
		t.Fatalf("root decided=%d live=%v", rootHome.Decided, titlesOf(rootHome.Live))
	}

	_, body := rig.raw(http.MethodGet, "/places/"+marketing+"/home", nil)
	if !strings.Contains(string(body), `"turn":0`) {
		t.Fatalf("a discussion on turn 0 omitted the count: %s", body)
	}
	if strings.Contains(string(body), `"decided":0`) {
		t.Fatalf("zero decided was sent: %s", body)
	}

	var listed struct {
		Councils []councilItem `json:"councils"`
	}
	if code := rig.do(http.MethodGet, "/councils", nil, &listed); code != 200 || len(listed.Councils) != 6 {
		t.Fatalf("list: %d len=%d", code, len(listed.Councils))
	}
	if listed.Councils[0].ID != other.ID || listed.Councils[5].ID != launch.ID {
		t.Fatalf("list order: %s … %s", listed.Councils[0].ID, listed.Councils[5].ID)
	}
	var filtered struct {
		Councils []councilItem `json:"councils"`
	}
	if code := rig.do(http.MethodGet, "/councils?place="+marketing, nil, &filtered); code != 200 || len(filtered.Councils) != 5 {
		t.Fatalf("place filter: %d len=%d", code, len(filtered.Councils))
	}
	for _, c := range filtered.Councils {
		if c.ID == other.ID {
			t.Fatal("the other place's discussion was included")
		}
	}
	var none struct {
		Councils []councilItem `json:"councils"`
	}
	if code := rig.do(http.MethodGet, "/councils?place=missing", nil, &none); code != 200 || len(none.Councils) != 0 {
		t.Fatalf("missing place: %d len=%d", code, len(none.Councils))
	}

	var hist HistoryList
	if code := rig.do(http.MethodGet, "/history", nil, &hist); code != 200 {
		t.Fatalf("history: %d", code)
	}
	if n := countHistory(hist.Items, launch.ChatID); n != 1 || histTitle(hist.Items, launch.ChatID) != "Marketing with Software" {
		t.Fatalf("launch in history: %d %q", n, histTitle(hist.Items, launch.ChatID))
	}
	for _, id := range []string{banner.ChatID, headline.ChatID, pricing.ChatID, legal.ChatID} {
		if countHistory(hist.Items, id) != 1 || histTitle(hist.Items, id) != "Marketing with Software" {
			t.Fatalf("%s in history: %d %q", id, countHistory(hist.Items, id), histTitle(hist.Items, id))
		}
	}
	if countHistory(hist.Items, other.ChatID) != 1 || histTitle(hist.Items, other.ChatID) != "Elsewhere with Docs" {
		t.Fatalf("other in history: %d %q", countHistory(hist.Items, other.ChatID), histTitle(hist.Items, other.ChatID))
	}

	var steered councilItem
	if code := rig.do(http.MethodPost, "/councils/"+headline.ID+"/steer", map[string]any{"text": "look at the fixture"}, &steered); code != 200 || steered.State != string(council.StateRunning) || steered.Turns != 1 {
		t.Fatalf("steer: %d state=%s turns=%d", code, steered.State, steered.Turns)
	}
	journal, err := os.ReadFile(steered.SessionFile)
	if err != nil || !strings.Contains(string(journal), "look at the fixture") {
		t.Fatalf("journal: %v %s", err, journal)
	}
	walk = map[string]bool{}
	for _, row := range session.ReadWorld(root).Sessions() {
		walk[row.ID] = true
	}
	if !walk[headline.ChatID] {
		t.Fatal("after a person spoke, the walk still skipped the chat")
	}
	hist = HistoryList{}
	if code := rig.do(http.MethodGet, "/history", nil, &hist); code != 200 || countHistory(hist.Items, headline.ChatID) != 1 {
		t.Fatalf("history after steer: %d count=%d", code, countHistory(hist.Items, headline.ChatID))
	}
	again := getHome(t, rig, marketing)
	if !liveHas(again.Live, headline.ID) || liveCount(again.Live, headline.ID) != 1 {
		t.Fatalf("steered discussion on home: %v", titlesOf(again.Live))
	}

	var ended apiError
	if code := rig.do(http.MethodPost, "/councils/"+pricing.ID+"/steer", map[string]any{"text": "too late"}, &ended); code != 409 || ended.Error != "That discussion has ended." {
		t.Fatalf("ended steer: %d %q", code, ended.Error)
	}
	var missing apiError
	if code := rig.do(http.MethodPost, "/councils/no-such/steer", map[string]any{"text": "hello"}, &missing); code != 404 || missing.Error != "That discussion doesn't exist." {
		t.Fatalf("missing steer: %d %q", code, missing.Error)
	}
	var empty apiError
	if code := rig.do(http.MethodPost, "/councils/"+banner.ID+"/steer", map[string]any{"text": "  "}, &empty); code != 400 || empty.Error != "A message is needed." {
		t.Fatalf("empty steer: %d %q", code, empty.Error)
	}

	dir := store.SessionDir(legal.ChatID)
	if err := os.WriteFile(filepath.Join(dir, teams.ConversationDeletedFile), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	hist = HistoryList{}
	if code := rig.do(http.MethodGet, "/history", nil, &hist); code != 200 || countHistory(hist.Items, legal.ChatID) != 0 {
		t.Fatalf("deleted council chat stayed in history: %d count=%d", code, countHistory(hist.Items, legal.ChatID))
	}
}

func TestHomeLiveWithoutACouncilStore(t *testing.T) {
	rig := newPlacesRig(t)
	id := rig.mk("Marketing")
	open := working(row("open1", "Publish draft", placesEpoch), "t-open", "Publish the draft", placesEpoch.Add(-time.Minute))
	open.Open = true
	rig.setRows(open)
	if code := rig.do(http.MethodPost, "/places/"+id+"/members", map[string]any{"chats": []string{"open1"}}, nil); code != 200 {
		t.Fatalf("file: %d", code)
	}
	home := getHome(t, rig, id)
	if home.Decided != 0 || len(home.Live) != 1 || home.Live[0].Kind != liveKindRunning || home.Live[0].Title != "Publish the draft" {
		t.Fatalf("home without discussions: decided=%d live=%v", home.Decided, titlesOf(home.Live))
	}
	_, body := rig.raw(http.MethodGet, "/places/"+id+"/home", nil)
	if strings.Contains(string(body), `"decided"`) {
		t.Fatalf("zero decided was sent: %s", body)
	}
}

func TestCouncilRoutesWithoutAStore(t *testing.T) {
	b := New(testToken, func(string) (Connection, error) {
		t.Fatal("an empty council list opened an engine")
		return Connection{}, nil
	})
	t.Cleanup(b.Close)
	w := request(b, http.MethodGet, "/api/engine/councils", "")
	if w.Code != http.StatusOK || strings.TrimSpace(w.Body.String()) != `{"councils":[]}` {
		t.Fatalf("list: %d %s", w.Code, w.Body.String())
	}
	w = request(b, http.MethodPost, "/api/engine/councils/cn_1/steer", "")
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "This discussion can't be steered from here.") {
		t.Fatalf("steer: %d %s", w.Code, w.Body.String())
	}
}

func padHex(n int) string {
	return fmt.Sprintf("%016x", n)
}

func mustBegin(t *testing.T, store *council.Store, a, b, topic string) council.Council {
	t.Helper()
	c, err := store.Begin(a, b, topic)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

type liveWant struct {
	kind, title, detail, task, council string
	turn, of                           int
}

func wantLive(t *testing.T, got []homeLiveRow, want ...liveWant) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("live has %d rows, want %d: %v", len(got), len(want), titlesOf(got))
	}
	for i, w := range want {
		row := got[i]
		turn, hasTurn := 0, false
		if row.Turn != nil {
			turn, hasTurn = *row.Turn, true
		}
		same := row.Kind == w.kind && row.Title == w.title && row.Detail == w.detail && row.TaskID == w.task && row.CouncilID == w.council && row.Of == w.of
		if w.kind == liveKindDiscussion {
			same = same && hasTurn && turn == w.turn
		}
		if !same {
			t.Fatalf("live[%d] = kind=%s title=%q detail=%q task=%s council=%s turn=%d present=%v of=%d, want %+v", i, row.Kind, row.Title, row.Detail, row.TaskID, row.CouncilID, turn, hasTurn, row.Of, w)
		}
	}
}

func liveHas(rows []homeLiveRow, councilID string) bool {
	return liveCount(rows, councilID) > 0
}

func liveCount(rows []homeLiveRow, councilID string) int {
	n := 0
	for _, row := range rows {
		if row.CouncilID == councilID {
			n++
		}
	}
	return n
}

func titlesOf(rows []homeLiveRow) []string {
	out := make([]string, len(rows))
	for i, row := range rows {
		out[i] = row.Kind + ":" + row.Title
		if row.Detail != "" {
			out[i] += " · " + row.Detail
		}
	}
	return out
}

func getHome(t *testing.T, rig *placesRig, id string) homeResponse {
	t.Helper()
	var home homeResponse
	if code := rig.do(http.MethodGet, "/places/"+id+"/home", nil, &home); code != 200 {
		t.Fatalf("GET /places/%s/home = %d", id, code)
	}
	return home
}

func (r *placesRig) raw(method, path string, body any) (int, []byte) {
	r.t.Helper()
	var buf json.RawMessage
	code := r.do(method, path, body, &buf)
	return code, buf
}

func countHistory(items []HistoryItem, id string) int {
	n := 0
	for _, item := range items {
		if item.ID == id {
			n++
		}
	}
	return n
}

func histTitle(items []HistoryItem, id string) string {
	for _, item := range items {
		if item.ID == id {
			return item.Title
		}
	}
	return ""
}
