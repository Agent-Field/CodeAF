package pair

// Contract 18.R for the stream door: `codeaf serve` takes one attempt per code,
// asks a person before it lets a device in, and shows that person the words the
// device shows. These run a real relay and a real Host, as the end-to-end test
// does, and read what the machine says the way a person would: from its screen.

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/relay"
)

// screenLog is everything the machine has said, in order.
type screenLog struct {
	mu    sync.Mutex
	lines []string
}

func (s *screenLog) say(line string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lines = append(s.lines, line)
}

func (s *screenLog) all() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.lines...)
}

// until waits for the screen to satisfy a condition, and fails if it never does.
func (s *screenLog) until(t *testing.T, what string, ok func([]string) bool) []string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if lines := s.all(); ok(lines) {
			return lines
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("the machine never said %s; it said %q", what, s.all())
	return nil
}

var shownCode = regexp.MustCompile(`with code\s+(\d{3} \d{3})`)

// codes are the codes a screen has shown, oldest first.
func codes(lines []string) []string {
	var out []string
	for _, line := range lines {
		if m := shownCode.FindStringSubmatch(line); m != nil {
			out = append(out, m[1])
		}
	}
	return out
}

// firstWith is the index of the first line containing text, or -1.
func firstWith(lines []string, text string) int {
	for i, line := range lines {
		if strings.Contains(line, text) {
			return i
		}
	}
	return -1
}

// serveRig is a machine that serves through a real relay, and the two books.
type serveRig struct {
	t        *testing.T
	host     *Host
	screen   *screenLog
	devices  *Book
	machines *Book
}

// newServeRig starts a machine whose person answers approve; nil is nobody there.
func newServeRig(t *testing.T, approve func(label, words string) bool) *serveRig {
	t.Helper()
	t.Setenv("CODEAF_HOME", t.TempDir())
	_, address := liveRelay(t)
	t.Setenv(RelayEnv, address)

	rig := &serveRig{t: t, screen: &screenLog{},
		devices:  BookAt(filepath.Join(t.TempDir(), "devices.json")),
		machines: BookAt(filepath.Join(t.TempDir(), "machines.json"))}
	rig.host = &Host{Service: address, Device: aDevice(t), Devices: rig.devices, Desk: &Desk{},
		Approve: approve, Say: rig.screen.say}

	ctx, stop := context.WithCancel(context.Background())
	ended := make(chan struct{})
	go func() { _ = rig.host.Run(ctx); close(ended) }()
	t.Cleanup(func() { stop(); <-ended })
	rig.screen.until(t, "its first code", func(l []string) bool { return len(codes(l)) > 0 })
	return rig
}

// reach is a device at the other end that types whatever typed says.
func (r *serveRig) reach(label string, typed func() string) (Reach, *screenLog) {
	mine := &screenLog{}
	return Reach{Name: r.host.Device.Name(), Device: aDevice(r.t), Machines: r.machines, Label: label, Say: mine.say,
		AskCode: func(string) (string, error) { return typed(), nil }}, mine
}

// latest is the code on the machine's screen right now.
func (r *serveRig) latest() string {
	all := codes(r.screen.all())
	return all[len(all)-1]
}

// wrongBy is a code that is not this one.
func wrongBy(code string) string {
	var a, b int
	_, _ = fmt.Sscanf(code, "%d %d", &a, &b)
	return fmt.Sprintf("%06d", (a*1000+b+1)%1_000_000)
}

const burnLine = "someone typed a wrong code, so that code is no longer good"

