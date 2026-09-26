//go:build !windows

package steploop

import (
	"crypto/rand"
	"fmt"
	"sync"
	"time"
)

var (
	seamMu sync.Mutex
	nowMS  = func() uint64 { return uint64(time.Now().UnixMilli()) }
	idFunc = defaultID

	idMu      sync.Mutex
	idLastMS  uint64
	idCounter uint64
)

// SetNowForTesting swaps the millisecond clock and returns a restore closure.
func SetNowForTesting(f func() uint64) func() {
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

// SetIDFactoryForTesting swaps MessageID/PartID ascending generation. Prefix
// is "msg" or "prt". It returns a restore closure.
func SetIDFactoryForTesting(f func(prefix string) string) func() {
	seamMu.Lock()
	prev := idFunc
	idFunc = f
	seamMu.Unlock()
	return func() {
		seamMu.Lock()
		idFunc = prev
		seamMu.Unlock()
	}
}

func currentNow() uint64 {
	seamMu.Lock()
	f := nowMS
	seamMu.Unlock()
	return f()
}

func nextID(prefix string) string {
	seamMu.Lock()
	f := idFunc
	seamMu.Unlock()
	return f(prefix)
}

// NewAscendingID lets production adapters mint session-layer records from the
// same ordered sequence as loop-owned messages and parts.
func NewAscendingID(prefix string) string {
	return nextID(prefix)
}

// defaultID mints an ascending ID: the prefix, six bytes of packed
// millisecond time and per-millisecond counter, then 14 random base-62
// characters (one crypto byte modulo 62 each).
func defaultID(prefix string) string {
	idMu.Lock()
	defer idMu.Unlock()
	ms := currentNow()
	if ms != idLastMS {
		idLastMS = ms
		idCounter = 0
	}
	idCounter++
	packed := ms*0x1000 + idCounter
	random := make([]byte, 14)
	if _, err := rand.Read(random); err != nil {
		panic(err)
	}
	const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	for index := range random {
		random[index] = alphabet[int(random[index])%len(alphabet)]
	}
	return fmt.Sprintf("%s_%012x%s", prefix, packed&0xffffffffffff, string(random))
}
