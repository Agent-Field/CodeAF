package lane

import (
	"context"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/Agent-Field/codeaf/internal/home"
)

type countedBeatSheet struct {
	refreshes []time.Time
	reads     int
	wanted    chan string
	block     <-chan struct{}
}

func (s *countedBeatSheet) Refresh(context.Context, string) error {
	s.refreshes = append(s.refreshes, time.Now())
	if s.block != nil {
		<-s.block
	}
	return nil
}

func (s *countedBeatSheet) Rows(string) []Row {
	s.reads++
	return nil
}

func (s *countedBeatSheet) Wanted() <-chan string { return s.wanted }

func (s *countedBeatSheet) Wants(model string) { s.wanted <- model }

// Contract 2: startup warms the frontier; an idle hour adds no refresh or ledger read.
func TestTheIdleBeatDoesNoWorkForAnHour(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	t.Cleanup(Default().Reset)
	synctest.Test(t, func(t *testing.T) {
		Default().Reset()
		defer Default().Reset()
		s := &countedBeatSheet{wanted: make(chan string, 8)}
		ctx, stop := context.WithCancel(context.Background())
		defer stop()
		go Beat(ctx, s, []string{"talk/model"}, 0)
		synctest.Wait()
		if len(s.refreshes) != 1 || s.reads != 1 {
			t.Fatalf("startup: %d refreshes, %d row reads; want 1 of each", len(s.refreshes), s.reads)
		}
		time.Sleep(time.Hour)
		synctest.Wait()
		if len(s.refreshes) != 1 || s.reads != 1 {
			t.Fatalf("idle hour added work: %d refreshes, %d row reads; want only startup", len(s.refreshes), s.reads)
		}
	})
}

// Round 2 C3a: a future ranking timestamp cannot keep an idle beat refreshing.
func TestRoutingWallClockRollbackDoesNotKeepIdleRefreshActive(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	t.Cleanup(Default().Reset)
	synctest.Test(t, func(t *testing.T) {
		Default().Reset()
		defer Default().Reset()
		s := &countedBeatSheet{wanted: make(chan string, 8)}
		ctx, stop := context.WithCancel(context.Background())
		defer stop()
		go Beat(ctx, s, []string{"talk/model"}, 0)
		synctest.Wait()
		Default().Chooser().Choose(Request{Model: "talk/model", Now: time.Now().Add(time.Hour)})
		synctest.Wait()
		time.Sleep(time.Hour)
		synctest.Wait()
		if len(s.refreshes) != 1 {
			t.Fatalf("idle after a future wall stamp: %d refreshes; want only startup", len(s.refreshes))
		}
	})
}

// Round 2 C3b: a backward ranking timestamp cannot suppress real resumed demand.
func TestRoutingBackwardClockStepDoesNotSuppressNewDemand(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	t.Cleanup(Default().Reset)
	synctest.Test(t, func(t *testing.T) {
		Default().Reset()
		defer Default().Reset()
		s := &countedBeatSheet{wanted: make(chan string, 8)}
		ctx, stop := context.WithCancel(context.Background())
		defer stop()
		go Beat(ctx, s, []string{"talk/model"}, 0)
		synctest.Wait()
		Default().Chooser().Choose(Request{Model: "talk/model", Now: time.Now()})
		synctest.Wait()
		time.Sleep(30 * time.Minute)
		Default().Chooser().Choose(Request{Model: "talk/model", Now: time.Now().Add(-time.Hour)})
		synctest.Wait()
		if len(s.refreshes) != 2 {
			t.Fatalf("resumed routing after a backward wall stamp: %d refreshes; want startup plus catch-up", len(s.refreshes))
		}
		time.Sleep(time.Hour)
		synctest.Wait()
		if len(s.refreshes) != 2 {
			t.Fatalf("idle after resumed routing: %d refreshes; want 2", len(s.refreshes))
		}
	})
}

// Contract 3: minute-by-minute routing keeps the original five-minute cadence.
func TestRoutingKeepsTheBeatUntilOneIdleInterval(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	t.Cleanup(Default().Reset)
	synctest.Test(t, func(t *testing.T) {
		Default().Reset()
		defer Default().Reset()
		s := &countedBeatSheet{wanted: make(chan string, 8)}
		ctx, stop := context.WithCancel(context.Background())
		defer stop()
		start := time.Now()
		go Beat(ctx, s, []string{"talk/model"}, 0)
		synctest.Wait()
		for range 12 {
			Default().Chooser().Choose(Request{Model: "talk/model", Now: time.Now()})
			synctest.Wait()
			time.Sleep(time.Minute)
		}
		synctest.Wait()
		if len(s.refreshes) != 3 {
			t.Fatalf("routing for 12 minutes: %d refreshes; want 3", len(s.refreshes))
		}
		for i, want := range []time.Time{start, start.Add(5 * time.Minute), start.Add(10 * time.Minute)} {
			if !s.refreshes[i].Equal(want) {
				t.Fatalf("refresh %d at %s; want %s", i, s.refreshes[i], want)
			}
		}
		// The last routing was at minute 11, so the minute-15 reading is allowed.
		time.Sleep(4 * time.Minute)
		synctest.Wait()
		if len(s.refreshes) != 4 {
			t.Fatalf("before the idle threshold: %d refreshes; want 4", len(s.refreshes))
		}
		time.Sleep(time.Hour)
		synctest.Wait()
		if len(s.refreshes) != 4 {
			t.Fatalf("after a full idle interval: %d refreshes; want 4", len(s.refreshes))
		}
	})
}

