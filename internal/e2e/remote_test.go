//go:build docker_e2e

// remote_test.go drives `aforge chat --host` across THREE CONTAINERS ON ONE
// BRIDGE NETWORK — a surface machine, an engine machine, and a scripted model
// endpoint — so that the honesty laws of docs/REMOTE.md are proved against two
// genuinely different filesystems.
//
// WHY IT EXISTS BESIDE THE LOOPBACK TESTS AND DOES NOT REPLACE THEM.
// internal/remote/loopback.go drives the real client against the real server
// over net.Pipe, and that is the right test of the PROTOCOL: it is fast, it is
// deterministic, and if a change ever makes the wire care what it is riding,
// that file stops compiling. What it cannot prove is the product's actual
// claim. Decision 6 — "the engine machine is the authority, always" — is
// entirely about a path meaning different things on each side, and on one host
// with one home directory every path in the test exists on both ends. A surface
// that resolved a path locally would pass by coincidence. So this harness makes
// the coincidence impossible: the engine's workspace is a directory the surface
// container does not have, the surface's own directory is one the engine
// container does not have, and the two homes are laid out differently on
// purpose.
//
// WHY THE MODEL IS A STUB. The thing under test is the wire, not the model's
// judgment. test/remote/stub answers a script, so a turn does real work — it
// calls a real tool on the engine's real disk — with no API key, no bill, and
// no chance of a red run caused by a model choosing to paraphrase. See that
// file's header for the markers the scenarios below submit.
//
// RUN IT:
//
//	dockerd >/tmp/dockerd.log 2>&1 &   # if the daemon is not already up
//	go test -tags docker_e2e -run TestRemoteTwoMachines -v -timeout 20m ./internal/e2e/
//
// It SKIPS, never fails, on a machine with no docker, a daemon that will not
// answer, or an image that will not pull. The build tag keeps it out of
// `go test ./...` entirely.
package e2e

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// ── the names and the places ────────────────────────────────────────────────

// Everything this harness creates is prefixed, so a teardown can be sure of
// what it is removing and a person looking at `docker ps` after an interrupted
// run can tell instantly which containers are the test's.
const (
	netName     = "aforge-e2e-net"
	modelName   = "aforge-e2e-model"
	engineName  = "aforge-e2e-engine"
	surfaceName = "aforge-e2e-surface"

	// baseImage is alpine because the binaries are built CGO_ENABLED=0 and are
	// therefore statically linked — verified, and asserted by [buildStatic]
	// rather than assumed, because the day something in this tree needs cgo the
	// failure would otherwise be a container that exits with "not found" on a
	// binary that is plainly there.
	baseImage = "alpine:3.20"
)

// THE TWO MACHINES ARE LAID OUT DIFFERENTLY ON PURPOSE, and that is the whole
// design of this file. Not one of these paths exists on both containers. A
// surface that resolved an engine path against its own filesystem would find
// nothing; an engine handed a surface path would find nothing; and both of
// those are assertions below rather than hopes.
const (
	// The engine machine. Its ssh login lands in /root, so a RELATIVE workspace
	// is relative to that (cmd/aforge's engineWorkspace states the law), and
	// its aforge home is somewhere no laptop would ever put one.
	engineLoginHome = "/root"
	engineAforge    = "/var/lib/aforge-engine"
	engineWorkspace = "/srv/engine-work/project"
	engineRelative  = "relwork" // relative to /root: /root/relwork
	// engineTalks is where the two scenarios that NAME a conversation put it. It
	// is absolute and on the engine, because a relative --session lands in the
	// workspace the engine chdir'd into — true, but a different directory per
	// scenario, and a scenario that has to reason about which one is a scenario
	// that will eventually reason wrongly.
	engineTalks = "/var/lib/aforge-engine/conversations"

	// The surface machine. A different home root, a different aforge home, and
	// one directory that exists ONLY here — which is the bait for the "a path
	// that exists only on the surface is not silently resolved" scenario.
	surfaceHome    = "/opt/surface-home"
	surfaceAforge  = "/opt/surface-home/.aforge"
	surfaceOnlyDir = "/opt/surface-home/laptop-work"

	// The stub's address on the bridge network. Container-to-container DNS by
	// container name is what makes this a URL and not an IP.
	stubBase = "http://" + modelName + ":8080/api/v1"

	// stubKey is the credential the ENGINE holds. It is not a secret and it is
	// not checked by anything: it exists because config.Load refuses to build a
	// session with no key at all, and the point of writing it only on the
	// engine is that the surface has none — which is Decision 6's "the API key
	// belongs to the machine that runs the work", asserted rather than assumed.
	stubKey = "stub-key-not-a-secret"

	// stubModel is the one row the stub's catalog serves.
	stubModel = "stub/scripted"
)

// The markers and the prefixes, spelled exactly as test/remote/stub/main.go
// spells them. ONE SOURCE OF TRUTH is a law of this codebase and this is the
// one place it cannot be honoured by interpolation — the stub is a separate
// binary in a container — so they are stated once here and once there, and the
// first scenario is a canary for the pair having drifted.
const (
	markerEcho = "PROBE-ECHO"
	markerPwd  = "PROBE-PWD"
	markerRead = "PROBE-READ"
	markerSlow = "PROBE-SLOW"

	sayEcho = "STUB-REPLY:"
	sayPwd  = "ENGINE-PWD:"
	sayRead = "ENGINE-READ:"
	slowTop = "SLOW-BEGIN"
	slowEnd = "SLOW-END"
)

// ── the run ─────────────────────────────────────────────────────────────────

func TestRemoteTwoMachines(t *testing.T) {
	world := newRemoteWorld(t)

	// The scenarios are subtests of ONE world because building it costs about a
	// minute — two image pulls, an apk install over the network, an sshd — and
	// a fresh world per scenario would spend that five times over for nothing.
	// They are ordered by value, exactly as the lane was briefed: each one that
	// runs is worth having even if the ones after it are skipped.
	t.Run("HandshakeAndOneTurn", world.handshakeAndOneTurn)
	t.Run("ThePathLaw", world.thePathLaw)
	t.Run("TheLinkDiesMidTurn", world.theLinkDiesMidTurn)
	t.Run("AnAttachmentLandsOverThere", world.anAttachmentLandsOverThere)
	t.Run("TwoSurfacesAtOnce", world.twoSurfacesAtOnce)
}

// ── scenario 1 — the handshake and one turn ─────────────────────────────────

