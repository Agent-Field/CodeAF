package main

// ── `aforge serve`, `aforge chat --at otter-lamp-42`, `aforge devices` ───────
//
// The three doors of reaching a machine WITHOUT ssh. One machine holds an
// outbound connection open and shows a pairing code; a device pairs with that
// code once and afterwards just opens; and the machine that owns the work lists
// and stops the devices it has let in.
//
//	big-machine$ aforge serve
//	  this machine is reachable as  otter-lamp-42
//	  pair a new device with code   715 302   (valid 10 minutes)
//
//	laptop$ aforge chat --at otter-lamp-42
//	  pairing with otter-lamp-42 — enter the code shown there: ______
//	  paired. this device is now a key to otter-lamp-42.   [chat opens]
//
// THIS IS A SECOND DOOR AND NEVER A REPLACEMENT ONE. `--host` over ssh is the
// floor and stays the floor: zero infrastructure, nothing of ours to trust, and
// a debugging story that cannot be beaten. `--at` is for the machine ssh cannot
// reach — behind a home router, on a work network, on a phone — and everything
// it needs beyond the two commands is a relay service that this build does not
// assume exists. When there is none, `--at` says so in one sentence and names
// the road that does work.
//
// THE PROMPT LAW, INHERITED FROM chatv3_host.go AND FOR THE SAME REASON:
// EVERYTHING THAT MIGHT ASK THE PERSON A QUESTION HAPPENS BEFORE THE SURFACE
// TAKES THE SCREEN. The pairing code is typed on a plain terminal, at a plain
// prompt, while stderr is still the terminal's. A TUI that came up first would
// draw a frame over the top of the one question this door has to ask.
//
// AND THE ENGINE IS THE MACHINE'S OWN `aforge engine`, started as a child, one
// per connection — the same shape ssh gives it, so the far half of this door is
// code that has been running in production since the ssh door shipped. What is
// new here is the pipe, and only the pipe.

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/pair"
	"github.com/Agent-Field/aforge-v2/internal/remote"
)

// ── the surface: aforge chat --at <name> ────────────────────────────────────

// atLaunch is one `--at` launch as the flag parser saw it. It is deliberately
// the same shape as [hostLaunch], because it is the same launch down a
// different pipe.
type atLaunch struct {
	// target is the flag as typed: a machine name, or name:path.
	target string
	// session is --session, passed through as the engine's own --session.
	session string
	// model and level are --model and --reasoning, carried in the hello.
	model string
	level string
	// once is --once: one message, printed, no terminal ownership.
	once string
	// pick is `aforge resume`: the same surface, opened on the session picker.
	pick bool
	// noCompact and yolo are refused rather than ignored, exactly as they are
	// over --host: they are properties of a session that is built over there.
	noCompact bool
	yolo      bool
	// budget is --max-hours / --max-cost, and it is refused for yolo's reason
	// and one more, exactly as it is over --host: what it bounds is a goal
	// owner that lives in the session (internal/session's principal.go), and
	// the session is on the far machine. A ceiling accepted here would bound
	// nothing at all.
	budget bool
}

// check refuses the flags this door cannot honour, in the same words the ssh
// door uses, because it is the same fact: the session is built on the other
// machine and a flag typed here has nowhere to land.
func (l atLaunch) check() error {
	var named []string
	if l.noCompact {
		named = append(named, "--no-compact")
	}
	if l.yolo {
		named = append(named, "--yolo")
	}
	if l.budget {
		named = append(named, "--max-hours/--max-cost")
	}
	if len(named) == 0 {
		return nil
	}
	name, _, _ := parseAtTarget(l.target)
	return fmt.Errorf("%s cannot travel over --at: the session is built on %s, so set it there — open the settings panel on that machine, or run `aforge chat %s` on it",
		strings.Join(named, " and "), name, strings.Join(named, " "))
}

// parseAtTarget splits `name` or `name:path` the way the ssh door splits its
// own target: everything before the FIRST colon is the machine.
//
// THE PATH IS NOT RESOLVED HERE. It is the other machine's path, resolved over
// there, exactly as it is over ssh — making it absolute here would resolve it
// against this machine's working directory and send somebody else's filesystem.
func parseAtTarget(raw string) (name, workspace string, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", errors.New("--at needs a machine name: --at otter-lamp-42, or --at otter-lamp-42:code/app — `aforge serve` prints the name of a machine")
	}
	name = raw
	if at := strings.Index(raw, ":"); at >= 0 {
		name = strings.TrimSpace(raw[:at])
		workspace = strings.TrimSpace(raw[at+1:])
	}
	if name == "" {
		return "", "", fmt.Errorf("--at %q has no machine in front of the colon", raw)
	}
	return name, workspace, nil
}

