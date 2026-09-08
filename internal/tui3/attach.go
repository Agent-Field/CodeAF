package tui3

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/remote"
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
// ── AND ORDINARY FILES RIDE THE SAME TRAY (`/attach`) ───────────────────────
//
// A log, a CSV, a PDF is not a picture and does not travel as content: it
// travels as a FILE, and what the model is told is its PATH, because the belt
// has a `read` tool and a 4MB CSV in the context window is 4MB nobody asked
// for. That is the one difference, and everything else about a file chip is a
// picture chip — the same tray, the same backspace, the same survival of a
// refusal.
//
// WHERE THE BYTES GO IS A QUESTION ABOUT WHOSE DISK IT IS, and there are
// exactly two answers:
//
//   - LOCAL: the session runs on this machine, the path already means something
//     to it, and nothing moves. The message names the file where it sits, which
//     is what internal/remote's image.go already blesses in as many words for a
//     picture that arrives with a path and no bytes.
//   - OVER A CONNECTION: the engine has never seen this disk, so the bytes
//     travel with the message and the engine writes them into that session's
//     own `attachments/` folder before the turn opens — and the path in the
//     sentence is a path on the machine that owns the journal (internal/remote's
//     file.go holds the whole of that law).
//
// The ceilings below therefore apply to the second case and not the first: a
// limit exists because bytes cross a wire, and refusing a file that is not
// going anywhere would be a rule invented for its own sake.
//
// What lives where:
//
//	the tray's state      [app.chips], and [app.sent] while a message is in flight
//	the row above the box [app.chipStrip], drawn by [app.inputBlock] (input.go)
//	what enter does       [app.submitImages]
//	what /image does      [app.attachPath]
//	what /attach does     [app.attachFilePath]
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

// maxAttachedFileBytes is the biggest single FILE this surface will put on a
// message that has to cross a connection, and the number is arithmetic on the
// wire's own ceiling rather than a taste.
//
// internal/remote's frameCap is 64MB and it is a cap on ONE LINE: a whole
// message — its words, its pictures and its files — travels as a single JSON
// frame, and bytes inside JSON are base64, which costs A THIRD MORE than the
// file weighs. So 48MB of raw attachment is what an entire message may really
// spend, and 16MB per file leaves room for two large ones, a screenshot and the
// sentence they came with without ever putting the frame within reach of the
// cap. It is not larger because every call on that wire has a ten-second
// deadline (internal/remote's callDeadline): a ceiling much above this would be
// a limit the connection failed before the number did.
const maxAttachedFileBytes = 16 << 20

// maxAttachedTotalBytes is what ALL the files on one message may weigh
// together — two thirds of the 48MB budget above, leaving the rest for the
// pictures on the same tray, which carry their own [maxAttachBytes] each.
//
// It exists because the per-file limit alone does not bound a message: four
// files under the ceiling are still four files on one line.
const maxAttachedTotalBytes = 32 << 20

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

// chip is one thing attached to the next message. It holds the RESOLVED path —
// the completion offers workspace-relative names and a person types
// "~/shot.png", and the thing that eventually reads the file must not have to
// know which — and the tray shows its base name, because the tray is a reminder
// and not a location.
type chip struct {
	path string
	// file marks a chip that is NOT a picture: it travels as a file and the
	// model is told its path, where a picture travels as content and is looked
	// at (this file's header states the difference).
	//
	// It is a flag on the one chip type rather than a second tray, and the
	// reason is that the tray is a fact about the MESSAGE: everything on it goes
	// with the next thing the person sends, backspace takes the last one off
	// whatever it was, and a refusal hands all of it back. Two lists would have
	// been two answers to "what is this message carrying".
	file bool
}

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

// fileChipMark is the same cell for a file: a page rather than a picture. It is
// a DIFFERENT glyph on purpose — the two kinds of chip do different things to a
// message, and a tray that drew them alike would leave a person wondering why
// their CSV was never looked at.
func fileChipMark(pal palette) string {
	if pal.ascii {
		return "+"
	}
	return "▤"
}

// pictureChips and fileChips split the tray into its two kinds.
//
// THE PICTURES ARE COUNTED AMONG THEMSELVES AND SO IS EVERYTHING THAT READS
// THEM. `[image #2]` in a sentence means the second PICTURE, not the second
// chip, so every place that numbers — the tray's own labels, the sentence's
// tokens, the markers in the transcript, the renumber after one comes off — is
// handed this slice and not the tray.
func pictureChips(chips []chip) []chip {
	out := make([]chip, 0, len(chips))
	for _, c := range chips {
		if !c.file {
			out = append(out, c)
		}
	}
	return out
}

