package tui3

// ── APPROVING A NEW DEVICE, ON A DEVICE THAT IS ALREADY IN THE FLEET ────────
//
// A new device asks to join and shows a link and a four-digit check. The
// person opens that link here (`/pair <link>` or a paste of it), and this
// screen says WHO is asking and lets them answer once: Approve or Deny.
//
// HOW THE ASK REACHES THIS DEVICE. The relay tells the other devices nothing
// while a request is pending; the contract delivers it by the link alone
// (docs/ux-pairing-contract.md, section 5: only `joined`, `presence` and
// `revoked` are pushed). So the screen opens on a link, and the push channel
// speaks once, afterwards: another device approved, and every device hears
// `<name> joined your fleet` ([app.announceJoined]).
//
// THE SCREEN IS A CARD IN THE PAIRING PANEL. It takes the panel's six seats
// (height, draw, hint, keys, press guard, beat) and adds no seat of its own;
// what a card does is the [panelCard] interface, so a second card (the device
// list, devices.go) is another type and not another switch.
//
// A CARD NEVER ANSWERS BY ACCIDENT. Approving lets a machine read this
// person's chats, so `enter` is nothing here: the keys are `a` and `d`, and
// `esc` closes the card WITHOUT deciding. The request stays open until it runs
// out, and the same link opens it again.

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/pair"
)

// PendingDevice is a request to join as the person sees it. Code is what the
// door needs to answer it; the rest is what the screen says.
type PendingDevice struct {
	Code        string
	Device      string // the new device's id, which the joined event names
	Name        string // opened by the door; the relay never held it plain
	Platform    string // darwin|linux|windows|ios|android|other
	Check       string // four digits the new device shows too
	RequestedAt time.Time
	ExpiresAt   time.Time
}

// DeviceRow is one device of the fleet as the device list shows it.
type DeviceRow struct {
	ID       string
	Name     string
	Platform string
	Self     bool
	Revoked  bool
	Online   bool      // set by the screen from the feed, not by the door
	LastSeen time.Time // zero when never seen
}

// Approvals is the door onto answering and revoking devices. Nil in [Options]
// is a connection that cannot, and the screens are then absent.
type Approvals interface {
	// Pending reads the request a typed link or code names.
	Pending(ctx context.Context, typed string) (PendingDevice, error)
	// Approve lets the device in; Deny turns it away. The first decision wins.
	Approve(ctx context.Context, p PendingDevice) error
	Deny(ctx context.Context, p PendingDevice) error
	// Devices lists the fleet. Revoke ends a device's membership at once.
	Devices(ctx context.Context) ([]DeviceRow, error)
	Revoke(ctx context.Context, id string) error
	// DeviceName opens a name the relay sealed, as a joined event carries it.
	DeviceName(sealed string) string
}

// The screen's own words, in the vocabulary law's terms.
const (
	approveAsk       = "A new device wants to join your fleet."
	approveCheckWord = "Check number %s - it must match the one on the new device."
	approveKeys      = "a approve · d deny · esc later"
	approveLoading   = "looking up the request…"
	approveGone      = "that request has run out - ask the new device for a new link"
	approveDenied    = "%s was turned away."
	approveTimeout   = 8 * time.Second
	joinedFleetWord  = "%s joined your fleet - your chats are now everywhere."
)

// linkShape is what link pairing is typed as (contract section 2): a short
// code with an optional key, or the codeaf.link URL. The six-digit code of the
// older pairing never matches.
var linkShape = regexp.MustCompile(`(?i)^(https?://)?(codeaf\.link/p/)?[0-9a-hjkmnp-tv-z]{8}([.#].+)?$|^(https?://)?codeaf\.link/p/.+$`)

// isPairLink is the one recogniser for a link pasted or typed where no pairing
// was asked for: it carries the key, or it is the codeaf.link URL. A bare
// eight-letter code is left alone, because a word can have that shape.
func isPairLink(typed string) bool {
	text := strings.TrimSpace(typed)
	if !isLinkShape(text) {
		return false
	}
	ref, err := pair.ReadLink(text)
	return (err == nil && len(ref.Key) > 0) || strings.Contains(strings.ToLower(text), "codeaf.link/p/")
}