// openChatV3At is the launch.
func openChatV3At(launch atLaunch) error {
	if err := launch.check(); err != nil {
		return err
	}
	name, workspace, err := parseAtTarget(launch.target)
	if err != nil {
		return err
	}
	if launch.pick && launch.once != "" {
		return fmt.Errorf(`aforge resume opens the session picker; for one headless message use: aforge chat --at %s --once "text"`, name)
	}

	device, err := pair.ThisDevice(pair.OpenKeeper())
	if err != nil {
		return err
	}
	reach := pair.Reach{
		Name:     name,
		Device:   device,
		Machines: pair.MachineBook(),
		Label:    pair.ThisMachineLabel(),
		// EVERY LINE OF THE PAIRING GOES TO STDERR, so that `--once` piped into
		// a file is the reply and nothing else.
		Say: func(line string) { fmt.Fprintln(os.Stderr, line) },
	}
	if stdinIsTerminal(os.Stdin) {
		reach.AskCode = askPairingCode
	}

	tunnel, err := reach.Open(context.Background())
	if err != nil {
		return err
	}
	defer func() { _ = tunnel.Close() }()

	client, err := remote.Dial(tunnel, name, remote.Hello{
		Workspace: workspace,
		Session:   launch.session,
		Model:     launch.model,
		Level:     launch.level,
	})
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()

	agent := client.Agent()
	welcome := client.Welcome()
	if launch.once != "" {
		return runHostOnce(agent, launch.once)
	}
	// THE SAME SURFACE THE ssh DOOR OPENS, assembled by the same function. What
	// belongs to the far machine comes off the welcome and what belongs to this
	// one is resolved here; the only thing that differs between the two doors is
	// the pipe, so anything that differed in the surface would be a bug.
	options := hostOptions(client, agent, name, welcome, launch.pick)
	return runSurface(context.Background(), options)
}

// askPairingCode is the one question this door asks, on a plain terminal.
func askPairingCode(name string) (string, error) {
	fmt.Fprint(os.Stderr, pair.PairingPrompt(name))
	reader := bufio.NewReader(os.Stdin)
	typed, err := reader.ReadString('\n')
	if err != nil && strings.TrimSpace(typed) == "" {
		return "", errors.New("no pairing code was typed, so nothing was paired")
	}
	return strings.TrimSpace(typed), nil
}

// ── the machine: aforge serve ───────────────────────────────────────────────

// runServe holds this machine's outbound connection open and answers the
// devices that arrive on it.
//
// IT LISTENS FOR NOTHING AND OPENS NOTHING. There is no port here, no inbound
// rule, and nothing for a person to configure on their router — the machine
// walks out to the relay and stays there.
func runServe(args []string) error {
	flags := commandFlags("serve")
	workspace := flags.String("workspace", "", "directory a connection works in when it does not name one; empty is the directory this command was run in")
	relayAddress := flags.String("relay", "", "the relay to be reachable through; empty reads "+pair.RelayEnv)
	if err := parseCommandFlags(flags, args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("usage: aforge serve [--workspace path] [--relay https://…]")
	}

	service := strings.TrimSpace(*relayAddress)
	if service == "" {
		service = pair.Relay()
	}
	if service == "" {
		// THE HONEST STATE, AND THE ONE MOST PEOPLE WILL MEET. The relay is a
		// service and this build does not assume one exists.
		return errors.New("no relay is set up on this machine, so there is nowhere to be reachable from — set " + pair.RelayEnv + " to a relay's address, or let people in over ssh with `aforge chat --host` from their side")
	}

	here := strings.TrimSpace(*workspace)
	if here == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		here = cwd
	}

	device, err := pair.ThisDevice(pair.OpenKeeper())
	if err != nil {
		return err
	}
	host := &pair.Host{
		Service: service,
		Device:  device,
		Devices: pair.DeviceBook(),
		Desk:    &pair.Desk{},
		Say:     func(line string) { fmt.Fprintln(os.Stdout, line) },
		Open:    func(tunnel io.ReadWriteCloser) { serveOneConnection(tunnel, here) },
	}

	// ctrl+c is how this command ends, so it is caught rather than left to kill
	// the process mid-registration: the machine gives its name up on the way
	// out instead of leaving the relay to notice.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := host.Run(ctx); err != nil {
		return err
	}
	fmt.Fprintln(os.Stdout, "this machine is no longer reachable")
	return nil
}

