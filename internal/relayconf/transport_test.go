//go:build relayurl

package relayconf

import (
	"net/http"
	"os"
	"strconv"
)

// workerTransport bounds requests to the local development proxy without
// changing canonical assertions or the lifetime of upgraded watch sockets.
type workerTransport struct {
	base  http.RoundTripper
	slots chan struct{}
}

func conformanceTransport() http.RoundTripper {
	limit, err := strconv.Atoi(os.Getenv("CODEAF_RELAYCONF_CONCURRENCY"))
	if err != nil || limit <= 0 {
		return http.DefaultTransport
	}
	base := http.DefaultTransport.(*http.Transport).Clone()
	base.MaxIdleConnsPerHost = limit
	return &workerTransport{base: base, slots: make(chan struct{}, limit)}
}

func (w *workerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	select {
	case w.slots <- struct{}{}:
		defer func() { <-w.slots }()
	case <-req.Context().Done():
		return nil, req.Context().Err()
	}
	return w.base.RoundTrip(req)
}
