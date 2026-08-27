# Remote access — how to try it, and what is not finished

Self-contained. Everything a person (or an agent) needs to exercise this wave by hand,
what each thing should look like on screen, and an honest list of what still needs work.

Nothing here assumes you read the other documents. If you want the *why*, that is
`docs/REMOTE.md` (architecture), `docs/remote-access-design.md` (product), and
`docs/remote-access-plan.md` (how it was built).

---

## 0. The one-paragraph summary

A conversation now lives on the machine that owns the work, and your terminal **attaches**
to it. Close the lid mid-answer, lose wifi, kill the terminal — the turn keeps running over
there and you land back in it. Files you drop into the chat travel to that machine. And a
machine with no inbound ssh can be reached by a name and a pairing code instead — that last
part is code-complete but has no service deployed, so it fails with a clear sentence today.

---

## 1. Build it

```sh
make build          # → bin/aforge
```

Add `bin/aforge` to the PATH of **both** machines you test with. The two halves must speak
the same protocol version: that version is checked at the door and a mismatch is refused with

```
<dest> runs a different version of aforge than this machine does — update the older one so both ends speak the same protocol
```

---

## 2. The automated proof, first

Two suites matter most here.

```sh
go test ./internal/remote/ ./internal/enginehost/ ./internal/pair/ ./internal/relay/ ./internal/furrow/
make test-remote        # the two-machine container harness — needs a docker daemon
```

`make test-remote` starts three containers (a scripted model, an engine with sshd, a
surface) that share **no path, no home directory and no credential**, and runs the real
binary across them. It takes about a minute. **It skips green wherever docker is absent** —
so if it finishes in under a second, it skipped; check `docker info` and start `dockerd`.

The binaries it copies in are built **for this machine's architecture**, because docker
starts the containers on it. If you see

```
setsid: can't execute '/usr/local/bin/modelstub': Exec format error
```

you are on a build of this harness that pinned `GOARCH=amd64`; it is fixed, and
`AFORGE_E2E_ARCH` overrides the choice if you are ever running containers of another
architecture on purpose.

Last full runs: **4 pass, 1 skip, 60s** on darwin/arm64 → linux/amd64 containers, and
**46s** on linux/arm64.

| Scenario | What it proves |
| --- | --- |
| `HandshakeAndOneTurn` | ssh → engine boot → real turn → reply; journal on the engine only |
| `ThePathLaw` | the engine's paths win; a surface-only path is refused by name |
| `TheLinkDiesMidTurn` | ssh killed mid-reply → redial → **the reply completes** |
| `TwoSurfacesAtOnce` | a second window attaches mid-stream and gets the tail of a turn it did not start |
| `AnAttachmentLandsOverThere` | **SKIPPED** — `/attach` is a slash command and `--once` has no tray; needs a pty driver |

Also relevant, and expected to fail for reasons that predate this branch: `internal/plan`,
four `internal/swepro` packages, `cmd/aforge TestHarnessEntriesFromStore`, `internal/tui
TestSettingsSheetIsOneCalmColumnAtEveryWidth`, and `internal/session
TestTheLegacyWorktreeStaysUnderTheRepository`. `cmd/aforge`'s two `TestTick*` need
`OPENROUTER_API_KEY` in the environment. See CLAUDE.md.

---

## 3. By hand, over ssh

You need a second machine you can already `ssh` into, with `aforge` on the PATH that a
**non-login** ssh command sees (test with `ssh devbox aforge version`).

### 3.0 Without a second machine: `--host localhost`

Everything below except the shared-disk law can be exercised against your own machine, and
this is how the wave was checked by hand. It is a real ssh pipe and a real second process;
what it cannot prove is that the two halves do not share a filesystem — over `localhost`
they do.

```sh
ssh localhost true                       # the only prerequisite
cp bin/aforge <somewhere on the PATH a non-login ssh sees>
ssh localhost aforge version             # both ends must speak the same protocol version
aforge chat --host localhost:code/app --model deepseek/deepseek-v4-flash
```

Driving the real surface without a keyboard, which is what an agent has to do:

```sh
tmux new-session -d -s dx -x 200 -y 50 \
  "bin/aforge chat --host localhost:code/app --session new --model deepseek/deepseek-v4-flash"
tmux send-keys -t dx "use the bash tool: sleep 45 && echo done" Enter
tmux capture-pane -p -t dx | tail -4          # read the screen back
```

To drop the link on purpose, kill the ssh child this session started — and kill it **by
pid**, because a pattern wide enough to match `aforge engine` also matches the shell you
typed it in:

```sh
kill -9 $(pgrep -f "^ssh -T .* localhost aforge engine" | head -1)
```

**Verified this way, on this tree:** a turn mid-`bash` survives the kill and completes
(`worked 1m03s · 1 tool call`, the tool's output on screen); the redial lands back in the
**same** conversation, not a new one; the `connection` segment reads
`reconnecting to localhost — trying for up to 5 minutes` for exactly as long as the link is
down and vanishes on its own; `/attach` puts the bytes in the far session's
`attachments/20260824-203246-<hash>-<name>` and the model reads the file by path; a message
submitted during the gap is refused visibly with
`submit failed: reconnecting to localhost — try that again in a moment` rather than lost.

### 3a. The basic connection

```sh
aforge chat --host devbox                 # the far machine's home directory
aforge chat --host devbox:code/app        # relative to the far machine's home
aforge chat --host devbox:/srv/code/app   # absolute, over there
aforge resume --host devbox               # the picker, on that machine's conversations
```

**What you should see.** The machine appears as part of the place — `devbox:app` in the
status line's place segment, `devbox:/srv/code/app` in `/status`, and `devbox · porting the
parser` in the legend under the input box. After its first measurement, the connection
segment reads like `devbox · 3ms`. There is **no** "connected" badge and no icon.

**What to check:** ask it to run `pwd` and read a file. Every path it names should be the far
machine's. `~` collapsing runs against *your* home, so expect full paths.

### 3b. The headline: a turn that survives a dropped link

1. Ask for something slow ("read every file in this repo and summarise the architecture").
2. While it is answering, **kill the connection** — close the laptop lid, drop wifi, or from
   another terminal `pkill -x ssh`.
3. Watch the status line. A `connection` segment appears:

   ```
   reconnecting to devbox — trying for up to 5 minutes
   ```

4. It should reconnect by itself and **the answer should continue**, not restart.

If the far machine is running a bare `aforge engine` on a pipe with no session host, the turn
does **not** survive, and the surface says so once rather than pretending:

```
devbox does not keep a turn running while nothing is attached, so the turn that was in flight did not survive the drop
```

If it cannot get back at all, after five minutes:

```
the connection to devbox is gone — run the same command to pick the conversation back up
```

### 3c. Questions that waited

1. Connect, ask for something that needs your approval (a command it must ask about).
2. When the card appears, **close the window without answering**.
3. Reconnect with the same command.

The card should come back, with a line above it saying how long it waited:

```
this question has been waiting 4 hours
```

Under a minute it says nothing at all (the emptiness law). A question type this build does
not know how to draw is skipped and left waiting rather than half-rendered.

### 3d. Two windows on one conversation

Open `aforge chat --host devbox` in two terminals. The second should say

```
another window is on this conversation — typing is here now
```

**The keyboard follows the newest window.** The second terminal keeps its composer; the
first one's box is replaced by a single dim line:

```
typing from <machine> now                                      enter takes it back
```

A window on the SAME machine reads `typing from another window now`. Start a turn in the
second; the first should draw it live — the message above the reply, the reply token by
token — with no keystroke on it. Press `enter` on the first: it takes the keyboard in one
round trip, its unsent draft is exactly where it was, and the second becomes the watcher in
the same instant. Close one and the other gets the keyboard back with no line at all.

Typing at a watcher does nothing; two spaces still open home. A message that races a
hand-over is answered rather than dropped:

```
the keyboard is on <machine> right now — press enter here to take it back
```

Driving it without a keyboard, which is what an agent has to do:

```sh
for s in w1 w2; do
  tmux new-session -d -s $s -x 140 -y 40 "aforge chat --host localhost:code/app --model <m>"
  sleep 11
done
tmux capture-pane -p -t w1 | tail -2      # the watcher's line
tmux send-keys -t w2 "say hello" Enter; sleep 20
tmux capture-pane -p -t w1 | head -12     # the turn w1 did not start, drawn live
tmux send-keys -t w1 Enter; sleep 3       # w1 takes the keyboard; w2 becomes the watcher
```

**Verified this way, on this tree** (`--host localhost`, two tmux surfaces, one workspace):
the arriving window's notice, the older window's line, a draft left untouched across two
hand-overs, a real turn drawn in the watching window with its question above it, `enter`
swapping the roles in both directions, and the survivor getting the keyboard back when the
other window closed.

### 3e. Files

```
/attach server.log        # tab completes, paths are LOCAL to the machine you are sitting at
```

The file's bytes travel and land in the far machine's session folder under `attachments/`.
The model is told the **path**, not the contents — so it will `read` it if it needs it, which
keeps a 4MB CSV out of the context window.

Refusals to try:

```
/attach                                  → /attach takes a path · try /attach server.log
/attach /some/directory                  → <name> is a folder · attach a file
/attach <a 20MB file>                    → <name> is 17MB and over the 16MB file limit
```

Ceilings: **16MB per file, 32MB per message**. They apply only over a connection — a local
`/attach` names a path the engine can already see and copies nothing.

### 3f. Files, links, and the browse page

The other direction: files on the far machine, opened from here. All of this was driven by
hand over `--host localhost` on this tree, and every command below is one an agent can run
without a keyboard.

**Set up a session and make it write a file.**

```sh
tmux new-session -d -s fx -x 200 -y 50 \
  "bin/aforge chat --host localhost:code/app --session new --model deepseek/deepseek-v4-flash"
tmux send-keys -t fx "write a file notes/hello.txt containing hello, then say where you put it" Enter
sleep 30
tmux capture-pane -p -t fx | tail -6
```

**1. The path in the reply is an OSC 8 link, and it points at the door.** `capture-pane -pe`
keeps the escape sequences, which is the only way to see a hyperlink from outside a
terminal:

```sh
tmux capture-pane -pe -t fx | grep -o ']8;;http://127.0.0.1:[0-9]*/o/[a-f0-9]*' | sort -u
```

Expect `]8;;http://127.0.0.1:<port>/o/<32 hex>`. **The link appears only after the ENGINE
confirmed the file** — a path the model merely names, or one that does not exist over
there, has no anchor at all. A path inside a code span that was never confirmed stays
ordinary inline code.