func fileChips(chips []chip) []chip {
	out := make([]chip, 0, len(chips))
	for _, c := range chips {
		if c.file {
			out = append(out, c)
		}
	}
	return out
}

// pictureOrdinal is where one chip sits among the pictures, counting from 1, or
// zero for a file. It is what [app.forgetToken] is owed when a chip comes off.
func pictureOrdinal(chips []chip, at int) int {
	if at < 0 || at >= len(chips) || chips[at].file {
		return 0
	}
	seen := 0
	for i := 0; i <= at; i++ {
		if !chips[i].file {
			seen++
		}
	}
	return seen
}

// attach adds one picture to the tray, and reports whether it changed anything.
// The same file twice is one chip: a person who picked a name out of the
// completion twice meant it once, and a message carrying the same photo two
// times pays for it two times.
func (a *app) attach(path string) bool { return a.attachChip(chip{path: path}) }

// attachFile adds one ordinary file to the tray, by the same rules.
func (a *app) attachFile(path string) bool {
	return a.attachChip(chip{path: path, file: true})
}

func (a *app) attachChip(held chip) bool {
	if !attachChipTo(&a.chips, held) {
		return false
	}
	a.touch()
	return true
}

// attachChipTo is that put ONTO A NAMED TRAY, which is what lets the drop door
// serve home's box and the errand pane's without a second idea of what a chip
// is (imagepaste.go's [app.pasteFilesInto]). It draws nothing: the caller knows
// whether anything on the screen changed.
func attachChipTo(chips *[]chip, held chip) bool {
	held.path = strings.TrimSpace(held.path)
	if held.path == "" {
		return false
	}
	for _, already := range *chips {
		if already.path == held.path {
			return false
		}
	}
	*chips = append(*chips, held)
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
	// THE NUMBER THAT MOVES IS A PICTURE'S. A file carries no token in the
	// sentence — the model is told its path and not a `[image #n]` — so taking
	// one off renumbers nothing, and handing the tray's own index to the
	// renumber would count a CSV as a picture and shift every token behind it.
	gone, pictures := pictureOrdinal(a.chips, i), len(pictureChips(a.chips))
	a.chips = append(a.chips[:i], a.chips[i+1:]...)
	if gone > 0 {
		a.forgetToken(gone, pictures)
	}
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

// attachFilePath is the /attach command: one path, put on the tray as a FILE,
// or one note saying why not. Every refusal names the file, for [app.attachPath]'s
// reason — "no such file" about a path the person typed is a sentence they can
// act on and "could not attach" is not.
//
// A PICTURE HANDED TO /attach IS STILL A PICTURE. Somebody who has learned one
// word for putting a thing into a message should not have to learn that this
// build has two, and a PNG sent as a file would be a path the model can read
// bytes out of and never look at. So a picture goes on the tray as a picture,
// which the chip's own glyph then says.
func (a *app) attachFilePath(raw string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		a.note("/attach takes a path · try /attach server.log")
		return
	}
	path := a.resolvePath(raw)
	info, err := os.Stat(path)
	if err != nil {
		a.note("no such file: " + raw)
		return
	}
	// A FOLDER USED TO BE A REFUSAL AND IS NOW THE DOOR. `<name> is a folder ·
	// attach a file` was this surface answering a perfectly clear gesture with a
	// correction: somebody who typed a directory after /attach was pointing at
	// somewhere they wanted the conversation to be about, and the only thing
	// wrong with it was that nothing here could hold that. Now something can, so
	// the path goes to the one seam every road onto a directory comes out of
	// (folderplace.go's [app.referPlace]) and the tray keeps its own job —
	// files, which is what a tray is for.
	//
	// The DROP road still says the old sentence, and deliberately: a folder
	// dropped on the window is a gesture nobody typed, and turning it into a
	// choice about where this conversation is would be inferring a lot from a
	// mouse (dropkeys.go).
	if info.IsDir() {
		a.referPlace(chosenPlace{Path: path, Door: placeFromAttach})
		return
	}
	if isImagePath(path) {
		if !a.attach(path) {
			a.note(filepath.Base(path) + " is already attached")
		}
		return
	}
	// THE CEILING IS ASKED AT THE DOOR AND NOT AT ENTER, where it can still be
	// asked about one file rather than about a message. It is only a ceiling
	// where the bytes have to cross a connection (this file's header): a local
	// session is about to be handed a path to a file it can already open, and a
	// size limit on a file that is not moving is a rule with no reason under it.
	if a.hosted() && info.Size() > maxAttachedFileBytes {
		a.note(oversizeFile(filepath.Base(path), info.Size()))
		return
	}
	if !a.attachFile(path) {
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
	path = windowsPathHere(path, a.wsl)
	if root := a.pathRoot(); !filepath.IsAbs(path) && root != "" {
		path = filepath.Join(root, path)
	}
	return path
}

// ── paths a Windows terminal hands to WSL ──────────────────────────────────

const (
	defaultWSLMountRoot = "/mnt"
	procVersionPath     = "/proc/version"
	wslConfigPath       = "/etc/wsl.conf"
)

// wslPaths is the boot fact needed to translate a Windows path without asking
// the environment, a config file, or another process on the keystroke road.
type wslPaths struct {
	inside bool
	distro string
	root   string
}

// bootWSLPaths is deliberately read once, at the app's construction, and never
// again. A process either is WSL or is not, and its automount root does not
// become a per-file question merely because several conversations are open in
// it.
//
// It was a package-level var over os.Getenv until the surface got one door onto
// the environment ([Options.Env]); it is a function of that door now, so a suite
// that hands [newApp] a table is told whether it is inside WSL by the table and
// not by the developer's shell. The two boot files are still read the way they
// were, once per app, which in a running aforge is once.
func bootWSLPaths(env func(string) string) wslPaths {
	return detectWSLPathsAt(env, procVersionPath, wslConfigPath)
}

// detectWSLPathsAt reads the two boot files once and returns the immutable fact
// every app caches. The paths are parameters so the contract runs unchanged on
// macOS and ordinary Linux rather than borrowing the test machine's identity.
func detectWSLPathsAt(getenv func(string) string, versionPath, configPath string) wslPaths {
	distro := strings.TrimSpace(getenv("WSL_DISTRO_NAME"))
	inside := distro != "" || strings.TrimSpace(getenv("WSL_INTEROP")) != ""
	if !inside {
		if version, err := os.ReadFile(versionPath); err == nil {
			inside = strings.Contains(strings.ToLower(string(version)), "microsoft")
		}
	}
	if !inside {
		return wslPaths{}
	}
	root := defaultWSLMountRoot
	if config, err := os.ReadFile(configPath); err == nil {
		root = wslAutomountRoot(config)
	}
	return wslPaths{inside: true, distro: distro, root: root}
}

// wslAutomountRoot reads only the setting this surface needs. Unknown sections
// and keys stay unknown rather than turning a small boot fact into an INI
// implementation with behavior of its own.
func wslAutomountRoot(config []byte) string {
	root := defaultWSLMountRoot
	section := ""
	for _, line := range strings.Split(string(config), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(line[1 : len(line)-1])
			continue
		}
		if !strings.EqualFold(section, "automount") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || !strings.EqualFold(strings.TrimSpace(key), "root") {
			continue
		}
		if value = strings.TrimSpace(value); value != "" {
			root = value
		}
	}
	if root == "/" {
		return root
	}
	return strings.TrimRight(root, "/")
}

