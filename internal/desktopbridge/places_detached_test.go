package desktopbridge

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/placegraph"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

// closedRig is the desktop at start-up: saved conversations on disk, the
// Places door attached, and NO conversation tab open. Every attach the
// background door makes is recorded, and so is every window the bridge opens.
type closedRig struct {
	t       *testing.T
	b       *Bridge
	advice  *PlaceAdvice
	store   *placegraph.Store
	root    string
	agent   *askingAgent
	mu      sync.Mutex
	rows    []session.SessionRow
	attach  []string // files the background door was opened on
	closes  atomic.Int32
	opens   atomic.Int32 // windows opened through POST /sessions
	welcome func(file string) remote.Welcome
	refuse  error
}

func newClosedRig(t *testing.T) *closedRig {
	t.Helper()
	dir := t.TempDir()
	rig := &closedRig{t: t, root: filepath.Join(dir, "projects", "desk")}
	rig.agent = &askingAgent{fakeAgent: fakeAgent{events: make(chan session.Event, 1), model: Model}}
	rig.welcome = func(file string) remote.Welcome {
		return remote.Welcome{SessionFile: file, Workspace: "/desk", Model: Model, Persistent: true, PlaceAsk: true, Launch: &remote.LaunchShape{OneModel: true}}
	}
	store, err := placegraph.Open(placegraph.Options{Path: filepath.Join(dir, "places.json")})
	if err != nil {
		t.Fatal(err)
	}
	rig.store = store
	rig.b = New(testToken, func(string) (Connection, error) {
		rig.opens.Add(1)
		return Connection{}, errors.New("no window is opened in these tests")
	})
	places := NewPlaces(store)
	places.World = func() session.World {
		rig.mu.Lock()
		defer rig.mu.Unlock()
		return session.World{Projects: []session.Project{{Name: "desk", Sessions: append([]session.SessionRow(nil), rig.rows...)}}}
	}
	rig.b.UsePlaces(places)
	ledger, err := placegraph.OpenLedger(filepath.Join(dir, "places-ai.json"))
	if err != nil {
		t.Fatal(err)
	}
	rig.advice = &PlaceAdvice{Ledger: ledger, SettleWait: -1, IdleCheck: time.Hour, SharedWorkspace: "/desk",
		Detached: func(file string) (Connection, error) {
			rig.mu.Lock()
			rig.attach = append(rig.attach, file)
			refuse := rig.refuse
			rig.mu.Unlock()
			if refuse != nil {
				return Connection{}, refuse
			}
			return Connection{Agent: rig.agent, Welcome: rig.welcome(file), Close: func() { rig.closes.Add(1) }}, nil
		}}
	if err := rig.b.UsePlaceAdvice(rig.advice); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rig.b.Close)
	return rig
}

// saved writes one conversation the way the session does — a folder with a
// transcript and a meta.json — and lists it in the world. A nil recap is a
// conversation nobody has summarised; a non-nil meta replaces the written one.
func (r *closedRig) saved(id, title string, recap *session.ConversationRecap, at time.Time, workspace string) session.SessionRow {
	r.t.Helper()
	dir := filepath.Join(r.root, id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		r.t.Fatal(err)
	}
	transcript := filepath.Join(dir, "transcript.jsonl")
	if err := os.WriteFile(transcript, []byte(`{"type":"message","role":"user","content":"hi"}`+"\n"), 0o600); err != nil {
		r.t.Fatal(err)
	}
	if err := session.SaveMeta(dir, session.Meta{ID: id, Title: title, Workspace: workspace, Created: at, LastUserAt: at, Tokens: 100, Recap: recap}); err != nil {
		r.t.Fatal(err)
	}
	row := session.SessionRow{ID: id, Dir: dir, Transcript: transcript, Title: title, Workspace: workspace, At: at, Tokens: 100}
	r.mu.Lock()
	r.rows = append(r.rows, row)
	r.mu.Unlock()
	return row
}

func (r *closedRig) attaches() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.attach...)
}

func (r *closedRig) organize() ProposalsView {
	r.t.Helper()
	w := request(r.b, "POST", "/api/engine/places/proposals/organize", `{}`)
	if w.Code != 202 && w.Code != 200 {
		r.t.Fatalf("organize: %d %s", w.Code, w.Body.String())
	}
	r.waitQuiet()
	w = request(r.b, "GET", "/api/engine/places/proposals", "")
	var v ProposalsView
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		r.t.Fatal(err)
	}
	return v
}