**2. The door fetches and opens the named local mirror, and refuses everything else identically.**

```sh
URL=$(tmux capture-pane -pe -t fx | grep -o 'http://127.0.0.1:[0-9]*/o/[a-f0-9]*' | head -1)
curl -s "$URL"; echo                                   # says which file opened from which machine
curl -s -o /dev/null -w '%{http_code}\n' "${URL%/*}/0000000000000000000000000000dead"   # 404
```

A wrong token and an unknown id answer the **same** 404 with the same body: a distinct
status for "real id, wrong token" would confirm existence to somebody guessing.

**3. `/files` opens the browse page.** It is written into the transcript as well as opened,
which is what makes it testable:

```sh
tmux send-keys -t fx "/files" Enter; sleep 2
BROWSE=$(tmux capture-pane -p -t fx | grep -o 'http://127.0.0.1:[0-9]*/browse/[a-f0-9]*' | tail -1)
PORT=$(printf '%s' "$BROWSE" | sed 's|.*127.0.0.1:\([0-9]*\)/.*|\1|')
TOKEN=${BROWSE##*/}
curl -s "http://127.0.0.1:$PORT/api/$TOKEN/ls?path=." | head -c 400; echo
curl -s -o /dev/null -w '%{http_code}\n' "http://127.0.0.1:$PORT/api/deadbeefdeadbeefdeadbeefdeadbeef/ls?path=."   # 404
```

The listing is JSON: `path` as the engine resolved it, `entries` with `dir`, `size`,
`mtime`, `mime`, and `truncated` past 2000 rows. In the page itself the rows are
directories first then files, sizes and times in one column each, a click walks into a
folder or opens a file in a new tab, and a row over 16MB is drawn **plain** with `too big
to cross` beside it rather than as a link that would fail.

**4. An upload lands in `attachments/` and opens no turn.**

```sh
printf 'up\n' > /tmp/up2.txt
curl -s -F file=@/tmp/up2.txt "http://127.0.0.1:$PORT/api/$TOKEN/put"; echo
tmux capture-pane -p -t fx | tail -3      # UNCHANGED: no message, no turn, no event
```

The answer is `{"landed":"…/attachments/20260824-215842-d9e4ec72-up2.txt"}` — the engine's
own path, with the arrival stamp and a digest in front of the name, in the session's own
folder. Confirm with `ls` on that directory. This is the whole of the deposit lane: bytes
kept, nothing said. `/attach` is the same landing place *with* a person's sentence on it.

**5. The copies on this machine: a CAS blob and a hardlinked mirror.**

```sh
ls ~/.aforge/v3/remote/cas/*/ | head               # content-addressed: <first two hex>/<sha256>
find ~/.aforge/v3/remote/mirror -type f | head     # mirror/<host>/<the engine's own path>
stat -c '%h %n' $(find ~/.aforge/v3/remote/mirror -type f | head -1)   # 2 links = same inode as the blob
```

