# `--host` parity reference

This is the code-derived inventory of the v3 surface at the current base. `WORKS` means the
door acts on the machine the session belongs to, or is intentionally a surface-local act.
`HONEST` means the door cannot cross and the surface gives the quoted sentence or is absent
by construction. `NOT YET` records a missing crossing whose absence has no dedicated
person-facing sentence. The engine machine owns the conversation; the surface machine owns
the terminal, draft, input history, browser and downloaded copies.

| Door | Verdict over `--host` | What the code does now |
|---|---|---|
| submit a message; follow up; steer | WORKS | The sentence is echoed locally, then `Submit` or `FollowUp` carries intent to the engine. A refusal removes the echo. |
| interrupt and compact | WORKS | `Interrupt` and `Compact` cross the wire. |
| model, context and reasoning controls | WORKS | The change crosses; pushed facts keep every attached surface current. |
| permission card | WORKS | The card and answer cross. `always` is session-only and therefore reads `allowed`, not `saved`. |
| standing-order card | WORKS | The proposal and answer cross and are stored on the engine machine. |
| account key card | WORKS | A pasted key crosses as an answer and is stored on the engine machine. |
| account browser card | HONEST | Only `not now` is offered, with `connecting an account is not available over --host yet`. |
| harness offer card | WORKS | A held offer can cross and wait; accepting runs the saved harness on the engine. |
| harness-design card | HONEST | Harness building is absent from the engine's belt because its result lane cannot cross. |
| adaptive-run cards and gauge | HONEST | Adaptive runs are absent from the engine's belt because their notes and spending gate cannot cross. |
| task rail and task rooms | NOT YET | The remote agent does not implement the task subscription, so the rail and rooms are absent. |
| `/help`, command chips and aliases | WORKS | They are surface-local descriptions of the same command table. |
| `/new` | WORKS | `Session.New` replaces the far conversation. |
| `/resume` and `aforge resume --host` | WORKS | The picker lists the far machine's sessions and `Session.Open` switches there. |
| `/compact` | WORKS | The compact request runs on the engine. |
| `/rewind` | WORKS | The remote rewind seam reads and applies the far conversation's journal. |
| `/copy` and `/select` | WORKS | These are local ways to select the transcript already held by the surface. |
| `/image` and pasted images | WORKS | The path is local; bytes cross and are remade in the far session folder. |
| `/attach`, terminal drops and attached files | WORKS | A drop made only of local files becomes visible tray chips; on send the local bytes cross, land in the far session's `attachments/`, and accompany the message. |
| `/export` | WORKS | Export is intentionally local and confirms `exported · <short path> · on this machine`. |
| `/files` | WORKS | Bare opens a local browse door onto the far workspace; a path fetches one far file and opens the local copy. |
| clicked reply paths | WORKS | `StatPaths` confirms files on the engine and a local loopback door fetches them. Confirmed directories are not links. |
| `/status` and `/cost` | WORKS | Far facts are pushed to the surface; remote paths carry the machine prefix. |
| `/cache` and `/cache clean` | WORKS | The command concerns the surface machine's build cache; it does not claim to clean the engine's cache. |
| `/model` and model picker | WORKS | The catalog is cached locally; the chosen intent crosses and the engine announces the resulting fact. |
| `/home` | WORKS | `Places.World` lists the far machine; no local fallback is drawn before the first answer. |
| `/history`, tasks place | WORKS | Task rows come from the far `World` snapshot. |
| `/standing`, standing place | WORKS | Current and project-wide items are read and saved through the far standing store. |
| `/memory`, memory place | HONEST | It draws `memory shows what this machine has learned, and this session is on another`. |
| spend place | HONEST | It draws `spend shows what this machine has cost, and this session is on another`. |
| search place | HONEST | It draws `search reads what was said on this machine, and this session is on another`. |
| `/settings`, `/set`, `/config` | HONEST | It opens and says `these rows are this machine's — the ones that govern the conversation are read from the profile on the other one`. Surface writes remain local. |
| `/permissions` | NOT YET | The YOLO badge is the far engine's posture, but the page reads and deletes this surface machine's saved approval rows and gives no host-specific sentence. |
| `/connect` | HONEST | It says `connecting an account is not available over --host yet — the sign-in opens a browser here and the account belongs to the machine over there. accounts already connected on that machine keep working.` |
| `/harness` and `/subharness` listing | HONEST | No registry seam is supplied, so the panels say harnesses are unavailable instead of listing the surface machine's registry. Running an already selected far harness still works. |
| `/task` | NOT YET | Bare `/task` opens the far-backed tasks place, but `/task <brief>` reaches `could not start the task · this session has no task door`; the remote handle has no task seam and the refusal is not in the surface's register. |
| `/crew` | NOT YET | The picker reads and writes this surface machine's profile. It does not change the crew the far engine resolves, and gives no host-specific sentence. |
| `/quit`, `/exit`, `/q`, double `ctrl+c` | WORKS | The surface closes deliberately; the engine distinguishes close from a torn pipe. |
| `space space`, `tab`, place digits and place clicks | WORKS | They navigate the surface; far-backed places use the cached far world. A place-row click only selects (home opens on the second click), the same as locally. |
| typing, editing, draft history and scroll keys | WORKS | These are intentionally local surface state. |
| watcher `enter` | WORKS | `Take` moves the one keyboard to this surface in one round trip. |
| two windows on one conversation | WORKS | The newest arrival drives; watchers receive the opening sentence and live turn. A reconnect does not steal the keyboard. |
| home conversation card | WORKS | `Session.Open` switches the far host to the selected conversation. |
| task, standing, memory, spend and search row actions | WORKS | Navigation remains local; actions on far home/tasks/standing rows use far data. The three honest-empty places have no actionable rows. |
| settings row click or `enter` | HONEST | The panel is usable for surface settings, but its opening sentence warns that conversation governance is read on the other machine. |
| named-path and file browse page click or drag | WORKS | Click fetches a far file into the surface cache and opens that named local copy; drag deposits one file in the far session folder without starting a turn. |
| generated image preview in the terminal | WORKS | Finished `generate_image` and `view_image` calls fetch eligible far bytes into the surface cache and paint that copy while continuing to name the far path. |
| terminal title, transcript, markdown and tool cards | WORKS | Events and pushed facts render through the ordinary v3 surface. |
| workspace, session and tool paths | WORKS | Person-facing remote paths are prefixed with `<machine>:`. The git branch is left empty because probing locally would be false. |
| YOLO badge and approval posture | WORKS | The posture is carried from the engine in the welcome. |
| status meter for spend and context | WORKS | Complete far facts are pushed; zero stays empty except the live `$0.00` status-line stabilizer. |
| connection meter | WORKS | `Ping` produces a rolling round-trip estimate; before the first answer it is empty and while redialling it is replaced by `reconnecting to <machine> — trying for up to 5 minutes`. |
| ssh passphrase and host-key questions | WORKS | They remain on the plain terminal before the TUI takes the screen. |
| ssh reuse, heartbeat, missed-heartbeats and traffic settings | WORKS | The surface's ssh process reads these local transport settings. |
| version mismatch | HONEST | Handshake refuses before the TUI with either `<dest> runs a different version of aforge than this machine does — update the older one so both ends speak the same protocol` or the engine's exact protocol-number refusal. |
| dropped-link reconnect | WORKS | The surface redials for up to five minutes and resumes the event gap when the host persisted it. |
| pipe-only engine after a drop | HONEST | The turn ends and the transcript says that the machine does not keep a turn running while nothing is attached. |
| detach, close and idle retirement | WORKS | Detach leaves persistent work running; close ends the conversation; idle hosts retire themselves. |
| `aforge engine --stop` | HONEST | It is not a command. There is no early-stop surface door; the host retires after its idle clocks. |
| `aforge engine --no-host` | WORKS | It deliberately serves one pipe without attaching to or starting a session host. |
| `--at` paired connection | WORKS | It uses another byte-stream transport after pairing; the conversation semantics above are the same, while ssh-specific settings and prompts do not apply. |
| model tools (`read`, `write`, `edit`, `bash`, web and enabled account tools) | WORKS | The engine assembles and runs the belt against its workspace, credentials and policy. |
| model memory tools | WORKS | Capability presence follows the engine configuration; an unavailable tool is absent, never a callable failure. When present, the tool runs on the engine. |
| model task, harness-design and adaptive-run tools | HONEST | Tools whose result or control lanes cannot reach the surface are omitted from the far belt. |

The authoritative implementation inventory is the honesty table in
`internal/tui3/host.go`, the remote methods in `internal/remote/wire.go`, and the host
assembly in `cmd/aforge/chatv3_host.go` and `cmd/aforge/engine.go`. When a lane turns a
`HONEST` or `NOT YET` row into `WORKS`, it must update that table, remove the old manual
refusal, and update this reference in the same change.