// Contract 4: stale routing returns while one coalesced background refresh blocks.
func TestStaleRoutingStartsOneRefreshWithoutWaiting(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	t.Cleanup(Default().Reset)
	synctest.Test(t, func(t *testing.T) {
		Default().Reset()
		defer Default().Reset()
		s := &countedBeatSheet{wanted: make(chan string, 8)}
		ctx, stop := context.WithCancel(context.Background())
		defer stop()
		go Beat(ctx, s, []string{"talk/model"}, 0)
		synctest.Wait()
		time.Sleep(30 * time.Minute)
		unblock := make(chan struct{})
		release := sync.OnceFunc(func() { close(unblock) })
		defer release()
		s.block = unblock
		returned := make(chan struct{})
		go func() {
			for range 3 {
				Default().Chooser().Choose(Request{Model: "talk/model", Now: time.Now()})
			}
			close(returned)
		}()
		synctest.Wait()
		select {
		case <-returned:
		default:
			t.Fatal("routing waited for the blocked refresh")
		}
		if len(s.refreshes) != 2 {
			t.Fatalf("stale burst: %d refreshes; want startup plus one catch-up", len(s.refreshes))
		}
		// A refresh completing after more than one interval is still a fresh reading.
		time.Sleep(6 * time.Minute)
		release()
		synctest.Wait()
		time.Sleep(time.Minute)
		Default().Chooser().Choose(Request{Model: "talk/model", Now: time.Now()})
		synctest.Wait()
		if len(s.refreshes) != 2 {
			t.Fatalf("fresh resume: %d refreshes; want 2", len(s.refreshes))
		}
		time.Sleep(4 * time.Minute)
		synctest.Wait()
		if len(s.refreshes) != 3 {
			t.Fatalf("fresh resume did not restore cadence: %d refreshes; want 3", len(s.refreshes))
		}
		time.Sleep(time.Hour)
		synctest.Wait()
		if len(s.refreshes) != 3 {
			t.Fatalf("idle after routing: %d refreshes; want 3", len(s.refreshes))
		}
	})
}

// Contracts 4 and 6: a completed burst survives a model join that blocks across idle.
func TestRoutingBeforeBeatDispatchKeepsItsCatchup(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	t.Cleanup(Default().Reset)
	synctest.Test(t, func(t *testing.T) {
		Default().Reset()
		defer Default().Reset()
		s := &countedBeatSheet{wanted: make(chan string, 8)}
		ctx, stop := context.WithCancel(context.Background())
		defer stop()
		go Beat(ctx, s, []string{"talk/model"}, 0)
		synctest.Wait()
		Default().Chooser().Choose(Request{Model: "talk/model", Now: time.Now()})
		synctest.Wait()
		unblock := make(chan struct{})
		release := sync.OnceFunc(func() { close(unblock) })
		defer release()
		s.block = unblock
		s.Wants("picked/model")
		s.Wants("picked/model")
		synctest.Wait()
		if len(s.refreshes) != 2 {
			t.Fatalf("model join: %d refreshes; want 2", len(s.refreshes))
		}
		// The join began while active, but its I/O outlasts the idle interval.
		time.Sleep(30 * time.Minute)
		// The beat cannot sample any of these calls until after all have returned.
		for range 3 {
			Default().Chooser().Choose(Request{Model: "talk/model", Now: time.Now()})
		}
		s.block = nil
		release()
		synctest.Wait()
		if len(s.refreshes) != 3 {
			t.Fatalf("completed routing burst: %d refreshes; want one catch-up for the stale model", len(s.refreshes))
		}
		time.Sleep(time.Hour)
		synctest.Wait()
		if len(s.refreshes) != 3 {
			t.Fatalf("short routing left %d refreshes; want 3", len(s.refreshes))
		}
	})
}

