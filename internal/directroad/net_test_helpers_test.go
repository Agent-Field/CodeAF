package directroad

import (
	"context"
	"net"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/livedoor"
	"github.com/pion/transport/v5/vnet"
)

const (
	testViewerIP = "10.0.0.1"
	testHostIP   = "10.0.0.2"
	testSTUNIP   = "10.0.0.3"
)

// testICETimers returns fast ICE liveness timers suitable for deterministic
// unit tests over loopback and vnet.
func testICETimers() ICETimers {
	return ICETimers{
		Disconnected:  300 * time.Millisecond,
		Failed:        600 * time.Millisecond,
		Keepalive:     100 * time.Millisecond,
		GatherTimeout: 300 * time.Millisecond,
	}
}

// loopbackConfig returns a Config that gathers real loopback candidates with
// no STUN servers.
func loopbackConfig() Config {
	return Config{
		Loopback: true,
		ICE:      testICETimers(),
	}
}

// eventually polls cond without sleeping until it returns true or timeout
// expires.
func eventually(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %v waiting for %s", timeout, what)
		}
		runtime.Gosched()
	}
}

// fnSignaller adapts a function into a livedoor.Signaller.
type fnSignaller func(payload []byte) error

func (f fnSignaller) Send(payload []byte) error {
	return f(payload)
}

// signalBridge wires a viewer Offer call to a Host in memory, recording every
// signalling payload in both directions and allowing tests to inspect, modify,
// or drop frames via optional hooks.
type signalBridge struct {
	ctx           context.Context
	host          *Host
	peer          livedoor.Peer
	viewerDeliver chan []byte

	mu             sync.Mutex
	session        livedoor.DirectSession
	viewerPayloads [][]byte
	hostPayloads   [][]byte
	viewerHook     func(payload []byte) ([]byte, bool)
	hostHook       func(payload []byte) ([]byte, bool)
	refuseOnNil    string
}

// newSignalBridge constructs a bridge between one viewer attempt and host.
func newSignalBridge(ctx context.Context, host *Host, peer livedoor.Peer) *signalBridge {
	if peer.Device == "" {
		peer.Device = "viewer-device-1"
	}
	return &signalBridge{
		ctx:           ctx,
		host:          host,
		peer:          peer,
		viewerDeliver: make(chan []byte, 256),
	}
}

// viewerSignaller returns the Signaller passed to Offer.
func (b *signalBridge) viewerSignaller() livedoor.Signaller {
	return fnSignaller(func(payload []byte) error {
		cp := append([]byte(nil), payload...)

		b.mu.Lock()
		b.viewerPayloads = append(b.viewerPayloads, cp)
		hook := b.viewerHook
		b.mu.Unlock()

		if hook != nil {
			var keep bool
			cp, keep = hook(cp)
			if !keep {
				return nil
			}
		}

		sig, err := livedoor.DecodeSignal(cp)
		if err == nil && sig.T == livedoor.SignalOffer {
			var sess livedoor.DirectSession
			if b.host != nil {
				sess = b.host.Offered(b.ctx, b.peer, cp, b.hostSignaller())
			}
			b.mu.Lock()
			b.session = sess
			reason := b.refuseOnNil
			b.mu.Unlock()

			if sess == nil && reason != "" {
				_ = sendSignal(b.hostSignaller(), livedoor.Signal{
					T:      livedoor.SignalRefuse,
					ID:     sig.ID,
					Reason: reason,
				})
			}
			return nil
		}

		b.mu.Lock()
		sess := b.session
		b.mu.Unlock()
		if sess != nil {
			sess.Deliver(cp)
		}
		return nil
	})
}

// hostSignaller returns the Signaller passed to Host.Offered.
func (b *signalBridge) hostSignaller() livedoor.Signaller {
	return fnSignaller(func(payload []byte) error {
		cp := append([]byte(nil), payload...)

		b.mu.Lock()
		b.hostPayloads = append(b.hostPayloads, cp)
		hook := b.hostHook
		b.mu.Unlock()

		if hook != nil {
			var keep bool
			cp, keep = hook(cp)
			if !keep {
				return nil
			}
		}

		select {
		case b.viewerDeliver <- cp:
		default:
		}
		return nil
	})
}

