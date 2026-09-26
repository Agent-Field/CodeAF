//go:build !windows

package orclient

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/seniordev/router/adaptive"
)

func TestCallerCancellationNeutralBeforeHeadersAndOnUndrainedClose(t *testing.T) {
	for _, closeOnly := range []bool{false, true} {
		t.Run(map[bool]string{false: "before-headers", true: "undrained-close"}[closeOnly], func(t *testing.T) {
			ctx, cancel := context.WithCancelCause(context.Background())
			defer cancel(nil)
			cause := errors.New("caller gave up")
			router := &spyRouter{inflight: 1}
			client := &Client{BaseURL: testBaseURL, Router: router, RouteChoice: &adaptive.RouteChoice{}, Fetcher: func(req *http.Request) (*http.Response, error) {
				if !closeOnly {
					cancel(cause)
					return nil, context.Cause(req.Context())
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(""))}, nil
			}}
			stream, err := client.DoStream(ctx, RequestParams{ModelID: "test"})
			if closeOnly {
				if err != nil {
					t.Fatal(err)
				}
				cancel(cause)
				if err = stream.Close(); err != nil {
					t.Fatal(err)
				}
				if err = stream.Close(); err != nil {
					t.Fatal(err)
				}
			} else if !errors.Is(err, cause) {
				t.Fatalf("err=%v", err)
			}
			if router.canceled != 1 || len(router.calls) != 0 || router.inflight != 0 {
				t.Fatalf("canceled=%d calls=%d inflight=%d", router.canceled, len(router.calls), router.inflight)
			}
		})
	}
}

func TestProviderFailureIsNotHiddenByLaterCallerCancellation(t *testing.T) {
	for _, watchdog := range []bool{false, true} {
		t.Run(map[bool]string{false: "http-502", true: "watchdog-first"}[watchdog], func(t *testing.T) {
			ctx, cancel := context.WithCancelCause(context.Background())
			defer cancel(nil)
			router := &spyRouter{inflight: 1}
			var fire func()
			t.Cleanup(SetTimerFactoryForTesting(func(_ float64, fn func()) Timer { fire = fn; return &cancellationTestTimer{} }))
			client := &Client{BaseURL: testBaseURL, Router: router, RouteChoice: &adaptive.RouteChoice{}, Fetcher: func(req *http.Request) (*http.Response, error) {
				if watchdog {
					fire()
					cancel(errors.New("caller gave up"))
					return nil, context.Cause(req.Context())
				}
				cancel(errors.New("caller gave up"))
				return &http.Response{StatusCode: 502, Body: io.NopCloser(strings.NewReader("provider unavailable"))}, nil
			}}
			if _, err := client.DoStream(ctx, RequestParams{ModelID: "test"}); err == nil {
				t.Fatal("provider failure suppressed")
			}
			if router.canceled != 0 || len(router.calls) != 1 || router.calls[0].err == nil || router.inflight != 0 {
				t.Fatalf("canceled=%d calls=%+v inflight=%d", router.canceled, router.calls, router.inflight)
			}
			if watchdog && !adaptive.IsLikelyTimeout(router.calls[0].err) {
				t.Fatal("watchdog timeout taxonomy changed")
			}
		})
	}
}

type cancellationTestTimer struct{}

func (*cancellationTestTimer) Stop() {}

func TestCompletedSuccessWinsOverLaterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	router := &spyRouter{inflight: 1}
	client := &Client{BaseURL: testBaseURL, Router: router, RouteChoice: &adaptive.RouteChoice{}, Fetcher: func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("data: [DONE]\n\n"))}, nil
	}}
	stream, err := client.DoStream(ctx, RequestParams{ModelID: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = stream.Parts(); err != nil {
		t.Fatal(err)
	}
	cancel(errors.New("caller gave up"))
	if err = stream.Close(); err != nil {
		t.Fatal(err)
	}
	if err = stream.Close(); err != nil {
		t.Fatal(err)
	}
	if router.canceled != 0 || len(router.calls) != 1 || router.calls[0].err != nil || router.inflight != 0 {
		t.Fatalf("completed success reclassified: canceled=%d calls=%+v inflight=%d", router.canceled, router.calls, router.inflight)
	}
}

func TestActualParentDeadlineAndMidstreamAbortAreNeutral(t *testing.T) {
	for _, midstream := range []bool{false, true} {
		t.Run(map[bool]string{false: "deadline-before-headers", true: "deadline-in-stream"}[midstream], func(t *testing.T) {
			ctx, cancel := context.WithTimeoutCause(context.Background(), 10*time.Millisecond, errors.New("caller deadline"))
			defer cancel()
			router := &spyRouter{inflight: 1}
			client := &Client{BaseURL: testBaseURL, Router: router, RouteChoice: &adaptive.RouteChoice{}, Fetcher: func(req *http.Request) (*http.Response, error) {
				if midstream {
					return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(""))}, nil
				}
				<-req.Context().Done()
				return nil, req.Context().Err()
			}}
			stream, err := client.DoStream(ctx, RequestParams{ModelID: "test"})
			if midstream {
				if err != nil {
					t.Fatal(err)
				}
				<-ctx.Done()
				parts, readErr := stream.Parts()
				if readErr != nil || len(parts) != 1 {
					t.Fatalf("parts=%+v err=%v", parts, readErr)
				}
				if _, ok := parts[0].(AbortPart); !ok {
					t.Fatalf("part=%T", parts[0])
				}
				_ = stream.Close()
			} else if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("err=%v", err)
			}
			if router.canceled != 1 || len(router.calls) != 0 || router.inflight != 0 {
				t.Fatalf("canceled=%d calls=%d inflight=%d", router.canceled, len(router.calls), router.inflight)
			}
		})
	}
}
