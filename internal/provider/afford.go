package provider

import (
	"regexp"
	"strconv"
	"sync/atomic"

	"github.com/Agent-Field/codeaf/internal/guard"
)

var affordableWords = regexp.MustCompile(`(?i)can only afford\s+([1-9][0-9]*)`)

// affordableTokens reads only a positive figure the vendor explicitly said it
// could reserve. An unrelated number in a 402 is not permission to retry.
func affordableTokens(body []byte) int {
	match := affordableWords.FindSubmatch(body)
	if len(match) < 2 {
		return 0
	}
	value, err := strconv.Atoi(string(match[1]))
	if err != nil {
		return 0
	}
	return value
}

type paymentRequiredHook struct{ call func(string) }

var paymentRequired atomic.Pointer[paymentRequiredHook]

// SetPaymentRequiredHook connects any client's 402 to the local chat door.
// The returned function removes only this registration when that door closes.
func SetPaymentRequiredHook(call func(string)) func() {
	if call == nil {
		return func() {}
	}
	hook := &paymentRequiredHook{call: call}
	paymentRequired.Store(hook)
	return func() { paymentRequired.CompareAndSwap(hook, nil) }
}

func announcePaymentRequired(base string) {
	if hook := paymentRequired.Load(); hook != nil {
		// A provider attempt never waits for the surface or its balance endpoint.
		guard.Go("provider.payment-required", func() { hook.call(base) })
	}
}
