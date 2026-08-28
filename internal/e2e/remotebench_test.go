//go:build ssh_bench

// remotebench_test.go MEASURES THE REMOTE-FILES WIRE AGAINST A REAL SECOND
// MACHINE, so that every number in docs/remote-files-bench.md is a number
// somebody produced on a link rather than a number somebody expected.
//
// WHY IT EXISTS BESIDE THE LOOPBACK TESTS. internal/remote's loopback proves
// the protocol: the real client against the real server over net.Pipe, fast and
// deterministic. What a pipe in one process cannot tell anybody is what the
// arrangement COSTS — a round trip is free over net.Pipe, base64 is free, ssh
// is not there at all, and a killed link is a thing a test invents rather than
// a thing that happens. Those are exactly the four numbers a person weighing
// `aforge chat --host` against `scp` wants, so they are measured here, over
// ssh, against a machine in another room.
//
// THE LOCAL HALF RUNS IN THIS PROCESS. [benchLink.spawn] is cmd/aforge's
// engineLink.spawn with the same shape — `ssh -T <dest> "aforge engine …
// --workspace '<ws>'"`, stdin and stdout as one [io.ReadWriteCloser] — handed
// to [remote.Roam] as its dialer. So what is being timed is the wire the
// product actually uses, including its redialling, and not a re-implementation
// of it that might be faster for reasons nobody shipped.
//
// AND EVERY FETCH IS VERIFIED. A benchmark that does not check what came back
// is measuring corruption-tolerant speed, which is not a property anybody
// wants. [fiveDigest] is checked on every repetition of every fetch, the scp
// copies included, and [noteDigest] is checked once at the top so that a run
// pointed at the wrong directory stops there instead of publishing a table.
//
// WHAT IT NEEDS ON THE FAR MACHINE: an aforge built from this branch (the two
// ends refuse each other at the door otherwise), and a workspace holding
// note.txt, sub/inner.txt, five-mb.bin and twenty-mb.bin. Nothing else on that
// machine is written to, and the one directory this harness makes is removed
// before it returns.
//
// TWO DEPARTURES FROM THE DOOR'S OWN SPAWN, both stated here because a
// benchmark that quietly ran something else than the product would be worthless.
//
//   - `--no-host`. The door lets the far `aforge engine` attach to a session
//     HOST that outlives the connection, and that is the right default for a
//     conversation. It is the wrong one for a measurement on somebody else's
//     laptop: the numbers would be partly about a socket splice, an already
//     running daemon of unknown age would answer instead of the binary under
//     test, and the run would leave a new daemon behind. `--no-host` is the
//     shape internal/remote calls the floor — the engine serves this pipe and
//     ends with it — so what is timed is ssh, frames, and an engine, and the
//     far machine is left as it was found.
//   - A provider key in front of the command. The engine refuses to boot
//     without one even though nothing here submits a turn, so [keyEnv] is
//     passed through when it is set and [placeholderKey] satisfies the door
//     when it is not. THE PLACEHOLDER IS NEVER SPENT: this harness fetches
//     files and lists directories and never opens a turn, and the engine
//     writes no key to the far machine's profile.
//
// RUN IT:
//
//	AFORGE_BENCH_HOST=blackmac go test -tags ssh_bench -run TestRemoteBench -v ./internal/e2e/
//
// The build tag keeps it out of `go test ./...` entirely, and with no
// AFORGE_BENCH_HOST it skips with the sentence that says what to set.
package e2e

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/remote"
)

// ── what the far machine is expected to hold ────────────────────────────────

const (
	// hostEnv names the ssh destination, and its absence is what makes this
	// file skip rather than fail: a benchmark that needs another machine is not
	// a broken test on a machine that has not got one.
	hostEnv = "AFORGE_BENCH_HOST"
	// workspaceEnv is the seeded directory over there, as `--host host:path`
	// would spell it — relative to the far machine's home.
	workspaceEnv = "AFORGE_BENCH_WORKSPACE"
	// linkEnv is one phrase describing the link, which goes into the document
	// because a throughput number with no link beside it means nothing — and
	// because only a person knows whether two machines are a cable apart or
	// coming back through a relay on another continent. Unset, the document
	// says it was not told rather than guessing.
	linkEnv = "AFORGE_BENCH_LINK"

	// keyEnv is a provider key for the FAR machine's engine, which refuses to
	// boot without one. Set it only if that machine has none of its own; it
	// travels on the remote command line and is therefore visible in that
	// machine's process list for as long as the run takes.
	keyEnv = "AFORGE_BENCH_KEY"

	defaultWorkspace = "af-files-e2e"

	// placeholderKey is what the far engine boots on when nothing else is
	// available. It is deliberately not a key: no turn is submitted anywhere in
	// this file, so nothing ever tries to spend it, and the shape of the
	// sentence says so to anybody who finds it in a process list.
	placeholderKey = "sk-aforge-bench-placeholder-never-spent"
)

// The seeded files and their digests. THE DIGESTS ARE THE POINT: they are
// checked on every repetition, so a run that reports 40MB/s is reporting the
// speed of bytes that arrived intact.
const (
	noteName   = "note.txt"
	noteDigest = "a643736dbe59d5b55777984b5e83f2a598693fa3063c87f4a92d89df195bb576"
	noteSize   = 20

	fiveName   = "five-mb.bin"
	fiveDigest = "b38c951d0036ca4c622663f1ebffc98564ad950f7dfec9a62276ce18c04e720f"
	fiveSize   = 5 << 20

	// twentyName is over the engine's maxFetchBytes (internal/remote/file.go)
	// and exists to be refused.
	twentyName = "twenty-mb.bin"

	// thousandDir is the only thing this harness creates on the far machine,
	// and it is removed again. See [seedThousand].
	thousandDir   = "bench-thousand"
	thousandCount = 1000
)

