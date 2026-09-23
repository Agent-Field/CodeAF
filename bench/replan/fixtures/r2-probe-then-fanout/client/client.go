// Package client is the supported way to fetch anything in this module.
package client

import (
	"errors"
	"fmt"

	"bloop/shop/transport"
)

// ErrNotFound is what Get answers for a 404.
var ErrNotFound = errors.New("client: not found")

// Options tunes one Get.
type Options struct {
	// Attempts is how many times Get asks in total before it gives up. Zero
	// and one both mean a single attempt.
	Attempts int
}

// Response is what came back.
type Response struct {
	Status int
	Body   string
}

// Get asks for url, trying up to opt.Attempts times while it keeps failing.
// A 404 answers ErrNotFound at once, without trying again.
func Get(url string, opt Options) (Response, error) {
	attempts := opt.Attempts
	if attempts < 1 {
		attempts = 1
	}
	var lastErr error
	for i := 0; i < attempts; i++ {
		code, body, err := transport.Do(url)
		switch {
		case err != nil:
			lastErr = err
		case code == 200:
			return Response{Status: code, Body: body}, nil
		case code == 404:
			return Response{Status: code}, ErrNotFound
		default:
			lastErr = fmt.Errorf("client: status %d", code)
		}
	}
	return Response{}, lastErr
}