// deliver returns the viewer's inbound signalling channel.
func (b *signalBridge) deliver() <-chan []byte {
	return b.viewerDeliver
}

// vnetRig hosts an in-memory pion vnet router with separate virtual NICs for
// the viewer, the host, and an optional fake STUN server.
type vnetRig struct {
	router    *vnet.Router
	viewerNet *vnet.Net
	hostNet   *vnet.Net
	stunNet   *vnet.Net

	dropPeers atomic.Bool

	mu            sync.Mutex
	chunksByDstIP map[string]int64
}

// newVNetRig builds and starts a virtual network router on 10.0.0.0/24 and
// registers router shutdown with t.Cleanup.
func newVNetRig(t *testing.T) *vnetRig {
	t.Helper()

	router, err := vnet.NewRouter(&vnet.RouterConfig{
		CIDR:          "10.0.0.0/24",
		LoggerFactory: silentLoggerFactory(),
	})
	if err != nil {
		t.Fatalf("vnet.NewRouter: %v", err)
	}

	viewerNet, err := vnet.NewNet(&vnet.NetConfig{
		StaticIPs: []string{testViewerIP},
	})
	if err != nil {
		t.Fatalf("vnet.NewNet viewer: %v", err)
	}
	hostNet, err := vnet.NewNet(&vnet.NetConfig{
		StaticIPs: []string{testHostIP},
	})
	if err != nil {
		t.Fatalf("vnet.NewNet host: %v", err)
	}
	stunNet, err := vnet.NewNet(&vnet.NetConfig{
		StaticIPs: []string{testSTUNIP},
	})
	if err != nil {
		t.Fatalf("vnet.NewNet stun: %v", err)
	}

	if err := router.AddNet(viewerNet); err != nil {
		t.Fatalf("router.AddNet viewer: %v", err)
	}
	if err := router.AddNet(hostNet); err != nil {
		t.Fatalf("router.AddNet host: %v", err)
	}
	if err := router.AddNet(stunNet); err != nil {
		t.Fatalf("router.AddNet stun: %v", err)
	}

	rig := &vnetRig{
		router:        router,
		viewerNet:     viewerNet,
		hostNet:       hostNet,
		stunNet:       stunNet,
		chunksByDstIP: make(map[string]int64),
	}

	router.AddChunkFilter(func(c vnet.Chunk) bool {
		if dst, ok := c.DestinationAddr().(*net.UDPAddr); ok && dst.IP != nil {
			ipStr := dst.IP.String()
			rig.mu.Lock()
			rig.chunksByDstIP[ipStr]++
			rig.mu.Unlock()
		}
		if rig.dropPeers.Load() {
			return false
		}
		return true
	})

	if err := router.Start(); err != nil {
		t.Fatalf("router.Start: %v", err)
	}
	t.Cleanup(func() {
		_ = router.Stop()
	})

	return rig
}

// viewerConfig returns a directroad Config bound to the viewer's vnet NIC.
func (r *vnetRig) viewerConfig() Config {
	return Config{
		Net: r.viewerNet,
		ICE: testICETimers(),
	}
}

// hostConfig returns a directroad Config bound to the host's vnet NIC.
func (r *vnetRig) hostConfig() Config {
	return Config{
		Net: r.hostNet,
		ICE: testICETimers(),
	}
}

// chunksTo returns the number of routed UDP chunks addressed to ip.
func (r *vnetRig) chunksTo(ip string) int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.chunksByDstIP[ip]
}

// pendingCount reports how many attempts are currently pending on h.
func (h *Host) pendingCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.pending)
}

// streamCount reports how many open streams are currently tracked on h.
func (h *Host) streamCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.streams)
}