// benchDoc is where the table is written, relative to this package's own
// directory, which is where `go test` runs.
const benchDoc = "../../docs/remote-files-bench.md"

// reproduce is the command a reader types to get these numbers themselves. It
// is written into the document, so it lives here once rather than in prose
// that could drift away from the build tag it names.
const reproduce = "AFORGE_BENCH_HOST=blackmac go test -tags ssh_bench -run TestRemoteBench -v ./internal/e2e/"

// ── the link ────────────────────────────────────────────────────────────────

// benchLink is one ssh child and the pipes it carries, which is cmd/aforge's
// engineLink with everything a surface needs taken out and one thing a
// benchmark needs put in: the current process is held so that [benchLink.kill]
// can end THIS harness's own child by its handle. Killing by handle rather than
// by pattern matters — a pkill for anything looking like ssh would take out the
// terminal somebody is reading this on.
type benchLink struct {
	dest      string
	workspace string
	// key is the provider key handed to the far engine, for the reason the
	// header states. Empty is a machine that has its own.
	key string

	mu      sync.Mutex
	process *exec.Cmd
	// spawns counts the ssh children this harness has started, which is how a
	// redial is proved to have actually happened rather than assumed from a
	// call that eventually worked.
	spawns int
}

// spawn starts one `ssh -T <dest> aforge engine --workspace '<ws>'` and hands
// back its pipes. It is [remote.Dialer], and it is deliberately the same shape
// as cmd/aforge/chatv3_host.go's engineLink.spawn: -T because there is nothing
// interactive on the far end and a pseudo-terminal would turn a frame into
// nonsense, and the workspace quoted because a path with a space in it is a
// path.
func (l *benchLink) spawn() (io.ReadWriteCloser, error) {
	remoteCommand := "aforge engine --no-host"
	if l.workspace != "" {
		remoteCommand += " --workspace " + shellWord(l.workspace)
	}
	if l.key != "" {
		remoteCommand = "OPENROUTER_API_KEY=" + shellWord(l.key) + " " + remoteCommand
	}
	process := exec.Command("ssh", "-T", "-o", "BatchMode=yes", l.dest, remoteCommand)
	stdin, err := process.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := process.StdoutPipe()
	if err != nil {
		return nil, err
	}
	// ssh's stderr is passed through unchanged. A locale warning from a mac
	// answering a Linux terminal is the usual thing on it and is harmless; a
	// host-key question or a passphrase prompt is not, and hiding either would
	// leave a run that hangs for no visible reason.
	process.Stderr = os.Stderr
	if err := process.Start(); err != nil {
		return nil, fmt.Errorf("could not start ssh to %s: %w", l.dest, err)
	}
	l.hold(process)
	return benchPipe{r: stdout, w: stdin}, nil
}

// hold takes the new child and reaps the old one in the background, for the
// reason the door states: a redial happens after the previous pipe died, and a
// child nobody waits on is a zombie for as long as this process is alive.
func (l *benchLink) hold(process *exec.Cmd) {
	l.mu.Lock()
	previous := l.process
	l.process, l.spawns = process, l.spawns+1
	l.mu.Unlock()
	if previous != nil {
		go func() { _ = previous.Wait() }()
	}
}

// kill ends the ssh child currently carrying the connection, which is how the
// cut in measurement 6 is made: a real process, ended by its own handle.
func (l *benchLink) kill() error {
	l.mu.Lock()
	process := l.process
	l.mu.Unlock()
	if process == nil || process.Process == nil {
		return fmt.Errorf("there is no ssh child to end")
	}
	return process.Process.Kill()
}

// started is how many ssh children this link has spawned.
func (l *benchLink) started() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.spawns
}

// benchPipe is the ssh child's two halves as one [io.ReadWriteCloser], which is
// what internal/remote dials on. Closing the write half is what tells the
// engine there is nothing more coming.
type benchPipe struct {
	r io.ReadCloser
	w io.WriteCloser
}

func (p benchPipe) Read(b []byte) (int, error)  { return p.r.Read(b) }
func (p benchPipe) Write(b []byte) (int, error) { return p.w.Write(b) }
func (p benchPipe) Close() error {
	err := p.w.Close()
	_ = p.r.Close()
	return err
}

// shellWord quotes a word for the far machine's login shell. It is the same
// quoting cmd/aforge's shellQuote does, written again because that one lives in
// package main and a test cannot reach it.
func shellWord(word string) string {
	return "'" + strings.ReplaceAll(word, "'", `'\''`) + "'"
}

// ── the run ─────────────────────────────────────────────────────────────────

