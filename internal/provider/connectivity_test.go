package provider

import (
	"context"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/lane/control"
	"github.com/Agent-Field/aforge-v2/internal/lane/lanestub"
)

func disconnectedDNS() error {
	return &net.DNSError{Err: "no such host", Name: "router.test", IsNotFound: true}
}

func TestConnectionRecoveryProbesWithoutGenerationAndResumes(t *testing.T) {
	var posts, heads atomic.Int32
	client, err := NewClient(Config{APIKey: "private-test-key", BaseURL: "https://router.test/api/v1", Model: "test/model",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.Method == http.MethodHead {
				if r.URL.Path != "/" || r.URL.RawQuery != "" || r.Header.Get("Authorization") != "" || r.Body != nil {
					t.Errorf("reachability check carried request material: %s %s, headers %v", r.Method, r.URL, r.Header)
				}
				if heads.Add(1) < 4 {
					return nil, disconnectedDNS()
				}
				return &http.Response{StatusCode: 401, Body: io.NopCloser(strings.NewReader(""))}, nil
			}
			if posts.Add(1) == 1 {
				return nil, disconnectedDNS()
			}
			if heads.Load() != 4 {
				t.Error("generation retried before reachability returned")
			}
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"role":"assistant","content":"back"},"finish_reason":"stop"}]}`))}, nil
		})}})
	if err != nil {
		t.Fatal(err)
	}
	var waits atomic.Int32
	client.wait = func(_ context.Context, d time.Duration) error {
		waits.Add(1)
		if d < connectionProbeInterval || d >= connectionProbeInterval*3/2 {
			t.Errorf("unbounded recovery cadence: %s", d)
		}
		return nil
	}
	response, err := client.CompleteWithMessages(context.Background(), userMessages("hello"))
	if err != nil || responseText(response) != "back" {
		t.Fatalf("recovery: %v, %v", response, err)
	}
	if posts.Load() != 2 || heads.Load() != 4 || waits.Load() != 3 {
		t.Fatalf("posts=%d heads=%d waits=%d; want only one failed send and one accepted generation", posts.Load(), heads.Load(), waits.Load())
	}
}

func TestConnectionRecoverySharesProbeAndKeepsOtherWaiters(t *testing.T) {
	client, _ := NewClient(Config{APIKey: "k", BaseURL: "https://router.test", Model: "test/model"})
	entered, release := make(chan struct{}), make(chan struct{})
	var probes atomic.Int32
	client.connectionProbe = func(ctx context.Context, _ string) error {
		if probes.Add(1) == 1 {
			close(entered)
		}
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	first, cancel := context.WithCancel(context.Background())
	defer cancel()
	doneFirst, doneSecond := make(chan error, 1), make(chan error, 1)
	go func() {
		_, err := client.waitConnection(first, "test/model", "https://router.test", true)
		doneFirst <- err
	}()
	<-entered
	second, stopSecond := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopSecond()
	go func() {
		_, err := client.waitConnection(second, "test/model", "https://router.test", false)
		doneSecond <- err
	}()
	// Wait for admission rather than guessing that the second goroutine ran.
	for {
		client.connection.mu.Lock()
		n := client.connection.active.waiters
		client.connection.mu.Unlock()
		if n == 2 {
			break
		}
		select {
		case <-second.Done():
			t.Fatal("second waiter was never admitted")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	cancel()
	if err := <-doneFirst; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	select {
	case err := <-doneSecond:
		t.Fatalf("one waiter canceled another: %v", err)
	default:
	}
	close(release)
	if err := <-doneSecond; err != nil {
		t.Fatal(err)
	}
	if probes.Load() != 1 {
		t.Fatalf("shared outage opened %d probes", probes.Load())
	}
}

func TestConnectionRecoveryLastWaiterCancelsProbe(t *testing.T) {
	client, _ := NewClient(Config{APIKey: "k", BaseURL: "https://router.test", Model: "test/model"})
	entered, left := make(chan struct{}), make(chan struct{})
	client.connectionProbe = func(ctx context.Context, _ string) error { close(entered); <-ctx.Done(); close(left); return ctx.Err() }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := client.waitConnection(ctx, "test/model", "https://router.test", true); done <- err }()
	<-entered
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	select {
	case <-left:
	case <-time.After(5 * time.Second):
		t.Fatal("last waiter left a live probe")
	}
}

func TestConnectionRecoveryDoesNotProbeHealthyOrPermanentResponses(t *testing.T) {
	for _, status := range []int{200, 400, 401, 403} {
		client, _ := NewClient(Config{APIKey: "k", BaseURL: "https://router.test", Model: "test/model", HTTPClient: handlerClient(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`))
		}))})
		client.connectionProbe = func(context.Context, string) error {
			t.Error("healthy/permanent HTTP response triggered connectivity probe")
			return nil
		}
		client.wait = func(context.Context, time.Duration) error { return nil }
		_, _ = client.CompleteWithMessages(context.Background(), userMessages("hello"))
	}
}

