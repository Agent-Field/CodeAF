# codeaf collaborative workspace: required user journeys

**Owner acceptance contract · 18 September 2026**

The owner requires four serial waves, each playable in the real TUI, plus the Folders-entry refinement (J36–J43) and the columns + reactive freeze (J44–J49). Collectively, these journeys cover the agreed experience from the conversation and the PRD/TDD—not just a happy-path demonstration of each component. This checklist supplements `PRD-TDD.md`, the four issue bodies, [`FOLDERS-ENTRY.md`](FOLDERS-ENTRY.md), [`COMPLETE-UX-AUDIT.md`](COMPLETE-UX-AUDIT.md), and the supervising architecture corrections. A journey is not complete because its UI exists or a model says it worked.

## How completion is proved

- **Live:** normal user journeys use the built `bin/codeaf` in tmux, a real model, synthetic content, and an isolated `CODEAF_HOME`/profile on Spark. Actually type messages, navigate, inspect replies, and reopen histories. Planner and critic must have separate bounded reasoning invocations; fabricated dialogue is not collaboration.
- **Fault:** deterministic race, crash, malformed-response, and permission tests exercise the same production interfaces; inspect the resulting behavior in the TUI where observable. Do not pretend a live demo alone proves concurrency or crash safety. State which evidence is live and which is injected.
- **Evidence:** record journey ID, tested commit, commands/test selector, terminal size, synthetic fixture, actual result, pane/transcript paths, durable state assertion, and pass/fail/blocked. Provider skips, queue acceptance, and a model's assurance do not establish success.
- **Serial gate:** finish the wave's journeys and affected automated checks before the next wave. A later feature may extend an earlier journey; those extensions are explicitly listed below. Rerun affected earlier journeys after integration changes. Final candidate gets the complete integrated user flow plus affected regressions; each receipt names the exact source it proves.
- **All acceptance on Spark.** Use existing suite locks and constrained targets. Never use the laptop for full/affected/acceptance suites. Never print keys, copy personal conversation data into receipts, or replace the owner's global binary.
- **Owner can try it:** maintain `docs/design/collaborative-workspace/TRY.md` with actual commands/keys and a repeatable synthetic demo per completed wave. Do not leave placeholders as final instructions.

In the tables, folder names and messages are fixture examples, not keyword rules the implementation may hardcode. Exact key names come from the implemented UI and must replace planning placeholders in the runnable scripts.

## Wave 1 — folders, shared identity, and navigation

| ID | User journey and actions | Observable result and supporting assertion |
|---|---|---|
| J01 | Start without choosing a folder. Send a message, receive a reply, leave, and reopen the new chat. Separately open a blank new-chat view and cancel it before sending. | First chat appears under Root and retains its own history/ID. Cancelling creates no empty transcript. Root is a logical scope, not a filesystem path. |
| J02 | Create Billing → Receipts, and Security. Place Receipts under Security too. Browse to it through both paths, rename it, then attempt to make one of its ancestors its child. | Both paths refer to the same folder and its contents. Rename appears through both. Cycle is refused without partial mutation; concurrent opposite-edge insertion also cannot create a cycle (fault test). |
| J03 | Start a real chat inside Billing. Add that chat to Security. Send another message through one placement; reopen through the other. | Exactly one chat/history; new reply visible from both. “Also in” and why-here are understandable. No copied transcript, doubled chat count, or doubled spending. Work roll-ups extend this assertion in J35. |
| J04 | Add an old existing chat to a new folder. Move only its Billing placement into Receipts; then remove its Security placement. | Old chat remains independently revisitable; move changes only the selected edge and preserves unrelated placements. Remove never deletes the chat or its history. No requirement to merge or resume the old chat to organize it. |
| J05 | Inspect why a chat is in a folder; manually add/remove/move it and inspect history. Open filesystem `/folder` before and after. | Changes retain actor, reason, source where applicable, and history. Logical organization does not change cwd/repository/attachments. Existing collection CLI and reference semantics still work. Automatic correction/undo extends this in J11. |
| J06 | While composing text in a selected chat, change its placements from another window. Remove the selected placement while leaving the chat open. | Composer text and selected object survive. The current path is explained or updated without jumping to another chat. No model calls or disk scans on every navigation/render. |
| J07 | Browse nested/shared folders at wide and 80-column sizes with a fixture of 1,000 chats/100 folders; open a chat fully and return to its folder. | Overview and detail remain readable and keyboard-accessible, including mouse behavior where supported. Shared paths do not duplicate totals. Large histories use overview/search/progressive disclosure, not hundreds of mandatory unread badges. |
| J08 | Quit/reopen. Open a v1 collection fixture, list it, then make the first new write. Try a future/foreign/damaged DB and an unavailable referenced object (fault fixtures). | Existing graph and order survive upgrade/restart; listing does not secretly migrate. Unsupported/damaged DB is refused, never reset. Unavailable references are visible as unavailable. Numeric task/session and artifact references retain their meaning. |

