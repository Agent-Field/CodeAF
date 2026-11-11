package ports

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"testing"
	"time"
)

func listen(t *testing.T, host string) (net.Listener, int) {
	t.Helper()
	l, err := net.Listen("tcp", net.JoinHostPort(host, "0"))
	if err != nil {
		t.Skipf("cannot listen on %s: %v", host, err)
	}
	t.Cleanup(func() { l.Close() })
	return l, l.Addr().(*net.TCPAddr).Port
}

func TestInspectRealOccupiedAndFreePort(t *testing.T) {
	c := New()
	l, port := listen(t, "0.0.0.0")
	got, err := c.Inspect(context.Background(), port)
	if err != nil || got.State != StateInUse || got.Port != port || got.CheckedAt.IsZero() {
		t.Fatalf("occupied: %+v %v", got, err)
	}
	if h := got.Holder; h != nil && (h.PID != os.Getpid() || h.Name == "") {
		t.Fatalf("holder named wrongly: %+v", h)
	}
	l.Close()
	got, err = c.Inspect(context.Background(), port)
	if err != nil || got.State != StateFree || got.Holder != nil {
		t.Fatalf("freed: %+v %v", got, err)
	}
}

func TestLoopbackOnlyListenerIsInUse(t *testing.T) {
	_, port := listen(t, "127.0.0.1")
	got, _ := New().Inspect(context.Background(), port)
	if got.State != StateInUse {
		t.Fatalf("loopback listener: %+v", got)
	}
}

func TestSuggestSkipsOccupiedNeighbours(t *testing.T) {
	a, pa := listen(t, "0.0.0.0")
	_ = a
	if pa+1 > MaxPort || pa < FirstSuggested {
		t.Skip("ephemeral port out of useful range")
	}
	b, err := net.Listen("tcp", net.JoinHostPort("0.0.0.0", strconv.Itoa(pa+1)))
	if err != nil {
		t.Skip("neighbour taken")
	}
	defer b.Close()
	got, err := New().Suggest(context.Background(), pa-1)
	if err != nil || got.Port < pa+2 || got.CheckedCount < 3 {
		t.Fatalf("suggest: %+v %v", got, err)
	}
	if again, _ := New().Inspect(context.Background(), got.Port); again.State == StateInUse {
		t.Fatalf("suggested port busy: %+v", again)
	}
}

func TestRangeNeverWraps(t *testing.T) {
	c := New()
	for _, p := range []int{0, -1, 65536} {
		if _, err := c.Inspect(context.Background(), p); !errors.Is(err, ErrInvalid) {
			t.Fatalf("inspect %d: %v", p, err)
		}
		if _, err := c.Suggest(context.Background(), p); !errors.Is(err, ErrInvalid) {
			t.Fatalf("suggest %d: %v", p, err)
		}
	}
	calls := 0
	c.bind = func(context.Context, string, string) error { calls++; return nil }
	if _, err := c.Suggest(context.Background(), MaxPort); !errors.Is(err, ErrNoCandidate) || calls != 0 {
		t.Fatalf("after 65535: %v, %d binds", err, calls)
	}
	var first int
	c.bind = func(_ context.Context, _, addr string) error {
		_, p, _ := net.SplitHostPort(addr)
		first, _ = strconv.Atoi(p)
		return nil
	}
	got, _ := c.Suggest(context.Background(), 80)
	if got.Port != FirstSuggested || first != FirstSuggested {
		t.Fatalf("low after: %+v", got)
	}
}

func fakeChecker(bind Binder) *Checker {
	c := New()
	c.bind = bind
	c.holder = nil
	return c
}

func TestUnknownIsNeverFree(t *testing.T) {
	c := fakeChecker(func(context.Context, string, string) error { return syscall.EACCES })
	got, _ := c.Inspect(context.Background(), 8080)
	if got.State != StateUnknown || got.Detail == "" {
		t.Fatalf("eacces: %+v", got)
	}
	if _, err := c.Suggest(context.Background(), 8080); !errors.Is(err, ErrNoCandidate) {
		t.Fatalf("refusal offered as free: %v", err)
	}
}

func TestIPv6AbsenceDoesNotOccupy(t *testing.T) {
	c := fakeChecker(func(_ context.Context, network, _ string) error {
		if network == "tcp6" {
			return syscall.EAFNOSUPPORT
		}
		return nil
	})
	if got, _ := c.Inspect(context.Background(), 8080); got.State != StateFree {
		t.Fatalf("%+v", got)
	}
}

func TestCandidateBusyContinuesAndBoundsHold(t *testing.T) {
	seen := map[string]bool{}
	c := fakeChecker(func(_ context.Context, _, addr string) error {
		_, p, _ := net.SplitHostPort(addr)
		seen[p] = true
		if p == "3000" || p == "3001" {
			return syscall.EADDRINUSE
		}
		return nil
	})
	got, err := c.Suggest(context.Background(), 2999)
	if err != nil || got.Port != 3002 || got.CheckedCount != 3 {
		t.Fatalf("%+v %v", got, err)
	}
	all := fakeChecker(func(context.Context, string, string) error { return syscall.EADDRINUSE })
	got, err = all.Suggest(context.Background(), 2000)
	if !errors.Is(err, ErrNoCandidate) || got.CheckedCount != MaxCandidates {
		t.Fatalf("bound: %+v %v", got, err)
	}
}