A file the model **wrote** during a turn is fetched speculatively at 2MB or under, before
anybody clicks — so `ls` the CAS immediately after a `write` and the blob is already there.
Over that size nothing is prefetched and the click pays for the fetch.

**6. The ceilings and the refusals, in the engine's own words.**

```
/files a-20mb-file.bin   → engine: a-20mb-file.bin is 20MB and the most one file may cross this connection is 16MB
/files /etc/passwd       → engine: /etc/passwd is outside this conversation's workspace and its own folder, and nothing outside those two crosses this connection
/files ../../secrets     → that is not a path inside the workspace on that machine
```

On a **local** session `/files <path>` refuses with
`that form of /files is for a session on another machine — this one is local, so the paths
in it are already yours to open`.

**7. Everything dies with the window.** Quit the session and both addresses stop
resolving:

```sh
tmux send-keys -t fx "/quit" Enter; sleep 2
curl -s -o /dev/null -w '%{http_code}\n' "$URL"      # connection refused: the listener is gone
```

**Verified this way, on this tree:** the link appears only after StatPaths confirms;
`/o/<id>` opens the fetched local mirror and any other id 404s; `/files` opens the browse page and lists
dirs-first with sizes and times; a `curl -F file=@…` deposit lands in `attachments/` and
adds nothing to the transcript; a written file is in the CAS before the click; the mirror
is a hardlink to the blob; the two-roots and 16MB refusals are the engine's sentences,
unchanged.

---

## 4. By hand, without ssh (`--at`) — expect it to refuse

This is the pairing road. **No relay service is deployed**, so the honest outcome today is a
clear refusal. Run it anyway; the refusals are the deliverable.

```sh
aforge devices                     # who may reach this machine, and its name
aforge serve                       # be reachable (needs AFORGE_RELAY set)
aforge chat --at otter-lamp-42     # reach a machine by its name
```

`aforge devices` on a fresh machine prints something like:

```
this machine is reachable as rudder-basil-62
its key is kept in a file on this machine, readable only by you (~/.aforge/v3/remote/device.key)

no devices are paired with this machine.
```

The four refusals, each naming what is actually wrong:

```
no relay is set up on this machine, so --at has nowhere to look for otter-lamp-42 — set AFORGE_RELAY to a relay's address, or reach that machine with --host over ssh
the relay at https://relay.example.com cannot be reached from here — check this machine's network, or reach that machine with --host over ssh
otter-lamp-42 is not connected to the relay right now — run `aforge serve` on that machine
this device is not paired with otter-lamp-42 — run `aforge serve` on that machine, then run this command again and type the code it shows
```

### Running a relay yourself, to see the whole flow

```sh
go run ./cmd/relay --listen :8787          # machine C, or the same box
export AFORGE_RELAY=http://localhost:8787  # on BOTH the engine and the surface
aforge serve                               # on the engine machine
```

`aforge serve` prints:

```
  this machine is reachable as  otter-lamp-42
  pair a new device with code   715 302   (valid 10 minutes)
```

Then from the other machine, `aforge chat --at otter-lamp-42` walks the pairing:

```
pairing with otter-lamp-42
a paired device is a key to that machine: it opens conversations there, runs whatever that machine allows to run, and spends that machine's model key. it is the same weight as an ssh key.
enter the code shown on otter-lamp-42: ______
paired. this device is now a key to otter-lamp-42.
```

After that, `--at otter-lamp-42` opens without a code. `aforge devices revoke <name>` takes
it back, and revocation is **always the engine machine's decision** — no surface can do it
down the wire.

---

## 5. What still needs work

Ranked. Nothing here is hidden in a comment; it is all real.

### Needs a decision or a deployment
1. **No relay is deployed and there is no default address.** `--at` cannot work for a real
   person until ops runs one and a default lands in `internal/pair/service.go`'s `Relay()`.
2. **The NAT claim is untested.** The relay exists so a machine with no inbound reachability
   can still be reached — and the container harness cannot prove that, because containers on
   a bridge *can* reach each other. This needs a container with egress but no inbound path.
   **It is the one claim in this wave still resting on design rather than a test.**
3. **`workspace_restore` writes files** and, under a blanket allow, would run unasked. It
   previews unless `confirm:true` and furrow seals state first, so it is undoable — but it is
   a candidate for `internal/approval`'s always-asks table. Not decided.

