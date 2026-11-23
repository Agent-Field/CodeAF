package syncwords

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"syscall"
	"testing"

	"github.com/Agent-Field/codeaf/internal/directory"
)

func TestIdleRelease_R8_OnlyDialFailureProvesRequestNotSent(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"refused", &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}, true},
		{"unreachable route", &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ENETUNREACH}, true},
		{"dns", &net.OpError{Op: "dial", Net: "tcp", Err: &net.DNSError{Err: "no such host"}}, true},
		{"dial deadline", &net.OpError{Op: "dial", Net: "tcp", Err: context.DeadlineExceeded}, true},
		{"response deadline", context.DeadlineExceeded, false},
		{"read reset", &net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET}, false},
		{"reset", syscall.ECONNRESET, false},
		{"unknown unreachable", directory.ErrUnreachable, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := UploadFailure(fmt.Errorf("%w: %w", directory.ErrUnreachable, &url.Error{Op: "Post", URL: "http://fixture.invalid/release", Err: tc.err}))
			if got := RequestNotSent(err); got != tc.want {
				t.Fatalf("RequestNotSent(%v)=%v, want %v", err, got, tc.want)
			}
		})
	}
}
