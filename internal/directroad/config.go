// Package directroad opens a peer-to-peer WebRTC data channel between two of a
// person's own computers and hands back an ordinary io.ReadWriteCloser byte
// stream.
//
// The road contract (D20) is fixed so any endpoint, including a phone, can
// interoperate without sharing this package:
//   - The viewer is always the SDP offerer and the host is always the answerer.
//   - Exactly one m=application data channel is used, pre-negotiated at id 0
//     with label "codeaf-live", ordered and reliable (MaxRetransmits and
//     MaxPacketLifeTime are left unset).
//   - Signalling frames (offer, answer, trickled ICE candidates, end, refuse)
//     travel as opaque livedoor.Signal JSON payloads up to livedoor.SignalMax
//     bytes over an already-authenticated relay tunnel owned by the caller.
//   - Once the data channel opens, the first byte on the stream is the intent
//     byte 'D', followed by a Noise_IK handshake and 2-byte-length-prefixed
//     records; NOTHING IN THIS PACKAGE KNOWS ABOUT NOISE OR THE INTENT BYTE,
//     which run in the caller above the returned io.ReadWriteCloser.
//   - DATA CHANNEL MESSAGES ARE NOT RECORD-ALIGNED, so a reader must treat the
//     channel as a continuous byte stream; senders cut writes into messages of
//     at most 16 KiB and receivers accept messages up to 64 KiB.
//   - ICE servers are STUN only; TURN is never configured or dialled.
package directroad

import (
	"io"
	"strings"
	"time"

	"github.com/pion/ice/v4"
	"github.com/pion/logging"
	"github.com/pion/transport/v5"
	"github.com/pion/webrtc/v4"
)

const (
	// dataChannelLabel is the single pre-negotiated WebRTC data channel label
	// shared by the viewer offerer and the host answerer.
	dataChannelLabel = "codeaf-live"

	// dataChannelID is the pre-negotiated SCTP stream identifier so neither side
	// waits on an in-band DCEP open handshake.
	dataChannelID uint16 = 0

	// Production defaults for ICETimers when a caller leaves a field unset.
	defaultICEDisconnected  = 3 * time.Second
	defaultICEFailed        = 5 * time.Second
	defaultICEKeepalive     = 1 * time.Second
	defaultICEGatherTimeout = 3 * time.Second
)

// droppedInterfacePrefixes lists container and virtual-machine bridge prefixes
// whose addresses are unreachable from another computer and would only add
// dead candidate pairs to ICE checks.
var droppedInterfacePrefixes = []string{
	"docker",
	"veth",
	"br-",
	"virbr",
	"vmnet",
	"vboxnet",
	"cni",
	"flannel",
	"cali",
	"kube",
}

// ICETimers are the ICE agent's liveness bounds. A zero field means the
// production default.
type ICETimers struct {
	Disconnected  time.Duration // default 3 s: no inbound for this long -> state Disconnected
	Failed        time.Duration // default 5 s more: Disconnected this long -> state Failed
	Keepalive     time.Duration // default 1 s: pion's consent-freshness STUN binding interval
	GatherTimeout time.Duration // default 3 s: STUN gathering gives up after this
}

// withDefaults fills any non-positive timer with its production default so
// callers and tests can override individual timers without zeroing the rest.
func (t ICETimers) withDefaults() ICETimers {
	if t.Disconnected <= 0 {
		t.Disconnected = defaultICEDisconnected
	}
	if t.Failed <= 0 {
		t.Failed = defaultICEFailed
	}
	if t.Keepalive <= 0 {
		t.Keepalive = defaultICEKeepalive
	}
	if t.GatherTimeout <= 0 {
		t.GatherTimeout = defaultICEGatherTimeout
	}
	return t
}

// Config controls candidate gathering, virtual networking, and liveness timers
// for both the viewer Offer call and the host answerer.
type Config struct {
	STUN            []string      // "stun:host:port" URLs, tried in parallel; empty = host candidates only, and NO STUN packet is ever sent
	Loopback        bool          // tests only: also offer loopback candidates
	Net             transport.Net // tests only: pion vnet; nil = the real network (do not call SetNet)
	ICE             ICETimers
	InterfaceFilter func(name string) bool // nil = the built-in filter below
}

