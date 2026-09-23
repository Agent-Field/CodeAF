// Package fake is the test double every package's tests install in place of
// the network.
package fake

import (
	"errors"
	"sync"

	"bloop/shop/transport"
)

// Server answers a scripted status and body for every URL, failing the first
// FailFirst requests with a network error.
type Server struct {
	mu        sync.Mutex
	Status    int
	Body      string
	FailFirst int
	Calls     int
	LastURL   string
}

// Install puts the server in front of transport.Do for the life of the test
// and answers a function that takes it away again.
func (s *Server) Install() func() {
	old := transport.Do
	transport.Do = func(url string) (int, string, error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.Calls++
		s.LastURL = url
		if s.Calls <= s.FailFirst {
			return 0, "", errors.New("fake: connection reset")
		}
		return s.Status, s.Body, nil
	}
	return func() { transport.Do = old }
}
