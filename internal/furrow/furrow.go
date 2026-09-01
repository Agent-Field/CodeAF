// Package furrow is aforge's seam onto `furrow`, a separate program aforge
// carries inside itself (Agent-Field, Apache-2.0,
// https://github.com/Agent-Field/furrow).
//
// furrow copy-on-write forks a whole workspace — files, dependencies, `.env`,
// the dev database, git's own mutable state — into byte-exact universes in
// about a second, seals that workspace continuously into an immutable
// content-addressed timeline, and syncs it between machines encrypted below the
// transport. It is Rust and aforge is Go, so THE ONLY INTEGRATION IS THE CLI —
// which is fine, because the CLI is furrow's declared API: `--json` everywhere,
// stable IDs, and destructive operations gated on an explicit ID plus `--yes`.
//
// EVERY AFORGE IS AN AFORGE WITH FURROW. The binary rides inside this one and
// is written out on first need (internal/furrowbin), so the half of the answer
// that used to vary by machine — is furrow installed — no longer does. What
// still varies is the half a person controls per project: a folder nobody ran
// `furrow watch` in is a folder furrow will not act on.
//
// THE LAW THIS WHOLE PACKAGE IS WRITTEN TO IS THE CODEBASE'S OWN: A CAPABILITY
// THAT CANNOT WORK IS ABSENT, NOT BROKEN. A folder furrow was never pointed at
// — or the rare machine where the binary could not be written out at all — and
// [Tools] returns nothing at all: the model is never handed a verb whose every
// call would be a refusal, and no other part of aforge notices this package
// exists. Everything here hangs off [Detect], which is why [Detect] is the
// first thing in the file and the most carefully cached: it is asked far more
// often than anything else is done.
//
// It is also written to be wrong about furrow safely. furrow's JSON is furrow's
// to change, and a version this package has never seen must degrade to "I could
// not read that" rather than to a panic or, worse, to a confident misreading of
// a restore. So every decode takes the few fields it needs and ignores the
// rest, every optional field is treated as optional, and no exit code is
// trusted over a document that parsed (see [run]).
package furrow

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/furrowbin"
)

// Binary is the program this package shells out to. It is normally the copy
// aforge carries and writes out itself; PATH is the fall-back, and
// AFORGE_FURROW overrides both for somebody who means a particular binary —
// their own build, or a newer furrow than this aforge is pinned to.
// [lookBinary] has the order and the reason for it.
const (
	Binary       = "furrow"
	BinaryEnvVar = "AFORGE_FURROW"
)

// detectTimeout bounds the two cheap calls [Detect] makes. Detection sits in
// front of belt construction, and a furrow whose store is on a wedged network
// mount must cost a session a bounded pause rather than a hang; a detection
// that times out reads as "not attached", which is the answer that leaves the
// belt honest.
const detectTimeout = 5 * time.Second

// readTimeout bounds the read-only questions — the timeline, the fork list, a
// restore preview. They touch the store and nothing else, so they are quick,
// but they are quick on furrow's terms and not on ours.
const readTimeout = 30 * time.Second

// detectTTL is how long an answer about one workspace is reused. Attachment
// changes exactly when somebody runs `furrow watch` or `furrow forget` in
// another terminal, which is rare and never urgent, and the belt is built once
// per session anyway — so this is not a freshness dial, it is a promise that a
// burst of tool calls costs one exec rather than one each.
const detectTTL = 30 * time.Second

// Presence is the whole of what this package knows about furrow and one folder,
// and it is deliberately more than a boolean: a caller offering the person
// something wants to say WHICH of the two halves is missing, because "install
// furrow" and "run furrow watch here" are completely different sentences.
type Presence struct {
	// Installed reports that a furrow binary was found, and Path is where. On
	// an ordinary build this is true everywhere — the binary is carried — and
	// the field is kept because the two halves are still separately
	// interesting: a state root that could not be written to is a machine
	// where it is false, and that is a fault worth being able to name.
	Installed bool
	Path      string

	// Version is what `furrow --version` printed, trimmed — "furrow 0.1.0".
	// It is carried for a person to read and for a bug report to quote, and
	// nothing in this package branches on it. Version-sniffing a tool whose
	// contract is its JSON would be a second contract to keep in step with the
	// first, and the decoders here are already written to survive a shape they
	// do not recognise.
	Version string

	// Attached reports that THIS folder is one furrow is watching, which is the
	// half a person controls per project rather than per machine.
	Attached bool

	// Head is the newest sealed snapshot's id, and Watching reports whether the
	// background watcher is running. A workspace can be attached with its
	// watcher stopped — the timeline is then frozen at Head until somebody runs
	// `furrow watch` again — and a caller that offers a restore point without
	// saying so is offering a stale one.
	Head     string
	Watching bool

	// Reason is furrow's own first line when Installed is true and Attached is
	// false. It is furrow's wording and not ours on purpose: the person is
	// going to type furrow's commands to fix it, so they should be reading
	// furrow's account of the problem.
	Reason string
}

