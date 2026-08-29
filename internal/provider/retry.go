package provider

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/calllog"
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
	//
	// IT IS THE PATIENCE OF A WATCHED CALL, and only that. A person is sitting
	// in front of a conversation's turn, and six attempts is roughly where
	// waiting stops being kinder than an error they can act on. A call made
	// under [WithPatientRateLimits] — a task node's, where nobody is watching
	// and a worktree of work is at stake — gets patientAttempts instead; see
	// patience.go for why the two are different answers to the same 429.
	rateLimitAttempts = 6
	// patientAttempts is the ceiling on a patient call, and it exists because
	// "waits it out however long that takes" was written as a loop with no exit
	// but the context's. Sixty attempts against the per-wait cap below is an
	// hour of pacing, so in practice patientPacingBudget is what ends a patient
	// call and this is the arithmetic backstop for the degenerate case: a
	// provider answering 429 with no delay at all, where the wall clock barely
	// moves and only a count is finite.
	patientAttempts = 60
	// watchedPacingBudget and patientPacingBudget are the WALL CLOCK a call may
	// spend held by pacing, measured from its first 429. A count of attempts
	// does not bound time when the provider names the waits: six attempts each
	// told to come back in a minute is five minutes of a person watching a
	// cursor, which no number of attempts can express.
	//
	// Two minutes is past the point where a watched turn should have said
	// something. Ten is the unwatched answer: a task node's work is worth
	// waiting for, and a burst clears in seconds, but ten unbroken minutes of
	// 429 is not a burst — it is an account that cannot serve this work now,
	// and a node that says so beats a node that sits.
	watchedPacingBudget = 2 * time.Minute
	patientPacingBudget = 10 * time.Minute
	baseBackoff         = 700 * time.Millisecond
	maxErrorPeek        = 8 << 10
	// maxBackoffShift caps the exponent, not the patience. A patient call may
	// take its hundredth attempt, and `1 << 99` is not a long wait — it is an
	// overflow, and an overflowed duration is a negative one. Seven doublings
	// already put the computed delay past maxProviderWait, which is where every
	// wait lands anyway, so clamping here changes nothing about a bounded call
	// and makes an unbounded one arithmetic rather than undefined.
	maxBackoffShift = 8
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
func (c *Client) send(ctx context.Context, request *ai.Request, knobs callKnobs, body []byte, stream bool) (*http.Response, error) {
	var lastErr error
	// The wait is sized for the reply the request PERMITS — the caller's answer
	// plus the thinking pass's room — and not for the caller's figure alone.
	// Sized from the figure, an always-thinking model was allowed to generate
	// for longer than the transport would wait, and the empty-answer failure
	// came back as a timeout.
	ceiling, _ := c.ceilingFor(request, knobs)
	httpClient := c.clientFor(stream, ceiling)
	// providerWait is the provider's own comeback instruction from the last
	// 429 (Retry-After); it outranks our computed backoff for the one attempt
	// it was issued for, and is then spent.
	var providerWait time.Duration
	// Whether this call waits pacing out or gives up on it, and who is told
	// while it waits (patience.go). Both ride the context because the adapter
	// underneath is shared by every agent in the process.
	patient := patientRateLimits(ctx)
	notice := pacingNoticeFrom(ctx)
	// parked says this call has already announced that it is waiting on the
	// provider. It is announced ONCE per call and taken back on EVERY exit —
	// through, or given up — because a surface left holding "still waiting"
	// for a call that has landed is worse than one that was never told.
	parked := false
	defer func() {
		if parked && notice != nil {
			notice(false)
		}
	}()
	// pacedSince is when this call FIRST drew a 429, and zero until it does. It
	// is what the pacing budget is measured against, so a call that spent four
	// minutes doing real work and then met one 429 has its full patience, and a
	// call that has been held from the start does not.
	var pacedSince time.Time
	attempts := 0
	// The body this call is carrying, kept for the model-call log and ONLY when
	// somebody asked for bodies (calllog.go). On every ordinary run this is nil
	// and the person's prompts never leave the process.
	if knobs.trace != nil && calllog.Bodies() {
		knobs.trace.body = body
	}
	// Rate limits get more patience than faults: they are the provider
	// pacing us, not failing, and abandoning work over pacing is the one
	// outcome the concurrency doctrine forbids. A patient call takes that much
	// further — a whole order of magnitude of it — and every other class of
	// failure below keeps the short patience it always had. But EVERY call ends:
	// patience that cannot be spent is a turn that can be held hostage by an
	// account somebody else is saturating (outOfPatience).
	for attempt := 0; ; attempt++ {
		if attempt > 0 && outOfPatience(patient, attempt, pacedSince) {
			break
		}
		attempts = attempt + 1
		// What the CALL has spent, for the row the completed answer writes at
		// the end of it (calllog.go). The count lives on the knobs rather than
		// here because a call that is repaired or relaxed comes back through
		// this loop with a new body and the same trace.
		knobs.trace.begin()
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
		attemptBegan := logNow()
		// THE ROW THAT SAYS A CALL IS IN FLIGHT, written before the wait rather
		// than after it. Without it a planning call four minutes into a
		// 65,536-token ceiling is indistinguishable from an idle process: the
		// log's newest line is the call BEFORE it, and everything a person can
		// see says "nothing since four minutes ago". Its partner is whichever
		// row ends this attempt, paired by the id they share.
		c.record(recordFacts{
			ctx: ctx, request: request, knobs: knobs, stream: stream,
			attempt: attempts, began: attemptBegan, phase: calllog.PhaseStart,
		})
		response, err := httpClient.Do(httpRequest)
		if err != nil {
			sharedLimiter.release(false, 0)
			cancelAttempt()
			// EVERY ATTEMPT THAT FAILED LEAVES A ROW, not only the last one. A
			// call that was paced four times and then landed reads as one slow
			// call in any surface above this; the four rows are the whole of
			// why it was slow (calllog.go).
			c.record(recordFacts{
				ctx: ctx, request: request, knobs: knobs, stream: stream,
				attempt: attempts, began: attemptBegan, err: err,
			})
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
		// The provider's comeback instruction is read before the slot goes back,
		// because it is what tells the limiter how wide this 429's window is:
		// one window, one halving (limiter.go).
		var named time.Duration
		if rateLimited {
			named = retryAfter(response)
		}
		sharedLimiter.release(rateLimited, named)
		if !retryableStatus(response.StatusCode) {
			if stream {
				response.Body = newIdleWatchdog(response.Body, streamIdleTimeout, cancelAttempt)
			}
			return response, nil
		}
		if rateLimited {
			providerWait = named
			if pacedSince.IsZero() {
				pacedSince = time.Now()
			}
			// The park begins on the FIRST 429 this call draws, not on the
			// first one it decides to wait out: by the time the backoff is
			// computed the call is already not moving, and that is the fact
			// anybody watching wants.
			if !parked && notice != nil {
				parked = true
				notice(true)
			}
		}
		// Drain a bounded prefix before closing so the connection can be reused
		// and the eventual error still says what the provider complained about.
		peek, _ := io.ReadAll(io.LimitReader(response.Body, maxErrorPeek))
		response.Body.Close()
		cancelAttempt()
		if rateLimited {
			// A 429 that names its upstream is one provider's pool, not this
			// account: the ledger refuses that lane so every request encoded
			// from here on routes around it. This call's own retries keep the
			// body they were built with and wait as they always did.
			if served := pacedProviderName(peek); served != "" {
				c.notePacedProvider(c.modelFor(request), served, named)
			}
		}
		lastErr = apiError(response.StatusCode, peek)
		c.record(recordFacts{
			ctx: ctx, request: request, knobs: knobs, stream: stream,
			attempt: attempts, began: attemptBegan,
			status: response.StatusCode, err: lastErr, responseBody: peek,
		})
		// A 5xx that names its upstream is that upstream failing, not this model:
		// the lane goes so the next encode routes around it (velocity.go's
		// refuseUpstream). A 429 was already answered above with the wait the
		// provider itself named, and this leaves it alone.
		c.refuseUpstream(c.modelFor(request), lastErr)
		// Non-rate-limit faults keep the original, shorter patience.
		if !rateLimited && attempt >= maxAttempts-1 {
			break
		}
	}
	if lastErr == nil {
		lastErr = errors.New("request failed")
	}
	// The count is what this call actually spent rather than the constant it
	// was bounded by: a patient call has no constant to name, and a fault that
	// broke out after three attempts never had six.
	return nil, fmt.Errorf("after %d attempts: %w", attempts, lastErr)
}

// outOfPatience reports whether this call has spent everything it is willing to
// spend on being paced, and it is the ONE PLACE either kind of call gives up on
// a 429.
//
// Two currencies, because each bounds what the other cannot. Attempts bound a
// provider that says "not yet" instantly and forever, where no amount of
// retrying moves a clock. The wall clock bounds a provider that names its own
// waits, where six attempts can be five minutes. A call is done when it runs
// out of either.
//
// It says nothing about faults: a timeout or a 500 keeps the short patience it
// always had, bounded by maxAttempts at the call site.
func outOfPatience(patient bool, attempt int, pacedSince time.Time) bool {
	attemptLimit, budget := rateLimitAttempts, watchedPacingBudget
	if patient {
		attemptLimit, budget = patientAttempts, patientPacingBudget
	}
	if attempt >= attemptLimit {
		return true
	}
	return !pacedSince.IsZero() && time.Since(pacedSince) >= budget
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
//
// ONE WAIT IS NEVER LONGER THAN maxProviderWait, whatever asked for it — the
// header, or the doubling. A patient call (patience.go) has no attempt ceiling
// to keep the exponent small, so the cap is what keeps its waits a minute
// apart instead of an hour, and it is what makes an interrupt land promptly:
// the longest a stopped call can still be sitting in a timer is one of these.
func backoffFor(attempt int, providerWait time.Duration) time.Duration {
	if attempt > maxBackoffShift {
		attempt = maxBackoffShift
	}
	delay := time.Duration(float64(baseBackoff) * float64(int(1)<<uint(attempt-1)))
	delay += time.Duration(rand.Int63n(int64(delay / 2)))
	if providerWait > maxProviderWait {
		providerWait = maxProviderWait
	}
	if providerWait > delay {
		delay = providerWait
	}
	if delay > maxProviderWait {
		delay = maxProviderWait
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