// ONE ATTEMPT PER CODE, FOR A MACHINE AS FOR A PERSON'S CHATS. A wrong code
// spends it; the machine says the code is no longer good and, after that, shows
// a new one, in that order and with nobody asking.
func TestServeCodeIsOneAttempt(t *testing.T) {
	t.Run("a wrong code spends it and the screen says so, then shows a new one", func(t *testing.T) {
		rig := newServeRig(t, func(string, string) bool { return true })
		first := rig.latest()
		reach, _ := rig.reach("laptop", func() string { return wrongBy(first) })

		if _, err := reach.Open(context.Background()); err == nil || !strings.Contains(err.Error(), "that is not the code shown on") {
			t.Fatalf("the wrong code was answered with %v", err)
		}
		lines := rig.screen.until(t, "a new code after the burn", func(l []string) bool { return len(codes(l)) >= 2 })
		burn, fresh := firstWith(lines, burnLine), firstWith(lines[1:], "with code")+1
		if burn < 0 || fresh <= burn {
			t.Fatalf("the screen said %q, want the burn line and then a new code", lines)
		}
		if all := codes(lines); all[1] == first {
			t.Fatal("the code after the burn is the burned one")
		}
	})
	t.Run("the old code typed after a burn does not work", func(t *testing.T) {
		rig := newServeRig(t, func(string, string) bool { return true })
		old := rig.latest()
		wrong, _ := rig.reach("laptop", func() string { return wrongBy(old) })
		_, _ = wrong.Open(context.Background())
		rig.screen.until(t, "a new code", func(l []string) bool { return len(codes(l)) >= 2 })

		again, _ := rig.reach("laptop", func() string { return old })
		if _, err := again.Open(context.Background()); err == nil {
			t.Fatal("a spent code paired a device")
		}
		if list, _ := rig.devices.Devices(); len(list) != 0 {
			t.Fatalf("the machine's book holds %v", list)
		}
	})
	t.Run("the new code typed after a burn works", func(t *testing.T) {
		rig := newServeRig(t, func(string, string) bool { return true })
		old := rig.latest()
		wrong, _ := rig.reach("laptop", func() string { return wrongBy(old) })
		_, _ = wrong.Open(context.Background())
		rig.screen.until(t, "a new code", func(l []string) bool { return len(codes(l)) >= 2 })

		right, _ := rig.reach("laptop", rig.latest)
		tunnel, err := right.Open(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		_ = tunnel.Close()
		if list, _ := rig.devices.Devices(); len(list) != 1 {
			t.Fatalf("the machine's book holds %v, want the one device", list)
		}
	})
}

// A MACHINE ASKS BEFORE IT ADMITS. With nobody to ask, or a person who says no,
// the device is not written down and is told it was refused there; a yes lets it
// in.
func TestServeAsksBeforeAdmitting(t *testing.T) {
	cases := map[string]struct {
		approve func(string, string) bool
		admits  bool
	}{
		"nobody to ask":     {nil, false},
		"a person says no":  {func(string, string) bool { return false }, false},
		"a person says yes": {func(string, string) bool { return true }, true},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			rig := newServeRig(t, c.approve)
			reach, _ := rig.reach("laptop", rig.latest)
			tunnel, err := reach.Open(context.Background())
			list, _ := rig.devices.Devices()
			known, _ := rig.machines.Machines()

			if c.admits {
				if err != nil || len(list) != 1 || len(known) != 1 {
					t.Fatalf("a yes gave %v, %d devices, %d machines", err, len(list), len(known))
				}
				_ = tunnel.Close()
				return
			}
			if err == nil || !strings.Contains(err.Error(), "did not let this device in") {
				t.Fatalf("the device was told %v, want that it was not let in", err)
			}
			if len(list) != 0 || len(known) != 0 {
				t.Fatalf("a refused device was written down: %d on the machine, %d on the device", len(list), len(known))
			}
			rig.screen.until(t, "that a device was turned away", func(l []string) bool {
				return firstWith(l, "was not let in") >= 0
			})
		})
	}
}

// BOTH SCREENS SHOW THE SAME THREE WORDS. What the device says it is waiting
// under is what the machine's person is asked to compare.
func TestServeJoinWordsMatchOnBothSides(t *testing.T) {
	asked := make(chan string, 1)
	rig := newServeRig(t, func(_, words string) bool { asked <- words; return true })
	reach, device := rig.reach("laptop", rig.latest)
	tunnel, err := reach.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_ = tunnel.Close()

	prefix := WaitingLine(reach.Name, "")
	lines := device.all()
	i := firstWith(lines, prefix)
	if i < 0 {
		t.Fatalf("the device never showed its words: %q", lines)
	}
	if shown, want := strings.TrimPrefix(lines[i], prefix), <-asked; shown != want || len(strings.Fields(want)) != wordsShown {
		t.Fatalf("the device shows %q and the machine asks about %q", shown, want)
	}
}

