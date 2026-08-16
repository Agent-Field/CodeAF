package tui2

import "time"

// The interruption budget, made mechanical.
//
// 10.5.27 is a taxonomy, not a transport: EXACTLY three events may reach the
// desktop — a question that needs answering, a delivery, a failure — and
// everything else the surface knows is progress, which goes to the taskbar and
// the title and nowhere else. That closure is the product decision, and this
// file is where it stops being a rule someone remembers and becomes a rule the
// type system keeps: [AttentionKind] is a closed enum with no general-purpose
// member, and there is no Notify variant that takes an arbitrary string for a
// reason. A door labelled "other" would be open inside a week.

// defaultAppName is what the title says when nobody said otherwise.
const defaultAppName = "aforge"

// The storm guards. Both are about the same failure: a surface that polls, and
// therefore re-derives the same fact thirty times a second, must not turn that
// into thirty desktop notifications.
const (
	// notifyRepeat is how long an IDENTICAL notification is swallowed. Same
	// kind, same title, same body inside this window is the same event being
	// re-noticed, not a second thing happening.
	notifyRepeat = 2 * time.Second

	// notifyFloor is the minimum gap between two notifications of the same
	// KIND. Four workers failing in the same tenth of a second is one
	// interruption; the user goes and looks either way. Different kinds are
	// never suppressed by this — a failure arriving right behind a delivery is
	// exactly the pair you must not lose.
	notifyFloor = 300 * time.Millisecond
)

// AttentionKind is the interruption budget. It is closed: these three, and
// nothing else, may cross to the desktop (10.5.27).
type AttentionKind uint8

const (
	// AttentionNone is the zero value and is not an event. It exists so that a
	// forgotten field is silence rather than a notification.
	AttentionNone AttentionKind = iota

	// AttentionNeedsInput: a turn stopped and is waiting on the human — an
	// open question, a consent gate, an approval.
	AttentionNeedsInput

	// AttentionDelivery: work finished and there is something to look at.
	AttentionDelivery

	// AttentionFailure: work stopped in a way it should not have.
	AttentionFailure
)

func (k AttentionKind) valid() bool {
	return k >= AttentionNeedsInput && k <= AttentionFailure
}

func (k AttentionKind) String() string {
	switch k {
	case AttentionNeedsInput:
		return "needs-input"
	case AttentionDelivery:
		return "delivery"
	case AttentionFailure:
		return "failure"
	default:
		return "none"
	}
}

// TerminalOptions is everything the surface is allowed to say to the terminal
// from OUTSIDE the frame. Each field is a channel, each channel is optional,
// and each one off is a surface that still works — that is the test every one
// of them has to pass before it is allowed to exist.
//
// [DefaultTerminalOptions] turns them all on. The zero value turns them all
// off, which is what the golden harness and any headless driver want: a shell
// built from the zero value emits not one byte outside its frame.
type TerminalOptions struct {
	// AppName is the name in the terminal title. Empty takes "aforge".
	AppName string

	// Title lets the terminal title carry the attention count (7.2).
	Title bool

	// Progress lets a running turn drive OSC 9;4 taskbar progress.
	Progress bool

	// Notify lets the three interruption-budget events reach the desktop.
	Notify bool

	// Bell lets the notification ladder fall all the way to a BEL when the
	// terminal has no richer channel. Off means a terminal we cannot notify
	// properly is simply not notified — quiet, and honest about it.
	Bell bool

	// PromptMarks emits OSC 133 prompt-zone marks on committed user messages,
	// so the terminal's own "jump to previous prompt" walks the conversation.
	//
	// OFF BY DEFAULT, and the reason is the surface and not the mark: this
	// shell is always the alt screen (Shell.View sets AltScreen), and a
	// semantic zone in the alt screen has nothing durable to anchor to. The
	// mark lands wherever the cursor happened to be for that frame, which
	// [Shell.MarkPrompt] has always said out loud — and iTerm2 does not keep an
	// invisible list, it DRAWS each mark as a blue triangle in the left gutter
	// of that row. So the channel bought nothing (prompt-jump navigation cannot
	// walk an alt screen's scrollback, because there is none) and cost a stray
	// chevron that appeared to wander down the conversation.
	//
	// The machinery stays, and so does this switch: an inline mode — one that
	// writes the conversation into the primary screen's scrollback — is exactly
	// the surface the mark was designed for, and it will turn this back on.
	PromptMarks bool

	// Hyperlinks lets trusted chrome render OSC 8 links (5.21).
	Hyperlinks bool
}

