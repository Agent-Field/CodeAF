// Package legacy is the old fetch helper.
//
// Deprecated: use package client instead. This package is to be deleted once
// nothing imports it.
package legacy

import (
	"fmt"

	"bloop/shop/transport"
)

// Fetch asks for url once, and then up to retries more times while it keeps
// failing. A 404 answers an empty body and no error.
func Fetch(url string, retries int) (string, error) {
	var lastErr error
	for attempt := 0; attempt <= retries; attempt++ {
		code, body, err := transport.Do(url)
		switch {
		case err != nil:
			lastErr = err
		case code == 200:
			return body, nil
		case code == 404:
			return "", nil
		default:
			lastErr = fmt.Errorf("legacy: status %d", code)
		}
	}
	return "", lastErr
}
