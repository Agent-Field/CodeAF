package tui3

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// THE ATTACHMENT TRAY: a picture is not a sentence.
//
// Everything else that comes out of the @ completion is TEXT — "@internal/
// session/agent.go" is a word in a sentence, and the agent's read tool resolves
// it if it wants to (files.go says why at length). An image cannot be that:
// there is no tool on the belt that turns a PNG into anything a model can look
// at, so a path typed into the sentence is a path the model can only guess
// about. The picture has to travel as content.
//
// So an image chosen from the completion does not land in the draft at all. It
// lands in a TRAY above it — one dim chip per picture — and the draft stays the
// sentence the person is writing. The chips are what the next message carries,
// they are removed the way the character behind the caret is removed, and they
// survive a refusal: a message the surface could not send is a message the
// person still holds, pictures included.
//
// What lives where:
//
//	the tray's state      [app.chips], and [app.sent] while a message is in flight
//	the row above the box [app.chipStrip], drawn by [app.inputBlock] (input.go)
//	what enter does       [app.submitImages]
//	what the completion does when the file is an image  [app.completeFile]
//	the gate              session.Config.SupportsImages, wired in cmd/aforge

// maxAttachBytes is the per-picture ceiling this surface enforces, and it is
// deliberately the SAME number internal/session's own guard uses
// (session/image.go's maxImageBytes) rather than a friendlier one: a photo that
// this tray accepted and the session then refused would be a refusal arriving
// after the message was already on screen, for a reason the person could have
// been told at the door.
//
// It is enforced against the STAT first, exactly as session does, because a
// limit checked after the read is a limit that already pulled a
// multi-gigabyte file into memory to discover it was too big.
const maxAttachBytes = 10 << 20

// chipGap is the space between two chips. Two cells, because one reads as a
// single wrapped label and three reads as a column.
const chipGap = "  "

// imageExtensions is what this surface will attach, and it is the same five
// internal/session will send (session/image.go's imageMediaTypes). A sixth
// format offered here would be a chip that becomes an error at submit.
var imageExtensions = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".webp": true, ".gif": true,
}

// isImagePath reports whether a path names a picture this surface can attach.
func isImagePath(path string) bool {
	return imageExtensions[strings.ToLower(filepath.Ext(strings.TrimSpace(path)))]
}

// chip is one attached picture. It holds the RESOLVED path — the completion
// offers workspace-relative names and a person types "~/shot.png", and the
// thing that eventually reads the file must not have to know which — and the
// tray shows its base name, because the tray is a reminder and not a location.
type chip struct{ path string }

func (c chip) name() string { return filepath.Base(c.path) }

// chipMark is the tray's glyph: a filled square with a border, which is a
// picture at one cell. The ascii floor gets an asterisk rather than a box
// approximation, for the reason styles.go states — a terminal that cannot draw
// U+25A3 should be given something it can, not something close.
func chipMark(pal palette) string {
	if pal.ascii {
		return "*"
	}
	return "▣"
}

// attach adds one picture to the tray, and reports whether it changed anything.
// The same file twice is one chip: a person who picked a name out of the
// completion twice meant it once, and a message carrying the same photo two
// times pays for it two times.
func (a *app) attach(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	for _, held := range a.chips {
		if held.path == path {
			return false
		}
	}
	a.chips = append(a.chips, chip{path: path})
	a.touch()
	return true
}

// dropChip removes the last chip — backspace on an empty box (input.go). It is
// the same gesture as deleting the character behind the caret, and with nothing
// typed the thing behind the caret is the tray.
func (a *app) dropChip() bool {
	if len(a.chips) == 0 {
		return false
	}
	a.removeChip(len(a.chips) - 1)
	return true
}

// removeChip takes one out by index — a click on it. THE DRAFT FOLLOWS THE TRAY:
// the picture's own `[image #n]` comes out of the sentence and everything behind
// it counts down, so the number a person reads is always the picture the model
// will be looking at ([app.forgetToken]).
func (a *app) removeChip(i int) {
	if i < 0 || i >= len(a.chips) {
		return
	}
	held := len(a.chips)
	a.chips = append(a.chips[:i], a.chips[i+1:]...)
	a.forgetToken(i+1, held)
	a.touch()
}

