package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// startHarness is a backend whose every minted conversation is a real folder in
// a temp places root and whose every opened conversation is a scripted fake of
// its own, so what each manager was sent is read per conversation. No model, no
// network.
type startHarness struct {
	b      *appBackend
	rec    *appRecorder
	places string
	mu     sync.Mutex
	minted []string
	convs  map[string]*fakeConversation
}

func newStartHarness(t *testing.T) *startHarness {
	t.Helper()
	h := &startHarness{rec: newAppRecorder(), places: t.TempDir(), convs: map[string]*fakeConversation{}}
	h.b = newFakeBackend(h.rec, func(ask appOpenAsk) (*appConn, error) {
		h.mu.Lock()
		defer h.mu.Unlock()
		conv := h.convs[ask.Transcript]
		if conv == nil {
			conv = &fakeConversation{script: []session.Event{{Kind: session.EventTurnDone}}}
			h.convs[ask.Transcript] = conv
		}
		return &appConn{Conv: conv, Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() {}}, nil
	})
	t.Cleanup(func() { h.b.Close() })
	h.b.state = &appState{path: filepath.Join(t.TempDir(), appStateName)}
	h.b.profileDir = t.TempDir()
	h.b.mint = func(string) (string, error) {
		h.mu.Lock()
		defer h.mu.Unlock()
		dir := filepath.Join(h.places, "conv-"+strconv.Itoa(len(h.minted)+1))
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return "", err
		}
		if err := os.WriteFile(filepath.Join(dir, "meta.json"), []byte("{}"), 0o600); err != nil {
			return "", err
		}
		path := filepath.Join(dir, "transcript.jsonl")
		h.minted = append(h.minted, path)
		return path, nil
	}
	h.b.unmint = func(transcript string) { appUnmint(h.places, transcript) }
	return h
}

func (h *startHarness) mintedCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.minted)
}

func (h *startHarness) conv(agent string) *fakeConversation {
	h.mu.Lock()
	defer h.mu.Unlock()
	for path, c := range h.convs {
		if appAgentID(path) == agent {
			return c
		}
	}
	return nil
}

func (h *startHarness) folders(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir(h.places)
	if err != nil {
		t.Fatal(err)
	}
	return len(entries)
}