func TestConnectionRecoveryDoesNotTreatResponseLossAsPreSendFailure(t *testing.T) {
	if !connectionFailure(disconnectedDNS()) || !connectionFailure(&net.OpError{Op: "dial", Err: errors.New("refused")}) {
		t.Fatal("connection failures were not recognized")
	}
	for _, err := range []error{io.EOF, io.ErrUnexpectedEOF, context.Canceled, context.DeadlineExceeded, &net.OpError{Op: "read", Err: errors.New("reset")}, x509.UnknownAuthorityError{}} {
		if connectionFailure(err) {
			t.Fatalf("ambiguous/permanent error entered pre-send recovery: %v", err)
		}
	}
}

func TestConnectionProbeDoesNotFollowRedirectOrForwardCredentials(t *testing.T) {
	var calls int
	client, _ := NewClient(Config{APIKey: "k", BaseURL: "https://router.test", Model: "test/model", HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.String() != "https://router.test/" || len(r.Header) != 0 || r.Body != nil {
			t.Errorf("probe leaked original URL or request: %+v", r)
		}
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://elsewhere.test/"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
	})}})
	if err := client.probeConnection(context.Background(), "https://user:secret@router.test/api/v1?key=secret#fragment"); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("probe followed redirect: %d calls", calls)
	}
}

