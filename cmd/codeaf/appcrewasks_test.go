package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// appMemberCrew is a crew the app holds whole: its manager is a created agent
// on one fake conversation, and its one other member, @verifier, a
// conversation on disk the crew opens on a fake of its own, whose woken turns
// the test hands over on turns.
type appMemberCrew struct {
	backend *appBackend
	rec     *appRecorder
	run     *appCrewRun
	crew    string
	lead    string
	member  string
	conv    *fakeConversation
	turns   chan remote.Following
	// refuse makes the member's conversation refuse to open, as one another
	// window holds does, and opens counts the tries.
	refuse atomic.Bool
	opens  atomic.Int32
	// noKey makes it refuse the way every open does before the window has
	// handed the keys over.
	noKey atomic.Bool
}

// appMemberRefusal is what opening a conversation another window holds says.
const appMemberRefusal = "this conversation is open in another codeaf window that the app cannot reach; close it there, or open it in that window"

func newAppMemberCrew(t *testing.T) *appMemberCrew {
	t.Helper()
	c := newAppMemberCrewRefusing(t, false)
	deadline := time.Now().Add(5 * time.Second)
	for !c.backend.isHeld(c.member) {
		if time.Now().After(deadline) {
			t.Fatal("the crew never held its member")
		}
		time.Sleep(time.Millisecond)
	}
	return c
}

// newAppMemberCrewRefusing is the crew, its look at the members begun, with
// the member's conversation refusing to open while refuse is set.
func newAppMemberCrewRefusing(t *testing.T, refuse bool) *appMemberCrew {
	t.Helper()
	return newAppMemberCrewWithRefusals(t, refuse, false)
}

// Both refusal flags are set before the first crew look can open a member.
func newAppMemberCrewWithRefusals(t *testing.T, refuse, noKey bool) *appMemberCrew {
	t.Helper()
	t.Setenv("CODEAF_HOME", filepath.Join(t.TempDir(), "state"))
	bucket, err := v3ProjectDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir := writeV3Session(t, bucket, "00000000000000c1", "check the findings", time.Now())
	transcript := session.Place{Dir: dir}.Transcript()

	c := &appMemberCrew{conv: &fakeConversation{}, turns: make(chan remote.Following, 1)}
	c.refuse.Store(refuse)
	c.noKey.Store(noKey)
	lead := &fakeConversation{}
	var mu sync.Mutex
	c.rec = newAppRecorder()
	c.backend = newFakeBackend(c.rec, func(ask appOpenAsk) (*appConn, error) {
		mu.Lock()
		defer mu.Unlock()
		if ask.Transcript == transcript {
			c.opens.Add(1)
			if c.noKey.Load() {
				return nil, appEngineError(config.ErrNoAPIKey)
			}
			if c.refuse.Load() {
				return nil, appwire.Errorf(appwire.CodeConflict, "%s", appMemberRefusal)
			}
			return &appConn{
				Conv: c.conv, Workspace: ask.Workspace, Transcript: transcript, Release: func() {},
				Follow: func() <-chan remote.Following { return c.turns },
			}, nil
		}
		return &appConn{Conv: lead, Workspace: ask.Workspace, Transcript: appFakeTranscript, Release: func() {}}, nil
	})
	t.Cleanup(func() { c.backend.Close() })
	c.backend.state = &appState{path: filepath.Join(t.TempDir(), appStateName)}
	c.backend.profileDir = t.TempDir()
	manager, err := mustCreate(t, c.backend, appwire.CreateAsk{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	c.lead, c.member, c.crew = manager.ID, appAgentID(transcript), teamstore.NewID()
	err = teamstore.Update(c.backend.profileDir, func(f *teamstore.File) error {
		f.Teams = append(f.Teams, teamstore.Team{ID: c.crew, Name: "Trudeau Watch", Made: time.Now()})
		if err := f.AddMember(c.crew, teamstore.Member{Key: appFakeTranscript, File: appFakeTranscript, Where: "/w", Word: "lead", Handle: "leftism"}); err != nil {
			return err
		}
		if err := f.AddMember(c.crew, teamstore.Member{Key: transcript, File: transcript, Where: "/w", Word: "Check the findings", Handle: "verifier", Started: true}); err != nil {
			return err
		}
		return f.SetManager(c.crew, appFakeTranscript)
	})
	if err != nil {
		t.Fatal(err)
	}
	// The crew's first look holds every member; the member's conversation opens
	// behind it.
	c.run = c.backend.crews()
	return c
}

// memberIs looks at the crew and waits for it to say its member, and the crew
// itself, in the states given.
func (c *appMemberCrew) memberIs(t *testing.T, member, crew string) appwire.CrewMember {
	t.Helper()
	c.backend.crewTick(c.run, false)
	i := c.rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
		changed, ok := data.(appwire.MemberChanged)
		return ok && changed.Member.Agent == c.member && changed.Member.State == member
	})
	c.rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
		changed, ok := data.(appwire.CrewChanged)
		return ok && changed.Crew.ID == c.crew && changed.Crew.State == crew
	})
	c.rec.mu.Lock()
	defer c.rec.mu.Unlock()
	return c.rec.data[i].(appwire.MemberChanged).Member
}

