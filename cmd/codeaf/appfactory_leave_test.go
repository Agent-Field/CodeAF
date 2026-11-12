package main

import (
	"encoding/json"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
)

// ── an engine whose app has gone lets go of this machine's factory ──────────

// disconnect is the app going away: its end of the engine's stdin closes, as
// it does when the app's process is killed.
func (w *factoryWire) disconnect() {
	w.t.Helper()
	if err := w.in.(io.Closer).Close(); err != nil {
		w.t.Fatal(err)
	}
}

// localFactoryReleased waits until nothing serves this machine's factory at
// address: serve.json is gone and the port no longer answers.
func localFactoryReleased(t *testing.T, address string, within time.Duration) {
	t.Helper()
	u, err := url.Parse(address)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(within)
	for {
		_, statErr := os.Stat(filepath.Join(v3FactoryRoot(), "serve.json"))
		conn, dialErr := net.DialTimeout("tcp", u.Host, 100*time.Millisecond)
		if conn != nil {
			conn.Close()
		}
		if os.IsNotExist(statErr) && dialErr != nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("this machine's factory was still held %s after the app went (serve.json: %v, port answers: %v)", within, statErr, dialErr == nil)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// ownRow is the local factory's row in a list.
func ownRow(t *testing.T, list appwire.FactoryList) appwire.FactoryConnection {
	t.Helper()
	for _, c := range list.Factories {
		if c.ID == appLocalID {
			return c
		}
	}
	t.Fatalf("no local row in %+v", list.Factories)
	return appwire.FactoryConnection{}
}

// THE REGRESSION: the app was killed for a relaunch, its engine stayed alive,
// and the new engine could not serve this machine's factory. The old engine is
// in the middle of carrying a reply on (the carry-on road holds its work lock
// across opening a conversation and starting the reply, appclose.go) when its
// app goes; the factory is let go anyway, before Close waits on that reply, and
// the new engine's next list serves it again.
func TestAnEngineWhoseAppWentReleasesTheFactoryWhileAReplyIsCarried(t *testing.T) {
	s := newLocalSetup(t)
	if st := s.start(); st.Authority != appwire.LocalAuthorityApp {
		t.Fatalf("start: %+v", st)
	}
	address := s.backend.local.host.address

	// The app opens again while the first engine still holds the factory: the
	// new engine neither serves it a second time nor joins it.
	w, _ := reopened(t, s.backend.factoryHub())
	var list appwire.FactoryList
	w.must("factory.list", struct{}{}, &list)
	if w.backend.local.running || ownRow(t, list).State == appwire.FactoryOK {
		t.Fatalf("the new engine took a factory the old one still holds: %+v", list.Factories)
	}

	// A reply is being carried on in the old engine when its app goes.
	carrying := &s.backend.closeRecovery.work
	carrying.Lock()
	var once sync.Once
	carried := func() { once.Do(carrying.Unlock) }
	t.Cleanup(carried)
	s.disconnect()
	localFactoryReleased(t, address, 2*appLocalStopWait+5*time.Second)
	if !s.backend.isClosing() {
		t.Fatal("the factory went but the engine never started leaving")
	}

	// The carry-on finishes, and the old engine finishes leaving.
	carried()
	ended := make(chan struct{})
	go func() {
		for range s.answers {
		}
		close(ended)
	}()
	select {
	case <-ended:
	case <-time.After(appLeaveWait):
		t.Fatal("the old engine never finished leaving after its reply was let go")
	}

	// The same list, asked again, now serves the factory here.
	w.must("factory.list", struct{}{}, &list)
	own := ownRow(t, list)
	if own.State != appwire.FactoryOK || own.Reason != "" || !w.backend.local.running || w.backend.local.host == nil {
		t.Fatalf("the new engine did not take the factory back: %+v", own)
	}
}

// A HELD ELECTION SAYS WHY, AND A DEAD HOLDER LETS GO. Another live process
// named by serve.json and holding serve.lock keeps the resume refused, and
// the row says so in words a person can read. Once that process is gone the
// next list serves the factory here, with no reason left on the row.
func TestAHeldElectionGivesTheLocalRowItsReasonUntilTheHolderGoes(t *testing.T) {
	s := newLocalSetup(t)
	s.start()
	s.backend.Close()

	// Another codeaf: a live process of its own, its receipt and its lock.
	holder := exec.Command("/bin/sleep", "60")
	if err := holder.Start(); err != nil {
		t.Skipf("no process to stand in for another codeaf: %v", err)
	}
	reaped := false
	t.Cleanup(func() {
		if !reaped {
			_ = holder.Process.Kill()
			_ = holder.Wait()
		}
	})
	receipt, _ := json.Marshal(factoryDiscovery{URL: "http://127.0.0.1:1", PID: holder.Process.Pid})
	if err := os.WriteFile(filepath.Join(v3FactoryRoot(), "serve.json"), receipt, 0o600); err != nil {
		t.Fatal(err)
	}
	unlock, err := factoryFileLock(filepath.Join(v3FactoryRoot(), "serve.lock"), true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(unlock)
	wait := factoryLockWait
	factoryLockWait = 50 * time.Millisecond
	t.Cleanup(func() { factoryLockWait = wait })

	w, _ := reopened(t, s.backend.factoryHub())
	var list appwire.FactoryList
	w.must("factory.list", struct{}{}, &list)
	own := ownRow(t, list)
	if w.backend.local.running || own.State != appwire.FactoryUnreachable || own.Reason != appLocalHeldElsewhere {
		t.Fatalf("held election: running=%v row=%+v", w.backend.local.running, own)
	}

	// The other codeaf is gone: its process ended and its lock with it. Its
	// receipt is left behind naming a dead pid, which holds nothing.
	_ = holder.Process.Kill()
	_ = holder.Wait()
	reaped = true
	unlock()
	// A fresh answer, not decoded over the last: an absent reason must read as none.
	list = appwire.FactoryList{}
	w.must("factory.list", struct{}{}, &list)
	own = ownRow(t, list)
	if own.State != appwire.FactoryOK || own.Reason != "" || !w.backend.local.running {
		t.Fatalf("after the holder went: running=%v row=%+v", w.backend.local.running, own)
	}
}