// THE MACHINE'S PERSON IS TOLD WHAT THE DEVICE CALLS ITSELF, tidied the way a
// name in a list is.
func TestServeApproveGetsTheDeviceLabel(t *testing.T) {
	for name, c := range map[string]struct{ said, shown string }{
		"a plain name":  {"kitchen laptop", "kitchen laptop"},
		"control codes": {"kit\x07chen\x1b[2Jlaptop", "kitchen[2Jlaptop"},
		"a long name":   {strings.Repeat("y", 300), strings.Repeat("y", 32)},
	} {
		t.Run(name, func(t *testing.T) {
			labels := make(chan string, 1)
			rig := newServeRig(t, func(label, _ string) bool { labels <- label; return true })
			reach, _ := rig.reach(c.said, rig.latest)
			tunnel, err := reach.Open(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			_ = tunnel.Close()
			if got := <-labels; got != c.shown {
				t.Fatalf("the machine was asked about %q, want %q", got, c.shown)
			}
			if list, _ := rig.devices.Devices(); len(list) != 1 || list[0].Label != c.shown {
				t.Fatalf("the machine wrote down %v", list)
			}
		})
	}
}

// A DROPPED NETWORK IS ONE LINE, NOT ONE LINE A RETRY. While the relay is gone
// the machine says once that it lost sync and is trying again, keeps trying
// without being restarted, and is reachable again when the relay is back
// without reprinting a banner whose name and code have not changed.
func TestServeSaysOnceThatItLostSyncAndComesBack(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	var down atomic.Bool
	door := &relay.Server{}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if down.Load() {
			http.Error(w, "error code: 502", http.StatusBadGateway)
			return
		}
		door.ServeHTTP(w, r)
	}))
	wire := &cuttable{Listener: server.Listener}
	server.Listener = wire
	server.Start()
	t.Cleanup(server.Close)
	screen := &screenLog{}
	host := &Host{Service: server.URL, Device: aDevice(t), Devices: BookAt(filepath.Join(t.TempDir(), "devices.json")), Desk: &Desk{}, Say: screen.say}
	ctx, stop := context.WithCancel(context.Background())
	ended := make(chan struct{})
	go func() { _ = host.Run(ctx); close(ended) }()
	t.Cleanup(func() { stop(); <-ended })
	screen.until(t, "its first code", func(l []string) bool { return len(codes(l)) == 1 })

	down.Store(true)
	wire.cut()
	time.Sleep(4 * time.Second) // the first retries come at one and two seconds
	down.Store(false)
	waitForCondition(t, "the machine to register again", func() bool {
		return len(door.Ledgers()) == 1
	})
	lines := screen.all()
	lost := 0
	for _, line := range lines {
		if line == LostSyncLine {
			lost++
		}
	}
	if lost != 1 {
		t.Fatalf("the machine said it lost sync %d times over one outage; want once: %q", lost, lines)
	}
	if got := len(codes(lines)); got != 1 {
		t.Fatalf("the unchanged banner was printed %d times across one brief outage; want 1: %q", got, lines)
	}
	// The lost line is answered by exactly one back line, so the screen of a
	// machine that recovered never ends on "lost sync".
	screen.until(t, "the back line", func(l []string) bool {
		for _, line := range l {
			if line == BackLine(host.Device.Name()) {
				return true
			}
		}
		return false
	})
	back := 0
	for _, line := range screen.all() {
		if line == BackLine(host.Device.Name()) {
			back++
		}
	}
	if back != 1 {
		t.Fatalf("the machine said it was back %d times after one outage; want once: %q", back, screen.all())
	}
}