// A CREW MEMBER'S PERMISSION PROMPT CLIMBS TO THE PERSON. It never becomes a
// packet the manager could answer, so it is the member's own ask: it opens on
// the member, it is an Inbox item named by the crew and the member's handle,
// the member reads asking with the question as its line and the crew reads
// needs_you. Answered from the crew's card, which is the member's own answer
// door, it closes everywhere — the Inbox moves it to done — and leaves exactly
// one receipt, in the member's conversation.
func TestAppCrewMemberPermissionReachesThePersonAndClosesEverywhere(t *testing.T) {
	c := newAppMemberCrew(t)
	c.conv.mu.Lock()
	c.conv.onAnswer = func(answer session.Answer) {
		closed := session.Question{Kind: answer.Kind, ID: answer.ID}
		c.conv.raise(session.Event{Kind: session.EventQuestionAnswered, Question: &closed, Answer: &answer})
	}
	c.conv.mu.Unlock()

	q := session.Question{
		ID: 7, Kind: session.QuestionConsent, Ask: session.AskPermission, Head: "needs your ok to run bash",
		Options: session.AnswerOptions(session.QuestionConsent), Blocking: session.Blocking{Turn: true},
	}
	c.conv.raise(session.Event{Kind: session.EventQuestion, Question: &q})
	i := c.rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
		opened, ok := data.(appwire.AskOpened)
		return ok && opened.Ask.Agent == c.member
	})
	c.rec.mu.Lock()
	ask := c.rec.data[i].(appwire.AskOpened).Ask
	c.rec.mu.Unlock()
	if ask.Kind != appwire.AskApproval || ask.Subkind != appwire.SubkindPermission || len(ask.Options) == 0 {
		t.Fatalf("the member's ask = %+v", ask)
	}

	j := c.rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
		added, ok := data.(appwire.InboxAddedEvent)
		return ok && added.Item.Ask.Agent == c.member
	})
	c.rec.mu.Lock()
	item := c.rec.data[j].(appwire.InboxAddedEvent).Item
	c.rec.mu.Unlock()
	if item.Where.Crew != c.crew || strings.Join(item.Where.Path, " / ") != "Trudeau Watch / verifier" || item.Where.Agent != c.member {
		t.Fatalf("the Inbox item says it comes from %+v", item.Where)
	}

	row := c.memberIs(t, teamstore.StateAsking, appwire.CrewNeedsYou)
	if row.Line != "needs your ok to run bash" {
		t.Fatalf("the member's row = %+v", row)
	}

	if err := c.backend.AnswerAsk(c.member, ask.ID, appwire.OptionAllowOnce, ""); err != nil {
		t.Fatal(err)
	}
	c.rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
		resolved, ok := data.(appwire.InboxResolvedEvent)
		return ok && resolved.ID == item.ID && resolved.By == appwire.ClosedByApp && strings.HasPrefix(resolved.Receipt, "Allowed once · you · ")
	})
	c.memberIs(t, teamstore.StateIdle, appwire.CrewIdle)
	receipts := 0
	c.rec.mu.Lock()
	for _, data := range c.rec.data {
		if added, ok := data.(appwire.EntryAdded); ok && added.Entry.Kind == appwire.EntryReceipt && added.Entry.ID == appReceiptEntryID(ask.ID) {
			receipts++
			if added.Entry.Agent != c.member {
				t.Errorf("the receipt is in %s's conversation, not the member's", added.Entry.Agent)
			}
		}
	}
	c.rec.mu.Unlock()
	if receipts != 1 {
		t.Fatalf("%d receipts for one answer", receipts)
	}
	if waiting, counts, _ := c.backend.ListInbox(appwire.InboxWaiting, ""); len(waiting) != 0 || counts.Waiting != 0 || counts.DoneToday != 1 {
		t.Fatalf("after the answer the Inbox waits on %+v (%+v)", waiting, counts)
	}
}

