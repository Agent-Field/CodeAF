package space

import (
	"bufio"
	"errors"
	"net"
	"net/http"
	"sync"
)

// sockSet closes the sockets a removed member still has open. The gateway
// owns the websocket after Hijack, so the only handle left is the net.Conn.
type sockSet struct {
	mu    sync.Mutex
	conns map[string][]net.Conn
}

func newSockSet() *sockSet { return &sockSet{conns: map[string][]net.Conn{}} }

func (s *sockSet) wrap(w http.ResponseWriter, member string) http.ResponseWriter {
	return &captureWriter{ResponseWriter: w, member: member, set: s}
}

func (s *sockSet) add(member string, conn net.Conn) net.Conn {
	tracked := &trackedConn{Conn: conn}
	tracked.drop = func() { s.drop(member, tracked) }
	s.mu.Lock()
	s.conns[member] = append(s.conns[member], tracked)
	s.mu.Unlock()
	return tracked
}

func (s *sockSet) drop(member string, conn net.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.conns[member]
	for i, item := range list {
		if item == conn {
			s.conns[member] = append(list[:i], list[i+1:]...)
			break
		}
	}
	if len(s.conns[member]) == 0 {
		delete(s.conns, member)
	}
}

func (s *sockSet) close(member string) {
	s.mu.Lock()
	list := s.conns[member]
	delete(s.conns, member)
	s.mu.Unlock()
	for _, conn := range list {
		_ = conn.Close()
	}
}

type trackedConn struct {
	net.Conn
	once sync.Once
	drop func()
}

func (t *trackedConn) Close() error {
	if t.drop != nil {
		t.once.Do(t.drop)
	}
	return t.Conn.Close()
}

type captureWriter struct {
	http.ResponseWriter
	member string
	set    *sockSet
}

func (c *captureWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hj, ok := c.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("websocket hijack is not available")
	}
	conn, rw, err := hj.Hijack()
	if err != nil {
		return nil, nil, err
	}
	return c.set.add(c.member, conn), rw, nil
}
