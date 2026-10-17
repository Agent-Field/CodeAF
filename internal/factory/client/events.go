package client

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

// Subscription is one GET /v1/events stream. Changed receives a signal
// whenever the server says the floor changed. The signal carries no job:
// the desk re-reads. Close ends the stream and closes Changed.
type Subscription struct {
	changed chan struct{}
	cancel  context.CancelFunc
}

// Subscribe opens the optional event stream. The desk calls it when it
// wants Changed in place of its own tick, and leaves it uncalled when it
// does not. ctx ends the stream the same way Close does.
func (c *Client) Subscribe(ctx context.Context) *Subscription {
	ctx, cancel := context.WithCancel(ctx)
	s := &Subscription{changed: make(chan struct{}, 1), cancel: cancel}
	go c.follow(ctx, s.changed)
	return s
}

// Changed is the tick. One unread signal is enough: the desk re-reads the
// whole floor, so a second change while that read is pending adds nothing.
func (s *Subscription) Changed() <-chan struct{} {
	if s == nil {
		return nil
	}
	return s.changed
}

// Close ends the stream. A second call is the same as the first.
func (s *Subscription) Close() {
	if s != nil {
		s.cancel()
	}
}

func (c *Client) follow(ctx context.Context, changed chan struct{}) {
	defer close(changed)
	for ctx.Err() == nil {
		err := c.oneStream(ctx, changed)
		if ctx.Err() != nil || !retryable(err) {
			return
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (c *Client) oneStream(ctx context.Context, changed chan<- struct{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/v1/events", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")
	c.authorize(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return statusError{code: resp.StatusCode}
	}
	return readTicks(resp.Body, changed)
}

// readTicks signals on each changed event. A line that is only a comment
// is the stream staying alive, and it is not a change.
func readTicks(r io.Reader, changed chan<- struct{}) error {
	sc := bufio.NewScanner(r)
	name := ""
	saw := false
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if line == "" {
			if saw && (name == "" || name == "changed") {
				ping(changed)
			}
			name, saw = "", false
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "event":
			name = value
			saw = true
		case "data":
			saw = true
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return io.EOF
}

func ping(changed chan<- struct{}) {
	select {
	case changed <- struct{}{}:
	default:
	}
}

type statusError struct{ code int }

func (e statusError) Error() string { return http.StatusText(e.code) }

// retryable is a stream that dropped. A refusal (anything short of a server
// failure) is the factory's answer, and following it again would spin.
func retryable(err error) bool {
	if err == nil {
		return false
	}
	var status statusError
	if errors.As(err, &status) {
		return status.code >= 500
	}
	return true
}
