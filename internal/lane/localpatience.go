package lane

import (
	"math"
	"net/netip"
	"net/url"
	"strings"
	"sync/atomic"
)

// ── HOW LONG A SLOW MACHINE OF YOUR OWN MAY TAKE ────────────────────────────
//
// EVERY WAIT BOUND ON THE MODEL-CALL PATH WAS MEASURED AGAINST HOSTED
// ENDPOINTS, and for a hosted endpoint they are right: a router that has said
// nothing for ten seconds, or a provider that has not sent a header in two
// minutes, is a connection that is not coming back, and the pool behind it has
// another machine to ask. A machine of your own is the opposite case on every
// count. It is ONE machine, so there is nowhere else to ask; it may be a laptop
// reading a fifteen-thousand-token prompt on a CPU, which takes minutes before
// the first token and is working the whole time; and a second request to it does
// not rescue anything, it queues behind the first or competes with it for the
// same GPU. Measured on 2026-11-19 against a real Ollama, a prompt of about
// twelve thousand tokens on four CPU cores was cut at the two-minute header
// bound, asked again, cut again, and the turn was lost to a model that was
// never stuck.
//
// So there is ONE multiplier, [PatienceFor], and every silence, stall and
// give-up bound on the call path is multiplied by it: hosted endpoints by one,
// which is exactly what they have always had, and a local endpoint by
// [LocalPatience]. A person who wants something else writes `response.patience`
// (or CODEAF_RESPONSE_PATIENCE), and that number then applies to every endpoint.

// LocalPatience is how many times the hosted bound a LOCAL endpoint is given
// when the person has not said otherwise. It is the one source of this figure:
// the transport, the stall watch, the plan's ceiling and give-up, the settings
// row's hint, `codeaf help env` and the manual all read it from here.
//
// TEN, because the bounds were argued against endpoints that answer in seconds
// and the machine this exists for answers in minutes: ten times the two-minute
// header bound is twenty minutes, which covers a prompt of a hundred thousand
// tokens prefilling at a hundred tokens a second, and ten times the ninety-second
// turn give-up is fifteen minutes before codeaf moves a conversation to another
// model. It is a default and a row, not a law: a machine slower than that writes
// a bigger number, and one faster writes `1` and has today's behaviour exactly.
const LocalPatience = 10.0

const (
	// MinResponsePatience and MaxResponsePatience are the range a person may
	// write. Below one is asking codeaf to give up sooner than the measurement
	// says any call takes, which is refused rather than honoured (the same
	// reading [UsePatience] has of `response.attempts`), and above a hundred is a
	// bound no person is waiting through.
	MinResponsePatience = 1.0
	MaxResponsePatience = 100.0
)

// responsePatience is the person's own `response.patience`, in thousandths so
// that an atomic integer can hold a fraction, and ZERO FOR `auto`. It is
// process-wide for the reason [UsePatience]'s figure is: the places that build a
// wait bound (the transport, the stall watch, the dispatcher's plan) cannot read
// a profile, and a setting that reached some of them and not the others would be
// a person told two different things about the same wait. internal/config
// publishes it at the one door every launch passes through.
var responsePatience atomic.Int64

// UseResponsePatience publishes the person's `response.patience`. Zero, a figure
// outside the range, or NaN is `auto`: [PatienceFor] then answers one for a
// hosted endpoint and [LocalPatience] for a local one. The range check is here as
// well as in the setting's parser because this is the door a test or a bench
// reaches without the parser, and a stored 0.2 must never shrink a bound.
func UseResponsePatience(factor float64) {
	if math.IsNaN(factor) || factor < MinResponsePatience || factor > MaxResponsePatience {
		responsePatience.Store(0)
		return
	}
	responsePatience.Store(int64(math.Round(factor * patienceScale)))
}

// PatienceIsAuto reports whether endpoint type determines the wait and rescue policy.
func PatienceIsAuto() bool {
	return responsePatience.Load() == 0
}

// PatienceFor is the multiplier on every wait bound for one request, decided by
// where the request is going and what the person wrote.
//
// A NUMBER THE PERSON WROTE APPLIES TO EVERY ENDPOINT, local or hosted, and is
// not multiplied by anything: `3` is three times everywhere, never thirty on a
// local machine. `auto` is one for a hosted endpoint — and one means the figures
// this build shipped with, bit for bit, which is the whole of why nothing about
// a hosted call moves — and [LocalPatience] for a local one.
//
// IT IS PURE AND CHEAP. It reads one atomic and parses one URL, with no network
// and no lookup, so the transport may ask it on every request.
func PatienceFor(baseURL, sourceID string) float64 {
	if stored := responsePatience.Load(); stored >= patienceScale {
		return float64(stored) / patienceScale
	}
	if IsLocalEndpoint(baseURL, sourceID) {
		return LocalPatience
	}
	return 1
}