// attachPath is the /image command: one path, attached, or one note saying why
// not. Every refusal names the file, because "not an image" about a path the
// person typed is a sentence they can act on and "could not attach" is not.
func (a *app) attachPath(raw string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		a.note("/image takes a path · try /image shot.png")
		return
	}
	path := a.resolvePath(raw)
	if !isImagePath(path) {
		a.note(filepath.Base(path) + " is not a picture · png, jpeg, webp and gif are")
		return
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		a.note("no such picture: " + raw)
		return
	}
	if !a.attach(path) {
		a.note(filepath.Base(path) + " is already attached")
	}
}

// resolvePath makes a typed or completed path absolute the way a person means
// it: "~" is home, a relative name is relative to the directory this
// conversation is about, and an absolute path is left alone.
//
// EVERY PATH THIS FUNCTION RESOLVES IS ON THIS MACHINE, and on a remote session
// that is what makes the root the local directory rather than the workspace
// ([app.pathRoot]). The one thing a person types a path for here is a picture,
// and the picture is on the laptop in front of them — its bytes travel with the
// message (internal/remote's SubmitImage), so the file never has to exist on the
// far side. Joining "shot.png" onto the far machine's workspace would name a
// path that exists on neither machine.
func (a *app) resolvePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "~" || strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(path, "~"), "/"))
		}
	}
	if root := a.pathRoot(); !filepath.IsAbs(path) && root != "" {
		path = filepath.Join(root, path)
	}
	return path
}

// ── the row above the box ───────────────────────────────────────────────────

// chipLabels is the tray's text, one label per chip. It is one function so that
// what is DRAWN and what a click is resolved against can never disagree about
// where a chip starts.
func chipLabels(chips []chip, pal palette) []string {
	mark := chipMark(pal)
	out := make([]string, 0, len(chips))
	for i, c := range chips {
		// THE NUMBER IS AS MUCH THE POINT OF A CHIP AS THE NAME IS. It is what
		// `[image #2]` in the sentence refers to and what the model sees second,
		// and a tray that showed only names would leave the person counting from
		// the left to find out which picture they were talking about.
		out = append(out, mark+" #"+strconv.Itoa(i+1)+" "+c.name())
	}
	return out
}

// chipStrip is the tray as one row, dim: the attachments are a fact about the
// message being written, not a thing being said, and the surface says what it
// is doing in the same voice it says everything else about itself.
//
// A PICKED HARNESS RIDES THE SAME ROW, first (harnesspick.go). It is the same
// kind of fact — something the next message carries besides its words — and the
// tray is the one place this surface keeps those. Its own cell is INK rather
// than dim, because it is the one thing up here that changes what enter does.
func (a *app) chipStrip(width int) string {
	cells := a.harnessTrayCells()
	labels := chipLabels(a.chips, a.pal)
	if len(cells) == 0 && len(labels) == 0 {
		return ""
	}
	painted := make([]string, 0, len(cells)+len(labels))
	for at, cell := range cells {
		if at == 0 {
			// THE POINTER LIGHTS ONE THING ON THIS ROW, never the row. Every cell up
			// here takes something off the message being written and each takes off a
			// different thing, so a band across the tray would offer to drop the
			// picture beside the one a person is aiming at (hover.go's law). The band
			// goes round exactly the cell's own cells, which is what [palette.hover]
			// does when it is given no width to pad to.
			if a.hoveringChip(trayHarnessChip) {
				painted = append(painted, a.pal.hover(a.pal.ink(cell), 0))
				continue
			}
			painted = append(painted, a.pal.ink(cell))
			continue
		}
		// The hint after it says what to do next; it is a sentence and comes off
		// nothing, so it does not light.
		painted = append(painted, a.pal.dim(cell))
	}
	for i, label := range labels {
		if a.hoveringChip(i) {
			painted = append(painted, a.pal.hover(a.pal.dim(label), 0))
			continue
		}
		painted = append(painted, a.pal.dim(label))
	}
	return fit(strings.Join(painted, chipGap), width)
}

// chipAt resolves a column to the chip drawn on it, or -1.
func chipAt(labels []string, x int) int {
	at := 0
	for i, label := range labels {
		width := ansi.StringWidth(label)
		if x >= at && x < at+width {
			return i
		}
		at += width + len(chipGap)
	}
	return -1
}

