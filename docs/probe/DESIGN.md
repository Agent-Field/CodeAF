# codeaf-probe — approved design

codeaf-probe is a CodeAF-specific companion binary that a coding agent drives to
test CodeAF. It is a separate executable in this repository. It is **not** a
subharness, not a general-purpose terminal simulator, not an LLM orchestrator,
and it performs **no persona or model generation** of its own. The coding agent
supplies the intelligence: profiles, scenario planning, judgment. The binary
supplies reliable terminal interaction, isolation, fast reuse, and evidence.

Sources this design was drawn from: `scripts/hosted-drive.sh` (real-binary,
isolated-home tmux drive with credential hygiene and owned-process cleanup) and
`docs/HEADLESS.md` (store isolation, model pinning, timeout honesty, measured
spend). `cmd/codeaf-replay` is chooser-regret replay, not a UI playback engine;
there is no generalized browser runtime and none is assumed.

## 1. Roles

- **Human** — asks a coding agent to test a branch. The human never talks to
  the probe.
- **Coordinator (coding agent)** — owns the whole test: inspects the source
  under test, chooses fixtures and profiles, plans the journey, and reads
  everything, including the probe's raw evidence.
- **Actor contexts** — independent coding-agent contexts that see only the
  objective, the profile, and the visible surface (screen observations). They
  deliberately do not see source code, probe internals, or other actors'
  reasoning, so their behavior is evidence of the product, not of the plan.
- **Evaluator** — an independent context that checks outcomes against the
  objective. It reads recordings and final states, never the actor's intent, so
  a pass is checked rather than self-reported.

The probe itself executes commands, renders screens, records evidence, and
manages fixtures. It generates nothing: no personas, no model calls, no
judgments.

## 2. Fixture, profile, outcomes

- **Fixture = product state.** A fixture is the state the product starts in —
  a prepared home/workspace/config (e.g. clean, or a returning-user state
  seeded through supported product setup or a controlled versioned seed helper;
  never unsafe raw-DB mutation to fake history).
- **Profile = actor behavior.** A profile describes how the actor should
  behave — goals, style, what it knows — and lives entirely outside the probe.
- **Functional outcomes vs usability hypotheses.** A functional outcome is a
  checkable fact about the product state (a commit exists, a card says merged,
  a file is unchanged); the evaluator can prove it from evidence. A usability
  hypothesis is a claim about a person's experience (findable, readable,
  low-friction); the actor reports it and the evaluator treats it as a
  hypothesis to weigh, not a fact. Every journey states which kind each
  assertion is.

## 3. Commands and contract

CLI verbs, each a short invocation; every response is compact JSON on stdout:

| Command | Purpose | Response (compact JSON) |
| --- | --- | --- |
| `prepare` | Build or adopt a CodeAF binary; establish immutable build identity; report contract version | `{"contract":1,"build_id":…,"bin":…,"reused":bool}` |
| `start` | Launch an isolated persistent terminal session on a fixture | `{"session":…,"rev":N,"screen_b64":…,"cursor":{…}}` |
| `observe` | Snapshot the screen now (with cursor and process status) | `{"rev":N,"screen_b64":…,"cursor":{…},"proc":…}` |
| `act` | Send real text/keys or resize; optionally bounded-wait for a new revision, atomically | `{"rev":N,"act_ok":true,"wait":{…}}` |
| `finish` | End a session; clean up only owned resources | `{"cleaned":[…],"kept":[]}` |
| `fixture` | `list`, `reset`, `verify` — prepare/restore reproducible product state | `{"fixture":…,"ok":true,"identity":…}` |
| `record` | Read step-indexed action/observation recordings | `{"steps":[…]}` |

The contract is discoverable: `codeaf-probe --json-contract` prints this table
plus error codes, so an agent can read the interface without reading this
document.

## 4. The persistent runtime, and what "screen" means

One persistent local tmux runtime, on **its own socket** (a name derived from
the session, as `hosted-drive.sh` does), survives short CLI invocations: each
command is a fresh process, but the session, its pane, and its build identity
persist until `finish` or a bounded session cap ends them.

