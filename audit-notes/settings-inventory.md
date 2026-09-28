# Settings inventory — every row the /settings panel shows

Investigation only. No code changed. Read on branch `feat/1089-custom-connections`
on 2026-09-20. This file is the complete row inventory of the v3 settings panel,
tab by tab in the bar's order, so a settings-page redesign can be argued from it.

Sources, and nothing else:

- The registry: `internal/config/settings.go` — `Settings.build()` (line 1762)
  declares every knob. 66 fixed rows sit in one composite literal (lines
  1776-2563), 10 model-slot rows come from `ModelSlots()`
  (`internal/config/modelslots.go:89` — 5 engine roles from
  `internal/store/role_bindings.go:92`, 5 media modalities from
  `internal/config/models.go:322`), and two rows are built only when the caller
  wires the seam they need (`standing.background` at line ~2455, `split_pct` in
  `splitRow()` at line 2876).
- The skin: `internal/tui3/settings.go` — the `settingUI` map (line ~150) is the
  registry key → {tab, label, about, widget} table. `chrome_test.go:103-113`
  (`TestEverySettingRowHasATab`) fails the build when a registry key has no
  entry, so the map is total over the registry by force.
- Generated sections that are not registry rows: `settingspend.go` (readings),
  the roles section inside `settings.go` itself (note: the brief expected a
  `settingsroles.go`; it does not exist — the roles section is
  `sheet.roleItems`, `internal/tui3/settings.go:1558`, and only
  `settingsroles_test.go` carries the name), `settingsautonomy.go` (autonomy
  rows), `connectcaps.go` (the Connections tab) and `modelservices.go` (the
  custom-connection services section on Providers).

Notation: "key" is the string the session's `settings` tool lists and
`change_setting` writes. "label" and "about" are the panel's own words, quoted
verbatim from `settingUI`. "default" is what an untouched profile reads (the
panel computes the same thing over a pristine profile, `settings.go:1106`).
`firstSentence(Hint)` means the panel left `about` empty and shows the first
sentence of the registry's own Hint (`settings.go:663`).

## How the panel builds a tab

- `sheet.build()` (`internal/tui3/settings.go:1215`): no search → one tab's
  rows; a search → every tab's matches under faint headings, and the tab bar
  follows the first match.
- Providers is led by `modelsSection` (`settings.go:700`): your model,
  provider, speed guard, routing, prompt profile, then the five tier
  rows in `roles.Tiers` order (reflex, low, worker, high, mastermind), then
  pinned roles. Every other row follows in registry order
  (`tabRows`, `settings.go:1430`). The registry builds the 10 model rows
  first, so on Providers the non-led model rows (planning, execution,
  verification, naming, drawing, speaking, composing, filming, voice) read
  between the roles section and the looking row.
- Search matches label, key, about and the registry's own label
  (`settingMatches`, `settings.go:1456`).

---

## 1. Session

Two registry rows. The tab comment says so: "It is two rows, and that is the
honest size of it" (`settings.go:56`).

| key | label | widget | kind/choices | default | about (verbatim) | source |
|---|---|---|---|---|---|---|
| memory.enabled | memory | cycle | choice: on/off | on | "a few things are carried from one conversation to the next. Off, each one starts knowing nothing about you." | registry `settings.go:2140`, skin `settings.go:213` |
| models.fallbacks | fallback models | text | text | blank ("nearest in the catalog") | "where a conversation goes when no provider will take the request: slugs, comma-separated, first tried first. Blank picks the nearest one." | registry `settings.go:2355`, skin `settings.go:458` |

Registry category vs tab: models.fallbacks is `CategoryModels` but lives under
Session ("where its model will not answer").

## 2. Context

Eight registry rows. Drawn in registry order: the four search rows, then the
four context-law rows.

