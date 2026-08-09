// Transient HTTP read retry — port of src/util/effect-http-client.ts:1-10
// (swe-pro 3b25a1a).
package util

import (
	"bytes"
	"io"
	"math/rand"
	"net/http"
	"time"
)

type retryTransport struct {
	base http.RoundTripper
}

func (r retryTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	var body []byte
	if request.Body != nil && request.GetBody == nil {
		body, _ = io.ReadAll(request.Body)
		request.Body = io.NopCloser(bytes.NewReader(body))
	}
	for attempt := 0; attempt < 3; attempt++ {
		req := request
		if attempt > 0 {
			req = request.Clone(request.Context())
			if request.GetBody != nil {
				req.Body, _ = request.GetBody()
			} else if body != nil {
				req.Body = io.NopCloser(bytes.NewReader(body))
			}
		}
		response, err := r.base.RoundTrip(req)
		if err == nil && !transientResponse(response) {
			return response, nil
		}
		if attempt == 2 {
			return response, err
		}
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		delay := 200 * time.Millisecond * time.Duration(1<<attempt)
		jitter := time.Duration(rand.Float64() * float64(delay))
		timer := time.NewTimer(jitter)
		select {
		case <-request.Context().Done():
			timer.Stop()
			return nil, request.Context().Err()
		case <-timer.C:
		}
	}
	panic("unreachable")
}

func transientResponse(response *http.Response) bool {
	if response == nil {
		return true
	}
	return response.StatusCode == http.StatusRequestTimeout ||
		response.StatusCode == http.StatusTooManyRequests ||
		response.StatusCode >= 500
}

func WithTransientReadRetry(client *http.Client) *http.Client {
	if client == nil {
		client = &http.Client{}
	}
	copy := *client
	base := client.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	copy.Transport = retryTransport{base: base}
	return &copy
}