// A RELAY THAT STILL HOLDS A LIVE SOCKET FOR THIS NAME IS WAITED OUT. While
// another live carrier holds this machine's name after a drop, a machine that
// was already serving keeps trying instead of quitting, and comes back as soon
// as that carrier ends.
func TestServeWaitsOutItsOwnStaleName(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	var connected atomic.Int32
	door := &relay.Server{
		Note: func(_, what string) {
			if what == "connected" {
				connected.Add(1)
			}
		},
	}
	server := httptest.NewUnstartedServer(door)
	wire := &cuttable{Listener: server.Listener}
	server.Listener = wire
	server.Start()
	t.Cleanup(server.Close)
	screen := &screenLog{}
	device := aDevice(t)
	host := &Host{Service: server.URL, Device: device, Devices: BookAt(filepath.Join(t.TempDir(), "devices.json")), Desk: &Desk{}, Say: screen.say}
	ctx, stop := context.WithCancel(context.Background())
	ended := make(chan struct{})
	var runErr error
	go func() { runErr = host.Run(ctx); close(ended) }()
	t.Cleanup(func() { stop(); <-ended })
	screen.until(t, "its first code", func(l []string) bool { return len(codes(l)) == 1 })

	wire.cut()
	stale, err := relay.Register(context.Background(), server.URL, device.Private())
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(3 * time.Second)
	_ = stale.Close()
	select {
	case <-ended:
		t.Fatalf("serve quit while its own old name was still held: %v", runErr)
	default:
	}
	waitForCondition(t, "the machine to retake its name", func() bool {
		return connected.Load() >= 3 && len(door.Ledgers()) == 1
	})
}

// Contract item 1: SILENT DIALS END AT THE HANDSHAKE DEADLINE. Sixteen silent
// dials fill a served machine's stream ceiling only until the handshake
// deadline fires; once it does, the serving machine closes all sixteen and a
// real `--at` dial succeeds.
func TestSilentDialsCannotHoldAMachine(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	door, address := liveRelay(t)
	t.Setenv(RelayEnv, address)

	screen := &screenLog{}
	devices := BookAt(filepath.Join(t.TempDir(), "devices.json"))
	machines := BookAt(filepath.Join(t.TempDir(), "machines.json"))
	host := &Host{
		Service:   address,
		Device:    aDevice(t),
		Devices:   devices,
		Desk:      &Desk{},
		Approve:   func(string, string) bool { return true },
		Say:       screen.say,
		handshake: 60 * time.Millisecond,
		Open: func(pipe io.ReadWriteCloser) {
			defer func() { _ = pipe.Close() }()
			_, _ = io.Copy(pipe, pipe)
		},
	}
	ctx, stop := context.WithCancel(context.Background())
	ended := make(chan struct{})
	go func() { _ = host.Run(ctx); close(ended) }()
	t.Cleanup(func() { stop(); <-ended })
	first := codes(screen.until(t, "its first code", func(l []string) bool { return len(codes(l)) == 1 }))[0]

	// Pair one device first so the post-flood check exercises an ordinary
	// paired `--at` dial.
	reach := Reach{
		Name:     host.Device.Name(),
		Device:   aDevice(t),
		Machines: machines,
		Label:    "laptop",
		AskCode:  func(string) (string, error) { return first, nil },
	}
	warm, err := reach.Open(context.Background())
	if err != nil {
		t.Fatalf("initial pairing failed: %v", err)
	}
	_ = warm.Close()

	// Open 16 silent streams (relay.MaxStreams) and send zero bytes on each.
	silent := make([]io.ReadWriteCloser, 0, relay.MaxStreams)
	t.Cleanup(func() {
		for _, s := range silent {
			_ = s.Close()
		}
	})
	for i := 0; i < relay.MaxStreams; i++ {
		s, err := relay.Dial(context.Background(), address, host.Device.Name())
		if err != nil {
			t.Fatalf("silent dial %d: %v", i, err)
		}
		silent = append(silent, s)
	}

	// Without waiting thirty real seconds, the short handshake deadline closes
	// every silent stream and frees the machine's slots on the relay.
	waitForCondition(t, "all 16 silent streams to close at the handshake deadline", func() bool {
		ledgers := door.Ledgers()
		return len(ledgers) == 1 && ledgers[0].Streams == 0
	})

	tunnel, err := reach.Open(context.Background())
	if err != nil {
		t.Fatalf("real --at dial after 16 silent dials failed: %v", err)
	}
	defer func() { _ = tunnel.Close() }()
	const want = "still reachable"
	if _, err := tunnel.Write([]byte(want)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, len(want))
	if _, err := io.ReadFull(tunnel, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != want {
		t.Fatalf("echoed %q, want %q", string(buf), want)
	}
}

// Contract item 2: A DIAL THAT ONLY SAYS "PAIR" CANNOT FREEZE PAIRING. Sending
// the single `P` intent byte and then nothing spends the live code before the
// exchange (preserving the one-attempt rule), hits the handshake deadline,
// retires that code, and prints a fresh code that pairs a real device.
func TestHalfStartedPairIntentRotatesTheCode(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	_, address := liveRelay(t)
	t.Setenv(RelayEnv, address)

	screen := &screenLog{}
	devices := BookAt(filepath.Join(t.TempDir(), "devices.json"))
	machines := BookAt(filepath.Join(t.TempDir(), "machines.json"))
	host := &Host{
		Service:   address,
		Device:    aDevice(t),
		Devices:   devices,
		Desk:      &Desk{},
		Approve:   func(string, string) bool { return true },
		Say:       screen.say,
		handshake: 60 * time.Millisecond,
		Open: func(pipe io.ReadWriteCloser) {
			defer func() { _ = pipe.Close() }()
			_, _ = io.Copy(pipe, pipe)
		},
	}
	ctx, stop := context.WithCancel(context.Background())
	ended := make(chan struct{})
	go func() { _ = host.Run(ctx); close(ended) }()
	t.Cleanup(func() { stop(); <-ended })
	first := codes(screen.until(t, "its first code", func(l []string) bool { return len(codes(l)) == 1 }))[0]

	half, err := relay.Dial(context.Background(), address, host.Device.Name())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = half.Close() })
	if _, err := half.Write([]byte{intentPair}); err != nil {
		t.Fatal(err)
	}

	lines := screen.until(t, "the next code after a stalled P intent", func(l []string) bool {
		return len(codes(l)) >= 2
	})
	if firstWith(lines, burnLine) < 0 {
		t.Fatalf("the machine never reported the burned attempt: %q", lines)
	}
	second := codes(lines)[1]
	if second == first {
		t.Fatalf("the rotated code %q matched the spent code", second)
	}

	reach := Reach{
		Name:     host.Device.Name(),
		Device:   aDevice(t),
		Machines: machines,
		Label:    "laptop",
		AskCode:  func(string) (string, error) { return second, nil },
	}
	tunnel, err := reach.Open(context.Background())
	if err != nil {
		t.Fatalf("pairing with the rotated code failed: %v", err)
	}
	_ = tunnel.Close()
	if list, _ := devices.Devices(); len(list) != 1 {
		t.Fatalf("the machine admitted %v, want 1 paired device", list)
	}
}

