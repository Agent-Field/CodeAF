//go:build !windows

// Package orclient is the OpenRouter streaming client: it assembles the
// chat-completions request body and headers, decodes the SSE response into
// stream parts (text, reasoning, tool calls, usage, finish), validates and
// repairs tool calls against the registered tool set, and registers the
// outcome of each call with the adaptive router.
//
// One DoStream call is one HTTP request. Nothing here loops, retries,
// executes a tool or touches session state; the caller owns all four. Early
// teardown cancels the request context rather than merely closing the body,
// so an abandoned stream never holds its connection open.
package orclient

import (
	"math/rand/v2"
	"sync"
	"time"
)

// ── seams ─────────────────────────────────────────────────────────────────

var seamMu sync.Mutex

// random is the id generator's randomness source; generateId draws one
// number per character.
var random func() float64 = defaultRandom

// nowMS is the millisecond clock.
var nowMS func() float64 = func() float64 { return float64(time.Now().UnixMilli()) }

// SetRandomForTesting swaps the randomness source. Returns a restore func.
func SetRandomForTesting(f func() float64) func() {
	seamMu.Lock()
	prev := random
	random = f
	seamMu.Unlock()
	return func() {
		seamMu.Lock()
		random = prev
		seamMu.Unlock()
	}
}

// SetNowForTesting swaps the clock. Returns a restore func.
func SetNowForTesting(f func() float64) func() {
	seamMu.Lock()
	prev := nowMS
	nowMS = f
	seamMu.Unlock()
	return func() {
		seamMu.Lock()
		nowMS = prev
		seamMu.Unlock()
	}
}

func currentRandom() func() float64 {
	seamMu.Lock()
	defer seamMu.Unlock()
	return random
}

func currentNow() func() float64 {
	seamMu.Lock()
	defer seamMu.Unlock()
	return nowMS
}

// idAlphabet is the id character set.
const idAlphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// idSize is the id length.
const idSize = 16

// generateId mints a 16-character id from the alphabet above, drawing one
// random number per character.
func generateId() string {
	draw := currentRandom()
	out := make([]byte, 0, idSize)
	for i := 0; i < idSize; i++ {
		idx := int(draw() * float64(len(idAlphabet)))
		if idx < 0 || idx >= len(idAlphabet) {
			continue
		}
		out = append(out, idAlphabet[idx])
	}
	return string(out)
}

func defaultRandom() float64 { return rand.Float64() }