| key | label | widget | kind/choices | default | about (verbatim) | source |
|---|---|---|---|---|---|---|
| search.provider | searching | cycle | choice: auto/firecrawl/duckduckgo/exa/jina-search | auto | dynamic — `config.SearchProviderHintAt` (`config/settings.go:3259`); the static map text is "where a web search goes. auto uses the best back end your keys reach and falls back to one that needs none." e.g. `now auto, duckduckgo — set search.exaKey or search.firecrawlKey to raise it` | registry `settings.go:1812`, skin `settings.go:470` |
| search.exaKey | exa key | text | text, secret | not set | "an exa.ai key, which buys better results and page fetches than the free back end. Optional." | registry `settings.go:1838`, skin `settings.go:481` |
| search.firecrawlKey | firecrawl key | text | text, secret | not set | "a firecrawl.dev key, for when the free monthly allowance runs out. Optional." | registry `settings.go:1846`, skin `settings.go:486` |
| search.jinaKey | jina key | text | text, secret | not set | "a jina.ai key. It buys nothing but headroom: page fetches already work unauthenticated." | registry `settings.go:1856`, skin `settings.go:491` |
| context_fill_pct | compact at | text | count, unit % | 60 (ctxbudget.DefaultFillPercent, `ctxbudget/ctxbudget.go:39`); unpinned conversations follow the model's window | "how much of the model's window codeaf fills before it compacts, as a percent. The rest stays as thinking and answer room." | registry `settings.go:2367`, skin `settings.go:637` |
| completion_reserve | answer room | text | count, unit tok | 65536 (`ctxbudget.go:45`) | "tokens every call keeps free for its answer and its reasoning." | registry `settings.go:2378`, skin `settings.go:643` |
| working_set_tokens | working set | text | count, unit tok | 160,000 (`ctxbudget.go:71`) | "the most material kept quoted in front of a worker at once, however large the model's window is." | registry `settings.go:2387`, skin `settings.go:648` |
| context_reuse_pct | context reuse | text | count, unit % | 250 (`ctxbudget.go:94`) | "how many times over one piece of work may re-send its whole context before codeaf tells it to land: 100 is once, 250 is two and a half times. At least 100." | registry `settings.go:2397`, skin `settings.go:456` |

Registry category vs tab: all four search keys and all four context knobs are
`CategoryModels` in the registry; the panel files them under Context.

## 3. Workspace

Twelve registry rows, in registry order (the three sign-in rows first, then the
practice rows, tenure, background checks, attribution, then the four ssh
rows).

| key | label | widget | kind/choices | default | about (verbatim) | source |
|---|---|---|---|---|---|---|
| google_oauth_client | google sign-in id | text | text | not set | "identifies codeaf to Google when you connect an account. Blank uses the one codeaf ships with." | registry `settings.go:1873`, skin `settings.go:607` |
| google_oauth_secret | google sign-in secret | text | text, secret | not set | "the secret that goes with the id above. It is kept masked once saved." | registry `settings.go:1883`, skin `settings.go:613` |
| slack_oauth_client | slack sign-in id | text | text | not set | "identifies codeaf to Slack when you connect a workspace. Blank uses the one codeaf ships with." | registry `settings.go:1894`, skin `settings.go:618` |
| practice_idle | quiet before practice | text | duration | 20m (config.go:191) | "how long the room stays quiet before codeaf starts practicing." | registry `settings.go:2245`, skin `settings.go:581` |
| brief_after | arrival brief after | text | duration | 4h (config.go:195) | "how long you have to be away before codeaf greets you with a summary. 0 always briefs." | registry `settings.go:2252`, skin `settings.go:586` |
| tenure_after | tenure after | text | count | 3 (DefaultTenureAfter, settings.go:1264) | "how many clean firings a standing charter needs before it earns tenure." | registry `settings.go:2430`, skin `settings.go:550` |
| standing.background | background checks | cycle | choice: on/off | on | "reminders, watches and routines are checked every 5 minutes with no window open. Off checks only while one is." (5 minutes from `standing.Interval`, `standing/standing.go:89`) | registry `settings.go:~2455` (`backgroundRow`, line 2830), skin `settings.go:596` |
| attribution | attribution | toggle | bool | on | "signs the commits and PRs codeaf writes for you — one trailer, one footer line." | registry `settings.go:2513`, skin `settings.go:602` |
| ssh.control_persist_seconds | ssh reuse | text | count, unit s | 300 (ssh.go:25) | "seconds an ssh connection stays reusable after it closes, so a quick reconnect skips the handshake. 0 turns it off; a change lands next launch." | registry `settings.go:2532`, skin `settings.go:154` |
| ssh.server_alive_seconds | ssh heartbeat | text | count, unit s | 3 (ssh.go:26) | "seconds of silence before ssh asks whether the far machine is still there. 0 turns heartbeats off; a change lands next launch." | registry `settings.go:2540`, skin `settings.go:160` |
| ssh.server_alive_misses | ssh missed heartbeats | text | count | 3 (ssh.go:27) | "how many unanswered heartbeats end a dead connection — three with the default heartbeat notices one in about nine seconds. A change lands next launch." | registry `settings.go:2548`, skin `settings.go:165` |
| ssh.ip_qos | ssh traffic | cycle | choice: lowdelay/af21/none | lowdelay (ssh.go:28) | "how ssh marks its traffic: lowdelay by default, af21 on networks that honor it, none where marking is filtered. A change lands next launch." | registry `settings.go:2556`, skin `settings.go:170` |

Registry category vs tab: the three sign-in rows are `CategoryModels` in the
registry ("which application asks for the accounts") but sit on Workspace; the
four ssh rows are `CategoryInterface` but sit on Workspace (the skin comment at
`settings.go:140-153` explains the move off Session and says the
`Connections`-tab clash "is a real defect and it is still open").