### Known-absent, deliberately, and documented as such
4. **No OS keychain, no Touch ID.** The device key is a file at
   `~/.aforge/v3/remote/device.key`, mode 0600 — exactly the exposure of an ssh key with no
   passphrase. `pair.Keeper` is the seam; filling it is Mac-only cgo work, with Linux and
   Windows each wanting their own.
5. **Traffic analysis is not defended.** The relay cannot read anything, but record sizes and
   timing are visible to it.
6. **Building a sub-harness and adaptive runs stay off over a connection.** Held questions did
   *not* fix this, contrary to the original plan: those cards are announced on subscriptions
   this protocol has no door for, so they never cross at all. Opening them means putting the
   card and its answer on the wire — a lane of its own.

### Gaps a tester will actually hit
7. **`/attach` has no automated two-machine coverage** (scenario 4 skips). It is covered by
   unit tests on both halves, and it has now been driven by hand through the real surface
   over a real connection (§3.0: tray, wire, far-side `attachments/`, the model reading the
   path) — but that run was `--host localhost`, where the two halves share a disk, so
   nothing yet proves the bytes had to travel. **A pty or tmux driver inside the container
   harness would close this** — `internal/e2e/tmux_test.go` is prior art, and §3.0's tmux
   recipe is the driving half of it.
8. **`aforge serve` spawns `aforge engine` as a child, one per connection**, and does not use
   the session host. So over `--at` the *persistence* story is weaker than over `--host`.
   Worth confirming what `Welcome.Persistent` reports there before trusting it.
9. **`filippo.io/cpace` is vendored** at `internal/pair/cpace` — ours now, bug included. The
   header says what reading it turned up. Diff against upstream before trusting a change.
10. **Idle policy is untested in anger.** A conversation with no surface, no turn and no
    waiting question closes after 30 minutes; the host exits 2 minutes after its last one.
    Nobody has watched this happen over a long session.
11. **An auxiliary call hits the model endpoint with a hardcoded
    `model="nex-agi/nex-n2-mini"`** regardless of the configured model. Harmless against the
    test stub, and **not caused by this branch** — but it means some internal call ignores the
    session's model, and somebody should find out which.

### Housekeeping
12. ~~`internal/pair/zz_probe_scratch_test.go` — scratch probe file~~ — deleted in this branch.
13. `gofmt -l` flags `cmd/aforge/chat.go`, unformatted on a clean tree and untouched here.
14. ~~A refused handshake printed its sentence **twice** — once by the engine's own stderr,
    which ssh puts on the person's terminal, and once by the surface reading the refusal off
    the wire.~~ Fixed on the merge: `remote.Refusal` is typed, and the engine door exits
    quietly on one because the reason has already been delivered
    (`cmd/aforge/engine.go`'s `quietRefusal`). The exit code is still 1.
15. ~~`make test-remote` built its container binaries for `amd64` unconditionally~~ — fixed;
    it follows the host, which is what docker starts the containers as.

---

## 6. Where the code is

| Path | What |
| --- | --- |
| `internal/remote/wire.go` | the protocol — frames, methods, version 2 |
| `internal/remote/server.go`, `held.go` | the engine half; `Session` is the conversation, `server` is one connection |
| `internal/remote/client.go`, `redial.go` | the surface half and the roaming loop |
| `internal/remote/file.go`, `image.go` | attachments, both directions, plus ListDir, StatPaths and Deposit.File |
| `internal/filedoor/` | the loopback door: `/o/<id>` opens, `/f/<id>` serves bytes, plus the browse page, `/api/<token>/ls`, `/put` |
| `internal/tui3/remotefiles.go`, `remoteopen.go` | the surface half: confirmed links, prefetch, the CAS and the mirror |
| `internal/remote/loopback.go` | a real client against a real server, in memory — start here when testing |
| `internal/enginehost/` | the session host: one per workspace, on a unix socket |
| `internal/pair/`, `internal/relay/`, `cmd/relay/` | pairing, the blind relay, the deployable |
| `internal/furrow/` | the four `workspace_*` verbs, absent unless furrow is installed |
| `internal/tui3/hostlink.go` | the status segment, the notice, held-card replay |
| `cmd/aforge/chatv3_host.go`, `chatv3_at.go`, `engine.go` | the three doors |
| `internal/e2e/remote_test.go`, `test/remote/` | the two-machine harness and its stub model |

The manual pages the chat itself reads: `staying-on-that-machine.md`,
`when-the-connection-drops.md`, `attaching-files.md`,
`reaching-this-machine-without-ssh.md`, `forking-and-syncing-a-workspace.md`,
`opening-files-from-that-machine.md`, and the rewritten `running-on-another-machine.md`.