func isLinkShape(typed string) bool { return linkShape.MatchString(strings.TrimSpace(typed)) }

// platformInfo is the icon and word a platform is shown as. A platform not
// listed reads as `other`, so a new one needs a row here and no code.
type platformInfo struct{ icon, word string }

var platforms = map[string]platformInfo{
	"darwin":  {"⌘", "Mac"},
	"linux":   {"$", "Linux"},
	"windows": {"▦", "Windows"},
	"ios":     {"▯", "iPhone or iPad"},
	"android": {"▯", "Android"},
	"other":   {"·", "unknown system"},
}

func platformOf(name string) platformInfo {
	if p, ok := platforms[name]; ok {
		return p
	}
	return platforms["other"]
}

// panelCard is a screen the pairing panel hosts. It is drawn and answered
// inside the panel's seats, and it lands its own messages through [cardMsg].
type panelCard interface {
	rows(width int, now time.Time, pal palette) []string
	// key answers one key and says whether the panel should close after it.
	key(a *app, name string) (cmd tea.Cmd, closeAfter bool)
	hint() string
}

// cardMsg is an answer coming back for a card. It carries the card it was
// asked for, so a late answer to a card that was closed is dropped.
type cardMsg interface{ land(a *app) tea.Cmd }

// approveCard is the approve screen's state. req is nil while it loads.
type approveCard struct {
	req     *PendingDevice
	line    string // a sentence that replaces the question: loading, refused, done
	working bool
}

type pendingMsg struct {
	card *approveCard
	req  PendingDevice
	err  error
}

type decidedMsg struct {
	card     *approveCard
	req      PendingDevice
	approved bool
	err      error
}

func (m pendingMsg) land(a *app) tea.Cmd {
	if a.pair.card != panelCard(m.card) {
		return nil
	}
	if m.err != nil {
		m.card.line = approveFailure(m.err)
		return nil
	}
	m.card.req, m.card.line = &m.req, ""
	return nil
}

func (m decidedMsg) land(a *app) tea.Cmd {
	if a.pair.card != panelCard(m.card) {
		return nil
	}
	m.card.working = false
	switch {
	case m.err != nil:
		m.card.line = approveFailure(m.err)
	case m.approved:
		a.pair.close()
		a.addMachine.grew()
		a.toastJoined(m.req.Name, m.req.Device)
	default:
		a.pair.close()
		a.note(fmt.Sprintf(approveDenied, m.req.Name))
	}
	return nil
}

// approveFailure is the one sentence for a refused or failed answer.
func approveFailure(err error) string {
	if err == nil {
		return ""
	}
	return approveGone + " (" + err.Error() + ")"
}

func (c *approveCard) hint() string {
	if c.req == nil || c.working {
		return pairDoneKeys
	}
	return approveKeys
}

func (c *approveCard) rows(width int, now time.Time, pal palette) []string {
	if c.req == nil {
		text := approveLoading
		if c.line != "" {
			text = c.line
		}
		return dressed(wrap(text, max(width, 4)), pal.ink)
	}
	r, info := c.req, platformOf(c.req.Platform)
	out := dressed(wrap(approveAsk, max(width, 4)), pal.ink)
	out = append(out, pal.accent(fit("  "+info.icon+" "+r.Name+" · "+info.word, width)))
	out = append(out, pal.dim(fit("  "+requestAge(r, now), width)))
	out = append(out, dressed(wrap(fmt.Sprintf(approveCheckWord, r.Check), max(width, 4)), pal.ink)...)
	if c.line != "" {
		out = append(out, dressed(wrap(c.line, max(width, 4)), pal.accent)...)
	}
	return out
}

// requestAge says when the device asked and how long the request has left.
func requestAge(r *PendingDevice, now time.Time) string {
	left := r.ExpiresAt.Sub(now)
	if left <= 0 {
		return askedWord(r.RequestedAt, now) + " · run out"
	}
	return askedWord(r.RequestedAt, now) + " · " + itoa(int((left+time.Minute-1)/time.Minute)) + " min left"
}

