package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/tui2"
)

// The door to the v2 surface, and nothing else.
//
// Disconnect, don't delete (11.1): the old chat keeps working, untouched, on
// Bubble Tea v1, while the new one grows beside it behind `aforge chat --v2`
// and AFORGE_CHAT_V2=1. Nothing in this file is reachable from the old path,
// and runChat is not edited — the switch happens before it, so the old command
// runs the same bytes it ran yesterday.
//
// The default flips only when the parity checklist passes, and the old surface
// is deleted a wave after that, never in the same one.

// chatV2Env is the operator's other door. Anything but an explicitly false
// value opens v2, because someone who exported it meant it.
const chatV2Env = "AFORGE_CHAT_V2"

// chatV2LinearEnv turns on the accessible rendering without a flag, for the
// people who want it every time rather than once (10.1.5).
const chatV2LinearEnv = "AFORGE_CHAT_LINEAR"

// wantChatV2 reports whether this invocation asked for the new surface, and
// returns the arguments with the switch removed so the rest parses normally.
// An explicit --v2=false wins over the environment: a flag typed now outranks
// a variable exported once.
func wantChatV2(args []string, getenv func(string) string) (bool, []string) {
	chosen := truthyEnv(getenv(chatV2Env))
	rest := make([]string, 0, len(args))
	for _, arg := range args {
		name, value, hasValue := strings.Cut(arg, "=")
		switch strings.TrimLeft(name, "-") {
		case "v2":
			if name != "-v2" && name != "--v2" {
				rest = append(rest, arg)
				continue
			}
			if hasValue {
				chosen = truthyEnv(value)
			} else {
				chosen = true
			}
		default:
			rest = append(rest, arg)
		}
	}
	return chosen, rest
}

func truthyEnv(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

// runChatV2 opens the v2 shell. This wave it shows the room and not what
// happens in it: the transcript, composer, status line and scope rail exist as
// empty panes wired through the compositor, and the engine seams land on top
// of them in the waves that follow.
func runChatV2(args []string) error {
	flags := flag.NewFlagSet("chat --v2", flag.ContinueOnError)
	database := flags.String("db", defaultChatDB(), "path to the durable graph database")
	sessionID := flags.String("session", "", "thread session id; empty resumes the last one, \"new\" starts a fresh one")
	linear := flags.Bool("linear", truthyEnv(os.Getenv(chatV2LinearEnv)),
		"single column, no motion — the accessible rendering")
	if err := flags.Parse(reorder(args, map[string]bool{"db": true, "session": true})); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("usage: aforge chat --v2 [--db path] [--session id|new] [--linear]")
	}

	// The store is not opened yet, so the path is expanded only far enough to
	// show the operator the same string the old surface would have used. A
	// shell that displayed a session it had not read would be the first lie in
	// a surface built to stop telling them.
	path := strings.TrimSpace(*database)
	if expanded, err := expandHome(path); err == nil {
		path = expanded
	}

	return tui2.Run(context.Background(), tui2.RunOptions{
		Options: tui2.Options{
			Linear:  *linear,
			DB:      path,
			Session: strings.TrimSpace(*sessionID),
		},
	})
}