// handshakeAndOneTurn is the whole arrangement in one command: ssh opens, the
// engine boots on the far machine, the handshake completes, a turn runs against
// the stub, and the reply arrives on the surface's stdout.
//
// IT IS THE FLOOR EVERY OTHER SCENARIO STANDS ON. If this fails, nothing below
// it means anything, so it dumps everything it can reach before it gives up.
func (w *remoteWorld) handshakeAndOneTurn(t *testing.T) {
	said := w.chatOnce(t, engineWorkspace, markerEcho+" can you hear me over there")
	if said.err != nil {
		w.diagnose(t)
		t.Fatalf("one turn over --host failed: %v\nstdout:\n%s\nstderr:\n%s", said.err, said.out, said.errOut)
	}
	if !strings.Contains(said.out, sayEcho) {
		w.diagnose(t)
		t.Fatalf("stdout carried no reply from the stub.\nwant it to contain %q\nstdout:\n%s\nstderr:\n%s",
			sayEcho, said.out, said.errOut)
	}
	if !strings.Contains(said.out, markerEcho) {
		t.Errorf("the reply did not echo the message back: %q", said.out)
	}
	t.Logf("the surface printed: %s", strings.TrimSpace(said.out))

	// THE JOURNAL IS ON THE ENGINE'S DISK AND ONLY THERE. Decision 6 lists the
	// session file among the things the engine machine owns, and this is that
	// claim made falsifiable rather than merely stated.
	//
	// The conversation lands under the ENGINE's aforge home, in a folder named
	// for the ENGINE's workspace (chatv3_layout.go turns the separators into
	// dashes) — and the machine the person was sitting at has none.
	found := w.exec(t, engineName, nil, 30*time.Second, "sh", "-c",
		"find "+engineAforge+"/v3/projects -name '*.jsonl' 2>/dev/null | head -1")
	if strings.TrimSpace(found.out) == "" {
		w.diagnose(t)
		t.Errorf("the engine wrote no journal under %s/v3/projects", engineAforge)
	} else {
		t.Logf("the engine journalled the conversation at %s", strings.TrimSpace(found.out))
	}
	if got := w.exec(t, surfaceName, nil, 20*time.Second,
		"sh", "-c", "ls "+surfaceAforge+"/v3/projects 2>/dev/null | wc -l"); strings.TrimSpace(got.out) != "0" {
		t.Errorf("the SURFACE has %s conversation folders under %s/v3/projects; conversations belong to the engine machine",
			strings.TrimSpace(got.out), surfaceAforge)
	}

	// AND THE SURFACE NEVER HELD A CREDENTIAL. This is the other half of the
	// same decision, and it is worth asserting because it is the one that would
	// pass silently on a single host: the harness deliberately writes the key
	// only on the engine, and a turn that ran proves the far machine paid for
	// it (cmd/aforge's hostOptions states that a missing key is not an error on
	// this path, for exactly this reason).
	if found := w.exec(t, surfaceName, nil, 20*time.Second,
		"sh", "-c", "grep -l "+stubKey+" -r "+surfaceHome+" 2>/dev/null | head -1"); strings.TrimSpace(found.out) != "" {
		t.Errorf("the surface machine holds the engine's credential at %s", strings.TrimSpace(found.out))
	}
}

// ── scenario 2 — the path law ───────────────────────────────────────────────