**tmux IS the terminal emulator and renderer.** Observations are
`capture-pane -e`-style rendered screen state — the pane as a real terminal
painted it, including the cursor position — **not** a stripped-ANSI byte stream
falsely called a screen. CodeAF's v3 surface is full-screen TUI code; only
rendered state is a truthful observation of it.

## 5. Build identity and reuse

`prepare` establishes an **immutable build identity** and reuses it:

- identity = source sha, dirty flag, go version, build flags;
- reuse the Go build cache so rebuilds are fast; a rebuild only happens when
  the identity changes;
- `--bin` accepts an explicit existing binary and skips building entirely;
- **never swap the target binary mid-session** — a session pins its build;
- the exact build/config identity (binary hash, model pins, config paths) is
  recorded on the session and echoed in every response that names a build.

## 6. Actor operations

Actor operations are what a person at a keyboard can do, and nothing more:

- `act --text` — real typed text;
- `act --keys` — real key presses (`Enter`, `Tab`, `Esc`, …);
- `act --resize` — resize the terminal (rows/cols).

No internal navigation shortcuts, no direct state writes, no pane-contents
injection. If the product can only be reached through its UI, the actor reaches
it through the UI.

## 7. Observation and waiting

- **Screen revision counters.** Every screen change bumps a monotonically
  increasing `rev` per session. An observation carries the `rev` it saw.
- **Snapshots and diffs.** Full rendered snapshots on demand; a diff against a
  named revision for cheap change reading.
- **Cursor and process status** ship with every observation.
- **Bounded waits with truthful reasons.** A wait returns when the revision
  advances or the bound expires, and says which happened and what it saw.
- **Screen quiet ≠ work complete.** The product may be thinking between
  frames; quietness is never reported as completion. Completion claims come
  from the evaluator against product state, not from the screen being still.
- **Atomic act+wait+observe.** `act` may send input, wait a bound, and return
  the resulting observation in one call — one tool turn, not three.
- **Stale revision rejected.** Acting against a `rev` that is no longer current
  is refused with an error rather than racing an unseen change.

No long blind scripted sequences: journeys are step-by-step with observations
between actions, which is also what makes them usability evidence.

## 8. Safety

- **Private artifacts by default** — sessions, recordings and screens are
  created owner-only (`umask 077`, as `hosted-drive.sh` does), never published.
- **No credentials in output** — every response and recording is scrubbed of
  values the probe knows are credentials (fields whose names say so,
  `_API_KEY`-suffixed variables); kept screens are private files.
- **Sanitized env/config** — sessions run in an isolated home/workspace with a
  copied, minimized environment; the user's own state is never written.
- **Bounded sessions/waits/concurrency** — every wait, session and recording
  has a hard bound; the truth is reported when a bound ends something.
- **Owned-process-only cleanup** — on `finish` or interruption, the probe ends
  its own tmux server by socket name and only engine processes it can prove it
  started (its executable, on its home's socket). It selects no process by
  command-line text.
- **Protect unsaved work** — cleanup never ends a session holding user work
  without a truthful report; fixture isolation keeps experiments away from real
  user data. The coordinator retains unrestricted shell, so containment is
  scoped handles, not a hard guarantee — stated, not claimed.

## 9. Fixture reproducibility, and what v1 does not do

- **Safe quiescent snapshots only.** A fixture is captured or seeded while the
  product is quiescent, via supported product setup or a versioned seed helper
  (schema/version aware, isolated from the real user's data). **No unsafe
  raw-DB mutation** to fake history.
- **Deterministic verification vs live campaign.** Fixture-backed journeys
  with no model calls are deterministic and CI-repeatable: same fixture, same
  actions, comparable outcomes. A paid/live agent campaign (real model calls,
  pinned and recorded per `docs/HEADLESS.md`) is nondeterministic by nature;
  its results are campaign evidence, never a CI gate. The two are labeled
  differently everywhere they are reported.
- **Playback vs re-execution.** Recorded evidence can be played back locally
  (a re-render of what happened); re-executing a journey may be
  nondeterministic. Playback is labeled playback.
- **Out of scope in v1:** no browser support, no video production, no A/B
  statistics engine, no model-worker orchestration inside the binary. The
  external coding agent owns all of that where it wants it.
