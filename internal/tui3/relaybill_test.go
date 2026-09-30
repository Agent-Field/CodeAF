//go:build relaybill

package tui3

import (
	"context"
	"flag"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/furrow"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/relaybill"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

var (
	billURL  = flag.String("bill-url", "", "base URL of the relay to load")
	billOut  = flag.String("bill-out", "", "where to write the run's manifest")
	billIdle = flag.Duration("bill-idle", 20*time.Minute, "how long the home screen stays open with the lease held")
	billWarm = flag.Int("bill-warm", 12, "moves between two machines that already hold the chat")
	billCold = flag.Int("bill-cold", 1, "moves to a machine that has never seen the chat")
	billGap  = flag.Duration("bill-gap", 130*time.Second, "quiet between phases")
)

// homePace is the home screen's own schedule for asking the directory (this
// package's machinePoll), offered to the load driver as the pace to ask at.
type homePace struct{ poll machinePoll }

func (h *homePace) Ready(now time.Time) bool           { return h.poll.ready(now) }
func (h *homePace) Landed(now time.Time, changed bool) { h.poll.landed(now, changed) }
func (h *homePace) Hurry()                             { h.poll.hurry() }

// TestRelayBill plays one heavy user through the real client against the relay
// named by -bill-url, with this surface's real home pace, and writes the
// windows of its phases for the analytics reader. It is a measurement, not a
// check, so it only runs when asked for with the relaybill tag.
func TestRelayBill(t *testing.T) {
	if *billURL == "" {
		t.Fatal("-bill-url is required: the relay to load")
	}
	bin, err := furrow.ResolveOwned()
	if err != nil {
		t.Fatalf("no engine program: %v", err)
	}
	t.Setenv(cell.EnvVar, "1")
	t.Setenv(home.EnvVar, t.TempDir())
	t.Setenv(syncsetup.URLVar, strings.TrimRight(*billURL, "/"))
	t.Setenv(syncsetup.IntervalVar, "")
	counter := &relaybill.Counter{Next: http.DefaultTransport}
	http.DefaultTransport = counter
	t.Cleanup(func() { http.DefaultTransport = counter.Next })

	cfg := relaybill.Config{
		Dir: t.TempDir(), Binary: bin, Idle: *billIdle, Warm: *billWarm, Cold: *billCold, Gap: *billGap,
		Tick:     homeEvery,
		Schedule: func() relaybill.Schedule { return &homePace{} },
		Log:      func(f string, a ...any) { t.Logf(f, a...) },
	}
	m, err := relaybill.Run(context.Background(), cfg, *billURL, counter)
	if *billOut != "" {
		if werr := m.Write(*billOut); werr != nil {
			t.Fatal(werr)
		}
	}
	if err != nil {
		t.Fatal(err)
	}
}