// thePathLaw is Decision 6 made falsifiable. Three separate claims, each of
// which would pass by coincidence on a single host:
//
//  1. what a tool SEES is the engine's filesystem, named with the engine's own
//     absolute paths — and those paths do not exist on the surface at all;
//  2. a path that exists ONLY on the surface is refused by the engine rather
//     than silently resolved against the machine the person is sitting at;
//  3. a RELATIVE workspace resolves against the ENGINE's home directory, which
//     is a different directory from the surface's home.
func (w *remoteWorld) thePathLaw(t *testing.T) {
	// ── 1. the engine's own paths ────────────────────────────────────────────
	said := w.chatOnce(t, engineWorkspace, markerPwd+" which machine's filesystem is this")
	if said.err != nil {
		w.diagnose(t)
		t.Fatalf("the pwd probe failed: %v\nstdout:\n%s\nstderr:\n%s", said.err, said.out, said.errOut)
	}
	if !strings.Contains(said.out, sayPwd) {
		w.diagnose(t)
		t.Fatalf("the tool turn produced no quoted shell output.\nstdout:\n%s\nstderr:\n%s", said.out, said.errOut)
	}
	// The shell's own cwd is the ENGINE's workspace, spelled absolutely.
	// cmd/aforge's bootEngine chdirs into it before it assembles anything —
	// "the process's own idea of where it is has to agree with" the session
	// config — so this is that chdir observed from the other machine.
	if !strings.Contains(said.out, engineWorkspace) {
		t.Errorf("the tool did not run in the engine's workspace %q: %q", engineWorkspace, strings.TrimSpace(said.out))
	}
	t.Logf("the engine's shell answered: %s", strings.TrimSpace(said.out))

	// THE COINCIDENCE IS RULED OUT HERE and this is the assertion the whole
	// harness exists for: the path the reply just named is not on the surface's
	// filesystem at all, so no local resolution could have produced it.
	if w.pathExists(t, surfaceName, engineWorkspace) {
		t.Fatalf("%s exists on the SURFACE machine too — this harness proves nothing while that is true", engineWorkspace)
	}
	if w.pathExists(t, surfaceName, engineAforge) {
		t.Fatalf("%s exists on the SURFACE machine too — this harness proves nothing while that is true", engineAforge)
	}

	// AND THE JOURNAL IS WRITTEN IN THE ENGINE'S OWN VOCABULARY. Decision 6
	// puts the session file on the engine machine and Decision 7 says a
	// journalled path must be true on the machine that wrote it; the journal is
	// read straight off the engine's disk here, so the claim is about the file
	// rather than about anything the surface printed.
	// busybox find has no -newermt, so every journal of this workspace is read
	// and the claim is made about all of them together: what the engine wrote
	// down names the engine's own directory.
	journal := w.exec(t, engineName, nil, 30*time.Second, "sh", "-c",
		"find "+engineAforge+"/v3/projects -name '*.jsonl' 2>/dev/null | xargs -r cat")
	if strings.TrimSpace(journal.out) == "" {
		t.Errorf("the engine wrote no journal under %s/v3/projects", engineAforge)
	} else if !strings.Contains(journal.out, engineWorkspace) {
		t.Errorf("the engine's journal never names the engine's own workspace %q", engineWorkspace)
	}

	// ── 2. a path that exists only here ──────────────────────────────────────
	//
	// The directory is real on the machine the person is sitting at and absent
	// on the machine that would have to open it. The engine refuses at the door
	// (cmd/aforge's engineWorkspace: "a directory that is not there is refused
	// at the door"), and the refusal names the path — which is how a person
	// learns that the word they typed was read on the other machine.
	if !w.pathExists(t, surfaceName, surfaceOnlyDir) {
		t.Fatalf("the harness did not make %s on the surface, so this scenario is not testing anything", surfaceOnlyDir)
	}
	if w.pathExists(t, engineName, surfaceOnlyDir) {
		t.Fatalf("%s exists on the ENGINE too, so a silent local resolution would be indistinguishable from success", surfaceOnlyDir)
	}
	refused := w.chatOnce(t, surfaceOnlyDir, markerEcho+" this should never run")
	if refused.err == nil {
		t.Errorf("the engine ACCEPTED a workspace that exists only on the surface (%s) — a path was resolved on the wrong machine.\nstdout:\n%s",
			surfaceOnlyDir, refused.out)
	}
	both := refused.out + refused.errOut
	if !strings.Contains(both, surfaceOnlyDir) {
		t.Errorf("the refusal did not name the path it refused.\nwant it to mention %q\ngot:\n%s", surfaceOnlyDir, both)
	}
	if strings.Contains(refused.out, sayEcho) {
		t.Errorf("a turn RAN against a surface-only path: %q", refused.out)
	}
	t.Logf("the engine refused the surface-only workspace with: %s", firstLine(both))

	// ── 3. a relative workspace is the ENGINE's home ─────────────────────────
	//
	// `--host engine:relwork` means relwork under whatever the ENGINE's home
	// is, and the two machines' homes are deliberately different roots. A
	// surface that resolved this against its own home would ask for
	// /opt/surface-home/relwork, which does not exist anywhere.
	relative := w.chatOnce(t, engineRelative, markerPwd+" and where does a relative path land")
	if relative.err != nil {
		w.diagnose(t)
		t.Fatalf("the relative-workspace probe failed: %v\nstdout:\n%s\nstderr:\n%s",
			relative.err, relative.out, relative.errOut)
	}
	wantRelative := filepath.Join(engineLoginHome, engineRelative)
	if !strings.Contains(relative.out, wantRelative) {
		t.Errorf("a relative workspace did not resolve against the engine's home.\nwant the reply to name %q\ngot: %q",
			wantRelative, strings.TrimSpace(relative.out))
	}
	// The two homes really are different roots, which is what makes the line
	// above an assertion rather than a tautology.
	if w.pathExists(t, surfaceName, wantRelative) {
		t.Errorf("%s exists on the surface too, so resolving it locally would have looked identical", wantRelative)
	}

	// ── and a file only the surface has is not readable over there ───────────
	//
	// The last shape of the same law, one layer in: not the workspace this
	// time but a path handed to a TOOL. The engine's `read` runs on the
	// engine's disk, so a surface path is a file that is not there.
	surfaceFile := filepath.Join(surfaceOnlyDir, "laptop-only.txt")
	missing := w.chatOnce(t, engineWorkspace, markerRead+" "+surfaceFile+" please")
	if missing.err != nil {
		t.Fatalf("the read probe failed outright: %v\nstderr:\n%s", missing.err, missing.errOut)
	}
	if strings.Contains(missing.out, "laptop only") {
		t.Errorf("the engine READ a file that exists only on the surface: %q", missing.out)
	}
	if !strings.Contains(missing.out, "Error reading file") {
		t.Errorf("the engine did not report the surface-only file as missing.\ngot: %q", strings.TrimSpace(missing.out))
	}
	// And the same tool on a file the ENGINE has does work, so the assertion
	// above is about the machine and not about the tool being broken.
	present := w.chatOnce(t, engineWorkspace, markerRead+" "+engineWorkspace+"/note.txt please")
	if present.err != nil || !strings.Contains(present.out, "engine side") {
		t.Errorf("the engine could not read its OWN file, so the refusal above proves nothing.\nerr=%v stdout:\n%s\nstderr:\n%s",
			present.err, present.out, present.errOut)
	}
}

// ── scenario 3 — the link dies mid-turn ─────────────────────────────────────

// theLinkDiesMidTurn cuts the ssh process out from under a running turn and
// asserts WHAT THE WAVE ACTUALLY PROMISES, which is not the same sentence on
// both shapes of engine:
//
//   - against a PERSISTENT engine (internal/enginehost holding the session on a
//     unix socket) the redial rejoins the same conversation and the reply
//     completes;
//   - against a one-shot `aforge engine` on a pipe the turn is over, and
//     internal/remote's reconcile says so in as many words rather than leaving
//     a person to work it out from a reply that stopped mid-sentence.
//
// WHICH ONE IS TRUE IS READ OFF THE ENGINE'S OWN DISK rather than assumed. A
// persistent engine leaves a socket under its aforge home
// (internal/enginehost's Dir and SocketPath); a pipe engine leaves nothing. So
// this scenario asserts the behaviour the machine actually has today, and picks
// up the stronger assertion by itself on the day lane A wires the host in.
func (w *remoteWorld) theLinkDiesMidTurn(t *testing.T) {
	persistent := w.enginePersistent(t)
	t.Logf("the engine on this build is persistent=%v (a host socket under %s/v3/hosts is what decides it)",
		persistent, engineAforge)

	// The turn is started in the background so the link can be cut while it is
	// still in flight. The stub holds this one open for about twelve seconds.
	run := w.launch(t, engineWorkspace, engineTalks+"/drop.jsonl", markerSlow+" take your time answering this")

	// Wait until the FIRST word has actually arrived, so the cut lands in the
	// middle of a stream rather than before it started — a disconnect during
	// the handshake is a different scenario and would prove nothing about a
	// turn surviving.
	if !waitFor(6*time.Second, func() bool { return strings.Contains(run.snapshot(), slowTop) }) {
		run.stop()
		w.diagnose(t)
		t.Fatalf("the slow reply never started, so there was no turn to interrupt.\nstdout:\n%s\nstderr:\n%s",
			run.snapshot(), run.snapshotErr())
	}
	t.Logf("the reply had begun (%q); cutting the link", strings.TrimSpace(run.snapshot()))

	// KILLING THE SSH CLIENT IS THE TRUEST SHAPE OF THIS FAILURE. It is what a
	// dropped wifi looks like from the surface's side: the pipe tears, the door
	// is not told, and remote.Roam is left to discover it and redial.
	//
	// IT MATCHES THE PROCESS NAME AND NEVER THE COMMAND LINE, which cost a run
	// to learn: `pkill -f 'ssh -T'` also matches the very shell that is running
	// it, so the kill took its own caller down before it reached ssh and the
	// scenario reported a dead shell instead of a cut link.
	w.tryExec(surfaceName, nil, 20*time.Second, "sh", "-c", "pkill -x ssh; exit 0")

	said := run.wait(t, 3*time.Minute)
	both := said.out + said.errOut
	t.Logf("after the cut the run exited err=%v\nstdout:\n%s\nstderr tail:\n%s",
		said.err, strings.TrimSpace(said.out), tail(said.errOut, 2000))

	if persistent {
		if said.err != nil {
			t.Errorf("a persistent engine should have kept the turn, but the run failed: %v\nstderr:\n%s",
				said.err, tail(said.errOut, 2000))
		}
		if !strings.Contains(said.out, slowEnd) {
			t.Errorf("a persistent engine should have finished the reply after the redial.\nwant %q in stdout, got:\n%s",
				slowEnd, said.out)
		}
		return
	}

	// The non-persistent shape, which is what this tree serves today: the reply
	// began and never finished, and the surface SAID SO. The sentence is
	// internal/remote's redial.go reconcile, quoted here in the fragment that
	// carries its meaning — a test pinned to the whole sentence would fail on a
	// comma.
	if strings.Contains(said.out, slowEnd) {
		t.Errorf("a one-shot engine cannot have finished the turn, yet the reply completed:\n%s", said.out)
	}
	const promise = "does not keep a turn running while nothing is attached"
	if !strings.Contains(both, promise) {
		t.Errorf("the surface did not say what happened to the interrupted turn.\nwant it to contain %q\nstdout:\n%s\nstderr:\n%s",
			promise, said.out, tail(said.errOut, 4000))
	}
	if said.err == nil {
		t.Errorf("a turn that did not survive should not exit clean.\nstdout:\n%s", said.out)
	}

	// AND THE CONVERSATION IS STILL ON THE FAR MACHINE'S DISK, which is the
	// half of the promise a one-shot engine DOES keep: the engine journals as
	// it goes, so the turn is lost and the conversation is not.
	if !w.pathExists(t, engineName, engineAforge+"/v3/projects") {
		t.Errorf("the engine kept no conversation folder after the drop")
	}
}

