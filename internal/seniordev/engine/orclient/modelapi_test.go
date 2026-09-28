//go:build !windows

package orclient

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/provider/modelapi"
	"github.com/Agent-Field/codeaf/internal/seniordev/router/adaptive"
)

// Every request goes to the route codeaf spells for the base URL it hands the
// run, whatever that base carries at its end: the client appends nothing of
// its own.
func TestARequestGoesToTheModelAPIsOwnRoute(t *testing.T) {
	for _, base := range []string{"http://127.0.0.1:4100/v1", "http://127.0.0.1:4100/v1/"} {
		var sent string
		client := &Client{BaseURL: base, Fetcher: func(req *http.Request) (*http.Response, error) {
			sent = req.URL.String()
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(""))}, nil
		}}
		stream, err := client.DoStream(context.Background(), RequestParams{ModelID: "vendor/model"})
		if err != nil {
			t.Fatal(err)
		}
		_ = stream.Close()
		if want := modelapi.ChatURL(base); sent != want {
			t.Fatalf("base %q sent the request to %q, want %q", base, sent, want)
		}
	}
}

// A client with no model API has nowhere to go, and says so rather than
// reaching for a default service. The route lease the router took for the
// call is settled on the way out, so a refused call never leaks an in-flight
// count.
func TestAClientWithNoModelAPIRefusesAndSettlesItsLease(t *testing.T) {
	router := &spyRouter{inflight: 1}
	fetched := false
	client := &Client{Router: router, RouteChoice: &adaptive.RouteChoice{}, Fetcher: func(*http.Request) (*http.Response, error) {
		fetched = true
		return nil, errors.New("unreachable")
	}}
	_, err := client.DoStream(context.Background(), RequestParams{ModelID: "vendor/model"})
	if !errors.Is(err, errNoModelAPI) {
		t.Fatalf("err = %v, want the missing model API", err)
	}
	if fetched {
		t.Fatal("a client with no model API sent a request anyway")
	}
	if router.inflight != 0 || len(router.calls) != 1 {
		t.Fatalf("the lease was not settled: inflight=%d calls=%d", router.inflight, len(router.calls))
	}
}
