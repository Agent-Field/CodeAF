package session

import (
	"errors"
	"fmt"
	"net"
	"os"
	"syscall"
	"testing"
)

// A TURN THAT NEVER LEFT THIS COMPUTER IS OFFLINE, said in a person's words.
// A refused dial, an unreachable network and a name that will not resolve are
// offline however deep the retry ladder wrapped them; a reset mid-answer is not.
func TestClassifyTurnErrorSortsAnUnreachableServiceAsOffline(t *testing.T) {
	dial := func(errno syscall.Errno) error {
		return &net.OpError{Op: "dial", Net: "tcp", Err: os.NewSyscallError("connect", errno)}
	}
	for name, err := range map[string]error{
		"refused":     fmt.Errorf("after 0 retries: %w", fmt.Errorf("after 8 attempts: execute request: %w", dial(syscall.ECONNREFUSED))),
		"unreachable": dial(syscall.ENETUNREACH),
		"dns":         fmt.Errorf("execute request: %w", &net.DNSError{Err: "no such host", Name: "openrouter.ai"}),
	} {
		if kind, _, words := ClassifyTurnError(err); kind != TurnErrorOffline || words != TurnOfflineWords {
			t.Errorf("%s: kind %q words %q, want offline", name, kind, words)
		}
	}
	if kind, _, _ := ClassifyTurnError(fmt.Errorf("read: %w", syscall.ECONNRESET)); kind == TurnErrorOffline {
		t.Error("a reset mid-answer was sorted offline")
	}
	if kind, _, _ := ClassifyTurnError(errors.New("something else")); kind != TurnErrorOther {
		t.Errorf("a plain error sorted %q", kind)
	}
}
