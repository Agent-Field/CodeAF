package lane

import (
	"math"
	"testing"
	"time"
)

// These tests are derived from the validation contract for slow-local-model
// patience and not from the implementation: a classification is a fact about
// which machines are the person's own, and a multiplier is a fact about how
// long a bound is held, and each is stated here in those terms.

// usePatienceFor publishes one `response.patience` and restores `auto` after.
func usePatienceFor(t *testing.T, factor float64) {
	t.Helper()
	UseResponsePatience(factor)
	t.Cleanup(func() { UseResponsePatience(0) })
}

// CONTRACT: which endpoints are local is decided from the address text alone.
func TestWhichEndpointsAreTheMachineOfTheirOwn(t *testing.T) {
	cases := []struct {
		name   string
		base   string
		source string
		want   bool
	}{
		// Loopback, every spelling people write.
		{"localhost with port", "http://localhost:11434/v1", "", true},
		{"localhost with no scheme", "localhost:11434/v1", "", true},
		{"localhost upper case", "http://LocalHost:8080/v1", "", true},
		{"a name under localhost", "http://api.localhost:8080/v1", "", true},
		{"127.0.0.1", "http://127.0.0.1:1234/v1", "", true},
		{"anywhere in 127/8", "http://127.9.8.7/v1", "", true},
		{"ipv6 loopback", "http://[::1]:11434/v1", "", true},
		{"ipv4-mapped loopback", "http://[::ffff:127.0.0.1]:11434/v1", "", true},
		{"unspecified", "http://0.0.0.0:8080/v1", "", true},

		// Private IPv4, at the edges of each block.
		{"10/8 low", "http://10.0.0.1:8000/v1", "", true},
		{"10/8 high", "http://10.255.255.254/v1", "", true},
		{"172.16 low edge", "http://172.16.0.1/v1", "", true},
		{"172.31 high edge", "http://172.31.255.254/v1", "", true},
		{"172.15 is public", "http://172.15.0.1/v1", "", false},
		{"172.32 is public", "http://172.32.0.1/v1", "", false},
		{"192.168", "http://192.168.1.50:11434/v1", "", true},
		{"192.169 is public", "http://192.169.1.50/v1", "", false},

		// Carrier-grade (Tailscale) 100.64.0.0/10.
		{"cgnat low", "http://100.64.0.1:11434/v1", "", true},
		{"cgnat high", "http://100.127.255.254/v1", "", true},
		{"100.63 is public", "http://100.63.0.1/v1", "", false},
		{"100.128 is public", "http://100.128.0.1/v1", "", false},

		// IPv6 unique-local and link-local.
		{"ula fd", "http://[fd12:3456:789a::1]:8080/v1", "", true},
		{"ula fc", "http://[fc00::1]/v1", "", true},
		{"link-local v6", "http://[fe80::1]/v1", "", true},
		{"link-local v6 with zone", "http://[fe80::1%25eth0]/v1", "", true},
		{"link-local v4", "http://169.254.10.10/v1", "", true},
		{"public v6", "http://[2606:4700:4700::1111]/v1", "", false},

		// Names that only resolve on a person's own network.
		{"dot local", "http://studio.local:1234/v1", "", true},
		{"dot lan", "http://gpu.lan/v1", "", true},
		{"dot internal", "http://llm.internal/v1", "", true},
		{"docker host", "http://host.docker.internal:11434/v1", "", true},
		{"single label host", "http://gpu-box:8000/v1", "", true},
		{"trailing dot is the same name", "http://studio.local./v1", "", true},
		{"notlocal is not .local", "http://notlocal.example.com/v1", "", false},
		{"locally is not .local", "http://example.locally/v1", "", false},

		// Hosted and public.
		{"openrouter", "https://openrouter.ai/api/v1", "", false},
		{"openai", "https://api.openai.com/v1", "", false},
		{"public ip", "http://8.8.8.8/v1", "", false},
		{"a public name that mentions localhost", "https://localhost.example.com/v1", "", false},
		{"a public name under .internal.example", "https://llm.internal.example.com/v1", "", false},
		{"empty", "", "", false},
		{"unparseable", "http://[::1", "", false},

		// The Ollama row is local wherever it runs, and only that row.
		{"ollama row at its own address", "http://localhost:11434/v1", "ollama", true},
		{"ollama row behind a public name", "https://ollama.example.com/v1", "ollama", true},
		{"ollama row upper case", "https://ollama.example.com/v1", "Ollama", true},
		{"another row at a public name", "https://ollama.example.com/v1", "openai", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsLocalEndpoint(tc.base, tc.source); got != tc.want {
				t.Errorf("IsLocalEndpoint(%q, %q) = %v, want %v", tc.base, tc.source, got, tc.want)
			}
		})
	}
}

