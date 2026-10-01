//go:build e2e

package e2e

// A FRESH INSTALL IS QUIET. A person who has not paired and has not opened the
// add-machine card has asked for nothing from the sync service, so none of the
// first use may reach it: not the home screen's device list, not its watch
// socket, not the device record, not the sealing and upload of a chat that ran
// tools. The sync address here is a counting server; it counts every request
// and every socket upgrade, and the test fails on the first one.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
)

// tally is a sync service that counts what reaches it and answers nothing.
type tally struct {
	mu    sync.Mutex
	paths []string
}

func (c *tally) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c.mu.Lock()
	c.paths = append(c.paths, r.Method+" "+r.URL.Path+" upgrade="+r.Header.Get("Upgrade"))
	c.mu.Unlock()
	http.Error(w, "counted", http.StatusServiceUnavailable)
}

func (c *tally) seen() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.paths...)
}

func TestFreshInstallSendsNothingToTheSyncService(t *testing.T) {
	key := requireTmuxAndKey(t)
	count := &tally{}
	srv := httptest.NewServer(count)
	defer srv.Close()
	env := []string{config.APIKeyEnv + "=" + key, "CODEAF_SYNC_URL=" + srv.URL, "CODEAF_CELLS=1", "CODEAF_SYNC_INTERVAL_MS=1000"}

	home, ws := newHome(t, nil), newWorkspace(t, "proj", true)
	app := startWithEnv(t, env, "quiet", home, ws, 180, 45)
	app.skipSetup(t)
	app.keys("Space")
	app.keys("Space")
	if _, ok := waitPlain(app, 12*time.Second, "Add another machine"); !ok {
		t.Fatalf("the card is hidden on a fresh install:\n%s", plain(app))
	}
	time.Sleep(20 * time.Second) // home polls, device row, watch socket would all have fired

	app.lit("run the shell command: echo quiet-check > note.txt  then say done")
	app.keys("Enter")
	if _, ok := waitPlain(app, 120*time.Second, "done"); !ok {
		t.Logf("the chat did not finish in time:\n%s", plain(app))
	}
	time.Sleep(5 * time.Second)
	if got := count.seen(); len(got) != 0 {
		t.Fatalf("a fresh install reached the sync service %d times: %s", len(got), strings.Join(got, "; "))
	}

	app.kill()
	app = startWithEnv(t, env, "engaged", home, ws, 180, 45)
	app.skipSetup(t)
	app.keys("Space")
	app.keys("Space")
	if _, ok := waitPlain(app, 12*time.Second, "Add another machine"); !ok {
		t.Fatalf("the card is gone on the second launch:\n%s", plain(app))
	}
	if got := count.seen(); len(got) != 0 {
		t.Fatalf("a second quiet launch reached the sync service: %s", strings.Join(got, "; "))
	}
	app.keys("M-d")
	deadline := time.Now().Add(15 * time.Second)
	for len(count.seen()) == 0 && time.Now().Before(deadline) {
		time.Sleep(500 * time.Millisecond)
	}
	if len(count.seen()) == 0 {
		t.Fatal("opening the add-machine card never reached the sync service")
	}
}