// defaultInterfaceFilter returns the built-in interface filter. It excludes
// local container and VM bridges that another machine can never reach and
// drops the kernel loopback interface unless loopback candidates were
// requested for a local test.
func defaultInterfaceFilter(includeLoopback bool) func(string) bool {
	return func(name string) bool {
		if !includeLoopback && (name == "lo" || name == "lo0" || name == "lo0String") {
			return false
		}
		for _, prefix := range droppedInterfacePrefixes {
			if strings.HasPrefix(name, prefix) {
				return false
			}
		}
		return true
	}
}

// silentLoggerFactory returns a logger factory that discards every line. PION
// MUST NEVER WRITE TO STDOUT OR STDERR because codeaf owns a terminal surface
// and any background log line would tear through the active screen.
func silentLoggerFactory() logging.LoggerFactory {
	return &logging.DefaultLoggerFactory{
		Writer:          io.Discard,
		DefaultLogLevel: logging.LogLevelDisabled,
	}
}

// sanitizeSTUNURLs keeps only plain stun: URLs so a turn: or turns: server can
// never reach pion even if misconfigured by a caller.
func sanitizeSTUNURLs(urls []string) []string {
	var clean []string
	for _, raw := range urls {
		u := strings.TrimSpace(raw)
		if strings.HasPrefix(strings.ToLower(u), "stun:") {
			clean = append(clean, u)
		}
	}
	return clean
}

// newPeerConnection builds a pion PeerConnection and the single pre-negotiated
// data channel with identical settings on both the viewer and the host.
func newPeerConnection(cfg Config) (*webrtc.PeerConnection, *webrtc.DataChannel, error) {
	var se webrtc.SettingEngine

	// PION MUST NEVER WRITE TO STDOUT OR STDERR because the program runs a
	// terminal UI and any warning line would draw over it.
	se.LoggerFactory = silentLoggerFactory()

	// Detach the data channel so OnOpen yields an io.ReadWriteCloser rather than
	// starting pion's internal callback read loop.
	se.DetachDataChannels()

	// EnableDataChannelBlockWrite forwards sctp.WithBlockWrite(true) to the SCTP
	// association when DetachDataChannels is enabled, so Stream.Write blocks
	// once a message is in pendingQueue until popPendingDataChunksToSend moves
	// it into the CWND/RWND-bounded inflightQueue instead of buffering without
	// bound against a slow reader.
	se.EnableDataChannelBlockWrite(true)

	timers := cfg.ICE.withDefaults()
	se.SetICETimeouts(timers.Disconnected, timers.Failed, timers.Keepalive)
	se.SetSTUNGatherTimeout(timers.GatherTimeout)

	// mDNS candidates are disabled because both peers are the person's own
	// computers and real addresses are the point of the direct road.
	se.SetICEMulticastDNSMode(ice.MulticastDNSModeDisabled)

	// Direct paths travel over UDP4 and UDP6 only; ICE-TCP is never used.
	se.SetNetworkTypes([]webrtc.NetworkType{
		webrtc.NetworkTypeUDP4,
		webrtc.NetworkTypeUDP6,
	})

	se.SetIncludeLoopbackCandidate(cfg.Loopback)

	filter := cfg.InterfaceFilter
	if filter == nil {
		filter = defaultInterfaceFilter(cfg.Loopback)
	}
	se.SetInterfaceFilter(filter)

	if cfg.Net != nil {
		se.SetNet(cfg.Net)
	}

	// Certificate fingerprint verification stays at its default (enabled);
	// DisableCertificateFingerprintVerification is NEVER called.
	var rtcCfg webrtc.Configuration
	if stunURLs := sanitizeSTUNURLs(cfg.STUN); len(stunURLs) > 0 {
		rtcCfg.ICEServers = []webrtc.ICEServer{
			{URLs: stunURLs},
		}
	}

	api := webrtc.NewAPI(webrtc.WithSettingEngine(se))
	pc, err := api.NewPeerConnection(rtcCfg)
	if err != nil {
		return nil, nil, err
	}

	negotiated := true
	id := dataChannelID
	dc, err := pc.CreateDataChannel(dataChannelLabel, &webrtc.DataChannelInit{
		Negotiated: &negotiated,
		ID:         &id,
	})
	if err != nil {
		_ = pc.Close()
		return nil, nil, err
	}

	return pc, dc, nil
}
