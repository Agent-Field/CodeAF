package provider

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	lanes "github.com/Agent-Field/codeaf/internal/lane"
)

type routingBeatSheet struct {
	refreshes int
	blocked   <-chan struct{}
}

func (s *routingBeatSheet) Rows(string) []lanes.Row { return nil }
func (s *routingBeatSheet) Refresh(context.Context, string) error {
	s.refreshes++
	if s.blocked != nil {
		<-s.blocked
	}
	return nil
}

func routingBeatClient(t *testing.T, strategy RoutingStrategy, direct bool) *Client {
	t.Helper()
	client, err := NewClient(Config{
		APIKey: "fixture", BaseURL: "https://routing.invalid/v1", Model: "routing/model",
		Routing: StaticRouting(strategy), Direct: direct,
		HTTPClient: handlerClient(answering(ok(answerFrom("", 10, 0, 0)))),
	})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

// Contracts 4 and 5: bare completions wake stale sheets without any agent lifetime.
func TestBareProviderRoutingWakesTheBeat(t *testing.T) {
	for _, strategy := range []RoutingStrategy{RoutingSimple, RoutingLatency} {
		t.Run(string(strategy), func(t *testing.T) {
			forgetLanes(t)
			synctest.Test(t, func(t *testing.T) {
				lanes.Default().Reset()
				defer lanes.Default().Reset()
				client := routingBeatClient(t, strategy, false)
				sheet := &routingBeatSheet{}
				lanes.Default().SetSheet(sheet)
				ctx, stop := context.WithCancel(context.Background())
				defer stop()
				go lanes.Beat(ctx, sheet, []string{"routing/model"}, 0)
				synctest.Wait()
				time.Sleep(30 * time.Minute)
				blocked := make(chan struct{})
				release := sync.OnceFunc(func() { close(blocked) })
				defer release()
				sheet.blocked = blocked
				// The scripted HTTP boundary completes even while refresh cannot finish.
				if _, err := client.CompleteWithMessages(ctx, userMessages("judge this landing")); err != nil {
					t.Fatal(err)
				}
				synctest.Wait()
				if sheet.refreshes != 2 {
					t.Fatalf("bare completion: %d refreshes; want startup plus catch-up", sheet.refreshes)
				}
				release()
				synctest.Wait()
				time.Sleep(time.Hour)
				synctest.Wait()
				if sheet.refreshes != 2 {
					t.Fatalf("completed call left %d refreshes; want 2", sheet.refreshes)
				}
			})
		})
	}
}

// Contract 5: typing selection counts even when a cold frontier sends no probe.
func TestColdProbeSelectionAloneWakesTheBeat(t *testing.T) {
	forgetLanes(t)
	synctest.Test(t, func(t *testing.T) {
		lanes.Default().Reset()
		defer lanes.Default().Reset()
		client := routingBeatClient(t, RoutingLatency, false)
		sheet := &routingBeatSheet{}
		lanes.Default().SetSheet(sheet)
		ctx, stop := context.WithCancel(context.Background())
		defer stop()
		go lanes.Beat(ctx, sheet, []string{"routing/model"}, 0)
		synctest.Wait()
		time.Sleep(30 * time.Minute)
		client.ProbeLanes(ctx, "routing/model")
		synctest.Wait()
		if sheet.refreshes != 2 {
			t.Fatalf("cold typing selection: %d refreshes; want 2", sheet.refreshes)
		}
		for range 12 {
			time.Sleep(time.Minute)
			client.ProbeLanes(ctx, "routing/model")
			synctest.Wait()
		}
		if sheet.refreshes != 4 {
			t.Fatalf("probe-only cadence: %d refreshes; want startup, catch-up, minute 5 and minute 10", sheet.refreshes)
		}
		// Last selection was minute 12; minute 15 is still inside its idle interval.
		time.Sleep(5 * time.Minute)
		synctest.Wait()
		if sheet.refreshes != 5 {
			t.Fatalf("typing grace interval: %d refreshes; want 5", sheet.refreshes)
		}
		time.Sleep(time.Hour)
		synctest.Wait()
		if sheet.refreshes != 5 {
			t.Fatalf("after typing stops: %d refreshes; want 5", sheet.refreshes)
		}
	})
}

// Contracts 4 and 5: a probe returns with workers and stale refresh both blocked.
func TestProbeRoutingDoesNotWaitForRefreshOrSend(t *testing.T) {
	forgetLanes(t)
	synctest.Test(t, func(t *testing.T) {
		lanes.Default().Reset()
		defer lanes.Default().Reset()
		client := routingBeatClient(t, RoutingLatency, false)
		sheet := &routingBeatSheet{}
		lanes.Default().SetSheet(sheet)
		lanes.Default().SetLedger(&recordingLedger{beliefs: []lanes.Belief{
			laneBelief("routing/model", "quick", 20, 500, 1.1),
			laneBelief("routing/model", "cheap", 30, 300, 1),
		}})
		sends := 0
		lanes.Default().SetProber(lanes.NewProber(lanes.ProberConfig{Send: func(ctx context.Context, _, _ string) (time.Duration, error) {
			sends++
			<-ctx.Done()
			return 0, ctx.Err()
		}}))
		ctx, stop := context.WithCancel(context.Background())
		defer stop()
		go lanes.Beat(ctx, sheet, []string{"routing/model"}, 0)
		synctest.Wait()
		time.Sleep(30 * time.Minute)
		blocked := make(chan struct{})
		release := sync.OnceFunc(func() { close(blocked) })
		defer release()
		sheet.blocked = blocked
		probeCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		client.ProbeLanes(probeCtx, "routing/model")
		synctest.Wait()
		if sends != 2 || sheet.refreshes != 2 {
			t.Fatalf("probe-only selection: %d sends, %d refreshes; want 2 each", sends, sheet.refreshes)
		}
		cancel()
		release()
		synctest.Wait()
		time.Sleep(time.Hour)
		synctest.Wait()
		if sheet.refreshes != 2 {
			t.Fatalf("after probe cancellation: %d refreshes; want 2", sheet.refreshes)
		}
	})
}

// Round 2 C2: routing-off and direct-service completions and probes stay idle.
func TestProviderRoutingGatesLeaveTheBeatIdle(t *testing.T) {
	for _, direct := range []bool{false, true} {
		name := "routing off"
		strategy := RoutingOff
		if direct {
			name, strategy = "direct service", RoutingLatency
		}
		t.Run(name, func(t *testing.T) {
			forgetLanes(t)
			synctest.Test(t, func(t *testing.T) {
				lanes.Default().Reset()
				defer lanes.Default().Reset()
				client := routingBeatClient(t, strategy, direct)
				sheet := &routingBeatSheet{}
				lanes.Default().SetSheet(sheet)
				ctx, stop := context.WithCancel(context.Background())
				defer stop()
				go lanes.Beat(ctx, sheet, []string{"routing/model"}, 0)
				synctest.Wait()
				time.Sleep(30 * time.Minute)
				if _, err := client.CompleteWithMessages(ctx, userMessages("read this")); err != nil {
					t.Fatal(err)
				}
				synctest.Wait()
				if sheet.refreshes != 1 {
					t.Fatalf("gated completion: %d refreshes; want only startup", sheet.refreshes)
				}
				client.ProbeLanes(ctx, "routing/model")
				synctest.Wait()
				if sheet.refreshes != 1 {
					t.Fatalf("refused probe: %d refreshes; want only startup", sheet.refreshes)
				}
			})
		})
	}
}

type recoveringSheetFetcher struct {
	calls   int
	serving bool
}

func (f *recoveringSheetFetcher) Fetch(context.Context, string, string) (io.ReadCloser, error) {
	f.calls++
	if !f.serving {
		return nil, lanes.ErrNoSheetHere
	}
	const page = `{"data":{"endpoints":[{"provider_name":"Cloudflare","latency_last_30m":{"p50":700},"throughput_last_30m":{"p50":60}}]}}`
	return io.NopCloser(strings.NewReader(page)), nil
}

// Round 2 C1: real completion demand can replace a weak negative capability
// answer after the existing support backoff, without requiring an agent turn.
func TestSilentRouterCanRecoverOnTheNextRoutingCompletion(t *testing.T) {
	forgetLanes(t)
	synctest.Test(t, func(t *testing.T) {
		lanes.Default().Reset()
		defer lanes.Default().Reset()
		client := routingBeatClient(t, RoutingLatency, false)
		fetch := &recoveringSheetFetcher{}
		lanes.WireSheet(client.config.BaseURL, "fixture", fetch, false)
		ctx, stop := context.WithCancel(context.Background())
		defer stop()
		go lanes.Beat(ctx, lanes.Default().Sheet(), []string{"routing/model"}, 0)
		synctest.Wait()
		if fetch.calls != 1 {
			t.Fatalf("startup made %d fetches; want 1", fetch.calls)
		}
		// A later endpoints page may replace an answer that named no serving lane.
		if !lanes.HeardPrefsSilent(client.config.BaseURL) || lanes.PrefsCarried(client.config.BaseURL) {
			t.Fatal("fixture did not establish the weak negative capability answer")
		}
		time.Sleep(30 * time.Minute)
		fetch.serving = true
		if _, err := client.CompleteWithMessages(ctx, userMessages("resume work")); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		if fetch.calls != 2 || !lanes.PrefsCarried(client.config.BaseURL) {
			t.Fatalf("resumed completion: %d fetches, carries preferences %v; want 2 and true", fetch.calls, lanes.PrefsCarried(client.config.BaseURL))
		}
	})
}

// Contract 5: the selection boundary still records routing with an installed chooser.
func TestInstalledProbeChooserStillCountsRouting(t *testing.T) {
	forgetLanes(t)
	synctest.Test(t, func(t *testing.T) {
		lanes.Default().Reset()
		defer lanes.Default().Reset()
		client := routingBeatClient(t, RoutingLatency, false)
		lanes.Default().SetChooser(&countingChooser{})
		sheet := &routingBeatSheet{}
		lanes.Default().SetSheet(sheet)
		ctx, stop := context.WithCancel(context.Background())
		defer stop()
		go lanes.Beat(ctx, sheet, []string{"routing/model"}, 0)
		synctest.Wait()
		time.Sleep(30 * time.Minute)
		client.ProbeLanes(ctx, "routing/model")
		synctest.Wait()
		if sheet.refreshes != 2 {
			t.Fatalf("installed probe chooser: %d refreshes; want startup plus catch-up", sheet.refreshes)
		}
	})
}

// A terminal refusal cannot be replaced by a later sheet, so routing through it
// wakes no refresh.
func TestATerminalPreferenceRefusalLeavesTheBeatIdle(t *testing.T) {
	forgetLanes(t)
	synctest.Test(t, func(t *testing.T) {
		lanes.Default().Reset()
		defer lanes.Default().Reset()
		client := routingBeatClient(t, RoutingLatency, false)
		fetch := &recoveringSheetFetcher{}
		lanes.WireSheet(client.config.BaseURL, "fixture", fetch, false)
		ctx, stop := context.WithCancel(context.Background())
		defer stop()
		go lanes.Beat(ctx, lanes.Default().Sheet(), []string{"routing/model"}, 0)
		synctest.Wait()
		if fetch.calls != 1 {
			t.Fatalf("startup made %d fetches; want 1", fetch.calls)
		}
		if !lanes.HeardPrefsRefused(client.config.BaseURL) || lanes.PrefsCarried(client.config.BaseURL) {
			t.Fatal("fixture did not establish a terminal preference refusal")
		}
		time.Sleep(30 * time.Minute)
		if _, err := client.CompleteWithMessages(ctx, userMessages("resume without routing")); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		if fetch.calls != 1 {
			t.Fatalf("completion after terminal preference refusal: %d fetches; want startup only", fetch.calls)
		}
	})
}
