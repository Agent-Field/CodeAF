package main

import (
	"net/http"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/relayserve"
)

func e14PairWait(t *testing.T, b *appBackend, id string, ready func(appwire.Pairing) bool) appwire.Pairing {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		p, err := b.GetPair(id)
		if err != nil {
			t.Fatal(err)
		}
		if p != nil && ready(*p) {
			return *p
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("A1: pairing did not reach its expected state")
	return appwire.Pairing{}
}

func TestA1PairedEnginesRetryOwnRecordsWithoutChats(t *testing.T) {
	e14PairedEnginesRetry(t, true)
}

func TestA1PairedEnginesRetryImmediatelyAfterPairingWithoutChats(t *testing.T) {
	e14PairedEnginesRetry(t, false)
}

func e14PairedEnginesRetry(t *testing.T, restart bool) {
	t.Helper()
	celltest.Available(t)
	svc := relayserve.New(relayserve.Config{})
	defer svc.Close()
	var allow atomic.Bool
	var puts, refused atomic.Int64
	_, _, _ = appDevicesRelay(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/v1/dir/devices/") {
				if !allow.Load() {
					refused.Add(1)
					http.Error(w, "temporarily unavailable", 503)
					return
				}
				puts.Add(1)
			}
			if strings.HasPrefix(r.URL.Path, "/v1/dir/") {
				next.ServeHTTP(w, r)
			} else {
				svc.Handler.ServeHTTP(w, r)
			}
		})
	})
	a, b := t.TempDir(), t.TempDir()
	ba, ra, wa := appDeviceBackend(t, a)
	bb, rb, wb := appDeviceBackend(t, b)
	wa.replyWithin, wb.replyWithin = 90*time.Second, 90*time.Second
	offer := machineCall[appwire.PairResult](t, wa, "pair.start", appwire.PairStartParams{Mode: "offer_code", Name: "Abir"}).Pairing
	shown := e14PairWait(t, ba, offer.ID, func(p appwire.Pairing) bool { return p.Code != "" })
	join := machineCall[appwire.PairResult](t, wb, "pair.start", appwire.PairStartParams{Mode: "use_code", Code: shown.Code, Name: "Abir"}).Pairing
	question := e14PairWait(t, ba, offer.ID, func(p appwire.Pairing) bool { return p.Question != nil })
	platform := map[string]string{"windows": "Windows", "darwin": "Mac", "linux": "Linux"}[runtime.GOOS]
	if want := "Pair with Abir (" + platform + ")?"; question.Question.Title != want {
		t.Fatalf("A3: same-name approval = %q, want %q", question.Question.Title, want)
	}
	_ = machineCall[appwire.PairResult](t, wa, "pair.answer", appwire.PairAnswerParams{ID: offer.ID, Question: question.Question.ID, Option: "y"})
	for _, side := range []struct {
		backend *appBackend
		id      string
	}{{ba, offer.ID}, {bb, join.ID}} {
		done := e14PairWait(t, side.backend, side.id, func(p appwire.Pairing) bool { return p.State == "done" || p.State == "failed" })
		if done.State != "done" {
			t.Fatalf("A1: pairing failed: %+v", done)
		}
		for _, line := range done.Lines {
			if strings.Contains(line, "could not be shared") || strings.Contains(line, "chat next connects") {
				t.Fatalf("A3: pairing success warned about a retried device record: %q", line)
			}
		}
	}
	if refused.Load() < 2 {
		t.Fatal("A1: both pairing-time writes must fail")
	}
	if restart {
		_ = ba.Close()
		_ = bb.Close()
		_, ra, wa = appDeviceBackend(t, a)
		_, rb, wb = appDeviceBackend(t, b)
		wa.replyWithin, wb.replyWithin = 90*time.Second, 90*time.Second
	}
	allow.Store(true)
	for _, rec := range []*appRecorder{ra, rb} {
		rec.waitFor(t, 15*time.Second, func(event string, data any) bool {
			return event == appwire.EventDevicesChanged && len(data.(appwire.DevicesResult).Devices.Devices) == 2
		})
	}
	for _, wire := range []*actionWire{wa, wb} {
		if view := appDeviceList(t, wire); len(view.Devices) != 2 || !view.Devices[0].Self {
			t.Fatalf("A1: restarted engine roster: %+v", view)
		}
	}
	if puts.Load() != 2 {
		t.Fatalf("A1: %d successful PUT attempts, want one per computer", puts.Load())
	}
	renamed := machineCall[appwire.DeviceRenameResult](t, wa, "devices.rename", appwire.DeviceRenameParams{Name: "studio"})
	if renamed.Line != "This computer is now called studio." || puts.Load() != 3 {
		t.Fatalf("A1/A3: rename was not published immediately: %+v, PUTs=%d", renamed, puts.Load())
	}
	appDeviceEvent(t, rb, func(v appwire.Devices) bool { return len(v.Devices) == 2 && v.Devices[1].Name == "studio" })
	_ = machineCall[appwire.DeviceRenameResult](t, wa, "devices.rename", appwire.DeviceRenameParams{Name: "studio"})
	if puts.Load() != 3 {
		t.Fatal("A1: unchanged rename rewrote the device")
	}
	allow.Store(false)
	pending := machineCall[appwire.DeviceRenameResult](t, wa, "devices.rename", appwire.DeviceRenameParams{Name: "office"})
	if pending.Line != "This computer is now called office. Your other computers will see it when they can reach the relay." {
		t.Fatalf("A3: pending rename receipt: %q", pending.Line)
	}
	allow.Store(true)
	rb.waitFor(t, 15*time.Second, func(event string, data any) bool {
		if event != appwire.EventDevicesChanged {
			return false
		}
		v := data.(appwire.DevicesResult).Devices
		return len(v.Devices) == 2 && v.Devices[1].Name == "office"
	})
	if puts.Load() != 4 {
		t.Fatalf("A1: retried rename sent %d PUTs, want four total changes", puts.Load())
	}
}