// IsLocalEndpoint says whether a request is going to a machine of the person's
// own: the one decision this feature turns on, and it is made HERE AND NOWHERE
// ELSE from the address text alone, with no DNS lookup — a lookup is a network
// call on the path of every request and answers a different question (where the
// name points today) from the one that matters (whose machine it is).
//
// An address is local when its host is:
//   - `localhost`, or any name under it;
//   - an IP in a loopback range (127.0.0.0/8, ::1), a private IPv4 range (10/8,
//     172.16/12, 192.168/16), the carrier-grade range 100.64.0.0/10 that
//     Tailscale and its kind hand out, an IPv6 unique-local address (fc00::/7),
//     or a link-local address of either family;
//   - a name ending `.local`, `.lan` or `.internal` (which includes
//     `host.docker.internal`), the names a home network, mDNS and a container
//     runtime give a neighbour; or
//   - a single word with no dot, such as `gpu-box`, which can only be resolved
//     by the local network's own search path.
//
// AND A SOURCE THAT IS THE OLLAMA ROW IS LOCAL WHEREVER IT RUNS. The row is the
// person saying "this is a model server I run", and a remote Ollama behind a
// public name is still one machine whose prefill is the thing being waited for.
//
// A public address, a public name and an empty one are not local: anything this
// cannot positively place on the person's own network is held to the hosted
// bounds it always had.
func IsLocalEndpoint(baseURL, sourceID string) bool {
	if strings.EqualFold(strings.TrimSpace(sourceID), "ollama") {
		return true
	}
	host := hostOf(baseURL)
	if host == "" {
		return false
	}
	if address, err := netip.ParseAddr(host); err == nil {
		return localAddress(address.Unmap())
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	for _, suffix := range localSuffixes {
		if strings.HasSuffix(host, suffix) {
			return true
		}
	}
	return !strings.Contains(host, ".")
}

// localSuffixes are the name endings that only ever resolve on a person's own
// network, spelled with the dot so `notlocal` is not one of them.
var localSuffixes = []string{".local", ".lan", ".internal"}

// cgnat is the shared-address range (RFC 6598) Tailscale and carrier-grade NAT
// use. It is not "private" to the standard library, and it is where a person's
// own GPU box on a tailnet lives.
var cgnat = netip.MustParsePrefix("100.64.0.0/10")

// localAddress is the address half of [IsLocalEndpoint].
func localAddress(address netip.Addr) bool {
	return address.IsLoopback() || address.IsPrivate() || address.IsLinkLocalUnicast() ||
		address.IsUnspecified() || cgnat.Contains(address)
}

// hostOf is the lower-cased host of a base URL, without its port, its brackets
// or a trailing dot, and empty when there is none. A base written without a
// scheme (`localhost:11434/v1`) is read as `http`, because a scheme-less string
// would otherwise parse with `localhost` as the SCHEME and no host at all.
func hostOf(baseURL string) string {
	raw := strings.TrimSpace(baseURL)
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	// An IPv6 zone (`fe80::1%eth0`) is part of the host text and not of the
	// address, and netip refuses a bare zone it cannot place.
	if cut := strings.IndexByte(host, '%'); cut >= 0 {
		host = host[:cut]
	}
	return host
}

// Stretch multiplies one bound by a patience factor, and it is the one place
// that arithmetic is written. A factor of one returns the bound itself — the
// SAME duration, not the product of a float round trip — because the contract
// of this feature is that a hosted call under `auto` moves by not one
// nanosecond.
func Stretch[D ~int64](bound D, factor float64) D {
	// The comparison also catches NaN, which cannot define a wait.
	if !(factor > 1) || math.IsInf(factor, 0) {
		return bound
	}
	scaled := float64(bound) * factor
	// MaxInt64 rounds up as a float, so equality is already outside the duration range.
	if scaled >= float64(math.MaxInt64) {
		return D(math.MaxInt64)
	}
	if scaled <= float64(math.MinInt64) {
		return D(math.MinInt64)
	}
	return D(scaled)
}