// serveOneConnection runs `aforge engine` for one connection, with the tunnel
// as its pipes.
//
// A CHILD PROCESS RATHER THAN A FUNCTION CALL, and that is the whole reason
// this door was cheap to build. The engine is a program that reads frames on
// stdin and writes them on stdout (engine.go); ssh gives it those pipes from a
// remote shell and this gives it those pipes from a tunnel. One conversation
// per process is also the lifetime the engine was written for — it chdirs into
// its workspace and opens one session — so two connections are two processes
// and never two conversations fighting over one.
func serveOneConnection(tunnel io.ReadWriteCloser, workspace string) {
	self, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "could not find this program on disk:", err)
		return
	}
	engine := exec.Command(self, "engine", "--workspace", workspace)
	engine.Stdin = tunnel
	engine.Stdout = tunnel
	// The engine's own stderr is this machine's, where a person running
	// `aforge serve` can see it. It is never put on the tunnel: stdout is the
	// protocol and one stray line there is a frame the surface cannot parse.
	engine.Stderr = os.Stderr
	if err := engine.Run(); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, "that connection ended:", err)
	}
}

// ── the machine: aforge devices ─────────────────────────────────────────────

// runDevices lists the devices this machine lets in, and stops one.
//
// THE LIST AND THE STOPPING BOTH BELONG TO THIS MACHINE. A device cannot list
// itself out of somebody's machine and cannot stop another device; this command
// answers about the machine it is typed on, which is the same law the rest of
// aforge keeps about remote surfaces.
func runDevices(args []string) error {
	device, err := pair.ThisDevice(pair.OpenKeeper())
	if err != nil {
		return err
	}
	book := pair.DeviceBook()

	if len(args) > 0 && args[0] == "revoke" {
		return revokeDevice(book, args[1:])
	}
	if askedForHelp(args) {
		return commandHelp("devices")
	}
	if len(args) > 0 {
		return fmt.Errorf("usage: aforge devices [revoke <name> [--all]]")
	}

	paired, err := book.Devices()
	if err != nil {
		return err
	}
	fmt.Print(pair.DevicesList(device.Name(), paired, pair.OpenKeeper(), time.Now()))
	known, err := pair.Machines()
	if err != nil {
		return err
	}
	fmt.Print(pair.MachinesList(known, time.Now()))
	return nil
}

// revokeDevice stops one device, or every device answering to one name.
//
// --ALL IS A FLAG LIKE EVERY OTHER FLAG IN THIS BINARY. It used to be read by
// hand, and only when it was the FIRST word after `revoke`, so `aforge devices
// revoke laptop --all` was refused — with a usage line that did not mention
// `--all` at all. A person taking back access to their own machine was told the
// wrong grammar for the gesture they had just typed correctly. Through
// [commandFlags] and [reorder] it is now accepted in either position, printed
// by `aforge devices revoke --help`, and named in the one usage table.
func revokeDevice(book *pair.Book, args []string) error {
	flags := commandFlags("devices revoke")
	all := flags.Bool("all", false, "stop every device answering to that name, not just the one")
	if err := parseCommandFlags(flags, reorder(flags, args)); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return errors.New("usage: aforge devices revoke <name> [--all] — `aforge devices` lists the names")
	}
	name := flags.Arg(0)
	if *all {
		count, err := book.RevokeAll(name)
		if err != nil {
			return err
		}
		// ONE DEVICE STOPPED IS ONE DEVICE STOPPED, whichever flag was typed:
		// `--all` over a name only one device answers to reads as the plain
		// form, in the plain form's own sentence, rather than as a second kind
		// of event with a count in front of it.
		if count == 1 {
			fmt.Println(pair.RevokedLine(name))
			return nil
		}
		fmt.Printf("%d devices called %s have been stopped — each needs a new pairing code to come back.\n", count, name)
		return nil
	}
	gone, err := book.Revoke(name)
	if err != nil {
		return err
	}
	fmt.Println(pair.RevokedLine(gone.Label))
	return nil
}