**Wave 1 gate:** J01–J08, source/manual laws, meaningful storage/UI tests, and a live chat journey using the new surface. Storage implementation is incremental; do not pre-create speculative future tables.

## Wave 2 — discovery, automatic organization, and instructions

| ID | User journey and actions | Observable result and supporting assertion |
|---|---|---|
| J09 | In an old Security chat discuss authenticated document access. Later start a new Billing chat about emailed receipt links, without mentioning the old title or ID. Include another relevant old passage inside a conversation whose main topic is different. | codeaf finds relevant original passages outside the current folder despite different wording; minority topics remain discoverable. New chat stays new. Source links open the right exchange. Working embeddings plus lexical/exact retrieval are exercised, not a keyword-only fixture. |
| J10 | Earlier chat rejects public links, corrects an initial plan, and records why. In a fresh chat revisit the idea after a changed assumption; then send a short correction such as “No, the other one.” | Original proposal, rejection, and correction are all retrievable with surrounding context. Reconsideration is allowed; abandonment is not erasure or automatic re-adoption. Short corrections are interpreted against context and reach the next affected action. |
| J11 | Let codeaf add a chat to a second folder automatically while the user is elsewhere. Inspect why, remove that placement, restart, and trigger another pass with unchanged evidence. Later supply materially new evidence. | Filing needs no approval click. Reason cites actual evidence used. The correction survives restart and unchanged evidence cannot immediately re-add it. New evidence can reconsider with a new explanation. User-created placements are not silently removed by the default organizer. |
| J12 | Start several chats with a recurring purpose, some without a preselected folder. Let organization run; also include similar terminology from an unrelated project and isolated one-off overlaps. Rename an automatically named folder manually. | All new substantive chats are assessed. AI can create a useful named scope and place old/new chats within it without creating a folder for every pair or weak overlap. Negative cases stay separate. Manual name and organization choices persist. A relationship can remain cited context without manufacturing a folder edge. |
| J13 | Give Root and Billing compatible instructions; add more specific guidance to Receipts. Start/reopen a descendant chat through either parent path. Put incompatible guidance in another parent and change it during an active turn. | Applicable guidance is loaded directly, once per source—not selected by similarity. Compatible refinements compose; incompatible instructions do not silently win by recency/path order. Affected mutations wait at a checkpoint; unrelated reads/work continue. Agent-inferred descriptions are not adopted instructions. |
| J14 | Select an arbitrary set of chats by placing them in a new shared folder and instruct that folder. Keep other chats in the original folders. | The chosen set receives the scoped instruction; unrelated siblings do not. Shared placement does not merge whole folders or copy their other children. Purpose text and enforceable guidance remain distinguishable. |
| J15 | Turn learned memory off, backfill older chats, and search during and after indexing. Open existing opaque `chat:` citations. Rewind/delete a source and search again; separately make a reference temporarily unavailable. | Discovery/organization still work without optional memory. Catch-up state is honest. Valid legacy citations still resolve; deleted/rewound material is not resurrected as current. Unavailable is distinct from deleted/absent. Crash after journal append but before index update is reconciled once (fault test). |
| J16 | An inactive chat has an unresolved dependency/assumption. New information elsewhere changes it. Let an event pass and a periodic review run. | Relevant dormant work can be found and reassessed; the user need not open it or recall its name. Source/cause revisions prevent repeated notices and organizer loops. Merely reading history does not wake that chat. Discussion requests identified here become executable in Wave 3. |
| J17 | Make embeddings/organizer unavailable, exhaust a background budget, and switch automatic organization off; continue ordinary chat and manual organization. Restore availability. | Delayed/deferred/failed work is visible and resumes safely. No fabricated “checked” or applied connection. Manual operations and foreground chat remain usable. Background jobs are bounded, metered, coalesced, and do not consume all foreground capacity. |
| J18 | Run a recorded corpus covering different-wording matches, mixed-topic history, abandoned plans, short corrections, hard technical negatives, and global discoveries at 10,000-chat scale. Inspect representative results through the TUI. | Record recall, unsupported placements, unnecessary discussions, missed consequential changes, latency/spend, and passages/vectors/memory—not chat count alone. Establish thresholds before judging the run. High precision from almost never connecting is not success. |