// Available is the one condition every caller in this codebase should branch
// on. Both halves have to be true for a single furrow verb to work, so they are
// asked as one question rather than left to each caller to remember to and.
func (p Presence) Available() bool { return p.Installed && p.Attached }

// presenceCache holds one answer per workspace root. It is package-level rather
// than per-Workspace because the expensive half of the answer — whether the
// binary exists at all — is a fact about the machine that every session in the
// process shares.
var (
	presenceMu    sync.Mutex
	presenceCache = map[string]cachedPresence{}
)

type cachedPresence struct {
	presence Presence
	asked    time.Time
}

// Forget drops the cached answer for every workspace. It exists for tests,
// which change what is on PATH between cases and would otherwise read a
// neighbour's answer, and for a caller that has just watched the person attach
// a folder and wants the next question answered by furrow rather than by a
// thirty-second-old memory.
func Forget() {
	presenceMu.Lock()
	defer presenceMu.Unlock()
	clear(presenceCache)
}

// Detect answers "is furrow here, and is this folder attached to it?" — the
// question everything else in this package hangs off.
//
// It is at most two short execs: `furrow --version`, which touches nothing, and
// `furrow --repo <root> --json status`, which opens the store. Both are cached
// together for [detectTTL] against the absolute root, because the two halves
// are useless apart and a caller asking one has always just asked the other.
//
// It never returns an error. Every way this can fail — no binary, a binary that
// will not run, a folder furrow was never pointed at, a store that will not
// open — is the same answer to the caller: the seam is not available here, and
// what it does about that does not depend on which. The distinctions a person
// CAN act on are carried in the fields instead.
func Detect(ctx context.Context, root string) Presence {
	root = absoluteRoot(root)

	presenceMu.Lock()
	if cached, ok := presenceCache[root]; ok && time.Since(cached.asked) < detectTTL {
		presenceMu.Unlock()
		return cached.presence
	}
	presenceMu.Unlock()

	presence := detect(ctx, root)

	presenceMu.Lock()
	presenceCache[root] = cachedPresence{presence: presence, asked: time.Now()}
	presenceMu.Unlock()
	return presence
}

func detect(ctx context.Context, root string) Presence {
	binary, err := lookBinary()
	if err != nil {
		return Presence{}
	}
	presence := Presence{Installed: true, Path: binary}

	versionCtx, cancelVersion := context.WithTimeout(ctx, detectTimeout)
	defer cancelVersion()
	if out, _, err := runBinary(versionCtx, binary, "", "--version"); err == nil {
		presence.Version = strings.TrimSpace(firstLine(string(out)))
	}

	statusCtx, cancelStatus := context.WithTimeout(ctx, detectTimeout)
	defer cancelStatus()
	out, stderr, err := runBinary(statusCtx, binary, root, "--json", "status")
	if err != nil {
		// A non-zero status here is the ordinary "this repository is not
		// watched; run `furrow watch` first". It is not a fault to report — it
		// is the answer — so furrow's sentence is carried and nothing is
		// logged.
		presence.Reason = firstLine(stderr)
		return presence
	}
	var status struct {
		Head           *string `json:"head"`
		WatcherRunning bool    `json:"watcher_running"`
	}
	if err := decodeLast(out, &status); err != nil {
		// furrow exited happily and said something this package cannot read.
		// Attached stays false: an unreadable status is a furrow whose other
		// answers this package cannot trust either, and a belt built on that
		// would be a belt of tools that fail in a way nobody can explain.
		presence.Reason = "furrow answered in a shape aforge does not understand"
		return presence
	}
	presence.Attached = true
	presence.Watching = status.WatcherRunning
	if status.Head != nil {
		presence.Head = *status.Head
	}
	return presence
}

// embedded is the road to the furrow aforge carries inside itself, and it is a
// variable for exactly one reason: the tests in this package have to be able to
// say "a machine with no furrow at all", which on a shipped build is a state
// that no longer exists. Nothing in the product reassigns it.
var embedded = furrowbin.Ensure

