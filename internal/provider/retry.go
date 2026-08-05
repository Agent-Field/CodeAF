package provider

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// Retry policy for the outbound call.
//
// One transient timeout on one node cost four of ten nodes in a real run: the
// node failed and three dependents were blocked behind it. Nothing about that
// was a planning or execution problem — the network hiccuped and a quarter of
// the work was thrown away. A bounded retry here is the smallest thing that
// makes a run survive its own infrastructure.
//
// This is deliberately a stopgap. Real hardening — per-provider budgets, circuit
// breaking, retry accounting — belongs in a client rewrite, not here.
const (
	maxAttempts  = 3
	baseBackoff  = 700 * time.Millisecond
	maxErrorPeek = 8 << 10
)

// send performs one request with retries, and returns a response whose body has
// not been read.
//
// The request is rebuilt on each attempt rather than reused: its body is a
// reader, and a retried request carrying a drained reader would silently post an
// empty document.
func (c *Client) send(ctx context.Context, request *ai.Request, body []byte, stream bool) (*http.Response, error) {
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			// Jittered, so several leaves that were rate-limited together do not
			// all come back at the same instant and trigger it again.
			delay := time.Duration(float64(baseBackoff) * float64(int(1)<<uint(attempt-1)))
			delay += time.Duration(rand.Int63n(int64(delay / 2)))
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}

		httpRequest, err := c.newHTTPRequest(ctx, request, body, stream)
		if err != nil {
			return nil, err
		}
		response, err := c.http.Do(httpRequest)
		if err != nil {
			// A cancelled or expired parent is a decision, not a fault. Retrying
			// it would burn the remaining deadline on calls that cannot land.
			if ctx.Err() != nil {
				return nil, fmt.Errorf("execute request: %w", err)
			}
			lastErr = fmt.Errorf("execute request: %w", err)
			continue
		}
		if !retryableStatus(response.StatusCode) {
			return response, nil
		}
		// Drain a bounded prefix before closing so the connection can be reused
		// and the eventual error still says what the provider complained about.
		peek, _ := io.ReadAll(io.LimitReader(response.Body, maxErrorPeek))
		response.Body.Close()
		lastErr = apiError(response.StatusCode, peek)
	}
	if lastErr == nil {
		lastErr = errors.New("request failed")
	}
	return nil, fmt.Errorf("after %d attempts: %w", maxAttempts, lastErr)
}

// retryableStatus separates "try again" from "this will never work". A 4xx other
// than 429 is a request we built wrong, and repeating it just spends the
// deadline three times over.
func retryableStatus(status int) bool {
	return status == http.StatusTooManyRequests || status >= 500
}