// chipPress is a click on the tray: the chip under the pointer comes off.
//
// The row is located through [app.chrome] rather than from a count of its own,
// for the reason view.go states about every geometric question on this surface:
// the frame, the hit-testing and the height must ask ONE function where the
// input block starts. The tray is that block's first row (input.go), so it is
// the input block's start and nothing else has to be known.
func (a *app) chipPress(x, y int) bool {
	at, ok := a.chipTrayTarget(x, y)
	if !ok {
		return false
	}
	if at == trayHarnessChip {
		a.dropHarnessChip()
		return true
	}
	a.removeChip(at)
	return true
}

// trayHarnessChip is what [app.chipTrayTarget] answers for the picked harness's
// own cell, which is not one of [app.chips] and has a different thing done to it.
const trayHarnessChip = -1

// chipTrayTarget resolves a pointer on the tray to the one thing it is over, and
// reports whether it was over anything at all.
//
// It is ONE function because the press and the pointer must never be able to
// disagree about which picture a cell belongs to: what lights is what comes off
// (hover.go's law). The row is located through [app.chrome] rather than from a
// count of its own, for the reason view.go states about every geometric question
// on this surface — the frame, the hit-testing and the height ask one function
// where the input block starts, and the tray is that block's first row (input.go).
//
// THE FIELD TEST IS FIRST AND IT IS WHAT KEEPS THIS CHEAP. A motion arrives once
// per cell the pointer crosses and nearly every session has nothing on the tray
// at all, so the frames that carry one are the only frames that pay for the
// chrome this rebuilds.
func (a *app) chipTrayTarget(x, y int) (int, bool) {
	cells := a.harnessTrayCells()
	if (len(a.chips) == 0 && len(cells) == 0) || a.sheet.open || a.pick.open {
		return 0, false
	}
	width, height := a.size()
	rows, _, _, _ := a.chrome(width)
	at := len(rows) - 1 - a.overlayHeight() - a.inputHeight()
	if at < 0 || y != height-len(rows)+at {
		return 0, false
	}
	column := x - len(inputPad)
	// THE HARNESS CELL IS ASKED FIRST BECAUSE IT IS DRAWN FIRST, and the
	// pictures start after it — the offset is computed from the same cells the
	// row was built from, so what is drawn and what a click resolves against
	// cannot disagree (harnesspick.go).
	if len(cells) > 0 {
		if chipAt(cells, column) == 0 {
			return trayHarnessChip, true
		}
		column -= harnessTrayWidth(cells)
	}
	i := chipAt(chipLabels(a.chips, a.pal), column)
	if i < 0 {
		return 0, false
	}
	return i, true
}

// ── sending them ────────────────────────────────────────────────────────────

// chipMarkers is what an image-bearing message leaves in the transcript: the
// file names, dim, after the words.
//
//	› what is wrong with this [image #1]  [#1 chart.png]
//
// The number is the one the sentence's token carries and the one on the chip it
// was sent from, so a reader can see which file `[image #1]` was.
//
// It is a MARKER and not a rendering. A terminal cell is not a place to show a
// picture, and the honest thing to draw for one is the name of the file the
// person pointed at, in the voice the surface uses for its own asides.
func chipMarkers(chips []chip, pal palette) string {
	if len(chips) == 0 {
		return ""
	}
	names := make([]string, 0, len(chips))
	for i, c := range chips {
		names = append(names, "[#"+strconv.Itoa(i+1)+" "+c.name()+"]")
	}
	return pal.dim(strings.Join(names, " "))
}

// userLine is the transcript's version of what was just sent: the sentence, and
// the pictures that went with it.
func userLine(text string, chips []chip, pal palette) string {
	markers := chipMarkers(chips, pal)
	switch {
	case markers == "":
		return text
	case strings.TrimSpace(text) == "":
		return markers
	default:
		return text + " " + markers
	}
}

