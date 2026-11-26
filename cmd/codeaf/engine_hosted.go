package main

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/enginehost"
	"github.com/Agent-Field/codeaf/internal/env"
	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/home"
)

// ── THE TWO KNOBS A HOSTED ENGINE IS STARTED WITH ──────────────────────────
//
// A codeaf engine that runs on a server for somebody who is not at that
// machine (the organization's hosted chats) is started by its supervisor with
// two environment variables and nothing else different from the engine a
// laptop runs. Both are read here, in one small file, so that "what changes
// when the supervisor says so" can be read in one place, and so that an engine
// started without them is, line for line, the engine this program has always
// been (contract C8).
//
// NEITHER IS A SETTING A PERSON CAN REACH. They are environment, set by the
// process that starts the engine under its own account; a chat's own files
// cannot write them, and the profile and the repository's config are not read
// for them.

// hostedFenceEnv turns the command fence on for every hello of this engine.
const hostedFenceEnv = "CODEAF_FENCE_REQUIRE"

// hostedConnectPortsEnv is the list of TCP ports this engine's commands may
// connect to (executor.ParseConnectPorts words: "443,80,22,9418,8080"). The
// supervisor writes it into every hosted engine's environment from the
// operator's settings; an engine started without it (an older supervisor, or a
// person's own machine) has no port limit, as it never had.
const hostedConnectPortsEnv = "CODEAF_FENCE_CONNECT_PORTS"

// hostedIdleEnv is the idle window of this engine's host, as a Go duration.
const hostedIdleEnv = "CODEAF_ENGINE_IDLE"

// hostedFenceRequired is whether this engine's commands must be confined.
// ONLY THE EXACT WORD 1 COUNTS: a supervisor that meant yes says 1, and a
// stray "0", "false" or an empty variable must not turn a requirement on for an
// engine that was never meant to carry one.
func hostedFenceRequired() bool {
	return strings.TrimSpace(env.Get(hostedFenceEnv)) == "1"
}

// hostedConnectPorts is the port list the supervisor gave this engine, and nil
// when it gave none. A list that cannot be read is an ERROR and never "no
// limit": an operator's typo must not turn the network fully open, so the
// engine's commands are refused with it instead.
func hostedConnectPorts() ([]int, error) {
	raw := strings.TrimSpace(env.Get(hostedConnectPortsEnv))
	if raw == "" {
		return nil, nil
	}
	ports, err := executor.ParseConnectPorts(raw)
	if err != nil {
		return nil, fmt.Errorf("%s is not a list of TCP ports (%v), so this chat's commands were not run", hostedConnectPortsEnv, err)
	}
	return ports, nil
}

// hostedEngineIdle is the idle window the supervisor asked this engine's host
// to keep, and false when it asked for none. A value that is not a duration,
// or is shorter than [enginehost.MinIdleOverride], is IGNORED and said once on
// stderr (which is the host's log): the host keeps its usual window, because a
// typo must not turn a half-hour grace into a host that retires every chat
// the moment a window closes.
func hostedEngineIdle() (time.Duration, bool) {
	raw := strings.TrimSpace(env.Get(hostedIdleEnv))
	if raw == "" {
		return 0, false
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d < enginehost.MinIdleOverride {
		fmt.Fprintf(os.Stderr, "codeaf engine: %s=%q is not a time of at least %s, so the usual idle time stays\n",
			hostedIdleEnv, raw, enginehost.MinIdleOverride)
		return 0, false
	}
	return d, true
}

// engineFencePlan decides how a hello's engine is fenced: whether it is
// fenced at all (a headless hello always is, as `--once` is; a hosted engine's
// every hello is) and whether that fence is a requirement. It is a pure
// function of the two facts so that "an interactive hello on a hosted engine is
// fenced, and on any other engine is not" is checked without booting one.
func engineFencePlan(headless, required bool) (fenced, require bool) {
	return headless || required, required
}

// hostedFence is the fence a hosted engine's commands keep to: the same fence
// [unattendedFence] draws for a run nobody is watching, made a requirement.
//
// THE PERSON'S OWN "turn the fence off" WORDS ARE IGNORED HERE. Everywhere else
// `jail.allow: off` is the owner of the machine choosing to run their own
// commands open, and the fence honours it. On a hosted engine the commands are
// somebody else's chat on the operator's machine, and the repository's config
// is a file the chat itself can write, so honouring it would let one command
// remove the fence from every command after it. The widening PATHS are still
// read: they only widen what the chat's own account can already reach.
func hostedFence(workspace, profileDir string) (*executor.Fence, error) {
	if strings.TrimSpace(workspace) == "" {
		return nil, fmt.Errorf("codeaf does not know which folder this run works in, so it cannot keep its commands inside it")
	}
	words, err := config.JailAllowAt(executor.RepositoryOf(workspace), profileDir)
	if err != nil {
		return nil, err
	}
	ports, err := hostedConnectPorts()
	if err != nil {
		return nil, err
	}
	person, _ := os.UserHomeDir()
	state := home.Join("jail")
	pruneFenceState(state, fenceStateMaxAge)
	fence, err := requiredFence(executor.FenceOptions{
		Root:             workspace,
		Home:             person,
		CodeafHome:       home.Dir(),
		OtherCodeafHomes: otherCodeafHomes(person),
		StateDir:         state,
		Allow:            executor.ParseFenceAllow(person, words...),
		Canvases:         []string{deskCanvasRoot(os.Getenv, person, runtime.GOOS)},
	})
	if err != nil {
		return nil, err
	}
	fence.ConnectPorts = ports
	return fence, nil
}

// requiredFence is [executor.NewFence] with the two things a hosted engine's
// fence must be: never turned off by its own options, and a requirement.
func requiredFence(opts executor.FenceOptions) (*executor.Fence, error) {
	opts.Allow.Off = false
	fence, err := executor.NewFence(opts)
	if err != nil {
		return nil, err
	}
	fence.Require = true
	return fence, nil
}