// A MEMBER WOKEN WHILE ITS QUESTION WAITS STILL NEEDS THE PERSON. A member
// mostly runs turns its manager wakes, and a woken turn's question can reach
// the app before the turn does; adopting the turn must not put "running" over
// the question on the member's row and on its crew.
func TestAppWokenMemberWaitingOnThePersonStaysAsking(t *testing.T) {
	c := newAppMemberCrew(t)
	q := session.Question{
		ID: 3, Kind: session.QuestionTask, Ask: session.AskPermission, Head: session.TaskProposalLead + "reading for the chat",
		Options: session.AnswerOptions(session.QuestionTask), Blocking: session.Blocking{Turn: true},
	}
	c.conv.raise(session.Event{Kind: session.EventQuestion, Question: &q})
	c.rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
		changed, ok := data.(appwire.AgentChanged)
		return ok && changed.Agent.ID == c.member && changed.Agent.State == appwire.StateNeedsYou
	})

	events := make(chan session.Event, 4)
	defer close(events)
	c.turns <- remote.Following{Events: events}
	i := c.rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
		started, ok := data.(appwire.TurnStarted)
		return ok && started.Agent == c.member
	})
	c.rec.mu.Lock()
	var said *appwire.Agent
	for k := i - 1; k >= 0 && said == nil; k-- {
		if changed, ok := c.rec.data[k].(appwire.AgentChanged); ok && changed.Agent.ID == c.member {
			said = &changed.Agent
		}
	}
	c.rec.mu.Unlock()
	if said == nil || said.State != appwire.StateNeedsYou || said.Line != q.Head {
		t.Fatalf("the woken member was said as %+v", said)
	}
	row := c.memberIs(t, teamstore.StateAsking, appwire.CrewNeedsYou)
	if row.Line != q.Head {
		t.Fatalf("the member's row = %+v", row)
	}
}

// A MEMBER THE CREW CANNOT OPEN IS NEVER SILENT. Its conversation is held by a
// window the app cannot reach, so its questions could reach nobody: the first
// failure is said on the crew's next look — a line on stderr naming the member
// and the reason, a warn log event, and one notice in the manager's chat — and
// the member's row reads failed with the reason. Tried again, by the poll and
// when the crew is looked at, it says nothing more while it still fails; once
// the other window lets go it is held, its row is itself again, and the
// recovery is said once.
func TestAppCrewMemberThatCannotOpenIsSaidAndRecovers(t *testing.T) {
	var mu sync.Mutex
	var warned []string
	was := appCrewWarn
	appCrewWarn = func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		warned = append(warned, fmt.Sprintf(format, args...))
	}
	t.Cleanup(func() { appCrewWarn = was })

	c := newAppMemberCrewRefusing(t, true)
	deadline := time.Now().Add(5 * time.Second)
	for c.opens.Load() == 0 || func() bool {
		recorded := false
		c.run.locked(func() { recorded = c.run.unheld[c.member] != nil })
		return !recorded
	}() {
		if time.Now().After(deadline) {
			t.Fatal("the crew never tried its member")
		}
		time.Sleep(time.Millisecond)
	}
	c.rec.mu.Lock()
	for _, data := range c.rec.data {
		if _, ok := data.(appwire.Log); ok {
			t.Errorf("the crew's first look said %+v, ahead of the hello it runs before", data)
		}
	}
	c.rec.mu.Unlock()

	row := c.memberIs(t, teamstore.StateFailed, appwire.CrewIdle)
	if row.Line != appUnheldLead+appMemberRefusal {
		t.Fatalf("the member's row = %+v", row)
	}
	c.rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
		l, ok := data.(appwire.Log)
		return ok && l.Level == "warn" && strings.Contains(l.Message, "@verifier") && strings.Contains(l.Message, appMemberRefusal)
	})
	i := c.rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
		added, ok := data.(appwire.EntryAdded)
		return ok && added.Entry.Kind == appwire.EntryNotice && strings.HasPrefix(added.Entry.Text, "@verifier could not be opened")
	})
	c.rec.mu.Lock()
	notice := c.rec.data[i].(appwire.EntryAdded).Entry
	c.rec.mu.Unlock()
	if notice.Agent != c.lead || !strings.Contains(notice.Text, appMemberRefusal) || !strings.Contains(notice.Text, "Trudeau Watch") {
		t.Fatalf("the notice = %+v", notice)
	}
	mu.Lock()
	first := strings.Join(warned, "\n")
	mu.Unlock()
	if !strings.Contains(first, "@verifier") || !strings.Contains(first, c.member) || !strings.Contains(first, appMemberRefusal) {
		t.Fatalf("stderr said %q", first)
	}

	// Looking at the crew tries again at once; still refused, nothing more is said.
	tried := c.opens.Load()
	if _, _, err := c.backend.CrewLog(c.crew, "", 0); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for c.opens.Load() == tried {
		if time.Now().After(deadline) {
			t.Fatal("opening the crew did not try the member again")
		}
		time.Sleep(time.Millisecond)
	}
	c.backend.crewTick(c.run, false)

	// The other window lets go: the next try holds it, and the next look says so.
	c.refuse.Store(false)
	if _, _, err := c.backend.CrewLog(c.crew, "", 0); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for !c.backend.isHeld(c.member) {
		if time.Now().After(deadline) {
			t.Fatal("the member is not held after the other window let go")
		}
		time.Sleep(time.Millisecond)
	}
	c.memberIs(t, teamstore.StateIdle, appwire.CrewIdle)
	c.rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
		added, ok := data.(appwire.EntryAdded)
		return ok && added.Entry.Agent == c.lead && added.Entry.Text == "@verifier is open here again; its questions reach you now."
	})
	c.backend.crewTick(c.run, false)

	notices, warns, again := 0, 0, 0
	c.rec.mu.Lock()
	for _, data := range c.rec.data {
		if added, ok := data.(appwire.EntryAdded); ok {
			switch {
			case strings.HasPrefix(added.Entry.Text, "@verifier could not be opened"):
				notices++
			case strings.HasPrefix(added.Entry.Text, "@verifier is open here again"):
				again++
			}
		}
		if l, ok := data.(appwire.Log); ok && l.Level == "warn" {
			warns++
		}
	}
	c.rec.mu.Unlock()
	if notices != 1 || warns != 1 || again != 1 {
		t.Fatalf("said %d notices, %d warnings and %d recoveries for one run of failures", notices, warns, again)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(warned) != 2 || !strings.Contains(warned[1], "open again") {
		t.Fatalf("stderr said %q", warned)
	}
}