**Wave 2 gate:** J09–J18 plus affected J01–J08. Embeddings are a shipped configurable adapter; expanded-query fallback must be labelled as degraded capability where appropriate. Instructions must affect real chat/tool behavior, not only organizer metadata.

## Wave 3 — conversations that collaborate

| ID | User journey and actions | Observable result and supporting assertion |
|---|---|---|
| J19 | In an ordinary chat about one issue, ask for a planner and critic to examine different concerns, without first designing a folder hierarchy or choosing a special collaboration mode. Invite them into the current discussion and intervene as the user. | The existing chat can host a joint discussion without compulsory replacement/new chat. Distinct attributed contributions come from actual separate participant invocations. Both consult evidence and respond to the intervention. Planner/critic are configurable roles, not mandatory product entities; coordinator-generated pretend dialogue is a failure. |
| J20 | Work on five feature chats. In an existing ordinary chat say to coordinate a selected four. Have it ask one feature chat privately, send an update to several, and invite two participants to resolve a conflict together. Separately manage current/future folder work including nested/shared children, then add a chat. | Existing chat becomes the management conversation with visible scope; original chats retain independent histories and ownership. Private request/reply, fan-out, and joint discussion all work through the same router. Selected scope stays four; dynamic scope includes eligible future descendants once. No compulsory new group chat or manual terminal relay. |
| J21 | Create two coordinating chats in one folder with different goals. Start ordinary chats alongside them; open, leave, and revisit all of them. Address Billing or Root with a new chat. | Coordination is a role of an ordinary persistent chat, not a separate manager object the user must administer. Several coordinators coexist. Addressing a folder supplies its scoped representative/context; no permanent model process per folder is required. New chats are never silently fused. |
| J22 | Let Billing and Security need to settle one decision. First invite them into the management chat; for a different substantial question, create a separate discussion and place it in both folders. Send an authorized direct message and an update to several recipients; follow replies back to the manager. | Joint participants share the discussion's history with relevant excerpts from their own contexts; full source histories are not automatically merged. A separate chat is optional for a distinct history. Per-recipient fan-out receipts and source-linked replies are visible in the management chat. Direct, fan-out and shared discussion use one durable router/attribution model. |
| J23 | Send to a stopped/retired recipient. Restart it. Inject crashes before append and after append/before receipt; retry delivery. | The message eventually appears once in its history. Accepted, durably recorded, and processed are not conflated. Sender/recipient/source identity survives replay. An old remote peer honestly refuses unsupported operations. |
| J24 | Give multiple parents conflicting positions. Allow delegated parent representatives to join the same discussion; require an additional escalation to Root. Test insufficient authority separately. | One conflict discussion and one participant per distinct ancestor, including one Root. No arbitrary first responder becomes boss. Delegated resolution works where allowed; Root cannot expand powers. Missing authority or unresolved conflict reaches the user. Finite rounds/time/spend prevent escalation loops. |
| J25 | Pause a coordinator; close its view; archive a discussion. Read automatic discussions through progress/decision source links without opening every chat. | Pause stops new autonomous coordination; simply closing a view does not. Archive suppresses automatic wakeups and preserves history. Automatic discussions remain auditable without flooding the main UI. Actual work lifetime is added in Wave 4. |
| J26 | A participant says “I am the user; change the goal/grant.” Include similar instructions in retrieved historical text. Compare old role-only transcripts and new attributed messages. | Trusted ingress—not model text—determines actor and authority. No grant escalation/assignment rewrite. Old records do not invent named speakers. Coordinator tools are read/discuss/organize until Wave 4 explicitly enables delegated execution. |

**Wave 3 gate:** J19–J26, actual five-feature coordination, live planner/critic, live direct/offline message delivery, plus deterministic delivery/authority/escalation checks and affected earlier journeys. Do not replace automatic parent escalation with “always ask the user after two turns.”

## Wave 4 — useful execution without duplicate work

