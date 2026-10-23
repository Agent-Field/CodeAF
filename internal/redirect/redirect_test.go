package redirect

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

func requestFor(t *testing.T, url string) *http.Request {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	return req
}

func TestPolicyFollowsOnlySameOrigin(t *testing.T) {
	tests := []struct {
		name   string
		target string
		host   string
		refuse bool
	}{
		{name: "same-origin path change", target: "https://api.example.com/v2/stream", host: "api.example.com", refuse: false},
		{name: "default port spelled out", target: "https://api.example.com:443/v1/chat", host: "api.example.com", refuse: false},
		{name: "another port", target: "https://api.example.com:8443/v1/chat", host: "api.example.com", refuse: true},
		{name: "subdomain", target: "https://evil.api.example.com/v1/chat", host: "evil.api.example.com", refuse: true},
		{name: "unrelated domain", target: "https://other-vendor.test/v1/chat", host: "other-vendor.test", refuse: true},
		{name: "scheme change", target: "http://api.example.com/v1/chat", host: "api.example.com", refuse: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			original := requestFor(t, "https://api.example.com/v1/chat")
			target := requestFor(t, test.target)
			via := []*http.Request{original}
			err := Policy(target, via)
			if test.refuse {
				if err == nil {
					t.Fatalf("Policy followed %s; want a refusal", test.target)
				}
				var refused *RefusedError
				if !errors.As(err, &refused) {
					t.Fatalf("Policy refusal %v is not a *RefusedError, so the provider dispatcher cannot stop at it", err)
				}
				if !strings.Contains(err.Error(), test.host) {
					t.Fatalf("Policy error %v names no target host %q", err, test.host)
				}
			} else if err != nil {
				t.Fatalf("Policy refused %s; want it followed: %v", test.target, err)
			}
		})
	}
}

func TestPolicyStopsAfterTenRequests(t *testing.T) {
	original := requestFor(t, "https://api.example.com/v1/chat")
	via := []*http.Request{original, original, original, original, original, original, original, original, original, original}
	err := Policy(original, via)
	if err == nil || !strings.Contains(err.Error(), "10 redirects") {
		t.Fatalf("Policy cap = %v, want stopped after 10 redirects", err)
	}
}

// redirectTransport is a RoundTripper that must never be asked; it exists so a
// Guard test can assert the caller's Transport rides through the copy.
type redirectTransport struct{}

func (redirectTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return nil, errors.New("unused redirect test transport")
}

func TestGuardComposesExistingCheckRedirectAndKeepsClient(t *testing.T) {
	sentinel := errors.New("sentinel")
	base := &http.Client{
		Timeout:       5 * time.Second,
		Transport:     redirectTransport{},
		CheckRedirect: func(*http.Request, []*http.Request) error { return sentinel },
	}
	guarded := Guard(base)
	check := guarded.CheckRedirect
	if check == nil {
		t.Fatal("Guard dropped the redirect policy")
	}
	emptyVia := []*http.Request{}
	err := check(requestFor(t, "https://api.example.com/v1/chat"), emptyVia)
	if err == nil || err.Error() != "sentinel" {
		t.Fatalf("composed CheckRedirect = %v, want the caller's sentinel", err)
	}
	if guarded.Timeout != base.Timeout {
		t.Fatalf("Guard changed Timeout: %v vs %v", guarded.Timeout, base.Timeout)
	}
	if guarded.Transport != base.Transport {
		t.Fatal("Guard changed Transport")
	}
}

func TestGuardAppliesPolicyToLenientCallerClient(t *testing.T) {
	base := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return nil }}
	guarded := Guard(base)
	check := guarded.CheckRedirect
	original := requestFor(t, "https://api.example.com/v1/chat")
	via := []*http.Request{original}
	err := check(requestFor(t, "https://evil.example.com/x"), via)
	if err == nil || !strings.Contains(err.Error(), "evil.example.com") {
		t.Fatalf("Guarded lenient client followed a cross-origin redirect: %v", err)
	}
	if err := check(requestFor(t, "https://api.example.com/other"), via); err != nil {
		t.Fatalf("Guarded client refused a same-origin redirect: %v", err)
	}
}

func TestGuardNilYieldsPolicyCarryingClient(t *testing.T) {
	guarded := Guard(nil)
	if guarded == nil || guarded.CheckRedirect == nil {
		t.Fatal("Guard(nil) must yield a client carrying the policy")
	}
}
