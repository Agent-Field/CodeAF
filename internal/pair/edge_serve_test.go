package pair

// Contract 18.R for the stream door: `codeaf serve` takes one attempt per code,
// asks a person before it lets a device in, and shows that person the words the
// device shows. These run a real relay and a real Host, as the end-to-end test
// does, and read what the machine says the way a person would: from its screen.

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
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