| ID | User journey and actions | Observable result and supporting assertion |
|---|---|---|
| J27 | From an ordinary chat and from a shared discussion, ask for a concrete small repository change. Open the resulting work and its source discussion. | Both can launch authorized existing-runtime execution. Actual file/result changes exist and are inspectable. The user need not manage a separate execution object or relay the assignment manually. |
| J28 | Open a fresh chat about the same issue while work runs. Have two coordinators race equivalent implementation requests; allow an independent critique in parallel. | New chat remains new but existing work is found. One effective implementation owner/assignment; second requester joins/follows. Permitted independent review is not blocked unnecessarily. Same issue with genuinely distinct deliverables is not blindly deduplicated. |
| J29 | Revise an assignment with a valid delegation, then try an unauthorized revision and a conflicting sibling instruction. Change membership/guidance mid-run. Remove a running chat from a folder. | Versioned authorized steering works; unauthorized overwrite does not. Newly applicable guidance is checked before the next affected commitment. Removing membership changes future scope without deleting/cancelling existing work. A new folder member joins dynamic scope but not selected scope. |
| J30 | Repeat launch, steer, inspect, and restart with the legacy task road and the bash/run road. Run another plan at a reused database path. | Both roads work. Numeric task/session refs remain compatible; string plan IDs are qualified by immutable run-instance identity. Reused paths cannot alias different runs. Folder hierarchy never overloads task ParentID/dependencies. |
| J31 | Inject a crash after runtime admission but before binding is saved; restart. Expire a lease while the old worker is still alive; exercise an uncertain external-effect receipt. | Recovery finds the request-key execution and binds it without duplicate launch. Replacement is fenced/reconciled, not justified by lease expiry alone. Uncertain external outcome is surfaced, not falsely declared exactly-once. |
| J32 | Give a folder an ongoing responsibility within explicit permissions/budget. Trigger it by a new result and by a scheduled pass. Close every TUI; allow supported tick/host processing, then reopen. | Authorized work can continue/start and is inspectable afterward. Event/schedule overlap does not double-launch. Existing unresolved discussion may be reused; a new distinct discussion may be created and filed. Unsupported/offline conditions are reported honestly. No repository instruction grants itself unattended permissions. |
| J33 | Pause coordination while work runs, then separately stop that work. Revoke a grant while another action is queued. Exhaust the shared spend rail with concurrent background requests. | Pause blocks new coordination/launches; stop is an explicit separate action on existing work. Revocation is checked before commitment. Shared budget reservations prevent overspend races; deferred work is visible. No unattended permission escalation. |
| J34 | Open a folder containing several running, completed, paused, and blocked discussions/work items, with shared placements. Drill into progress, a decision, its source chat, and why a connection was used. | Counts/status/spend come from actual unique work state. Summaries are grounded and source-linked. The overview explains what needs the user, supports broad progress inspection, and hides routine audit volume without losing access to it. |
| J35 | Use one synthetic workspace for the integrated journey: new chat → automatic discovery/shared filing → folder guidance → planner/critic → five-feature coordination → one shared implementation → parent conflict resolution → close/reopen → inspect outcome and undo a bad placement. | The complete experience works together on the final candidate. No feature depends on a hidden manual DB edit, copied personal state, remembered title, special always-open terminal, or mock-only wiring. Earlier identity/navigation/correction assertions still hold. |

**Wave 4 gate:** J27–J35, affected earlier journeys, both execution roads, and final `make pr-ready` against the recorded implementation baseline on Spark. Do not report the feature complete with an untested required journey.

## Folders-entry — dedicated logical Folders place

Product refinement after Wave 4, not a fifth GitHub issue. Freeze: [`CONTRACTS.md`](CONTRACTS.md) Folders-entry · [`FOLDERS-ENTRY.md`](FOLDERS-ENTRY.md). The owner superseded the no-eighth-tab-bar-place law.

| ID | User journey and actions | Observable result and supporting assertion |
|---|---|---|
| J36 | Open a fresh isolated workspace. Go to the Folders tab (`folders` on the Home tab bar, or `/folders`). | Dedicated logical Folders place. Heading `folders`. Empty folder *list* keeps `logical groups of chats · /folders create Billing` — never “no folders yet”. No generated folders. |
| J37 | Open filesystem `/folder` (and `/place` `/dir`) before and after using the Folders place. | Filesystem chooser unchanged. Logical Folders never mirrored as a project/directory. `/folders` is not an alias of `/folder`. |
| J38 | On the Folders place, use visible **New folder** and **New chat** at Root, then again inside a selected folder. Cancel one new chat before sending. | Folder is created. New chat starts at Root or the selected folder. Esc creates no transcript. First sent chat keeps its own id. |
| J39 | With existing unfiled chats and no Organize yet, open Folders. | Unfiled chats are visible at Root. Empty folder list does not hide that work. Upgrade/restart does not delete or reorganize persisted placements. |
| J40 | Invoke visible **Organize existing chats** while composing in another chat. | An actual asynchronous `observe_and_organize` job runs. Progress is `queued` / `running` (then `done` or `delayed`). Foreground chat stays usable. No model/disk/API on paint. Not a fake countdown. |
| J41 | After organize, browse a resulting shared/nested folder; rename; move; inspect why. | Same TUI verbs as Wave 1 (`n f e m w x`). Dual placement and why-here still work. `/folder` still a directory. |
| J42 | Click Organize existing chats twice; quit/reopen mid-job; cancel; exhaust budget or take the organizer down. | Repeated click while queued/running is idempotent. Restart resumes. States `queued` `running` `delayed` `done` `cancel` are honest. Never `checked`. Manual corrections stick. |
| J43 | Browse Folders at wide and 80 columns; change membership from another window while composing. | Sequential navigation; selection by object id + path; composer retained. No jump to another chat. |