func TestRemoteBench(t *testing.T) {
	dest := strings.TrimSpace(os.Getenv(hostEnv))
	if dest == "" {
		t.Skipf("this benchmark needs a real second machine: set %s to an ssh destination that already works and has an aforge from this branch on the PATH a non-login ssh command sees (`ssh <host> aforge version` must answer), and %s to the seeded workspace there — it defaults to %q, and must hold %s, %s and %s.",
			hostEnv, workspaceEnv, defaultWorkspace, noteName, fiveName, twentyName)
	}
	workspace := strings.TrimSpace(os.Getenv(workspaceEnv))
	if workspace == "" {
		workspace = defaultWorkspace
	}
	// A LINK NOBODY NAMED IS NOT CALLED A LAN. Guessing here is how a table
	// measured over a relay ends up published as a local network.
	link := strings.TrimSpace(os.Getenv(linkEnv))

	key := strings.TrimSpace(os.Getenv(keyEnv))
	if key == "" {
		key = placeholderKey
	}
	pipes := &benchLink{dest: dest, workspace: workspace, key: key}
	client, err := remote.Roam(dest, remote.Hello{Workspace: workspace}, remote.Roaming{Dial: pipes.spawn})
	if err != nil {
		// A MACHINE THAT IS ASLEEP IS NOT A FAILING TEST. The one thing this
		// file needs is a second computer, and a laptop with its lid shut is
		// the ordinary state of one — so an unreachable host skips with the
		// far end's own words and the fix, exactly as a missing
		// AFORGE_BENCH_HOST does.
		//
		// A VERSION MISMATCH IS DIFFERENT AND FAILS. There the machine
		// answered: two builds that disagree about the wire is a fault
		// somebody has to fix, and a skip would hide it.
		if strings.Contains(err.Error(), "different version of aforge") {
			t.Fatalf("%s and this machine do not speak the same wire (this build is v%d): %v — rebuild the far aforge from this branch", dest, remote.Version, err)
		}
		t.Skipf("no engine to measure on %s: %v — wake the machine, check `ssh %s aforge version` answers, and run it again.", dest, err, dest)
	}
	defer func() { _ = client.Close() }()

	welcome := client.Welcome()
	if welcome.Version != remote.Version {
		t.Fatalf("the far end answered wire v%d and this build speaks v%d — the two ends are not the same protocol, and every number below would be a number about a mismatch", welcome.Version, remote.Version)
	}
	t.Logf("engine on %s: wire v%d, workspace %s", dest, welcome.Version, welcome.Workspace)

	// THE WORKSPACE IS PROVED BEFORE ANYTHING IS TIMED. A run against a
	// directory holding different files would produce a whole table of numbers
	// about nothing, so the smallest seeded file is fetched once and checked
	// against its digest, and a mismatch stops the run here rather than at the
	// bottom of a document.
	note, err := client.FetchFile(noteName)
	if err != nil {
		t.Fatalf("could not fetch %s from the workspace on %s: %v — this benchmark needs the seeded workspace, not an empty directory", noteName, dest, err)
	}
	verify(t, note, noteDigest, noteSize)

	var table []benchRow
	add := func(row benchRow) { table = append(table, row) }

	// The numbers measurements later in the file need from earlier ones. They
	// are declared here because the subtests are steps of one measurement and
	// not independent tests — running only the sixth would be measuring a
	// redial with nothing to compare it to.
	var (
		fetched   remote.FetchedFile
		wireFetch time.Duration
		scpCopy   time.Duration
		rttFloor  time.Duration
		coldDial  time.Duration
		sshHop    time.Duration
	)

	t.Run("rtt-floor", func(t *testing.T) {
		const reps = 50
		took := make(sample, 0, reps)
		for i := 0; i < reps; i++ {
			started := time.Now()
			facts, err := client.StatPaths([]string{noteName})
			elapsed := time.Since(started)
			if err != nil {
				t.Fatalf("stat %d of %d: %v", i+1, reps, err)
			}
			if len(facts) != 1 || !facts[0].Exists || facts[0].Dir {
				t.Fatalf("stat %d of %d answered %+v, and %s is a file that is there", i+1, reps, facts, noteName)
			}
			took = append(took, elapsed)
		}
		rttFloor = took.median()
		add(benchRow{
			what:   "round trip, smallest useful call (`StatPaths` on one path)",
			n:      reps,
			median: rttFloor,
			p95:    took.p95(),
			note:   "the floor under every other number here",
		})
		t.Logf("rtt floor: median %s, p95 %s over %d calls", span(rttFloor), span(took.p95()), reps)
	})

	t.Run("connection-setup", func(t *testing.T) {
		// WHAT ONE FRESH CONNECTION COSTS, measured twice, because two later
		// numbers are unreadable without it: `scp` pays a whole ssh connection
		// per copy while the wire's engine is already up, and a redial pays a
		// connection AND an engine boot before the first call can go out.
		const reps = 5
		bare := make(sample, 0, reps)
		for i := 0; i < reps; i++ {
			started := time.Now()
			hop := exec.Command("ssh", "-o", "BatchMode=yes", dest, "true")
			hop.Stderr = os.Stderr
			if err := hop.Run(); err != nil {
				t.Fatalf("plain ssh %d of %d: %v", i+1, reps, err)
			}
			bare = append(bare, time.Since(started))
		}
		sshHop = bare.median()
		add(benchRow{
			what:   "one fresh ssh connection (`ssh <host> true`)",
			n:      reps,
			median: bare.median(),
			p95:    bare.p95(),
			note:   "what every `scp` pays and an established wire does not",
		})

		dial := make(sample, 0, 3)
		for i := 0; i < 3; i++ {
			extra := &benchLink{dest: dest, workspace: workspace, key: key}
			started := time.Now()
			spare, err := remote.Roam(dest, remote.Hello{Workspace: workspace}, remote.Roaming{Dial: extra.spawn})
			elapsed := time.Since(started)
			if err != nil {
				t.Fatalf("cold dial %d of 3: %v", i+1, err)
			}
			_ = spare.Close()
			dial = append(dial, elapsed)
		}
		coldDial = dial.median()
		add(benchRow{
			what:   "cold dial — ssh, `aforge engine` starting over there, handshake",
			n:      3,
			median: coldDial,
			p95:    dial.p95(),
			note:   "paid once when a session opens, and again by every redial",
		})
		t.Logf("plain ssh: median %s; cold dial: median %s", span(bare.median()), span(coldDial))
	})

	t.Run("list-dir", func(t *testing.T) {
		const seededReps = 20
		took := make(sample, 0, seededReps)
		rows := 0
		for i := 0; i < seededReps; i++ {
			started := time.Now()
			listing, err := client.ListDir(".")
			elapsed := time.Since(started)
			if err != nil {
				t.Fatalf("listing the workspace, %d of %d: %v", i+1, seededReps, err)
			}
			if listing.Truncated {
				t.Fatalf("the seeded workspace came back truncated, which means it is not the small directory this benchmark was pointed at")
			}
			rows = len(listing.Entries)
			took = append(took, elapsed)
		}
		add(benchRow{
			what:   fmt.Sprintf("`ListDir` of the seeded workspace (%d rows)", rows),
			n:      seededReps,
			median: took.median(),
			p95:    took.p95(),
			note:   "a listing this small is a round trip and nothing else",
		})
		t.Logf("listdir %d rows: median %s, p95 %s", rows, span(took.median()), span(took.p95()))

		// The big directory is made here and removed before this subtest
		// returns, whatever happens: the far machine is somebody's laptop.
		remove := seedThousand(t, dest, workspace)
		defer remove()

		const bigReps = 10
		big := make(sample, 0, bigReps)
		for i := 0; i < bigReps; i++ {
			started := time.Now()
			listing, err := client.ListDir(thousandDir)
			elapsed := time.Since(started)
			if err != nil {
				t.Fatalf("listing the %d-entry directory, %d of %d: %v", thousandCount, i+1, bigReps, err)
			}
			if len(listing.Entries) != thousandCount || listing.Truncated {
				t.Fatalf("the %d-entry directory came back with %d rows (truncated: %v)", thousandCount, len(listing.Entries), listing.Truncated)
			}
			big = append(big, elapsed)
		}
		add(benchRow{
			what:   fmt.Sprintf("`ListDir` of a %d-entry directory", thousandCount),
			n:      bigReps,
			median: big.median(),
			p95:    big.p95(),
			note:   fmt.Sprintf("under the engine's %d-row ceiling, so nothing is cut", 2000),
		})
		t.Logf("listdir %d rows: median %s, p95 %s", thousandCount, span(big.median()), span(big.p95()))
	})

	t.Run("fetch-throughput", func(t *testing.T) {
		const reps = 10
		took := make(sample, 0, reps)
		for i := 0; i < reps; i++ {
			started := time.Now()
			file, err := client.FetchFile(fiveName)
			elapsed := time.Since(started)
			if err != nil {
				t.Fatalf("fetch %d of %d: %v", i+1, reps, err)
			}
			verify(t, file, fiveDigest, fiveSize)
			fetched = file
			took = append(took, elapsed)
		}
		wireFetch = took.median()
		add(benchRow{
			what:   fmt.Sprintf("`FetchFile` of %s (%d bytes)", fiveName, fiveSize),
			n:      reps,
			median: wireFetch,
			p95:    took.p95(),
			note:   fmt.Sprintf("%s over the wire, sha256 checked every time", rate(fiveSize, wireFetch)),
		})
		t.Logf("fetch %s: median %s (%s), p95 %s", fiveName, span(wireFetch), rate(fiveSize, wireFetch), span(took.p95()))
	})

	t.Run("scp-comparison", func(t *testing.T) {
		if wireFetch == 0 {
			t.Skip("the wire's own fetch did not measure, so there is nothing to compare against")
		}
		const reps = 10
		took := make(sample, 0, reps)
		into := t.TempDir()
		for i := 0; i < reps; i++ {
			landed := fmt.Sprintf("%s/copy-%d.bin", into, i)
			pull := exec.Command("scp", "-q", "-o", "BatchMode=yes", dest+":"+workspace+"/"+fiveName, landed)
			pull.Stderr = os.Stderr
			started := time.Now()
			err := pull.Run()
			elapsed := time.Since(started)
			if err != nil {
				t.Fatalf("scp %d of %d: %v", i+1, reps, err)
			}
			// The digest is checked OUTSIDE the timer, so scp is timed doing
			// scp's job and nothing else — and it is still checked, for the
			// same reason the wire's fetch is.
			bytes, err := os.ReadFile(landed)
			if err != nil {
				t.Fatalf("reading back scp copy %d: %v", i+1, err)
			}
			if got := digest(bytes); got != fiveDigest {
				t.Fatalf("scp copy %d came back as %s, and %s is the file", i+1, got, fiveDigest)
			}
			_ = os.Remove(landed)
			took = append(took, elapsed)
		}
		scpCopy = took.median()
		over := float64(wireFetch-scpCopy) / float64(scpCopy) * 100
		add(benchRow{
			what:   "`scp` of the same file to a local temp file",
			n:      reps,
			median: scpCopy,
			p95:    took.p95(),
			note:   fmt.Sprintf("%s; includes a local disk write the wire's fetch does not do", rate(fiveSize, scpCopy)),
		})
		// The row states the direction in words as well as in a sign, because
		// a bare `-22%` in a column called overhead is a number a reader has to
		// stop and decode.
		against := fmt.Sprintf("the wire costs %.0f%% more than `scp` — base64 and a JSON frame on the same ssh", over)
		if over < 0 {
			against = fmt.Sprintf("the wire is %.0f%% FASTER: `scp` opens a fresh ssh per copy, the engine is already connected", -over)
		}
		add(benchRow{
			what: "the wire against raw `scp`",
			note: against,
		})
		t.Logf("scp: median %s (%s); %s", span(scpCopy), rate(fiveSize, scpCopy), against)
	})

	t.Run("bytes-that-never-travel", func(t *testing.T) {
		if fetched.Hash == "" {
			t.Skip("nothing was fetched, so there is no cached copy to compare against")
		}
		const reps = 5
		// The dedup path as the surface walks it: ask the engine whether the
		// path is still there, hash what is already held, and compare. Nothing
		// but the question crosses.
		cached := make(sample, 0, reps)
		for i := 0; i < reps; i++ {
			started := time.Now()
			facts, err := client.StatPaths([]string{fiveName})
			if err != nil {
				t.Fatalf("stat %d of %d: %v", i+1, reps, err)
			}
			if len(facts) != 1 || !facts[0].Exists {
				t.Fatalf("stat %d of %d says %s is not there", i+1, reps, fiveName)
			}
			if got := digest(fetched.Bytes); got != fetched.Hash {
				t.Fatalf("the copy already held hashes to %s and the engine called it %s", got, fetched.Hash)
			}
			cached = append(cached, time.Since(started))
		}
		again := make(sample, 0, reps)
		for i := 0; i < reps; i++ {
			started := time.Now()
			file, err := client.FetchFile(fiveName)
			elapsed := time.Since(started)
			if err != nil {
				t.Fatalf("second fetch %d of %d: %v", i+1, reps, err)
			}
			verify(t, file, fiveDigest, fiveSize)
			again = append(again, elapsed)
		}
		saved := float64(again.median()) / float64(cached.median())
		add(benchRow{
			what:   "second open of a file already held — stat, then hash what is here",
			n:      reps,
			median: cached.median(),
			p95:    cached.p95(),
			note:   fmt.Sprintf("0 of the file's %d bytes cross", fiveSize),
		})
		add(benchRow{
			what:   "second open as a full fetch instead",
			n:      reps,
			median: again.median(),
			p95:    again.p95(),
			note:   fmt.Sprintf("%.0fx the work of the line above, for bytes already on this disk", saved),
		})
		t.Logf("dedup: median %s against a full fetch's %s (%.0fx)", span(cached.median()), span(again.median()), saved)
	})

	t.Run("survives-the-cut", func(t *testing.T) {
		before := pipes.started()
		for i := 0; i < 2; i++ {
			file, err := client.FetchFile(fiveName)
			if err != nil {
				t.Fatalf("fetch %d before the cut: %v", i+1, err)
			}
			verify(t, file, fiveDigest, fiveSize)
		}
		// THE CHILD IS ENDED BY ITS HANDLE. This harness spawned it, holds its
		// *exec.Cmd, and kills that one process — never a pattern, which on a
		// developer's machine would end the ssh session somebody is watching
		// this run through.
		if err := pipes.kill(); err != nil {
			t.Fatalf("could not end the ssh child: %v", err)
		}
		cut := time.Now()

		// A call made in the gap is REFUSED and not queued (internal/remote's
		// redial.go says so, and the sentence is `reconnecting to <host> — try
		// that again in a moment`), so recovery is measured by asking again
		// until one succeeds.
		var (
			back    time.Duration
			gap     time.Duration
			refused int
			last    error
		)
		deadline := time.Now().Add(remote.RoamWindow)
		for time.Now().Before(deadline) {
			asked := time.Now()
			file, err := client.FetchFile(fiveName)
			if err == nil {
				// THE TWO HALVES ARE REPORTED SEPARATELY, because they are two
				// different facts: how long the link took to come back, and how
				// long the work then took. A single number would hide which of
				// them a slow recovery was.
				gap, back = asked.Sub(cut), time.Since(cut)
				verify(t, file, fiveDigest, fiveSize)
				break
			}
			last = err
			refused++
			time.Sleep(100 * time.Millisecond)
		}
		if back == 0 {
			t.Fatalf("the connection did not come back inside %s; the last thing it said was %v", remote.RoamWindow, last)
		}
		if after := pipes.started(); after <= before {
			t.Fatalf("the fetch succeeded without a second ssh child ever being started (%d before, %d after), so nothing was actually redialled", before, after)
		}
		add(benchRow{
			what:   "the gap — ssh child killed, until a call is taken again",
			n:      1,
			median: gap,
			note:   fmt.Sprintf("%d calls refused in it with `reconnecting to %s — try that again in a moment`", refused, dest),
		})
		add(benchRow{
			what:   "kill to the next VERIFIED 5MB fetch",
			n:      1,
			median: back,
			note:   "the gap above plus the fetch itself, digest checked",
		})
		t.Logf("recovery: gap %s, verified fetch back at %s, %d calls refused, ssh children %d → %d", span(gap), span(back), refused, before, pipes.started())
	})

	t.Run("the-refusal-is-fast", func(t *testing.T) {
		const reps = 5
		took := make(sample, 0, reps)
		said := ""
		for i := 0; i < reps; i++ {
			started := time.Now()
			_, err := client.FetchFile(twentyName)
			elapsed := time.Since(started)
			if err == nil {
				t.Fatalf("%s crossed, and the engine's ceiling is 16MB", twentyName)
			}
			if !strings.Contains(err.Error(), "the most one file may cross this connection is 16MB") {
				t.Fatalf("the refusal for %s was %q, which is not the engine's ceiling sentence", twentyName, err)
			}
			said = err.Error()
			took = append(took, elapsed)
		}
		add(benchRow{
			what:   fmt.Sprintf("refusal of %s (over the 16MB ceiling)", twentyName),
			n:      reps,
			median: took.median(),
			p95:    took.p95(),
			note:   "the size is read off the far disk, so the no comes back at round-trip speed",
		})
		t.Logf("refusal: median %s — %q", span(took.median()), said)
	})

	// THE DOCUMENT IS ONLY REWRITTEN BY A RUN THAT FINISHED. A table half of
	// whose rows are missing would be a document that quietly under-reports,
	// which is the same failure as cherry-picking.
	printed := markdown(table)
	t.Logf("\n%s", printed)
	if t.Failed() {
		t.Logf("docs/remote-files-bench.md was NOT rewritten, because a measurement failed and half a table is worse than none")
		return
	}
	found := findings{
		dest: dest, workspace: workspace, link: link, welcome: welcome, rows: table,
		rtt: rttFloor, sshHop: sshHop, coldDial: coldDial, wireFetch: wireFetch, scpCopy: scpCopy,
	}
	if err := os.WriteFile(benchDoc, []byte(document(found)), 0o644); err != nil {
		t.Fatalf("could not write %s: %v", benchDoc, err)
	}
	t.Logf("wrote %s", benchDoc)
}