// CONTRACT: under `auto` a hosted endpoint is held to the bound it always had,
// and a local one to the named local multiple.
func TestAutoIsOneForHostedAndTheLocalDefaultForLocal(t *testing.T) {
	usePatienceFor(t, 0)
	if got := PatienceFor("https://openrouter.ai/api/v1", ""); got != 1 {
		t.Errorf("hosted under auto = %v, want exactly 1", got)
	}
	if got := PatienceFor("http://localhost:11434/v1", ""); got != LocalPatience {
		t.Errorf("local under auto = %v, want %v", got, LocalPatience)
	}
	if LocalPatience != 10 {
		t.Errorf("LocalPatience = %v: the manual, `help env` and the settings hint all interpolate it, but the change entry promises ten", LocalPatience)
	}
}

// CONTRACT: a number the person wrote applies to every endpoint and is never
// multiplied again, so `3` is three everywhere and not thirty on a local box.
func TestAWrittenNumberAppliesToEveryEndpointUnmultiplied(t *testing.T) {
	usePatienceFor(t, 3)
	for _, base := range []string{"https://openrouter.ai/api/v1", "http://localhost:11434/v1", "http://192.168.1.5:8000/v1"} {
		if got := PatienceFor(base, ""); got != 3 {
			t.Errorf("PatienceFor(%q) under 3 = %v, want 3", base, got)
		}
	}
	usePatienceFor(t, 1)
	if got := PatienceFor("http://localhost:11434/v1", ""); got != 1 {
		t.Errorf("a person who wrote 1 gets %v on a local endpoint, want exactly 1 (today's behaviour)", got)
	}
	usePatienceFor(t, 2.5)
	if got := PatienceFor("https://api.openai.com/v1", ""); got != 2.5 {
		t.Errorf("a fractional 2.5 = %v", got)
	}
}

// CONTRACT: a figure outside the range, or not a number, can never shrink or
// explode a bound; it is `auto`.
func TestAFigureOutsideTheRangeIsAuto(t *testing.T) {
	for _, bad := range []float64{0, 0.5, 0.999, -3, 100.001, 101, 1e9, math.NaN(), math.Inf(1), math.Inf(-1)} {
		UseResponsePatience(bad)
		if got := PatienceFor("https://api.openai.com/v1", ""); got != 1 {
			t.Errorf("published %v: hosted = %v, want 1", bad, got)
		}
		if got := PatienceFor("http://localhost:11434/v1", ""); got != LocalPatience {
			t.Errorf("published %v: local = %v, want %v", bad, got, LocalPatience)
		}
	}
	UseResponsePatience(0)
	for _, edge := range []float64{MinResponsePatience, MaxResponsePatience} {
		UseResponsePatience(edge)
		if got := PatienceFor("https://api.openai.com/v1", ""); got != edge {
			t.Errorf("edge %v published as %v", edge, got)
		}
	}
	UseResponsePatience(0)
}

// CONTRACT: hosted behaviour is bit for bit unchanged — a factor of one or less
// returns the very same bound.
func TestStretchIsTheIdentityAtOneAndBelow(t *testing.T) {
	bounds := []time.Duration{0, 1, 10 * time.Second, 2 * time.Minute, 150 * time.Second, 20 * time.Minute, 7*time.Hour + 3}
	for _, bound := range bounds {
		for _, factor := range []float64{1, 0.5, 0, -2, math.NaN()} {
			if got := Stretch(bound, factor); got != bound {
				t.Errorf("Stretch(%v, %v) = %v, want the bound itself", bound, factor, got)
			}
		}
	}
	if got := Stretch(2*time.Minute, 10); got != 20*time.Minute {
		t.Errorf("ten times two minutes = %v, want 20m", got)
	}
	if got := Stretch(90*time.Second, 10); got != 15*time.Minute {
		t.Errorf("ten times ninety seconds = %v, want 15m", got)
	}
	if got := Stretch(10*time.Second, 2.5); got != 25*time.Second {
		t.Errorf("2.5 times ten seconds = %v, want 25s", got)
	}
}