// lookBinary finds furrow. Three roads, in this order: the path somebody
// configured, the copy aforge carries, then PATH.
//
// AFORGE_FURROW stays first because it is the only one a person chose. An
// AFORGE_FURROW that names something missing is an error and not a quiet fall
// back to the others: somebody who set that variable meant that binary, and
// silently running a different one is the kind of help nobody asked for.
//
// THE EMBEDDED COPY COMES BEFORE PATH, AND THAT IS THE NO-VARIANCE RULING. The
// version riding inside this binary is the one this aforge was built and tested
// against; whatever a machine happens to have on its PATH is a different
// program with the same name, possibly older, possibly newer than the JSON the
// decoders here were written for. PATH remains as the fall-back for a build
// that carries nothing — a plain `go build ./...`, or an extraction that could
// not write — which is also why a failure here is never fatal: it is one more
// road not taken, and if none of them answer the seam is absent exactly as it
// always was.
func lookBinary() (string, error) {
	if configured := strings.TrimSpace(os.Getenv(BinaryEnvVar)); configured != "" {
		info, err := os.Stat(configured)
		if err != nil {
			return "", err
		}
		if info.IsDir() {
			return "", fmt.Errorf("%s names a directory: %s", BinaryEnvVar, configured)
		}
		return configured, nil
	}
	if carried, err := embedded(); err == nil {
		return carried, nil
	}
	return exec.LookPath(Binary)
}

// Workspace is one attached folder, and the receiver every operation in this
// package hangs off. Holding it is a claim that furrow was here and this folder
// was attached at the moment [Open] asked, which is the strongest claim
// anything can make about another process.
type Workspace struct {
	root   string
	binary string
}

// Root is the absolute path of the workspace, canonical as [Open] resolved it.
func (w *Workspace) Root() string { return w.root }

// Open is the seam: the workspace when furrow can act on this folder, and nil
// when it cannot. THE NIL IS THE POINT — a caller writes `if ws := furrow.Open(…);
// ws != nil` and every capability behind it is absent rather than present and
// refusing, which is this codebase's law about tools stated in a return type.
func Open(ctx context.Context, root string) *Workspace {
	presence := Detect(ctx, root)
	if !presence.Available() {
		return nil
	}
	return &Workspace{root: absoluteRoot(root), binary: presence.Path}
}

// attachTimeout bounds the one call that can be slow. Attaching a workspace
// reads every file in it once to seal the first snapshot, so it is the only
// thing in this package whose cost is the person's project rather than
// furrow's; a minute is generous for the repositories aforge works in and short
// enough that a task waiting on it is never waiting on a hang.
const attachTimeout = 60 * time.Second

// Attach is [Open] for a caller that is willing to ATTACH THE FOLDER ITSELF.
//
// THE RULING BEHIND IT: every aforge carries furrow, so a capability that only
// engages when somebody remembered to type `furrow watch` is a capability the
// binary has and never uses — which is this codebase's absent-not-broken law
// running in the bad direction. A folder aforge is about to write in is a folder
// aforge may attach, on the same consent as the write; nothing here reaches a
// folder that was not already going to be worked in.
//
// It attaches WITHOUT LEAVING A WATCHER RUNNING (`--no-daemon`). What the
// caller needs is the ability to fork the live workspace, which does not depend
// on a background sealer, and a program that quietly started a daemon in
// somebody's project would be doing more than the write it was consenting to.
//
// Every failure answers nil, exactly as [Open] does, and the caller falls to
// whatever it would have done on a machine without furrow.
func Attach(ctx context.Context, root string) *Workspace {
	if workspace := Open(ctx, root); workspace != nil {
		return workspace
	}
	binary, err := lookBinary()
	if err != nil {
		return nil
	}
	attachCtx, cancel := context.WithTimeout(ctx, attachTimeout)
	defer cancel()
	if _, _, err := runBinary(attachCtx, binary, absoluteRoot(root), "--json", "watch", "--no-daemon"); err != nil {
		return nil
	}
	// The cached answer was taken before the attach and now says the opposite of
	// what is true. Dropping it is the whole reason this cannot simply call
	// Detect again.
	Forget()
	return Open(ctx, root)
}

// ── the process seam ─────────────────────────────────────────────────────────

// ErrUnreadable says furrow answered in a shape this package could not decode.
// It is a sentinel because it is the one failure a caller may want to word
// differently from every other: the rest mean furrow said no, this one means
// aforge and furrow disagree about what furrow says, and only the second is
// worth quoting a version at somebody over.
var ErrUnreadable = errors.New("furrow: unreadable answer")