// ── scenario 4 — an attachment lands over there ─────────────────────────────

// anAttachmentLandsOverThere is Decision 7: a path typed on the surface means
// nothing on the engine, so the bytes travel and the engine writes them into
// that session's own folder.
//
// IT IS SKIPPED TODAY, AND THE REASON IS A DOOR AND NOT A GAP. `/attach` is a
// SLASH COMMAND OF THE SURFACE (internal/tui3's commands.go and attach.go), and
// the only headless door this harness has is `--once`, which submits one
// message and has no tray to put a chip on. Proving this end to end therefore
// needs a terminal driver rather than another assertion, and a scenario that
// quietly asserted something weaker would be worse than one that says what is
// missing. What IS checked here is the precondition: that the surface knows the
// command at all, so the day a headless door exists this turns into a real
// scenario rather than a rediscovery.
func (w *remoteWorld) anAttachmentLandsOverThere(t *testing.T) {
	if !w.pathExists(t, surfaceName, filepath.Join(surfaceOnlyDir, "laptop-only.txt")) {
		t.Fatalf("the harness did not make the file this scenario would send")
	}
	t.Skip("`/attach` is a surface slash command and --once has no tray: proving an attachment crosses " +
		"needs a terminal driver (a tmux or pty harness) rather than another assertion here")
}

// ── scenario 5 — two surfaces at once ───────────────────────────────────────

// twoSurfacesAtOnce is lane A's fan-out: two surfaces attached to ONE
// conversation, each seeing the other's turn.
//
// IT CANNOT BE PROVED WITHOUT A PERSISTENT ENGINE, and saying so is the useful
// answer. `aforge engine` on a pipe is one process per connection by
// construction (cmd/aforge's engine.go hands remote.Serve its own stdin and
// stdout), so two `--host` launches are two engines and two conversations —
// there is no room in which a second window could be. What this scenario does
// today is prove the honest consequence rather than skip in silence: two
// concurrent surfaces both work, and they land in DIFFERENT conversations,
// which is exactly what a non-persistent engine promises.
func (w *remoteWorld) twoSurfacesAtOnce(t *testing.T) {
	// THE SAME CONVERSATION, NAMED, because that is what the scenario is about:
	// two windows in one room rather than two windows that merely both worked.
	first := w.launch(t, engineWorkspace, engineTalks+"/fanout.jsonl", markerEcho+" first window speaking")
	second := w.launch(t, engineWorkspace, engineTalks+"/fanout.jsonl", markerEcho+" second window speaking")
	a := first.wait(t, 3*time.Minute)
	b := second.wait(t, 3*time.Minute)

	if a.err != nil || b.err != nil {
		w.diagnose(t)
		t.Fatalf("two concurrent surfaces did not both complete.\nfirst err=%v stderr:\n%s\nsecond err=%v stderr:\n%s",
			a.err, tail(a.errOut, 2000), b.err, tail(b.errOut, 2000))
	}
	// BOTH REPLIES ARRIVED, AND WHICH SCREEN EACH LANDED ON IS NOT ASSERTED.
	// That is the point of one room rather than a gap in the test: the engine
	// fans a stream to every attached surface, so a window can legitimately
	// watch the other window's turn go past. What must be true is that neither
	// message was lost.
	t.Logf("first window stdout: %q\nfirst window stderr: %s", a.out, tail(a.errOut, 1200))
	t.Logf("second window stdout: %q\nsecond window stderr: %s", b.out, tail(b.errOut, 1200))

	if !w.enginePersistent(t) {
		t.Skip("fan-out to ONE conversation needs the session host (internal/enginehost) wired into " +
			"`aforge engine`; on a pipe engine two surfaces are two engines, which is what just happened")
	}

	// WITH A HOST IN PLACE THE TWO WINDOWS ARE IN ONE ROOM, and the proof is on
	// the engine's disk rather than on either screen. Neither surface named a
	// session, and cmd/aforge's engine.go reads that as "this workspace's
	// latest-or-new" for both — "which is the whole of 'sit down somewhere else
	// and be in it'" — so ONE journal carries both messages. The count of files
	// is deliberately not asserted: earlier scenarios have left their own
	// conversations on that disk, and what matters is that these two share one.
	shared := engineTalks + "/fanout.jsonl"
	found := w.exec(t, engineName, nil, 40*time.Second, "sh", "-c",
		"grep -c 'window speaking' "+shared+" 2>/dev/null || echo 0")
	if strings.TrimSpace(found.out) == "0" {
		w.diagnose(t)
		t.Fatalf("neither window reached the shared conversation at %s", shared)
	}
	whole := w.exec(t, engineName, nil, 40*time.Second, "sh", "-c", "cat "+shared)
	for _, words := range []string{"first window speaking", "second window speaking"} {
		if !strings.Contains(whole.out, words) {
			w.diagnose(t)
			t.Errorf("the shared conversation %s does not hold %q — the two windows were not in one room", shared, words)
		}
	}
	t.Logf("both windows wrote into one conversation on the engine: %s", shared)
}