The `standing.background` row is BUILT ONLY when the caller wires a watch
(`SettingsOptions.BackgroundChecks`). The v3 panel
(`internal/tui3/settings.go:1153`) and the session settings tool
(`internal/session/tools_settings.go:114`) both wire one, so it is present in
practice.

## 4. Display

Nine registry rows on v3. A tenth — `split_pct`, "chat width" — is mapped in
`settingUI` (`settings.go:646`) but the v3 panel never builds it: the registry
only builds the row when the caller passes `SettingsOptions.SaveSplitPct`, and
the v3 chat deliberately does not (registry comment at `settings.go:2480-2502`:
"the honest sheet is one without the row"). It is the one `settingUI` entry
with no registry row on this surface.

| key | label | widget | kind/choices | default | about (verbatim) | source |
|---|---|---|---|---|---|---|
| ui.mouse | mouse | cycle | choice: on/off | on | "on gives hover and click; off gives the terminal's own text selection back." | registry `settings.go:2065`, skin `settings.go:650` |
| ui.timestamps | timestamps | cycle | choice: footers/separators/off | footers | "footers puts a receipt under each finished turn; separators only marks the gaps." | registry `settings.go:2074`, skin `settings.go:656` |
| ui.work | turn work | cycle | choice: fold/open | fold | "fold completed turn machinery into one worked chip, or keep it open." | registry `settings.go:2085`, skin `settings.go:662` |
| ui.icons | step icons | cycle | choice: auto/rich/plain | auto | "Rich icons normally; plain symbols when your terminal needs them." | registry `settings.go:2092`, skin `settings.go:666` |
| ui.task_column | task column | toggle | bool | on | "stands the task roster beside the chat. With no foreground command to background, ctrl+g closes it and brings it back; this is where the answer is remembered." | registry `settings.go:2468`, skin `settings.go:632` |
| ui.quick_switch | quick switch | toggle | bool | on | "ctrl+tab switches on the press where the terminal can send it. Off, it waits for enter. alt+k always opens the list and waits for your choice." | registry `settings.go:2479`, skin `settings.go:638` |
| history.enabled | input history | toggle | bool | on | "remembers the messages you send, so the up arrow walks them back in a later session." | registry `settings.go:2487`, skin `settings.go:622` |
| draft.persist | keep drafts | toggle | bool | on | "keeps the half-typed message in the box across a restart, per directory." | registry `settings.go:2496`, skin `settings.go:627` |
| ui.hints | hints | toggle | bool | on | "one-line tips above the box until you have used what each one teaches. Off silences them, and what's-new lines with them." | registry `settings.go:2504`, skin `settings.go:644` |
| (split_pct) | chat width | text | percent | 80 (settings.go:1273) | "the chat pane's share of the frame while the task rail is open." — mapped but never built on v3 | skin `settings.go:646`, registry `splitRow` `settings.go:2876` |

## 5. Spending

Money and nothing else. The tab is laid out by `spendingItems()`
(`internal/tui3/settingspend.go:79`), which interleaves 4 registry rows with 3
always-present readings plus 2 conditional ones. `today` is a receipt the
cursor steps over; `per task` and `per standing run` are "rails this build HAS
and does not keep a settings row for".

| row | key | widget | default | what it says (verbatim) | source |
|---|---|---|---|---|---|
| today | — (reading) | none, cursor steps over | nil before the day's first paid call | "$3.42 of $500 · resets at midnight" shape; "what the day has cost, against what it is allowed" | settingspend.go:124 |
| unwritten | — (reading, only when writes were dropped) | none | absent at zero | "<N> spending records could not be written" · "every figure here is short by that much" | settingspend.go:344 |
| unbilled | — (reading, only when calls went unpriced) | none | absent at zero | "<N> calls the provider charged for and could not be priced" · "no figure was invented" | settingspend.go:357 |
| per day | daily_budget_usd | text | $500 (config.go:121) | "what codeaf may spend on your work in a day. When the day's calls reach it, new work waits for midnight or for you to raise it here. none removes the limit." | registry `settings.go:1905`, skin `settings.go:498` |
| per conversation | session.spendRailUSD | text | no limit (0.0, settings.go:1306) | "what one conversation may spend before it stops starting turns. The turn in flight always finishes and your message stays yours to send again. none removes the limit." | registry `settings.go:2233`, skin `settings.go:519` |
| per plan | plan_consent_usd | text | $100 (settings.go:1260) | "above this estimate a planned job quotes its step count and its price and waits for your go-ahead — it asks, it does not stop. none never asks." | registry `settings.go:1918`, skin `settings.go:504` |
| per task | — (reading) | none | — | "no limit of its own" · "it spends against the day and this conversation" | settingspend.go:263 |
| per standing run | — (reading) | none | $5 a firing (`standing.DefaultPerRunUSD`, standing.go:246) | "$5 a firing" · "each order may name its own" | settingspend.go:274 |
| practice | practice_budget_usd | text | $50 of the day (config.go:189) | "the slice of the day codeaf may spend practicing on itself. When it is gone practice stops until tomorrow and your own work is untouched. 0 here turns practice off rather than uncapping it." | registry `settings.go:1928`, skin `settings.go:509` |