func TestCancelAndDeadlineAreNotFree(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	n := 0
	c := fakeChecker(func(context.Context, string, string) error {
		n++
		cancel()
		return syscall.EADDRINUSE
	})
	if _, err := c.Suggest(ctx, 4000); !errors.Is(err, context.Canceled) || n != 1 {
		t.Fatalf("cancel: %v after %d", err, n)
	}
	c = fakeChecker(func(ctx context.Context, _, _ string) error { <-ctx.Done(); return ctx.Err() })
	start := time.Now()
	got, _ := c.Inspect(context.Background(), 4000)
	if got.State != StateUnknown || time.Since(start) > 3*time.Second {
		t.Fatalf("deadline: %+v after %v", got, time.Since(start))
	}
}

func TestConcurrencyBudgetRefusesAndReleases(t *testing.T) {
	hold := make(chan struct{})
	c := fakeChecker(func(context.Context, string, string) error { <-hold; return nil })
	done := make(chan struct{}, Concurrency)
	for i := 0; i < Concurrency; i++ {
		go func() { c.Inspect(context.Background(), 5000); done <- struct{}{} }()
	}
	for len(c.slots) < Concurrency {
		time.Sleep(time.Millisecond)
	}
	if _, err := c.Inspect(context.Background(), 5000); !errors.Is(err, ErrBusy) {
		t.Fatalf("over budget: %v", err)
	}
	close(hold)
	for i := 0; i < Concurrency; i++ {
		<-done
	}
	if len(c.slots) != 0 {
		t.Fatalf("slots leaked: %d", len(c.slots))
	}
}

func TestProbeListenersAreAllClosed(t *testing.T) {
	_, port := listen(t, "0.0.0.0")
	c := New()
	for i := 0; i < 3; i++ {
		c.Inspect(context.Background(), port)
	}
	l, _ := net.Listen("tcp", ":0")
	l.Close()
	other, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	p := other.Addr().(*net.TCPAddr).Port
	other.Close()
	c.Inspect(context.Background(), p)
	again, err := net.Listen("tcp", ":"+strconv.Itoa(p))
	if err != nil {
		t.Fatalf("probe kept the port: %v", err)
	}
	again.Close()
}

func TestParseLsofFailsClosed(t *testing.T) {
	ok := "p42\ncnode\n"
	if h := ParseLsof([]byte(ok)); h == nil || h.PID != 42 || h.Name != "node" {
		t.Fatalf("%+v", h)
	}
	if h := ParseLsof([]byte("p42\ncnode\np42\ncnode\n")); h == nil || h.PID != 42 {
		t.Fatalf("same process on v4 and v6: %+v", h)
	}
	for name, in := range map[string]string{
		"empty": "", "truncated": "p42\ncno", "no name": "p42\n", "no pid": "cnode\n",
		"two": "p42\ncnode\np43\ncnode\n", "zero": "p0\ncnode\n", "negative": "p-4\ncx\n",
		"junk": "p42\ncnode\nxjunk\n", "word": "pabc\ncx\n", "blank line": "p42\n\ncnode\n",
		"control": "p42\nc\x1b[31m\n",
	} {
		if h := ParseLsof([]byte(in)); h != nil {
			t.Errorf("%s: held %+v", name, h)
		}
	}
	if h := ParseLsof([]byte("p9\nc/usr/local/bin/my server\n")); h == nil || h.Name != "my server" {
		t.Fatalf("name is a base name: %+v", h)
	}
}

func script(t *testing.T, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("unix helper")
	}
	p := filepath.Join(t.TempDir(), "helper.sh")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o700); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestHelperTimeoutDoesNotHangOrLeak(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "alive")
	p := script(t, "(/bin/sleep 1; echo x > "+marker+") &\n/bin/sleep 30\n")
	start := time.Now()
	_, err := runCapped(context.Background(), p, nil, 200*time.Millisecond, 1024)
	if err == nil || time.Since(start) > 2*time.Second {
		t.Fatalf("err %v after %v", err, time.Since(start))
	}
	time.Sleep(1500 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("a descendant of the helper outlived its cancellation")
	}
}

func TestHelperOutputOverCapFailsClosed(t *testing.T) {
	p := script(t, "/bin/yes pxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx\n")
	start := time.Now()
	out, err := runCapped(context.Background(), p, nil, 2*time.Second, 4096)
	if err == nil || out != nil || time.Since(start) > 1500*time.Millisecond {
		t.Fatalf("%d bytes, %v after %v", len(out), err, time.Since(start))
	}
}

func TestHelperFailureAndTruncationOmitHolder(t *testing.T) {
	c := New()
	c.bind = func(context.Context, string, string) error { return syscall.EADDRINUSE }
	c.holder = func(context.Context, int) *Holder { return nil }
	got, _ := c.Inspect(context.Background(), 8080)
	if got.State != StateInUse || got.Holder != nil {
		t.Fatalf("%+v", got)
	}
	c.holder = func(context.Context, int) *Holder { return &Holder{PID: 0, Name: "x"} }
	if got, _ = c.Inspect(context.Background(), 8080); got.Holder != nil {
		t.Fatalf("pid 0 reported: %+v", got.Holder)
	}
}

// A real lsof against a real listener of this very process. It proves the
// lookup on THIS platform only; it says nothing about another one's lsof.
func TestRealLsofNamesThisProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no lsof here")
	}
	_, port := listen(t, "0.0.0.0")
	h := lsofHolder(context.Background(), port)
	if h == nil {
		t.Skip("lsof absent or not permitted here")
	}
	if h.PID != os.Getpid() || h.Name == "" || filepath.Base(h.Name) != h.Name {
		t.Fatalf("holder %+v, want pid %d", h, os.Getpid())
	}
}