// submitImages is enter with a full tray. It is [app.submit] with pictures, and
// it is a second function rather than a flag on the first because exactly two
// things differ: what lands in the transcript (the markers) and which method is
// called on the seam.
//
// The FILES ARE READ IN THE COMMAND, off the loop. Ten megabytes is small
// enough to be nothing and large enough to be a visible hitch at the moment a
// person pressed enter, and nothing on this surface waits on a disk while it is
// drawing (draft.go says the same about a file a hundredth the size).
//
// The tray is emptied here and REMEMBERED in [app.sent]: the message may still
// be refused — by the gate, or by a picture that grew past the ceiling since it
// was attached — and a refusal that also lost the person's attachments would
// make them go and find the files again.
func (a *app) submitImages(text string) tea.Cmd {
	agent, ctx := a.agent, a.ctx
	chips := append([]chip(nil), a.chips...)
	a.chips, a.sent = nil, chips
	// EVERY PICTURE IS NAMED IN THE WORDS THAT GO WITH IT. A pasted one already
	// carries its `[image #n]` where the person put it; one attached by /image or
	// the @ completion has none, and gets its token appended here so that "image
	// 2" means something whichever door the picture came in by (imagepaste.go).
	// The transcript is drawn from the same string, so what the person reads and
	// what the model reads are one sentence.
	text = imageSentence(text, chips)

	if a.stream == nil {
		a.turn++
	}
	// And a turn starting disarms the door here too, for [app.submitting]'s
	// reason: from this moment ctrl+c is the interrupt again (quitarm.go).
	a.disarmQuit()
	a.sel = -1
	// The person's line goes in WITHOUT cutting a reply that is still streaming
	// in two — see [app.said], which is the whole of this wave's render-order fix
	// and belongs to every door onto the transcript, not just the plain one.
	a.said(entry{
		kind: entryUser, text: userLine(text, chips, a.pal), turn: a.turn,
	})
	a.state = stateWorking
	a.lastDelta = time.Now()
	// The turn is open and the first request is out with nothing back from it.
	a.awaited = time.Now()
	a.follow()
	a.touch()
	return tea.Batch(func() tea.Msg {
		images, err := readAttachments(chips)
		if err != nil {
			return submittedMsg{err: err}
		}
		ch, err := agent.SubmitImage(ctx, text, images)
		return submittedMsg{ch: ch, err: err}
	}, a.wake())
}

// chipsSettled is what a submit's answer does to the tray, and it is called for
// EVERY submit — a plain one holds no chips and this is a no-op for it.
//
// A refusal puts the pictures back, in front of anything attached while the
// message was in flight and without duplicating it. A success drops them: they
// are in the conversation now.
func (a *app) chipsSettled(err error) {
	if len(a.sent) == 0 {
		return
	}
	sent := a.sent
	a.sent = nil
	if err == nil {
		return
	}
	restored := append([]chip(nil), sent...)
	for _, held := range a.chips {
		if !heldBy(restored, held.path) {
			restored = append(restored, held)
		}
	}
	a.chips = restored
	a.touch()
}

func heldBy(chips []chip, path string) bool {
	for _, c := range chips {
		if c.path == path {
			return true
		}
	}
	return false
}

// readAttachments turns the tray into what the session takes, and it is the
// door's copy of session's own guard: stat first, refuse what is too big, and
// name the FILE in the refusal — a person holding four chips needs to know
// which one the message is stuck on.
//
// The bytes are read here rather than left to the session so that the message
// is assembled from what was on disk at the moment enter was pressed, which is
// the picture the person was looking at.
func readAttachments(chips []chip) ([]session.Image, error) {
	out := make([]session.Image, 0, len(chips))
	for _, c := range chips {
		info, err := os.Stat(c.path)
		if err != nil || info.IsDir() {
			return nil, fmt.Errorf("could not read %s", c.name())
		}
		if info.Size() > maxAttachBytes {
			return nil, oversizeAttachment(c)
		}
		data, err := os.ReadFile(c.path)
		if err != nil {
			return nil, fmt.Errorf("could not read %s", c.name())
		}
		// Checked again: the file could have grown between the stat and the
		// read, which is session's own reasoning about its own guard.
		if len(data) > maxAttachBytes {
			return nil, oversizeAttachment(c)
		}
		out = append(out, session.Image{Path: c.path, Bytes: data})
	}
	return out, nil
}

func oversizeAttachment(c chip) error {
	return fmt.Errorf("%s is over the %dMB image limit", c.name(), maxAttachBytes>>20)
}
