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

// waitContext is in media.go: one timer, stopped on the way out, so a retry
// loop that returns early does not leave a timer behind for the length of the
// backoff it abandoned.

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
	maxAttempts = 3
	// rateLimitAttempts is the patience for 429s specifically: the provider
	// pacing us is not a fault. Retry-After raises the wait rather than
	// bounding it — it is the provider's floor, not its ceiling — so the
	// ceiling is ours: maxProviderWait.
	rateLimitAttempts = 6
	baseBackoff       = 700 * time.Millisecond
	maxErrorPeek      = 8 << 10
	// maxProviderWait caps what a Retry-After may ask for. The header may be an
	// HTTP date, and a provider that names tomorrow morning is asking a call to
	// sleep for hours inside a run the user is watching. A minute is already
	// far past the point where waiting beats failing and saying so.
	maxProviderWait = time.Minute
)

// send performs one request with retries, and returns a response whose body has
// not been read.
//
// The request is rebuilt on each attempt rather than reused: its body is a
// reader, and a retried request carrying a drained reader would silently post an
// empty document.
func (c *Client) send(ctx context.Context, request *ai.Request, body []byte, stream bool) (*http.Response, error) {
	var lastErr error
	maxTokens := 0
	if request.MaxTokens != nil {
		maxTokens = *request.MaxTokens
	}
	httpClient := c.clientFor(stream, maxTokens)
	// providerWait is the provider's own comeback instruction from the last
	// 429 (Retry-After); it outranks our computed backoff for the one attempt
	// it was issued for, and is then spent.
	var providerWait time.Duration
	// Rate limits get more patience than faults: they are the provider
	// pacing us, not failing, and abandoning work over pacing is the one
	// outcome the concurrency doctrine forbids.
	for attempt := 0; attempt < rateLimitAttempts; attempt++ {
		if attempt > 0 {
			delay := backoffFor(attempt, providerWait)
			// Spent. It described one moment to come back at, and coming back
			// is what we are doing; carrying it forward made a single 429 set
			// the floor for every remaining attempt of the call.
			providerWait = 0
			if err := c.wait(ctx, delay); err != nil {
				return nil, err
			}
		}

		// A stream gets its own cancellable context so the idle watchdog has
		// something to pull. Every path that does not hand the body back to the
		// caller releases it here; the path that does hands the cancel to the
		// watchdog, which fires it on stall or on Close.
		attemptCtx, cancelAttempt := attemptContext(ctx, stream)

		httpRequest, err := c.newHTTPRequest(attemptCtx, request, body, stream)
		if err != nil {
			cancelAttempt()
			return nil, err
		}
		if err := sharedLimiter.acquire(ctx); err != nil {
			cancelAttempt()
			return nil, err
		}
		response, err := httpClient.Do(httpRequest)
		if err != nil {
			sharedLimiter.release(false)
			cancelAttempt()
			// A cancelled or expired parent is a decision, not a fault. Retrying
			// it would burn the remaining deadline on calls that cannot land.
			if ctx.Err() != nil {
				return nil, fmt.Errorf("execute request: %w", err)
			}
			lastErr = fmt.Errorf("execute request: %w", err)
			if attempt >= maxAttempts-1 {
				break
			}
			continue
		}
		rateLimited := response.StatusCode == http.StatusTooManyRequests
		sharedLimiter.release(rateLimited)
		if !retryableStatus(response.StatusCode) {
			if stream {
				response.Body = newIdleWatchdog(response.Body, streamIdleTimeout, cancelAttempt)
			}
			return response, nil
		}
		if rateLimited {
			providerWait = retryAfter(response)
		}
		// Drain a bounded prefix before closing so the connection can be reused
		// and the eventual error still says what the provider complained about.
		peek, _ := io.ReadAll(io.LimitReader(response.Body, maxErrorPeek))
		response.Body.Close()
		cancelAttempt()
		lastErr = apiError(response.StatusCode, peek)
		// Non-rate-limit faults keep the original, shorter patience.
		if !rateLimited && attempt >= maxAttempts-1 {
			break
		}
	}
	if lastErr == nil {
		lastErr = errors.New("request failed")
	}
	return nil, fmt.Errorf("after %d attempts: %w", rateLimitAttempts, lastErr)
}

// backoffFor is how long to wait before one retry.
//
// Exponential and jittered, so several leaves that were rate-limited together
// do not all come back at the same instant and trigger it again. The provider's
// own Retry-After raises that wait when it named a later moment — it knows its
// own pacing better than the formula does — but only up to maxProviderWait.
// The header may be an HTTP date, and a date is how a wait becomes hours: the
// old code took it literally and applied it, and then went on applying it to
// every later attempt of the same call.
func backoffFor(attempt int, providerWait time.Duration) time.Duration {
	delay := time.Duration(float64(baseBackoff) * float64(int(1)<<uint(attempt-1)))
	delay += time.Duration(rand.Int63n(int64(delay / 2)))
	if providerWait > maxProviderWait {
		providerWait = maxProviderWait
	}
	if providerWait > delay {
		delay = providerWait
	}
	return delay
}

// clientFor picks how this call is bounded in time.
//
// A completion is bounded in total: the answer arrives in one piece, so the
// adaptive budget is a real statement about when the call has gone wrong. A
// stream is not bounded in total at all, because http.Client.Timeout covers
// body reads and the body reads are the answer — that deadline killed every
// stream at the budget however healthily it was delivering. What bounds a
// stream is silence: the header deadline on the streaming transport and the
// idle watchdog send wraps the body in.
func (c *Client) clientFor(stream bool, maxTokens int) http.Client {
	if stream {
		client := *c.stream
		client.Timeout = 0
		return client
	}
	client := *c.http
	client.Timeout = adaptiveCompletionTimeout(maxTokens, c.config.Timeout)
	return client
}

// attemptContext gives a streamed attempt a cancel of its own. Only a stream
// needs one: its body outlives send, so the thing that stops it has to be
// handed on to the watchdog rather than deferred here. A non-streamed attempt
// keeps the caller's context and gets a cancel that does nothing, which lets
// every exit path call it unconditionally.
func attemptContext(ctx context.Context, stream bool) (context.Context, context.CancelFunc) {
	if !stream {
		return ctx, func() {}
	}
	return context.WithCancel(ctx)
}

// retryableStatus separates "try again" from "this will never work". A 4xx other
// than 429 is a request we built wrong, and repeating it just spends the
// deadline three times over.
func retryableStatus(status int) bool {
	return status == http.StatusTooManyRequests || status >= 500
}