// ── the far machine's big directory ─────────────────────────────────────────

// seedThousand makes a directory of [thousandCount] empty files on the far
// machine in ONE ssh command, and hands back the removal.
//
// IT ONLY EVER TOUCHES ONE PATH, under the workspace it was pointed at and
// named after this harness. A benchmark that ran `rm -rf` against anything it
// had not itself made would be a benchmark nobody should run on their own
// laptop.
func seedThousand(t *testing.T, dest, workspace string) func() {
	t.Helper()
	dir := workspace + "/" + thousandDir
	quoted := shellWord(dir)
	build := fmt.Sprintf("rm -rf %s && mkdir -p %s && cd %s && i=1; while [ $i -le %d ]; do : > f$i.txt; i=$((i+1)); done && ls | wc -l",
		quoted, quoted, quoted, thousandCount)
	out, err := exec.Command("ssh", "-o", "BatchMode=yes", dest, build).CombinedOutput()
	if err != nil {
		t.Fatalf("could not make the %d-entry directory on %s: %v — %s", thousandCount, dest, err, strings.TrimSpace(string(out)))
	}
	if made := strings.TrimSpace(string(out)); made != fmt.Sprint(thousandCount) {
		t.Fatalf("the %d-entry directory came out holding %s files", thousandCount, made)
	}
	return func() {
		if out, err := exec.Command("ssh", "-o", "BatchMode=yes", dest, "rm -rf "+quoted).CombinedOutput(); err != nil {
			t.Errorf("could not remove %s from %s: %v — %s", dir, dest, err, strings.TrimSpace(string(out)))
		}
	}
}