// Contract item 5: A DROPPED REGISTRATION BACKS OFF, PRINTS AN UNCHANGED BANNER
// ONCE, AND PHRASES RATE LIMITS FOR A PERSON.
func TestDroppedRegistrationBacksOffAndPrintsBannerOnce(t *testing.T) {
	t.Run("ten immediate drops grow the wait each time up to the cap and print the banner once", func(t *testing.T) {
		t.Setenv("CODEAF_HOME", t.TempDir())
		var clockMu sync.Mutex
		now := time.Now()
		clock := func() time.Time {
			clockMu.Lock()
			defer clockMu.Unlock()
			return now
		}
		advance := func(d time.Duration) {
			clockMu.Lock()
			now = now.Add(d)
			clockMu.Unlock()
		}

		connected := make(chan struct{}, 32)
		door := &relay.Server{
			Now: clock,
			Note: func(_, what string) {
				if what == "connected" {
					connected <- struct{}{}
				}
			},
		}
		server := httptest.NewUnstartedServer(door)
		wire := &cuttable{Listener: server.Listener}
		server.Listener = wire
		server.Start()
		t.Cleanup(server.Close)

		slept := make(chan time.Duration, 32)
		screen := &screenLog{}
		host := &Host{
			Service: server.URL,
			Device:  aDevice(t),
			Devices: BookAt(filepath.Join(t.TempDir(), "devices.json")),
			Desk:    &Desk{Now: clock},
			Say:     screen.say,
			Now:     clock,
			sleep: func(ctx context.Context, d time.Duration) error {
				select {
				case <-ctx.Done():
					return ctx.Err()
				default:
				}
				advance(d)
				slept <- d
				return nil
			},
		}
		ctx, stop := context.WithCancel(context.Background())
		ended := make(chan struct{})
		go func() { _ = host.Run(ctx); close(ended) }()
		t.Cleanup(func() { stop(); <-ended })

		screen.until(t, "its first code", func(l []string) bool { return len(codes(l)) == 1 })
		awaitStep(t, connected, "initial registration")

		var waits []time.Duration
		for i := 0; i < 10; i++ {
			wire.cut()
			waits = append(waits, awaitStep(t, slept, fmt.Sprintf("backoff %d", i+1)))
			awaitStep(t, connected, fmt.Sprintf("reconnect %d", i+1))
		}

		wantWaits := []time.Duration{
			1 * time.Second,
			2 * time.Second,
			4 * time.Second,
			8 * time.Second,
			16 * time.Second,
			32 * time.Second,
			time.Minute,
			time.Minute,
			time.Minute,
			time.Minute,
		}
		for i, want := range wantWaits {
			if waits[i] != want {
				t.Fatalf("backoff %d was %v, want %v (all: %v)", i+1, waits[i], want, waits)
			}
		}
		if got := len(codes(screen.all())); got != 1 {
			t.Fatalf("banner printed %d times across 10 immediate drops; want 1: %q", got, screen.all())
		}

		// Confirm Host.Run has entered hold (and recorded `since := h.now()`)
		// before advancing the clock, by sending an unknown intent byte that
		// Host.answer immediately closes without touching the code or screen.
		probe, err := relay.Dial(context.Background(), server.URL, host.Device.Name())
		if err != nil {
			t.Fatal(err)
		}
		_, _ = probe.Write([]byte{'?'})
		_, _ = io.Copy(io.Discard, probe)
		_ = probe.Close()

		// Once a registration stays up for at least one minute, a later drop
		// resets the backoff to 1s.
		advance(steadyAfter + time.Second)
		wire.cut()
		if reset := awaitStep(t, slept, "backoff after steady registration"); reset != time.Second {
			t.Fatalf("backoff after holding >= %v was %v, want 1s", steadyAfter, reset)
		}
	})

	t.Run("ErrTooMany gets a person-facing sentence and backs off without leaking the sentinel", func(t *testing.T) {
		t.Setenv("CODEAF_HOME", t.TempDir())
		throttled := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "too many registrations from here — wait a minute", http.StatusTooManyRequests)
		}))
		t.Cleanup(throttled.Close)

		ctx, stop := context.WithCancel(context.Background())
		defer stop()
		var waits []time.Duration
		screen := &screenLog{}
		host := &Host{
			Service: throttled.URL,
			Device:  aDevice(t),
			Devices: BookAt(filepath.Join(t.TempDir(), "devices.json")),
			Desk:    &Desk{},
			Say:     screen.say,
			sleep: func(ctx context.Context, d time.Duration) error {
				waits = append(waits, d)
				if len(waits) == 3 {
					stop()
					return context.Canceled
				}
				return nil
			},
		}
		if err := host.Run(ctx); err != nil {
			t.Fatalf("Host.Run under 429 returned %v, want nil on context cancel", err)
		}
		lines := screen.all()
		if len(lines) != 1 || lines[0] != TooManyRegistrationsLine {
			t.Fatalf("screen under 429 said %q; want [%q] once", lines, TooManyRegistrationsLine)
		}
		for _, line := range lines {
			if strings.Contains(line, "relay:") {
				t.Fatalf("raw package sentinel leaked to screen: %q", line)
			}
		}
		want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}
		for i := range want {
			if waits[i] != want[i] {
				t.Fatalf("429 backoff %d was %v, want %v (all: %v)", i+1, waits[i], want[i], waits)
			}
		}
	})
}

func waitForCondition(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func awaitStep[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(5 * time.Second):
		var zero T
		t.Fatalf("timed out waiting for %s", what)
		return zero
	}
}

// cuttable is a listener whose connections a test can drop, hijacked ones
// included, the way a network that goes away drops them.
type cuttable struct {
	net.Listener
	mu    sync.Mutex
	conns []net.Conn
}

func (c *cuttable) Accept() (net.Conn, error) {
	conn, err := c.Listener.Accept()
	if err == nil {
		c.mu.Lock()
		c.conns = append(c.conns, conn)
		c.mu.Unlock()
	}
	return conn, err
}

func (c *cuttable) cut() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, conn := range c.conns {
		_ = conn.Close()
	}
	c.conns = nil
}
