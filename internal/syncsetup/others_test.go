package syncsetup

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/session"
)

// ANOTHER COMPUTER IS KNOWN ONLY WHILE THE LAST COMPLETE ROSTER LISTS ONE: this
// computer alone, or beside one that was removed, knows of none; the answer is
// kept on disk for the next launch, and belongs to one pairing.
func TestOthersPairedFollowsTheLastCompleteRoster(t *testing.T) {
	home := t.TempDir()
	here := deviceBinding{relay: "https://relay.example", identity: "id-1", self: "this-mac"}
	known := func(b deviceBinding) bool {
		_, err := os.Stat(othersPath(home, b))
		return err == nil
	}
	if known(here) {
		t.Fatal("a computer that never read a roster knows of another")
	}
	rememberOthers(home, here, map[string]directory.Device{"this-mac": {}})
	if known(here) {
		t.Fatal("a roster of this computer alone names another")
	}
	rememberOthers(home, here, map[string]directory.Device{"this-mac": {}, "laptop": {}})
	if !known(here) {
		t.Fatal("a roster with a second computer names none")
	}
	if known(deviceBinding{relay: here.relay, identity: "id-2", self: here.self}) {
		t.Fatal("another pairing inherited this one's roster")
	}
	rememberOthers(home, here, map[string]directory.Device{"this-mac": {}, "laptop": {Revoked: true}})
	if known(here) {
		t.Fatal("a removed computer still counts as another")
	}
}

// A CHAT THAT COULD NOT BE SHARED IS SAID ONLY TO A PERSON WITH ANOTHER
// COMPUTER: with none known the pass fails, backs off and retries exactly as
// before, and says nothing; once a roster names a second computer, the next
// failure is said.
func TestAChatThatCouldNotBeSharedIsSaidOnlyWithAnotherComputer(t *testing.T) {
	var refuse atomic.Bool
	r := newDriveRig(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if refuse.Load() && req.Method == http.MethodPost && strings.HasPrefix(req.URL.Path, "/v1/dir/cells/") {
				http.Error(w, "unavailable", http.StatusInternalServerError)
				return
			}
			next.ServeHTTP(w, req)
		})
	})
	c, err := cell.CreateIn(filepath.Join(r.a.Home, "v3", "projects", "local"), cell.Options{Class: cell.FilesOnly})
	mustNil(t, err)
	work := filepath.Join(c.Root, "work")
	mustNil(t, os.MkdirAll(work, 0700))
	mustNil(t, session.SaveMeta(c.Root, session.Meta{ID: c.ID, Title: "kept here", Workspace: work, Owned: true, Created: time.Now()}))
	appendTo(t, transcriptOf(c), `{"type":"session","version":3,"id":"`+c.ID+`"}`+"\n")
	appendTo(t, transcriptOf(c), `{"type":"message","role":"user","content":"a saved message"}`+"\n")
	engine := cellstore.EngineFor(work)
	engine.DataRoot = filepath.Join(r.a.Home, "v3", "stores")
	_, err = engine.Seal(t.Context(), c, cellstore.TurnInfo{})
	mustNil(t, err)
	mustNil(t, cellstore.Await(t.Context(), c))

	at := time.Now()
	clock := func() time.Time { return at }
	var said []string
	say := func(_ cell.Cell, line string) { said = append(said, line) }
	refuse.Store(true)
	if err := r.a.PublishLocalChats(t.Context(), clock, say); err == nil {
		t.Fatal("the relay refused the chat and the pass reported no failure")
	}
	if len(said) != 0 {
		t.Fatalf("with no other computer the failure was said: %q", said)
	}
	if r.a.OthersPaired() {
		t.Fatal("this computer alone knows of another")
	}
	// The second computer joins the roster, and this one reads it.
	mustNil(t, r.b.Rename(t.Context(), "laptop"))
	if _, err := r.a.Dir.List(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !r.a.OthersPaired() {
		t.Fatal("a roster with a second computer was not remembered")
	}
	at = at.Add(2 * SharingRetryMax)
	if err := r.a.PublishLocalChats(t.Context(), clock, say); err == nil {
		t.Fatal("the retry reported no failure")
	}
	if len(said) != 1 || !strings.Contains(said[0], "could not be shared") {
		t.Fatalf("with another computer paired the failure reads %q", said)
	}
}