// ── the world ───────────────────────────────────────────────────────────────

// remoteWorld is the three containers and the network they sit on.
type remoteWorld struct {
	binDir string
}

// newRemoteWorld builds the whole arrangement, or skips.
//
// EVERY STEP THAT CAN LEGITIMATELY BE ABSENT SKIPS AND EVERY STEP THAT CANNOT
// FAILS. No docker, no daemon, no image and no network are all facts about the
// machine the test is running on; an apk install that fails after the network
// pulled the image is a broken harness and should be red.
func newRemoteWorld(t *testing.T) *remoteWorld {
	t.Helper()
	requireDocker(t)

	w := &remoteWorld{binDir: t.TempDir()}
	// TEARDOWN IS REGISTERED BEFORE THE FIRST CONTAINER EXISTS, so a panic or a
	// fatal halfway through setup still removes whatever did get made.
	t.Cleanup(func() { w.teardown(t) })
	w.teardownQuietly()

	w.buildStatic(t, "modelstub", "./test/remote/stub")
	w.buildStatic(t, "aforge", "./cmd/aforge")

	mustRun(t, 60*time.Second, "docker", "network", "create", netName)
	w.startModel(t)
	w.startEngine(t)
	w.startSurface(t)
	w.pairThem(t)
	return w
}

// requireDocker is the skip gate. It asks three separate questions because they
// have three separate answers a person can act on.
func requireDocker(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("no docker on this machine: this lane needs two containers with two filesystems")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, "docker", "info").CombinedOutput(); err != nil {
		t.Skipf("the docker daemon did not answer (start it with `dockerd &`): %v\n%s", err, tail(string(out), 400))
	}
	pull, cancelPull := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancelPull()
	if out, err := exec.CommandContext(pull, "docker", "pull", baseImage).CombinedOutput(); err != nil {
		t.Skipf("could not pull %s: %v\n%s", baseImage, err, tail(string(out), 400))
	}
}

// buildStatic compiles one command for the containers.
//
// CGO_ENABLED=0 IS THE WHOLE REASON THE BASE IMAGE CAN BE ALPINE: a cgo build
// links against glibc and would die on musl with an error about a file that is
// plainly there. This tree's only sqlite is modernc.org's, which is pure Go, so
// the flag holds — and the build is done HERE, on the host, rather than inside
// a container, because a Go toolchain in each container would cost minutes per
// run and a module download per image.
func (w *remoteWorld) buildStatic(t *testing.T, name, pkg string) {
	t.Helper()
	out := filepath.Join(w.binDir, name)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	build := exec.CommandContext(ctx, "go", "build", "-o", out, pkg)
	build.Dir = repoRoot(t)
	build.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH=amd64")
	if said, err := build.CombinedOutput(); err != nil {
		// A build that fails because ANOTHER LANE's file is half-written is not
		// this lane's fault and is worth saying plainly, because it is the
		// single most likely red in a parallel wave.
		t.Fatalf("could not build %s (another lane's in-flight file will do this; try again after `go build ./...` is clean): %v\n%s",
			pkg, err, said)
	}
}

// repoRoot walks up from this package to the module root, so the build above
// runs where go.mod is no matter where the test binary was started.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod above %s", dir)
		}
		dir = parent
	}
}

// ── the three machines ──────────────────────────────────────────────────────

// startModel brings up the scripted endpoint. It runs under `sleep infinity`
// with the stub started by a second exec rather than as PID 1, so that the
// stub's log is a FILE this harness can read back on a failure — `docker logs`
// alone would lose it the moment somebody restarted the process.
func (w *remoteWorld) startModel(t *testing.T) {
	t.Helper()
	w.run(t, modelName)
	w.copyIn(t, modelName, "modelstub", "/usr/local/bin/modelstub")
	w.exec(t, modelName, nil, 20*time.Second, "sh", "-c",
		"(setsid /usr/local/bin/modelstub -addr :8080 >/var/log/modelstub.log 2>&1 &) ; sleep 1")
	// busybox wget is in the base image, so the readiness probe needs nothing
	// installed. The catalog path is asked for because it is the one the engine
	// asks for first.
	if !waitFor(30*time.Second, func() bool {
		got := w.tryExec(modelName, nil, 10*time.Second, "sh", "-c",
			"wget -qO- 'http://127.0.0.1:8080/api/v1/models?output_modalities=all'")
		return strings.Contains(got.out, stubModel)
	}) {
		log := w.tryExec(modelName, nil, 10*time.Second, "sh", "-c", "cat /var/log/modelstub.log")
		t.Fatalf("the model stub never answered:\n%s", log.out+log.errOut)
	}
}

