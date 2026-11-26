package directroad

import (
	"context"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/livedoor"
	"github.com/pion/stun/v4"
	"github.com/pion/transport/v5/vnet"
)

// fakeSTUNServer listens on a vnet Net at :3478, counts incoming STUN Binding
// requests, and replies with the sender's address in XOR-MAPPED-ADDRESS.
type fakeSTUNServer struct {
	conn     net.PacketConn
	requests atomic.Int64
	done     chan struct{}
}

func startFakeSTUNServer(t *testing.T, nw *vnet.Net) *fakeSTUNServer {
	t.Helper()
	conn, err := nw.ListenPacket("udp4", testSTUNIP+":3478")
	if err != nil {
		t.Fatalf("stun ListenPacket: %v", err)
	}
	srv := &fakeSTUNServer{
		conn: conn,
		done: make(chan struct{}),
	}
	go srv.serve()
	t.Cleanup(func() {
		_ = conn.Close()
		<-srv.done
	})
	return srv
}

func (s *fakeSTUNServer) serve() {
	defer close(s.done)
	buf := make([]byte, 1500)
	for {
		n, addr, err := s.conn.ReadFrom(buf)
		if err != nil {
			return
		}
		var req stun.Message
		if err := stun.Decode(buf[:n], &req); err != nil {
			continue
		}
		if req.Type != stun.BindingRequest {
			continue
		}
		s.requests.Add(1)

		udpAddr, ok := addr.(*net.UDPAddr)
		if !ok {
			continue
		}
		resp, err := stun.Build(&req, stun.BindingSuccess, &stun.XORMappedAddress{
			IP:   udpAddr.IP,
			Port: udpAddr.Port,
		})
		if err != nil {
			continue
		}
		_, _ = s.conn.WriteTo(resp.Raw, addr)
	}
}

// Contract V36: when Config.STUN is empty, zero STUN packets are sent across a
// full connect; when Config.STUN is configured on the same rig, the fake STUN
// server receives Binding requests.
//
// The "road is relay" half of V36 lives one layer up: a relay-only computer
// never constructs this package's objects (internal/syncsetup pins that).
func TestNoSTUNRequestIsMadeWhenTheRoadIsRelayOrStunIsOff(t *testing.T) {
	// Part A: with Config.STUN empty, a full connect and byte transfer sends
	// zero packets to the STUN server address.
	t.Run("stun_empty_sends_zero_stun_packets", func(t *testing.T) {
		rig := newVNetRig(t)
		stunSrv := startFakeSTUNServer(t, rig.stunNet)

		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
		defer cancel()

		host := New(rig.hostConfig())
		defer host.Close()

		hostStreamCh := make(chan io.ReadWriteCloser, 1)
		acceptDone := make(chan struct{})
		defer close(acceptDone)

		host.SetAcceptor(ctx, func(_ context.Context, stream io.ReadWriteCloser) {
			hostStreamCh <- stream
			<-acceptDone
		})

		bridge := newSignalBridge(ctx, host, livedoor.Peer{Device: "device-no-stun"})
		viewerStream, err := Offer(ctx, rig.viewerConfig(), bridge.viewerSignaller(), bridge.deliver())
		if err != nil {
			t.Fatalf("Offer with STUN off failed: %v", err)
		}
		defer func() { _ = viewerStream.Close() }()

		var hostStream io.ReadWriteCloser
		select {
		case hostStream = <-hostStreamCh:
		case <-ctx.Done():
			t.Fatal("timed out waiting for host stream")
		}

		if _, err := viewerStream.Write([]byte("ping")); err != nil {
			t.Fatalf("viewer Write: %v", err)
		}
		var echo [4]byte
		if _, err := io.ReadFull(hostStream, echo[:]); err != nil {
			t.Fatalf("host ReadFull: %v", err)
		}

		if got := stunSrv.requests.Load(); got != 0 {
			t.Fatalf("fake STUN server received %d Binding requests with STUN empty, want 0", got)
		}
		if got := rig.chunksTo(testSTUNIP); got != 0 {
			t.Fatalf("vnet router routed %d chunks to STUN IP %s with STUN empty, want 0", got, testSTUNIP)
		}
	})

	// Part B: with a STUN URL configured on the same rig, the gatherer DOES
	// send Binding requests to the fake STUN server (while ignoring any non-stun
	// turn: entry), proving the rig detects STUN traffic.
	t.Run("stun_configured_reaches_fake_server", func(t *testing.T) {
		rig := newVNetRig(t)
		stunSrv := startFakeSTUNServer(t, rig.stunNet)

		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
		defer cancel()

		stunURL := "stun:" + testSTUNIP + ":3478"
		vCfg := rig.viewerConfig()
		vCfg.STUN = []string{stunURL, "turn:" + testSTUNIP + ":3478"}
		hCfg := rig.hostConfig()
		hCfg.STUN = []string{stunURL}

		host := New(hCfg)
		defer host.Close()

		hostStreamCh := make(chan io.ReadWriteCloser, 1)
		acceptDone := make(chan struct{})
		defer close(acceptDone)

		host.SetAcceptor(ctx, func(_ context.Context, stream io.ReadWriteCloser) {
			hostStreamCh <- stream
			<-acceptDone
		})

		bridge := newSignalBridge(ctx, host, livedoor.Peer{Device: "device-with-stun"})
		viewerStream, err := Offer(ctx, vCfg, bridge.viewerSignaller(), bridge.deliver())
		if err != nil {
			t.Fatalf("Offer with STUN configured failed: %v", err)
		}
		defer func() { _ = viewerStream.Close() }()

		select {
		case <-hostStreamCh:
		case <-ctx.Done():
			t.Fatal("timed out waiting for host stream with STUN configured")
		}

		eventually(t, 2*time.Second, "fake STUN server to receive Binding request", func() bool {
			return stunSrv.requests.Load() > 0 && rig.chunksTo(testSTUNIP) > 0
		})
	})
}
