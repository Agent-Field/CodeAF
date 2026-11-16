package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// markedCode is a code as the server makes one for lb's flow.
func markedCode(lb *passkeyLoopback, fill byte) string {
	c := make([]byte, 32)
	for i := range 16 {
		c[i] = fill
	}
	mac := hmac.New(sha256.New, lb.key)
	mac.Write(c[:16])
	copy(c[16:], mac.Sum(nil))
	return base64.RawURLEncoding.EncodeToString(c)
}

// N3: the loopback takes only its own flow's code, at its exact path and
// Host. Anything else is answered and forgotten, rate-limited, and never
// closes it; its own code is taken once.
func TestThePasskeyLoopbackTakesOnlyItsOwnFlowsCode(t *testing.T) {
	lb, err := openPasskeyLoopback()
	if err != nil {
		t.Fatal(err)
	}
	defer lb.close()
	base := fmt.Sprintf("http://127.0.0.1:%d/", lb.port())
	get := func(u, host string) int {
		t.Helper()
		req, _ := http.NewRequest("GET", u, nil)
		if host != "" {
			req.Host = host
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return -1
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	mine := markedCode(lb, 'a')
	foreign := base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("f", 32)))
	for name, try := range map[string][2]string{
		"a wrong path":             {base + "guess?code=" + mine, ""},
		"a malformed code":         {base + lb.path + "?code=short", ""},
		"another flow's code":      {base + lb.path + "?code=" + foreign, ""},
		"another Host (rebinding)": {base + lb.path + "?code=" + mine, "evil.test"},
	} {
		if got := get(try[0], try[1]); got != http.StatusNotFound {
			t.Fatalf("%s = %d", name, got)
		}
	}
	if lb.received() != "" {
		t.Fatal("a refused request left a code")
	}
	// A burst of foreign requests is held to the allowance, and the
	// listener is still there afterwards.
	limited := false
	for range passkeyLoopbackRefusals + 5 {
		if get(base+lb.path+"?code="+foreign, "") == http.StatusTooManyRequests {
			limited = true
		}
	}
	if !limited {
		t.Fatal("a burst of foreign requests was never limited")
	}
	for i := 0; ; i++ {
		got := get(base+lb.path+"?code="+mine, "")
		if got == http.StatusOK {
			break
		}
		if got != http.StatusTooManyRequests || i > 50 {
			t.Fatalf("its own code after the burst = %d", got)
		}
		lb.mu.Lock()
		lb.window = lb.window.Add(-2e9)
		lb.mu.Unlock()
	}
	if lb.received() != mine {
		t.Fatalf("received = %q", lb.received())
	}
	if got := get(base+lb.path+"?code="+markedCode(lb, 'b'), ""); got == http.StatusOK {
		t.Fatal("a second code was accepted")
	}
	if lb.received() != mine {
		t.Fatal("a second code replaced the first")
	}
	var none *passkeyLoopback
	none.close()
	if none.received() != "" {
		t.Fatal("a nil loopback has a code")
	}
}