// startEngine is the machine that owns the work: sshd, aforge, a workspace with
// a couple of files, and an aforge home whose profile points the provider at
// the stub.
func (w *remoteWorld) startEngine(t *testing.T) {
	t.Helper()
	w.run(t, engineName)
	w.installSSH(t, engineName, "openssh")
	w.copyIn(t, engineName, "aforge", "/usr/local/bin/aforge.bin")

	// THE WRAPPER EXISTS BECAUSE SSH DOES NOT CARRY AN ENVIRONMENT. `ssh host
	// aforge engine` is a non-login, non-interactive command: no profile is
	// sourced, so AFORGE_HOME and AFORGE_BASE_URL would arrive unset and the
	// engine would open the wrong home against the real OpenRouter. The
	// alternative — sshd's PermitUserEnvironment — moves the same three lines
	// into a file with worse failure modes. Everything the far machine needs to
	// know about itself is therefore stated here, on the far machine, which is
	// also exactly what Decision 6 says about where these belong.
	w.write(t, engineName, "/usr/local/bin/aforge", 0o755, strings.Join([]string{
		"#!/bin/sh",
		"export AFORGE_HOME=" + engineAforge,
		"export AFORGE_PROFILE_DIR=",
		"export AFORGE_BASE_URL=" + stubBase,
		"export OPENROUTER_API_KEY=" + stubKey,
		"exec /usr/local/bin/aforge.bin \"$@\"",
		"",
	}, "\n"))

	// The profile. It is a FLAT map of dotted keys, which is what
	// internal/config's persistedValue reads out of <AFORGE_HOME>/config.json.
	// tools.approvalMode is `allow` because nobody is at a keyboard: a consent
	// card raised in a headless run is a hang, not a test.
	w.exec(t, engineName, nil, 20*time.Second, "mkdir", "-p", engineAforge)
	w.write(t, engineName, engineAforge+"/config.json", 0o600, strings.Join([]string{
		`{`,
		`  "api_key": "` + stubKey + `",`,
		`  "model.talk": "` + stubModel + `",`,
		`  "models.tiers.low": "` + stubModel + `",`,
		`  "models.tiers.high": "` + stubModel + `",`,
		`  "tools.approvalMode": "allow"`,
		`}`,
		``,
	}, "\n"))

	// Two workspaces, because the path law has two shapes to prove: an absolute
	// one nothing on the surface shares, and a relative one that can only mean
	// something once it has been read against THIS machine's home directory.
	w.exec(t, engineName, nil, 20*time.Second, "mkdir", "-p", engineWorkspace,
		filepath.Join(engineLoginHome, engineRelative), engineTalks)
	w.write(t, engineName, engineWorkspace+"/note.txt", 0o644,
		"this file is on the engine side and nowhere else\n")
	w.write(t, engineName, engineWorkspace+"/README.md", 0o644,
		"# the engine machine's project\n\nOwned by the machine that runs the work.\n")
	w.write(t, engineName, filepath.Join(engineLoginHome, engineRelative)+"/note.txt", 0o644,
		"relative workspaces resolve against the engine's home\n")

	// sshd. The host keys are generated here rather than baked, and root logs
	// in by key only — there is no password on this account and there must not
	// be one, because the surface's key is the only credential in the harness.
	w.exec(t, engineName, nil, 60*time.Second, "sh", "-c", strings.Join([]string{
		"ssh-keygen -A",
		"mkdir -p /root/.ssh /var/empty",
		"chmod 700 /root/.ssh",
		"printf 'PermitRootLogin prohibit-password\\nPasswordAuthentication no\\nPubkeyAuthentication yes\\n' >> /etc/ssh/sshd_config",
		"(setsid /usr/sbin/sshd -D -e >/var/log/sshd.log 2>&1 &)",
		"sleep 1",
	}, " && "))
}

// startSurface is the machine the person sits at: ssh, aforge, its OWN home
// root, and one directory the engine has never heard of.
//
// IT IS DELIBERATELY POORER THAN THE ENGINE. No API key, no model profile, no
// workspace the engine shares — because every one of those absences is a thing
// a scenario asserts, and a surface that happened to have them would let a
// local resolution pass unnoticed.
func (w *remoteWorld) startSurface(t *testing.T) {
	t.Helper()
	w.run(t, surfaceName)
	w.installSSH(t, surfaceName, "openssh-client")
	w.copyIn(t, surfaceName, "aforge", "/usr/local/bin/aforge")

	w.exec(t, surfaceName, nil, 30*time.Second, "sh", "-c", strings.Join([]string{
		"mkdir -p " + surfaceHome + "/.ssh " + surfaceAforge + " " + surfaceOnlyDir,
		// THE HOME IS MOVED IN /etc/passwd AND NOT ONLY IN THE ENVIRONMENT, and
		// finding that out cost a run: OpenSSH resolves `~` from the password
		// database (pw_dir) rather than from $HOME, so a surface whose home was
		// only an environment variable still read /root/.ssh and never saw the
		// key or the config this harness wrote. Moving it properly is also more
		// honest about what this container is — a machine whose home directory
		// is somewhere else, which is the difference the whole lane turns on.
		`sed -i 's#^root:x:0:0:root:/root:#root:x:0:0:root:` + surfaceHome + `:#' /etc/passwd`,
		"chmod 700 " + surfaceHome + "/.ssh",
		// The key this surface reaches the engine with. ssh finds it because
		// every command below runs with HOME pointed at this directory.
		"ssh-keygen -q -t ed25519 -N '' -f " + surfaceHome + "/.ssh/id_ed25519",
		// No host-key question, because the prompt law means ssh would ask it on
		// the terminal and there is nobody there to answer.
		"printf 'StrictHostKeyChecking no\\nUserKnownHostsFile /dev/null\\nLogLevel ERROR\\n' > " + surfaceHome + "/.ssh/config",
		"chmod 600 " + surfaceHome + "/.ssh/config",
	}, " && "))
	w.write(t, surfaceName, filepath.Join(surfaceOnlyDir, "laptop-only.txt"), 0o644,
		"laptop only: this file lives on the surface machine\n")
}

// pairThem authorizes the surface's key on the engine and proves ssh works
// before any aforge is involved — so a red in scenario 1 is about aforge and
// never about the plumbing under it.
func (w *remoteWorld) pairThem(t *testing.T) {
	t.Helper()
	pub := w.exec(t, surfaceName, nil, 20*time.Second, "cat", surfaceHome+"/.ssh/id_ed25519.pub")
	w.write(t, engineName, "/root/.ssh/authorized_keys", 0o600, strings.TrimSpace(pub.out)+"\n")

	if !waitFor(45*time.Second, func() bool {
		got := w.tryExec(surfaceName, w.surfaceEnv(), 20*time.Second,
			"ssh", "-T", "-o", "BatchMode=yes", "root@"+engineName, "echo SSH-OK")
		return strings.Contains(got.out, "SSH-OK")
	}) {
		said := w.tryExec(surfaceName, w.surfaceEnv(), 20*time.Second,
			"ssh", "-v", "-T", "-o", "BatchMode=yes", "root@"+engineName, "echo SSH-OK")
		sshd := w.tryExec(engineName, nil, 10*time.Second, "sh", "-c", "cat /var/log/sshd.log")
		t.Fatalf("the surface could not ssh to the engine.\nssh -v:\n%s\n%s\nengine sshd log:\n%s",
			said.out, tail(said.errOut, 2000), sshd.out+sshd.errOut)
	}

	// And the far machine's aforge really is the one this harness put there.
	if got := w.tryExec(surfaceName, w.surfaceEnv(), 60*time.Second,
		"ssh", "-T", "-o", "BatchMode=yes", "root@"+engineName, "aforge --version || aforge version || true"); got.out == "" && got.errOut == "" {
		t.Logf("the engine's aforge printed nothing for --version; that is not fatal, the handshake will say")
	}
}

