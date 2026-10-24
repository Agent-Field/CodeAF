# Changelog

Release history for codeaf. Unreleased changes live in
[docs/changes/unreleased/](docs/changes/unreleased/).

<!-- codeaf-changes inserts new versions directly below this line -->

## v0.8.2 — 2026-10-23

### Fixed

- **the installer says why GitHub's API refused, and retries once without a token GitHub refused** — [#1890](https://github.com/Agent-Field/codeaf/pull/1890) · `build` `docs`

## v0.8.1 — 2026-10-21

### Fixed

- **model usage accounting covers compatible providers and separates estimates from measurements** — [#1887](https://github.com/Agent-Field/codeaf/pull/1887) · `engine` `docs`

## v0.8.0 — 2026-10-14

### Added

- **ai& is offered as a provider of its own, asked for by key and nothing else** — [#1737](https://github.com/Agent-Field/codeaf/pull/1737) · `chat` `engine`

- **memories belong to an owner, every write walks one door, and failure is visible** — [#1745](https://github.com/Agent-Field/codeaf/pull/1745) · `chat` `engine` `docs`

- **ordinary memory retains evidence and conditional context before actions** — [#1777](https://github.com/Agent-Field/codeaf/pull/1777) · `chat` `engine` `docs`

- **sec, sec-af's security audit built into codeaf, audits a repository or its changes** — [#1781](https://github.com/Agent-Field/codeaf/pull/1781) · `chat` `engine` `docs` `build`

- **review, pr-af's code review built into codeaf, reviews a GitHub pull request and posts on your yes** — [#1784](https://github.com/Agent-Field/codeaf/pull/1784) · `chat` `engine` `docs` `build`

- **senior-dev checks CMake projects itself and takes --verify-build and --verify-test** — [#1793](https://github.com/Agent-Field/codeaf/pull/1793) · `engine` `docs`

### Changed

- **Teams previews, shared conversations and scoped deletion** — [#1750](https://github.com/Agent-Field/codeaf/pull/1750) · `chat` `engine` `remote` `docs`

- **Choose a provider during setup and pick only available models** — [#1789](https://github.com/Agent-Field/codeaf/pull/1789) · `chat` `docs`

- **launches offer a new codeaf in a quieter, non-blocking way, and installs run in the background** — [#1790](https://github.com/Agent-Field/codeaf/pull/1790) · `chat`

- **Clear settings categories and AI team navigation without resetting preferences** — [#1792](https://github.com/Agent-Field/codeaf/pull/1792) · `chat` `engine` `docs`

### Fixed

- **a reply that is only a number and a full stop draws instead of vanishing** — [#1746](https://github.com/Agent-Field/codeaf/pull/1746) · `chat`

- **the hosted drive passes on a Mac whose task column starts open** — [#1756](https://github.com/Agent-Field/codeaf/pull/1756) · `build`

- **PlanDB run workers can search and fetch through native tools** — [#1783](https://github.com/Agent-Field/codeaf/pull/1783) · `engine` `chat`

- **a store written before memory owners existed opens again on the next launch** — [#1787](https://github.com/Agent-Field/codeaf/pull/1787) · `chat` `engine`

- **senior-dev reads its window from codeaf offline, refuses tiny models, keeps progress on compaction** — [#1791](https://github.com/Agent-Field/codeaf/pull/1791) · `engine` `chat` `docs`

- **Keep AI team hover responsive with large conversation previews** — [#1794](https://github.com/Agent-Field/codeaf/pull/1794) · `chat`

- **Built-in provider lists accept appended catalog rows** — [#1795](https://github.com/Agent-Field/codeaf/pull/1795) · `chat` `docs`

- **senior-dev keeps its session store out of the folder and writes back a removed .senior-dev** — [#1800](https://github.com/Agent-Field/codeaf/pull/1800) · `engine` `docs`

- **senior-dev's ending no longer says the build and tests passed when it ran none** — [#1801](https://github.com/Agent-Field/codeaf/pull/1801) · `engine` `docs`

- **senior-dev help fits eighty columns and is checked before merging** — [#1810](https://github.com/Agent-Field/codeaf/pull/1810) · `engine` `build` `docs`

- **senior-dev checks selected-service model capacity before starting** — [#1811](https://github.com/Agent-Field/codeaf/pull/1811) · `engine` `docs`

- **task check declarations are refused before work when they cannot be rerun** — [#1812](https://github.com/Agent-Field/codeaf/pull/1812) · `engine` `docs`

- **the belt worker page fits its budget and is checked before merging** — [#1813](https://github.com/Agent-Field/codeaf/pull/1813) · `engine` `build` `docs`

- **updater acceptance follows resolved install paths and current settings** — [#1814](https://github.com/Agent-Field/codeaf/pull/1814) · `build`

- **security audits distinguish incomplete analysis from a completed search** — [#1815](https://github.com/Agent-Field/codeaf/pull/1815) · `chat` `engine`

- **hosted acceptance checks actual task endings and retains diagnostics** — [#1816](https://github.com/Agent-Field/codeaf/pull/1816) · `build`

- **review agents reserve answer time and retain failure evidence** — [#1817](https://github.com/Agent-Field/codeaf/pull/1817) · `chat` `engine` `docs`

- **review resolves model words and linked roots and formats final answers** — [#1818](https://github.com/Agent-Field/codeaf/pull/1818) · `chat` `engine` `docs`

- **shell programs price qualified model names on their selected service** — [#1819](https://github.com/Agent-Field/codeaf/pull/1819) · `engine` `docs`

- **review rejects malformed consistency claims and retains partial coverage** — [#1820](https://github.com/Agent-Field/codeaf/pull/1820) · `chat` `engine` `docs`

- **provider connections read complete bounded model catalogs** — [#1822](https://github.com/Agent-Field/codeaf/pull/1822) · `chat` `engine` `docs`

- **concurrent provider-memo saves preserve every learned fact at restart** — [#1824](https://github.com/Agent-Field/codeaf/pull/1824) · `engine` `chat`

## v0.7.1 — 2026-10-05

### Changed

- **Usage receipts distinguish routing, model families and missing counts** — [#1755](https://github.com/Agent-Field/codeaf/pull/1755) · `engine` `remote` `build` `docs`

## v0.7.0 — 2026-10-04

### Changed

- **ctrl+enter queues a follow-up, and queued messages can be taken back** — [#1714](https://github.com/Agent-Field/codeaf/pull/1714) · `chat` `engine`

- **senior-dev leaves no wip commits, writes its own commit message, and gets fixes handed back** — [#1716](https://github.com/Agent-Field/codeaf/pull/1716) · `chat` `engine` `docs`

- **first run answers clicks and a second enter, drops the telemetry notice, lists free models when low** — [#1720](https://github.com/Agent-Field/codeaf/pull/1720) · `chat` `docs`

- **touched packages run concurrently and attribute flaky or inherited failures** — [#1734](https://github.com/Agent-Field/codeaf/pull/1734) · `build` `docs`

### Fixed

- **the @ list finds recent conversations on every box without taking over prose** — [#1725](https://github.com/Agent-Field/codeaf/pull/1725) · `chat` `docs`

- **Token usage reaches telemetry while a session is still running** — [#1731](https://github.com/Agent-Field/codeaf/pull/1731) · `docs` `engine`

- **forgotten OpenAI keys and stale folder environments no longer hide a connected key** — [#1752](https://github.com/Agent-Field/codeaf/pull/1752) · `chat` `engine` `docs`

## v0.6.0 — 2026-10-01

### Added

- **senior-dev can be told something while it works, from its page or the chat, until it hands in** — [#1689](https://github.com/Agent-Field/codeaf/pull/1689) · `engine` `chat` `docs`

- **skills kept for OpenCode and Goose are read too, including their ~/.config folders** — [#1691](https://github.com/Agent-Field/codeaf/pull/1691) · `chat` `resident`

### Changed

- **Routine coding replies lead with the outcome and stay short** — [#1428](https://github.com/Agent-Field/codeaf/pull/1428) · `chat` `docs`

- **Home copies the current conversation name from its second menu action** — [#1433](https://github.com/Agent-Field/codeaf/pull/1433) · `chat` `docs`

- **Sessions filters whole conversations by name, project or nested task** — [#1434](https://github.com/Agent-Field/codeaf/pull/1434) · `chat` `docs`

- **a reply cut off mid-stream is dropped and asked again** — [#1497](https://github.com/Agent-Field/codeaf/pull/1497) · `chat` `engine`

- **Home points to the standing place for a waiting order's due time** — [#1544](https://github.com/Agent-Field/codeaf/pull/1544) · `chat` `docs`

- **the size budget is measured on darwin/amd64 and raised to 68,850,000 bytes** — [#1695](https://github.com/Agent-Field/codeaf/pull/1695) · `build`

- **Every recognised slash command is chipped wherever it stands, in the box and the sent transcript** — [#1699](https://github.com/Agent-Field/codeaf/pull/1699) · `chat` `docs`

- **dev and staging builds report to OpenRouter as their own apps; a release stays AgentField AI** — [#1723](https://github.com/Agent-Field/codeaf/pull/1723) · `chat` `engine` `build`

### Fixed

- **Provider keys no longer reach a model's shell commands** — [#1486](https://github.com/Agent-Field/codeaf/pull/1486) · `engine`

- **a late task start is an uncertain receipt** — [#1539](https://github.com/Agent-Field/codeaf/pull/1539) · `chat` `remote`

- **Startup and home conversation openings keep the interface responsive** — [#1662](https://github.com/Agent-Field/codeaf/pull/1662) · `chat` `engine`

- **the teams cap, header, keyboard and wall show the right thing** — [#1668](https://github.com/Agent-Field/codeaf/pull/1668) · `chat`

- **a job's log path is spelled the way the session spelled its folder** — [#1687](https://github.com/Agent-Field/codeaf/pull/1687) · `chat` `engine`

- **a note or say to a program that cannot read it is refused without storing the words** — [#1689](https://github.com/Agent-Field/codeaf/pull/1689) · `chat`

- **the model picker keeps the cursor's row on screen at every width** — [#1696](https://github.com/Agent-Field/codeaf/pull/1696) · `chat`

- **the current model wears no band of its own and every model list owns the wheel** — [#1696](https://github.com/Agent-Field/codeaf/pull/1696) · `chat`

- **a typed /senior-dev names its ceiling and its ending cards wear the badge** — [#1697](https://github.com/Agent-Field/codeaf/pull/1697) · `chat` `docs`

- **no zero rates, no raw ids as names, and the manual matches the screen** — [#1698](https://github.com/Agent-Field/codeaf/pull/1698) · `chat` `docs`

- **Flags after the brief, run-engine token counts, long-home ssh paths and auth failures** — [#1700](https://github.com/Agent-Field/codeaf/pull/1700) · `engine` `remote` `docs`

- **chat sign-in cancel, picture names, speech spend, palette lists and team wording are accurate** — [#1702](https://github.com/Agent-Field/codeaf/pull/1702) · `chat` `engine` `docs`

- **a carried-on run counts only its own files, and a published branch is pushed, not merged** — [#1705](https://github.com/Agent-Field/codeaf/pull/1705) · `engine` `chat` `docs`

- **a terminal resize leaves no text from the previous frame** — [#1709](https://github.com/Agent-Field/codeaf/pull/1709) · `chat` `docs`

- **a stopped run names the work its worker already committed** — [#1710](https://github.com/Agent-Field/codeaf/pull/1710) · `chat` `engine` `docs`

- **settings says checker, no zero durations or byte counts, and help and manual match the screen** — [#1711](https://github.com/Agent-Field/codeaf/pull/1711) · `chat` `docs`

- **skills context stays out of messages, rewind, reopened chats and titles** — [#1713](https://github.com/Agent-Field/codeaf/pull/1713) · `chat`

- **A shell senior-dev run with flags after the brief runs instead of failing to parse** — [#1717](https://github.com/Agent-Field/codeaf/pull/1717) · `engine`

## v0.5.1 — 2026-10-01

### Fixed

- **the first launch of the day no longer waits on the model list before it draws** — [#1724](https://github.com/Agent-Field/codeaf/pull/1724) · `chat` `engine`

## v0.5.0 — 2026-09-30

### Added

- **custom connections are named, sit beside each other, and switch from the Providers tab** — [#1089](https://github.com/Agent-Field/codeaf/pull/1089) · `chat` `engine`

- **Skill facts gain trust, cost card and digest; briefs gain an ordered skills list** — [#1327](https://github.com/Agent-Field/codeaf/pull/1327) · `engine` `resident` `chat`

- **a ChatGPT plan connects as the Codex model service, in the browser or with codeaf connect** — [#1336](https://github.com/Agent-Field/codeaf/pull/1336) · `chat` `engine` `docs`

- **/skill puts skills in front of a conversation by hand, with a chip that says which ones are on** — [#1346](https://github.com/Agent-Field/codeaf/pull/1346) · `chat`

- **the chat shows which skills each message carried** — [#1348](https://github.com/Agent-Field/codeaf/pull/1348) · `chat`

- **while a run is live the chat's turn opens on the run's rows, so a change of mind reaches the work** — [#1357](https://github.com/Agent-Field/codeaf/pull/1357) · `chat` `engine` `docs`

- **Chat and task pages share ten animated working-logo studies** — [#1384](https://github.com/Agent-Field/codeaf/pull/1384) · `chat` `docs`

- **Report total session token usage in anonymous telemetry** — [#1387](https://github.com/Agent-Field/codeaf/pull/1387) · `chat` `build` `docs`

- **Clicking a Home project selects it for the next message** — [#1422](https://github.com/Agent-Field/codeaf/pull/1422) · `chat` `docs`

- **the places bar has a chats word that goes back to the conversation** — [#1429](https://github.com/Agent-Field/codeaf/pull/1429) · `chat` `docs`

- **a team can have a manager chat, with a Traffic rail of who told whom** — [#1429](https://github.com/Agent-Field/codeaf/pull/1429) · `chat` `engine` `docs`

- **a handle on the Traffic opens the member at the message** — [#1429](https://github.com/Agent-Field/codeaf/pull/1429) · `chat` `docs`

- **staging promotion checks the selected build, and stageaf installs it beside codeaf** — [#1438](https://github.com/Agent-Field/codeaf/pull/1438) · `chat` `build` `docs`

- **codeaf reads the OpenRouter balance and starts a near-zero account on free models** — [#1440](https://github.com/Agent-Field/codeaf/pull/1440) · `chat`

- **senior-dev, a coding agent built into codeaf, takes a whole task in the folder itself** — [#1488](https://github.com/Agent-Field/codeaf/pull/1488) · `chat` `engine` `docs`

- **a Traffic row shows how long ago it happened, and a press opens that message** — [#1500](https://github.com/Agent-Field/codeaf/pull/1500) · `chat` `docs`

- **Run non-interactive shell commands with ! and see their output live** — [#1654](https://github.com/Agent-Field/codeaf/pull/1654) · `chat` `engine` `remote` `docs`

### Changed

- **one fzf-style fuzzy matcher ranks every quick-search picker, and the settings search takes spaces** — [#1089](https://github.com/Agent-Field/codeaf/pull/1089) · `chat`

- **The message-box rules** — [#1334](https://github.com/Agent-Field/codeaf/pull/1334) · `docs`

- **the node belt is the default again** — [#1340](https://github.com/Agent-Field/codeaf/pull/1340) · `engine` `chat`

- **one message carries the skills its own words choose, and a person's attachments always ride** — [#1345](https://github.com/Agent-Field/codeaf/pull/1345) · `chat`

- **the prompt says what a skill is, and a missed skill name says what the shelf holds** — [#1347](https://github.com/Agent-Field/codeaf/pull/1347) · `chat`

- **the belt measurement and the design for the chat as manager over the plan store** — [#1354](https://github.com/Agent-Field/codeaf/pull/1354) · `docs`

- **the worker harness is the default belt again, and a run has no worker bound** — [#1355](https://github.com/Agent-Field/codeaf/pull/1355) · `engine` `chat`

- **a note on a task reaches that task's running worker, and the chat can read the notes back** — [#1356](https://github.com/Agent-Field/codeaf/pull/1356) · `engine` `chat` `docs`

- **a conversation against one machine waits for it instead of giving up on it** — [#1358](https://github.com/Agent-Field/codeaf/pull/1358) · `engine` `chat`

- **persistedCount says in its own doc that it destroys the difference between unset and zero** — [#1369](https://github.com/Agent-Field/codeaf/pull/1369) · `engine`

- **the Slack thread limit is written down once, and the description a model reads interpolates it** — [#1371](https://github.com/Agent-Field/codeaf/pull/1371) · `engine` `remote`

- **Home returns to double-space navigation and the ask-here choice** — [#1388](https://github.com/Agent-Field/codeaf/pull/1388) · `chat` `docs`

- **one list of twenty-two tips, said two ways — home rotates them, a conversation ranks them** — [#1389](https://github.com/Agent-Field/codeaf/pull/1389) · `chat` `docs`

- **Claude Code plugin and Codex skills reach the chat, memory off included** — [#1396](https://github.com/Agent-Field/codeaf/pull/1396) · `chat` `engine` `remote`

- **codeaf always signs what it writes, and names only the bare model in `Assisted-by`** — [#1412](https://github.com/Agent-Field/codeaf/pull/1412) · `chat` `engine` `resident` `docs`

- **a run's rows on the rail and its page look like every other task** — [#1420](https://github.com/Agent-Field/codeaf/pull/1420) · `chat`

- **the places bar sits where the chat strip does** — [#1429](https://github.com/Agent-Field/codeaf/pull/1429) · `chat` `docs`

- **the tab strip reads Home, the team chip, the manager, then the tabs** — [#1429](https://github.com/Agent-Field/codeaf/pull/1429) · `chat` `docs`

- **team Traffic is threaded, the newest thread at the top, and a manager's question keeps its answers** — [#1429](https://github.com/Agent-Field/codeaf/pull/1429) · `chat` `engine` `docs`

- **engine --status, the older engine gives up the slot, held chats always move, --max-hours ends it** — [#1430](https://github.com/Agent-Field/codeaf/pull/1430) · `engine` `remote` `chat`

- **a task's crew is routed per task, and /crew is the panel of what is allowed and pinned** — [#1436](https://github.com/Agent-Field/codeaf/pull/1436) · `engine` `chat` `docs`

- **Every task opens the one task room, on either engine** — [#1485](https://github.com/Agent-Field/codeaf/pull/1485) · `chat` `docs`

- **a conversation's tip covers the project on the keys row, with a cross, after 15 quiet seconds** — [#1487](https://github.com/Agent-Field/codeaf/pull/1487) · `chat` `docs`

- **one side column holds Tasks and Traffic in every chat** — [#1494](https://github.com/Agent-Field/codeaf/pull/1494) · `chat` `docs`

- **a standing card names its kind, and each button says what it will do** — [#1494](https://github.com/Agent-Field/codeaf/pull/1494) · `chat` `engine`

- **A hovered tab's full title sits on the blank row and ends under its tab** — [#1496](https://github.com/Agent-Field/codeaf/pull/1496) · `chat` `docs`

- **tui3 head grows an air row under the top navigation line** — [#1509](https://github.com/Agent-Field/codeaf/pull/1509) · `chat`

- **unify provider vocabulary, background-list connected provider models, and ease adding providers** — [#1513](https://github.com/Agent-Field/codeaf/pull/1513) · `chat` `docs`

- **clarify when accepting a standing card repeats work** — [#1520](https://github.com/Agent-Field/codeaf/pull/1520) · `docs`

- **chat keeps messages readable and folds operational work across every conversation view** — [#1607](https://github.com/Agent-Field/codeaf/pull/1607) · `chat` `engine` `docs`

- **the installer walks a person through the first minute and leaves codeaf ready in the same terminal** — [#1635](https://github.com/Agent-Field/codeaf/pull/1635) · `build` `docs`

- **compaction can end with a summary, refused requests recover, and a summary leaves a visible line** — [#1658](https://github.com/Agent-Field/codeaf/pull/1658) · `chat` `engine` `remote`

- **senior-dev works in a private worktree copy that starts from where you are** — [#1674](https://github.com/Agent-Field/codeaf/pull/1674) · `chat` `docs`

### Renamed

- **Home names its standing-order panel standing** — [#1415](https://github.com/Agent-Field/codeaf/pull/1415) · `chat` `docs`

### Fixed

- **a compaction pass that found nothing to do no longer takes the conversation away** — [#1341](https://github.com/Agent-Field/codeaf/pull/1341) · `chat` `engine`

- **a replay draws the record once, whatever was already on the screen** — [#1342](https://github.com/Agent-Field/codeaf/pull/1342) · `chat`

- **a quiet reply from the only machine there is gets asked again, with a wait** — [#1343](https://github.com/Agent-Field/codeaf/pull/1343) · `engine`

- **a run's row on the tasks place is the store's task, wears its state and opens its page** — [#1359](https://github.com/Agent-Field/codeaf/pull/1359) · `chat` `engine`

- **the picture hand refuses a prompt that is not there** — [#1372](https://github.com/Agent-Field/codeaf/pull/1372) · `engine` `chat`

- **skills that are switched off say so instead of looking like a feature that was never built** — [#1380](https://github.com/Agent-Field/codeaf/pull/1380) · `chat` `engine`

- **/cost and every multi-row note keep their columns — the gutter moves every row, not just the first** — [#1390](https://github.com/Agent-Field/codeaf/pull/1390) · `chat`

- **one machine is waited on, a pool is never one machine, and the suite lock is seen and freed** — [#1392](https://github.com/Agent-Field/codeaf/pull/1392) · `engine` `chat` `build`

- **the chat's run digest leads with the live run, in the side list's words, on every message** — [#1393](https://github.com/Agent-Field/codeaf/pull/1393) · `chat` `engine` `docs`

- **the task page's keys, its note and its tab mark say one thing, and three catalog warms are owned** — [#1395](https://github.com/Agent-Field/codeaf/pull/1395) · `chat` `engine`

- **a Codex model carries its 272k window, and a Codex call row names Codex as its endpoint** — [#1402](https://github.com/Agent-Field/codeaf/pull/1402) · `chat` `engine` `docs`

- **run workers honour machine limits, keep private records, and finish at their step cap** — [#1410](https://github.com/Agent-Field/codeaf/pull/1410) · `chat` `engine`

- **run workers wait for their step to be recorded and their notes delivered before acting again** — [#1410](https://github.com/Agent-Field/codeaf/pull/1410) · `engine` `docs`

- **a new task never picks up an earlier run, and every run ending is written on its record** — [#1411](https://github.com/Agent-Field/codeaf/pull/1411) · `chat` `engine`

- **codeaf do on the run engine edits in place, commits nothing, and stops at a price** — [#1416](https://github.com/Agent-Field/codeaf/pull/1416) · `engine` `chat`

- **tasks approved together are one run, and a run that could not start says so** — [#1417](https://github.com/Agent-Field/codeaf/pull/1417) · `chat` `engine`

- **a large paste no longer freezes the box for twenty seconds** — [#1419](https://github.com/Agent-Field/codeaf/pull/1419) · `chat`

- **a deliverable path on another machine no longer refuses the task** — [#1421](https://github.com/Agent-Field/codeaf/pull/1421) · `engine`

- **The home tab keeps its spelling and position across views** — [#1424](https://github.com/Agent-Field/codeaf/pull/1424) · `chat` `docs`

- **Closing a task removes it from the current Sessions results immediately** — [#1425](https://github.com/Agent-Field/codeaf/pull/1425) · `chat` `docs`

- **Home conversations keep their working mark while tasks run** — [#1426](https://github.com/Agent-Field/codeaf/pull/1426) · `chat` `docs`

- **with the manager in front the right column is the traffic** — [#1429](https://github.com/Agent-Field/codeaf/pull/1429) · `chat` `docs`

- **Conversation drafts and attachments stay with their conversation** — [#1431](https://github.com/Agent-Field/codeaf/pull/1431) · `chat` `docs`

- **a team's wrap-up survives a restart, and a cap packet is raised once** — [#1494](https://github.com/Agent-Field/codeaf/pull/1494) · `engine`

- **worker commits carry attribution when a run ends** — [#1495](https://github.com/Agent-Field/codeaf/pull/1495) · `chat` `engine`

- **the /crew panel's weak-checker warning counts a pinned checker** — [#1501](https://github.com/Agent-Field/codeaf/pull/1501) · `chat` `docs`

- **a connect offer shows its sign-in reason together with the waiting state** — [#1502](https://github.com/Agent-Field/codeaf/pull/1502) · `engine`

- **a reminder's time reads with a middle dot, and Traffic ages stay short past a month** — [#1503](https://github.com/Agent-Field/codeaf/pull/1503) · `chat`

- **a message that carries skills shows your words, not the skills' description sheet** — [#1504](https://github.com/Agent-Field/codeaf/pull/1504) · `chat`

- **enter takes the answer the pointer stands on, on a standing card** — [#1506](https://github.com/Agent-Field/codeaf/pull/1506) · `chat`

- **one wake per member reply, team_stop reaches background members, a demoted manager loses its verbs** — [#1514](https://github.com/Agent-Field/codeaf/pull/1514) · `chat` `engine` `remote`

- **a task's worker can stop its own job, and a run's checker checks its own copy** — [#1515](https://github.com/Agent-Field/codeaf/pull/1515) · `chat` `engine`

- **wall tiles say what each conversation spent, and the teams surface stops saying untrue things** — [#1516](https://github.com/Agent-Field/codeaf/pull/1516) · `chat` `engine`

- **runs land every file by its real name, --one-model reaches every run seat, caches stay out** — [#1517](https://github.com/Agent-Field/codeaf/pull/1517) · `chat` `engine`

- **crew follow-ups — checker ceiling by seat, route pins beside the tier row, landing line at the end** — [#1518](https://github.com/Agent-Field/codeaf/pull/1518) · `chat` `engine` `docs`

- **a devaf first run reaches its own binary, shows the usage notice on screen, prints no raw lines** — [#1519](https://github.com/Agent-Field/codeaf/pull/1519) · `build` `chat` `engine`

- **senior-dev works in plain folders and resolves shell model names** — [#1530](https://github.com/Agent-Field/codeaf/pull/1530) · `chat`

- **telemetry --help exits 0, the manual names six --help groups, a wall tile shows its first age** — [#1533](https://github.com/Agent-Field/codeaf/pull/1533) · `chat` `docs`

- **a side row hides its clock while its page is open; senior-dev manual matches its row and card** — [#1534](https://github.com/Agent-Field/codeaf/pull/1534) · `chat` `docs`

- **Home search's card follows the pointer and returns to the cursor's match** — [#1541](https://github.com/Agent-Field/codeaf/pull/1541) · `chat` `docs`

- **a spend under a hundredth of a cent reads <$0.0001 on every receipt, never $0.0000** — [#1542](https://github.com/Agent-Field/codeaf/pull/1542) · `chat` `docs`

- **task proposal countdown properly handles keypress and avoids starting declined task** — [#1547](https://github.com/Agent-Field/codeaf/pull/1547) · `chat`

- **standing execution enforces git branch isolation and worktree validation** — [#1560](https://github.com/Agent-Field/codeaf/pull/1560) · `engine`

- **plandb done refuses terminal tasks and manager tasks derive project workspace** — [#1562](https://github.com/Agent-Field/codeaf/pull/1562) · `engine` `resident`

- **Slash command completion executes bare /task, /redo, and /workspace immediately** — [#1563](https://github.com/Agent-Field/codeaf/pull/1563) · `chat`

- **interactive chat turns and task crew delegations enforce the daily spend rail** — [#1595](https://github.com/Agent-Field/codeaf/pull/1595) · `chat` `engine`

- **standing order project scoping normalization, live rail allowance words, and task activity labels** — [#1596](https://github.com/Agent-Field/codeaf/pull/1596) · `chat` `engine`

- **manager conversation initialization, handle fallback, and approval posture inheritance** — [#1597](https://github.com/Agent-Field/codeaf/pull/1597) · `chat` `engine`

- **plan-born task worker briefs carry the project's standing orders** — [#1598](https://github.com/Agent-Field/codeaf/pull/1598) · `chat` `engine`

- **bound foreground bash spill files and preserve their initial output** — [#1603](https://github.com/Agent-Field/codeaf/pull/1603) · `chat` `engine`

- **bound background logs and keep recursive searches out of runtime output** — [#1603](https://github.com/Agent-Field/codeaf/pull/1603) · `chat` `engine`

- **blank terminal briefs stop before planning or model work** — [#1604](https://github.com/Agent-Field/codeaf/pull/1604) · `engine` `docs`

- **Single-model chat defers independent Model Pool judging** — [#1610](https://github.com/Agent-Field/codeaf/pull/1610) · `chat`

- **one-time standing approvals preserve the action and report approval truthfully** — [#1612](https://github.com/Agent-Field/codeaf/pull/1612) · `chat` `engine`

- **Session close joins its routing-cache refresh** — [#1616](https://github.com/Agent-Field/codeaf/pull/1616) · `engine`

- **Held tasks stop cleanly and recover their exact working folder** — [#1619](https://github.com/Agent-Field/codeaf/pull/1619) · `engine` `chat`

- **First standing approval preserves the shared timer's owner** — [#1622](https://github.com/Agent-Field/codeaf/pull/1622) · `engine`

- **paid reasoning-ceiling retries remain in session and budget accounting** — [#1625](https://github.com/Agent-Field/codeaf/pull/1625) · `chat`

- **CLI help names task model choices and unattended chat budgets** — [#1626](https://github.com/Agent-Field/codeaf/pull/1626) · `docs`

- **first-run judge fixture joins its usage writer before home cleanup** — [#1627](https://github.com/Agent-Field/codeaf/pull/1627) · `chat`

- **hedge fixtures hold the primary before its first visible word** — [#1627](https://github.com/Agent-Field/codeaf/pull/1627) · `engine`

- **Opening a task room gives its note box the keyboard** — [#1628](https://github.com/Agent-Field/codeaf/pull/1628) · `chat` `docs`

- **Home tasks keep the project displayed before submission** — [#1629](https://github.com/Agent-Field/codeaf/pull/1629) · `chat`

- **A reopened hosted conversation draws a completed reply once** — [#1632](https://github.com/Agent-Field/codeaf/pull/1632) · `chat` `engine`

- **Home tasks, standing work, and traffic keep their context and navigation** — [#1632](https://github.com/Agent-Field/codeaf/pull/1632) · `chat` `engine`

- **a check naming issue_.go is sent back, and a file nothing makes starts no paid fix** — [#1634](https://github.com/Agent-Field/codeaf/pull/1634) · `engine` `docs`

- **the manual's standing, pool, task-card, team-start and first-run pages say what the code does** — [#1639](https://github.com/Agent-Field/codeaf/pull/1639) · `chat` `docs`

- **a task that lands mid-answer is said after it, and Done draws every task its heading counts** — [#1640](https://github.com/Agent-Field/codeaf/pull/1640) · `chat` `docs`

- **a stopped background job reads as ended at once, and its log-folder cleanup runs just after it** — [#1641](https://github.com/Agent-Field/codeaf/pull/1641) · `chat` `engine`

- **Tab selects the highlighted slash command on Home and in conversations** — [#1642](https://github.com/Agent-Field/codeaf/pull/1642) · `chat` `docs`

- **Completed task forks retire their Furrow timelines before their directories** — [#1651](https://github.com/Agent-Field/codeaf/pull/1651) · `engine`

- **Conversation names skip shell commands and task-only chats remain resumable** — [#1667](https://github.com/Agent-Field/codeaf/pull/1667) · `chat` `engine` `docs`

- **Budget refusals name the limit that stopped the conversation** — [#1671](https://github.com/Agent-Field/codeaf/pull/1671) · `chat` `engine` `docs`

- **a closed conversation has no memory pass still reading or writing its brain** — [#1673](https://github.com/Agent-Field/codeaf/pull/1673) · `engine`

- **a stopped answer can still be opened after reopening its conversation** — [#1681](https://github.com/Agent-Field/codeaf/pull/1681) · `chat` `docs`

- **do --json answers an early refusal, no unasked merge after senior-dev, manual truth** — [#1682](https://github.com/Agent-Field/codeaf/pull/1682) · `chat` `engine` `docs` `build`

### Removed

- **the installer says nothing about telemetry** — [#1488](https://github.com/Agent-Field/codeaf/pull/1488) · `build` `docs`

- **Search lives on Home instead of a separate page and slash command** — [#1650](https://github.com/Agent-Field/codeaf/pull/1650) · `chat` `docs`

## v0.4.1 — 2026-09-22

### Fixed

- **a waiting message stays waiting when you leave the chat, and still goes when its answer ends** — [#1385](https://github.com/Agent-Field/codeaf/pull/1385) · `chat`

## v0.4.0 — 2026-09-22

### Added

- **the worker harness — the bash belt, the plan store on SQLite, the shell doors** — [#1109](https://github.com/Agent-Field/codeaf/pull/1109) · `engine` `chat` `build`

- **the worker harness — pause, person notes, finish gate, archive, spend, crew arms** — [#1109](https://github.com/Agent-Field/codeaf/pull/1109) · `engine` `build`

- **a run nothing is driving can be carried on, in the copy its work is already in** — [#1305](https://github.com/Agent-Field/codeaf/pull/1305) · `engine`

- **a dev build installs beside codeaf as devaf and follows the dev channel** — [#1333](https://github.com/Agent-Field/codeaf/pull/1333) · `build` `chat` `docs`

### Changed

- **UX changes — consistent message boxes, Home sessions and task trees** — [#1071](https://github.com/Agent-Field/codeaf/pull/1071) · `chat` `engine` `remote`

- **The model picker is a table with headed columns, not a ragged tail of facts** — [#1107](https://github.com/Agent-Field/codeaf/pull/1107) · `chat`

- **the worker harness replaces the task engine under /task and do** — [#1109](https://github.com/Agent-Field/codeaf/pull/1109) · `docs` `engine` `chat`

- **A task is one worker and a plan store; the older node engine is deleted** — [#1109](https://github.com/Agent-Field/codeaf/pull/1109) · `engine` `chat` `build`

- **the worker harness — the work seat, the belt's pages, parent wakes, the run's store** — [#1109](https://github.com/Agent-Field/codeaf/pull/1109) · `engine` `chat` `remote`

- **Decision dialogs support updated requests and concurrent clarification** — [#1146](https://github.com/Agent-Field/codeaf/pull/1146) · `chat` `engine` `remote` `docs`

- **the curated lexer set loads on first use, not all 279 at init** — [#1193](https://github.com/Agent-Field/codeaf/pull/1193) · `chat`

- **the task rail and the task page draw a belt run's plan as a tree, and the work tab is designed** — [#1204](https://github.com/Agent-Field/codeaf/pull/1204) · `chat` `engine`

- **a belt landing no longer wakes the chat, the conversation gains a work tab, three hand-off rules** — [#1205](https://github.com/Agent-Field/codeaf/pull/1205) · `chat` `engine`

- **the installer prints a receipt, a three-line telemetry notice, and the bare export PATH line last** — [#1208](https://github.com/Agent-Field/codeaf/pull/1208) · `build` `docs`

- **chat answers number their steps, say where a job stands, and end on one next action** — [#1209](https://github.com/Agent-Field/codeaf/pull/1209) · `chat`

- **a proposal learns every problem in one refusal, and a task stands where its brief plainly says** — [#1214](https://github.com/Agent-Field/codeaf/pull/1214) · `chat` `engine`

- **The telemetry off switch quiets the Model Pool too, and `codeaf telemetry show` prints both streams** — [#1214](https://github.com/Agent-Field/codeaf/pull/1214) · `chat` `engine` `docs`

- **a run's step rows show the work and leave out the run's own bookkeeping** — [#1257](https://github.com/Agent-Field/codeaf/pull/1257) · `chat` `engine`

- **quick-search rows draw the matched letters in bold, and internal/fuzzy says where each term hit** — [#1321](https://github.com/Agent-Field/codeaf/pull/1321) · `chat`

- **one fzf-style fuzzy matcher ranks every quick-search picker, and the settings search takes spaces** — [#1321](https://github.com/Agent-Field/codeaf/pull/1321) · `chat`

- **the commit co-author links to the CodeAF account, and assisted-by names the model** — [#1325](https://github.com/Agent-Field/codeaf/pull/1325) · `engine` `docs`

### Fixed

- **a belt check runs on the crew checking seat, not the thinking seat** — [#1207](https://github.com/Agent-Field/codeaf/pull/1207) · `chat`

- **a run's result is read from the store when the root's worker returns after the run ended** — [#1210](https://github.com/Agent-Field/codeaf/pull/1210) · `engine`

- **a landing's report counts as evidence, and a check refused for its cd says the form that passes** — [#1212](https://github.com/Agent-Field/codeaf/pull/1212) · `chat` `engine`

- **a reopened conversation keeps its tasks, and a task file that cannot be read is never overwritten** — [#1213](https://github.com/Agent-Field/codeaf/pull/1213) · `chat` `engine`

- **a check is one command as the shell reads its quotes, and its refusal says what passes** — [#1215](https://github.com/Agent-Field/codeaf/pull/1215) · `chat` `engine`

- **a review conclusion is judged by the commands the worker declared and the checker ran** — [#1220](https://github.com/Agent-Field/codeaf/pull/1220) · `chat`

- **the conversation's tasks tool reads a run's tasks, by the names the rail shows** — [#1222](https://github.com/Agent-Field/codeaf/pull/1222) · `chat` `engine`

- **unread profile config keys are reported once at load** — [#1223](https://github.com/Agent-Field/codeaf/pull/1223) · `engine`

- **a resident dispatch pass does not run again the leaf it just released** — [#1225](https://github.com/Agent-Field/codeaf/pull/1225) · `engine`

- **a place sweep note can no longer write after the process it belongs to closes** — [#1227](https://github.com/Agent-Field/codeaf/pull/1227) · `chat`

- **a hosted conversation reads and steers its run** — [#1230](https://github.com/Agent-Field/codeaf/pull/1230) · `chat` `engine` `remote`

- **a task's page opens with the task, and a run's rows on the rail open their pages** — [#1231](https://github.com/Agent-Field/codeaf/pull/1231) · `chat`

- **a run works in a copy of its own, and its work comes home when it ends** — [#1232](https://github.com/Agent-Field/codeaf/pull/1232) · `chat` `engine`

- **a self-finished codeaf do run seats its review before it ends** — [#1233](https://github.com/Agent-Field/codeaf/pull/1233) · `engine`

- **every run a conversation has made stays readable** — [#1234](https://github.com/Agent-Field/codeaf/pull/1234) · `chat` `engine`

- **a wait whose stream has stopped arriving paints at the spinner cadence, not thirty frames a second** — [#1235](https://github.com/Agent-Field/codeaf/pull/1235) · `chat`

- **a reconnect reuses its registered identity instead of introducing itself again** — [#1236](https://github.com/Agent-Field/codeaf/pull/1236) · `chat`

- **chat names unread config keys once before using default models** — [#1239](https://github.com/Agent-Field/codeaf/pull/1239) · `chat` `engine`

- **a task's page says only what is true of it** — [#1240](https://github.com/Agent-Field/codeaf/pull/1240) · `chat`

- **what is typed after a press on a run's row goes to its page, never the conversation** — [#1244](https://github.com/Agent-Field/codeaf/pull/1244) · `chat`

- **unread-config notice skips keys the product itself retired** — [#1245](https://github.com/Agent-Field/codeaf/pull/1245) · `engine` `chat`

- **stop it ends a run, from the card, from the run's own page, and from the chat** — [#1250](https://github.com/Agent-Field/codeaf/pull/1250) · `chat` `engine`

- **a press on a run's row opens its page at once, and an open page reads on a beat** — [#1251](https://github.com/Agent-Field/codeaf/pull/1251) · `chat`

- **drawing the side list never waits on the engine for a run's rows** — [#1255](https://github.com/Agent-Field/codeaf/pull/1255) · `chat`

- **a run or a part you stopped reads stopped on its page, not incomplete** — [#1258](https://github.com/Agent-Field/codeaf/pull/1258) · `chat` `engine`

- **a call that did not run is no longer drawn as a step that ran** — [#1260](https://github.com/Agent-Field/codeaf/pull/1260) · `chat` `engine`

- **a task's step numbers continue across a wake** — [#1261](https://github.com/Agent-Field/codeaf/pull/1261) · `chat` `engine`

- **a time limit a person sets now ends a run, by the same road as the cost limit** — [#1262](https://github.com/Agent-Field/codeaf/pull/1262) · `chat` `engine`

- **the dim line under a step row is only drawn when it must be the row's own** — [#1265](https://github.com/Agent-Field/codeaf/pull/1265) · `chat` `engine`

- **a worker that has stopped making progress is told once, and then ends, well short of the step cap** — [#1266](https://github.com/Agent-Field/codeaf/pull/1266) · `engine` `chat`

- **closing chat now stops and joins the pool judge sweep** — [#1267](https://github.com/Agent-Field/codeaf/pull/1267) · `chat`

- **a run's dollar limit now holds while a worker is still working** — [#1268](https://github.com/Agent-Field/codeaf/pull/1268) · `chat` `engine`

- **a parked task is finished only by a worker woken for it** — [#1271](https://github.com/Agent-Field/codeaf/pull/1271) · `engine`

- **a headless launch and task-owner open reach a restarting host, not a crashed one** — [#1272](https://github.com/Agent-Field/codeaf/pull/1272) · `chat` `engine`

- **a backslash pair inside double quotes is text to the one-command reader** — [#1273](https://github.com/Agent-Field/codeaf/pull/1273) · `engine` `chat`

- **closing a process cancels and joins lazy catalog warming before a late cache write** — [#1274](https://github.com/Agent-Field/codeaf/pull/1274) · `engine`

- **no run answers done while a finished piece of work has had no review** — [#1275](https://github.com/Agent-Field/codeaf/pull/1275) · `engine` `chat`

- **closing chat stops the place sweep before it can change the home again** — [#1276](https://github.com/Agent-Field/codeaf/pull/1276) · `chat` `engine`

- **an ending names the limit that caused it** — [#1279](https://github.com/Agent-Field/codeaf/pull/1279) · `engine` `chat`

- **a run's dollars reach the conversation's own total** — [#1280](https://github.com/Agent-Field/codeaf/pull/1280) · `engine` `chat`

- **a run is handed what is left of every dollar limit** — [#1281](https://github.com/Agent-Field/codeaf/pull/1281) · `engine` `chat`

- **a cancelled command is ended by the call that ran it** — [#1282](https://github.com/Agent-Field/codeaf/pull/1282) · `engine` `chat`

- **a long declared check says why it was refused** — [#1283](https://github.com/Agent-Field/codeaf/pull/1283) · `chat` `engine`

- **the heavy-suite lock is held for the suite, not for what the suite leaves behind** — [#1284](https://github.com/Agent-Field/codeaf/pull/1284) · `build`

- **a usage flush says whether it drained or ran out of time** — [#1285](https://github.com/Agent-Field/codeaf/pull/1285) · `engine`

- **a row that needs you names what it needed and opens the item** — [#1288](https://github.com/Agent-Field/codeaf/pull/1288) · `chat`

- **a task's page draws its brief through the reader the transcript already uses** — [#1289](https://github.com/Agent-Field/codeaf/pull/1289) · `chat`

- **a standing firing runs behind the gate a task node runs behind** — [#1290](https://github.com/Agent-Field/codeaf/pull/1290) · `engine`

- **a live run counts as work, so its conversation is not retired out from under it** — [#1291](https://github.com/Agent-Field/codeaf/pull/1291) · `chat` `engine`

- **a session stopped on a sub-harness question says so instead of working** — [#1293](https://github.com/Agent-Field/codeaf/pull/1293) · `chat` `engine`

- **work nothing is driving comes back interrupted, not failed and stopped** — [#1296](https://github.com/Agent-Field/codeaf/pull/1296) · `chat` `engine`

- **a run's working copy is written down, so carrying it on adopts the work instead of deleting it** — [#1298](https://github.com/Agent-Field/codeaf/pull/1298) · `chat` `engine`

- **the interrupted page no longer offers two answers nobody can press** — [#1304](https://github.com/Agent-Field/codeaf/pull/1304) · `chat`

- **a watch stopped for permission says the line, and no longer says a person asked** — [#1314](https://github.com/Agent-Field/codeaf/pull/1314) · `chat`

- **a sign-in and a harness ask say what they are waiting for, instead of only that somebody is needed** — [#1315](https://github.com/Agent-Field/codeaf/pull/1315) · `engine` `chat`

- **the front tab and the tab beside it agree about whether one conversation needs a person** — [#1316](https://github.com/Agent-Field/codeaf/pull/1316) · `chat`

- **the caret parks in the box the person is typing into, and hides on surfaces with no box** — [#1321](https://github.com/Agent-Field/codeaf/pull/1321) · `chat`

- **a landing whose accept is already in flight stops asking the person to accept it** — [#1322](https://github.com/Agent-Field/codeaf/pull/1322) · `engine` `chat`

- **a heavy suite takes both the file lock and the older directory lock, so a checkout behind** — [#1324](https://github.com/Agent-Field/codeaf/pull/1324) · `build`

- **the drafted-with footer links to the codeaf repository** — [#1329](https://github.com/Agent-Field/codeaf/pull/1329) · `engine` `docs`

- **a landing that turned the work back offers its answer without standing in needs you** — [#1331](https://github.com/Agent-Field/codeaf/pull/1331) · `engine` `chat`

- **a changed directory in a nested module is not a package of the root module** — [#1332](https://github.com/Agent-Field/codeaf/pull/1332) · `build`

- **hosted chat stops the live run it can see** — [#1376](https://github.com/Agent-Field/codeaf/pull/1376) · `chat` `engine` `remote`

## v0.3.0 — 2026-09-18

### Added

- **A model family toggle under the crew, so a preset can answer from open weights or from everything** — [#1073](https://github.com/Agent-Field/codeaf/pull/1073) · `chat` `engine`

- **custom connections are named, sit beside each other, and switch from the Providers tab** — [#1089](https://github.com/Agent-Field/codeaf/pull/1089) · `chat` `engine`

- **The crew's seats are picked from a row — table, catalog or learn** — [#1093](https://github.com/Agent-Field/codeaf/pull/1093) · `chat` `engine`

- **Three pure packages — a crew picker, an additive tally and a measurement-index reader** — [#1093](https://github.com/Agent-Field/codeaf/pull/1093) · `engine`

- **The pool client gains the relay's real addresses, an install nonce, and the index signer's key** — [#1093](https://github.com/Agent-Field/codeaf/pull/1093) · `engine`

- **Three more `internal/pool` packages — an outbox, a settings resolver and a signed-document fetcher** — [#1093](https://github.com/Agent-Field/codeaf/pull/1093) · `engine`

- **a landed task is scored by a model outside the crew into the own sheet, outbox and judge seat** — [#1093](https://github.com/Agent-Field/codeaf/pull/1093) · `chat` `engine`

- **A pure package that scores a finished run seat by seat, by a judge outside the crew** — [#1093](https://github.com/Agent-Field/codeaf/pull/1093) · `engine`

- **A pure package that records a run's judged scores into an own sheet and outbox rows** — [#1093](https://github.com/Agent-Field/codeaf/pull/1093) · `engine`

- **A Cloudflare Worker relay that folds installs' rows into a signed, versioned pool index** — [#1093](https://github.com/Agent-Field/codeaf/pull/1093) · `engine`

- **The Model Pool's index is seated at start-up from a seed in the binary** — [#1093](https://github.com/Agent-Field/codeaf/pull/1093) · `chat` `engine`

- **A tier row that says `auto` computes its model from the catalog** — [#1093](https://github.com/Agent-Field/codeaf/pull/1093) · `chat` `engine`

- **The Model Pool reaches its first surface — a `model_pool` setting and a `codeaf pool` verb** — [#1102](https://github.com/Agent-Field/codeaf/pull/1102) · `chat` `engine`

- **a headless exec run leaves a pending judge record at its tail for the pool's sweep** — [#1137](https://github.com/Agent-Field/codeaf/pull/1137) · `engine`

- **the headless task door leaves a pending judge record at its tail** — [#1138](https://github.com/Agent-Field/codeaf/pull/1138) · `engine`

- **a `codeaf do` errand leaves a pending landing for the Model Pool's judge** — [#1139](https://github.com/Agent-Field/codeaf/pull/1139) · `engine`

- **pool status says what is waiting for a judge and what the last sweep did** — [#1142](https://github.com/Agent-Field/codeaf/pull/1142) · `engine`

- **The seed index is regenerated from the relay by one in-repo command** — [#1148](https://github.com/Agent-Field/codeaf/pull/1148) · `chat` `engine`

- **pool show --cells lists the held index's cells with their installs and dims** — [#1149](https://github.com/Agent-Field/codeaf/pull/1149) · `engine`

- **a chat surface now runs under a soft memory limit drawn from the machine** — [#1158](https://github.com/Agent-Field/codeaf/pull/1158) · `chat`

- **the relay can purge fixture vendors and refuse them at the door** — [#1167](https://github.com/Agent-Field/codeaf/pull/1167) · `engine`

- **pool status says the install identity, the judged total, and that nothing has landed** — [#1169](https://github.com/Agent-Field/codeaf/pull/1169) · `chat`

- **A non-verified `codeaf do` run names its kept branch and its verdict** — [#1182](https://github.com/Agent-Field/codeaf/pull/1182) · `engine`

- **The `tasks` text and the record line name a failed node's kept branch and verdict** — [#1184](https://github.com/Agent-Field/codeaf/pull/1184) · `engine`

- **A non-verified `codeaf exec` or `codeaf run` run names its kept branch and its verdict** — [#1186](https://github.com/Agent-Field/codeaf/pull/1186) · `engine`

- **a job subtree is cut when it passes its CPU or process bound** — [#1190](https://github.com/Agent-Field/codeaf/pull/1190) · `chat` `engine`

### Changed

- **the coverage round buys once, and the envelope says when the requested work was done** — [#1093](https://github.com/Agent-Field/codeaf/pull/1093) · `resident` `docs`

- **The crew's default family is `all`, and both families' worker, careful and mastermind seats moved** — [#1093](https://github.com/Agent-Field/codeaf/pull/1093) · `chat` `engine`

- **Six cells of the fixed crew tables moved to the measured seats** — [#1093](https://github.com/Agent-Field/codeaf/pull/1093) · `chat` `engine`

- **The fuel meter asks an installed tariff first, and its table holds the ids this build ships** — [#1093](https://github.com/Agent-Field/codeaf/pull/1093) · `engine`

- **A re-dispatch after running out is granted more room, and one that banks nothing new is refused** — [#1093](https://github.com/Agent-Field/codeaf/pull/1093) · `resident` `docs`

- **Every usage row names the seat the call ran under** — [#1093](https://github.com/Agent-Field/codeaf/pull/1093) · `engine`

- **The crew picker blends a measured pool rating into a seat's quality by its observation count** — [#1101](https://github.com/Agent-Field/codeaf/pull/1101) · `engine`

- **The README telemetry section is one collapsed block at the end of Docs** — [#1115](https://github.com/Agent-Field/codeaf/pull/1115) · `docs`

- **a launch compiles 78 fewer patterns, and a surface holds 8 Ps instead of every core** — [#1125](https://github.com/Agent-Field/codeaf/pull/1125) · `chat` `engine` `build`

- **the relay carries the acceptable metric beside the judge's opinion** — [#1129](https://github.com/Agent-Field/codeaf/pull/1129) · `build`

- **the relay writes a submit batch once per distinct cell and day** — [#1136](https://github.com/Agent-Field/codeaf/pull/1136) · `engine`

- **pool show and verify say both metrics the index carries** — [#1141](https://github.com/Agent-Field/codeaf/pull/1141) · `engine`

- **the seed index follows the relay's first clean publish** — [#1180](https://github.com/Agent-Field/codeaf/pull/1180) · `chat` `engine`

- **a settle turn runs under the checker's own bound, not the run's wall** — [#1183](https://github.com/Agent-Field/codeaf/pull/1183) · `chat`

- **the status word says working while a task subtree still turns at rest** — [#1191](https://github.com/Agent-Field/codeaf/pull/1191) · `chat`

### Renamed

- **the /connect row for a custom service reads Custom OpenAI-compatible API, not Something else** — [#1107](https://github.com/Agent-Field/codeaf/pull/1107) · `chat` `docs`

### Fixed

- **a finishing task can read the evidence its context has archived** — [#808](https://github.com/Agent-Field/codeaf/pull/808) · `chat` `engine`

- **a question's own answer box answers to the whole caret vocabulary, not a hand-rolled copy of it** — [#1032](https://github.com/Agent-Field/codeaf/pull/1032) · `chat` `docs`

- **an answered landing carries its answer's fate, and an in-flight settle holds the question down** — [#1086](https://github.com/Agent-Field/codeaf/pull/1086) · `chat` `engine`

- **the headless doors wait for the catalog rows a pick reads** — [#1093](https://github.com/Agent-Field/codeaf/pull/1093) · `engine`

- **No git command runs without a directory, and the checkout guard reads a linked worktree's own share** — [#1093](https://github.com/Agent-Field/codeaf/pull/1093) · `engine`

- **A node retry asks the router to avoid the upstream machine that just failed it** — [#1094](https://github.com/Agent-Field/codeaf/pull/1094) · `engine`

- **Usage counts from a hosted chat carry real numbers, and nothing without a version ever leaves** — [#1111](https://github.com/Agent-Field/codeaf/pull/1111) · `chat` `docs`

- **a service connected mid-conversation reaches the conversation that is open** — [#1114](https://github.com/Agent-Field/codeaf/pull/1114) · `chat` `engine`

- **a decision card under --yolo takes its own default instead of waiting for somebody** — [#1117](https://github.com/Agent-Field/codeaf/pull/1117) · `chat`

- **a design's thread stops its parts before it closes, and a node machinery cut says what cut it** — [#1118](https://github.com/Agent-Field/codeaf/pull/1118) · `chat` `engine`

- **a bare auto row names the pick word's rung, so a seat computed under learn says learned** — [#1119](https://github.com/Agent-Field/codeaf/pull/1119) · `chat` `engine`

- **a bare auto row under picked from = catalog reads the catalog's figures alone** — [#1120](https://github.com/Agent-Field/codeaf/pull/1120) · `chat` `engine`

- **the do door seats its worker from the settings the run will use** — [#1122](https://github.com/Agent-Field/codeaf/pull/1122) · `engine`

- **a launch's pool writers stop when the process closes** — [#1124](https://github.com/Agent-Field/codeaf/pull/1124) · `chat`

- **The pool index's min_installs floor counts a cell's installs, not its rows** — [#1127](https://github.com/Agent-Field/codeaf/pull/1127) · `engine`

- **The pool relay folds a retried batch once** — [#1128](https://github.com/Agent-Field/codeaf/pull/1128) · `engine`

- **A row the relay refuses by line leaves the outbox and the rows around it are sent** — [#1130](https://github.com/Agent-Field/codeaf/pull/1130) · `engine`

- **The published pool index holds only the cells that meet its min_installs floor** — [#1131](https://github.com/Agent-Field/codeaf/pull/1131) · `engine`

- **A run a process death left unjudged is judged on the next chat start** — [#1132](https://github.com/Agent-Field/codeaf/pull/1132) · `engine`

- **the outbox file is compacted back to the rows it still holds** — [#1133](https://github.com/Agent-Field/codeaf/pull/1133) · `engine`

- **The finished-tree check finds a project cloned into a subdirectory** — [#1135](https://github.com/Agent-Field/codeaf/pull/1135) · `engine`

- **the manual said the status row's ledger stays the conversation's; it is the tree's** — [#1143](https://github.com/Agent-Field/codeaf/pull/1143) · `docs` `chat`

- **The wall refuses a first replan round it cannot hold** — [#1144](https://github.com/Agent-Field/codeaf/pull/1144) · `resident`

- **an unstamped binary says it was built outside a git checkout, not without make build** — [#1151](https://github.com/Agent-Field/codeaf/pull/1151) · `build` `docs`

- **the terminal audit names the gitignored build products a run left in the deliverable tree** — [#1152](https://github.com/Agent-Field/codeaf/pull/1152) · `chat` `engine`

- **status says when the relay dropped a row and why** — [#1154](https://github.com/Agent-Field/codeaf/pull/1154) · `engine`

- **a job teardown never signals a process group whose pid was reused** — [#1155](https://github.com/Agent-Field/codeaf/pull/1155) · `chat` `engine` `resident`

- **an exhausted leaf continues the plan it has instead of a second full planning pass** — [#1156](https://github.com/Agent-Field/codeaf/pull/1156) · `resident` `chat`

- **pool status's index line says what this run's own fetch did to the cache** — [#1161](https://github.com/Agent-Field/codeaf/pull/1161) · `chat`

- **the soft memory limit now takes the cgroup bound, not just physical memory** — [#1162](https://github.com/Agent-Field/codeaf/pull/1162) · `chat`

- **the pool spells a seat by its bare model id** — [#1163](https://github.com/Agent-Field/codeaf/pull/1163) · `engine`

- **The relay bounds judge severity and publishes mean and sd on one scale** — [#1165](https://github.com/Agent-Field/codeaf/pull/1165) · `engine`

- **a job's shell cannot reach the tmux server hosting codeaf** — [#1166](https://github.com/Agent-Field/codeaf/pull/1166) · `engine`

- **An unattended run cannot push or move a branch it did not create** — [#1168](https://github.com/Agent-Field/codeaf/pull/1168) · `engine`

- **an exec run's worker spend reaches the usage ledger** — [#1170](https://github.com/Agent-Field/codeaf/pull/1170) · `engine`

- **the task door honors a stored crew word, and keeps its pins under the learned pick** — [#1171](https://github.com/Agent-Field/codeaf/pull/1171) · `chat` `engine`

- **a landing card under --yolo takes its default instead of parking the run** — [#1174](https://github.com/Agent-Field/codeaf/pull/1174) · `chat`

- **the model warm stops when the process closes** — [#1179](https://github.com/Agent-Field/codeaf/pull/1179) · `chat` `engine`

- **the auditor's calls say they are the auditor's and which node they check** — [#1181](https://github.com/Agent-Field/codeaf/pull/1181) · `engine`

- **a parked worker is handed back with a record at a third of its allowance** — [#1187](https://github.com/Agent-Field/codeaf/pull/1187) · `engine`

- **the unconnected custom row on /connect reads Custom OpenAI-compatible API again, not custom** — [#1195](https://github.com/Agent-Field/codeaf/pull/1195) · `chat`

## v0.2.1 — 2026-09-17

### Added

- **Every release carries THIRD-PARTY-NOTICES.md beside its binaries** — [#1051](https://github.com/Agent-Field/codeaf/pull/1051) · `build` `docs`

- **a launch says when a newer codeaf is out, and /update installs it and restarts** — [#1060](https://github.com/Agent-Field/codeaf/pull/1060) · `chat` `build`

- **Anonymous usage counts, on by default with a notice printed once** — [#1095](https://github.com/Agent-Field/codeaf/pull/1095) · `docs`

### Changed

- **The switcher's foot is one instruction and its name column takes the spare width** — [#1040](https://github.com/Agent-Field/codeaf/pull/1040) · `chat` `docs`

- **The switcher card lists conversations in the tab row's order, not by recency** — [#1040](https://github.com/Agent-Field/codeaf/pull/1040) · `chat` `docs`

- **A closed tab leaves the switcher's list at the same moment it leaves the row** — [#1040](https://github.com/Agent-Field/codeaf/pull/1040) · `chat` `docs`

- **ctrl+k kills to the end of the line, and the conversation switcher moves to alt+k** — [#1040](https://github.com/Agent-Field/codeaf/pull/1040) · `chat`

- **the spend place is three tables in four columns each — no bars, one right edge, money in cents** — [#1043](https://github.com/Agent-Field/codeaf/pull/1043) · `chat`

- **A home panel stands where its content puts it, and the quiet ones gather in a rail on the right** — [#1046](https://github.com/Agent-Field/codeaf/pull/1046) · `chat` `docs`

- **The README is the launch story and the reference it used to be is docs/GUIDE.md** — [#1049](https://github.com/Agent-Field/codeaf/pull/1049) · `docs`

- **The install story, the compiled-in manual and the branch rules say what is true on release day** — [#1050](https://github.com/Agent-Field/codeaf/pull/1050) · `docs` `build`

- **The thinking wheel comes back to auto, so the chip can reach the state a fresh install is in** — [#1057](https://github.com/Agent-Field/codeaf/pull/1057) · `chat`

- **ctrl+c leaves on the press that lands** — [#1061](https://github.com/Agent-Field/codeaf/pull/1061) · `chat` `docs`

- **Workspace documentation uses accessible references** — [#1064](https://github.com/Agent-Field/codeaf/pull/1064) · `docs`

- **a wrong completion check is answered with [no change], and the answer before it stays in view** — [#1067](https://github.com/Agent-Field/codeaf/pull/1067) · `chat` `engine` `docs`

- **the drafted-with footer names CodeAF and still links to agentfield.ai** — [#1075](https://github.com/Agent-Field/codeaf/pull/1075) · `resident` `docs`

- **The manual and the guide say the install proxy is live, because it is** — [#1076](https://github.com/Agent-Field/codeaf/pull/1076) · `docs` `chat`

- **task commits are authored by the agentfield-bot account; the old local address is legacy** — [#1088](https://github.com/Agent-Field/codeaf/pull/1088) · `chat` `engine`

- **The wordmark is drawn `CodeAF`, four rows tall, with the capitals on the `d`'s cap line** — [#1106](https://github.com/Agent-Field/codeaf/pull/1106) · `chat`

### Fixed

- **The switcher's close key says close tab, because put away is home's word for archiving** — [#1040](https://github.com/Agent-Field/codeaf/pull/1040) · `chat` `docs`

- **The switcher's fold says where the tabs stop, and every refusing row says why** — [#1040](https://github.com/Agent-Field/codeaf/pull/1040) · `chat` `docs`

- **Taking a conversation from the switcher leaves the place you took it from** — [#1040](https://github.com/Agent-Field/codeaf/pull/1040) · `chat` `docs`

- **An empty target is nothing to open, and never a window on the working directory** — [#1042](https://github.com/Agent-Field/codeaf/pull/1042) · `chat`

- **the installer takes the newest channel build, not the first one GitHub lists** — [#1056](https://github.com/Agent-Field/codeaf/pull/1056) · `build`

- **The folder walk's budget bounds every entry, and the suite never walks your home** — [#1058](https://github.com/Agent-Field/codeaf/pull/1058) · `chat`

- **"ask before running" reaches the gate you are already behind, and /status names it** — [#1063](https://github.com/Agent-Field/codeaf/pull/1063) · `chat`

- **a job root working in the person's directory is offered its overflow file outside that tree** — [#1096](https://github.com/Agent-Field/codeaf/pull/1096) · `engine` `resident`

- **The README shows how to pin a release version again** — [#1103](https://github.com/Agent-Field/codeaf/pull/1103) · `docs`

- **a release download finishes over a real network, on its own clock** — [#1110](https://github.com/Agent-Field/codeaf/pull/1110) · `chat` `build`

## v0.2.0 — 2026-09-15

### Added

- **The sizing reading and the handover's brief say what they are while you wait** — [#101](https://github.com/Agent-Field/codeaf/pull/101) · `engine` `chat`

- **ctrl+k is the switcher, and the task page folds a family under its root** — [#105](https://github.com/Agent-Field/codeaf/pull/105) · `chat`

- **a conversation can be about places beyond the one it opened in, and work there lands as a diff** — [#109](https://github.com/Agent-Field/codeaf/pull/109) · `chat` `engine` `build`

- **sent pictures draw in the transcript, and a drop converts however the terminal spells it** — [#115](https://github.com/Agent-Field/codeaf/pull/115) · `chat`

- **a foreground command backgrounds itself at a threshold, and the running turn teaches one hint line** — [#134](https://github.com/Agent-Field/codeaf/pull/134) · `chat` `engine`

- **settled tasks grade the models, and the divider reads that record before it assigns a tier** — [#155](https://github.com/Agent-Field/codeaf/pull/155) · `chat` `engine`

- **missing OpenRouter profiles connect in the browser** — [#206](https://github.com/Agent-Field/codeaf/pull/206) · `chat`

- **a defect issue now owes a replication a stranger can run and an acceptance that starts end to end** — [#209](https://github.com/Agent-Field/codeaf/pull/209) · `docs`

- **an ffmpeg library and edit_video — a join that cannot come out silent** — [#254](https://github.com/Agent-Field/codeaf/pull/254) · `chat` `engine` `docs`

- **a real-model lane that proves a family's parts work apart and land, on a repository and a folder** — [#280](https://github.com/Agent-Field/codeaf/pull/280) · `engine`

- **a person can open aforge's own manual — `aforge manual` and `/manual`, with no key and no spend** — [#297](https://github.com/Agent-Field/codeaf/pull/297) · `chat` `docs`

- **a real-model end-to-end lane proving the manual is reachable through the live chat surface** — [#309](https://github.com/Agent-Field/codeaf/pull/309) · `chat` `engine`

- **aforge logs shows the whole row, both token counts, five filters, JSON and a body** — [#359](https://github.com/Agent-Field/codeaf/pull/359) · `build` `chat`

- **one switch keeps the full record of a run, in a folder of its own that nobody else can read** — [#361](https://github.com/Agent-Field/codeaf/pull/361) · `chat` `engine` `docs`

- **a task brief names the journal path and line of the person's original words** — [#475](https://github.com/Agent-Field/codeaf/pull/475) · `engine` `chat`

- **/status --json prints the status note as one JSON object** — [#484](https://github.com/Agent-Field/codeaf/pull/484) · `chat`

- **continue re-arms the same failed task instead of proposing a new one** — [#622](https://github.com/Agent-Field/codeaf/pull/622) · `chat` `engine` `docs`

- **ctrl+w closes the tab in front, the way every browser closes one** — [#653](https://github.com/Agent-Field/codeaf/pull/653) · `chat`

- **long-running compact steps show quiet elapsed time** — [#653](https://github.com/Agent-Field/codeaf/pull/653) · `chat` `docs`

- **Open a New chat start page from plus without starting work** — [#653](https://github.com/Agent-Field/codeaf/pull/653) · `chat`

- **ctrl+shift+t reopens the tab you just closed** — [#653](https://github.com/Agent-Field/codeaf/pull/653) · `chat`

- **each compact step carries one still action icon in a fixed gutter** — [#653](https://github.com/Agent-Field/codeaf/pull/653) · `chat` `engine` `docs`

- **every chat tab holds its own engine connection, so opening one stops none of the others** — [#657](https://github.com/Agent-Field/codeaf/pull/657) · `chat` `engine` `remote`

- **Organize existing records through independent logical collections** — [#661](https://github.com/Agent-Field/codeaf/pull/661) · `engine` `chat` `docs`

- **a running turn now says what went up and what came back, in tokens, at the right edge of its work** — [#666](https://github.com/Agent-Field/codeaf/pull/666) · `chat`

- **The message box selects text with the pointer, and ctrl+z takes back what you typed** — [#669](https://github.com/Agent-Field/codeaf/pull/669) · `chat` `docs`

- **one object for every decision the engine hands a person, and one door every answer goes through** — [#712](https://github.com/Agent-Field/codeaf/pull/712) · `engine`

- **The model can ask through one shared question door** — [#728](https://github.com/Agent-Field/codeaf/pull/728) · `chat` `engine`

- **A question opens into a page you can read, compare, annotate and ask back at** — [#732](https://github.com/Agent-Field/codeaf/pull/732) · `chat` `engine`

- **questions reach home, another page, a sheet and an unattended window as one decision** — [#733](https://github.com/Agent-Field/codeaf/pull/733) · `chat` `engine`

- **one block above the box for every question, with esc meaning later rather than no** — [#734](https://github.com/Agent-Field/codeaf/pull/734) · `chat`

- **A pinned Back to main row returns directly from nested tasks** — [#771](https://github.com/Agent-Field/codeaf/pull/771) · `chat`

- **ctrl+r in /model fetches today's model list, and aforge models --refresh does the same from a script** — [#787](https://github.com/Agent-Field/codeaf/pull/787) · `chat`

- **a second place models come from, connected from /connect** — [#800](https://github.com/Agent-Field/codeaf/pull/800) · `chat` `engine` `docs`

- **The terminal's own tab says where you are in aforge, in Terminal.app and iTerm2 too** — [#809](https://github.com/Agent-Field/codeaf/pull/809) · `chat` `docs`

- **A quick task does the small job where you are — no copy of the folder, no branch, no check** — [#811](https://github.com/Agent-Field/codeaf/pull/811) · `chat` `engine` `docs`

- **A small model gets a smaller prefix, derived from its own window and the crew's worker seat** — [#812](https://github.com/Agent-Field/codeaf/pull/812) · `chat` `engine`

- **the chat signs the git work it does for you, in the resident's own words and bytes** — [#812](https://github.com/Agent-Field/codeaf/pull/812) · `chat` `engine` `resident`

- **a service's subscription door is tried before its metered one** — [#861](https://github.com/Agent-Field/codeaf/pull/861) · `chat` `engine` `docs`

- **Every call can be watched — one seam for a call's whole life, fed from the readings it already has** — [#868](https://github.com/Agent-Field/codeaf/pull/868) · `engine`

- **The tasks place draws the cursor row's record beside the list, and answers it there** — [#902](https://github.com/Agent-Field/codeaf/pull/902) · `chat`

- **home headings carry a dim explainer beside the word** — [#907](https://github.com/Agent-Field/codeaf/pull/907) · `chat`

- **a chooser change is judged by regret on ten days of log, not by a screenshot** — [#922](https://github.com/Agent-Field/codeaf/pull/922) · `engine` `docs`

- **an answer is a message to the conversation, so the work carries on while you decide** — [#954](https://github.com/Agent-Field/codeaf/pull/954) · `engine` `chat` `remote`

- **a launch sweep names live sessions on an older build** — [#1015](https://github.com/Agent-Field/codeaf/pull/1015) · `chat` `engine`

- **/drafts, the page over the kill ring** — [#1018](https://github.com/Agent-Field/codeaf/pull/1018) · `chat`

- **a fourth routing mode, `simple`, sends exactly what you asked for and nothing else** — [#1019](https://github.com/Agent-Field/codeaf/pull/1019) · `engine` `chat` `docs`

- **a picture's step row names the image model, and opening it shows the prompt** — [#1026](https://github.com/Agent-Field/codeaf/pull/1026) · `chat`

### Changed

- **dev is the trunk, staging is a pointer, and nothing publishes by itself** — [#82](https://github.com/Agent-Field/codeaf/pull/82) · `build` `docs`

- **money has one editor — the Spending tab — and every door lands on it** — [#110](https://github.com/Agent-Field/codeaf/pull/110) · `chat` `engine` `docs`

- **web search is Firecrawl by default, keyless, on both surfaces** — [#113](https://github.com/Agent-Field/codeaf/pull/113) · `chat` `engine`

- **the forming block shows the brief being written, and several tasks forming share one block** — [#125](https://github.com/Agent-Field/codeaf/pull/125) · `chat` `engine`

- **the reflex tier ships on mistralai/mistral-nemo instead of nex-agi/nex-n2-mini** — [#140](https://github.com/Agent-Field/codeaf/pull/140) · `chat` `engine`

- **a quiet conversation left alone is let go of, so a window's memory can come back** — [#151](https://github.com/Agent-Field/codeaf/pull/151) · `chat`

- **the write-your-notes rule is held rather than advised, and a turn that will not comply stops** — [#154](https://github.com/Agent-Field/codeaf/pull/154) · `chat` `engine`

- **a landing's claims are its checklist, and a divider is checked on its parts' work too** — [#156](https://github.com/Agent-Field/codeaf/pull/156) · `engine`

- **the leash judges what a task produced, not what it was called, and it has to open the tree** — [#159](https://github.com/Agent-Field/codeaf/pull/159) · `engine`

- **a task starts from its parent's folder as it stands, and its brief is checked against it first** — [#162](https://github.com/Agent-Field/codeaf/pull/162) · `engine` `chat`

- **both doors that write a brief carry the handoff manifest, and the prefix came out lighter for it** — [#179](https://github.com/Agent-Field/codeaf/pull/179) · `engine` `chat`

- **a worker stopped by the write-your-notes rule lands under its own words** — [#182](https://github.com/Agent-Field/codeaf/pull/182) · `chat` `engine`

- **a repository task inherits the whole world, its .env included, and still comes home as a merge** — [#183](https://github.com/Agent-Field/codeaf/pull/183) · `engine` `chat`

- **home's detail card is four groups with one gap law, and it is 56 cells wide** — [#219](https://github.com/Agent-Field/codeaf/pull/219) · `chat`

- **no two parts of one division may own the same path, and it is refused at admission** — [#236](https://github.com/Agent-Field/codeaf/pull/236) · `engine`

- **every part of a division opens on the same world, whoever wrote the parts** — [#240](https://github.com/Agent-Field/codeaf/pull/240) · `engine`

- **a fold marker names the journal path the model can grep, not a store id** — [#247](https://github.com/Agent-Field/codeaf/pull/247) · `engine` `chat`

- **the deliverable-file path law is said once, in system.md** — [#248](https://github.com/Agent-Field/codeaf/pull/248) · `engine`

- **nobody waits long — one hazard controller on every call, and a wait that says why** — [#253](https://github.com/Agent-Field/codeaf/pull/253) · `chat` `engine` `docs`

- **a parent commits its work to the family branch before it hands out the parts** — [#262](https://github.com/Agent-Field/codeaf/pull/262) · `engine`

- **web search settings land on the next search, and every search names its back end** — [#263](https://github.com/Agent-Field/codeaf/pull/263) · `chat` `engine`

- **the crew has a worker seat, and its presets are open-weight models picked off the catalog's scores** — [#278](https://github.com/Agent-Field/codeaf/pull/278) · `chat` `engine` `docs`

- **a proposed task waits while you type, a bare no means no, and the default window is fifteen seconds** — [#279](https://github.com/Agent-Field/codeaf/pull/279) · `chat` `engine` `remote`

- **the task engine has a ceiling on how many endings one function may have, and it only turns one way** — [#284](https://github.com/Agent-Field/codeaf/pull/284) · `engine`

- **No call sets a temperature — every request leaves sampling to the provider** — [#308](https://github.com/Agent-Field/codeaf/pull/308) · `engine` `chat`

- **the manual's wire lane prints what each pass spent, summed off the ledger as it asks** — [#331](https://github.com/Agent-Field/codeaf/pull/331) · `engine`

- **The reply guard reads a kilobyte, watches the thinking, and a reply you stopped mid-soup is not kept** — [#332](https://github.com/Agent-Field/codeaf/pull/332) · `engine` `chat`

- **a task's page runs the conversation's reducer, and learns durations, retries and notices** — [#337](https://github.com/Agent-Field/codeaf/pull/337) · `chat`

- **a task page reads a correction as a correction — steers are elbows and the engine reads the record** — [#351](https://github.com/Agent-Field/codeaf/pull/351) · `chat` `engine`

- **a task's page folds its settled work into phase chips, and ctrl+e, a click or a scroll up opens them** — [#355](https://github.com/Agent-Field/codeaf/pull/355) · `chat`

- **a model that has been watched thinking is waited against what it really does** — [#365](https://github.com/Agent-Field/codeaf/pull/365) · `engine` `docs`

- **the proposal hold of** — [#390](https://github.com/Agent-Field/codeaf/pull/390) · `engine` `chat` `remote`

- **the split gate is off by default, and the count and the plan's sizing stay as pins** — [#435](https://github.com/Agent-Field/codeaf/pull/435) · `engine`

- **the chat manual can answer how to run aforge with nobody watching** — [#436](https://github.com/Agent-Field/codeaf/pull/436) · `chat` `docs`

- **background jobs are their own section, named, paged, and stoppable** — [#439](https://github.com/Agent-Field/codeaf/pull/439) · `chat`

- **a person's manual answer heads each section with a line no page can contain** — [#446](https://github.com/Agent-Field/codeaf/pull/446) · `chat` `docs`

- **the chat manual stops saying a nested question is hidden, and names the folder that would not freeze** — [#447](https://github.com/Agent-Field/codeaf/pull/447) · `chat` `docs`

- **the money pages count the rows /cost draws, and an order is stopped in the words people ask in** — [#451](https://github.com/Agent-Field/codeaf/pull/451) · `chat` `docs`

- **the lanes page answers the floating alias, the headless run, and the third routing word** — [#453](https://github.com/Agent-Field/codeaf/pull/453) · `chat` `docs`

- **the manual's stemmer meets itself by Porter's measure, and "much" is a stop word** — [#458](https://github.com/Agent-Field/codeaf/pull/458) · `chat` `engine`

- **the surface paces the wire's lumps, never its own fold** — [#473](https://github.com/Agent-Field/codeaf/pull/473) · `chat`

- **the touched-packages job no longer runs on pull requests** — [#499](https://github.com/Agent-Field/codeaf/pull/499) · `build`

- **Check-refused tasks now say incomplete and keep their reason** — [#512](https://github.com/Agent-Field/codeaf/pull/512) · `chat` `docs`

- **the finished-work heading carries the asker's words, so the plain floor keeps its slack** — [#516](https://github.com/Agent-Field/codeaf/pull/516) · `chat` `docs`

- **what every surface says, and what it does when it does not fit** — [#518](https://github.com/Agent-Field/codeaf/pull/518) · `chat` `build` `docs`

- **the touched packages run on every pull request again, and check is green only when they are** — [#557](https://github.com/Agent-Field/codeaf/pull/557) · `build` `docs`

- **caption sentences now head work, and ctrl+e opens their outline before machinery** — [#563](https://github.com/Agent-Field/codeaf/pull/563) · `chat` `engine`

- **a task never writes a protected branch, and repository work is always on a branch of its own** — [#589](https://github.com/Agent-Field/codeaf/pull/589) · `engine` `chat`

- **a body-on call log keeps a long debug session instead of rotating at 32 MB** — [#622](https://github.com/Agent-Field/codeaf/pull/622) · `engine` `docs`

- **Named chat breadcrumbs and readable expanded task answers** — [#653](https://github.com/Agent-Field/codeaf/pull/653) · `chat`

- **live conversation work rolls through three compact step descriptions** — [#653](https://github.com/Agent-Field/codeaf/pull/653) · `chat` `docs`

- **Keep streaming prose compact until the response confirms an answer** — [#653](https://github.com/Agent-Field/codeaf/pull/653) · `chat` `engine`

- **Conversation work keeps attributable direction, independent views and retrievable results** — [#653](https://github.com/Agent-Field/codeaf/pull/653) · `chat` `engine` `docs`

- **A dedicated tab strip over the chat, with the task trail on its own row beneath** — [#653](https://github.com/Agent-Field/codeaf/pull/653) · `chat`

- **Name conversations promptly with distinct full and tab titles** — [#653](https://github.com/Agent-Field/codeaf/pull/653) · `chat` `engine` `remote` `docs`

- **Give conversation navigation a padded header and a Home button** — [#653](https://github.com/Agent-Field/codeaf/pull/653) · `chat`

- **Separate chat navigation from task metadata and make tab dismissal explicit** — [#653](https://github.com/Agent-Field/codeaf/pull/653) · `chat`

- **keep useful step descriptions visible between actions** — [#653](https://github.com/Agent-Field/codeaf/pull/653) · `chat` `docs`

- **Ctrl+t starts a new chat and the task roster moved to alt+t** — [#653](https://github.com/Agent-Field/codeaf/pull/653) · `chat`

- **Leave default reasoning to the model** — [#653](https://github.com/Agent-Field/codeaf/pull/653) · `engine` `chat`

- **conversations retain their work, context and delivery across navigation** — [#653](https://github.com/Agent-Field/codeaf/pull/653) · `chat` `engine` `remote` `docs`

- **Keep running task work compact and expandable** — [#653](https://github.com/Agent-Field/codeaf/pull/653) · `chat`

- **Keep many conversation tabs readable with a scrollable strip** — [#653](https://github.com/Agent-Field/codeaf/pull/653) · `chat`

- **Separate the conversation switcher from the page beneath it** — [#653](https://github.com/Agent-Field/codeaf/pull/653) · `chat`

- **one browser chooses folders and files, with a real preview beside them** — [#657](https://github.com/Agent-Field/codeaf/pull/657) · `chat` `engine` `remote`

- **Work starts from the original request and checking keeps recoverable evidence** — [#660](https://github.com/Agent-Field/codeaf/pull/660) · `chat` `engine`

- **Small edits across several files can finish in the conversation** — [#664](https://github.com/Agent-Field/codeaf/pull/664) · `chat` `engine`

- **Model generation uses provider defaults until you choose an override** — [#665](https://github.com/Agent-Field/codeaf/pull/665) · `chat` `engine` `resident` `docs`

- **a task row answers one question in one word, and no decision stays with the model past a turn** — [#668](https://github.com/Agent-Field/codeaf/pull/668) · `engine` `chat`

- **a task tries the check on another model, one merge round and one rerun before it asks you** — [#673](https://github.com/Agent-Field/codeaf/pull/673) · `engine`

- **the chat manual learns the three tiers, and stops teaching five vocabularies for one state** — [#679](https://github.com/Agent-Field/codeaf/pull/679) · `chat` `docs`

- **first-run setup groups models and spending before the first conversation** — [#680](https://github.com/Agent-Field/codeaf/pull/680) · `chat`

- **the landing card asks one question, in one word, with two answers** — [#689](https://github.com/Agent-Field/codeaf/pull/689) · `chat`

- **every row of work but the landing card draws one tier, one glyph and one word** — [#694](https://github.com/Agent-Field/codeaf/pull/694) · `chat`

- **A your-call landing opens its report with the question its row is asking** — [#696](https://github.com/Agent-Field/codeaf/pull/696) · `engine` `chat` `docs`

- **the chat pages follow the rail and the roster, and the ! leaves every row of work** — [#709](https://github.com/Agent-Field/codeaf/pull/709) · `chat` `docs`

- **Task pages group scoped model and Stop controls below the task tree** — [#710](https://github.com/Agent-Field/codeaf/pull/710) · `chat` `engine` `docs`

- **every mark a person sees comes from one vocabulary, in the tier their terminal can draw** — [#711](https://github.com/Agent-Field/codeaf/pull/711) · `chat` `docs`

- **a task whose files moved under it asks you to resolve it, instead of saying nobody could check it** — [#714](https://github.com/Agent-Field/codeaf/pull/714) · `engine` `chat`

- **images start collapsed and share explicit preview and original-file controls** — [#744](https://github.com/Agent-Field/codeaf/pull/744) · `chat` `remote` `docs`

- **The box at home is a draft, and the rule above it says which conversation** — [#747](https://github.com/Agent-Field/codeaf/pull/747) · `chat`

- **The thinking rung is on the seam beside the model, pressable, and it crosses --host** — [#747](https://github.com/Agent-Field/codeaf/pull/747) · `chat`

- **The conversation's name and model are on the seam, and the status row is a ledger** — [#747](https://github.com/Agent-Field/codeaf/pull/747) · `chat`

- **the icon law reaches every surface package, not just the chat** — [#753](https://github.com/Agent-Field/codeaf/pull/753) · `chat` `resident` `docs`

- **the approval question is the question block now, answered by digits, with esc meaning later** — [#758](https://github.com/Agent-Field/codeaf/pull/758) · `chat`

- **the stop card and the close-a-tab card are the question block now, answered by a cursor and enter** — [#758](https://github.com/Agent-Field/codeaf/pull/758) · `chat`

- **moving a conversation out of another window asks with a card, and home can ask at all** — [#758](https://github.com/Agent-Field/codeaf/pull/758) · `chat`

- **the task proposal is the question block now, answered by digits, and its card is the assignment** — [#758](https://github.com/Agent-Field/codeaf/pull/758) · `chat`

- **tasks can search and read earlier conversations without filesystem discovery** — [#769](https://github.com/Agent-Field/codeaf/pull/769) · `chat` `engine` `docs`

- **the connect offer asks on the question block and its key is typed in the message box** — [#780](https://github.com/Agent-Field/codeaf/pull/780) · `chat` `engine`

- **the harness offer and a finished design ask on the question block, and five old keys are gone** — [#780](https://github.com/Agent-Field/codeaf/pull/780) · `chat` `engine`

- **the standing card asks on the question block, and esc on it now means later rather than no** — [#780](https://github.com/Agent-Field/codeaf/pull/780) · `chat` `engine`

- **Home is seven panels, each answering one question, under a four-place bar** — [#783](https://github.com/Agent-Field/codeaf/pull/783) · `chat` `docs`

- **→ walks into a model's lanes, a fold always opens, and a pinned lane rides the model's name** — [#785](https://github.com/Agent-Field/codeaf/pull/785) · `chat` `docs`

- **A wide read is quick tasks, a wide change is one task, and independent pieces start in one breath** — [#811](https://github.com/Agent-Field/codeaf/pull/811) · `chat` `engine` `docs`

- **A harness message says how to read itself, so the page stopped explaining messages** — [#812](https://github.com/Agent-Field/codeaf/pull/812) · `chat`

- **The page keeps existence, the load reply keeps the prose — long mechanics are pulled, not pushed** — [#812](https://github.com/Agent-Field/codeaf/pull/812) · `chat` `engine`

- **A tool description is a contract, and a schema law fails the build when one turns back into prose** — [#812](https://github.com/Agent-Field/codeaf/pull/812) · `chat` `engine`

- **A tool result is cut to a tenth of the model's window, and each description quotes its own cap** — [#812](https://github.com/Agent-Field/codeaf/pull/812) · `chat` `engine`

- **Pushes publish dev, staging and rc builds, and a curl line installs any of them** — [#813](https://github.com/Agent-Field/codeaf/pull/813) · `build` `docs`

- **A settings row for the prompt profile, a smaller `ask` schema, a job footer said once per change** — [#844](https://github.com/Agent-Field/codeaf/pull/844) · `chat` `engine`

- **The token column shows what moves — ↑ is the request size, the moving side glows, tasks draw it** — [#849](https://github.com/Agent-Field/codeaf/pull/849) · `chat` `engine` `docs`

- **the call log says who answered and what a failure cost, and a committed census reads it** — [#852](https://github.com/Agent-Field/codeaf/pull/852) · `engine` `build` `docs`

- **every wait is spoken within a second, and a ceiling that fires always acts** — [#853](https://github.com/Agent-Field/codeaf/pull/853) · `chat` `engine` `docs`

- **One dispatcher, one plan — a failed call is bounded by one deadline** — [#858](https://github.com/Agent-Field/codeaf/pull/858) · `engine` `chat`

- **The rung is a move, the race reads the plan, and the person's patience is time** — [#864](https://github.com/Agent-Field/codeaf/pull/864) · `engine` `chat`

- **A warm wire and a socket that never waits — the reader reads, and a keystroke crosses** — [#872](https://github.com/Agent-Field/codeaf/pull/872) · `engine` `remote` `chat`

- **A task may hand out twenty pieces, tasks nest three deep, and the goal is the whole job's wall time** — [#874](https://github.com/Agent-Field/codeaf/pull/874) · `engine` `chat`

- **propose_task is a staged tool, so its card can go up before the reply that carries it is whole** — [#880](https://github.com/Agent-Field/codeaf/pull/880) · `chat` `engine`

- **a task's worker starts as soon as its folder exists, and memory and sizing are read beside it** — [#883](https://github.com/Agent-Field/codeaf/pull/883) · `engine` `chat`

- **home's `needs you` is two groups — the questions that stopped something, then what to check** — [#900](https://github.com/Agent-Field/codeaf/pull/900) · `chat`

- **The tasks list becomes a table — two columns, a sort key you can see, and every fold shut** — [#905](https://github.com/Agent-Field/codeaf/pull/905) · `chat`

- **A leaf's grant is written in one place, with the measurement saying it is too small beside it** — [#918](https://github.com/Agent-Field/codeaf/pull/918) · `engine` `chat` `docs`

- **The turn decides whether to become a task, and a task it starts inherits what it already read** — [#923](https://github.com/Agent-Field/codeaf/pull/923) · `chat` `engine`

- **A leaf's grant is 220,000, measured, with the band it sits in written down** — [#931](https://github.com/Agent-Field/codeaf/pull/931) · `engine` `chat` `docs`

- **A question is drawn one way, in one frame, with the amber on its marks alone** — [#933](https://github.com/Agent-Field/codeaf/pull/933) · `chat`

- **one call asks for one set of machines, whichever door its request left by** — [#934](https://github.com/Agent-Field/codeaf/pull/934) · `engine` `chat` `docs`

- **the chooser demands the set it admitted rather than suggesting it** — [#937](https://github.com/Agent-Field/codeaf/pull/937) · `engine` `chat` `docs`

- **With exactly one stoppable row on screen, x raises its card without alt+t first** — [#943](https://github.com/Agent-Field/codeaf/pull/943) · `chat`

- **questions from one step are one panel with tabs, and permissions from one batch one frame** — [#946](https://github.com/Agent-Field/codeaf/pull/946) · `chat` `docs`

- **/task starts at once, and its brief and its width are read beside the worker** — [#950](https://github.com/Agent-Field/codeaf/pull/950) · `chat` `engine` `remote` `docs`

- **the gate grades a grave call irreversible, and a permission's pointer opens by the stakes** — [#953](https://github.com/Agent-Field/codeaf/pull/953) · `engine` `chat`

- **An answer's evidence stands beside the answers, and the page a question opens is two panes** — [#972](https://github.com/Agent-Field/codeaf/pull/972) · `chat`

- **the controller-acted list moves onto the phase type, beside Waiting()** — [#982](https://github.com/Agent-Field/codeaf/pull/982) · `engine`

- **Home's box is three rows tall, typed in or not, and one command clears every engine** — [#1000](https://github.com/Agent-Field/codeaf/pull/1000) · `chat` `engine` `docs`

- **`auto` lends the routing to OpenRouter and takes it back when its answers turn bad** — [#1003](https://github.com/Agent-Field/codeaf/pull/1003) · `chat` `engine`

- **The turn fold keeps the newest slice of a repeatedly read file and points the older ones at it** — [#1015](https://github.com/Agent-Field/codeaf/pull/1015) · `engine` `docs`

- **`simple` is the routing row this build ships with, and it is the whole algorithm** — [#1019](https://github.com/Agent-Field/codeaf/pull/1019) · `engine` `chat` `docs`

- **the README sells the chat flow and says which install road is actually open** — [#1021](https://github.com/Agent-Field/codeaf/pull/1021) · `chat` `docs`

- **the thing behind a model that answers you is called a provider, everywhere a person reads** — [#1027](https://github.com/Agent-Field/codeaf/pull/1027) · `chat` `engine` `docs`

- **an unconnected account is reached through one question and the same turn carries on** — [#1037](https://github.com/Agent-Field/codeaf/pull/1037) · `chat` `engine` `docs`

### Renamed

- **the four pipeline verbs live under `aforge plan`, and `aforge run <name>` is the saved program** — [#518](https://github.com/Agent-Field/codeaf/pull/518) · `build` `docs`

- **a flag about money is not spelled like a flag about tokens, and a duration flag takes a duration** — [#518](https://github.com/Agent-Field/codeaf/pull/518) · `build` `docs`

- **The product, its binary, its module path, its state root and its variables are codeaf** — [#1041](https://github.com/Agent-Field/codeaf/pull/1041) · `chat` `engine` `build` `docs`

### Fixed

- **a declared door is something that can actually run** — [#92](https://github.com/Agent-Field/codeaf/pull/92) · `engine`

- **a hint on a failed tool result is a command with evidence behind it, or it is silence** — [#93](https://github.com/Agent-Field/codeaf/pull/93) · `engine` `docs`

- **a reply waiting on a job it started is waiting, and carrying on stops after three** — [#95](https://github.com/Agent-Field/codeaf/pull/95) · `chat` `engine` `docs`

- **work only a person can do goes to the person, not to a worker** — [#97](https://github.com/Agent-Field/codeaf/pull/97) · `engine`

- **a checkpoint that arrives after the close writes nothing** — [#99](https://github.com/Agent-Field/codeaf/pull/99) · `engine`

- **a watch that fires wakes the conversation, and only the firing does** — [#102](https://github.com/Agent-Field/codeaf/pull/102) · `chat` `engine`

- **a background job's page tails its log while it runs, and stops saying finished over work that is not** — [#106](https://github.com/Agent-Field/codeaf/pull/106) · `chat`

- **a background job cannot draw on the chat** — [#107](https://github.com/Agent-Field/codeaf/pull/107) · `engine`

- **the forming task card moves for the whole arrival** — [#114](https://github.com/Agent-Field/codeaf/pull/114) · `chat`

- **another window's running task says which life it is in, and a task may not read your login** — [#121](https://github.com/Agent-Field/codeaf/pull/121) · `chat` `engine` `docs`

- **A live stage keeps drawing, and the mark's reading is one of them** — [#122](https://github.com/Agent-Field/codeaf/pull/122) · `chat` `engine`

- **tool-grammar leaks are cut and rerouted, and /model says what it leaves running** — [#131](https://github.com/Agent-Field/codeaf/pull/131) · `chat` `engine`

- **enter on home's typed sentence opens a new conversation, and never sends it into the old one** — [#132](https://github.com/Agent-Field/codeaf/pull/132) · `chat`

- **a resumed conversation opens knowing what it has already spent** — [#135](https://github.com/Agent-Field/codeaf/pull/135) · `chat`

- **the money on the status line is the whole tree's, live, not the conversation's half of it** — [#153](https://github.com/Agent-Field/codeaf/pull/153) · `chat` `engine`

- **refusals name a door, the composer owns the letters, and the check reads the ground** — [#157](https://github.com/Agent-Field/codeaf/pull/157) · `chat` `engine`

- **resource policy is derived from what the process measured, not from constants** — [#158](https://github.com/Agent-Field/codeaf/pull/158) · `chat` `engine`

- **a picture dropped on home becomes a picture, on every box that can take one** — [#160](https://github.com/Agent-Field/codeaf/pull/160) · `chat`

- **a typed drop lands on whichever box has the keyboard, and a start in a typed folder carries the tray** — [#165](https://github.com/Agent-Field/codeaf/pull/165) · `chat`

- **a fork's hands and an adaptive run's nodes are on the machine's day total once, not twice** — [#180](https://github.com/Agent-Field/codeaf/pull/180) · `engine` `chat`

- **a context fill somebody set decides where a conversation folds; one nobody set still does not** — [#181](https://github.com/Agent-Field/codeaf/pull/181) · `chat` `engine` `docs`

- **the crew reaches the headless doors, and every run says which voice chose its models** — [#186](https://github.com/Agent-Field/codeaf/pull/186) · `engine` `docs`

- **two task nodes can no longer be handed one journal, which is what made five session tests flake** — [#187](https://github.com/Agent-Field/codeaf/pull/187) · `engine`

- **only work demotes an answer, and a turn's end settles the whole turn** — [#190](https://github.com/Agent-Field/codeaf/pull/190) · `chat`

- **the tmux TUI suite runs again — 9 of 9, with one table of words guarding every needle** — [#191](https://github.com/Agent-Field/codeaf/pull/191) · `chat` `build`

- **the sweep tells furrow to forget a swept session's forks** — [#197](https://github.com/Agent-Field/codeaf/pull/197) · `engine`

- **a task's card names where the work happened in plain words, from the rung that made it** — [#198](https://github.com/Agent-Field/codeaf/pull/198) · `chat` `engine`

- **four flaky tests now measure the claim rather than the machine they ran on or the hour** — [#199](https://github.com/Agent-Field/codeaf/pull/199) · `engine` `build`

- **a divided parent reads its two facts as one, and stops buying a turn on an empty request** — [#200](https://github.com/Agent-Field/codeaf/pull/200) · `engine`

- **a standing end that stands before the item's own first firing is refused, not stood up** — [#203](https://github.com/Agent-Field/codeaf/pull/203) · `chat` `engine` `docs`

- **a standing card's hint is built from the chips it drew, so it cannot name an absent answer** — [#204](https://github.com/Agent-Field/codeaf/pull/204) · `chat` `docs`

- **an answer asked from home renders as markdown, through the transcript's own renderer** — [#216](https://github.com/Agent-Field/codeaf/pull/216) · `chat`

- **the task tree's total goes with the conversation it is about** — [#217](https://github.com/Agent-Field/codeaf/pull/217) · `chat` `docs`

- **a tool call in the transcript is one row, and the row is measured in the bytes it draws** — [#218](https://github.com/Agent-Field/codeaf/pull/218) · `chat`

- **the move-it-here story is on the row and the card, and an idle window lets go in a quarter second** — [#220](https://github.com/Agent-Field/codeaf/pull/220) · `chat`

- **dragging over text selects characters, not rows — and a double-click takes the word** — [#224](https://github.com/Agent-Field/codeaf/pull/224) · `chat`

- **one owned decision about what the final answer is, and a turn's end settles it once** — [#234](https://github.com/Agent-Field/codeaf/pull/234) · `chat` `engine`

- **A task that hands parts out lands what the whole family wrote, not the parent's slice of it** — [#237](https://github.com/Agent-Field/codeaf/pull/237) · `engine` `chat`

- **a task family on a plain folder gets a family tree of its own, and its parts get worktrees off it** — [#239](https://github.com/Agent-Field/codeaf/pull/239) · `engine`

- **parts work in a copy of their own, not a copy of the repository** — [#251](https://github.com/Agent-Field/codeaf/pull/251) · `engine` `chat`

- **a file dragged from Windows into a WSL session becomes a picture, not a path in the message** — [#257](https://github.com/Agent-Field/codeaf/pull/257) · `chat`

- **a task working on a plain folder no longer writes over an edit you made while it ran** — [#270](https://github.com/Agent-Field/codeaf/pull/270) · `engine` `chat`

- **steering a task with nobody inside is refused with the reason instead of swallowed** — [#275](https://github.com/Agent-Field/codeaf/pull/275) · `engine` `chat`

- **a landing that cannot save the work keeps it and asks for your look, rather than deleting it** — [#277](https://github.com/Agent-Field/codeaf/pull/277) · `engine` `chat`

- **a growth governor may refuse a round, never a review's finding** — [#287](https://github.com/Agent-Field/codeaf/pull/287) · `engine` `resident`

- **a landing carries your uncommitted work or names the files, and a writing turn becomes a task** — [#288](https://github.com/Agent-Field/codeaf/pull/288) · `engine` `chat`

- **spending is written down per call, so an interrupted turn's money is on every surface** — [#290](https://github.com/Agent-Field/codeaf/pull/290) · `engine` `chat`

- **a leaf that ran out of its tokens is never settled on a judge's say-so** — [#291](https://github.com/Agent-Field/codeaf/pull/291) · `engine` `resident`

- **a landing that needs your look asks on every surface at any depth, and a timeout says who could not** — [#292](https://github.com/Agent-Field/codeaf/pull/292) · `engine` `chat`

- **a manual page read is bounded like a file read, and the rest of the page is asked for by heading** — [#295](https://github.com/Agent-Field/codeaf/pull/295) · `chat` `engine`

- **a page's own title is a search term, so a plain question reaches the page it names** — [#298](https://github.com/Agent-Field/codeaf/pull/298) · `chat` `docs`

- **generated manual packs are ignored again, and a build refuses to track them** — [#299](https://github.com/Agent-Field/codeaf/pull/299) · `build`

- **the shipped default keeps one ledger key instead of three, so it routes with priors** — [#300](https://github.com/Agent-Field/codeaf/pull/300) · `engine`

- **the manual quotes the code's own figures, and a moved figure names the page** — [#301](https://github.com/Agent-Field/codeaf/pull/301) · `chat` `docs`

- **a ground step owns every cell it covers — a sweep lights code spans and chips** — [#303](https://github.com/Agent-Field/codeaf/pull/303) · `chat`

- **a turn spent watching its own pieces is not a turn to hand over** — [#304](https://github.com/Agent-Field/codeaf/pull/304) · `engine`

- **two mints of one subharness at the same instant can no longer both win** — [#306](https://github.com/Agent-Field/codeaf/pull/306) · `engine`

- **the mint gate is taken under a deadline, so a mint refuses rather than waits** — [#310](https://github.com/Agent-Field/codeaf/pull/310) · `engine`

- **a crew older than a seat still fills it, and the run says which row it took** — [#311](https://github.com/Agent-Field/codeaf/pull/311) · `build` `engine` `docs`

- **looking something up in the manual searches your own words, not only the ones the model composed** — [#313](https://github.com/Agent-Field/codeaf/pull/313) · `chat` `engine`

- **a conversation seats a crew older than the work seat too, and says so once** — [#314](https://github.com/Agent-Field/codeaf/pull/314) · `chat` `engine` `docs`

- **the belief write leaves the send path, and no lock in internal/lane waits** — [#317](https://github.com/Agent-Field/codeaf/pull/317) · `chat` `engine`

- **an empty profile directory is the ordinary launch, so the crew draws and the notices remember** — [#320](https://github.com/Agent-Field/codeaf/pull/320) · `chat`

- **the ordinary launch is shown its front door, its rung and its own gate** — [#326](https://github.com/Agent-Field/codeaf/pull/326) · `chat`

- **a node that ran out with work recorded is offered again, and the sentence names the bound** — [#327](https://github.com/Agent-Field/codeaf/pull/327) · `engine` `resident` `docs`

- **a task on an attached repository seals, and work aforge starts is named before it is announced** — [#333](https://github.com/Agent-Field/codeaf/pull/333) · `engine` `chat`

- **a part of a division owns what its done-condition names, not what its brief mentions** — [#336](https://github.com/Agent-Field/codeaf/pull/336) · `engine`

- **AFORGE_DEBUG records the calls, the tools and the choices, not just a header** — [#338](https://github.com/Agent-Field/codeaf/pull/338) · `chat` `engine` `docs`

- **one model has one ledger identity, at every door and in the file already on disk** — [#353](https://github.com/Agent-Field/codeaf/pull/353) · `engine`

- **the headless doors fetch a lane sheet of their own** — [#354](https://github.com/Agent-Field/codeaf/pull/354) · `engine` `resident`

- **the tagged e2e lane runs on any machine, and the folder landing test is green on a Mac** — [#356](https://github.com/Agent-Field/codeaf/pull/356) · `build`

- **the model-call log never loses a row over a number JSON cannot spell** — [#357](https://github.com/Agent-Field/codeaf/pull/357) · `engine`

- **stopping is bounded at ten seconds, it says so on the way, and there is a door behind it** — [#358](https://github.com/Agent-Field/codeaf/pull/358) · `chat` `engine`

- **a failed headless run keeps its record, and every headless call names its tag and node** — [#363](https://github.com/Agent-Field/codeaf/pull/363) · `engine`

- **the background timer is a (home, program) pair, and a launch speaks only for its own** — [#366](https://github.com/Agent-Field/codeaf/pull/366) · `chat`

- **a machine that refuses a model is struck at once, dropped from the pin, and drawn as refused** — [#368](https://github.com/Agent-Field/codeaf/pull/368) · `chat` `engine`

- **a compile that comes back with no goal keeps the person's own words instead of ending the run** — [#369](https://github.com/Agent-Field/codeaf/pull/369) · `chat` `resident` `engine`

- **the standing package builds for windows again, through the lock and pid packages the tree has** — [#378](https://github.com/Agent-Field/codeaf/pull/378) · `build` `chat`

- **a headless run's total includes the plan model's reading of the request** — [#387](https://github.com/Agent-Field/codeaf/pull/387) · `engine` `resident`

- **a background reading over a connection lets go of its latch when it dies, and says so once** — [#396](https://github.com/Agent-Field/codeaf/pull/396) · `chat` `remote`

- **the checkpoint fixtures answer the namer by shape, so the early name spends no scripted step** — [#400](https://github.com/Agent-Field/codeaf/pull/400) · `engine`

- **a usage frame is folded into the call's count, never swapped in for it** — [#405](https://github.com/Agent-Field/codeaf/pull/405) · `engine`

- **the chat journal and the demo seeder post through the one thread door, and its sweep is green** — [#406](https://github.com/Agent-Field/codeaf/pull/406) · `chat` `build`

- **the standalone harness-design tool renders a brief again, and both doors read one machinery map** — [#410](https://github.com/Agent-Field/codeaf/pull/410) · `chat` `build`

- **the settings sheet's completeness is measured on the laid-out sheet, its calm on the window** — [#413](https://github.com/Agent-Field/codeaf/pull/413) · `build`

- **The goroutine sweep reads the defers, and every bare spawn in the guarded tree runs under guard.Go** — [#414](https://github.com/Agent-Field/codeaf/pull/414) · `chat` `engine` `build`

- **an observation is retired when it stops being needed, and a re-read of retired bytes gets the bytes** — [#415](https://github.com/Agent-Field/codeaf/pull/415) · `engine`

- **a file the request names is never scratch, so a leaf changing it is never at a standstill** — [#416](https://github.com/Agent-Field/codeaf/pull/416) · `resident`

- **a base is asked whether it publishes a lane sheet, never read for its hostname** — [#419](https://github.com/Agent-Field/codeaf/pull/419) · `engine` `chat` `resident`

- **the machine writes a device into its list before it says the pairing held** — [#420](https://github.com/Agent-Field/codeaf/pull/420) · `remote` `docs`

- **the background check walks its items on a machine that has no API key yet** — [#421](https://github.com/Agent-Field/codeaf/pull/421) · `chat` `build`

- **a brief that fails to write is retried, then composed, and the plan it belongs to is kept** — [#422](https://github.com/Agent-Field/codeaf/pull/422) · `chat` `engine`

- **an oversized node that cannot run in parallel is divided into ordered stages** — [#424](https://github.com/Agent-Field/codeaf/pull/424) · `engine`

- **the planner is told what a goal names against one worker's reach, and no pass hands over more** — [#425](https://github.com/Agent-Field/codeaf/pull/425) · `engine`

- **once nothing is changing the job stops spending — no gate, no repair, no resume** — [#430](https://github.com/Agent-Field/codeaf/pull/430) · `resident` `engine`

- **concurrent hedge arms no longer freeze while recording their call rows** — [#438](https://github.com/Agent-Field/codeaf/pull/438) · `engine`

- **quitting stops the task graph, so a node admitted a moment before never runs on unreachable** — [#448](https://github.com/Agent-Field/codeaf/pull/448) · `engine`

- **--one-model now reaches the mark reader and the brief writer, which had no model at all** — [#449](https://github.com/Agent-Field/codeaf/pull/449) · `chat` `engine`

- **every door that opens the chat surface parks the standard logger in chat.log** — [#450](https://github.com/Agent-Field/codeaf/pull/450) · `chat` `remote`

- **the headless ↻ line says why a claim went back on the queue, not only how much it picked up** — [#454](https://github.com/Agent-Field/codeaf/pull/454) · `engine` `resident` `docs`

- **a reading covers what the request names, and is retaken only when the tree changed** — [#460](https://github.com/Agent-Field/codeaf/pull/460) · `engine`

- **a constraint the person states is a law of the run, held at the gate and never advice** — [#461](https://github.com/Agent-Field/codeaf/pull/461) · `engine` `resident`

- **The system prompt names exactly the tools the agent reading it can call** — [#462](https://github.com/Agent-Field/codeaf/pull/462) · `chat` `engine`

- **the tui3 harness stops waiting on ticks that cannot reach it and names every command that parks** — [#463](https://github.com/Agent-Field/codeaf/pull/463) · `chat`

- **a run ends when the request is satisfied, not when the plan runs out** — [#464](https://github.com/Agent-Field/codeaf/pull/464) · `engine` `chat`

- **a settled task with green checks is not carried on as unfinished** — [#468](https://github.com/Agent-Field/codeaf/pull/468) · `engine` `chat`

- **isOpenRouter reads the alias marker off the model before the prefix check** — [#470](https://github.com/Agent-Field/codeaf/pull/470) · `engine`

- **a lane that attaches mid-job is handed the live job, not the checkpoint stub** — [#472](https://github.com/Agent-Field/codeaf/pull/472) · `chat`

- **A hand is told which of the tools on its caller's page are actually its own** — [#474](https://github.com/Agent-Field/codeaf/pull/474) · `chat` `engine`

- **a gated task inherits every prerequisite report, bounded and counted, and never cuts its own brief** — [#476](https://github.com/Agent-Field/codeaf/pull/476) · `engine` `chat` `docs`

- **two spaces go home from every place, and the memory card editor keeps its keyboard** — [#486](https://github.com/Agent-Field/codeaf/pull/486) · `chat`

- **an open /model list no longer rewrites via and speed on every paint** — [#491](https://github.com/Agent-Field/codeaf/pull/491) · `chat`

- **a shaper that spends its whole window thinking starts the task unshaped with a note** — [#493](https://github.com/Agent-Field/codeaf/pull/493) · `engine` `chat`

- **aforge do takes its wall as a duration with a unit, and a bare number is still seconds** — [#495](https://github.com/Agent-Field/codeaf/pull/495) · `engine` `docs`

- **the landing accounts for its own change, on every belt** — [#496](https://github.com/Agent-Field/codeaf/pull/496) · `engine`

- **internal/session compiles on windows — the session side of the standing fix** — [#501](https://github.com/Agent-Field/codeaf/pull/501) · `engine`

- **under --one-model the status line names the flag, and no crew receipt is posted** — [#503](https://github.com/Agent-Field/codeaf/pull/503) · `chat` `docs`

- **An unattended run decides on what landed and what ran, and stops when nothing moves** — [#507](https://github.com/Agent-Field/codeaf/pull/507) · `chat`

- **asking a command for help is not a failure any more, and every command has a usage** — [#523](https://github.com/Agent-Field/codeaf/pull/523) · `build`

- **exec --json says why a run failed, not only that it did** — [#524](https://github.com/Agent-Field/codeaf/pull/524) · `engine`

- **the spend lab opens on the fixture's clock, not the wall's** — [#526](https://github.com/Agent-Field/codeaf/pull/526) · `chat`

- **the day's money on the top line is the day's money on the spend place, wherever you are standing** — [#527](https://github.com/Agent-Field/codeaf/pull/527) · `chat`

- **a reopened conversation redraws the jobs it ran, and its job page is reachable again** — [#529](https://github.com/Agent-Field/codeaf/pull/529) · `chat`

- **a pinned lane the router refuses for a model yields to another lane, and says so** — [#533](https://github.com/Agent-Field/codeaf/pull/533) · `chat` `engine`

- **An unattended run reads every ending, and a landing that cannot come home is not offered again** — [#534](https://github.com/Agent-Field/codeaf/pull/534) · `chat`

- **a remainder is the original assignment plus a finding, and it divides on its size** — [#544](https://github.com/Agent-Field/codeaf/pull/544) · `engine`

- **whether a base carries a routing preference is learned from the base, never from its hostname** — [#547](https://github.com/Agent-Field/codeaf/pull/547) · `engine` `chat`

- **a price ceiling that empties the endpoint set no longer fails the turn** — [#553](https://github.com/Agent-Field/codeaf/pull/553) · `chat` `engine` `docs`

- **a harness saved note no longer races the done channel that tests wait on** — [#555](https://github.com/Agent-Field/codeaf/pull/555) · `engine`

- **a node is measured by the material it will read, not the file its words mention** — [#564](https://github.com/Agent-Field/codeaf/pull/564) · `engine`

- **a task waits for the command it started instead of inventing a way to poll it** — [#574](https://github.com/Agent-Field/codeaf/pull/574) · `engine`

- **Active tool context stays whole until the work uses it** — [#575](https://github.com/Agent-Field/codeaf/pull/575) · `chat` `engine` `docs`

- **the end-to-end lanes ask for a key the way the product does, so a machine with one runs them** — [#576](https://github.com/Agent-Field/codeaf/pull/576) · `build`

- **a task's contract, and a division's checks, are written where the work actually happens** — [#579](https://github.com/Agent-Field/codeaf/pull/579) · `engine` `chat`

- **A checkpoint hands over work it can hand over, and keeps the pieces it is still holding** — [#580](https://github.com/Agent-Field/codeaf/pull/580) · `engine` `chat`

- **A task's ground never climbs out of the scratch directory it lives in** — [#585](https://github.com/Agent-Field/codeaf/pull/585) · `engine`

- **an ignore list never empties the set the request is sent to** — [#586](https://github.com/Agent-Field/codeaf/pull/586) · `engine`

- **A delivery the gate could not judge is never ok — it is its own ending, said in words** — [#593](https://github.com/Agent-Field/codeaf/pull/593) · `engine` `docs`

- **under a steward with a wall, inline head work is bounded by a share of the wall** — [#595](https://github.com/Agent-Field/codeaf/pull/595) · `chat`

- **tool captions return, with its narrator off the scripted queue and its prompt paid for** — [#598](https://github.com/Agent-Field/codeaf/pull/598) · `chat` `engine`

- **A step out of a pasted reproduction is never run as a check** — [#599](https://github.com/Agent-Field/codeaf/pull/599) · `chat` `engine`

- **A file counts as made only when its content changed, and a stash is said out loud** — [#601](https://github.com/Agent-Field/codeaf/pull/601) · `chat`

- **docs/HEADLESS.md names the `unchecked` ending, and three probes hold the page that explains it** — [#604](https://github.com/Agent-Field/codeaf/pull/604) · `docs`

- **an interrupt from outside reaches Close, so nothing the session owned is orphaned** — [#605](https://github.com/Agent-Field/codeaf/pull/605) · `chat` `engine`

- **a finished leaf's ending is journaled, so a restart restores it as done** — [#606](https://github.com/Agent-Field/codeaf/pull/606) · `engine`

- **a leaf's done carries the commands it ran, or says no check was run** — [#607](https://github.com/Agent-Field/codeaf/pull/607) · `engine`

- **a task's landing time is a fact of the record now, and not a subtraction anybody performs** — [#608](https://github.com/Agent-Field/codeaf/pull/608) · `chat` `engine`

- **a leaf knows its own clock, and reading is never the reason one is stopped** — [#610](https://github.com/Agent-Field/codeaf/pull/610) · `engine` `docs`

- **A task's checker is told what was already red, and old red no longer refuses a landing** — [#611](https://github.com/Agent-Field/codeaf/pull/611) · `chat` `engine`

- **a leaf's room reads the wall it runs under, and a tree that does not build is never settled** — [#612](https://github.com/Agent-Field/codeaf/pull/612) · `engine` `docs`

- **continue task N reaches the continue verb, and a missing graph says so** — [#622](https://github.com/Agent-Field/codeaf/pull/622) · `chat` `engine` `docs`

- **a timeout is not a person's no, and a look does not ask in default mode** — [#622](https://github.com/Agent-Field/codeaf/pull/622) · `chat` `engine` `docs`

- **an unanswered approval waits; silence is never a no** — [#622](https://github.com/Agent-Field/codeaf/pull/622) · `chat` `engine` `docs`

- **a stalled first prompt names /model instead of hanging silent** — [#622](https://github.com/Agent-Field/codeaf/pull/622) · `chat` `engine` `docs`

- **a commit, an undo, or a one-line fix is never converted into a task** — [#622](https://github.com/Agent-Field/codeaf/pull/622) · `chat` `engine` `docs`

- **a leaf's landing reserve is bounded in tokens as well as turns, so a spent grant cannot buy another** — [#624](https://github.com/Agent-Field/codeaf/pull/624) · `engine` `docs`

- **A run left on its own answers to the same git list a task does** — [#625](https://github.com/Agent-Field/codeaf/pull/625) · `engine` `chat`

- **a landing reads the commit its branch was cut from, and not only its name** — [#626](https://github.com/Agent-Field/codeaf/pull/626) · `engine` `chat`

- **the wall is read while a session is idle, so hours that run out with work still going end the run** — [#627](https://github.com/Agent-Field/codeaf/pull/627) · `chat` `engine`

- **a leaf is judged on the job's tree, not on what it happened to write itself** — [#628](https://github.com/Agent-Field/codeaf/pull/628) · `engine` `docs`

- **three identical timeouts of one command end a leaf's round, with a reason that names the command** — [#629](https://github.com/Agent-Field/codeaf/pull/629) · `engine` `docs`

- **A command quoted in the person's ask names no check, in either spelling** — [#630](https://github.com/Agent-Field/codeaf/pull/630) · `chat` `engine`

- **a cut stream's bill is asked for from the provider's own receipt, and a price nobody has is said** — [#631](https://github.com/Agent-Field/codeaf/pull/631) · `engine` `chat`

- **a lane a person pinned is honoured at every door, not only in the conversation** — [#632](https://github.com/Agent-Field/codeaf/pull/632) · `engine` `chat` `docs`

- **a check run over an unchanged tree is not run again, and none starts that the wall cannot hold** — [#633](https://github.com/Agent-Field/codeaf/pull/633) · `chat` `engine`

- **a stream that trickles is hedged or cut on a named reason, not left to the wall** — [#636](https://github.com/Agent-Field/codeaf/pull/636) · `chat` `engine`

- **a run left going checkpoints on every door, not only where somebody is watching** — [#637](https://github.com/Agent-Field/codeaf/pull/637) · `chat` `engine`

- **A file the run created counts as made only while there is something in it** — [#638](https://github.com/Agent-Field/codeaf/pull/638) · `chat` `engine`

- **a task's report keeps the fenced tail it promised, and says when it was cut** — [#639](https://github.com/Agent-Field/codeaf/pull/639) · `engine` `chat`

- **a run that was handed a list answers the list, item by item, at every ending** — [#640](https://github.com/Agent-Field/codeaf/pull/640) · `engine` `chat` `docs`

- **a job's ending reaches the model whole, and the general trim is gone** — [#641](https://github.com/Agent-Field/codeaf/pull/641) · `chat` `engine`

- **A mark reader nobody could reach is not a witness, and the checks stand in for it** — [#642](https://github.com/Agent-Field/codeaf/pull/642) · `chat`

- **a thinking level on a class value reaches every request its seat sends** — [#643](https://github.com/Agent-Field/codeaf/pull/643) · `engine` `chat` `docs`

- **a run that has not landed carries no landing time, and the index answers with the file's last word** — [#644](https://github.com/Agent-Field/codeaf/pull/644) · `chat` `engine`

- **a slash written as prose is not a folder, and a task is no longer refused over one** — [#645](https://github.com/Agent-Field/codeaf/pull/645) · `engine` `chat`

- **an errand's ending names the gate that judged it, and one reader decides a task's audit posture** — [#646](https://github.com/Agent-Field/codeaf/pull/646) · `engine` `chat` `docs`

- **A faulted gate's row and handover name the fault that happened, not one fixed sentence** — [#647](https://github.com/Agent-Field/codeaf/pull/647) · `engine` `docs`

- **the status line names the gate --yolo opened** — [#648](https://github.com/Agent-Field/codeaf/pull/648) · `chat`

- **a covering refusal teaches which machines this account cannot reach** — [#649](https://github.com/Agent-Field/codeaf/pull/649) · `engine` `chat` `docs`

- **a cost that was really spent never reads $0.00, and money.go is the only place the head spells one** — [#650](https://github.com/Agent-Field/codeaf/pull/650) · `resident` `docs`

- **a refused headless run names the directory it worked in and says nothing reached disk** — [#651](https://github.com/Agent-Field/codeaf/pull/651) · `chat` `docs`

- **A file the run only read is not the change it made, so a refusal names only what it was shown** — [#652](https://github.com/Agent-Field/codeaf/pull/652) · `engine` `chat`

- **Learn generation work and keep successful prompt caches stable** — [#653](https://github.com/Agent-Field/codeaf/pull/653) · `engine` `chat`

- **Wait for delegated workers when their session closes** — [#653](https://github.com/Agent-Field/codeaf/pull/653) · `chat` `engine`

- **Make chat switching respond to hover and give conversation navigation room to read** — [#653](https://github.com/Agent-Field/codeaf/pull/653) · `chat`

- **Browse chat names before switching and return to the same task reading position** — [#653](https://github.com/Agent-Field/codeaf/pull/653) · `chat`

- **Select the last open chat when closing the current tab** — [#653](https://github.com/Agent-Field/codeaf/pull/653) · `chat`

- **Wait for endpoint reachability after a lost connection** — [#653](https://github.com/Agent-Field/codeaf/pull/653) · `chat`

- **A turn delivering a result this conversation owns finishes it here, not in a second task** — [#653](https://github.com/Agent-Field/codeaf/pull/653) · `engine` `chat`

- **Name chats from the first message without waiting for the answer** — [#653](https://github.com/Agent-Field/codeaf/pull/653) · `chat` `engine` `remote`

- **Finished task scrolling preserves collapsed work and disclosure arrows match their state** — [#653](https://github.com/Agent-Field/codeaf/pull/653) · `chat`

- **Keep hosted chat identity attached to the selected session** — [#653](https://github.com/Agent-Field/codeaf/pull/653) · `chat`

- **Keep explicit verification checks for unattended inline work** — [#653](https://github.com/Agent-Field/codeaf/pull/653) · `chat` `engine`

- **Bound memory preparation and detect stale engines after rebuilding** — [#653](https://github.com/Agent-Field/codeaf/pull/653) · `engine` `chat`

- **Keep elapsed feedback beside a completed step while the turn continues** — [#653](https://github.com/Agent-Field/codeaf/pull/653) · `chat`

- **Later acceptance restores a retained task checkout before delivery** — [#653](https://github.com/Agent-Field/codeaf/pull/653) · `engine` `chat`

- **reopening long conversations keeps completed work folded** — [#653](https://github.com/Agent-Field/codeaf/pull/653) · `chat` `engine` `docs`

- **Keep retained task work distinct from delivery to the requested workspace** — [#653](https://github.com/Agent-Field/codeaf/pull/653) · `chat`

- **Keep guest recovery with its owner and preserve unanswered corrections** — [#653](https://github.com/Agent-Field/codeaf/pull/653) · `chat`

- **Group task header facts and preserve saved design answers** — [#653](https://github.com/Agent-Field/codeaf/pull/653) · `chat`

- **Make task header cost easier to read** — [#653](https://github.com/Agent-Field/codeaf/pull/653) · `chat`

- **Keep intermediate task narration distinct from the answer** — [#653](https://github.com/Agent-Field/codeaf/pull/653) · `chat`

- **Preserve code blocks in task reports without repeating the answer** — [#653](https://github.com/Agent-Field/codeaf/pull/653) · `chat`

- **Read finished tasks as request and answer** — [#653](https://github.com/Agent-Field/codeaf/pull/653) · `chat`

- **Align compact task navigation with the reading gutter** — [#653](https://github.com/Agent-Field/codeaf/pull/653) · `chat`

- **Reject explicit failed completions before learning provider speed or cache affinity** — [#653](https://github.com/Agent-Field/codeaf/pull/653) · `engine` `chat`

- **Replace failed network attempts on the live conversation page** — [#653](https://github.com/Agent-Field/codeaf/pull/653) · `chat`

- **Footer throughput identifies the whole-turn average** — [#653](https://github.com/Agent-Field/codeaf/pull/653) · `chat`

- **same-named tags cannot disguise protected landing branches** — [#654](https://github.com/Agent-Field/codeaf/pull/654) · `engine` `chat`

- **Folder and attachment browsing share one modal with live columns** — [#659](https://github.com/Agent-Field/codeaf/pull/659) · `chat`

- **the status row stops losing its last cell when the turn clock ticks mid-frame** — [#667](https://github.com/Agent-Field/codeaf/pull/667) · `chat`

- **Unusable naming replies recover and compact tabs reveal their full names on hover** — [#671](https://github.com/Agent-Field/codeaf/pull/671) · `chat` `engine` `docs`

- **endpoint relaxation keeps the price ceiling until a second refusal** — [#678](https://github.com/Agent-Field/codeaf/pull/678) · `chat` `engine` `docs`

- **a turn split by a failed first step stops drawing a second work door above the split** — [#681](https://github.com/Agent-Field/codeaf/pull/681) · `chat`

- **A kept branch is delivered only when the check could find the files it compared** — [#682](https://github.com/Agent-Field/codeaf/pull/682) · `chat` `engine`

- **a check that answers while the send keeps failing now waits, and a media wait names its model** — [#683](https://github.com/Agent-Field/codeaf/pull/683) · `chat`

- **The New chat start page keeps its keyboard, and no question is drawn on it** — [#684](https://github.com/Agent-Field/codeaf/pull/684) · `chat`

- **Collections survive a second aforge, and a read never takes the writer lock** — [#685](https://github.com/Agent-Field/codeaf/pull/685) · `engine` `chat` `docs`

- **enter on a running conversation opens it here, and the window it left steps back** — [#691](https://github.com/Agent-Field/codeaf/pull/691) · `chat` `engine` `remote` `docs`

- **a first turn nobody has measured an answer for still respects the price of writing one** — [#693](https://github.com/Agent-Field/codeaf/pull/693) · `engine` `chat`

- **A conversation's identity is written under a lock of its own, so two windows lose nothing** — [#695](https://github.com/Agent-Field/codeaf/pull/695) · `chat` `engine`

- **Keep task details inside their column and short task windows usable** — [#697](https://github.com/Agent-Field/codeaf/pull/697) · `chat` `docs`

- **an unattended one-message run stays for the work it hands over, and says every ending** — [#701](https://github.com/Agent-Field/codeaf/pull/701) · `chat` `docs`

- **A state path too long for a socket answers at once, in words, and starts nothing** — [#704](https://github.com/Agent-Field/codeaf/pull/704) · `chat` `engine` `docs`

- **one sentence for every road onto a folder, and a chooser that opens where the person is** — [#713](https://github.com/Agent-Field/codeaf/pull/713) · `chat` `remote` `docs`

- **a rescue that takes over an answer withdraws the words the dead machine wrote** — [#715](https://github.com/Agent-Field/codeaf/pull/715) · `chat` `engine`

- **The tui3 harness only gives up on a command that may never answer** — [#716](https://github.com/Agent-Field/codeaf/pull/716) · `chat`

- **tool rounds and worker handoffs retain their own evidence** — [#717](https://github.com/Agent-Field/codeaf/pull/717) · `engine` `chat` `docs`

- **a tab and its switcher row say the same thing, and an unnamed chat has one name** — [#720](https://github.com/Agent-Field/codeaf/pull/720) · `chat`

- **Home-card terminal checks follow the current layout and asynchronous Git reading** — [#721](https://github.com/Agent-Field/codeaf/pull/721) · `chat` `docs`

- **A list marker in front of a naming label is markup, and a "Sure!" before one is still an answer** — [#723](https://github.com/Agent-Field/codeaf/pull/723) · `chat` `engine`

- **/help and seven manual pages say what this build actually does** — [#725](https://github.com/Agent-Field/codeaf/pull/725) · `chat` `docs`

- **A step caption keeps a filename, a version number and a path whole** — [#731](https://github.com/Agent-Field/codeaf/pull/731) · `chat` `engine`

- **a rebuild of the same commit is not an older aforge** — [#732](https://github.com/Agent-Field/codeaf/pull/732) · `build` `engine` `docs`

- **a plain launch sits down in the conversation the engine already holds** — [#736](https://github.com/Agent-Field/codeaf/pull/736) · `chat` `engine`

- **a tasks section heading says what its fold hides, and stops stating a total the foot disagrees with** — [#739](https://github.com/Agent-Field/codeaf/pull/739) · `chat`

- **the add context sheet leaves no cell of itself behind, and always draws its way out** — [#742](https://github.com/Agent-Field/codeaf/pull/742) · `chat` `docs`

- **a landing that is your call can be answered from a window on an engine host** — [#743](https://github.com/Agent-Field/codeaf/pull/743) · `chat` `remote` `docs`

- **One thinking ladder, walked the same way at every door a person can turn it** — [#745](https://github.com/Agent-Field/codeaf/pull/745) · `chat` `docs`

- **streamed reasoning is replayed as completed blocks** — [#746](https://github.com/Agent-Field/codeaf/pull/746) · `engine` `chat` `docs`

- **The up arrow recalls in every conversation, not only the first one** — [#747](https://github.com/Agent-Field/codeaf/pull/747) · `chat`

- **The live rate, the via rider and the phase words cross to a conversation on an engine host** — [#747](https://github.com/Agent-Field/codeaf/pull/747) · `chat`

- **the auto-settle floor survives a restart — a landing aforge was deciding comes back yours** — [#751](https://github.com/Agent-Field/codeaf/pull/751) · `chat` `engine` `docs`

- **an adaptive run prices a bare model id as unpriced, never at a vendor row whose name merely matches** — [#752](https://github.com/Agent-Field/codeaf/pull/752) · `engine`

- **Accepted rescue cuts reach the bounded call retry** — [#755](https://github.com/Agent-Field/codeaf/pull/755) · `chat` `engine`

- **a plain launch is greeted by home again, asks from it, and shows what it has spent** — [#756](https://github.com/Agent-Field/codeaf/pull/756) · `chat` `engine`

- **internal/config builds every provider client, and a thinking level leaves the model id** — [#757](https://github.com/Agent-Field/codeaf/pull/757) · `chat` `engine`

- **the model catalog cache belongs to the base it was fetched from** — [#759](https://github.com/Agent-Field/codeaf/pull/759) · `chat` `engine`

- **a thinking model is not a silent one, and no turn ends with nothing said** — [#760](https://github.com/Agent-Field/codeaf/pull/760) · `chat` `engine`

- **AFORGE_QUESTION_DEMO is registered as operator plumbing, so internal/config is green** — [#762](https://github.com/Agent-Field/codeaf/pull/762) · `build`

- **a window whose link was repaired takes its task rail back** — [#764](https://github.com/Agent-Field/codeaf/pull/764) · `chat` `remote` `engine` `docs`

- **Question handling supports six additional interaction cases** — [#765](https://github.com/Agent-Field/codeaf/pull/765) · `chat` `engine` `docs`

- **a conversation that moves mid-thought asks its question again where it lands** — [#766](https://github.com/Agent-Field/codeaf/pull/766) · `chat` `engine`

- **A question crosses the engine link, so the road an ordinary aforge takes has one** — [#768](https://github.com/Agent-Field/codeaf/pull/768) · `chat` `remote`

- **Question handling completes six additional interaction cases** — [#772](https://github.com/Agent-Field/codeaf/pull/772) · `chat` `engine` `docs`

- **home says the gate's own sentence, and names a reminder that fired while you were away** — [#773](https://github.com/Agent-Field/codeaf/pull/773) · `chat`

- **The model's task doors now reach the same work yours do — stop, settle, and a bare id** — [#774](https://github.com/Agent-Field/codeaf/pull/774) · `chat` `engine` `docs`

- **an accept whose merge was refused says so, and the landing it left behind can be answered again** — [#776](https://github.com/Agent-Field/codeaf/pull/776) · `chat` `engine`

- **the column and the composer say the same thing about a landing as its card does** — [#778](https://github.com/Agent-Field/codeaf/pull/778) · `chat`

- **a held row that is asking lands on its own answers, and the move card waits for enter** — [#779](https://github.com/Agent-Field/codeaf/pull/779) · `chat` `docs`

- **the manual stops quoting two landing receipts that no code spells any more** — [#781](https://github.com/Agent-Field/codeaf/pull/781) · `chat` `docs`

- **a turn that loads a tool and stops without using it is sent back once** — [#782](https://github.com/Agent-Field/codeaf/pull/782) · `engine` `docs`

- **answers are rows, keys are digits, a checklist is ticked on the card, a switch forgets questions** — [#784](https://github.com/Agent-Field/codeaf/pull/784) · `chat` `engine`

- **a long reply still writing at its lane's pace is no longer cut at the stream wall** — [#786](https://github.com/Agent-Field/codeaf/pull/786) · `chat` `engine`

- **every question has a pointer the arrows walk and enter takes, and the task page shows its question** — [#789](https://github.com/Agent-Field/codeaf/pull/789) · `chat`

- **the question room's answers walk, open and click, and an open one reads in three tiers** — [#791](https://github.com/Agent-Field/codeaf/pull/791) · `chat`

- **a 429 inside an open stream now paces the pool that served it, like one that arrives as a status** — [#793](https://github.com/Agent-Field/codeaf/pull/793) · `engine`

- **a model that keeps refusing is given up on, and the reply finishes on the next one in the chain** — [#794](https://github.com/Agent-Field/codeaf/pull/794) · `chat` `engine`

- **a failed request leaves a row where you are reading, and a finished step stops saying it runs** — [#795](https://github.com/Agent-Field/codeaf/pull/795) · `chat`

- **an errand is bounded as an errand, and a rung that is answering is not cut by a wall clock** — [#796](https://github.com/Agent-Field/codeaf/pull/796) · `chat` `engine`

- **a question with evidence under each answer reaches the person on the first ask** — [#797](https://github.com/Agent-Field/codeaf/pull/797) · `engine` `chat`

- **A model picked while a task is being checked is the next run's, not a rewrite of what ran** — [#802](https://github.com/Agent-Field/codeaf/pull/802) · `chat` `engine`

- **a handover no longer lists finished work as work that is left** — [#804](https://github.com/Agent-Field/codeaf/pull/804) · `engine` `chat`

- **a task room draws its own machine and rate, because news now says which work it is about** — [#806](https://github.com/Agent-Field/codeaf/pull/806) · `chat` `engine` `remote`

- **A reply waiting on its own running task is left alone rather than pushed on** — [#811](https://github.com/Agent-Field/codeaf/pull/811) · `chat` `engine` `docs`

- **a checking call the window refused is no longer written down as a call that was made** — [#812](https://github.com/Agent-Field/codeaf/pull/812) · `engine`

- **the job footer stops defeating de-duplication, and every footer is budgeted inside its own cap** — [#812](https://github.com/Agent-Field/codeaf/pull/812) · `chat` `engine`

- **the routed memory block rides at the tail, so message[0] holds still for a session** — [#812](https://github.com/Agent-Field/codeaf/pull/812) · `chat` `engine` `docs`

- **An answer the door never acknowledged is still your answer, not another window's** — [#812](https://github.com/Agent-Field/codeaf/pull/812) · `chat`

- **The lean prompt profile keys off the context window and an explicit pin, never off the worker seat** — [#812](https://github.com/Agent-Field/codeaf/pull/812) · `chat` `engine`

- **a cancelled arm is waited off the temp home, and a recovered lane is a candidate again** — [#814](https://github.com/Agent-Field/codeaf/pull/814) · `engine`

- **roomfeed waits for the room's way back; reopened rooms keep each call's duration** — [#820](https://github.com/Agent-Field/codeaf/pull/820) · `chat` `engine`

- **blanks keep [tab] next blank at a hundred columns, and a stood order refreshes the column count** — [#821](https://github.com/Agent-Field/codeaf/pull/821) · `chat`

- **a stalled check under load keeps its stall account, person manual labels match** — [#822](https://github.com/Agent-Field/codeaf/pull/822) · `chat` `engine` `docs`

- **Nested landing accept banks the question and writes `you took this as done`** — [#823](https://github.com/Agent-Field/codeaf/pull/823) · `chat` `engine`

- **A dev build publishes its release instead of skipping publish after the build** — [#834](https://github.com/Agent-Field/codeaf/pull/834) · `build`

- **a refused race recovers instead of ending the turn, and account exclusions are learned once** — [#835](https://github.com/Agent-Field/codeaf/pull/835) · `chat` `engine` `docs`

- **a waiting question rides behind the row's door instead of over it** — [#840](https://github.com/Agent-Field/codeaf/pull/840) · `chat` `docs`

- **A bare launch draws the spend panel and the head's money on its first frame, not three seconds later** — [#842](https://github.com/Agent-Field/codeaf/pull/842) · `chat`

- **the provider and the live tok/s no longer vanish at random** — [#845](https://github.com/Agent-Field/codeaf/pull/845) · `chat` `remote`

- **a keystroke is not refused at ten seconds behind an unrelated getter** — [#846](https://github.com/Agent-Field/codeaf/pull/846) · `chat` `remote`

- **a later task is not pointed at an earlier task's copy** — [#847](https://github.com/Agent-Field/codeaf/pull/847) · `engine` `chat`

- **a retry goes to a different machine, and the ledger is keyed on who answered** — [#850](https://github.com/Agent-Field/codeaf/pull/850) · `chat` `engine` `docs`

- **a window that just acted is still watching, and a stop names the door it really came through** — [#851](https://github.com/Agent-Field/codeaf/pull/851) · `chat` `engine` `remote`

- **One classifier for every failed call — the transport states facts, one function picks the move** — [#854](https://github.com/Agent-Field/codeaf/pull/854) · `engine` `chat`

- **a correction that fell through gets its turn drawn — the steer lane is stamped by conversation** — [#855](https://github.com/Agent-Field/codeaf/pull/855) · `chat`

- **A hosted conversation keeps drawing the turns the engine starts after this window's own first turn** — [#856](https://github.com/Agent-Field/codeaf/pull/856) · `chat`

- **the router can reach the fast lanes again, and a refusal now counts against a lane** — [#859](https://github.com/Agent-Field/codeaf/pull/859) · `engine` `chat`

- **a decayed belief draws no tail** — [#862](https://github.com/Agent-Field/codeaf/pull/862) · `chat`

- **Opening a task's page or switching tabs no longer counts the token figures up from zero** — [#867](https://github.com/Agent-Field/codeaf/pull/867) · `chat` `docs`

- **A conversation that ran a quick task reopens with its task graph** — [#869](https://github.com/Agent-Field/codeaf/pull/869) · `engine` `chat`

- **a question you talk past comes down, and your own words go out at once** — [#870](https://github.com/Agent-Field/codeaf/pull/870) · `chat` `engine` `docs`

- **Nothing auxiliary blocks the person — one reading beside the work, never in front of it** — [#871](https://github.com/Agent-Field/codeaf/pull/871) · `chat` `engine`

- **a machine is asked only when the answer is expected inside the wait its work will sit through** — [#873](https://github.com/Agent-Field/codeaf/pull/873) · `engine` `chat` `docs`

- **the frame reads cached data without disk access** — [#875](https://github.com/Agent-Field/codeaf/pull/875) · `chat` `build` `docs`

- **nothing hidden happens on a turn — one file memo, one off-path reading, one deferred write** — [#876](https://github.com/Agent-Field/codeaf/pull/876) · `chat` `engine` `docs`

- **A quick task comes through one door, its checkpoint rebuilds it, and it holds the files it wrote** — [#879](https://github.com/Agent-Field/codeaf/pull/879) · `chat` `engine` `docs`

- **A task no longer pays for a fork it will not use, and its runner reads git once a batch** — [#881](https://github.com/Agent-Field/codeaf/pull/881) · `engine` `docs`

- **A task being sized shows the model thinking, the seconds and the tokens coming back** — [#882](https://github.com/Agent-Field/codeaf/pull/882) · `engine` `chat` `docs`

- **a fan of tasks starts what this machine can carry, not all of it at once** — [#885](https://github.com/Agent-Field/codeaf/pull/885) · `engine` `chat`

- **a refused call keeps the schema's sentence for the model, and the person reads their own words** — [#890](https://github.com/Agent-Field/codeaf/pull/890) · `chat` `engine` `docs`

- **a row of the tasks place says its state once, and a root names its folder only where that is news** — [#895](https://github.com/Agent-Field/codeaf/pull/895) · `chat` `engine`

- **a working copy that holds work is kept, whoever did the work** — [#899](https://github.com/Agent-Field/codeaf/pull/899) · `chat` `engine` `docs`

- **A conversation's anchor writes a whole identity, so an older snapshot cannot undo it** — [#904](https://github.com/Agent-Field/codeaf/pull/904) · `chat` `engine`

- **plain engine-road chats keep this machine's connection, harness, and profile doors** — [#906](https://github.com/Agent-Field/codeaf/pull/906) · `chat` `engine`

- **every question is raised and settled through one door, and answering leaves the prompt alone** — [#910](https://github.com/Agent-Field/codeaf/pull/910) · `engine`

- **a task's check runs in its own copy, and a piece that comes home late folds into its parent** — [#911](https://github.com/Agent-Field/codeaf/pull/911) · `chat` `engine` `docs`

- **A conversation that has said nothing is called new conversation on the tasks place, not by its id** — [#915](https://github.com/Agent-Field/codeaf/pull/915) · `chat`

- **an answer is drawn the moment it is pressed, and a question takes keys only where it is drawn** — [#919](https://github.com/Agent-Field/codeaf/pull/919) · `chat`

- **a wire below its promised pace is rescued out of the call's own budget, and every attempt is watched** — [#924](https://github.com/Agent-Field/codeaf/pull/924) · `chat` `engine` `docs`

- **a named refusal is a move, an account's ceiling waits once, and a person's pick outranks the chain** — [#925](https://github.com/Agent-Field/codeaf/pull/925) · `chat` `engine` `docs`

- **a task card quotes the report's first line that says something, not its literal first line** — [#930](https://github.com/Agent-Field/codeaf/pull/930) · `chat`

- **a word you say while work is running lands at the next request, not the next turn** — [#935](https://github.com/Agent-Field/codeaf/pull/935) · `chat` `engine` `remote`

- **a planning model that thinks past its wall is asked for its answer instead of losing it all** — [#940](https://github.com/Agent-Field/codeaf/pull/940) · `engine` `resident`

- **The tasks tool counts another window's families, not its parts** — [#944](https://github.com/Agent-Field/codeaf/pull/944) · `engine`

- **the machine reading is divided by the lanes the whole process is running, not one conversation's** — [#947](https://github.com/Agent-Field/codeaf/pull/947) · `engine`

- **an observation the reply already answered is not raised again** — [#948](https://github.com/Agent-Field/codeaf/pull/948) · `chat` `engine` `docs`

- **a check that runs out of time is told its window, keeps what it read, and says it was the clock** — [#949](https://github.com/Agent-Field/codeaf/pull/949) · `chat` `engine`

- **A task's page says a call is still open, with that call's own clock and rate** — [#951](https://github.com/Agent-Field/codeaf/pull/951) · `chat` `engine`

- **A reading beside the work is waited on, not guessed at, and a held memory line is not lost** — [#952](https://github.com/Agent-Field/codeaf/pull/952) · `chat` `engine`

- **Asking back gets an answer with the question still open, and the chip counts only what waits on you** — [#960](https://github.com/Agent-Field/codeaf/pull/960) · `chat`

- **a split drawing reaches the boundary its own cut opened, and never says it carried on** — [#962](https://github.com/Agent-Field/codeaf/pull/962) · `engine`

- **a countdown on a question is let go when the conversation closes, like everything else it armed** — [#963](https://github.com/Agent-Field/codeaf/pull/963) · `engine`

- **an answer left for a closed conversation is applied as it opens** — [#965](https://github.com/Agent-Field/codeaf/pull/965) · `engine` `chat`

- **an idle conversation on an older engine build is named on the way in** — [#966](https://github.com/Agent-Field/codeaf/pull/966) · `chat` `engine`

- **a cold question about how it writes, and about what to do after installing, reaches its page again** — [#973](https://github.com/Agent-Field/codeaf/pull/973) · `chat` `docs`

- **A namer that answers with its plan is refused, so the task keeps the person's own words** — [#975](https://github.com/Agent-Field/codeaf/pull/975) · `chat` `engine`

- **key presses avoid unnecessary redraws** — [#977](https://github.com/Agent-Field/codeaf/pull/977) · `chat` `build`

- **Two cold questions reach their page first instead of fourth, so the retrieval floor has margin** — [#978](https://github.com/Agent-Field/codeaf/pull/978) · `chat` `docs`

- **a task the sizing judge called wide keeps the divide verb when the reviewer refuses its first parts** — [#987](https://github.com/Agent-Field/codeaf/pull/987) · `engine` `docs`

- **the permission frame names its way out wherever the pointer stands** — [#992](https://github.com/Agent-Field/codeaf/pull/992) · `chat` `docs`

- **the reading that tells a model what other windows did no longer writes into a closed conversation** — [#997](https://github.com/Agent-Field/codeaf/pull/997) · `engine`

- **The stale-engine refusal names the workspace in the command it tells you to run** — [#998](https://github.com/Agent-Field/codeaf/pull/998) · `chat` `engine` `docs`

- **the OpenRouter-takeover gate counts a refusal only when the router's own pick was refused** — [#1015](https://github.com/Agent-Field/codeaf/pull/1015) · `engine` `docs`

- **a `read` of lines the conversation already holds answers with a pointer** — [#1015](https://github.com/Agent-Field/codeaf/pull/1015) · `engine` `docs`

- **the cache pin is admitted into the lane chooser's enforced set rather than dropped by it** — [#1015](https://github.com/Agent-Field/codeaf/pull/1015) · `chat`

- **The race can no longer pull the first taking-stock mark below half the handoff price** — [#1015](https://github.com/Agent-Field/codeaf/pull/1015) · `chat` `engine`

- **billed arms journal their call lines, and a hopped turn seals one usage line per model** — [#1015](https://github.com/Agent-Field/codeaf/pull/1015) · `chat` `engine`

- **the model picker's `auto` row stops promising a takeover the `simple` routing never does** — [#1019](https://github.com/Agent-Field/codeaf/pull/1019) · `chat` `docs`

- **the chip names a machine only while the wire would demand it, and a routing change lands now** — [#1025](https://github.com/Agent-Field/codeaf/pull/1025) · `chat` `engine` `docs`

- **a task-start question you answered is not asked again when you come back to the tab** — [#1036](https://github.com/Agent-Field/codeaf/pull/1036) · `chat` `engine`

- **a conversation you put away with ctrl+e leaves home on the keystroke, not a few seconds later** — [#1039](https://github.com/Agent-Field/codeaf/pull/1039) · `chat`

- **the stable release notes are rendered to fit a GitHub release page instead of copied whole** — [#1044](https://github.com/Agent-Field/codeaf/pull/1044) · `build` `docs`

### Removed

- **the v2 chat surface is gone — tui2 is now only the library v3 draws with** — [#111](https://github.com/Agent-Field/codeaf/pull/111) · `chat` `resident` `build`

- **a window holds as many conversations as you open** — [#137](https://github.com/Agent-Field/codeaf/pull/137) · `chat`

- **There is one worker — swe, the bare worker and every way of choosing between workers are gone** — [#235](https://github.com/Agent-Field/codeaf/pull/235) · `engine` `chat` `build` `docs`

- **the proposal hold of** — [#367](https://github.com/Agent-Field/codeaf/pull/367) · `engine` `chat` `remote`

- **the v2 chat entry point, and everything only it reached** — [#469](https://github.com/Agent-Field/codeaf/pull/469) · `chat`

- **lanestub's router-shaped second address is gone; the plain base is the only base** — [#554](https://github.com/Agent-Field/codeaf/pull/554) · `engine`

- **The caption outline of** — [#591](https://github.com/Agent-Field/codeaf/pull/591) · `chat` `engine`

- **five stale artifacts are gone — no binary at the root, and harnesses/ is only ever generated** — [#788](https://github.com/Agent-Field/codeaf/pull/788) · `build` `docs`

- **the v2 remnants — the root compositor, the model picker, the blocks engine** — [#790](https://github.com/Agent-Field/codeaf/pull/790) · `chat`

- **the v1 window and internal/command — 111 lines moved, 36,037 deleted** — [#792](https://github.com/Agent-Field/codeaf/pull/792) · `chat` `resident` `build`

- **`fork` is off the belt: a reply no longer copies itself into hands** — [#811](https://github.com/Agent-Field/codeaf/pull/811) · `chat` `engine` `docs`

- **The mid-stream ledger teaching of** — [#990](https://github.com/Agent-Field/codeaf/pull/990) · `engine`

## v0.1.0 — 2026-08-17
