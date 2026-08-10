package tui2

import (
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

// What the terminal can actually do, asked rather than assumed.
//
// The doctrine here is 10.1.2, and it is mostly a list of things we refuse to
// believe. tmux will never speak the kitty keyboard protocol, so key
// disambiguation is an ENHANCEMENT: it may make a binding nicer, and no
// important binding may exist only because of it. tmux forwards focus events
// only if its user turned them on, so nothing may depend on focus. And
// COLORTERM inside tmux is tmux's claim about itself, not the terminal's — so
// the color profile is upgraded on a terminfo answer we asked for, never on an
// environment variable we read.
//
// The one thing we do ask for eagerly is synchronized output. Bubble Tea v2
// probes for it, but declines to over SSH; SSH is exactly where an atomic
// frame is worth the most, and tmux 3.7 honors mode 2026. So the shell asks
// again in Init. The query is a DECRQM: a terminal that does not know it says
// nothing, and nothing is a correct answer.

// support is a three-state answer, because "we have not asked yet" and "the
// terminal said no" are different facts and the status line should not print
// one when it means the other.
type support uint8

const (
	supportUnknown support = iota
	supportNo
	supportYes
)

func (s support) String() string {
	switch s {
	case supportYes:
		return "yes"
	case supportNo:
		return "no"
	default:
		return "?"
	}
}

// Capabilities is what the shell has learned about its terminal. Every field
// starts at its most pessimistic value, and the surface is required to be
// usable while every one of them stays there.
type Capabilities struct {
	// Disambiguation reports basic kitty-protocol key disambiguation: the
	// terminal can tell shift+enter from enter, ctrl+i from tab. Enhancement
	// only.
	Disambiguation bool

	// EventTypes reports that the terminal can send key RELEASE and repeat
	// events. We record it and bind nothing to it — a chord that needs a key
	// release is a chord tmux users do not have (10.1.2).
	EventTypes bool

	// SyncOutput reports mode 2026. When yes, a frame reaches the screen whole
	// instead of tearing halfway down.
	SyncOutput support

	// UnicodeCore reports mode 2027, which decides whether the terminal
	// measures a grapheme the way we do. It changes width arithmetic, so it is
	// worth knowing even though Bubble Tea acts on it for us.
	UnicodeCore support

	// Color is the profile Bubble Tea detected. Verified says whether the
	// terminal itself confirmed truecolor through a terminfo query, as opposed
	// to something in the environment having said so.
	Color    colorprofile.Profile
	Verified bool
}

// negotiate is the command the shell issues once at startup. It asks the two
// questions whose answers change how we render, and asks nothing that would
// commit us to a capability we then have to live without elsewhere.
func negotiate() tea.Cmd {
	return tea.Batch(
		// Mode 2026 and 2027. Bubble Tea installs the answers on its renderer;
		// we listen to the same replies so the status line can say what we got.
		tea.Raw(ansi.RequestModeSynchronizedOutput),
		tea.Raw(ansi.RequestModeUnicodeCore),
		// Truecolor, from the terminal rather than from COLORTERM. If the
		// terminal answers either capability we upgrade; if it stays silent we
		// keep whatever profile was detected and lose nothing but gradients.
		tea.RequestCapability("RGB"),
		tea.RequestCapability("Tc"),
	)
}

// requestedEnhancements is what we ask the keyboard for: nothing.
//
// Bubble Tea v2 already requests basic disambiguation on its own, and reports
// what it got. Everything in this struct beyond that — release events,
// alternate keys, all-keys-as-escapes — would be a request for a capability
// half our users cannot have, and the temptation to then bind something to it.
// Leaving it zero is the enhancement-only law, written where it is enforced.
func requestedEnhancements() tea.KeyboardEnhancements {
	return tea.KeyboardEnhancements{}
}

// observe folds a message into the capability record and reports whether
// anything changed. Returning the change is what lets the shell repaint only
// when a fact moved, rather than on every stray terminal reply.
func (c *Capabilities) observe(msg tea.Msg) bool {
	switch msg := msg.(type) {
	case tea.KeyboardEnhancementsMsg:
		before := *c
		c.Disambiguation = msg.SupportsKeyDisambiguation()
		c.EventTypes = msg.SupportsEventTypes()
		return *c != before

	case tea.ModeReportMsg:
		switch msg.Mode {
		case ansi.ModeSynchronizedOutput:
			// Recognized at all is the answer we wanted. A terminal that has
			// the mode reports it set or reset; one that does not know it
			// reports "not recognized", and some say nothing at all.
			return c.setSupport(&c.SyncOutput, !msg.Value.IsNotRecognized())
		case ansi.ModeUnicodeCore:
			return c.setSupport(&c.UnicodeCore, !msg.Value.IsNotRecognized())
		}

	case tea.ColorProfileMsg:
		if c.Color == msg.Profile {
			return false
		}
		c.Color = msg.Profile
		return true

	case tea.CapabilityMsg:
		// The terminal answered a terminfo query itself. This is the only
		// evidence of truecolor we treat as evidence.
		switch msg.String() {
		case "RGB", "Tc":
			if c.Verified {
				return false
			}
			c.Verified = true
			return true
		}
	}
	return false
}

func (c *Capabilities) setSupport(field *support, ok bool) bool {
	want := supportNo
	if ok {
		want = supportYes
	}
	if *field == want {
		return false
	}
	*field = want
	return true
}

// The send-vs-newline bindings (10.1.2), stated here rather than discovered in
// a composer. Enter sends; a newline needs a modifier; and the modifier tmux
// can deliver is bound unconditionally, so multi-line input is never a feature
// of someone else's terminal.
const (
	// SendKey is enter, on every terminal, always.
	SendKey = "enter"

	// LegacyNewlineKey is always bound, everywhere, protocol or no protocol.
	// alt+enter survives tmux, screen, and a terminal that has never heard of
	// the kitty protocol.
	LegacyNewlineKey = "alt+enter"

	// EnhancedNewlineKey is the one people reach for first. It exists only
	// where the terminal can tell the two enters apart, which is why it can
	// never be the only way to do anything.
	EnhancedNewlineKey = "shift+enter"
)

// NewlineKey names the binding a surface should ADVERTISE for inserting a
// newline: the nicest one this terminal can actually deliver.
func (c Capabilities) NewlineKey() string {
	if c.Disambiguation {
		return EnhancedNewlineKey
	}
	return LegacyNewlineKey
}

// NewlineKeys names every binding a surface must ACCEPT. Advertising one and
// accepting several is the whole shape of the enhancement-only law: the legacy
// chord is in this slice unconditionally, so a composer written against it
// cannot accidentally make Shift+Enter load-bearing.
//
// SEAM — the composer (Wave 3) binds this set, not a string literal.
func (c Capabilities) NewlineKeys() []string {
	if c.Disambiguation {
		return []string{EnhancedNewlineKey, LegacyNewlineKey}
	}
	return []string{LegacyNewlineKey}
}