// askedWord is when the device asked: "just now" inside a minute, else "5m ago".
func askedWord(at, now time.Time) string {
	if ago := sinceAt(at, now); ago != "now" {
		return "asked " + ago + " ago"
	}
	return "asked just now"
}

func dressed(lines []string, dress func(string) string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = dress(l)
	}
	return out
}

// key is the card's whole claim on the keyboard: a, d, esc, and nothing else.
func (c *approveCard) key(a *app, name string) (tea.Cmd, bool) {
	if name == "esc" {
		return nil, true
	}
	if c.req == nil || c.working {
		return nil, false
	}
	answer, ok := map[string]bool{"a": true, "d": false}[name]
	if !ok {
		return nil, false
	}
	c.working = true
	return a.decide(c, answer), false
}

// decide answers the request off the loop.
func (a *app) decide(c *approveCard, approve bool) tea.Cmd {
	door, req := a.approvals, *c.req
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), approveTimeout)
		defer cancel()
		call := door.Deny
		if approve {
			call = door.Approve
		}
		return decidedMsg{card: c, req: req, approved: approve, err: call(ctx, req)}
	}
}

// openApprove is `/pair <link>`: read the request and show the card.
func (a *app) openApprove(typed string) tea.Cmd {
	if a.approvals == nil {
		a.note(pairUnavailableWord)
		return nil
	}
	if a.pair.run != nil && !a.pair.done {
		a.pair.open = true
		a.touch()
		return nil
	}
	card := &approveCard{}
	a.showCard(card)
	door := a.approvals
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), approveTimeout)
		defer cancel()
		req, err := door.Pending(ctx, typed)
		return pendingMsg{card: card, req: req, err: err}
	}
}

// showCard puts a card in the pairing panel, replacing any card or finished
// pairing that was there.
func (a *app) showCard(c panelCard) {
	a.closeLists()
	a.dismissWelcome()
	// HOME IS THE WHOLE SCREEN and draws no panel, so a card opened from it
	// would be state nobody can see: the person steps out to the box first.
	a.closeHome()
	a.pair.close()
	a.pair.open, a.pair.card = true, c
	a.touch()
}

// cardKey is the card's press: its own keys, and ctrl+c is the app's.
func (a *app) cardKey(msg tea.KeyPressMsg) tea.Cmd {
	cmd, closeAfter := a.pair.card.key(a, msg.String())
	if closeAfter {
		a.pair.close()
		a.letGoOffHome()
	}
	a.touch()
	return cmd
}

// toastJoined is the news that a device is in. The device is remembered, so
// the joined event the relay sends back to this same screen does not say it a
// second time.
func (a *app) toastJoined(name, device string) {
	a.approve.markApproved(device)
	a.toast(fmt.Sprintf(joinedFleetWord, name))
}

// toast says a sentence where it is seen at once: in the transcript, and on
// home's message line when home is what the person is looking at.
func (a *app) toast(text string) {
	a.note(text)
	if a.at(pageHome) {
		a.home.say(text, "")
	}
}

// approveState is what the approve screens remember between cards.
type approveState struct {
	// seq is the newest joined event said, and approved the devices this
	// screen approved itself, whose joined event is already told.
	seq      uint64
	approved map[string]bool
}

func (s *approveState) markApproved(device string) {
	if s.approved == nil {
		s.approved = map[string]bool{}
	}
	s.approved[device] = true
}

// told reports, once, whether the device's news was already said here.
func (s *approveState) told(device string) bool {
	was := s.approved[device]
	delete(s.approved, device)
	return was
}

// announceJoined says each joined event the feed holds that this screen has
// not said, once.
func (a *app) announceJoined() {
	if a.dirFeed == nil || a.approvals == nil {
		return
	}
	for _, j := range a.dirFeed.State().Joined {
		if j.Seq <= a.approve.seq {
			continue
		}
		a.approve.seq = j.Seq
		if !a.approve.told(j.Device) {
			a.toast(fmt.Sprintf(joinedFleetWord, a.approvals.DeviceName(j.Name)))
		}
	}
}