func (r *closedRig) waitQuiet() {
	r.t.Helper()
	eventually(r.t, "the place advice going quiet", func() bool {
		r.advice.mu.Lock()
		defer r.advice.mu.Unlock()
		return len(r.advice.order) == 0 && !r.advice.organize && r.advice.busy == ""
	})
}

// The real garden chats, with their real model-written titles and recaps
// (internal/placegraph's testdata), saved as conversations on disk.
func (r *closedRig) savedRealChats() map[string]string {
	r.t.Helper()
	raw, err := os.ReadFile("../placegraph/testdata/real-chats-2026-10-09.json")
	if err != nil {
		r.t.Fatal(err)
	}
	var file struct {
		Chats []struct {
			Group, ChatID, Title, Line, Discussed string
		} `json:"chats"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		r.t.Fatal(err)
	}
	group := map[string]string{}
	at := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	for i, c := range file.Chats {
		r.saved(c.ChatID, c.Title, &session.ConversationRecap{Line: c.Line, Discussed: c.Discussed, Messages: 2, UpdatedAt: at}, at.Add(time.Duration(i)*time.Minute), "/desk")
		group[c.ChatID] = c.Group
	}
	return group
}

// gardenAnswer groups the garden chats, and finds nothing among what no rule
// grouped (the question that shows the rest says they are probably unrelated).
func gardenAnswer(_ context.Context, q placegraph.ModelRequest) (session.PlacesAnswer, error) {
	if strings.Contains(q.User, "probably unrelated") {
		return session.PlacesAnswer{Text: `{"belong": false, "chats": [], "use": "", "name": "", "under": "root", "confidence": 10}`, Model: adviceModel}, nil
	}
	return session.PlacesAnswer{Text: `{"belong": true, "chats": ["c1","c2","c3","c4","c5"], "use": "", "name": "Garden drip irrigation", "under": "root", "confidence": 93}`, Model: adviceModel}, nil
}

// THE START-UP CASE: no tab open, prior chats on disk. Home asks once, the
// background door attaches to ONE existing saved conversation as a reader, the
// model is asked about the real garden chats with their recaps in front of it,
// and an offer for exactly those five is waiting — without a window being
// opened and without a conversation being minted.
func TestHomeOffersAGroupAtStartUpWithNoConversationOpen(t *testing.T) {
	rig := newClosedRig(t)
	group := rig.savedRealChats()
	rig.agent.answer = gardenAnswer
	v := rig.organize()
	if len(v.Proposals) != 1 || v.Proposals[0].Kind != placegraph.ProposalCreate || len(v.Proposals[0].ChatIDs) != 5 {
		t.Fatalf("offers %+v", v.Proposals)
	}
	for _, id := range v.Proposals[0].ChatIDs {
		if group[id] != "garden" {
			t.Fatalf("a %s chat was offered as garden", group[id])
		}
	}
	// One question about the garden group, then one about what no rule grouped.
	if rig.agent.calls() != 2 || rig.agent.asked[0].Role != "placesuggest" || !strings.Contains(rig.agent.asked[1].User, "probably unrelated") {
		t.Fatalf("asked %d times", rig.agent.calls())
	}
	if !strings.Contains(rig.agent.asked[0].User, "Recommended pressure-compensating hard-water emitters") {
		t.Fatalf("the saved recaps were not the evidence:\n%s", rig.agent.asked[0].User)
	}
	if v.LastAsk == nil || v.LastAsk.Via != "saved" || v.LastAsk.Model != adviceModel || !v.LastAsk.Offered {
		t.Fatalf("last ask %+v", v.LastAsk)
	}
	attached := rig.attaches()
	newest := rig.rows[len(rig.rows)-1].Transcript
	if len(attached) != 1 || attached[0] != newest {
		t.Fatalf("attached %v, want the newest saved conversation %s", attached, newest)
	}
	if rig.opens.Load() != 0 {
		t.Fatal("a window was opened for a background ask")
	}
	if !v.Engine.Asks || v.Engine.Via != "saved" {
		t.Fatalf("engine %+v", v.Engine)
	}
}

// The reader prefers a conversation the engine host is already holding, so
// nothing has to be booted for it.
func TestTheReaderPrefersAConversationAlreadyHeld(t *testing.T) {
	rig := newClosedRig(t)
	rig.savedRealChats()
	rig.mu.Lock()
	rig.rows[2].Open = true
	held := rig.rows[2].Transcript
	rig.mu.Unlock()
	rig.agent.answer = gardenAnswer
	rig.organize()
	if got := rig.attaches(); len(got) != 1 || got[0] != held {
		t.Fatalf("attached %v, want the held %s", got, held)
	}
}

// AN EMPTY LIBRARY ASKS NOTHING AND OPENS NOTHING. There is nothing to
// organize and no conversation to ride.
func TestAnEmptyLibraryAsksNothing(t *testing.T) {
	rig := newClosedRig(t)
	v := rig.organize()
	if len(rig.attaches()) != 0 || rig.agent.calls() != 0 || len(v.Proposals) != 0 {
		t.Fatalf("attached %v, asked %d, offers %v", rig.attaches(), rig.agent.calls(), v.Proposals)
	}
	if v.Engine.Asks || v.Engine.Reason == "" {
		t.Fatalf("engine %+v", v.Engine)
	}
}

// A job the rules answer opens nothing: a group sharing a real folder is
// named after it without a model, so no reader is attached for it.
func TestAJobTheRulesAnswerAttachesNothing(t *testing.T) {
	rig := newClosedRig(t)
	at := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	for _, id := range []string{"a1", "a2", "a3", "a4", "a5"} {
		rig.saved(id, "notes "+id, nil, at, "/home/u/garden")
	}
	v := rig.organize()
	if len(v.Proposals) != 1 || len(rig.attaches()) != 0 || rig.agent.calls() != 0 {
		t.Fatalf("offers %v, attached %v, asked %d", v.Proposals, rig.attaches(), rig.agent.calls())
	}
}

// An existing terminal library supplies a reader even before the first
// desktop conversation; its launcher joins the saved chat's own host.
func TestTheReaderCanRideAnExistingTerminalConversation(t *testing.T) {
	rig := newClosedRig(t)
	project := t.TempDir()
	rig.saved("elsewhere", "Terminal work", nil, time.Now(), project)
	rig.saved("missing", "Unavailable remote project", nil, time.Now().Add(time.Minute), filepath.Join(project, "gone"))
	if file := rig.advice.backgroundFile(); file == "" {
		t.Fatal("existing terminal library has no reader")
	}
}

// An engine that answers with a DIFFERENT conversation — a mint — is closed
// and refused, nothing is asked, and the door is not tried again at once.
func TestAReaderThatOpenedAnotherConversationIsRefused(t *testing.T) {
	rig := newClosedRig(t)
	rig.savedRealChats()
	rig.welcome = func(string) remote.Welcome {
		return remote.Welcome{SessionFile: filepath.Join(rig.root, "minted", "transcript.jsonl"), PlaceAsk: true}
	}
	rig.agent.answer = gardenAnswer
	v := rig.organize()
	if rig.agent.calls() != 0 || rig.closes.Load() != 1 || len(v.Proposals) != 0 {
		t.Fatalf("asked %d, closed %d, offers %v", rig.agent.calls(), rig.closes.Load(), v.Proposals)
	}
	if v.Engine.Asks || !strings.Contains(v.Engine.Reason, "try again") {
		t.Fatalf("engine %+v", v.Engine)
	}
	if v.LastAsk == nil || v.LastAsk.Error == "" || v.LastAsk.Via != "saved" {
		t.Fatalf("last ask %+v", v.LastAsk)
	}
}

// ONE CACHED CONNECTION: a second job reuses it, it is closed when it sits
// idle, a failed ask drops it, and the bridge closing closes it.
func TestTheBackgroundConnectionIsCachedAndAlwaysClosed(t *testing.T) {
	rig := newClosedRig(t)
	rig.savedRealChats()
	calls := 0
	rig.agent.answer = func(ctx context.Context, q placegraph.ModelRequest) (session.PlacesAnswer, error) {
		calls++
		return session.PlacesAnswer{Text: `{"belong": false, "chats": [], "use": "", "name": "", "under": "root", "confidence": 10}`, Model: adviceModel}, nil
	}
	rig.organize()
	if len(rig.attaches()) != 1 || rig.closes.Load() != 0 {
		t.Fatalf("attached %v closed %d", rig.attaches(), rig.closes.Load())
	}
	// A failed ask on the cached door closes it.
	rig.advice.mu.Lock()
	door := rig.advice.bg
	rig.advice.mu.Unlock()
	ask := rig.advice.askThrough(func(context.Context) (placeAskDoor, error) { return door.door, nil }, "saved")
	rig.agent.answer = func(context.Context, placegraph.ModelRequest) (session.PlacesAnswer, error) {
		return session.PlacesAnswer{}, errors.New("engine went away")
	}
	if _, err := ask(context.Background(), placegraph.ModelRequest{Role: "placesuggest", System: "s", User: "u"}); err == nil {
		t.Fatal("the failure was swallowed")
	}
	if rig.closes.Load() != 1 {
		t.Fatalf("a failed reader was kept: closed %d", rig.closes.Load())
	}
	// A fresh one is attached on demand, and closed when the bridge closes.
	if _, err := rig.advice.detached(context.Background(), rig.advice.backgroundFile()); err != nil {
		t.Fatal(err)
	}
	rig.b.Close()
	if rig.closes.Load() != 2 {
		t.Fatalf("closing the bridge left the reader open: closed %d", rig.closes.Load())
	}
}

func TestAnIdleReaderIsClosed(t *testing.T) {
	rig := newClosedRig(t)
	rig.advice.DetachedIdle = 20 * time.Millisecond
	rig.savedRealChats()
	if _, err := rig.advice.detached(context.Background(), rig.advice.backgroundFile()); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the idle reader closing", func() bool { return rig.closes.Load() == 1 })
}

// A conversation whose meta.json is damaged is weighed on its title alone:
// no recap, no failure, and the rest of the library is still read.
func TestADamagedMetaIsNoRecapNotAFailure(t *testing.T) {
	rig := newClosedRig(t)
	at := time.Now()
	row := rig.saved("broken", "Tomato drip emitter cleaning", &session.ConversationRecap{Line: "x", Messages: 2}, at, "/desk")
	if err := os.WriteFile(filepath.Join(row.Dir, "meta.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	rig.saved("fine", "Raised bed drip tubing", &session.ConversationRecap{Line: "Recommended half-inch drip tubing.", Messages: 2}, at, "/desk")
	lib := rig.advice.library()
	byID := map[string]placegraph.ChatEvidence{}
	for _, e := range lib {
		byID[e.ChatID] = e
	}
	if byID["broken"].Summary != "" || byID["broken"].Title == "" || byID["broken"].Replies != 1 {
		t.Fatalf("damaged meta read as %+v", byID["broken"])
	}
	if !strings.HasPrefix(byID["fine"].Summary, "Recommended half-inch drip tubing.") {
		t.Fatalf("summary %q", byID["fine"].Summary)
	}
}

// Readiness comes from what the session saved: a recap of one message (the
// person's own, nothing answered yet) proves no reply, and no recap is no
// summary — nothing is made up to fill either.
func TestReadinessAndSummaryAreNeverInvented(t *testing.T) {
	rig := newClosedRig(t)
	at := time.Now()
	row := rig.saved("asked", "Drip timer question", &session.ConversationRecap{Line: "The person asked about a timer.", Messages: 1}, at, "/desk")
	rig.mu.Lock()
	rig.rows[0].Tokens = 0
	rig.mu.Unlock()
	_ = row
	rig.saved("plain", "Drip emitter flush", nil, at, "/desk")
	byID := map[string]placegraph.ChatEvidence{}
	for _, e := range rig.advice.library() {
		byID[e.ChatID] = e
	}
	if byID["asked"].Replies != 0 {
		t.Fatalf("a one-message recap counted as a reply: %+v", byID["asked"])
	}
	if byID["plain"].Summary != "" {
		t.Fatalf("a summary was invented: %q", byID["plain"].Summary)
	}
	// A STALE recap — the person spoke after it — is still the conversation's
	// own account of what it covers, and is used.
	stale := rig.saved("stale", "Drip pressure", &session.ConversationRecap{Line: "Found a 25 psi regulator is needed.", Messages: 2, UpdatedAt: at.Add(-time.Hour)}, at, "/desk")
	_ = stale
	for _, e := range rig.advice.library() {
		if e.ChatID == "stale" && !strings.HasPrefix(e.Summary, "Found a 25 psi regulator") {
			t.Fatalf("stale recap dropped: %+v", e)
		}
	}
}

// Reading offers with no tab open is still free: GET never attaches a reader
// and never asks.
func TestReadingOffersWithNoTabOpenAttachesNothing(t *testing.T) {
	rig := newClosedRig(t)
	rig.savedRealChats()
	for i := 0; i < 3; i++ {
		if w := request(rig.b, "GET", "/api/engine/places/proposals", ""); w.Code != 200 {
			t.Fatalf("read: %d", w.Code)
		}
	}
	if len(rig.attaches()) != 0 || rig.agent.calls() != 0 {
		t.Fatalf("a read attached %v / asked %d", rig.attaches(), rig.agent.calls())
	}
}
