package relayserve_test

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/pairbox"
	"github.com/Agent-Field/codeaf/internal/pairbox/pairboxtest"
	"github.com/Agent-Field/codeaf/internal/relayserve"
)

// pairRig is an in-process relay on the fake clock, and a second one capped at
// two mailboxes for RelayFull. Both trust the proxy header, as a relay under
// conformance must, so every case is a network of its own.
func pairRig(t *testing.T, store string, logf func(string, ...any)) pairboxtest.Rig {
	clock := pairboxtest.NewFakeClock()
	serve := func(limits pairbox.Limits) (string, pairbox.Box) {
		svc := relayserve.New(relayserve.Config{Store: store, Now: clock.Now, Logf: logf, TrustProxy: true, Pair: limits})
		srv := httptest.NewServer(svc.Handler)
		t.Cleanup(func() { srv.Close(); svc.Close() })
		return srv.URL, pairbox.NewHTTP(srv.URL, srv.Client())
	}
	small := pairbox.DefaultLimits
	small.MaxBoxes = 2
	url, box := serve(pairbox.DefaultLimits)
	_, tiny := serve(small)
	return pairboxtest.Rig{Box: box, Clock: clock, URL: url, Small: tiny}
}

func TestPairingWithoutAStore(t *testing.T) {
	pairboxtest.Run(t, func(t *testing.T) pairboxtest.Rig { return pairRig(t, "", nil) })
}

func TestPairingBesideAStore(t *testing.T) {
	pairboxtest.Run(t, func(t *testing.T) pairboxtest.Rig { return pairRig(t, t.TempDir(), nil) })
}

// The pairing routes log a verb, a route pattern and a status, and never the
// key that owns a side, the nameplate, or a message.
func TestPairingLogsNoSecrets(t *testing.T) {
	var mu sync.Mutex
	var lines []string
	logf := func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		lines = append(lines, strings.TrimSpace(fmt.Sprintf(format, args...)))
	}
	rig := pairRig(t, "", logf)
	key, err := pairbox.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	made, err := rig.Box.Create(bg, key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rig.Box.Post(bg, made.Nameplate, pairbox.SideA, key, []byte("sealed-message")); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	all := strings.Join(lines, "\n")
	for _, secret := range []string{key.String(), "sealed-message", "/" + made.Nameplate + "/"} {
		if strings.Contains(all, secret) {
			t.Errorf("the log carries %q:\n%s", secret, all)
		}
	}
	if !strings.Contains(all, "POST /v1/pair/{plate}/{side} 201") {
		t.Errorf("the log lacks the verb and status of a post:\n%s", all)
	}
}
