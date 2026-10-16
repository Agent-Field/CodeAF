package main

// THE INDEPENDENT REVIEW'S REPRODUCTIONS of crews.create start_manager and
// crews.root.start (R1–R6, R8), kept as regressions. Each asserts the correct
// behaviour and each failed against the first cut (6a35c115e); R1 and R2 are
// re-aimed at the hazard itself — a second hand-over of one request's goal —
// rather than at where the first cut happened to keep its record.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

func reviewGoalSends(h *startHarness, goal string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, c := range h.convs {
		c.mu.Lock()
		for _, s := range c.texts {
			if s == goal {
				n++
			}
		}
		c.mu.Unlock()
	}
	return n
}

func reviewTurnDoneFor(agent string) func(string, any) bool {
	return func(event string, data any) bool {
		d, ok := data.(appwire.TurnDone)
		return ok && d.Agent == agent
	}
}

// R1: A FAILED READ IS NOT "NEW". The same request after its crew was renamed,
// with the teams file unreadable for the lookup, must not make a second crew or
// send the goal again; once the file reads again it is a repeat.
func TestReviewR1ReplayTreatsAReadFailureAsNew(t *testing.T) {
	h := newStartHarness(t)
	ask := appwire.CrewCreate{Name: "Docs", StartManager: true, ManagerStartsMembers: true, RequestID: "r-1", Goal: "ship the docs"}
	first, err := h.b.CrewStart(ask)
	if err != nil {
		t.Fatal(err)
	}
	h.rec.waitFor(t, 5*time.Second, reviewTurnDoneFor(first.Start.Manager))
	renamed := "Docs (renamed)"
	if _, err := h.b.CrewUpdate(appwire.CrewUpdate{ID: first.Crew.ID, Name: &renamed}); err != nil {
		t.Fatal(err)
	}
	path := teamstore.Path(h.b.profileDir)
	good, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// The teams file is unreadable for the repeat's look, and whole again by
	// the time anything would be minted (a transient read failure).
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	mint := h.b.mint
	h.b.mint = func(w string) (string, error) {
		_ = os.WriteFile(path, good, 0o600)
		return mint(w)
	}
	second, err2 := h.b.CrewStart(ask)
	if err2 == nil && !second.Start.Replayed {
		h.rec.waitFor(t, 5*time.Second, reviewTurnDoneFor(second.Start.Manager))
	}
	if err := os.WriteFile(path, good, 0o600); err != nil {
		t.Fatal(err)
	}
	teams, sends := len(h.file(t).Open()), reviewGoalSends(h, "ship the docs")
	if teams != 1 || sends != 1 || (err2 == nil && !second.Start.Replayed) {
		t.Fatalf("DEFECT R1: same request_id after a transient read failure: err=%v replayed=%v crews=%d goal sent %d times", err2, second.Start.Replayed, teams, sends)
	}
	third, err := h.b.CrewStart(ask)
	if err != nil || !third.Start.Replayed || third.Crew.ID != first.Crew.ID || third.Start.Manager != first.Start.Manager {
		t.Fatalf("once the file reads again the request is a repeat: %+v %v", third, err)
	}
}

