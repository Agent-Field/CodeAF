package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/guard"
	"github.com/Agent-Field/aforge-v2/internal/tui"
	"github.com/Agent-Field/aforge-v2/internal/tui2/chat"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
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

// The engine the v2 surface is handed is the engine the old one is handed, and
// the model palette's door is one of the seams that proves it. chat.ModelControl
// is asked for by type assertion at runtime — it is optional, and a visitor
// window legitimately has none — so this is where a signature drifting apart
// from it becomes a build failure rather than a palette that silently stops
// offering a door (12.6.2's finding, applied to a new seam).
var _ chat.ModelControl = (*chatCommander)(nil)

// chatV2Env is the operator's other door. Anything but an explicitly false
// value opens v2, because someone who exported it meant it.
const chatV2Env = "AFORGE_CHAT_V2"

// Linear mode (10.1.5) used to be a flag here and nothing else, deliberately:
// somebody who wants the accessible rendering every time wants it PERSISTED,
// and a persisted preference belongs in the settings registry — where it gets
// a row, a group and a live preview (8.2.19) — not in a second environment
// variable this file invented. That row has landed
// (internal/config/settings.go: KeyLinearMode, LinearModeAt,
// AFORGE_CHAT_LINEAR) and the registry's completeness gate covers the pin.
// resolveLinear (chatv2_linear.go) is the seam: an explicit --linear on this
// command line still outranks the registry, the same way --v2 outranks
// AFORGE_CHAT_V2 above.

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

// runChatV2 opens the v2 surface on the real engine.
//
// It is deliberately the same nine lines of setup runChat performs, in the same
// order, against the same constructors: open the window, claim the residency,
// silence the logger for as long as something else owns the terminal, run. The
// only difference is the last step — which surface the engine is handed to.
// That is the lens law (Decision 7) enforced by construction: there is one way
// to build an aforge, and a second surface may choose what it draws, never what
// it is drawing.
func runChatV2(args []string) error {
	// The room policy rides this variable rather than carrying a flag of its own
	// (internal/resident's resolveRoomPolicy), and the reason it does is stated
	// there: a HALF-pinned surface — v2 rooms with legacy re-homing — is not a
	// configuration anyone wants. `--v2` typed without the variable exported was
	// exactly that combination, because the flag reached this file and the
	// resident only ever read the environment. Setting it here is not a second
	// door: it is this door telling the rest of the process which surface it
	// opened, before anything that reads it is built.
	if err := os.Setenv(chatV2Env, "1"); err != nil {
		return fmt.Errorf("select the v2 room policy: %w", err)
	}

	flags := flag.NewFlagSet("chat --v2", flag.ContinueOnError)
	database := flags.String("db", defaultChatDB(), "path to the durable graph database")
	sessionID := flags.String("session", "", "thread session id; empty resumes the last one, \"new\" starts a fresh one")
	linear := flags.Bool("linear", false,
		"single column, no motion — the accessible rendering")
	// The glyph tier's launch ladder (12.7 E.1). It is registered here and
	// resolved below, after the log redirect, because the resolver writes the
	// reason it chose a tier into chat.log — and a line written before the
	// redirect would tear through the alt screen instead.
	nerdFont := registerNerdFont(flags)
	// Colour is a flag and never an environment pin of this package's own
	// invention: the token layer detects the terminal's vocabulary from the
	// standard ecosystem variables, and an operator who disagrees says so here.
	colour := flags.String("color", "",
		"colour vocabulary: none, 16, 256 or truecolor; empty asks the terminal")
	if err := flags.Parse(reorder(args, map[string]bool{
		"db": true, "session": true, "color": true,
	})); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("usage: aforge chat --v2 [--db path] [--session id|new] [--linear] [--color name] [--nerd-font|--no-nerd-font]")
	}

	profile := tokens.DetectProfile(os.Getenv)
	if named := strings.TrimSpace(*colour); named != "" {
		parsed, ok := tokens.ParseProfile(named)
		if !ok {
			return fmt.Errorf("unknown colour vocabulary %q: use none, 16, 256 or truecolor", named)
		}
		profile = parsed
	}

	path, err := expandHome(strings.TrimSpace(*database))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create chat database directory: %w", err)
	}
	window, err := openChatWindow(path, strings.TrimSpace(*database), strings.TrimSpace(*sessionID))
	if err != nil {
		return err
	}
	defer window.close()

	role := newChatResidency(window)
	defer role.stop()
	if err := role.claim(); err != nil {
		return err
	}

	// Anything written to stderr or the standard logger while the alt screen is
	// up tears straight through it as a raw row. Same fix as the old surface,
	// for the same reason.
	if logFile, logErr := os.OpenFile(filepath.Join(filepath.Dir(defaultChatDB()), "chat.log"),
		os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); logErr == nil {
		log.SetOutput(logFile)
		defer func() {
			log.SetOutput(os.Stderr)
			_ = logFile.Close()
		}()
	}

	// Both tiers are resolved here, below the redirect: linear because the glyph
	// ladder's highest rung reads it (10.1.5 outranks an explicit --nerd-font),
	// and the glyph tier because resolving it logs which rung answered.
	linearOn := resolveLinear(flags, *linear)
	glyphs := resolveGlyphSet(flags, nerdFont, linearOn)

	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	commander := role.commander()

	// One feed for the life of the window, re-pointed at whichever engine holds
	// the role. The surface listens to this channel and nothing else, so a
	// promotion changes who is talking without changing what is being listened
	// to — see chatV2Residency.
	feed := make(chan chat.StreamEvent, streamBridgeQueue)
	bridgeStreamEventsInto(ctx, streamEventsOf(commander), feed)

	err = chat.Run(ctx, chat.Options{
		Backend:   window.graph,
		Commander: commander,
		Session:   window.session,
		Database:  path,
		Events:    feed,
		Residents: &chatV2Residency{role: role, ctx: ctx, feed: feed},
		Profile:   profile,
		Linear:    linearOn,
		GlyphSet:  glyphs,
	})
	seenErr := role.sessionClosed()
	role.stop()
	return errors.Join(err, seenErr)
}

