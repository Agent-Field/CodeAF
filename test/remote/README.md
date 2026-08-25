# The two-machine harness

`internal/e2e/remote_test.go` drives `aforge chat --host` across **three containers on one
bridge network** — a surface machine, an engine machine, and a scripted model endpoint —
so the remote lane is proved against two genuinely different filesystems.

This directory holds the one thing that harness needs and could not borrow: `stub/`, a
deterministic stand-in for the model endpoint.

## What this proves that the loopback tests cannot

`internal/remote/loopback.go` drives the real client against the real server over
`net.Pipe`. That is the right test of the **protocol** — fast, deterministic, and if a
change ever makes the wire care what it is riding, that file stops compiling.

What it cannot prove is the product's actual claim. Every honesty law in
`docs/REMOTE.md` Decision 6 — *the engine machine is the authority, always* — is about a
path meaning different things on each side. On one host with one home directory, every
path in the test exists on both ends, so **a surface that resolved a path locally would
pass by coincidence**. This harness makes the coincidence impossible:

| | engine machine | surface machine |
|---|---|---|
| home directory | `/root` | `/opt/surface-home` (moved in `/etc/passwd`, not just `$HOME`) |
| aforge home | `/var/lib/aforge-engine` | `/opt/surface-home/.aforge` |
| workspace | `/srv/engine-work/project` | *absent* |
| a directory of its own | *absent* | `/opt/surface-home/laptop-work` |
| API key | yes | **none** |

Not one of those paths exists on both machines, and the test asserts that before it
asserts anything about them.

## The scenarios

| Scenario | What it proves |
|---|---|
| **HandshakeAndOneTurn** | ssh opens, the engine boots over there, the handshake completes, a real turn runs, the reply lands on the surface's stdout — and the journal is on the engine's disk while the surface holds neither a conversation nor a credential. |
| **ThePathLaw** | a tool's `pwd` names the **engine's** workspace (a path the surface does not have); a workspace that exists only on the surface is refused by the engine, naming the path; a *relative* workspace resolves against the **engine's** home; `read` on a surface-only file fails while the same tool on an engine file works. |
| **TheLinkDiesMidTurn** | the ssh client is killed mid-reply. Against the persistent engine host the redial rejoins the same conversation and the reply completes; against a one-shot pipe engine the turn is over and the surface *says so* in `internal/remote`'s own words. Which of the two is asserted is read off the engine's disk, so the harness follows the build instead of assuming it. |
| **AnAttachmentLandsOverThere** | **skipped** — see below. |
| **TwoSurfacesAtOnce** | two windows name one conversation; the second attaches while the first's turn is still streaming and is handed the rest of it, and one journal on the engine holds both messages. |

### Why the attachment scenario is skipped

`/attach` is a **surface slash command** (`internal/tui3/commands.go`, `attach.go`), and
the only headless door this harness has is `--once`, which submits one message and has no
tray to put a chip on. Proving an attachment crosses therefore needs a terminal driver — a
tmux or pty harness — rather than another assertion here. The scenario checks its own
precondition and then skips with that sentence, so the day a headless door exists this
becomes a real scenario rather than a rediscovery.

## The model stub

`stub/main.go` speaks OpenRouter's OpenAI-compatible dialect exactly as
`internal/provider` writes and reads it (`<base>/chat/completions` with SSE,
`<base>/models?output_modalities=all`) and answers from a script keyed off markers in the
person's own message:

| marker | what it answers |
|---|---|
| `PROBE-ECHO` | one reply, no tools |
| `PROBE-PWD` | calls `bash` with `pwd`, then quotes the answer back |
| `PROBE-READ <path>` | calls `read` on the path, then quotes the answer back |
| `PROBE-SLOW` | a reply spread over about twelve seconds |

**Quoting the tool output back is the whole trick.** `--once` prints the model's reply on
stdout and the tool's own output nowhere at all, so a scenario that wants to assert on
what a tool *saw* has to have the reply carry it. A real model would summarize; this one
echoes verbatim, which is what makes an assertion about an absolute path possible.

The stub means the harness needs **no API key**, spends **no money** (its catalog prices
are zero), and cannot go red because a model chose to paraphrase.

## Running it

```sh
dockerd >/tmp/dockerd.log 2>&1 &        # if the daemon is not already up
go test -tags docker_e2e -count=1 -run TestRemoteTwoMachines -v -timeout 20m ./internal/e2e/
```

About 65 seconds end to end on a warm image cache. The build tag keeps it out of
`go test ./...` entirely, and the test **skips, never fails**, when docker is missing, the
daemon will not answer, or the image will not pull. Containers and the network are named
`aforge-e2e-*` and removed in `t.Cleanup`, so an interrupted run leaves nothing behind.

Both binaries are built **on the host** with `CGO_ENABLED=0` and copied in with
`docker cp`. That is why the base image can be `alpine:3.20`: a cgo build would link
against glibc and die on musl. This tree's only sqlite is `modernc.org`'s, which is pure
Go, so the flag holds — verified, not assumed.

## The alpine/TLS workaround

Inside the containers, `apk` is pointed at plain http before anything is installed:

```sh
sed -i "s#https://#http://#g" /etc/apk/repositories && apk add --no-cache openssh
```

**Why it is there.** This environment routes outbound HTTPS through an intercepting proxy
whose CA the base image does not carry, so apk over https fails certificate verification
and the install dies. The container's egress to plain http is direct and unmolested.

**Why it costs nothing.** apk signs and verifies its own index and packages regardless of
the transport, so the rewrite does not remove a check apk was relying on. It is scoped to
a throwaway test container and to nothing else, and a failure to install still **skips**
rather than fails — a machine with no reachable package mirror is a fact about the
machine, not a broken harness.

## Gotchas worth keeping

- **OpenSSH reads the home from `/etc/passwd`, not `$HOME`.** A surface whose home was
  only an environment variable still read `/root/.ssh` and never saw its own key. The
  harness moves root's home properly with `sed` on `/etc/passwd`, which is also more
  honest about what that container is.
- **`pkill -f 'ssh -T'` matches the shell running it.** The kill took its own caller down
  before it reached ssh. Match the process *name*: `pkill -x ssh`.
- **Cancelling a `docker exec` does not end the process inside the container.** A
  scenario that gave up on a slow turn left an aforge still attached, and the *next*
  scenario's surfaces joined that conversation and were handed its reply. Every background
  launch sweeps the surface container when its subtest ends.
- **A relative `--session` lands in the workspace, not the home** (the engine has already
  chdir'd there). The two scenarios that name a conversation use an absolute engine path.
- **busybox `find` has no `-newermt`** and busybox `wget` is the readiness probe, because
  nothing is installed in the model container at all.
