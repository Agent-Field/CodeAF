//go:build linux

package executor

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

// listener returns a local TCP address a jailed child can only reach if it
// shares this network namespace.
func listener(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("no loopback: %v", err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	return l.Addr().String()
}

// TestClassGovernsReach dials a listener outside the child's namespace under
// each class through the real jail: only a sandboxed call without setup is cut off.
func TestClassGovernsReach(t *testing.T) {
	if !Probe().UserNS {
		t.Skip("user namespaces unavailable")
	}
	addr := listener(t)
	dialAddr := ExecRequest{Argv: []string{"bash", "-c", fmt.Sprintf("exec 3<>/dev/tcp/%s", strings.Replace(addr, ":", "/", 1))}, Timeout: 10 * time.Second}
	cases := []struct {
		name    string
		class   Class
		setup   bool
		reaches bool
		effect  SideEffect
	}{
		{"sandboxed denies", Sandboxed, false, false, EffectLocal},
		{"sandboxed setup", Sandboxed, true, true, EffectExternal},
		{"host-bound open", HostBound, false, true, EffectExternal},
	}
	for _, c := range cases {
		req := dialAddr
		req.Setup = c.setup
		res, err := Local{Root: t.TempDir(), Class: c.class, Jail: DefaultJail()}.Exec(context.Background(), req, nil)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if (res.Exit == 0) != c.reaches || res.SideEffect != c.effect {
			t.Errorf("%s: exit %d effect %s stderr %q", c.name, res.Exit, res.SideEffect, res.Stderr)
		}
	}
}