## 6. Safety

Eight registry rows, plus the generated autonomy section hung directly under
"approval countdown".

| key | label | widget | kind/choices | default | about (verbatim) | source |
|---|---|---|---|---|---|---|
| tools.approvalMode | ask before running | cycle | choice: prompt/allow/deny | prompt (settings.go:1293) | "what happens when the model asks to run a tool. Dangerous shell commands are asked about whichever way this is set." | registry `settings.go:1943`, skin `settings.go:108` |
| tools.approval | tool exceptions | text | text | none | "exceptions to the answer above, one per tool: read:allow, bash:prompt." | registry `settings.go:1952`, skin `settings.go:113` |
| tools.bashPatterns | shell command rules | text | text | none | "answers for single shell commands, first match wins: allow git status*, deny rm -rf *." | registry `settings.go:1963`, skin `settings.go:120` |
| approval.guardian | guardian | cycle | choice: off/on | off (settings.go:1300) | "asks a small model first whether a call is plainly safe, so you are only asked about the rest." | registry `settings.go:1975`, skin `settings.go:129` |
| approval.timeout_seconds | approval countdown | text | count, unit s | 10 (settings.go:1366) | "seconds an approval question counts down before it pauses and keeps waiting. Never answers no; any key stops the clock; 0 waits from the start." | registry `settings.go:1984`, skin `settings.go:136` |
| bash.background_after_seconds | background after | text | count, unit s | 30 (bash.go:8) | (the registry hint constant, quoted whole) "seconds a foreground command runs before it is kept running as a background job and the chat moves on. 0 waits for the command's own timeout. A change lands on the next session." | registry `settings.go:1993`, skin `settings.go:141` |
| task.settle | who settles work that needs a look | cycle | choice: ask/auto | ask | "ask puts it on the landed card for you. auto lets the chat read the work and decide, and ask you only when it cannot tell." | registry `settings.go:2126`, skin `settings.go:224` |
| task.autoapprove_seconds | task countdown | text | count, unit s | 15 (settings.go:1316) | "seconds a proposed task waits for you before it starts. 0 waits for your answer instead." | registry `settings.go:2155`, skin `settings.go:219` |

### The autonomy section (generated, not registry rows)

`autonomyItems()` (`internal/tui3/settingsautonomy.go:82`) appends one row per
question kind, under the heading "questions while you are away", directly after
the approval-countdown row. The rows exist only when the conversation has a
project to keep rules in (`sheet.autonomyDoor`); values are the engine's
per-project rules, cycled by enter. Heading + 8 rows:

| row | default reading | fixed? | source |
|---|---|---|---|
| permission | ask me (person's word for the engine's policy) | no | settingsautonomy.go:82 |
| choice | recommend then go · 30s | no | settingsautonomy.go:82 |
| judgement | ask me | no | settingsautonomy.go:82 |
| clarification | ask me · never runs on a clock | fixed | settingsautonomy.go:99 |
| confirmation | ask me · destructive always asks | fixed | settingsautonomy.go:99 |
| landing | decide yourself | no | settingsautonomy.go:82 |
| assumptions | recommend then go · 10m | no | settingsautonomy.go:82 |
| already done | ask me | no | settingsautonomy.go:82 |

Each row's one line (verbatim, one line for all eight): "what happens to a
question of this shape when nobody answers it · kept for this project ·
/autonomy <kind> ask · recommend <duration> · decide" (settingsautonomy.go:51).
The kind names and default words shown above are the section's own example
rendering, quoted from the file header (settingsautonomy.go:26-35); which rows
read which word is per-project state, not code.

## 7. Tasks

Seven registry rows.

| key | label | widget | kind/choices | default | about (verbatim) | source |
|---|---|---|---|---|---|---|
| task.start | starting a task | cycle | choice: sized/single | sized | "what /task does with your brief: sized reads it for width first, so the one worker that starts can hand the parts out once it has opened the material, single starts that worker without reading the brief at all." | registry `settings.go:2102`, skin `settings.go:189` |
| task.audit | check task work | cycle | choice: on/off | on | "each task's work is checked over before it merges. Off merges on the task's own word." | registry `settings.go:2113`, skin `settings.go:201` |
| task.repair_rounds | task repair rounds | text | count | 1 (settings.go:1330) | "times a task that came back with something missing is sent back to finish it before it lands as incomplete. 0 lets the first gap end it." | registry `settings.go:2167`, skin `settings.go:231` |
| task.parallel | tasks at once | text | count, blank = no limit | blank/0 (settings.go:1341) | "how many tasks may run at the same time. Blank is no limit — the machine and the provider are the real ceilings." | registry `settings.go:2181`, skin `settings.go:237` |
| task.max_load | busy machine | text | number, per core | 1.5 (settings.go:1349) | "the load per core at which new tasks wait instead of starting. Running tasks are never touched. 0 stops watching." | registry `settings.go:2196`, skin `settings.go:241` |
| task.min_free_mb | memory floor | text | count, unit MB | 1536 (settings.go:1357) | "MB of memory that must be free before another task starts. 0 stops watching." | registry `settings.go:2206`, skin `settings.go:245` |
| task.model | task model | select (model picker) | text | blank ("follows the conversation") | "the model a task runs on when you have not asked for another. Blank runs it on the model you are talking to." | registry `settings.go:2222`, skin `settings.go:252` |