// ── verifying ───────────────────────────────────────────────────────────────

// verify is the rule this file exists to keep: what came back is the file that
// was asked for, checked on every repetition and not once at the end.
func verify(t *testing.T, file remote.FetchedFile, want string, size int) {
	t.Helper()
	if len(file.Bytes) != size {
		t.Fatalf("%s came back as %d bytes and it is %d", file.Name, len(file.Bytes), size)
	}
	got := digest(file.Bytes)
	if got != want {
		t.Fatalf("%s hashes to %s and it is %s — the bytes on this link are not the bytes on that disk", file.Name, got, want)
	}
	// The engine's own digest travels with the file (wire.go's FetchedFile),
	// and a surface that caches by content trusts it, so it is checked too.
	if file.Hash != "" && file.Hash != want {
		t.Fatalf("the engine called %s %s and it hashes to %s", file.Name, file.Hash, want)
	}
}

func digest(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// ── the numbers ─────────────────────────────────────────────────────────────

// sample is one measurement's repetitions. THE MEDIAN OF THE WHOLE RUN IS WHAT
// IS REPORTED — never a best-of, which on a shared link measures how quiet the
// network happened to be for one instant.
type sample []time.Duration

func (s sample) sorted() []time.Duration {
	out := append([]time.Duration(nil), s...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func (s sample) median() time.Duration {
	if len(s) == 0 {
		return 0
	}
	ordered := s.sorted()
	middle := len(ordered) / 2
	if len(ordered)%2 == 1 {
		return ordered[middle]
	}
	return (ordered[middle-1] + ordered[middle]) / 2
}

// p95 is the nearest-rank ninety-fifth percentile, which on a sample of ten is
// the slowest of the ten and says so honestly rather than interpolating a
// number no repetition produced.
func (s sample) p95() time.Duration {
	if len(s) == 0 {
		return 0
	}
	ordered := s.sorted()
	rank := (95*len(ordered) + 99) / 100
	if rank > len(ordered) {
		rank = len(ordered)
	}
	return ordered[rank-1]
}

// span writes a duration the way a table wants it: milliseconds while that is
// readable, seconds once it is not.
func span(d time.Duration) string {
	switch {
	case d == 0:
		return ""
	case d < time.Millisecond:
		return fmt.Sprintf("%.2f ms", float64(d)/float64(time.Millisecond))
	case d < time.Second:
		return fmt.Sprintf("%.1f ms", float64(d)/float64(time.Millisecond))
	default:
		return fmt.Sprintf("%.2f s", d.Seconds())
	}
}

// rate is bytes over a duration as a person reads it.
func rate(size int, d time.Duration) string {
	if d <= 0 {
		return ""
	}
	return fmt.Sprintf("%.1f MB/s", float64(size)/(1<<20)/d.Seconds())
}

// ── the table ───────────────────────────────────────────────────────────────

// benchRow is one measured line. An empty median or p95 draws NOTHING, by the
// emptiness law: a derived row like the overhead ratio has no repetitions and a
// column of zeros would be inventing them.
type benchRow struct {
	what   string
	n      int
	median time.Duration
	p95    time.Duration
	note   string
}

func markdown(rows []benchRow) string {
	var out strings.Builder
	out.WriteString("| measurement | n | median | p95 | note |\n")
	out.WriteString("| --- | --- | --- | --- | --- |\n")
	for _, row := range rows {
		reps := ""
		if row.n > 0 {
			reps = fmt.Sprint(row.n)
		}
		fmt.Fprintf(&out, "| %s | %s | %s | %s | %s |\n", row.what, reps, span(row.median), span(row.p95), row.note)
	}
	return out.String()
}

// ── the document ────────────────────────────────────────────────────────────

// findings is one finished run: the table, and the few numbers the prose
// around it has to interpolate rather than repeat. A number that appears in two
// places drifts, so the sentences below are written FROM these fields and never
// beside them.
type findings struct {
	dest      string
	workspace string
	link      string
	welcome   remote.Welcome
	rows      []benchRow

	rtt       time.Duration
	sshHop    time.Duration
	coldDial  time.Duration
	wireFetch time.Duration
	scpCopy   time.Duration
}

// document is docs/remote-files-bench.md, written by the run that measured it.
//
// THE WHOLE PAGE IS GENERATED AND NOT ONLY THE TABLE, which is the only way the
// prose around a number cannot drift away from it: a rerun on a different pair
// of machines rewrites the sentences with its own numbers in them, and there is
// nowhere for a stale figure to survive.
func document(f findings) string {
	var out strings.Builder
	out.WriteString("# Remote files over ssh — what the wire costs\n\n")
	over := "a link this run was not told the name of — the round-trip row below is the only description of it there is"
	if strings.TrimSpace(f.link) != "" {
		over = f.link
	}
	fmt.Fprintf(&out, "Measured %s, %s → `%s`, over %s.\n\n", time.Now().Format("2006-01-02"), here(), f.dest, over)
	fmt.Fprintf(&out, "- **surface** — this machine, %s/%s, running the local half in-process: `internal/remote`'s client, dialled exactly as `aforge chat --host` dials it.\n", runtime.GOOS, runtime.GOARCH)
	fmt.Fprintf(&out, "- **engine** — `%s`, %s, running `aforge engine --no-host --workspace '%s'` under `ssh -T`, which is the shape that serves the pipe and ends with it. Workspace as the engine resolved it: `%s`.\n", f.dest, far(f.dest), f.workspace, f.welcome.Workspace)
	fmt.Fprintf(&out, "- **wire** — v%d on both ends. The handshake refuses a mismatch at the door, so every number below is one protocol talking to itself.\n\n", remote.Version)
	out.WriteString("Reproduce, with a seeded workspace on the far machine (`note.txt`, `sub/inner.txt`, `five-mb.bin`, `twenty-mb.bin`) and an aforge from this branch on its PATH:\n\n")
	fmt.Fprintf(&out, "```\n%s\n```\n\n", reproduce)
	out.WriteString("Every figure is the **median of the whole run** — no best-of, no discarded repetitions — and every fetch's sha256 was checked against the seeded file before its time was counted.\n\n")
	out.WriteString(markdown(f.rows))

	out.WriteString("\n## What the rows mean\n\n")
	fmt.Fprintf(&out, "**The round trip is the floor.** Every call is one frame out and one frame back, so nothing here is faster than a stat: %s. A listing of a small directory costs the same, because it IS the same round trip; a thousand entries costs the round trip plus the JSON.\n\n", span(f.rtt))
	out.WriteString(overhead(f))
	fmt.Fprintf(&out, "**The dedup path is the real speed-up, and it is a hash comparison.** A file already held is not fetched again: the surface asks whether the path is still there and compares the digest it already has (`FetchedFile.Hash`, the CAS's own key). None of the file's %d bytes cross — the difference between those two rows is the difference between a question and a transfer.\n\n", fiveSize)
	fmt.Fprintf(&out, "**The transfer survives the cut.** The measurement is literal: two 5MB fetches complete, this harness kills its own ssh child by its process handle, and the clock runs until a fetch succeeds again AND verifies. In the gap calls are refused rather than queued — `reconnecting to %s — try that again in a moment` — and internal/remote's reader goroutine opens the next ssh itself. The gap is not mysterious once the cold-dial row is beside it: one second of first backoff (redial.go's `firstBackoff`), then a whole cold dial (%s here — ssh, plus `aforge engine` starting over there), and the fetch that follows takes what a fetch takes. A persistent engine host, which is the door's default shape, replaces the boot with a socket attach and is the faster of the two.\n\n", f.dest, span(f.coldDial))
	fmt.Fprintf(&out, "**The refusal is fast.** A file over the 16MB ceiling is refused from its SIZE on the far disk, before a byte is read, so the no comes back at round-trip speed rather than after 20MB of transfer: `engine: twenty-mb.bin is 20MB and the most one file may cross this connection is 16MB`.\n\n")

	out.WriteString("## Honest asymmetries in these numbers\n\n")
	fmt.Fprintf(&out, "- `scp` writes to a local file and the wire's fetch holds bytes in memory, so the `scp` row carries a disk write the fetch does not. If anything that is kind to `scp`.\n")
	fmt.Fprintf(&out, "- Each `scp` includes its own connection setup (%s median for a bare `ssh <host> true`); the wire's fetches are timed on an engine that is already up, and the connection they amortise is the cold dial row. Both are what the two tools actually cost a person, which is why they are compared as they are rather than adjusted.\n", span(f.sshHop))
	fmt.Fprintf(&out, "- The far engine is run with `--no-host` so that nothing but ssh, frames and an engine is in the middle, and so that no session host is left running on somebody else's laptop. The door's default attaches to one.\n")
	fmt.Fprintf(&out, "- The thousand-entry directory is made and removed by the run itself, inside the workspace; the engine's own session journal is written where that machine keeps journals, as it is for any `--host` session.\n")
	fmt.Fprintf(&out, "- macOS answering a Linux terminal often prints a locale warning on ssh's stderr. It is noise and changes nothing measured here.\n")
	out.WriteString(stalls(f.rows))
	return out.String()
}

// stallRatio is how far a p95 has to stand off its median before the document
// says so out loud. A link that is merely uneven moves a p95 by a factor of two
// or three; an order of magnitude is not unevenness, it is one repetition that
// STOPPED, and a reader deserves to be told which it was.
const stallRatio = 20

// stalls is the paragraph a run writes about its own worst repetitions.
//
// THE OUTLIER STAYS IN THE TABLE AND IS EXPLAINED INSTEAD. Dropping it would
// make a nicer page and a dishonest one — the p95 column exists precisely so
// that a stall cannot hide behind a median — so a run that had one says what
// happened and points at the median as the number to read. A clean run writes
// nothing here, by the emptiness law.
func stalls(rows []benchRow) string {
	var said []string
	for _, row := range rows {
		if row.n < 2 || row.median <= 0 || row.p95 < stallRatio*row.median {
			continue
		}
		said = append(said, fmt.Sprintf("- One repetition of **%s** took %s against a median of %s. That is a link that stalled, not a cost of the thing being measured; it is left in the table because a benchmark that removes its worst repetition is a benchmark that has stopped measuring the link it ran on. Read the median.\n", row.what, span(row.p95), span(row.median)))
	}
	if len(said) == 0 {
		return ""
	}
	return "\n## What stalled during this run\n\n" + strings.Join(said, "")
}

// overhead is the paragraph comparing the wire to raw `scp`, and it is written
// from the numbers rather than around an assumption about which way they came
// out — a benchmark whose prose only survives one outcome is a benchmark that
// has decided its answer in advance.
func overhead(f findings) string {
	buys := "What the wire buys, which `scp` has no way to offer: the transfer rides the conversation's OWN connection, so it redials itself when the link drops; the path was authorized by the engine under the two-roots law rather than by whatever that account happens to be able to read; and nothing asked the person for a second credential — no second authentication, no second host key, no second window."
	if f.scpCopy <= 0 || f.wireFetch <= 0 {
		return "**The fetch is bytes over the same ssh.** " + buys + "\n\n"
	}
	ratio := float64(f.wireFetch-f.scpCopy) / float64(f.scpCopy) * 100
	if f.wireFetch < f.scpCopy {
		return fmt.Sprintf("**The wire is not the slow way to move a file.** Fetching the 5MB file over the wire — frames, base64, one JSON round trip — came back **%.0f%% faster** than `scp` of the same file on the same link (%s against %s). That is not the encoding being free; it is one whole ssh connection being expensive. `scp` opens one per copy, and the wire's engine is already connected. At a large enough file the encoding would dominate again and the ordering would swap; what this run says is that at the sizes a person actually clicks on, the encoding is not the term that matters. %s\n\n",
			-ratio, span(f.wireFetch), span(f.scpCopy), buys)
	}
	return fmt.Sprintf("**The fetch costs %.0f%% more than raw `scp`** on the same link (%s against %s), and that difference is exactly what it looks like: base64 and a JSON frame on top of the same ssh transport. %s\n\n",
		ratio, span(f.wireFetch), span(f.scpCopy), buys)
}

// here names this machine for the document's header line.
func here() string {
	name, err := os.Hostname()
	if err != nil || strings.TrimSpace(name) == "" {
		return "this machine"
	}
	return "`" + name + "`"
}

// far asks the other machine what it is, which is one read-only command and the
// only thing that makes the header line true rather than assumed.
func far(dest string) string {
	out, err := exec.Command("ssh", "-o", "BatchMode=yes", dest, "uname -sm").Output()
	said := strings.TrimSpace(string(out))
	if err != nil || said == "" {
		return "another machine"
	}
	return said
}