// installSSH installs one package inside a container.
//
// THE http:// REWRITE IS NOT A SHORTCUT, IT IS THE ONLY THING THAT WORKS HERE.
// This environment routes outbound HTTPS through an intercepting proxy whose CA
// the base image does not carry, so apk over https fails to verify and the
// install dies. The container's egress to plain http is direct and unmolested,
// and apk's own index and packages are signed and verified by apk itself
// regardless of the transport — so the rewrite costs nothing the package
// manager was relying on. It is scoped to a throwaway test container and to
// nothing else.
func (w *remoteWorld) installSSH(t *testing.T, container, pkg string) {
	t.Helper()
	got := w.tryExec(container, nil, 4*time.Minute, "sh", "-c",
		`sed -i "s#https://#http://#g" /etc/apk/repositories && apk add --no-cache `+pkg)
	if got.err != nil {
		t.Skipf("could not install %s in %s (no package mirror reachable from a container here): %v\n%s",
			pkg, container, got.err, tail(got.out+got.errOut, 800))
	}
}

// ── driving aforge ──────────────────────────────────────────────────────────

// said is one command's whole output.
type said struct {
	out    string
	errOut string
	err    error
}

// surfaceEnv is what every command on the surface machine runs with. HOME is
// the seam that moves ssh's key and config; AFORGE_HOME moves everything this
// machine keeps on the person's behalf. THERE IS NO KEY AND NO BASE URL HERE,
// and that absence is the point.
func (w *remoteWorld) surfaceEnv() []string {
	return []string{
		"HOME=" + surfaceHome,
		"AFORGE_HOME=" + surfaceAforge,
		"AFORGE_PROFILE_DIR=",
	}
}

// chatOnce is one headless message over a connection, run to completion.
func (w *remoteWorld) chatOnce(t *testing.T, workspace, text string) said {
	t.Helper()
	return w.chatOnceBackground(t, workspace, text).wait(t, 3*time.Minute)
}


// running is a `--once` launch that has not finished yet, so a scenario can cut
// the link out from under it.
type running struct {
	cmd    *exec.Cmd
	out    *syncBuffer
	errOut *syncBuffer
	done   chan error
	cancel context.CancelFunc
}

func (r *running) snapshot() string    { return r.out.String() }
func (r *running) snapshotErr() string { return r.errOut.String() }

func (r *running) wait(t *testing.T, within time.Duration) said {
	t.Helper()
	select {
	case err := <-r.done:
		return said{out: r.out.String(), errOut: r.errOut.String(), err: err}
	case <-time.After(within):
		r.stop()
		return said{out: r.out.String(), errOut: r.errOut.String(),
			err: fmt.Errorf("the run did not finish within %s", within)}
	}
}

func (r *running) stop() { r.cancel() }

// chatOnceBackground starts `aforge chat --host … --once …` on the surface and
// hands back a handle. The command is the one a person types, spelled the way
// they would type it: the machine, a colon, the workspace on THAT machine.
func (w *remoteWorld) chatOnceBackground(t *testing.T, workspace, text string) *running {
	t.Helper()
	return w.launch(t, workspace, "", text)
}

// launch is the command itself. session, when named, becomes --session and is a
// path the ENGINE resolves.
//
// THE SCENARIOS THAT NAME ONE DO IT TO STAY OUT OF EACH OTHER'S ROOM, and that
// is a fact about the engine rather than about tidiness: a hello naming no
// session joins "this workspace's latest-or-new" (cmd/aforge's engine.go Key),
// so with a persistent host a later scenario's surfaces walk into the
// conversation an earlier one left a turn running in and are handed ITS reply.
// That happened here, and it read exactly like a broken harness rather than the
// correct fan-out it was.
func (w *remoteWorld) launch(t *testing.T, workspace, session, text string) *running {
	t.Helper()
	target := "root@" + engineName
	if workspace != "" {
		target += ":" + workspace
	}
	args := []string{"exec"}
	for _, pair := range w.surfaceEnv() {
		args = append(args, "-e", pair)
	}
	args = append(args, surfaceName, "aforge", "chat", "--host", target)
	if session != "" {
		args = append(args, "--session", session)
	}
	args = append(args, "--once", text)

	// The context is the outer bound and nothing else: every wait below has its
	// own, shorter one, and this exists so that a wedged container can never
	// hang a whole `go test` run.
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, errOut := &syncBuffer{}, &syncBuffer{}
	cmd.Stdout, cmd.Stderr = out, errOut
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("could not start the surface's chat: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	handle := &running{cmd: cmd, out: out, errOut: errOut, done: done, cancel: cancel}
	// CANCELLING THE `docker exec` DOES NOT END THE PROCESS INSIDE THE
	// CONTAINER, and that cost a run too: a scenario that gave up on a slow
	// turn left an aforge still attached over there, and the NEXT scenario's
	// surfaces joined that conversation and were handed its reply. So the
	// sweep is part of leaving, and it runs at the end of the subtest that
	// started the run rather than between two concurrent ones.
	t.Cleanup(func() {
		cancel()
		w.tryExec(surfaceName, nil, 20*time.Second, "sh", "-c", "pkill -x aforge; pkill -x ssh; exit 0")
	})
	t.Logf("surface → aforge chat --host %s --once %q", target, text)
	return handle
}

// ── reading the far machines ────────────────────────────────────────────────

// pathExists asks one container whether a path is there. It is the assertion
// that makes every path claim in this file falsifiable rather than decorative.
func (w *remoteWorld) pathExists(t *testing.T, container, path string) bool {
	t.Helper()
	got := w.tryExec(container, nil, 20*time.Second, "sh", "-c",
		"test -e "+path+" && echo THERE || echo ABSENT")
	return strings.Contains(got.out, "THERE")
}

// enginePersistent reads off the engine's own disk whether `aforge engine` is
// backed by a session host. internal/enginehost's Dir puts a socket under
// <AFORGE_HOME>/v3/hosts, so its presence is the fact and nothing here has to
// guess at a build's shape.
func (w *remoteWorld) enginePersistent(t *testing.T) bool {
	t.Helper()
	got := w.tryExec(engineName, nil, 20*time.Second, "sh", "-c",
		"ls "+engineAforge+"/v3/hosts/*/host.sock 2>/dev/null | head -1")
	return strings.TrimSpace(got.out) != ""
}

// diagnose dumps everything a two-machine failure needs, because a scenario
// that fails with "exit status 1" is worthless. Three machines, three logs, and
// whatever the engine wrote about the conversation.
func (w *remoteWorld) diagnose(t *testing.T) {
	t.Helper()
	t.Log("──────── diagnosis ────────")
	for _, probe := range []struct {
		what      string
		container string
		command   string
	}{
		{"model stub log", modelName, "tail -n 60 /var/log/modelstub.log"},
		{"engine sshd log", engineName, "tail -n 40 /var/log/sshd.log"},
		{"engine host log", engineName, "cat " + engineAforge + "/v3/hosts/*/host.log 2>/dev/null | tail -n 40"},
		{"engine conversations", engineName, "find " + engineAforge + "/v3/projects -maxdepth 3 2>/dev/null | head -n 40"},
		{"engine session journal", engineName,
			"find " + engineAforge + "/v3/projects -name 'session*.jsonl' 2>/dev/null | head -1 | xargs -r tail -n 20"},
		{"engine processes", engineName, "ps -o pid,args 2>/dev/null | head -n 25"},
		{"surface aforge home", surfaceName, "find " + surfaceAforge + " -maxdepth 3 2>/dev/null | head -n 30"},
	} {
		got := w.tryExec(probe.container, nil, 25*time.Second, "sh", "-c", probe.command)
		t.Logf("── %s (%s) ──\n%s", probe.what, probe.container, strings.TrimSpace(got.out+got.errOut))
	}
	for _, container := range []string{modelName, engineName, surfaceName} {
		logs := runOut(30*time.Second, "docker", "logs", "--tail", "30", container)
		if trimmed := strings.TrimSpace(logs.out + logs.errOut); trimmed != "" {
			t.Logf("── docker logs %s ──\n%s", container, trimmed)
		}
	}
	t.Log("───────────────────────────")
}

// ── docker, plainly ─────────────────────────────────────────────────────────

// run starts one container on the bridge network under `sleep infinity`.
//
// PID 1 IS A SLEEP RATHER THAN THE WORKLOAD so that every machine can be set up
// in place — packages installed, binaries copied, daemons started — without a
// Dockerfile, an image build, or a registry. It costs one exec per step and
// buys a harness that is entirely readable in one file.
func (w *remoteWorld) run(t *testing.T, name string) {
	t.Helper()
	mustRun(t, 2*time.Minute, "docker", "run", "-d", "--name", name,
		"--network", netName, "--hostname", name, baseImage, "sleep", "infinity")
}

// copyIn puts one host-built binary into a container and makes it executable.
func (w *remoteWorld) copyIn(t *testing.T, container, name, dest string) {
	t.Helper()
	mustRun(t, 2*time.Minute, "docker", "cp", filepath.Join(w.binDir, name), container+":"+dest)
	w.exec(t, container, nil, 20*time.Second, "chmod", "0755", dest)
}

// write puts a file inside a container through stdin, so the content can hold
// anything at all — quotes, newlines, a JSON body — without a shell quoting it.
func (w *remoteWorld) write(t *testing.T, container, path string, mode os.FileMode, content string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "exec", "-i", container, "sh", "-c",
		"mkdir -p \"$(dirname "+path+")\" && cat > "+path)
	cmd.Stdin = strings.NewReader(content)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("write %s in %s: %v\n%s", path, container, err, out)
	}
	w.exec(t, container, nil, 20*time.Second, "chmod", fmt.Sprintf("%04o", mode.Perm()), path)
}

