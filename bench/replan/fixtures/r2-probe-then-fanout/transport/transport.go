// Package transport is the one place a request leaves this module. Tests
// replace Do with a function of their own.
package transport

import "errors"

// Do performs one request and answers the status code and the body.
var Do = func(url string) (int, string, error) {
	return 0, "", errors.New("transport: this module has no network")
}
