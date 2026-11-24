package main

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/executor"
)

// The contract, in the words of the operator who starts hosted chats:
//
//	C4  an engine started with the requirement fences every conversation it
//	    opens, the one a person is typing into as much as a scripted one, and
//	    that fence cannot be switched off by anything the chat itself can write.
//	C7  an engine started with an idle time honours a real duration and ignores
//	    anything that is not one.
//	C8  an engine started without the variables is the engine this program has
//	    always been: a typing person gets no fence, a scripted run gets the
//	    usual one.

func TestC4_HostedEnvTurnsOnTheFenceForInteractiveHello(t *testing.T) {
	for value, want := range map[string]bool{
		"1": true, " 1 ": true,
		"": false, "0": false, "false": false, "true": false, "yes": false, "11": false,
	} {
		t.Setenv(hostedFenceEnv, value)
		if got := hostedFenceRequired(); got != want {
			t.Errorf("%s=%q: required = %v, want %v", hostedFenceEnv, value, got, want)
		}
	}

	t.Setenv(hostedFenceEnv, "1")
	fenced, required := engineFencePlan(false, hostedFenceRequired())
	if !fenced || !required {
		t.Fatalf("an interactive hello on a hosted engine: fenced %v, required %v; want both", fenced, required)
	}
	fenced, required = engineFencePlan(true, hostedFenceRequired())
	if !fenced || !required {
		t.Fatalf("a headless hello on a hosted engine: fenced %v, required %v; want both", fenced, required)
	}
}

func TestC8_EngineFenceUnchangedByDefault(t *testing.T) {
	t.Setenv(hostedFenceEnv, "")
	if hostedFenceRequired() {
		t.Fatal("the requirement is on with the variable empty")
	}
	if fenced, required := engineFencePlan(false, hostedFenceRequired()); fenced || required {
		t.Fatalf("an interactive hello got fenced (%v, required %v) on an engine nobody asked to fence", fenced, required)
	}
	if fenced, required := engineFencePlan(true, hostedFenceRequired()); !fenced || required {
		t.Fatalf("a headless hello is fenced %v, required %v; it must be fenced as --once is, and not required", fenced, required)
	}
}

func TestC4_HostedFenceCannotBeTurnedOffByItsOwnWords(t *testing.T) {
	root := t.TempDir()
	off := executor.ParseFenceAllow("", "off")
	if !off.Off {
		t.Fatal("the fixture's off word was not read as off")
	}
	opts := executor.FenceOptions{Root: root, StateDir: t.TempDir(), Allow: off}

	// Everywhere else, the person's own words may remove the fence.
	if plain, err := executor.NewFence(opts); err != nil || plain != nil {
		t.Fatalf("the ordinary fence with off words: %v, %v; want none", plain, err)
	}
	fence, err := requiredFence(opts)
	if err != nil {
		t.Fatal(err)
	}
	if fence == nil || !fence.Require {
		t.Fatalf("a hosted engine's fence was removed or lost its requirement by the chat's own off words: %+v", fence)
	}
}

func TestC7_HostedEngineIdleReadsOnlyAValidDuration(t *testing.T) {
	for value, want := range map[string]time.Duration{
		"8s": 8 * time.Second, "5s": 5 * time.Second, "90m": 90 * time.Minute, " 30s ": 30 * time.Second,
	} {
		t.Setenv(hostedIdleEnv, value)
		if got, ok := hostedEngineIdle(); !ok || got != want {
			t.Errorf("%s=%q: got %v, %v; want %v, true", hostedIdleEnv, value, got, ok, want)
		}
	}
	for _, value := range []string{"", "4s", "0", "-10s", "banana", "10", "1ms"} {
		t.Setenv(hostedIdleEnv, value)
		if got, ok := hostedEngineIdle(); ok {
			t.Errorf("%s=%q was applied as %v; it is empty, not a duration, or below the floor", hostedIdleEnv, value, got)
		}
	}
}

// P1-0c: the port list the supervisor writes into a hosted engine's
// environment reaches the fence, and one that cannot be read refuses the
// engine's commands instead of opening the network.
func TestP1_0c_HostedEngineReadsItsPortList(t *testing.T) {
	t.Setenv(hostedConnectPortsEnv, "")
	if ports, err := hostedConnectPorts(); ports != nil || err != nil {
		t.Fatalf("an engine with no list: %v, %v; it must have no limit, as it never had", ports, err)
	}
	t.Setenv(hostedConnectPortsEnv, " 443,80, 8080 ")
	ports, err := hostedConnectPorts()
	if err != nil || len(ports) != 3 || ports[0] != 80 || ports[1] != 443 || ports[2] != 8080 {
		t.Fatalf("a list: %v, %v", ports, err)
	}
	for _, bad := range []string{"https", "443,0", "70000"} {
		t.Setenv(hostedConnectPortsEnv, bad)
		if ports, err := hostedConnectPorts(); err == nil || ports != nil {
			t.Errorf("%q was read as %v; it must refuse, never mean no limit", bad, ports)
		}
	}
}
