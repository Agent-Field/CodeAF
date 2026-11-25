package sessionhost

import (
	"bytes"
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// THE VALIDATION CONTRACT (the operator is told when the network lets a chat reach a cloud
// metadata address, because the port fence cannot stop it):
//
//  1. A machine that accepts a connection on a metadata address is named in one warning line.
//  2. A machine that answers on none of them, or only slowly, gets no warning and no delay.
//  3. The check only connects and closes: it never reads or writes.
func TestTheOperatorIsToldWhichMetadataAddressesAnswer(t *testing.T) {
	var (
		mu     sync.Mutex
		asked  []string
		closed atomic.Int32
	)
	answering := map[string]bool{"169.254.169.254:80": true, "[fd00:ec2::254]:80": true}
	dial := func(_ context.Context, network, address string) (net.Conn, error) {
		mu.Lock()
		asked = append(asked, network+" "+address)
		mu.Unlock()
		if !answering[address] {
			return nil, errors.New("no route")
		}
		client, server := net.Pipe()
		t.Cleanup(func() { _ = server.Close() })
		return &touchedConn{Conn: client, t: t, closed: &closed}, nil
	}
	var out bytes.Buffer
	warnIfMetadataReachable(t.Context(), &out, dial)
	line := out.String()
	if !strings.Contains(line, "169.254.169.254:80") || !strings.Contains(line, "[fd00:ec2::254]:80") || strings.Contains(line, "169.254.170.2") {
		t.Fatalf("the warning names the wrong addresses: %q", line)
	}
	if strings.Count(line, "\n") != 1 || !strings.Contains(line, "Block 169.254.0.0/16") {
		t.Fatalf("the warning is not one line that says what to do: %q", line)
	}
	if len(asked) != len(metadataAddresses) {
		t.Fatalf("asked %v", asked)
	}
	if closed.Load() != 2 {
		t.Fatalf("closed %d of the 2 connections that were made", closed.Load())
	}
}

// touchedConn fails the test on any read or write: the check only connects and closes.
type touchedConn struct {
	net.Conn
	t      *testing.T
	closed *atomic.Int32
}

func (c *touchedConn) Read([]byte) (int, error) {
	c.t.Error("the check read from the address")
	return 0, net.ErrClosed
}
func (c *touchedConn) Write([]byte) (int, error) {
	c.t.Error("the check wrote to the address")
	return 0, net.ErrClosed
}
func (c *touchedConn) Close() error { c.closed.Add(1); return c.Conn.Close() }

func TestNoWarningWhenNothingAnswersOrTheNetworkDropsThePacket(t *testing.T) {
	refused := func(context.Context, string, string) (net.Conn, error) { return nil, errors.New("refused") }
	dropped := func(ctx context.Context, _, _ string) (net.Conn, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	for name, dial := range map[string]dialer{"refused": refused, "dropped": dropped} {
		var out bytes.Buffer
		start := time.Now()
		warnIfMetadataReachable(t.Context(), &out, dial)
		if out.Len() != 0 {
			t.Errorf("%s: a warning for an address that did not answer: %q", name, out.String())
		}
		if took := time.Since(start); took > 5*time.Second {
			t.Errorf("%s: the check waited %v", name, took)
		}
	}
}