func TestMediaConnectionRecoveryDoesNotResubmitAnAcceptedJob(t *testing.T) {
	for _, lostResponse := range []bool{false, true} {
		var posts, accepted atomic.Int32
		client, _ := NewMediaClient(Config{APIKey: "k", BaseURL: "https://router.test/api/v1", HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.Method == http.MethodHead {
				return &http.Response{StatusCode: 405, Body: io.NopCloser(strings.NewReader(""))}, nil
			}
			if posts.Add(1) == 1 && !lostResponse {
				return nil, disconnectedDNS()
			}
			accepted.Add(1)
			if lostResponse {
				return nil, &net.OpError{Op: "read", Err: io.ErrUnexpectedEOF}
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"id":"one-job"}`))}, nil
		})}})
		response, err := client.doEndpoint(context.Background(), http.MethodPost, "https://router.test/api/v1/videos", []byte(`{"prompt":"a scene"}`), true)
		if response != nil {
			response.Body.Close()
		}
		if lostResponse && err == nil || !lostResponse && err != nil {
			t.Fatalf("lost=%v error=%v", lostResponse, err)
		}
		if accepted.Load() != 1 {
			t.Fatalf("lost=%v accepted %d jobs", lostResponse, accepted.Load())
		}
	}
}

func TestConnectionRecoveryExpiredCallerDoesNotStartAProbe(t *testing.T) {
	client, _ := NewClient(Config{APIKey: "k", BaseURL: "https://router.test", Model: "test/model"})
	client.connectionProbe = func(context.Context, string) error { t.Error("expired call started a probe"); return nil }
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	if _, err := client.waitConnection(ctx, "test/model", "https://router.test", true); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}

func TestConnectionRecoveryCertificateFailureIsNotWaitedOut(t *testing.T) {
	client, _ := NewClient(Config{APIKey: "k", BaseURL: "https://router.test", Model: "test/model"})
	client.connectionProbe = func(context.Context, string) error { return x509.UnknownAuthorityError{} }
	client.wait = func(context.Context, time.Duration) error { t.Error("certificate failure was retried"); return nil }
	_, err := client.waitConnection(context.Background(), "test/model", "https://router.test", true)
	var unknown x509.UnknownAuthorityError
	if !errors.As(err, &unknown) {
		t.Fatalf("certificate error lost: %v", err)
	}
}

func TestMediaRedirectDNSFailureDoesNotReplayOriginalSubmission(t *testing.T) {
	var accepted atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		accepted.Add(1)
		http.Redirect(w, r, "http://offline.invalid/result", http.StatusFound)
	}))
	defer server.Close()
	transport := &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		if strings.HasPrefix(address, "offline.invalid:") {
			return nil, disconnectedDNS()
		}
		return (&net.Dialer{}).DialContext(ctx, network, address)
	}}
	defer transport.CloseIdleConnections()
	client, _ := NewMediaClient(Config{APIKey: "k", BaseURL: server.URL, HTTPClient: &http.Client{Transport: transport}})
	client.connection.connectionProbe = func(context.Context, string) error {
		t.Error("a redirected accepted request entered pre-send recovery")
		return nil
	}
	_, err := client.do(context.Background(), "/videos", []byte(`{"prompt":"scene"}`))
	if err == nil || accepted.Load() != 1 {
		t.Fatalf("err=%v accepted=%d", err, accepted.Load())
	}
}

func TestConnectionRecoveryPausesRoutingAndDoesNotTeachProviderLatency(t *testing.T) {
	rig := newLaneRig(t, "connection/recovery",
		lanestub.Lane{Name: "A", Profile: lanestub.Profile{TTFT: time.Millisecond, Rate: 2000, Tokens: 24}},
		lanestub.Lane{Name: "B", Profile: lanestub.Profile{TTFT: time.Millisecond, Rate: 2000, Tokens: 24}},
	)
	original := rig.client.stream.Transport
	var posts atomic.Int32
	watches := make(chan *streamWatch, 1)
	rig.client.stream.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if posts.Add(1) == 1 {
			watches <- streamWatchFrom(r.Context())
			return nil, disconnectedDNS()
		}
		return original.RoundTrip(r)
	})
	released := make(chan struct{})
	rig.client.connectionProbe = func(ctx context.Context, _ string) error {
		select {
		case <-released:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	ctx, cancel := context.WithTimeout(talking(), 5*time.Second)
	defer cancel()
	report := &HedgeReport{}
	ctx = WithLaneChoice(WithHedgeReport(ctx, report), choiceFor(rig.model, 12*time.Millisecond))
	done := make(chan error, 1)
	go func() { _, err := rig.client.CompleteWithMessages(ctx, userMessages("hello")); done <- err }()
	var watch *streamWatch
	select {
	case watch = <-watches:
	case <-ctx.Done():
		t.Fatal("request did not start")
	}
	if watch == nil {
		t.Fatal("request bypassed routing controller")
	}
	waitFor(t, func() bool { watch.mu.Lock(); defer watch.mu.Unlock(); return watch.recovering })
	// Advance the controller far beyond its normal deadline while disconnected.
	watch.quiet(waitNow().Add(5 * time.Minute))
	watch.mu.Lock()
	paused, action := watch.deadline.IsZero(), watch.acted.Kind
	watch.mu.Unlock()
	if !paused || action != control.None || posts.Load() != 1 {
		t.Fatalf("offline routing acted: paused=%v action=%v sends=%d", paused, action, posts.Load())
	}
	close(released)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if posts.Load() != 2 || report.Hedged() {
		t.Fatalf("recovery duplicated generation: sends=%d hedge=%v", posts.Load(), report.Hedged())
	}
	if got := rig.ledger.noted(); len(got) != 0 {
		t.Fatalf("offline call taught provider timing: %+v", got)
	}
	if _, ok := watch.sighting(rig.model, 24); ok {
		t.Fatal("offline arm admitted as a latency sighting")
	}
}