## 8. Providers

Which model answers what. 27 registry rows, plus two generated sections.

Registry rows in drawn order (modelsSection first, `settings.go:700`, then
registry order — note the model rows are built first, so the nine non-led model
rows land between the roles section and "looking"):

| key | label | widget | kind/choices | default | about (verbatim) | source |
|---|---|---|---|---|---|---|
| model.talk | your model | select | model | the live conversation model | "the model you are talking to. Everything below it is a model codeaf uses on your behalf." (set by `init`, `settings.go:691`); registry label is "conversation", hint "the model that answers you here. It changes on your next message." (`settings.go:2960`) | registry `modelRow` `settings.go:2686`; skin `settings.go:691` |
| lane.talk | provider | lane (opens the picker's provider fold) | text (auto / openrouter / pinned: <name> / pinned: <name>, borrow when slow) | auto | dynamic — `laneAutoSaid(s.routing)` (`palette.go:1652`); under the default routing `simple`: "which provider answers your model. routing is simple, so auto sends no choice of ours at all and openrouter's own routing answers; a provider you pin is the whole request. enter opens them all with what has been measured of each." | registry `settings.go:2038`, skin `settings.go:699` |
| lane.guard | speed guard | toggle | bool | on (settings.go:935) | "an answer that is slow to start is asked of the next-best provider as well, and you read whichever replies first. One extra call, under a tenth of spend." | registry `settings.go:2055`, skin `settings.go:703` |
| routing | routing | cycle | choice: simple/latency/price/off | simple | "one model is served by many providers. simple is the one it ships with and sends no preference of ours — no pinned provider means the router's own default answers, and a pinned provider is the whole request; latency asks for the fastest and demotes one that keeps being slow; price asks for the cheapest; off asks for nothing, measures nothing, and leaves the two rows above it with no provider to name. a change here takes effect on your next message." | registry `settings.go:1999`, skin `settings.go:779` |
| prompt.profile | prompt profile | cycle | choice: auto/lean/full | auto | "how much codeaf tells the model before you type. auto reads the model's context window and goes lean under 32,000 tokens; lean and full say so yourself, for a provider that reports a window its model does not really have." | registry `settings.go:2022`, skin `settings.go:710` |
| models.tiers.reflex | reflex | select | model | mistralai/mistral-nemo (shipped, settings.go:1017) | "near-free · reads every turn — memory, titles, safety" | registry `settings.go:2297`, skin `settings.go:334` |
| models.tiers.low | small work | select | model | deepseek/deepseek-v4-flash-0731 (shipped) | "cheap · the small calls — names, digests, the safety gate" | registry `settings.go:2306`, skin `settings.go:364` |
| models.tiers.worker | worker | select | model | empty — auto, routed per task | "does the work · every task, its parts, every run node — most of the bill" | registry `settings.go:2317`, skin `settings.go:368` |
| models.tiers.high | checker | select | model | empty — auto, routed per task | "checks what must not be wrong — audits, briefs, vision" | registry `settings.go:2327`, skin `settings.go:372` |
| models.tiers.mastermind | planner | text | model, may carry :low/:medium/:high | empty — auto, routed per task | "plans runs and designs harnesses — add :low, :medium or :high" | registry `settings.go:2338` |
| models.roles | pinned roles | text | text (role:model pairs) | none | "exceptions to the five rows above, one per role: title:openai/gpt-5-mini." | registry `settings.go:2347`, skin `settings.go:386` |
| model.plan | planning | select | model | blank, "follows execution" | firstSentence of hint: "the model that plans and reviews the work." (full hint `settings.go:2965`: "the model that plans and reviews the work. Empty follows the work model.") | registry `settings.go:2686` (modelRow), skin `settings.go:683` (init) |
| model.work | execution | select | model | the work model | "the model that does the work." (full hint: "the model that does the work. It changes on the next job.") | registry modelRow; skin init |
| model.verify | verification | select | model | blank, "follows execution" | "the model that checks the work — gates, judges, second opinions." (full hint adds "Nothing reads this binding yet; it is written down and waiting.") | registry modelRow; skin init |
| model.scribe | naming | select | model | blank, "follows execution" | "the model that writes the short things — titles, labels, summaries." (full hint adds "Nothing reads this binding yet; it is written down and waiting.") | registry modelRow; skin init |
| model.image | drawing | select | model, picker filtered to models that can draw | automatic | "the model that draws." | registry `mediaModelRow` `settings.go:2738`; skin init |
| model.speech | speaking | select | model | automatic | "the model that speaks." | registry mediaModelRow; skin init |
| model.music | composing | select | model | automatic | "the model that composes." | registry mediaModelRow; skin init |
| model.video | filming | select | model | automatic | "the model that films." | registry mediaModelRow; skin init |
| model.voice | voice | select | model | automatic | "the model that hears you when you speak." | registry mediaModelRow; skin init |
| vision_model | looking | select | model, picker filtered to models that can see | automatic | "the model that looks at images. Blank picks one that can see." | registry `settings.go:1777`, skin `settings.go:674` |
| effort | thinking | cycle | choice: auto + the effort rungs (auto, plus five explicit levels; `EffortChoices`, `config/effort.go:29`) | auto (effort.Ship = None, `effort/effort.go:63`) | "how hard the model thinks, unless something nearer the work says otherwise. ctrl+v moves the rung of whatever you stand on — the rung beside the model above the message box for one conversation, a task, or a standing item — and ctrl+t in /model dials one model. This row answers for everything nobody dialled." | registry `settings.go:1789`, skin `settings.go:757` |
| document_engine | reading | cycle | choice: auto/local/free/ocr | auto (config.go:62) | "which rung reads your documents. auto walks local, then free, then paid OCR." | registry `settings.go:1798`, skin `settings.go:680` |
| api_key | openrouter key | text | text, secret | not set | "the key codeaf talks to models with. A missing default key opens connect openrouter in your browser; paste a replacement here if needed. A change lands on this conversation at once." | registry `settings.go:1829`, skin `settings.go:476` |
| reply.guard | reply guard | cycle | choice: on/off | on | "on cuts a reply that has come apart — one line or one letter repeated, alphabets mixed inside words — throws it away and asks once more. Code blocks are never judged." | registry `settings.go:2417`, skin `settings.go:427` |

### The roles section (generated, not registry rows)

`roleItems()` (`internal/tui3/settings.go:1558`) hangs one row per REGISTERED
role directly under "pinned roles", grouped under five headings reading
"roles · <tier label>". Every pin these rows write lands in models.roles — the
section is a view of that one registry row. Registered roles in this binary: 7
from `roles.DefaultAssignment` (`internal/roles/roles.go:369`) plus 16
registered from internal/session init functions. 23 rows in 5 groups:

| tier group heading | roles (row labels) |
|---|---|
| roles · reflex | reflex |
| roles · small work | title, caption, router, consolidate, task-name, job-name, intake, guardian, sentinel, spellout |
| roles · worker | worker |
| roles · checker | careful, auditor, repair, shaper, vision |
| roles · planner | planner, designer, mark-reader, handoff, division, router-confirm |

Each row shows the model the role resolves to right now; a pinned row also says
"pinned". The one line under a row (verbatim): unpinned → "<description> ·
follows <tier> above. enter pins it to a model of its own."; pinned →
"<description> · pinned, so it ignores <tier> above. del clears the pin."
(`roleAbout`, `settings.go:1632`). Descriptions come from
`roles.Describe` (`internal/roles/roles.go:421`), e.g. planner — "the plan that
steers an adaptive run", auditor — "whether finished-looking work is actually
finished", guardian — "is this one tool call plainly safe".

### The services section (generated, only with custom connections)

When the profile has custom model sources, a "services" heading plus one row per
persisted connected service, then an "add custom connection" row and (with two
or more custom connections) an "active connection" row, all after the
openrouter-key row (`sheet.build`, `settings.go:1331-1347`;
`modelServiceRows`/`customAddRow`/`connectionSwitcherRow`,
`internal/tui3/modelservices.go:1095-1183`). Values are built from the door
name, key env, region and order (e.g. "coding-plan · $Z_AI_API_KEY · order 1").
A service with an unmetered overflow plan also gets a "when the plan is paused"
row. This is the section branch `feat/1089-custom-connections` is about.

## 9. Connections

Not the registry at all. `buildConnections()` (`connectcaps.go:332`) reads the
engine's account catalog (connected accounts via `app.conns`, model services
via `app.modelRows`) and groups it (`groupConnections`, `connectcaps.go:441`):
a "models" group first, then the connected accounts flat with no heading, then
everything available under lowercase category words ("billing", "other", …).

