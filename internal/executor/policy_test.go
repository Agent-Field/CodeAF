package executor

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestPolicyTable(t *testing.T) {
	cases := []struct {
		class  Class
		setup  bool
		denies bool
		effect SideEffect
	}{
		{Sandboxed, false, true, EffectLocal},
		{Sandboxed, true, false, EffectExternal},
		{HostBound, false, false, EffectExternal},
		{HostBound, true, false, EffectExternal},
		{FilesOnly, false, true, EffectLocal},
	}
	for _, c := range cases {
		p := PolicyFor(c.class, c.setup)
		if p.Denies() != c.denies || classify(p) != c.effect {
			t.Errorf("class %d setup %v: denies %v effect %s", c.class, c.setup, p.Denies(), classify(p))
		}
	}
}

func TestExecMarksSideEffectFromClass(t *testing.T) {
	cases := []struct {
		class Class
		setup bool
		want  SideEffect
	}{
		{Sandboxed, false, EffectLocal},
		{Sandboxed, true, EffectExternal},
		{HostBound, false, EffectExternal},
	}
	for _, c := range cases {
		req := sh("true")
		req.Setup = c.setup
		res, err := Local{Root: t.TempDir(), Class: c.class}.Exec(context.Background(), req, nil)
		if err != nil || res.SideEffect != c.want {
			t.Errorf("class %d setup %v: %s, %v; want %s", c.class, c.setup, res.SideEffect, err, c.want)
		}
	}
}

func TestClassOfWalksUpAndDefaultsToLegacy(t *testing.T) {
	root := t.TempDir()
	if got := ClassOf(root); got != HostBound {
		t.Fatalf("undeclared = %d, want host-bound", got)
	}
	Declare(root, Sandboxed)
	if got := ClassOf(root + "/sub/dir"); got != Sandboxed {
		t.Fatalf("declared subtree = %d, want sandboxed", got)
	}
	if c, ok := ParseClass("files-only"); !ok || c != FilesOnly {
		t.Fatalf("ParseClass = %d %v", c, ok)
	}
}

func TestRefusalIsReadable(t *testing.T) {
	err := refuseWithoutNamespaces(ExecRequest{Net: NetPolicy{}})
	if !errors.Is(err, ErrNoIsolation) || !strings.Contains(err.Error(), "user namespaces") {
		t.Fatalf("refusal = %v", err)
	}
	if refuseWithoutNamespaces(ExecRequest{Net: openNet}) != nil {
		t.Fatal("an open call must run degraded, not be refused")
	}
}

// A workspace no cell declared (CODEAF_CELLS off) runs exactly as before the
// policy existed: unjailed, network open.
func TestUndeclaredWorkspaceRunsAsBefore(t *testing.T) {
	res, err := In(t.TempDir()).Exec(context.Background(), sh("true"), nil)
	if err != nil || res.SideEffect != EffectExternal || res.JailDegraded {
		t.Fatalf("legacy call: %+v, %v", res, err)
	}
}