// streamEventsOf is the live token feed, or nil when there is nothing behind
// this window to produce one. A visitor window has no head and therefore no
// stream: its replies land whole, at the poll, which is the honest rendering of
// "another process is doing the talking".
func streamEventsOf(commander tui.Commander) <-chan tui.StreamEvent {
	if commander == nil {
		return nil
	}
	return commander.StreamEvents()
}

// chatV2Residency is the v2 surface's residency door.
//
// The old window asks its commander for the role on every poll cycle and adopts
// whatever replacement it is handed (internal/tui/model.go's applyResidency).
// The new window had no such door at all: it captured one commander at startup
// and kept it forever, so an `aforge chat --v2` opened while ANY other aforge
// held the lease — including a headless `aforge do` that would release it a
// minute later — was a visitor for its whole life, with no head in process, no
// promotion when the lease came free, and nothing on screen saying so. The
// stderr notice residency.go prints is swallowed whole by the alt screen.
//
// This closes it with the same mechanism and one difference: the stream feed is
// not swapped. The window holds one channel and this type re-points the bridge
// at each newly adopted engine, so the surface never has to reason about a
// channel changing underneath a read already in flight.
type chatV2Residency struct {
	role *chatResidency
	ctx  context.Context
	feed chan chat.StreamEvent
}

// Residency implements chat.Residents.
func (r *chatV2Residency) Residency() (chat.Residency, chat.Commander) {
	state, adopt := r.role.Poll()
	role := chat.Residency{Visitor: state.Visitor, PID: state.PID, Note: state.Note}
	if adopt == nil {
		return role, nil
	}
	// A promoted window has a head to listen to for the first time, and nothing
	// else would ever go looking for it.
	bridgeStreamEventsInto(r.ctx, streamEventsOf(adopt), r.feed)
	return role, adopt
}

// bridgeStreamEvents translates the engine's keyed feed into the v2 surface's.
//
// The two vocabularies are the same five boundaries and differ only in which
// package declares them, and that is the point: the v2 surface owns its own
// event type, so a field added there — the truncation law's finish_reason, when
// a later wave wants it live rather than at the poll — reaches the new window
// without touching the struct the old one reads. The old chat keeps rendering
// the bytes it rendered yesterday, which is the whole disconnection strategy
// (11.1) stated as a type.
//
// One goroutine per engine the window has, forwarding a struct of three fields.
// It ends when that engine's feed closes or the surface does.
//
// streamBridgeQueue matches the engine's own buffer so the bridge never becomes
// the narrow point: a head streaming faster than a frame can draw backs up
// against the surface's queue, not against the provider's reader.
const streamBridgeQueue = 256

// bridgeStreamEventsInto forwards one engine's feed into a channel the caller
// owns.
//
// The destination is deliberately never closed. A window outlives the engines
// that pass through it — a visitor promoted, a resident that stood down — and a
// bridge that closed the surface's feed when ITS engine went away would tell the
// window "there is no more talking, ever" on the very cycle the residency door
// is telling it the opposite. What ends the forwarding is the window's own
// context, and what ends the window is the program.
func bridgeStreamEventsInto(ctx context.Context, source <-chan tui.StreamEvent, out chan<- chat.StreamEvent) {
	if source == nil || out == nil {
		return
	}
	guard.Go("chat v2 stream bridge", func() {
		for {
			select {
			case <-ctx.Done():
				return
			case event, open := <-source:
				if !open {
					return
				}
				select {
				case out <- chat.StreamEvent{
					Kind:    streamKindV2(event.Kind),
					Delta:   event.Delta,
					Session: event.Session,
				}:
				case <-ctx.Done():
					return
				}
			}
		}
	})
}

// streamKindV2 maps one vocabulary onto the other. It is a switch rather than a
// cast so the day either side gains a boundary the other has not heard of, the
// compiler says so here instead of the surface silently drawing the wrong
// phase.
func streamKindV2(kind tui.StreamEventKind) chat.StreamKind {
	switch kind {
	case tui.StreamStarted:
		return chat.StreamStarted
	case tui.StreamDelta:
		return chat.StreamDelta
	case tui.StreamThinking:
		return chat.StreamThinking
	case tui.StreamFinished:
		return chat.StreamFinished
	case tui.StreamFailed:
		return chat.StreamFailed
	// The tool-activity boundaries the head emits around every belt call
	// (internal/head/activity.go). They cross here rather than on a channel of
	// their own because they are the same turn as the tokens beside them.
	case tui.StreamToolBegin:
		return chat.StreamToolBegin
	case tui.StreamToolEnd:
		return chat.StreamToolEnd
	case tui.StreamToolFailed:
		return chat.StreamToolFailed
	}
	return chat.StreamFinished
}