// outputCap is how much of a captured stream survives into a tool result,
// counted in bytes and cut on a rune boundary. It is one number because both
// the ends it serves — a person reading a merge check's failure, a model
// reading a fork's build log — want the same thing: enough to see what broke,
// not a megabyte of test output spent as context.
const outputCap = 4000

// run is every call this package makes into furrow, and it holds the one rule
// that makes reading furrow safe:
//
// A DOCUMENT THAT PARSED OUTWEIGHS A NON-ZERO EXIT. furrow deliberately exits
// with the status of what it ran — a merge that found conflicts, an `exec`
// whose command failed its own tests — after printing a complete, truthful JSON
// account of what happened. Treating that exit as a failure would throw away
// the answer and report nothing where furrow reported everything, so the exit
// status is consulted only when stdout carried nothing this package could read.
//
// stderr is returned alongside because for two commands it is the payload
// rather than the diagnosis: under `--json`, `furrow exec` redirects the
// command's own stdout to stderr so the JSON on stdout stays clean.
func (w *Workspace) run(ctx context.Context, args ...string) (stdout []byte, stderr string, err error) {
	return runBinary(ctx, w.binary, w.root, args...)
}

func runBinary(ctx context.Context, binary, root string, args ...string) ([]byte, string, error) {
	full := args
	if root != "" {
		full = append([]string{"--repo", root}, args...)
	}
	command := exec.CommandContext(ctx, binary, full...)
	var out, errOut bytes.Buffer
	command.Stdout = &out
	command.Stderr = &errOut
	// furrow's own law is that a prompt in machine mode is an error and not a
	// hang, and its rewind honours that by refusing without --yes when stdin is
	// not a terminal. Handing it a closed stdin is aforge agreeing to that
	// contract from its side rather than relying on how a session happened to
	// be launched.
	command.Stdin = nil
	err := command.Run()
	return out.Bytes(), errOut.String(), err
}

// decodeLast reads every JSON value on a stream and keeps the last one.
//
// It is a stream and not one document because furrow really does print two: an
// applied `rewind` prints the PLAN it is about to carry out and then the
// account of what it did, one pretty-printed object after the other. The last
// is the outcome in every command that does this, and a decoder that demanded a
// single document would fail on exactly the call whose answer matters most.
//
// A trailing fragment is not an error either. Everything decoded before it
// stands, on the same reasoning: a furrow that grew a new line at the end of
// its output must not cost this package the answer it already read.
func decodeLast(stdout []byte, into any) error {
	last, err := lastDocument(stdout)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(last, into); err != nil {
		return fmt.Errorf("%w: %s", ErrUnreadable, err)
	}
	return nil
}

// documents returns every complete JSON value on a stream, in order.
func documents(stdout []byte) []json.RawMessage {
	decoder := json.NewDecoder(bytes.NewReader(stdout))
	var found []json.RawMessage
	for {
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			// io.EOF is the ordinary end; anything else is a fragment after the
			// documents that did parse, and those are kept.
			if !errors.Is(err, io.EOF) && len(found) == 0 {
				return nil
			}
			return found
		}
		found = append(found, value)
	}
}

func lastDocument(stdout []byte) (json.RawMessage, error) {
	found := documents(stdout)
	if len(found) == 0 {
		return nil, fmt.Errorf("%w: nothing furrow printed was JSON", ErrUnreadable)
	}
	return found[len(found)-1], nil
}

// failure turns a furrow that said no into one sentence in the person's words,
// carrying furrow's own first line because that is the line they will search
// for and the line they can act on.
func failure(stderr string, err error) error {
	line := firstLine(stderr)
	if line == "" {
		return fmt.Errorf("furrow could not do that: %w", err)
	}
	return fmt.Errorf("furrow could not do that: %s", strings.TrimPrefix(line, "Error: "))
}

func firstLine(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if index := strings.IndexByte(text, '\n'); index >= 0 {
		text = text[:index]
	}
	return strings.TrimSpace(text)
}

// cap trims a captured stream to [outputCap] bytes on a rune boundary and says
// how much was left behind. Saying so is the emptiness law's neighbour: a
// truncation nobody is told about is a result that reads as complete and is
// not.
func capped(text string) string {
	text = strings.TrimRight(text, "\n")
	if len(text) <= outputCap {
		return text
	}
	cut := outputCap
	for cut > 0 && !isRuneStart(text[cut]) {
		cut--
	}
	return text[:cut] + fmt.Sprintf("\n… %d more bytes not shown", len(text)-cut)
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }

func absoluteRoot(root string) string {
	if root == "" {
		root = "."
	}
	if absolute, err := filepath.Abs(root); err == nil {
		return absolute
	}
	return root
}