// R2: A GOAL IS NEVER HANDED OVER WITHOUT A DURABLE RECORD OF ITS REQUEST. With
// the record unwritable, the start either refuses with nothing made, or what it
// sent is still answered as a repeat after the crew is renamed: the goal never
// reaches the model twice.
func TestReviewR2RecordWriteFailureStillLaunches(t *testing.T) {
	h := newStartHarness(t)
	_ = h.b.crewLayer().read(func(*appCrewLayerFile) {})
	stateDir := filepath.Dir(h.b.state.where())
	blocks := []string{h.b.crewLayer().path + ".tmp", filepath.Join(stateDir, "crew-starts")}
	if err := os.MkdirAll(filepath.Join(blocks[0], "x"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(blocks[1], []byte("not a folder"), 0o600); err != nil {
		t.Fatal(err)
	}
	ask := appwire.CrewCreate{Name: "Ops", StartManager: true, ManagerStartsMembers: true, RequestID: "r-2", Goal: "run ops"}
	first, err := h.b.CrewStart(ask)
	if err == nil {
		h.rec.waitFor(t, 5*time.Second, reviewTurnDoneFor(first.Start.Manager))
	} else if open, minted := len(h.file(t).Open()), h.folders(t); open != 0 || minted != 0 || reviewGoalSends(h, "run ops") != 0 {
		t.Fatalf("a refused start left %d crews and %d folders behind: %v", open, minted, err)
	}
	for _, block := range blocks {
		_ = os.RemoveAll(block)
	}
	if err == nil {
		renamed := "Ops (renamed)"
		if _, err := h.b.CrewUpdate(appwire.CrewUpdate{ID: first.Crew.ID, Name: &renamed}); err != nil {
			t.Fatal(err)
		}
	}
	second, err2 := h.b.CrewStart(ask)
	if err2 == nil && !second.Start.Replayed {
		h.rec.waitFor(t, 5*time.Second, reviewTurnDoneFor(second.Start.Manager))
	}
	if sends := reviewGoalSends(h, "run ops"); sends != 1 {
		t.Fatalf("DEFECT R2: first err=%v; repeat err=%v replayed=%v; goal sent %d times, open crews %d",
			err, err2, second.Start.Replayed, sends, len(h.file(t).Open()))
	}
}

// R3: A LIVE REQUEST IS NEVER ANSWERED "NEW". With orphaned records of the same
// request_id beside the live one, every lookup is a repeat (the first cut
// answered in map order, and some lookups said "new").
func TestReviewR3DuplicateRecordsMakeReplayNondeterministic(t *testing.T) {
	h := newStartHarness(t)
	ask := appwire.CrewCreate{Name: "Web", StartManager: true, ManagerStartsMembers: true, RequestID: "r-3", Goal: "build web"}
	got, err := h.b.CrewStart(ask)
	if err != nil {
		t.Fatal(err)
	}
	h.rec.waitFor(t, 5*time.Second, reviewTurnDoneFor(got.Start.Manager))
	print := appStartFingerprint(appwire.CrewCreate{Name: "Web", StartManager: true, ManagerStartsMembers: true, RequestID: "r-3", Goal: "build web"})
	_ = h.b.crewLayer().write(func(f *appCrewLayerFile) error {
		for i := 0; i < 8; i++ {
			f.crew("gone" + string(rune('a'+i))).Start = &appStartRecord{RequestID: "r-3", Fingerprint: print, Manager: "old", Goal: true}
		}
		return nil
	})
	missed := 0
	for i := 0; i < 64; i++ {
		_, done, _ := h.b.startReplay("r-3", print)
		if !done {
			missed++
		}
	}
	if missed > 0 {
		t.Fatalf("DEFECT R3: %d of 64 lookups of a live request answered 'new'", missed)
	}
}

// acceptedThenLost is a conversation whose first submit is taken (the words
// reach the engine) and then answered with an error, as a dropped link or a
// missing receipt does on the remote client.
type acceptedThenLost struct {
	*fakeConversation
	lost atomic.Bool
}

func (c *acceptedThenLost) Submit(ctx context.Context, text string) (<-chan session.Event, error) {
	events, err := c.fakeConversation.Submit(ctx, text)
	if err == nil && c.lost.CompareAndSwap(false, true) {
		go func() {
			for range events {
			}
		}()
		return nil, errors.New("engine: the engine took the message without saying how")
	}
	return events, err
}

func (c *acceptedThenLost) SubmitMessage(ctx context.Context, m session.Submission) (<-chan session.Event, error) {
	return c.Submit(ctx, m.Text)
}

// R4: A FIRST MESSAGE WHOSE HAND-OVER FAILED MAY HAVE BEEN SENT, and is not
// armed for agents.retry: the first cut armed it, and the retry was a SECOND
// send of the goal, without the client id.
func TestReviewR4UncertainSubmitErrorIsRetriedAsNeverSent(t *testing.T) {
	h := newStartHarness(t)
	inner := h.b.open
	h.b.open = func(ask appOpenAsk) (*appConn, error) {
		conn, err := inner(ask)
		if err != nil {
			return nil, err
		}
		conn.Conv = &acceptedThenLost{fakeConversation: conn.Conv.(*fakeConversation)}
		return conn, nil
	}
	got, err := h.b.CrewStart(appwire.CrewCreate{Name: "Lost", StartManager: true, ManagerStartsMembers: true, RequestID: "r-4", Goal: "only once", ClientID: "c-4"})
	if err != nil {
		t.Fatal(err)
	}
	h.rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
		d, ok := data.(appwire.TurnDone)
		return ok && !d.OK && d.Agent == got.Start.Manager
	})
	_, rerr := h.b.Retry(got.Start.Manager)
	if rerr == nil {
		h.rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
			d, ok := data.(appwire.TurnDone)
			return ok && d.OK && d.Agent == got.Start.Manager
		})
	}
	if n := reviewGoalSends(h, "only once"); n > 1 {
		t.Fatalf("DEFECT R4: an uncertain first-goal error was armed as a retry; the goal reached the engine %d times (retry err=%v)", n, rerr)
	}
}