Row types, all through the same settings-row grammar (`connRow`,
`connectcaps.go:205`):

| row type | what it shows | widget/answer |
|---|---|---|
| service (connected) | "✓ <name>" + the account email or $KEY_ENV; one dim summary line of its capabilities when closed (`connSummary`, connectcaps.go:402) | enter expands it |
| capability (of the open service) | the capability's own phrase, e.g. "read your mail" | the word is the control: yes / ask first / off, cycled by enter (`nextCapState`) |
| disconnect | the last row of the open service | "disconnect", then "enter again" to confirm |
| service (available) | name + one dim tag: "key" or "sign in" (tag only once the list is too long to read the sentences of, `connBrowsing`) | enter starts the same sign-in /connect starts, or opens the same masked key box |
| waiting | "waiting in your browser…" / "checking your key…" while the trip is out | — |

The search box here filters the accounts rather than the registry
(`onConnections`, connectcaps.go:326) — the only tab where typing does not
search settings.

---

## Counts

Registry rows (this branch, `internal/config/settings.go`):

| part | count | source |
|---|---|---|
| fixed rows in the build literal | 66 | lines 1776-2563, counted by extracting every `Key:` in the literal |
| model-slot rows | 10 | 5 roles (`store.ModelRoles`, role_bindings.go:92) + 5 modalities (`mediaModalities`, models.go:322) |
| standing.background | +1 when the caller wires a watch — the v3 panel and the session tool both do | settings.go:~2455, backgroundRow settings.go:2830 |
| split_pct | +1 only when the caller wires SaveSplitPct — neither the v3 panel nor the session tool does | settings.go:2480-2502, splitRow settings.go:2876 |

