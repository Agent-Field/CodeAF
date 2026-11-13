package syncsetup

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/dirwatch"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/relayserve"
)

const c31Chat = "01J000000000000000000C31A"

// The holder runs in another process so stopping it closes its sockets without
// releasing its chat or sending a watch goodbye.
func TestC31HolderProcessHelper(t *testing.T) {
	home := os.Getenv("CODEAF_TEST_C31_HOME")
	if home == "" {
		return
	}
	// The holder's home is prepared like every other test home, so the proof
	// does not depend on an engine binary being installed on this computer.
	celltest.Available(t)
	s, ok, err := Open(home)
	mustNil(t, err)
	if !ok {
		t.Fatal("holder home unavailable")
	}
	mustNil(t, s.putDevice(t.Context(), "holder"))
	hostedOwner(t, s)
	title, err := directory.SealTitle(directory.MetadataKey(s.Identity.CellKey()), "Take back the saved chat")
	mustNil(t, err)
	_, err = s.Dir.Create(t.Context(), c31Chat, directory.CellInit{Head: strings.Repeat("1", 64), Class: "chat", Title: title})
	mustNil(t, err)
	feed := s.Follow()
	c31AwaitFeed(t, feed, requestTimeout, func(st dirwatch.State) bool { return st.Up })
	fmt.Println("C31 holder ready")
	select {}
}

func c31AwaitFeed(t *testing.T, feed dirwatch.Follower, within time.Duration, accept func(dirwatch.State) bool) dirwatch.State {
	t.Helper()
	timer := time.NewTimer(within)
	defer timer.Stop()
	for {
		st := feed.State()
		if accept(st) {
			return st
		}
		select {
		case <-feed.Changes():
		case <-timer.C:
			t.Fatalf("Home feed did not reach the required presence within %s: %+v", within, feed.State())
		}
	}
}

func TestC31TakeBackOfferAfterHolderStopsWithoutGoodbye(t *testing.T) {
	svc := relayserve.New(relayserve.Config{Store: t.TempDir()})
	defer svc.Close()
	relay := httptest.NewServer(svc.Handler)
	defer relay.Close()
	c31TakeBack(t, relay.URL)
}

func TestC31HostedTakeBackOfferAfterHolderStopsWithoutGoodbye(t *testing.T) {
	c31TakeBack(t, hostedLearnerEndpoint(t))
}

// Home must offer the saved chat while its holder's lease is still valid. The
// public Home feed and listing are the same inputs the resume band reads.
func c31TakeBack(t *testing.T, endpoint string) {
	t.Helper()
	t.Setenv(config.ProfileDirEnv, "")
	homeA := machine(t, endpoint)
	id, err := identity.Load(homeA)
	mustNil(t, err)
	deviceA, err := identity.Device(homeA)
	mustNil(t, err)
	homeB := machine(t, endpoint)
	_, err = identity.Adopt(homeB, id, true)
	mustNil(t, err)
	child := exec.Command(os.Args[0], "-test.run=^TestC31HolderProcessHelper$", "-test.v", "-test.timeout=15m")
	child.Env = append(os.Environ(), "CODEAF_TEST_C31_HOME="+homeA)
	stdout, err := child.StdoutPipe()
	mustNil(t, err)
	child.Stderr = os.Stderr
	mustNil(t, child.Start())
	defer func() {
		if child.ProcessState == nil {
			_ = child.Process.Kill()
			_ = child.Wait()
		}
	}()
	ready := make(chan bool, 1)
	go func() {
		scan := bufio.NewScanner(stdout)
		for scan.Scan() {
			if scan.Text() == "C31 holder ready" {
				ready <- true
				return
			}
		}
		ready <- false
	}()
	select {
	case ok := <-ready:
		if !ok {
			t.Fatal("holder exited before opening Home")
		}
	case <-time.After(requestTimeout):
		t.Fatal("holder did not open Home")
	}
	b, ok, err := Open(homeB)
	mustNil(t, err)
	if !ok {
		t.Fatal("second Home unavailable")
	}
	mustNil(t, b.putDevice(t.Context(), "observer"))
	hostedOwner(t, b)
	feed := b.Follow()
	defer feed.Close()
	c31AwaitFeed(t, feed, requestTimeout, func(st dirwatch.State) bool { return st.Up && slices.Contains(st.Online, deviceA.ID()) })
	before, err := b.Dir.Cell(t.Context(), c31Chat)
	mustNil(t, err)
	if before.Cell.Lease.Device != deviceA.ID() || before.Cell.Lease.Expires <= time.Now().UnixMilli() {
		t.Fatalf("holder has no live lease: %+v", before)
	}
	t.Logf("C31 before SIGKILL: holder online, lease expires in %s", time.Until(time.UnixMilli(before.Cell.Lease.Expires)))
	started := time.Now()
	mustNil(t, child.Process.Kill())
	if err := child.Wait(); err == nil {
		t.Fatal("holder exited without SIGKILL")
	}
	st := c31AwaitFeed(t, feed, 30*time.Second, func(st dirwatch.State) bool { return st.Up && !slices.Contains(st.Online, deviceA.ID()) })
	elapsed := time.Since(started)
	rows, _, err := b.RowsAt(t.Context())
	mustNil(t, err)
	offered := false
	for _, row := range rows {
		if row.Cell == c31Chat {
			if row.Status != chatlist.Running || row.DeviceID != deviceA.ID() || row.DurableAgo > 24*time.Hour {
				t.Fatalf("resume row is not a recent held chat: %+v", row)
			}
			offered = st.Up && !slices.Contains(st.Online, row.DeviceID)
		}
	}
	if !offered {
		t.Fatalf("Home cannot offer the holder's chat: %+v, %+v", rows, st)
	}
	after, err := b.Dir.Cell(t.Context(), c31Chat)
	mustNil(t, err)
	if after.Cell.Lease != before.Cell.Lease || after.Cell.Lease.Expires <= time.Now().UnixMilli() {
		t.Fatalf("offline presence changed or outwaited ownership: before=%+v after=%+v", before, after)
	}
	_, err = b.Dir.Acquire(context.Background(), c31Chat, directory.AcquireOpts{})
	if !errors.Is(err, directory.ErrLeaseHeld) {
		t.Fatalf("offline hint authorized ordinary acquisition: %v", err)
	}
	t.Logf("C31 resume offer on B: true after %s, holder lease remains unexpired", elapsed)
}