// DefaultTerminalOptions is every channel a full-screen surface can honestly
// use. It is what a real terminal gets; each channel still degrades on its own,
// so "on" never means "required".
//
// PromptMarks is the one channel that is off, and it is off for a reason about
// THIS surface rather than about terminals — see the field's own comment.
func DefaultTerminalOptions() TerminalOptions {
	return TerminalOptions{
		AppName:    defaultAppName,
		Title:      true,
		Progress:   true,
		Notify:     true,
		Bell:       true,
		Hyperlinks: true,
	}
}

// sane fills in the defaults a caller left blank.
func (o TerminalOptions) sane() TerminalOptions {
	o.AppName = oscText(o.AppName, oscTitleMax)
	if o.AppName == "" {
		o.AppName = defaultAppName
	}
	return o
}

// notifier decides whether an event becomes bytes, and which bytes. It owns
// two facts the caller does not have — whether the terminal is focused, and
// what it has already said recently — and nothing else.
type notifier struct {
	// focus is what the TERMINAL said about its own focus, three-state.
	//
	// The three states matter more here than anywhere else in the surface.
	// Focus reporting is off in tmux by default and this shell never turns it
	// on (10.1.2, and View's ReportFocus=false), so supportUnknown is the
	// NORMAL state, not an edge case — which is exactly why the gate is
	// written as "suppress when the terminal SAID it is focused" and never as
	// "notify when the terminal said it is not". Written the second way, the
	// ordinary tmux user would receive no notifications at all and would have
	// no way to find out why. Unknown degrades to notifying, always.
	focus support

	last   notifyKey
	lastAt time.Time

	// now is injected so the storm guards are testable without sleeping.
	now func() time.Time
}

type notifyKey struct {
	kind        AttentionKind
	title, body string
}

func newNotifier() notifier {
	return notifier{now: time.Now}
}

// setFocus records what the terminal said. It returns whether the fact moved,
// so a shell can decline to repaint for a message that told it nothing new.
func (n *notifier) setFocus(focused bool) bool {
	want := supportNo
	if focused {
		want = supportYes
	}
	if n.focus == want {
		return false
	}
	n.focus = want
	return true
}

// suppressed reports whether the focus gate is closed. Only a terminal that
// actively told us it has focus closes it.
func (n *notifier) suppressed() bool { return n.focus == supportYes }

// emit renders one event, or "" when this event must not become bytes.
//
// The order of the gates is the order of the reasons, cheapest and most
// absolute first: an event outside the taxonomy, then an operator who turned
// the channel off, then a terminal we are looking at, then an event we already
// said. Every one of them returns the empty string, and the caller writes
// nothing rather than writing nothing-shaped bytes — "zero bytes when nothing
// changed" is a property of this function returning "", not of a writer being
// clever downstream.
func (n *notifier) emit(opts TerminalOptions, caps Capabilities, kind AttentionKind, title, body string) string {
	if !kind.valid() || !opts.Notify {
		return ""
	}
	tier := caps.notifyTier()
	if tier == tierBell && !opts.Bell {
		return ""
	}
	if n.suppressed() {
		return ""
	}

	key := notifyKey{kind: kind, title: title, body: body}
	now := n.clock()
	if !n.lastAt.IsZero() {
		since := now.Sub(n.lastAt)
		if since >= 0 {
			if key == n.last && since < notifyRepeat {
				return ""
			}
			if kind == n.last.kind && since < notifyFloor {
				return ""
			}
		}
	}

	bytes := notifyBytes(tier, title, body)
	if bytes == "" {
		return ""
	}
	n.last, n.lastAt = key, now
	return bytes
}

func (n *notifier) clock() time.Time {
	if n.now == nil {
		return time.Now()
	}
	return n.now()
}