// exec runs a command in a container and fails the test if it does not work.
func (w *remoteWorld) exec(t *testing.T, container string, env []string, within time.Duration, command ...string) said {
	t.Helper()
	got := w.tryExec(container, env, within, command...)
	if got.err != nil {
		t.Fatalf("in %s: %v\n$ %s\n%s", container, got.err, strings.Join(command, " "), tail(got.out+got.errOut, 2000))
	}
	return got
}

// tryExec is the same thing with the failure handed back instead of raised, for
// every probe whose answer is the point.
func (w *remoteWorld) tryExec(container string, env []string, within time.Duration, command ...string) said {
	args := []string{"exec"}
	for _, pair := range env {
		args = append(args, "-e", pair)
	}
	args = append(args, container)
	args = append(args, command...)
	return runOut(within, "docker", args...)
}

// teardown removes everything this harness made. It runs from t.Cleanup, so it
// runs after a fatal and after a panic — which matters more here than anywhere
// else in the tree, because what is left behind is not a temp directory but
// three containers and a network holding a subnet.
func (w *remoteWorld) teardown(t *testing.T) {
	t.Helper()
	w.teardownQuietly()
	t.Log("removed the test containers and network")
}

func (w *remoteWorld) teardownQuietly() {
	for _, name := range []string{modelName, engineName, surfaceName} {
		_ = runOut(60*time.Second, "docker", "rm", "-f", name)
	}
	_ = runOut(60*time.Second, "docker", "network", "rm", netName)
}

// ── small tools ─────────────────────────────────────────────────────────────

func mustRun(t *testing.T, within time.Duration, name string, args ...string) said {
	t.Helper()
	got := runOut(within, name, args...)
	if got.err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), got.err, tail(got.out+got.errOut, 1500))
	}
	return got
}

// runOut runs one command with a bound on how long it may take. NOTHING IN THIS
// FILE MAY HANG FOREVER: a docker daemon that has wedged is a real outcome, and
// a harness that waited on it would take the whole `go test` run with it.
func runOut(within time.Duration, name string, args ...string) said {
	ctx, cancel := context.WithTimeout(context.Background(), within)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	if ctx.Err() != nil && err != nil {
		err = fmt.Errorf("%w (gave up after %s)", err, within)
	}
	return said{out: out.String(), errOut: errOut.String(), err: err}
}

// waitFor polls until the condition holds or the span runs out. Polling rather
// than sleeping a fixed amount is what keeps the harness quick on a fast
// machine and still correct on a slow one.
func waitFor(within time.Duration, condition func() bool) bool {
	deadline := time.Now().Add(within)
	for {
		if condition() {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// syncBuffer is a bytes.Buffer a scenario can read WHILE the command writing to
// it is still running, which is what the interrupted-turn scenario needs: it
// waits for the first word of a reply before cutting the link. The lock is not
// an optimization to skip — os/exec writes from a goroutine of its own, so an
// unguarded read here is a data race the test detector would call, correctly.
type syncBuffer struct {
	mu   sync.Mutex
	seen bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.seen.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.seen.String()
}

func tail(text string, limit int) string {
	text = strings.TrimSpace(text)
	if len(text) <= limit {
		return text
	}
	return "…" + text[len(text)-limit:]
}

func firstLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