Total registry rows a session's settings tool lists: **77** (66 + 10 + 1).
Total on a surface that passes SaveSplitPct (v1/v2 sheets): 78.
The same count on `origin/dev` is **81** (70 fixed + 10 + 1): four rows landed on
dev after this branch forked (merge base 09299019b, 2026-09-16) - see the
reconciliation below.

Registry rows per tab (Session 2 + Context 8 + Workspace 12 + Display 9 +
Spending 4 + Safety 8 + Tasks 7 + Providers 27 = 77) account for every row a
session's settings tool lists: 66 fixed + 10 model slots + 1 background
checks.

Panel rows (v3), per tab, on an ordinary profile with one custom-connection
source absent and a project open:

| tab | registry rows | generated rows | total row bodies |
|---|---|---|---|
| Session | 2 | 0 | 2 |
| Context | 8 | 0 | 8 |
| Workspace | 12 | 0 | 12 |
| Display | 9 (10 mapped; chat width never built) | 0 | 9 |
| Spending | 4 | 3 always (today, per task, per standing run) + 2 only when writes were dropped or calls went unpriced | 7 (up to 9) |
| Safety | 8 | 8 autonomy rows + 1 heading, only with a project | 16 + 1 heading |
| Tasks | 7 | 0 | 7 |
| Providers | 27 | 23 role rows + 5 group headings; + services section when custom connections exist (1 per service + add row + switcher) | 50 + headings (+ services) |
| Connections | 0 | the whole tab: every connected account (1 + its capabilities + disconnect) and the whole available catalog, grouped | dynamic |

Panel total on that profile: 77 registry rows + 34 generated rows = 111 row
bodies, plus the Connections tab on top.

### Reconciling the "81 settings" figure

The brief says a settings listing from the product's own settings tool reported
"81 settings". The count in that listing is the number of registry rows the
tool's registry builds (`settingListing`, `internal/session/tools_settings.go:325`,
header at line 361: "%d settings, as they read now."). On this branch that
number is 77; on `origin/dev` it is exactly 81, and the figure reconciles with
no guesswork at all.