func (h *startHarness) file(t *testing.T) *teamstore.File {
	t.Helper()
	f, err := teamstore.Load(h.b.profileDir)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func startCode(t *testing.T, err error) string {
	t.Helper()
	var wire *appwire.Error
	if !errors.As(err, &wire) {
		t.Fatalf("want a wire error, got %v", err)
	}
	return wire.Code
}

// THE GOAL IS THE MANAGER'S FIRST MESSAGE, EXACTLY ONCE, with the person's own
// words, the client's id and the durable first entry as agents.create keeps
// them; the team holds only that manager.
func TestStartCrewSendsTheGoalOnceAsTheManagersFirstMessage(t *testing.T) {
	h := newStartHarness(t)
	said := "rebuild the docs site on Astro"
	got, err := h.b.CrewStart(appwire.CrewCreate{
		Name: "  Docs site ", StartManager: true, ManagerStartsMembers: true, RequestID: "r-1",
		Goal: "  Rebuild the docs site on Astro; keep every URL working.  ", Said: &said, ClientID: "c-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Crew.Name != "Docs site" || got.Start.RequestID != "r-1" || got.Start.Replayed ||
		got.Start.Goal.State != appwire.GoalStarting || got.Start.Goal.ClientID != "c-1" || got.Start.Manager == "" {
		t.Fatalf("the start = %+v", got)
	}
	if len(got.Crew.Members) != 1 || got.Crew.Manager != got.Start.Manager {
		t.Fatalf("a manager-only team holds one member, its manager: %+v", got.Crew)
	}
	user := entryWith(h.rec, t, func(e appwire.Entry) bool { return e.Kind == appwire.EntryUser })
	if user.Text != said || user.ClientID != "c-1" || user.ID != "u0" || user.Agent != got.Start.Manager {
		t.Fatalf("the first entry = %+v", user)
	}
	h.rec.waitFor(t, 5*time.Second, appTurnDone)
	conv := h.conv(got.Start.Manager)
	if sent := sentTexts(conv); len(sent) != 1 || sent[0] != "Rebuild the docs site on Astro; keep every URL working." {
		t.Fatalf("the manager was sent %q", sent)
	}
	if h.mintedCount() != 1 {
		t.Fatalf("one manager conversation, not %d", h.mintedCount())
	}
	// The same request again is a lookup: nothing minted, nothing sent.
	again, err := h.b.CrewStart(appwire.CrewCreate{
		Name: "Docs site", StartManager: true, ManagerStartsMembers: true, RequestID: "r-1",
		Goal: "Rebuild the docs site on Astro; keep every URL working.", Said: &said, ClientID: "c-1",
	})
	if err != nil || !again.Start.Replayed || again.Start.Goal.State != appwire.GoalUnknown || again.Crew.ID != got.Crew.ID || again.Start.Manager != got.Start.Manager {
		t.Fatalf("the repeat = %+v %v", again, err)
	}
	if h.mintedCount() != 1 || len(sentTexts(conv)) != 1 {
		t.Fatalf("a repeat executed again: minted %d, sent %q", h.mintedCount(), sentTexts(conv))
	}
	if _, err := h.b.CrewStart(appwire.CrewCreate{Name: "Another", StartManager: true, ManagerStartsMembers: true, RequestID: "r-1", Goal: "x"}); startCode(t, err) != appwire.CodeBadParams {
		t.Fatalf("a key used for a different team: %v", err)
	}
	if n := len(h.file(t).Teams); n != 1 {
		t.Fatalf("%d teams", n)
	}
}

// An empty goal makes the team and its manager and sends nothing.
func TestStartCrewWithoutAGoalSendsNothing(t *testing.T) {
	h := newStartHarness(t)
	got, err := h.b.CrewStart(appwire.CrewCreate{Name: "quiet", StartManager: true, ManagerStartsMembers: true, RequestID: "r-q"})
	if err != nil || got.Start.Goal.State != appwire.GoalNone || got.Start.Goal.ClientID != "" {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := mustCreateOpen(t, h, got.Start.Manager); err != nil {
		t.Fatal(err)
	}
	if conv := h.conv(got.Start.Manager); conv == nil || len(sentTexts(conv)) != 0 {
		t.Fatalf("a manager with no goal was sent %q", sentTexts(conv))
	}
}

// mustCreateOpen waits for a created agent's conversation to be held.
func mustCreateOpen(t *testing.T, h *startHarness, id string) (appwire.Agent, error) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		a, _ := h.b.find(id)
		if a != nil && h.b.liveOf(a) != nil {
			var view appwire.Agent
			h.b.locked(func() { view = a.view })
			return view, nil
		}
		if time.Now().After(deadline) {
			t.Fatalf("the manager's conversation never opened")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// PICKED AGENTS ARE MEMBERS AND THE NEW MANAGER JOINS THEM; an unknown agent, a
// duplicate and a named manager are refused and leave nothing behind.
func TestStartCrewPicksAgentsAndRefusesBadOnesWithoutLeaks(t *testing.T) {
	h := newStartHarness(t)
	picked, err := mustCreate(t, h.b, appwire.CreateAsk{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	before := h.folders(t)
	for name, ask := range map[string]appwire.CrewCreate{
		"unknown":   {Name: "a", StartManager: true, RequestID: "r-u", Agents: []string{"no-such-agent"}},
		"duplicate": {Name: "b", StartManager: true, RequestID: "r-d", Agents: []string{picked.ID, picked.ID}},
		"manager":   {Name: "c", StartManager: true, RequestID: "r-m", Agents: []string{picked.ID}, Manager: picked.ID},
		"both":      {Name: "d", StartManager: true, RequestID: "r-b", Agents: []string{picked.ID}, ManagerStartsMembers: true},
		"none":      {Name: "e", StartManager: true, RequestID: "r-n"},
		"no key":    {Name: "f", StartManager: true, ManagerStartsMembers: true},
		"stray":     {Name: "g", ManagerStartsMembers: true, Agents: []string{picked.ID}},
	} {
		if _, err := startViaWire(h.b, ask); err == nil {
			t.Fatalf("%s was not refused", name)
		}
	}
	if h.folders(t) != before || len(h.file(t).Teams) != 0 {
		t.Fatalf("a refused start left folders %d->%d, teams %d", before, h.folders(t), len(h.file(t).Teams))
	}
	got, err := h.b.CrewStart(appwire.CrewCreate{Name: "mixed", StartManager: true, RequestID: "r-ok", Agents: []string{picked.ID}, Goal: "plan it"})
	if err != nil || len(got.Crew.Members) != 2 || got.Crew.Manager != got.Start.Manager {
		t.Fatalf("%+v %v", got, err)
	}
}

// startViaWire is the shape check the wire does, then the engine.
func startViaWire(b *appBackend, ask appwire.CrewCreate) (appwire.CrewStartResult, error) {
	if err := ask.CheckStart(); err != nil {
		return appwire.CrewStartResult{}, err
	}
	return b.CrewStart(ask)
}

// A REFUSED COMMIT TAKES THE MINTED FOLDER BACK: a name already in use leaves no
// second team, no second agent and no folder.
func TestStartCrewRefusedNameRollsBackTheMint(t *testing.T) {
	h := newStartHarness(t)
	if _, err := h.b.CrewStart(appwire.CrewCreate{Name: "dup", StartManager: true, ManagerStartsMembers: true, RequestID: "r-1"}); err != nil {
		t.Fatal(err)
	}
	folders := h.folders(t)
	_, err := h.b.CrewStart(appwire.CrewCreate{Name: "DUP", StartManager: true, ManagerStartsMembers: true, RequestID: "r-2", Goal: "never sent"})
	if startCode(t, err) != appwire.CodeBadParams {
		t.Fatalf("%v", err)
	}
	if h.folders(t) != folders || len(h.file(t).Teams) != 1 {
		t.Fatalf("the refusal leaked: folders %d->%d teams %d", folders, h.folders(t), len(h.file(t).Teams))
	}
	for _, c := range h.convs {
		if len(sentTexts(c)) != 0 {
			t.Fatalf("a refused goal reached a model: %q", sentTexts(c))
		}
	}
}

// A SHUTTING-DOWN ENGINE MAKES NOTHING.
func TestStartCrewWhileClosingMakesNothing(t *testing.T) {
	h := newStartHarness(t)
	h.b.locked(func() { h.b.closing = true })
	_, err := h.b.CrewStart(appwire.CrewCreate{Name: "late", StartManager: true, ManagerStartsMembers: true, RequestID: "r-c", Goal: "x"})
	if startCode(t, err) != appwire.CodeClosed || h.mintedCount() != 0 || len(h.file(t).Teams) != 0 {
		t.Fatalf("%v minted %d", err, h.mintedCount())
	}
	if _, err := h.b.RootStart(appwire.RootStartAsk{}); startCode(t, err) != appwire.CodeClosed || h.mintedCount() != 0 {
		t.Fatalf("%v", err)
	}
}

// A MISSING ATTACHMENT REFUSES THE WHOLE START before anything is made.
func TestStartCrewRefusesAMissingAttachmentBeforeMinting(t *testing.T) {
	h := newStartHarness(t)
	_, err := h.b.CrewStart(appwire.CrewCreate{
		Name: "files", StartManager: true, ManagerStartsMembers: true, RequestID: "r-f", Workspace: t.TempDir(),
		Attachments: []appwire.AttachSource{{Kind: appwire.AttachFile, Path: "nope.md"}},
	})
	if startCode(t, err) != appwire.CodeBadParams || h.mintedCount() != 0 || len(h.file(t).Teams) != 0 {
		t.Fatalf("%v minted %d", err, h.mintedCount())
	}
}

// ATTACHMENTS RIDE ON THE GOAL as agents.create carries them.
func TestStartCrewCarriesTypedAttachmentsOnTheFirstMessage(t *testing.T) {
	h := newStartHarness(t)
	work := t.TempDir()
	if err := os.WriteFile(filepath.Join(work, "brief.md"), []byte("# brief\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := h.b.CrewStart(appwire.CrewCreate{
		Name: "brief", StartManager: true, ManagerStartsMembers: true, RequestID: "r-a", Workspace: work, Goal: "read this",
		Attachments: []appwire.AttachSource{{Kind: appwire.AttachFile, Path: "brief.md"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	user := entryWith(h.rec, t, func(e appwire.Entry) bool { return e.Kind == appwire.EntryUser })
	if user.Text != "read this" || len(user.Attachments) != 1 || user.Attachments[0].Path != "brief.md" {
		t.Fatalf("%+v", user)
	}
	h.rec.waitFor(t, 5*time.Second, appTurnDone)
	if sent := sentTexts(h.conv(got.Start.Manager)); len(sent) != 1 || sent[0] != "read this\n\nAttached file: brief.md" {
		t.Fatalf("%q", sent)
	}
}

// ONE ROOT, EVER: the root is made only by the person's call, with one manager
// conversation, every later team under it, and a repeat made nothing.
func TestRootStartMakesOneRootAndAManagerAndAnExistingOneIsReturned(t *testing.T) {
	h := newStartHarness(t)
	if _, ok := h.file(t).Root(); ok {
		t.Fatal("a root before anyone asked")
	}
	first, err := h.b.CrewStart(appwire.CrewCreate{Name: "alpha", StartManager: true, ManagerStartsMembers: true, RequestID: "r-1"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := h.b.RootStart(appwire.RootStartAsk{})
	if err != nil || !got.RootCreated || !got.ManagerCreated || got.Manager == "" {
		t.Fatalf("%+v %v", got, err)
	}
	f := h.file(t)
	root, ok := f.Root()
	if !ok || root.Name != teamstore.RootName || root.Closed() || appAgentID(root.Manager) != got.Manager {
		t.Fatalf("root = %+v", root)
	}
	top, _ := f.Team(first.Crew.ID)
	if top.Parent != root.ID || len(f.TopManagers()) != 1 || appAgentID(f.TopManagers()[0].Key) != first.Start.Manager {
		t.Fatalf("the top-level team was not seated under the root: %+v", top)
	}
	// A team started later lands under the root with its manager seated.
	second, err := h.b.CrewStart(appwire.CrewCreate{Name: "beta", StartManager: true, ManagerStartsMembers: true, RequestID: "r-2"})
	if err != nil {
		t.Fatal(err)
	}
	f = h.file(t)
	if b, _ := f.Team(second.Crew.ID); b.Parent != root.ID || len(f.TopManagers()) != 2 {
		t.Fatalf("the later team: %+v", b)
	}
	minted := h.mintedCount()
	again, err := h.b.RootStart(appwire.RootStartAsk{})
	if err != nil || again.RootCreated || again.ManagerCreated || again.Manager != got.Manager || h.mintedCount() != minted {
		t.Fatalf("a repeat made something: %+v %v", again, err)
	}
	roots := 0
	for _, team := range h.file(t).Teams {
		if team.Root {
			roots++
		}
	}
	if roots != 1 {
		t.Fatalf("%d roots", roots)
	}
}

// A ROOT WHOSE MANAGER'S CONVERSATION IS GONE is given a new manager, not a new root.
func TestRootStartReplacesAMissingManagerWithoutASecondRoot(t *testing.T) {
	h := newStartHarness(t)
	first, err := h.b.RootStart(appwire.RootStartAsk{})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(h.places, "conv-1")); err != nil {
		t.Fatal(err)
	}
	second, err := h.b.RootStart(appwire.RootStartAsk{})
	if err != nil || second.RootCreated || !second.ManagerCreated || second.Manager == first.Manager || second.Crew.ID != first.Crew.ID {
		t.Fatalf("%+v %v", second, err)
	}
}

// CONCURRENT CREATION NEVER DUPLICATES A ROOT OR LEAKS A CONVERSATION: one call
// wins, the rest are refused busy or answered with what the winner made.
func TestRootStartConcurrentCallsMakeOneRootAndOneManager(t *testing.T) {
	h := newStartHarness(t)
	const callers = 24
	var wg sync.WaitGroup
	results := make([]appwire.RootStartResult, callers)
	errs := make([]error, callers)
	start := make(chan struct{})
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results[i], errs[i] = h.b.RootStart(appwire.RootStartAsk{})
		}()
	}
	close(start)
	wg.Wait()
	created := 0
	managers := map[string]bool{}
	for i := range results {
		switch {
		case errs[i] == nil:
			if results[i].ManagerCreated {
				created++
			}
			managers[results[i].Manager] = true
		case startCode(t, errs[i]) != appwire.CodeBusy:
			t.Fatalf("caller %d: %v", i, errs[i])
		}
	}
	roots := 0
	for _, team := range h.file(t).Teams {
		if team.Root {
			roots++
		}
	}
	if created != 1 || roots != 1 || len(managers) != 1 || h.mintedCount() != 1 || h.folders(t) != 1 {
		t.Fatalf("created %d roots %d managers %v minted %d folders %d", created, roots, managers, h.mintedCount(), h.folders(t))
	}
}

// CONCURRENT STARTS OF ONE REQUEST SEND THE GOAL AT MOST ONCE.
func TestStartCrewConcurrentSameRequestSendsTheGoalOnce(t *testing.T) {
	h := newStartHarness(t)
	const callers = 16
	var wg sync.WaitGroup
	start := make(chan struct{})
	var mu sync.Mutex
	managers := map[string]bool{}
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			got, err := h.b.CrewStart(appwire.CrewCreate{Name: "once", StartManager: true, ManagerStartsMembers: true, RequestID: "r-same", Goal: "do it", ClientID: "c-same"})
			if err != nil {
				if c := startCode(t, err); c != appwire.CodeBusy {
					t.Errorf("%v", err)
				}
				return
			}
			mu.Lock()
			managers[got.Start.Manager] = true
			mu.Unlock()
		}()
	}
	close(start)
	wg.Wait()
	if len(managers) != 1 || h.mintedCount() != 1 || len(h.file(t).Teams) != 1 {
		t.Fatalf("managers %v minted %d teams %d", managers, h.mintedCount(), len(h.file(t).Teams))
	}
	for m := range managers {
		h.rec.waitFor(t, 5*time.Second, appTurnDone)
		if sent := sentTexts(h.conv(m)); len(sent) != 1 || sent[0] != "do it" {
			t.Fatalf("%q", sent)
		}
	}
}

// A CONVERSATION THAT CANNOT OPEN KEEPS ITS TEAM and an honest failed turn the
// existing retry re-sends once; the team is not torn down.
func TestStartCrewBootFailureLeavesTheTeamAndARetryableTurn(t *testing.T) {
	h := newStartHarness(t)
	// Every open fails until the failure has been heard, so the crew's own
	// holding of its members cannot take the boot's place.
	var healed atomic.Bool
	inner := h.b.open
	h.b.open = func(ask appOpenAsk) (*appConn, error) {
		if !healed.Load() {
			return nil, errors.New("the profile could not be read")
		}
		return inner(ask)
	}
	got, err := h.b.CrewStart(appwire.CrewCreate{Name: "flaky", StartManager: true, ManagerStartsMembers: true, RequestID: "r-x", Goal: "start up"})
	if err != nil || got.Start.Goal.State != appwire.GoalStarting {
		t.Fatalf("%+v %v", got, err)
	}
	h.rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
		done, ok := data.(appwire.TurnDone)
		return ok && !done.OK && done.Agent == got.Start.Manager
	})
	if len(h.file(t).Teams) != 1 {
		t.Fatal("the team was torn down")
	}
	healed.Store(true)
	if _, err := h.b.Retry(got.Start.Manager); err != nil {
		t.Fatal(err)
	}
	h.rec.waitFor(t, 5*time.Second, appTurnDone)
	if sent := sentTexts(h.conv(got.Start.Manager)); len(sent) != 1 || sent[0] != "start up" {
		t.Fatalf("%q", sent)
	}
}

// The wire gates: an engine that is not a starter refuses before anything is
// made, and start fields without the opt-in are refused, not ignored.
func TestCrewStartShapeIsCheckedBeforeTheEngine(t *testing.T) {
	cases := map[string]appwire.CrewCreate{
		"fields without opt-in": {Name: "a", RequestID: "r", Agents: []string{"x"}},
		"no name":               {StartManager: true, ManagerStartsMembers: true, RequestID: "r"},
		"long name":             {Name: strings.Repeat("n", appwire.CrewNameMax+1), StartManager: true, ManagerStartsMembers: true, RequestID: "r"},
		"long goal":             {Name: "a", StartManager: true, ManagerStartsMembers: true, RequestID: "r", Goal: strings.Repeat("g", appwire.CrewGoalMax+1)},
		"too many":              {Name: "a", StartManager: true, RequestID: "r", Agents: make([]string, appwire.CrewStartMembersMax+1)},
		"said without goal":     {Name: "a", StartManager: true, ManagerStartsMembers: true, RequestID: "r", ClientID: "c"},
		"control key":           {Name: "a", StartManager: true, ManagerStartsMembers: true, RequestID: "r\n"},
	}
	for name, ask := range cases {
		if err := ask.CheckStart(); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

// ── the durable claim, uncertainty and exact words (after review R1–R6) ──────

// startClaimOf reads a request's claim as the engine would.
func startClaimOf(t *testing.T, h *startHarness, request string) (appStartClaim, bool) {
	t.Helper()
	claim, found, err := h.b.startClaims().read(request)
	if err != nil {
		t.Fatal(err)
	}
	return claim, found
}

// THE CLAIM IS ON DISK BEFORE THE GOAL MOVES: by the time the manager's
// conversation is opened to be handed the goal, the request's claim already
// says committed and names the crew and the manager the answer names.
func TestStartGoalIsHandedOverOnlyUnderACommittedClaim(t *testing.T) {
	h := newStartHarness(t)
	var seen []string
	inner := h.b.open
	h.b.open = func(ask appOpenAsk) (*appConn, error) {
		claim, found, err := h.b.startClaims().read("r-c")
		seen = append(seen, claim.State)
		if err != nil || !found {
			t.Errorf("opened with no readable claim: %v", err)
		}
		return inner(ask)
	}
	got, err := h.b.CrewStart(appwire.CrewCreate{Name: "Claimed", StartManager: true, ManagerStartsMembers: true, RequestID: "r-c", Goal: "go", ClientID: "c-c"})
	if err != nil {
		t.Fatal(err)
	}
	h.rec.waitFor(t, 5*time.Second, reviewTurnDoneFor(got.Start.Manager))
	claim, _ := startClaimOf(t, h, "r-c")
	if len(seen) == 0 || seen[0] != appClaimCommitted || claim.Crew != got.Crew.ID || claim.Manager != got.Start.Manager || claim.ClientID != "c-c" || !claim.Goal {
		t.Fatalf("states at open %v, claim %+v, answer %+v", seen, claim, got)
	}
	// A refused start takes its claim back with its folder.
	if _, err := h.b.CrewStart(appwire.CrewCreate{Name: "Claimed", StartManager: true, ManagerStartsMembers: true, RequestID: "r-d", Goal: "go"}); startCode(t, err) != appwire.CodeBadParams {
		t.Fatalf("a taken name: %v", err)
	}
	if _, found := startClaimOf(t, h, "r-d"); found {
		t.Fatal("a refused start left its claim behind")
	}
}

// A CLAIM THAT CANNOT BE WRITTEN AS COMMITTED NEVER LETS THE GOAL GO: the team
// and its manager exist and are answered by their real ids, the goal is
// not_sent with why, nothing reaches the model, and a repeat says the same.
func TestStartCommitFailureAnswersNotSentAndSendsNothing(t *testing.T) {
	h := newStartHarness(t)
	h.b.commitStart = func(appStartClaims, appStartClaim) error { return errors.New("the disk is full") }
	got, err := h.b.CrewStart(appwire.CrewCreate{Name: "Held", StartManager: true, ManagerStartsMembers: true, RequestID: "r-n", Goal: "never twice", ClientID: "c-n"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Start.Goal.State != appwire.GoalNotSent || !strings.Contains(got.Start.Goal.Error, "the disk is full") ||
		got.Start.Goal.ClientID != "c-n" || got.Crew.Manager != got.Start.Manager || got.Start.Manager == "" {
		t.Fatalf("%+v", got)
	}
	// The manager is held as an ordinary idle conversation, opened with no words.
	h.rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
		d, ok := data.(appwire.AgentChanged)
		return ok && d.Agent.ID == got.Start.Manager && d.Agent.State == appwire.StateIdle && h.conv(got.Start.Manager) != nil
	})
	if n := reviewGoalSends(h, "never twice"); n != 0 {
		t.Fatalf("the goal was sent %d times", n)
	}
	h.b.commitStart = nil
	again, err := h.b.CrewStart(appwire.CrewCreate{Name: "Held", StartManager: true, ManagerStartsMembers: true, RequestID: "r-n", Goal: "never twice", ClientID: "c-n"})
	if err != nil || !again.Start.Replayed || again.Start.Goal.State != appwire.GoalNotSent || again.Crew.ID != got.Crew.ID {
		t.Fatalf("%+v %v", again, err)
	}
	if n := reviewGoalSends(h, "never twice"); n != 0 {
		t.Fatalf("the repeat sent the goal %d times", n)
	}
}

// EVERY CLAIM THAT CANNOT BE RESOLVED IS REFUSED, NEVER NEW: an unreadable one,
// one whose crew was never committed, one whose crew was deleted after its goal
// may have gone, and a link where the claims' folder should be. None mints.
func TestStartUnresolvableClaimsAreNeverNew(t *testing.T) {
	ask := func(id string) appwire.CrewCreate {
		return appwire.CrewCreate{Name: "X " + id, StartManager: true, ManagerStartsMembers: true, RequestID: id, Goal: "g"}
	}
	t.Run("unreadable", func(t *testing.T) {
		h := newStartHarness(t)
		dir := h.b.startClaims().dir
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, appClaimName("r-u")), []byte("{half"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := h.b.CrewStart(ask("r-u")); startCode(t, err) != appwire.CodeUncertain || h.mintedCount() != 0 {
			t.Fatalf("%v minted %d", err, h.mintedCount())
		}
	})
	t.Run("intent without its crew", func(t *testing.T) {
		h := newStartHarness(t)
		a := ask("r-i")
		a.Name, a.Goal = strings.TrimSpace(a.Name), strings.TrimSpace(a.Goal)
		made, err := h.b.startClaims().create(appStartClaim{V: appStartClaimV, RequestID: "r-i", Fingerprint: appStartFingerprint(a), State: appClaimIntent, Crew: "t-never", Manager: "m", ManagerKey: "/k", Goal: true})
		if err != nil || !made {
			t.Fatal(made, err)
		}
		if _, err := h.b.CrewStart(ask("r-i")); startCode(t, err) != appwire.CodeUncertain || h.mintedCount() != 0 {
			t.Fatalf("%v minted %d", err, h.mintedCount())
		}
	})
	t.Run("crew deleted after its goal", func(t *testing.T) {
		h := newStartHarness(t)
		got, err := h.b.CrewStart(ask("r-g"))
		if err != nil {
			t.Fatal(err)
		}
		h.rec.waitFor(t, 5*time.Second, reviewTurnDoneFor(got.Start.Manager))
		if err := h.b.CrewClose(got.Crew.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := h.b.CrewDelete(got.Crew.ID); err != nil {
			t.Fatal(err)
		}
		before := h.mintedCount()
		if _, err := h.b.CrewStart(ask("r-g")); startCode(t, err) != appwire.CodeBadParams || h.mintedCount() != before || reviewGoalSends(h, "g") != 1 {
			t.Fatalf("%v minted %d sent %d", err, h.mintedCount()-before, reviewGoalSends(h, "g"))
		}
	})
	t.Run("a link for the folder", func(t *testing.T) {
		h := newStartHarness(t)
		elsewhere := t.TempDir()
		if err := os.Symlink(elsewhere, h.b.startClaims().dir); err != nil {
			t.Fatal(err)
		}
		if _, err := h.b.CrewStart(ask("r-l")); err == nil {
			t.Fatal("a start was made through a link")
		}
		if entries, _ := os.ReadDir(elsewhere); len(entries) != 0 || len(h.file(t).Teams) != 0 || h.folders(t) != 0 {
			t.Fatalf("wrote %d entries through the link; teams %d folders %d", len(entries), len(h.file(t).Teams), h.folders(t))
		}
	})
}

// RECORDS THE FIRST CUT LEFT ON THE CREW LAYER are read, never written: one
// record of a live crew is a repeat whose goal is unknown, and two records of
// one request are refused every time — never answered "new" in map order.
func TestStartLegacyRecordsStayUnknownAndDuplicatesAreRefused(t *testing.T) {
	h := newStartHarness(t)
	agent, err := h.b.CreateAgent(appwire.CreateAsk{})
	if err != nil {
		t.Fatal(err)
	}
	crew, err := h.b.CrewCreate(appwire.CrewCreate{Name: "Old", Agents: []string{agent.ID}})
	if err != nil {
		t.Fatal(err)
	}
	minted := h.mintedCount()
	ask := appwire.CrewCreate{Name: "Old", StartManager: true, ManagerStartsMembers: true, RequestID: "r-old", Goal: "g"}
	print := appStartFingerprint(ask)
	_ = h.b.crewLayer().write(func(f *appCrewLayerFile) error {
		f.crew(crew.ID).Start = &appStartRecord{RequestID: "r-old", Fingerprint: print, Manager: agent.ID, Goal: true}
		return nil
	})
	got, err := h.b.CrewStart(ask)
	if err != nil || !got.Start.Replayed || got.Start.Goal.State != appwire.GoalUnknown || got.Crew.ID != crew.ID {
		t.Fatalf("%+v %v", got, err)
	}
	_ = h.b.crewLayer().write(func(f *appCrewLayerFile) error {
		f.crew("gone").Start = &appStartRecord{RequestID: "r-old", Fingerprint: print, Manager: "old", Goal: true}
		return nil
	})
	for i := 0; i < 32; i++ {
		if _, done, err := h.b.startReplay("r-old", print); !done || err == nil {
			t.Fatalf("lookup %d of two records: done=%v err=%v", i, done, err)
		}
	}
	if h.mintedCount() != minted {
		t.Fatal("a legacy request minted")
	}
}

// AN ACCEPTED-THEN-LOST FIRST MESSAGE IS SAID TO BE UNCERTAIN and never armed
// for a retry, and a repeat of the request says unknown, never sent twice.
func TestStartLostReceiptIsUncertainAndNeverRetried(t *testing.T) {
	h := newStartHarness(t)
	inner := h.b.open
	h.b.open = func(ask appOpenAsk) (*appConn, error) {
		conn, err := inner(ask)
		if err == nil {
			conn.Conv = &acceptedThenLost{fakeConversation: conn.Conv.(*fakeConversation)}
		}
		return conn, err
	}
	ask := appwire.CrewCreate{Name: "Lost", StartManager: true, ManagerStartsMembers: true, RequestID: "r-l", Goal: "only once", ClientID: "c-l"}
	got, err := h.b.CrewStart(ask)
	if err != nil {
		t.Fatal(err)
	}
	var done appwire.TurnDone
	h.rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
		d, ok := data.(appwire.TurnDone)
		if ok && d.Agent == got.Start.Manager {
			done = d
		}
		return ok && d.Agent == got.Start.Manager
	})
	if done.OK || done.ErrorKind != appwire.ErrorUncertain || !strings.Contains(done.Error, "may have been sent") {
		t.Fatalf("%+v", done)
	}
	if _, err := h.b.Retry(got.Start.Manager); startCode(t, err) != appwire.CodeNotFound {
		t.Fatalf("an uncertain first message was offered again: %v", err)
	}
	again, err := h.b.CrewStart(ask)
	if err != nil || !again.Start.Replayed || again.Start.Goal.State != appwire.GoalUnknown || again.Start.Goal.ClientID != "c-l" {
		t.Fatalf("%+v %v", again, err)
	}
	if n := reviewGoalSends(h, "only once"); n != 1 {
		t.Fatalf("the goal reached the engine %d times", n)
	}
}

// A FIRST MESSAGE THAT NEVER REACHED ITS CONVERSATION IS RETRIED WITH ITS OWN
// CLIENT ID; a retry whose own hand-over is lost answers uncertain and is not
// offered a second time.
func TestRetryOfAnUnsentFirstMessageKeepsItsClientIDAndAnUnsureRetryIsNotRepeated(t *testing.T) {
	h := newStartHarness(t)
	var healed atomic.Bool
	var seen []string
	inner := h.b.open
	h.b.open = func(ask appOpenAsk) (*appConn, error) {
		if !healed.Load() {
			return nil, errors.New("the profile could not be read")
		}
		conn, err := inner(ask)
		if err == nil {
			conn.Conv = &clientIDsSeen{acceptedThenLost: &acceptedThenLost{fakeConversation: conn.Conv.(*fakeConversation)}, seen: &seen}
		}
		return conn, err
	}
	got, err := h.b.CrewStart(appwire.CrewCreate{Name: "Twice", StartManager: true, ManagerStartsMembers: true, RequestID: "r-t", Goal: "once", ClientID: "c-t"})
	if err != nil {
		t.Fatal(err)
	}
	h.rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
		d, ok := data.(appwire.TurnDone)
		return ok && !d.OK && d.Agent == got.Start.Manager && d.ErrorKind == appwire.ErrorOther
	})
	var client string
	if a, err := h.b.find(got.Start.Manager); err == nil {
		h.b.locked(func() { client = a.failedClient })
	}
	if client != "c-t" {
		t.Fatalf("the retry would carry %q", client)
	}
	healed.Store(true)
	if _, err := h.b.Retry(got.Start.Manager); startCode(t, err) != appwire.CodeUncertain {
		t.Fatalf("a lost retry: %v", err)
	}
	if _, err := h.b.Retry(got.Start.Manager); startCode(t, err) != appwire.CodeNotFound {
		t.Fatalf("an uncertain retry was offered again: %v", err)
	}
	if n := reviewGoalSends(h, "once"); n != 1 {
		t.Fatalf("the goal reached the engine %d times", n)
	}
	if len(seen) != 1 || seen[0] != "c-t" {
		t.Fatalf("the retry carried client ids %q", seen)
	}
}

// clientIDsSeen is a lost-receipt conversation that notes the client id each
// message was handed over with.
type clientIDsSeen struct {
	*acceptedThenLost
	seen *[]string
}

func (c *clientIDsSeen) SubmitMessage(ctx context.Context, m session.Submission) (<-chan session.Event, error) {
	c.fakeConversation.mu.Lock()
	*c.seen = append(*c.seen, m.ClientID)
	c.fakeConversation.mu.Unlock()
	return c.acceptedThenLost.SubmitMessage(ctx, m)
}

// THE PERSON'S WORDS ARE KEPT EXACTLY: said is not trimmed, and an empty said
// beside a goal is the empty entry it has always meant, not the goal.
func TestStartKeepsSaidExactlyAsAuthored(t *testing.T) {
	for _, said := range []string{"  please, ship it\n", ""} {
		h := newStartHarness(t)
		got, err := h.b.CrewStart(appwire.CrewCreate{Name: "Words", StartManager: true, ManagerStartsMembers: true, RequestID: "r-w", Goal: "Ship the release.", Said: &said})
		if err != nil {
			t.Fatal(err)
		}
		var user appwire.Entry
		h.rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
			d, ok := data.(appwire.EntryAdded)
			if ok && d.Entry.Kind == appwire.EntryUser && d.Entry.Agent == got.Start.Manager {
				user = d.Entry
				return true
			}
			return false
		})
		if user.Text != said {
			t.Fatalf("said %q, entry %q", said, user.Text)
		}
		if sent := sentTexts(h.conv(got.Start.Manager)); len(sent) != 1 || sent[0] != "Ship the release." {
			t.Fatalf("%q", sent)
		}
	}
}

// A ROOT MANAGER WHOSE CONVERSATION CANNOT BE CHECKED IS NOT REPLACED ON A
// GUESS: the start refuses and nothing is minted.
func TestRootStartDoesNotReplaceAManagerItCannotCheck(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permissions do not bind root")
	}
	h := newStartHarness(t)
	first, err := h.b.RootStart(appwire.RootStartAsk{})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(h.places, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(h.places, 0o700) })
	minted := h.mintedCount()
	if _, err := h.b.RootStart(appwire.RootStartAsk{}); err == nil || h.mintedCount() != minted {
		t.Fatalf("err %v, minted %d more", err, h.mintedCount()-minted)
	}
	_ = os.Chmod(h.places, 0o700)
	again, err := h.b.RootStart(appwire.RootStartAsk{})
	if err != nil || again.ManagerCreated || again.Manager != first.Manager {
		t.Fatalf("%+v %v", again, err)
	}
}

// C1 and C2: a real conversation (in-process engine, scripted model on loopback,
// fake key) keeps the person's said and client id on the persisted first line, and
// after a restart the same request is a repeat that mints nothing, opens no
// first turn and leaves exactly one person line in the record. A read-only
// crew open may still be finishing when the replay answers.
func TestStartRealJournalRepeatAfterRestartSendsNothing(t *testing.T) {
	root := resolvedTempDir(t)
	model, workspace := wakeEnvironment(t, root)
	rec := newAppRecorder()
	first := newAppBackend(rec, appOpenInProcess)
	closed := false
	defer func() {
		if !closed {
			first.Close()
		}
	}()
	said := "please rebuild the docs"
	ask := appwire.CrewCreate{Name: "Docs", StartManager: true, ManagerStartsMembers: true, RequestID: "r-7",
		Goal: "Rebuild the docs site; keep URLs.", Said: &said, ClientID: "c-7", Workspace: workspace}
	got, err := first.CrewStart(ask)
	if err != nil {
		t.Fatal(err)
	}
	rec.waitFor(t, 60*time.Second, reviewTurnDoneFor(got.Start.Manager))
	member, err := first.agentMember(got.Start.Manager)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	closed = true
	model.mu.Lock()
	requestsBeforeRestart := len(model.requests)
	model.mu.Unlock()
	if n := appUserCount(member.File); n != 1 {
		t.Fatalf("the record holds %d person lines", n)
	}
	second := newAppBackend(newAppRecorder(), appOpenInProcess)
	defer second.Close()
	minted := 0
	mint := second.mint
	second.mint = func(w string) (string, error) {
		minted++
		return mint(w)
	}
	entries, _, err := second.Entries(got.Start.Manager, "", appwire.MaxEntries)
	if err != nil {
		t.Fatal(err)
	}
	if users := appUsersOf(entries); len(users) != 1 || users[0].Text != said || users[0].ClientID != "c-7" {
		t.Fatalf("the reopened record's person lines: %+v", users)
	}
	again, err := second.CrewStart(ask)
	if err != nil || !again.Start.Replayed || again.Start.Goal.State != appwire.GoalUnknown ||
		again.Start.Manager != got.Start.Manager || again.Crew.ID != got.Crew.ID {
		t.Fatalf("the repeat after restart: %+v %v", again, err)
	}
	a, err := second.find(got.Start.Manager)
	if err != nil {
		t.Fatal(err)
	}
	// C2 permits a read-only open, including a crew hold admitted before its
	// goroutine takes the agent's gate. Wait for both to finish before asking
	// whether the repeat left a first turn booting, running or queued.
	run := second.crews()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var opening bool
		second.locked(func() { opening = a.opening })
		run.locked(func() { opening = opening || run.opening[got.Start.Manager] })
		if !opening {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the read-only conversation open did not settle within five seconds")
		}
		time.Sleep(time.Millisecond)
	}
	var booting, running bool
	var queued int
	second.locked(func() {
		booting = a.boot != nil || a.opening || a.starting
		running = a.live != nil && a.live.running
		queued = len(a.queue)
	})
	if minted != 0 || booting {
		t.Fatalf("the repeat minted %d and left a first turn booting: %v", minted, booting)
	}
	if turns := second.turns.Load(); turns != 0 || running || queued != 0 {
		t.Fatalf("the repeat started %d turns, running: %v, queued: %d", turns, running, queued)
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
	model.mu.Lock()
	newRequests := len(model.requests) - requestsBeforeRestart
	model.mu.Unlock()
	if newRequests != 0 {
		t.Fatalf("after the restart the model received %d new requests", newRequests)
	}
	if n := appUserCount(member.File); n != 1 {
		t.Fatalf("after the repeat the record holds %d person lines", n)
	}
}

// AN INTENT CLAIM WHOSE CREW EXISTS IS NOT PROOF OF AN UNSENT GOAL: the start
// holding it may be about to hand it over, in another process, or may have
// died before it could. A repeat says unknown and sends nothing.
func TestStartIntentClaimWithItsCrewIsAnUnknownRepeat(t *testing.T) {
	h := newStartHarness(t)
	agent, err := h.b.CreateAgent(appwire.CreateAsk{})
	if err != nil {
		t.Fatal(err)
	}
	crew, err := h.b.CrewCreate(appwire.CrewCreate{Name: "Elsewhere", Agents: []string{agent.ID}})
	if err != nil {
		t.Fatal(err)
	}
	ask := appwire.CrewCreate{Name: "Elsewhere", StartManager: true, ManagerStartsMembers: true, RequestID: "r-e", Goal: "g"}
	if made, err := h.b.startClaims().create(appStartClaim{V: appStartClaimV, RequestID: "r-e", Fingerprint: appStartFingerprint(ask),
		State: appClaimIntent, Crew: crew.ID, Manager: agent.ID, ManagerKey: "/k", Goal: true}); err != nil || !made {
		t.Fatal(made, err)
	}
	minted := h.mintedCount()
	got, err := h.b.CrewStart(ask)
	if err != nil || !got.Start.Replayed || got.Start.Goal.State != appwire.GoalUnknown || h.mintedCount() != minted {
		t.Fatalf("%+v %v minted %d", got, err, h.mintedCount()-minted)
	}
}

// A REQUEST CLAIMED ELSEWHERE BETWEEN THE LOOKUP AND THE CLAIM is the other
// start's: this one takes its folder back and answers from the other claim —
// uncertain while that start has made no crew — and never makes a second.
func TestStartLosingTheClaimToAnotherStartMakesNothing(t *testing.T) {
	h := newStartHarness(t)
	ask := appwire.CrewCreate{Name: "Raced", StartManager: true, ManagerStartsMembers: true, RequestID: "r-race", Goal: "g"}
	mint := h.b.mint
	h.b.mint = func(w string) (string, error) {
		a := ask
		a.Name, a.Goal = strings.TrimSpace(a.Name), strings.TrimSpace(a.Goal)
		if made, err := h.b.startClaims().create(appStartClaim{V: appStartClaimV, RequestID: "r-race", Fingerprint: appStartFingerprint(a),
			State: appClaimIntent, Crew: "t-other", Manager: "m", ManagerKey: "/k", Goal: true}); err != nil || !made {
			t.Errorf("the other start's claim: %v %v", made, err)
		}
		return mint(w)
	}
	if _, err := h.b.CrewStart(ask); startCode(t, err) != appwire.CodeUncertain {
		t.Fatalf("%v", err)
	}
	if len(h.file(t).Teams) != 0 || h.folders(t) != 0 || reviewGoalSends(h, "g") != 0 {
		t.Fatalf("teams %d folders %d", len(h.file(t).Teams), h.folders(t))
	}
}

// THE GOAL A CREW START HANDS ITS MANAGER STATES THAT CREW'S EXACT ID.
func TestStartCrewGoalStatesTheNewCrew(t *testing.T) {
	h := newStartHarness(t)
	got, err := h.b.CrewStart(appwire.CrewCreate{Name: "Docs", StartManager: true, ManagerStartsMembers: true, RequestID: "r-goal", Goal: "rebuild the docs"})
	if err != nil {
		t.Fatal(err)
	}
	h.rec.waitFor(t, 5*time.Second, appTurnDone)
	conv := h.conv(got.Start.Manager)
	conv.mu.Lock()
	submissions := append([]session.Submission(nil), conv.submissions...)
	conv.mu.Unlock()
	if len(submissions) != 1 || submissions[0].Creation == nil || submissions[0].Creation.Team != got.Crew.ID {
		t.Fatalf("the goal was handed as %+v, want the crew %s", submissions, got.Crew.ID)
	}
}
