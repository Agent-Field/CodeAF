package pair

import (
	"errors"
	"fmt"
	"net/url"
)

// ServiceTimeout names a service that did not answer without diagnosing the network.
// The cause remains available to callers that distinguish cancellation from failure.
func ServiceTimeout(cause error, address string) error {
	var request *url.Error
	if errors.As(cause, &request) {
		if parsed, err := url.Parse(request.URL); err == nil && parsed.Host != "" {
			address = parsed.Scheme + "://" + parsed.Host
		}
	}
	return serviceTimeout{cause: cause, address: address}
}

type serviceTimeout struct {
	cause   error
	address string
}

func (e serviceTimeout) Error() string {
	return fmt.Sprintf("the sync service at %s did not answer in time — try again", e.address)
}

func (e serviceTimeout) Unwrap() error { return e.cause }