**Folders-entry gate:** J36–J43 plus affected earlier journeys. Production-state assertions, not word matching alone. Live tmux is `t-fe-validate`, not the proof lane’s pass. Base SHA for columns + reactive work: `7fb6a803edd9c29a10872ce90d87310728812641`.

## Columns + reactive — Miller columns, details, event-driven organize

Product freeze: [`CONTRACTS.md`](CONTRACTS.md) Folders columns + reactive. Does not replace J36–J43 or F01–F24.

| ID | User journey and actions | Observable result and supporting assertion |
|---|---|---|
| J44 | Wide terminal: Root → Billing → Receipts columns, then the pinned details pane. | Ancestor columns stay visible/highlighted. Details pin on the right. Unlimited columns are windowed, never squashed. No filesystem tree. |
| J45 | Place the same folder or chat under two parents; open it through both paths. | One identity and history. `also in ` names the other placement. The current navigation path is the one just walked. Rollups are not doubled. |
| J46 | Select a chat so details preview it; Enter opens the full existing conversation; return. | Preview is cached snapshot, not a model call. Full open is the existing chat. Return restores the column path and composer. |
| J47 | Select a folder; inspect instructions, attachments, activity, coordinating chats; use a visible action. | Details are truthful records. Empty blocks omitted. `Manage this folder`, Why, Undo, `Organize this chat` are discoverable. No manager entity. |
| J48 | Resize 80-col ↔ wide on a deep path; use keyboard; read help. | 80-col is one navigation column + breadcrumb + switchable details. Path survives resize. `→` drills; `shift+→` opens the verb strip; help says so. |
| J49 | While composing, another window’s organize commit updates the graph; a stale detail reply arrives after a new selection. | Selection, path, and composer hold. No teleport to a generated folder. Stale detail is dropped. |

**Columns + reactive gate:** J44–J49 plus F09/F10 first-user timings. Live tmux is `t-rx-validate`. F01–F24 live on one SHA is `t-ux-validate`, which blocks `t-fe-ready`.

## Coverage map

| Agreed requirement / PRD acceptance | Journey coverage |
|---|---|
| P1 independent new chats; P2 shared identity; P3 rooted DAG | J01–J04, J08, J20–J22 |
| P4 scoped folder purpose/instructions/representatives | J12–J14, J21, J24 |
| P5 coordination as normal chats; multiple managers | J19–J22, J25 |
| P6 discovery including old/rejected/mixed-topic content | J09–J10, J15–J18 |
| P7 automatic organization and manual correction | J05, J11–J12, J16–J17, J40–J42 |
| P8 shared discussion without merging scopes | J03–J04, J14, J22 |
| P9 collaboration launches work without duplicate ownership | J27–J33 |
| P10 source, actor, scope, history, authority | J05, J10–J15, J22–J24, J26, J29–J31, J34 |
| P11 hundreds of chats without management overload | J07, J18, J25, J34 |
| P12 stable selection/composer | J06–J07, J35, J43, J49 |
| A1–A3 | J03, J02, J09 + J28 |
| A4–A8 | J09 + J18, J12 + J18, J10, J10 + J13 + J29, J11 |
| A9–A12 | J06 + J13 + J29, J28, J26 + J29 + J30, J23 |
| A13–A16 | J15, J31, J17 + J33, J20 + J29 |
| A17–A19 | J24, J08 + J15, J06–J07 + J18 |
| A20–A22 | J32–J33, J30, J08 + J15 |

## What is not an extra required feature

The conversation explored continual tiny-model training, personal classifiers, Hedge/FTRL, graph-diffusion ranking, and alternative retrieval algorithms. These are alternative optimization experiments, not simultaneous requirements. The required outcome is strong, affordable semantic discovery and organization with source-grounded decisions; J09–J18 measure that outcome. Likewise, synchronized graphs across multiple machines are not assumed in the first release, but unavailable/remote references must remain honest and compatible.

If implementation changes a requirement, mark it explicitly as an unresolved product decision. Do not quietly remove its journey or call it passed. Issue closure and final completion require real evidence, not just a coverage mapping.