// A launch looks at the crews before the window has handed the keys over, so
// every member fails to open for want of one for a moment. That is not a member
// the person must do anything about: nothing is said, the row is not marked
// failed, the wait does not widen, and the member is held once the key is here,
// with no "open again" news for a failure nobody was told about.
func TestAppCrewMemberWaitingForTheKeyIsNotSaid(t *testing.T) {
	var mu sync.Mutex
	var warned []string
	was := appCrewWarn
	appCrewWarn = func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		warned = append(warned, fmt.Sprintf(format, args...))
	}
	t.Cleanup(func() { appCrewWarn = was })

	c := newAppMemberCrewWithRefusals(t, false, true)
	if _, _, err := c.backend.CrewLog(c.crew, "", 0); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		var u *appUnheld
		c.run.locked(func() {
			if got := c.run.unheld[c.member]; got != nil {
				copied := *got
				u = &copied
			}
		})
		if u != nil {
			if !u.quiet || u.tries != 0 {
				t.Fatalf("a hold refused for want of a key was recorded as a failure: %+v", *u)
			}
			if u.next.After(time.Now().Add(appCrewPoll + time.Second)) {
				t.Fatalf("a hold waiting for the key is tried again at %v, later than the next poll", u.next)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the crew never tried its member")
		}
		time.Sleep(time.Millisecond)
	}
	c.backend.crewTick(c.run, false)
	c.backend.crewTick(c.run, false)
	if line, unheld := c.backend.unheldLine(c.member); unheld {
		t.Fatalf("the member's row reads %q while it only waits for the key", line)
	}

	// The key arrives: the next try holds the member.
	c.noKey.Store(false)
	if _, _, err := c.backend.CrewLog(c.crew, "", 0); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for !c.backend.isHeld(c.member) {
		if time.Now().After(deadline) {
			t.Fatal("the member is not held once the key is here")
		}
		time.Sleep(time.Millisecond)
	}
	c.backend.crewTick(c.run, false)

	c.rec.mu.Lock()
	defer c.rec.mu.Unlock()
	for _, data := range c.rec.data {
		if added, ok := data.(appwire.EntryAdded); ok && strings.HasPrefix(added.Entry.Text, "@verifier") {
			t.Errorf("waiting for the key was said in the chat: %q", added.Entry.Text)
		}
		if l, ok := data.(appwire.Log); ok && strings.Contains(l.Message, "@verifier") {
			t.Errorf("waiting for the key was logged on the wire: %+v", l)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(warned) != 0 {
		t.Errorf("stderr said %q while the member only waited for the key", warned)
	}
}