This branch forked from dev at 09299019b (2026-09-16, the merge base), and two
waves landed on dev after the fork carrying four registry rows this branch does
not have. Counting `Key:` entries inside `func (s *Settings) build` the same
way on `git show origin/dev:internal/config/settings.go` gives 70 fixed rows,
and 70 + 10 model slots (dev's `ModelRoles` is the same five,
role_bindings.go:92, and its `mediaModalities` the same five) + 1 background
checks (dev's session tool wires the watch too, tools_settings.go:140 on dev)
= 81. Any binary built from current dev history prints 81; no build of this
branch can, because its registry tops out at 77 (78 with split_pct).

| key (on dev, not here) | the row | landed on dev in |
|---|---|---|
| model_pool | CategoryModels, choice, label "model pool" (dev settings.go:1911) | fde49a587, #1194 |
| models.pool.public_key | CategoryModels, text, label "pool key" (dev settings.go:1926) | fde49a587, #1194 |
| telemetry | CategoryInterface, bool, label "telemetry", default on (dev settings.go:2627) | cc8bea7e9, #1095 |

The `Key:` diff between this branch's build and dev's is exactly those three
rows and nothing else: every key this branch has, dev has too, and no key dev
has is missing here but those three. The three-row difference is fixed rows that
landed on dev after the fork, not conditional rows and not rows this branch
retired. The deletion comment at settings.go:3046 names two rows gone outright
(`practice_demand_pct`, `propose_new_skills`), and an earlier draft of this
section blamed them for the gap; that was wrong and this section replaces it:
both rows are absent from `origin/dev` as well, so they explain no difference
on either side.

## Discrepancies

### (a) Registry keys with no settingUI row

None, by force: `TestEverySettingRowHasATab`
(`internal/tui3/chrome_test.go:103`) fails the build on a registry key with no
`settingUI` entry. The reverse hole exists instead: `split_pct` (chat width)
has a `settingUI` entry (`internal/tui3/settings.go:646`) whose registry row is
never built on the v3 panel — the mapping is total over a registry that is
smaller than the map.

### (b) Panel labels that differ from the registry's own Label

The settings tool prints the registry's Label; the panel shows its own. A
person told "change the daily budget" by the chat and then searching the panel
for the word "budget" will not find it — though the search does match the key
(`daily_budget_usd`), which is the escape hatch.

| key | registry Label | panel label |
|---|---|---|
| daily_budget_usd | daily budget | per day |
| plan_consent_usd | ask before spending | per plan |
| practice_budget_usd | practice budget | practice |
| session.spendRailUSD | session ceiling | per conversation |
| tools.approval | tool approvals | tool exceptions |
| task.audit | task audit | check task work |
| task.settle | who settles a task nobody could check | who settles work that needs a look |
| google_oauth_client | google app id | google sign-in id |
| google_oauth_secret | google app secret | google sign-in secret |
| slack_oauth_client | slack app id | slack sign-in id |
| context_fill_pct | context fill | compact at |
| model.talk | conversation | your model |

### (c) Overlapping or confusable wording

- THE TWO APPROVAL LISTS. "tool exceptions" (tools.approval) and "shell
  command rules" (tools.bashPatterns) both answer for the same gate, one row
  apart, and the first row's own about names bash: "exceptions to the answer
  above, one per tool: read:allow, bash:prompt." — while the second row's
  about is "answers for single shell commands, first match wins: allow git
  status*, deny rm -rf *." A person who wants bash to stop asking has two rows
  that both look like the answer, and the registry's own hint for tool
  approvals says bash belongs to the other one.
- THREE CLOCKS ON SAFETY. "approval countdown" (approval.timeout_seconds,
  "seconds an approval question counts down before it pauses and keeps
  waiting"), "task countdown" (task.autoapprove_seconds, "seconds a proposed
  task waits for you before it starts") and "background after"
  (bash.background_after_seconds, "seconds a foreground command runs before
  it is kept running as a background job") are all counts in seconds on the
  same tab; the first two differ only in what is waiting.
- "ask before running" vs "ask before spending" vs "who settles work that
  needs a look". Three rows whose labels all begin with a verb about asking,
  across Safety and Spending; the panel renamed plan_consent to "per plan",
  which removes one collision, but the registry label "ask before spending" is
  still what the settings tool prints.
- THE SAME QUESTION SPLIT ACROSS TWO TABS. "check task work" (task.audit)
  lives on Tasks and "who settles work that needs a look" (task.settle) lives
  on Safety, though the registry's own comment says settle "sits under
  `task.` beside the audit row because it is the other end of that row's
  question" (settings.go:459). Searching finds both; browsing finds one at a
  time.
- TWO "MEMORY" ROWS, UNRELATED. "memory" (memory.enabled, Session — what
  codeaf remembers of you) and "memory floor" (task.min_free_mb, Tasks —
  machine RAM) share a word for different subjects; the Tasks tab also has
  "busy machine" one row above, which is the pair's real subject.
- TWO "BACKGROUND" ROWS, UNRELATED. "background after"
  (bash.background_after_seconds, Safety — foreground commands becoming jobs)
  and "background checks" (standing.background, Workspace — the machine's
  timer). Different tabs, same first word.
- "session ceiling" vs the Session tab. The registry calls
  session.spendRailUSD "session ceiling" and the settings tool prints that;
  the panel moved the row to Spending as "per conversation" — a person reading
  the Session tab's promise ("this conversation and only this conversation")
  will not find the row that bounds this conversation.
- "thinking" (effort) vs the planner row's ":high". Two rows both about how hard a
  model thinks; effort's about says it "answers for everything nobody dialled"
  and names ctrl+v and /model's ctrl+t, and the planner row's about says
  "add :low, :medium or :high". The distinction (install-wide rung vs one
  tier's level) is stated only in the about lines.
- The Connections tab name does double duty: it is the accounts tab, and the
  four ssh rows' own comment says "THE TAB LITERALLY NAMED `Connections`
  COULD NOT TAKE THEM… the word doing two jobs on one screen is a real defect
  and it is still open" (settings.go:140-153).
