package syncwords

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strings"
	"syscall"

	"github.com/Agent-Field/codeaf/internal/clusterctl"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/relay"
	"github.com/Agent-Field/codeaf/internal/wireauth"
)

type explained struct {
	line  string
	cause error
}

func (e explained) Error() string { return e.line }
func (e explained) Unwrap() error { return e.cause }

// Explain gives every command the same diagnosis while retaining the typed cause.
func Explain(err error, address string) error {
	if err == nil || errors.Is(err, directory.ErrRevoked) || errors.Is(err, context.Canceled) {
		return err
	}
	var request *url.Error
	var network net.Error
	if address == "" && errors.As(err, &request) {
		if u, e := url.Parse(request.URL); e == nil {
			address = u.Scheme + "://" + u.Host
		}
	}
	if address == "" {
		address = HostedURL
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &network) && network.Timeout():
		return explained{"the sync service at " + address + " did not answer in time — try again", err}
	case IsStruggling(err):
		return explained{Struggling(address), err}
	case network != nil, errors.Is(err, syscall.ECONNREFUSED), errors.Is(err, syscall.ENETUNREACH), errors.Is(err, syscall.EHOSTUNREACH), errors.Is(err, relay.ErrUnreachable):
		return explained{Unreachable(address), err}
	case errors.Is(err, directory.ErrUnreachable):
		return explained{Struggling(address), err}
	}
	return err
}

// NetworkFailure is the boundary used by saving: these failures leave local
// transcripts intact, and a local seal is not evidence of network recovery.
func NetworkFailure(err error) bool {
	var upload interface{ SyncFailure() bool }
	if errors.As(err, &upload) && upload.SyncFailure() {
		return true
	}
	var local *net.OpError
	if errors.As(err, &local) && strings.HasPrefix(local.Net, "unix") {
		return false
	}
	var request *url.Error
	var operation *net.OpError
	var dns *net.DNSError
	return errors.As(err, &request) || errors.As(err, &operation) || errors.As(err, &dns) || errors.Is(err, directory.ErrUnreachable) || errors.Is(err, wireauth.ErrUnavailable) || errors.Is(err, clusterctl.ErrUnavailable) || errors.Is(err, relay.ErrUnreachable) || errors.Is(err, relay.ErrStruggling)
}

// UploadFailure marks a failure after local durability succeeded. It survives
// error wrapping and is also carried explicitly by remote saving replies.
func UploadFailure(err error) error {
	if err == nil {
		return nil
	}
	return uploadFailure{err}
}

type uploadFailure struct{ cause error }

func (e uploadFailure) Error() string     { return e.cause.Error() }
func (e uploadFailure) Unwrap() error     { return e.cause }
func (e uploadFailure) SyncFailure() bool { return true }

// UploadPending requires positive local durability, rather than a network-shaped error.
func UploadPending(err error) bool {
	if err == nil {
		return false
	}
	if upload, ok := err.(interface{ SyncFailure() bool }); ok {
		return upload.SyncFailure()
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		causes := joined.Unwrap()
		if len(causes) == 0 {
			return false
		}
		for _, cause := range causes {
			if !UploadPending(cause) {
				return false
			}
		}
		return true
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return UploadPending(wrapped.Unwrap())
	}
	return false
}

// IsStruggling identifies an answered service failure without changing the
// relay client's established error classes.
func IsStruggling(err error) bool {
	var response interface{ HTTPStatus() int }
	return errors.Is(err, wireauth.ErrUnavailable) || errors.Is(err, clusterctl.ErrUnavailable) || errors.Is(err, relay.ErrStruggling) || errors.As(err, &response) && response.HTTPStatus() >= 500
}