// Contract 6: each independent queue receives its own catch-up; duplicate beats join.
func TestRoutingWakesEveryIndependentSheet(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	t.Cleanup(Default().Reset)
	synctest.Test(t, func(t *testing.T) {
		Default().Reset()
		defer Default().Reset()
		sheets := []*countedBeatSheet{{wanted: make(chan string, 8)}, {wanted: make(chan string, 8)}}
		ctx, stop := context.WithCancel(context.Background())
		defer stop()
		for _, sheet := range sheets {
			go Beat(ctx, sheet, []string{"talk/model"}, 0)
		}
		synctest.Wait()
		// A duplicate registers no competing fetch loop.
		Beat(ctx, sheets[0], []string{"talk/model"}, 0)
		synctest.Wait()
		time.Sleep(30 * time.Minute)
		Default().Chooser().Choose(Request{Model: "talk/model", Now: time.Now()})
		synctest.Wait()
		for i, sheet := range sheets {
			if len(sheet.refreshes) != 2 {
				t.Fatalf("sheet %d: %d refreshes; want startup plus catch-up", i, len(sheet.refreshes))
			}
		}
		stop()
		synctest.Wait()
		time.Sleep(30 * time.Minute)
		Default().Chooser().Choose(Request{Model: "talk/model", Now: time.Now()})
		synctest.Wait()
		for i, sheet := range sheets {
			if len(sheet.refreshes) != 2 {
				t.Fatalf("stopped sheet %d made another refresh", i)
			}
		}
	})
}

// Contracts 2 and 6: drawing the TUI's typical prediction is not a send or probe.
func TestRoutingPredictionsLeaveTheBeatIdle(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	t.Cleanup(Default().Reset)
	synctest.Test(t, func(t *testing.T) {
		Default().Reset()
		defer Default().Reset()
		s := &countedBeatSheet{wanted: make(chan string, 8)}
		ctx, stop := context.WithCancel(context.Background())
		defer stop()
		go Beat(ctx, s, []string{"talk/model"}, 0)
		synctest.Wait()
		for range 60 {
			Default().Chooser().Choose(Request{Model: "talk/model", Now: time.Now(), Typical: true})
			time.Sleep(time.Minute)
		}
		synctest.Wait()
		if len(s.refreshes) != 1 {
			t.Fatalf("predictions added refreshes: %d; want only startup", len(s.refreshes))
		}
	})
}

// Contract 3: a periodic round may finish its in-flight model, then stops at idle.
func TestRoutingStopsUnreadModelsAfterTheIdleThreshold(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	t.Cleanup(Default().Reset)
	synctest.Test(t, func(t *testing.T) {
		Default().Reset()
		defer Default().Reset()
		s := &countedBeatSheet{wanted: make(chan string, 8)}
		ctx, stop := context.WithCancel(context.Background())
		defer stop()
		go Beat(ctx, s, []string{"talk/model", "work/model"}, 0)
		synctest.Wait()
		for range 4 {
			time.Sleep(time.Minute)
			Default().Chooser().Choose(Request{Model: "talk/model", Now: time.Now()})
			synctest.Wait()
		}
		unblock := make(chan struct{})
		release := sync.OnceFunc(func() { close(unblock) })
		defer release()
		s.block = unblock
		time.Sleep(6 * time.Minute)
		synctest.Wait()
		if len(s.refreshes) != 3 {
			t.Fatalf("periodic round: %d readings; want two startup and one blocked", len(s.refreshes))
		}
		s.block = nil
		release()
		synctest.Wait()
		time.Sleep(time.Hour)
		synctest.Wait()
		if len(s.refreshes) != 3 {
			t.Fatalf("idle began new model readings: %d; want only the in-flight model", len(s.refreshes))
		}
	})
}

// Contracts 3 and 6: routing racing beat registration still starts every cadence.
func TestRoutingDuringBeatRegistrationReachesEverySheet(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	t.Cleanup(Default().Reset)
	synctest.Test(t, func(t *testing.T) {
		Default().Reset()
		defer Default().Reset()
		ctx, stop := context.WithCancel(context.Background())
		defer stop()
		start := make(chan struct{})
		sheets := make([]*countedBeatSheet, 8)
		for i := range sheets {
			sheet := &countedBeatSheet{wanted: make(chan string, 8)}
			sheets[i] = sheet
			go func() {
				<-start
				Beat(ctx, sheet, []string{"talk/model"}, 0)
			}()
		}
		go func() {
			<-start
			Default().Chooser().Choose(Request{Model: "talk/model", Now: time.Now()})
		}()
		close(start)
		synctest.Wait()
		for range 12 {
			time.Sleep(time.Minute)
			Default().Chooser().Choose(Request{Model: "talk/model", Now: time.Now()})
			synctest.Wait()
		}
		for i, sheet := range sheets {
			if len(sheet.refreshes) != 3 {
				t.Fatalf("concurrently joined sheet %d: %d refreshes; want 3", i, len(sheet.refreshes))
			}
		}
	})
}