// R5: A PERMANENTLY DELETED ROOT MANAGER (tombstoned folder) is gone: root.start
// replaces it, and it leaves the root (the first cut answered it as existing).
func TestReviewR5RootStartCountsADeletedManagerAsPresent(t *testing.T) {
	h := newStartHarness(t)
	first, err := h.b.RootStart(appwire.RootStartAsk{})
	if err != nil {
		t.Fatal(err)
	}
	f := h.file(t)
	root, _ := f.Root()
	m, _ := root.Member(root.Manager)
	if err := os.WriteFile(filepath.Join(filepath.Dir(m.File), teamstore.ConversationDeletedFile), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := h.b.RootStart(appwire.RootStartAsk{})
	if err != nil {
		t.Logf("second start: %v", err)
		return
	}
	if !second.ManagerCreated && second.Manager == first.Manager {
		t.Fatalf("DEFECT R5: root.start answered the permanently deleted manager %s as existing", second.Manager)
	}
	// The deleted manager LEAVES the root: it is not kept as a member nobody
	// can open.
	root, _ = h.file(t).Root()
	if _, still := root.Member(m.Key); still || root.Manager == m.Key {
		t.Fatalf("the deleted manager %s is still in the root: %+v", m.Key, root.Members)
	}
}

// R6: ATTACHMENTS ARE READ ONCE, before anything is made. A file that goes
// away after that reading cannot turn into a refusal after the crew was
// committed (the first cut read it again, and left a crew with no manager agent
// and no record).
func TestReviewR6AttachmentVanishingAfterValidationFailsAfterCommit(t *testing.T) {
	h := newStartHarness(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "brief.txt")
	if err := os.WriteFile(file, []byte("brief"), 0o600); err != nil {
		t.Fatal(err)
	}
	mint := h.b.mint
	h.b.mint = func(w string) (string, error) {
		_ = os.Remove(file)
		return mint(w)
	}
	got, err := h.b.CrewStart(appwire.CrewCreate{Name: "Gone", StartManager: true, ManagerStartsMembers: true, RequestID: "r-6", Goal: "read it",
		Workspace: dir, Attachments: []appwire.AttachSource{{Kind: appwire.AttachFile, Path: file}}})
	open := len(h.file(t).Open())
	if err != nil && open == 1 && strings.Contains(err.Error(), "was made") {
		t.Fatalf("DEFECT R6 (post-commit refusal reachable without shutdown): %v; open crews %d", err, open)
	}
	if err == nil {
		h.rec.waitFor(t, 5*time.Second, reviewTurnDoneFor(got.Start.Manager))
		if sent := sentTexts(h.conv(got.Start.Manager)); len(sent) != 1 || sent[0] != "read it\n\nAttached file: "+file {
			t.Fatalf("the goal and its attachment, read once: %q", sent)
		}
	}
	if err != nil || open != 1 {
		t.Fatalf("a file that went away after it was read refused a made crew: %v; open crews %d", err, open)
	}
}

// R8: the real mint's folder holds only what appUnmint accepts, so a refused
// start's rollback really removes it.
func TestReviewR8RealMintIsUnmintable(t *testing.T) {
	root := resolvedTempDir(t)
	_, workspace := wakeEnvironment(t, root)
	transcript, err := appMint(workspace)
	if err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(filepath.Dir(transcript))
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	appUnmint(session.PlacesRoot(), transcript)
	if _, err := os.Stat(filepath.Dir(transcript)); err == nil {
		t.Fatalf("DEFECT R8: real minted folder %v survives appUnmint (places root %s, folder %s)", names, session.PlacesRoot(), filepath.Dir(transcript))
	}
	t.Logf("minted folder held %v and was taken back", names)
}
