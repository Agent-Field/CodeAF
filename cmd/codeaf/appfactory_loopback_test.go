package main

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// The loopback keeps only a well-formed code at its exact path, keeps the
// first one, and stops listening after it.
func TestThePasskeyLoopbackTakesOneCodeAtItsOwnPath(t *testing.T) {
	lb, err := openPasskeyLoopback()
	if err != nil {
		t.Fatal(err)
	}
	defer lb.close()
	base := fmt.Sprintf("http://127.0.0.1:%d/", lb.port())
	code := base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("c", 32)))
	get := func(u string) int {
		t.Helper()
		resp, err := http.Get(u)
		if err != nil {
			return -1
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if got := get(base + "guess?code=" + code); got != http.StatusNotFound {
		t.Fatalf("a wrong path = %d", got)
	}
	if got := get(base + lb.path + "?code=short"); got != http.StatusBadRequest {
		t.Fatalf("a malformed code = %d", got)
	}
	if lb.received() != "" {
		t.Fatal("a refused request left a code")
	}
	if got := get(base + lb.path + "?code=" + code); got != http.StatusOK {
		t.Fatalf("the code = %d", got)
	}
	if lb.received() != code {
		t.Fatalf("received = %q", lb.received())
	}
	other := base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("d", 32)))
	if got := get(base + lb.path + "?code=" + other); got == http.StatusOK {
		t.Fatal("a second code was accepted")
	}
	if lb.received() != code {
		t.Fatal("a second code replaced the first")
	}
	var none *passkeyLoopback
	none.close()
	if none.received() != "" {
		t.Fatal("a nil loopback has a code")
	}
}