// windowsPathHere is the ONE translation before any attachment stat. Outside
// WSL it changes nothing, so the existing missing-file sentence remains the
// answer instead of pretending a Windows path belongs to an ordinary Linux or
// macOS process.
func windowsPathHere(path string, wsl wslPaths) string {
	if !wsl.inside {
		return path
	}
	if drive, rest, ok := cutWindowsDrive(path); ok {
		root := wsl.root
		if root == "" {
			root = defaultWSLMountRoot
		}
		root = strings.TrimRight(root, "/")
		rest = strings.TrimLeft(strings.ReplaceAll(rest, `\`, "/"), "/")
		return root + "/" + strings.ToLower(string(drive)) + "/" + rest
	}
	distro, rest, ok := cutWSLUNC(path)
	if !ok || (wsl.distro != "" && distro != wsl.distro) {
		return path
	}
	return "/" + strings.TrimLeft(strings.ReplaceAll(rest, `\`, "/"), "/")
}

// cutWindowsDrive recognizes the three drive spellings that can reach WSL. It
// requires a character after the separator, which keeps a lone `c:` or `c:/`
// an unfinished prefix rather than a path-shaped disk question.
func cutWindowsDrive(path string) (byte, string, bool) {
	at := 0
	if strings.HasPrefix(path, "/") {
		at = 1
	}
	if !windowsDriveHead(path) || len(path) < at+4 {
		return 0, "", false
	}
	return path[at], path[at+3:], true
}

func windowsDriveHead(path string) bool {
	at := 0
	if strings.HasPrefix(path, "/") {
		at = 1
	}
	if len(path) < at+3 || !asciiDriveLetter(path[at]) || path[at+1] != ':' {
		return false
	}
	if at == 1 {
		return path[at+2] == '/'
	}
	return windowsSeparator(path[at+2])
}

func asciiDriveLetter(char byte) bool {
	return char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z'
}

func windowsSeparator(char byte) bool { return char == '/' || char == '\\' }

// cutWSLUNC recognizes only WSL's two hosts. Other UNC paths belong to another
// machine and stay untouched, which lets the existing local-file refusal do the
// talking rather than inventing a mount for them.
func cutWSLUNC(path string) (distro, rest string, ok bool) {
	forward := strings.ReplaceAll(path, `\`, "/")
	lower := strings.ToLower(forward)
	for _, prefix := range []string{"//wsl.localhost/", "//wsl$/"} {
		if !strings.HasPrefix(lower, prefix) {
			continue
		}
		remaining := forward[len(prefix):]
		distro, rest, ok = strings.Cut(remaining, "/")
		return distro, rest, ok && distro != "" && rest != ""
	}
	return "", "", false
}

func windowsDriveShape(path string) bool {
	_, _, ok := cutWindowsDrive(path)
	return ok
}

func wslUNCShape(path string) bool {
	_, _, ok := cutWSLUNC(path)
	return ok
}

// ── the row above the box ───────────────────────────────────────────────────

// chipLabels is the tray's text, one label per chip. It is one function so that
// what is DRAWN and what a click is resolved against can never disagree about
// where a chip starts.
func chipLabels(chips []chip, pal palette) []string {
	mark := chipMark(pal)
	out := make([]string, 0, len(chips))
	// The pictures are counted along the row rather than asked for one at a
	// time, because this is rebuilt on every pointer motion that crosses the
	// tray ([app.chipTrayTarget]).
	seen := 0
	for _, c := range chips {
		// A FILE CARRIES NO NUMBER, because there is nothing for a number to
		// refer to: the model is told the file's path, not `[file #2]`, so a
		// digit here would be a reference to something that is not in the
		// sentence. Its name is the whole of what it needs to say.
		if c.file {
			out = append(out, fileChipMark(pal)+" "+c.name())
			continue
		}
		seen++
		// THE NUMBER IS AS MUCH THE POINT OF A PICTURE'S CHIP AS THE NAME IS. It
		// is what `[image #2]` in the sentence refers to and what the model sees
		// second, and a tray that showed only names would leave the person
		// counting from the left to find out which picture they were talking
		// about. It counts PICTURES and not chips, so a file dropped between two
		// screenshots does not move the second one's number ([pictureChips]).
		out = append(out, mark+" #"+strconv.Itoa(seen)+" "+c.name())
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
// AND THE THINKING DIAL RIDES THE SAME ROW, LAST AND RIGHT-ALIGNED
// (effortchip.go). It is the one cell up here that is not cargo — nothing comes
// off the message when it is pressed — so it does not stand in the cargo's
// queue: it keeps the same columns whether the tray is empty or carrying four
// screenshots, which is what lets a hand learn where it is.
func (a *app) chipStrip(width int) string {
	// The dial's columns are recorded where the row is laid out, which is what
	// keeps the press and the paint in step (jumpchip.go's [app.jumpChip] makes
	// the same bargain for the same reason). Cleared first, so a frame that draws
	// no dial cannot be pressed against the last frame that did.
	a.effortSpan = hudSpan{}
	cells := a.harnessTrayCells()
	// AND THE FOLDERS THIS CONVERSATION IS ABOUT RIDE THE SAME ROW, between the
	// harness and the cargo (folderchip.go). They are the same kind of fact —
	// something every next message carries besides its words — and this is the
	// one place on the surface those are kept.
	places := a.placeTrayCells()
	labels := chipLabels(a.chips, a.pal)
	dial := a.effortChipText()
	dialCells := ansi.StringWidth(dial)
	// A CHIP THAT DOES NOT FIT IS DROPPED RATHER THAN TRUNCATED, which is
	// pickrow.go's law about an answer and is the same law here: half a rung word
	// is a word somebody reads as another rung.
	if dial != "" && dialCells+effortTrayGap > width {
		dial, dialCells = "", 0
	}
	if len(cells) == 0 && len(places) == 0 && len(labels) == 0 && dial == "" {
		return ""
	}
	painted := make([]string, 0, len(cells)+len(places)+len(labels))
	for at, cell := range cells {
		if at == 0 {
			// THE POINTER LIGHTS ONE THING ON THIS ROW, never the row. Every cell up
			// here takes something off the message being written and each takes off a
			// different thing, so a band across the tray would offer to drop the
			// picture beside the one a person is aiming at (hover.go's law). The band
			// goes round exactly the cell's own cells, which is what [palette.cursor]
			// does when it is given no width to pad to.
			if a.hoveringChip(trayHarnessChip) {
				painted = append(painted, a.pal.cursor(a.pal.ink(cell), 0))
				continue
			}
			painted = append(painted, a.pal.ink(cell))
			continue
		}
		// The hint after it says what to do next; it is a sentence and comes off
		// nothing, so it does not light.
		painted = append(painted, a.pal.dim(cell))
	}
	for at, cell := range places {
		if a.hoveringChip(trayPlaceChip - at) {
			painted = append(painted, a.pal.cursor(a.pal.dim(cell), 0))
			continue
		}
		painted = append(painted, a.pal.dim(cell))
	}
	for i, label := range labels {
		if a.hoveringChip(i) {
			painted = append(painted, a.pal.cursor(a.pal.dim(label), 0))
			continue
		}
		painted = append(painted, a.pal.dim(label))
	}
	// The cargo is fitted to what is left after the dial, so a long file name
	// ellipsizes rather than pushing the dial off the end of the row.
	room := width
	if dial != "" {
		room = width - dialCells - effortTrayGap
	}
	cargo, cargoCells := fitWidth(strings.Join(painted, chipGap), room)
	if dial == "" {
		return cargo
	}
	a.effortSpan = hudSpan{from: width - dialCells, to: width}
	return cargo + strings.Repeat(" ", width-cargoCells-dialCells) + a.paintEffortChip(dial)
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
// It answers a COMMAND as well as whether it took the press, because one of the
// cells up here now acts on the conversation rather than on the draft: taking a
// folder off is a call that may cross a wire, and it goes back to the loop as a
// command rather than being run under the pointer (folderchip.go).
func (a *app) chipPress(x, y int) (tea.Cmd, bool) {
	at, ok := a.chipTrayTarget(x, y)
	if !ok {
		return nil, false
	}
	if at == trayHarnessChip {
		a.dropHarnessChip()
		return nil, true
	}
	// THE DIAL IS THE ONE CELL UP HERE THAT OPENS SOMETHING rather than taking
	// something off (effortchip.go). It is the self-teaching door beside the
	// chord, which docs/DESIGN-LANGUAGE.md requires of every chord on this
	// surface: a person who never pressed ctrl+v can still find the five rungs,
	// click one, and read the key off the list.
	if at == trayEffortChip {
		a.openEffortMenu()
		return nil, true
	}
	// AND A FOLDER'S CELL TAKES THE FOLDER OFF THE CONVERSATION — not off the
	// message, which is what every other cargo cell up here does. It is the same
	// gesture over a longer-lived object, and the work goes off the loop because
	// over a connection it is a round trip (folderchip.go).
	if at <= trayPlaceChip {
		return a.dropPlaceChip(trayPlaceChip - at)
	}
	a.removeChip(at)
	return nil, true
}

// trayHarnessChip is what [app.chipTrayTarget] answers for the picked harness's
// own cell, which is not one of [app.chips] and has a different thing done to it.
const trayHarnessChip = -1

// trayEffortChip is what [app.chipTrayTarget] answers for the thinking dial at
// the right end of the row (effortchip.go). It is not one of [app.chips] either,
// and what is done to it is the opposite of what is done to them: a press opens
// the ladder rather than taking anything off the message.
const trayEffortChip = -2

// trayPlaceChip is what [app.chipTrayTarget] answers for the FIRST folder this
// conversation is about, and the ones after it count DOWNWARD from here —
// `trayPlaceChip - n` (folderchip.go). They are numbered away from zero rather
// than sharing the chips' own space because what a press does to them is
// different in kind: a picture comes off the message, a folder comes off the
// conversation, and one index space for both would make an off-by-one delete the
// wrong sort of thing.
const trayPlaceChip = -3

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
	places := a.placeTrayCells()
	// THE DIAL KEEPS THIS ROW ALIVE ON A TRAY WITH NO CARGO ON IT
	// (effortchip.go), so the field test asks about it too — and asks the cheap
	// half first, because a session whose model wants no thinking at all draws no
	// dial and should pay nothing for the question.
	// AND A PLACE TAKING THE FRAME IS NOT THIS ROW AT ALL. Home draws a tray of
	// its own over its own box (placebodies.go's [app.placeTray]) and resolves
	// every press against its own two maps (homemouse.go); the geometry below is
	// the CONVERSATION's, so a press answered here while a place is up would be a
	// click on a chip the frame never drew.
	if (len(a.chips) == 0 && len(cells) == 0 && len(places) == 0 && a.effortChipText() == "") ||
		a.at(pageSettings) || a.at(pageHome) || a.pick.open {
		return 0, false
	}
	width, height := a.size()
	rows, _, _, _ := a.chrome(width)
	at := len(rows) - 1 - a.overlayHeight() - a.inputHeight()
	if at < 0 || y != height-len(rows)+at {
		return 0, false
	}
	column := x - len(inputPad)
	// THE DIAL IS ASKED FIRST BECAUSE IT IS THE ONE CELL WHOSE COLUMNS THE
	// LAYOUT RECORDED, and laying the chrome out directly above is what wrote
	// them — the same order [app.jumpPress] and [app.statusPress] keep. The
	// cargo's own offsets are counted from the left and cannot reach this far.
	if a.effortSpan.holds(column) {
		return trayEffortChip, true
	}
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
	// AND THE FOLDERS ARE ASKED FOR NEXT BECAUSE THEY ARE DRAWN NEXT
	// (folderchip.go), with the offset counted off the very cells the row was
	// built from — so what is drawn and what a click resolves against cannot
	// disagree. The counting cell at the end of them answers to nothing: `+2 more
	// folders` is a sentence, and pressing a sentence means nothing on this
	// surface. [app.dropPlaceChip] refuses it by the same bound.
	if len(places) > 0 {
		if at := chipAt(places, column); at >= 0 {
			return trayPlaceChip - at, true
		}
		column -= placeTrayWidth(places)
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
// The marker stays above the rendering because it carries the correspondence:
// the name, its clickable file door, and the `[image #n]` the model was handed.
// The thumbnail under the line is the look; this is how a reader names it.
func chipMarkers(chips []chip, pal palette) string {
	if len(chips) == 0 {
		return ""
	}
	names := make([]string, 0, len(chips))
	seen := 0
	for _, c := range chips {
		// A FILE'S MARKER IS ITS NAME AND NOTHING ELSE, for [chipLabels]' reason
		// — the number would refer to a token the sentence does not carry — and
		// because the name is what a reader is actually looking for when they
		// scroll back to "which log did I send it".
		if c.file {
			names = append(names, "["+c.name()+"]")
			continue
		}
		seen++
		names = append(names, "[#"+strconv.Itoa(seen)+" "+c.name()+"]")
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
	return a.submitImagesShown(text, text)
}

func (a *app) submitImagesShown(text, shown string) tea.Cmd {
	agent, ctx := a.agent, a.ctx
	chips := append([]chip(nil), a.chips...)
	a.chips, a.sent = nil, chips
	pictures, files := pictureChips(chips), fileChips(chips)
	// EVERY PICTURE IS NAMED IN THE WORDS THAT GO WITH IT. A pasted one already
	// carries its `[image #n]` where the person put it; one attached by /image or
	// the @ completion has none, and gets its token appended here so that "image
	// 2" means something whichever door the picture came in by (imagepaste.go).
	// The transcript is drawn from the same string, so what the person reads and
	// what the model reads are one sentence.
	text = imageSentence(text, pictures)
	shown = imageSentence(shown, pictures)

	// AND WHAT THE MODEL IS TOLD ABOUT A FILE IS A PATH, which is a sentence
	// this surface writes only where the file is not going anywhere. On a local
	// session the path already means something to the engine, so the words are
	// composed here; over a connection the bytes travel and the ENGINE composes
	// the same sentence about the paths it wrote them to, because those are the
	// only paths that exist on the machine that owns the journal
	// (internal/remote's file.go, whose [remote.AttachedSentence] both ends call
	// so that a model never meets two phrasings of one fact).
	//
	// The transcript keeps the person's own line either way — the paths go to
	// the model and the NAMES go on the screen ([chipMarkers]), because a
	// scrollback full of absolute paths is a scrollback nobody reads.
	hosted, spoken := a.hosted(), text
	if len(files) > 0 && !hosted {
		spoken = remote.AttachedSentence(text, chipPaths(files))
	}

	if a.stream == nil {
		a.turn++
	}
	// And a turn starting disarms the door here too, for [app.submitting]'s
	// reason: from this moment ctrl+c is the interrupt again (quitarm.go).
	a.disarmQuit()
	a.sel = -1
	// The person's line goes in WITHOUT cutting a reply that is still streaming
	// in two — see [feed.said], which is the whole of this wave's render-order fix
	// and belongs to every door onto the transcript, not just the plain one.
	// And the block carries the context this turn runs in, for [app.submitting]'s
	// reason and by the same law: a door onto the transcript that dropped the mark
	// would be a picture-carrying message the history could not place
	// (turncontext.go).
	a.said(entry{
		kind: entryUser, text: userLine(shown, chips, a.pal), turn: a.turn,
		context: a.turnContext(), pictures: chipPaths(pictures), picturesHere: true,
	})
	// And it is marked until the far end has it, for [app.submittingShown]'s
	// reason and by the same door (echo.go). A message carrying files has a
	// LONGER gap than a plain one — the bytes go up before the turn opens — so
	// this is the road the mark matters most on.
	mark := a.echoPending()
	a.state = stateWorking
	a.lastDelta = time.Now()
	// The turn is open and the first request is out with nothing back from it.
	a.awaited = time.Now()
	a.follow()
	a.touch()
	return tea.Batch(func() tea.Msg {
		images, err := readAttachments(pictures)
		if err != nil {
			return submittedMsg{err: err, echo: mark}
		}
		// A MESSAGE WITH NO FILES AND A MESSAGE WHOSE FILES ARE ALREADY ON THE
		// ENGINE'S OWN DISK ARE THE SAME CALL. Locally nothing is copied and
		// nothing is read — the sentence composed above names the files where
		// they sit, which is what internal/remote's image.go blesses in as many
		// words for a picture that arrives with a path and no bytes: a caller
		// naming a file on the engine's own disk, which the session reads itself.
		if len(files) == 0 || !hosted {
			ch, err := agent.SubmitImage(ctx, spoken, images)
			return submittedMsg{ch: ch, err: err, echo: mark}
		}
		// A CAPABILITY THAT CANNOT WORK IS ABSENT, NOT BROKEN. A door that
		// handed no file seam over is a connection this build cannot put a file
		// through, and the honest thing is to say so with the person's tray
		// still in their hands rather than to send the words without the file.
		taker, ok := agent.(fileSubmitter)
		if !ok {
			return submittedMsg{err: errors.New(attachRemoteWord), echo: mark}
		}
		loaded, err := readFiles(files)
		if err != nil {
			return submittedMsg{err: err, echo: mark}
		}
		ch, err := taker.SubmitFiles(ctx, spoken, loaded, images)
		return submittedMsg{ch: ch, err: err, echo: mark}
	}, a.wake())
}

// fileSubmitter is the optional seam "this session can be handed a file the
// surface read", and it is asserted rather than required for the reason task.go
// asserts its own: a surface must not demand of every agent a method only one
// kind of agent can have. The local agent does not implement it and does not
// need to — the file is already on its disk.
//
// It is internal/remote's [remote.Agent] by shape and nothing else, and the
// argument is that package's [remote.WireFile] because a Go method set is
// matched on the exact type: a slice of a look-alike declared here would be a
// seam nothing satisfies.
type fileSubmitter interface {
	SubmitFiles(ctx context.Context, text string, files []remote.WireFile, images []session.Image) (<-chan session.Event, error)
}

// attachRemoteWord is what a message carrying a file says when the connection
// was opened without a door for one. It belongs with host.go's set — the whole
// of what a connection cannot do, said in one voice — and lives here because it
// is about the tray.
const attachRemoteWord = "this connection cannot carry a file · the words were not sent"

// chipPaths is the tray's paths, in order.
func chipPaths(chips []chip) []string {
	out := make([]string, 0, len(chips))
	for _, c := range chips {
		out = append(out, c.path)
	}
	return out
}

// chipsSettled is what a submit's answer does to the tray, and it is called for
// EVERY submit — a plain one holds no chips and this is a no-op for it.
//
// A refusal puts the pictures back, in front of anything attached while the
// message was in flight and without duplicating it. A success drops them: they
// are in the conversation now.
//
// AND THEY GO BACK ON THE CONVERSATION'S TRAY, WHEREVER THE PERSON IS STANDING
// (recipient.go). A refusal can take a second to arrive — over a connection it
// is the whole upload — and a task's page opened in that second owns the box:
// handing the pictures to whatever tray is on screen would attach the
// conversation's screenshots to a message being written to a worker, which is
// this wave's own defect said about the tray instead of the words.
func (a *app) chipsSettled(err error) {
	if len(a.sent) == 0 {
		return
	}
	sent := a.sent
	a.sent = nil
	if err == nil {
		return
	}
	a.atMainComposer(func(state *composerState) {
		restored := append([]chip(nil), sent...)
		for _, held := range state.chips {
			if !heldBy(restored, held.path) {
				restored = append(restored, held)
			}
		}
		state.chips = restored
	})
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

// readFiles turns the file half of the tray into what travels, and it is
// [readAttachments] for everything that is not a picture: stat first, refuse
// what is too big, name the FILE in the refusal, and read the bytes at the
// moment enter was pressed rather than leaving the far end to open something
// that may have changed under it.
//
// IT IS ONLY EVER CALLED WHERE THE BYTES CROSS A CONNECTION. A local session
// gets the path and no bytes at all (this file's header), so nothing here is
// paid for by somebody whose file is not going anywhere.
//
// THE NAME IS A NAME AND NOT A PATH. The engine joins it to a directory of the
// engine's own choosing, so a path in this field would be this surface asking a
// remote machine to write wherever it liked — internal/remote's attachmentName
// refuses one, and it is this side's job not to send one (wire.go's
// [remote.WireFile] documents the field as a name for exactly that reason).
func readFiles(chips []chip) ([]remote.WireFile, error) {
	out := make([]remote.WireFile, 0, len(chips))
	total := int64(0)
	for _, c := range chips {
		info, err := os.Stat(c.path)
		if err != nil || info.IsDir() {
			return nil, fmt.Errorf("could not read %s", c.name())
		}
		if info.Size() > maxAttachedFileBytes {
			return nil, errors.New(oversizeFile(c.name(), info.Size()))
		}
		data, err := os.ReadFile(c.path)
		if err != nil {
			return nil, fmt.Errorf("could not read %s", c.name())
		}
		// Checked again, for [readAttachments]' reason: a file can grow between
		// the stat and the read, and the ceiling that matters is the one on what
		// is actually about to be put on the wire.
		if int64(len(data)) > maxAttachedFileBytes {
			return nil, errors.New(oversizeFile(c.name(), int64(len(data))))
		}
		total += int64(len(data))
		if total > maxAttachedTotalBytes {
			return nil, fmt.Errorf("the files on this message are over the %dMB limit", maxAttachedTotalBytes>>20)
		}
		out = append(out, remote.WireFile{Name: c.name(), MIME: fileMIME(c.path), Bytes: data})
	}
	return out, nil
}

// oversizeFile is the ceiling's sentence, and it is one function because the
// door says it about a file being attached and the send says it about a file
// that grew afterwards — one limit said one way (ONE SOURCE OF TRUTH).
//
// It is shaped exactly like the picture's, `%s is over the %dMB image limit`,
// because they are the same refusal about two kinds of thing and a person who
// has read one should recognize the other.
// THE SIZE IS ROUNDED UP, so the sentence can never read "is 16MB and over the
// 16MB limit" — which is what truncation says about a file one byte past the
// ceiling, and is a sentence that reads like a bug rather than like a limit.
func oversizeFile(name string, size int64) string {
	const megabyte = 1 << 20
	return fmt.Sprintf("%s is %dMB and over the %dMB file limit",
		name, (size+megabyte-1)/megabyte, maxAttachedFileBytes>>20)
}

// fileMIME is what this surface believed the file was, or "" when it cannot
// say. It is a HINT and nothing is refused for lacking it — wire.go's
// [remote.WireFile] says so — which is the whole difference from a picture,
// whose type the provider genuinely needs.
func fileMIME(path string) string {
	kind := mime.TypeByExtension(filepath.Ext(path))
	if cut, _, found := strings.Cut(kind, ";"); found {
		return strings.TrimSpace(cut)
	}
	return kind
}
