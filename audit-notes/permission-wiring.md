# Permission wiring: the settings row, the gate, and the card

An audit of how `tools.approvalMode` travels from `/settings` to the running
gate to the approval card, and of every word each surface uses on the way.
Branch `feat/1089-custom-connections`, 2026-09-20. Evidence base only - no fixes
proposed here.

The question behind it: a person using `/settings` reports that the wording
around on/off/allow/ask is confusing - the row labelled "ask before running"
cycles `prompt / allow / deny`, while the card they are then shown answers in
different words - and suspects a settings change is "not properly in
permission". Both suspicions are answered below: the wiring is live and real
(there is one project-layer exception that can silently swallow a sheet edit),
and the vocabulary is genuinely split across at least four spellings for the
same three answers.

## 1. The flow, in one diagram

```
/settings, Safety tab
  row "ask before running"                    internal/config/settings.go:1943
  key  tools.approvalMode                     internal/config/settings.go:131
  choices  prompt / allow / deny              internal/config/settings.go:513
  default  prompt                             internal/config/settings.go:1293
  read   ToolApprovalModeAt(profileDir)        internal/config/settings.go:1948
         PROFILE ONLY - no project layer      internal/config/settings.go:3430
  write  writeChoice -> the profile's config.json
                                              internal/config/settings.go:1949
        |
        |  the sheet never writes the project layer
        |  (internal/tui3/settings.go:57-60, footNote at :3163)
        v
launch and every live rebuild
  v3Policy(workspace, profileDir, yolo)       cmd/codeaf/chatv3.go:1589
  mode = ProjectStringAt(workspace, profileDir, tools.approvalMode)
                                              cmd/codeaf/chatv3.go:1590
  ProjectStringAt                             internal/config/projectconfig.go:361
    -> .codeaf/config.json at the cwd (no walk up)
    -> ResolveString                           internal/config/projectconfig.go:260
       project value present -> PROJECT WINS (validated, not forgiven)
                                              internal/config/projectconfig.go:269-280
       absent -> ToolApprovalModeAt(profileDir)  the profile
                                              internal/config/projectconfig.go:285-286
  --yolo replaces the DEFAULT and nothing else cmd/codeaf/chatv3.go:1594-1596
  raw = {"default": mode, "tools": ..., "bash.patterns": ...}
  approval.Load(raw)                           internal/approval/approval.go (Load)
        |
        v
the running session
  Config.ApprovalPolicy at launch; a standing pointer after that
  every read through approvalGate()            internal/session/approvalgate.go:64
  approve() called PER TOOL CALL as the pre-action pass
                                              internal/session/hooks.go:500
                                              internal/session/consent.go:216
  Policy.Check(tool, args)                     internal/approval/approval.go (Check)
    allow  -> the call runs
    prompt -> a question is raised (after memo, guardian and the two floors)
    deny   -> refusal "denied by approval rule: <rule>",
              the model is told no and keeps going
                                              internal/session/consent.go:231
        |
        v
the card (internal/tui3)
  "needs your ok to run bash"                   internal/tui3/consent.go:148
  [1] allow once  [2] always  [3] deny  [esc] later
                                              internal/session/answers.go:218-222
  bash's widening answer relabelled
  "always, this command"                        internal/tui3/consent.go:393
  a banked always -> RememberToolApproval / RememberBashApproval (allow only)
  -> refreshV3Policy pushes the rebuilt gate   cmd/codeaf/chatv3_approval.go:84-101
```

## 2. Who reads the mode, and what each value does

The gate readers, all of them:

- `cmd/codeaf`'s `v3Policy` (cmd/codeaf/chatv3.go:1589) - the launch build, and
  every live rebuild through the same function (section 5).
- `internal/session`'s `Agent.decide` (internal/session/consent.go:169) reads
  the standing policy per call - not the key; the key is read only at
  launch/rebuild, then handed over as a `Policy`.
- `internal/tui3`'s `approvalPosture` (internal/tui3/app.go:8992) - the YOLO
  badge, reading the PROFILE live (section 10, bug 2).
- `internal/remote`'s Welcome.ApprovalMode (internal/remote/wire.go:898) - the
  hosted window's badge, answered by the ENGINE machine once at boot
  (cmd/codeaf/engine.go:730).
- `internal/session`'s `promptMode` (internal/session/looped.go:967) - reads the
  policy DEFAULT to ask "is this a session where somebody is expected to answer
  questions"; used by the approval tests, not the gate itself.

What the three values do to one call, from `Agent.approve`
(internal/session/consent.go:216-310) and `Policy.Check`
(internal/approval/approval.go):

- `allow` - `approve` returns `(toolResult{}, true)` at consent.go:227-228 and
  the call runs. Two floors can still turn a blanket allow into a prompt: the
  critical bash table (floor.go, checkBash) and calls that act in the person's
  name outside the machine (approval.go `actsInThePersonsName`:
  `gmail_send`, `calendar_create`, `slack_send`, and any `*_request` tool whose
  verb is not GET). A connected account's capability set to yes can lift the
  prompt back (consent.go:182-191); a capability set to ask floors an allow
  back down (consent.go:192-197); neither touches a deny.
- `prompt` - the call blocks and a question is raised. Before the person sees
  it, in order: a session memo for this tool can stand in
  (consent.go:244-253, refused with "denied by approval rule: <rule>
  (remembered for this session)" if the remembered answer was no), then the
  guardian, if on, can turn the prompt into an allow (guardian.go). Neither the
  memo nor the guardian is consulted for the two floors
  (internal/approval/floor.go `AlwaysAsks`).
- `deny` - refusal at consent.go:231, "denied by approval rule: " + the rule.
  The model receives an error result it can act on; the turn is not ended.

`ParseAction` (internal/approval/approval.go) is the authority for the three
words: a settings file that says "ask" or "always" is an error, never a silent
reinterpretation.

## 3. The approval card, verbatim

The single-call card, as drawn (internal/tui3/consent.go:20 documents it, and
the strings come from the sources cited):

```
╭─ ? needs your ok to run bash ──────────────────────── bash · 7s ─╮
│ rm -rf build · bash pattern "rm -rf *"                           │
│                                                                  │
│   1  allow once                                                  │
│   2  always, this command                                        │
│ ▸ 3  deny                                           safe answer  │
│                                                                  │
╰─ ↑↓ choose · enter take it · esc later ──────────────────────────╯
  c change · ? ask back · 1–3 jump
```

Every card string about approvals, with its source:

- "needs your ok to run " + tool - `consentHead`,
  internal/tui3/consent.go:148-150. The session's own lead is the same
  sentence (`consentHeadLead`, internal/session/consent.go:604), so the card
  that arrives on the questions lane and this one are the same question.
- "allow once" - option 1, internal/session/answers.go:218.
- "always" - option 2, marked widening, internal/session/answers.go:220. Its
  label is rewritten per tool by `alwaysWord`, internal/tui3/consent.go:393-401:
  "always, this command" (bash), "always, this tool" (everything else),
  "always, this tool (session)" when nothing is wired to write.
- "deny" - option 3, marked safe, internal/session/answers.go:222.
- "later" - the esc word, internal/tui3/questionkeys.go:325.
- "safe answer" - the dim mark on the refusal, internal/tui3/questionpanel.go:64.
- "↑↓ choose · enter take it · esc later" - the bottom edge, constant across
  widths (internal/manual/chat/permissions.md documents the law; the words are
  the question block's own).
- the reason line under the offer - the rule in the rules' own words
  (internal/approval `Decision.Rule`): `default`, `default (unset)`,
  `tool "edit"`, `bash pattern "rm -rf *"`, `critical command "rm -rf /"`,
  `<tool> acts in your name outside this machine`, `you said yes to "<phrase>"`,
  `"<phrase>" is set to ask first`.
- "it will not run this without your word" - `ConsentFallbackReason`, the
  reason shown when the rule line is empty, internal/session/question.go:2539.
- "allowed" / "denied" - the transcript row annotation, `decisionWord`,
  internal/tui3/consent.go:440-445.
- "always · saved — /permissions to change" - the annotation when the widening
  yes was persisted, `consentSavedWord`, internal/tui3/consent.go:278.
- "denied · no answer" - what a PREVIOUS build wrote when the clock answered
  no; this build never writes it (`consentExpiredWord`,
  internal/tui3/consent.go:194). Kept as the named lesson F41.
- the frame variant for several calls from one batch:
  "allow all " + N (internal/tui3/questionset.go:120), "one by one" (:121),
  "deny all" (:122), options at internal/tui3/questionset.go:412.
- what the model is told on refusal, all of it
  (internal/session/consent.go): "denied by approval rule: <rule>" (:231),
  "denied by approval rule: <rule> (remembered for this session)" (:252),
  "denied by the person: <rule>" (:296), "not approved: the question timed
  out" (:307), "not approved: ended before an answer" (:309),
  "needs approval but no resolver is attached: <rule>" (:284),
  "refused in a task: <rule> — nobody to ask" (:277).

"Allow or deny", the phrase the person reported, exists nowhere in the repo
(grep for `allow or deny`: no matches). It is a close paraphrase of the card's
answers - the two words the card and the settings row do share. The card never
says "prompt": the question itself is the asking, so the settings word for
"ask" never appears at the moment of asking. That gap is mismatch 1 below.

## 4. Precedence: the project layer outranks the sheet

The `/settings` sheet reads and writes the GLOBAL profile, by design. The
registry row's own reader is `ToolApprovalModeAt(dir)` - profile only
(internal/config/settings.go:1948), and the sheet's header comment states the
law (internal/tui3/settings.go:57-60): "Every write goes through
[config.Setting.Apply], which validates in plain language and persists to the
GLOBAL profile. The project layer (<workspace>/.codeaf/config.json) is
deliberately not writable from here".

The gate reads the PROJECT ladder. `v3Policy` resolves the mode through
`config.ProjectStringAt(workspace, profileDir, config.KeyToolApprovalMode)`
(cmd/codeaf/chatv3.go:1590), and `ResolveString` is the whole answer
(internal/config/projectconfig.go:260-288): a project file that carries the key
WINS when present and is validated rather than forgiven (:269-280); absent the
key, the row falls through to `ToolApprovalModeAt(profileDir)` (:285-286). The
project file is `.codeaf/config.json` at the cwd, no walk up to a git root
(the merge law at internal/config/projectconfig.go:34-43, the read-at-cwd law
at :57-58). So the real order,
for all three safety rows, is:

    project file  >  profile  >  default ("prompt", settings.go:1293)

Consequences, each already stated in the code's own comments:

- The sheet can DISPLAY one value while the gate uses another. A repository
  whose `.codeaf/config.json` says `"tools.approvalMode": "allow"` runs every
  call unasked while the Safety tab shows the profile's `prompt`.
- A sheet edit inside such a repository is a silent no-op for that workspace.
  The write lands, the live rebuild succeeds (it re-reads the project ladder,
  chatv3_approval.go:115-122), the receipt says nothing is wrong - and the gate
  is unchanged, because the rebuild re-read the project's answer.
- For `tools.approval` and `tools.bashPatterns` the project row replaces the
  person's WHOLE (projectconfig.go law 2, "NOTHING DEEP-MERGES"): a consent
  card's "always" written inside such a repository takes effect everywhere
  EXCEPT that repository (cmd/codeaf/chatv3_approval.go:37-43, "THE PROFILE IS
  THE PLACE"; internal/config/approvalmemory.go head; internal/tui3/
  permissions.go:57-60).

The only hint a person gets is the sheet's one foot line, shown on every tab:
"saved to your profile · a project's own .codeaf/config.json is a hand edit"
(internal/tui3/settings.go:3163). Nothing on the row, nothing on the badge,
nothing on the card says a project layer is answering this row.

This repo (agentfield/codeaf) carries no `.codeaf/config.json` (checked on this
branch: neither `.codeaf/` nor the legacy `.aforge-v3/` exists), so on this
machine today the precedence trap is latent, not live - the person's report is
answered by the wiring being live (section 5) and by the vocabulary (section 7).
The trap is real for any workspace that does carry one.

## 5. Live or next launch

Both questions have a definite answer in the code.

Is the mode read per tool call? The POLICY is. Every call goes through
`Agent.approve` as the pre-action pass (internal/session/hooks.go:500,
consent.go:216), and every read of the policy goes through `approvalGate()`
(internal/session/approvalgate.go:64-69), which returns the pushed pointer if
there is one and otherwise the launch's. The KEY is not read per call - that is
the file walk the gate deliberately avoids (approvalgate.go:15-21: "a gate that
re-read them on the tool path would put a file walk in front of every call").
A change becomes live by a PUSH, not by a re-read.

Does changing it in /settings affect the current conversation immediately?
Yes, when the door is wired. `applySetting` (internal/tui3/settings.go:2237)
has the three safety rows on the live seam (settings.go:2276-2298): after the
registry write, `approvalsReloaded()` (internal/tui3/permissions.go:474-479)
calls the `ApplyApprovals` seam, which is `applyV3Approvals`
(cmd/codeaf/chatv3.go:638, chatv3_approval.go:115-122) - a full `v3Policy`
rebuild through the project layer, pushed into the running agent with
`SetApprovalPolicy` (internal/session/approvalgate.go:46). The YOLO badge is
re-read on the same keystroke and only on the branch where the push landed
(settings.go:2294-2295). This landed in change entry
`docs/changes/unreleased/1063-approval-row-lands-live.md`: before it, the row
landed on the next session only.

When the seam is not wired (nil), or the rebuild fails, the panel says so in
one sentence instead of claiming an effect: "saved · from the next session"
(`gateNextSessionWord` = "saved" + `nextSessionWord`, internal/tui3/settings.go:
2230; `nextSessionWord` = " · from the next session",
internal/tui3/permissions.go:99). The same words are `/permissions`' own
receipt when it drops a rule the running gate could not be told about
(permissions.go:407-408). A linked-local engine has the same door from the
other side: `RefreshApprovals` (cmd/codeaf/engine.go:709-711,
internal/remote/server.go:2503) rebuilds the engine's running gate before a
banked approval crosses the wire.

One live exception by design: `--yolo`. The flag replaces the DEFAULT for the
whole run and writes nothing down (cmd/codeaf/chatv3.go:1594-1596,
v3SurfacePosture at :1573), so cycling the row to `prompt` in `/settings`
mid-session saves the row for the next launch while this run stays on allow -
and the badge stays up, because the badge reports the posture in force
(internal/tui3/app.go:8951-8999; the manual states the same law under
"--yolo").

## 6. tools.approval - the per-tool exceptions

The row is `name:action` pairs, comma, semicolon or newline separated
(`ParseToolApprovals`, internal/config/settings.go:3722; duplicates are an
error, not last-one-wins). A tool rule beats the default for that tool
(internal/approval/approval.go, `Policy.base`), and YES - an exception can turn
a blanket allow back into a prompt: `bash:prompt` holds under both `allow` and
`--yolo`, because the flag "replaces the default and nothing else"
(cmd/codeaf/chatv3.go:1594-1596).

Two floors and one replacement law surround it:

- The seeded floor: `v3BuiltinApprovals` (cmd/codeaf/chatv3.go:1646-1706) seeds
  read, grep, find, ls, jobs, remember, track, recall, manual and settings as
  allow UNDER whatever the person wrote, so a first "always" on any tool does
  not strip the free reads. `commit` and `change_setting` are deliberately not
  on it.
- The acts-in-the-person's-name floor yields only to a rule that NAMES the tool
  (`gmail_send:allow` runs silently; approval.go:71-91) or to a capability set
  to yes (consent.go:182-191).
- The project replacement law: a repository answering `tools.approval`
  replaces the person's whole row, seeded floor and all (section 4).

## 7. tools.bashPatterns - the shell command rules

The row's own format (internal/config/approvalmemory.go:86-145): one rule per
entry, ACTION FIRST, entries separated by commas or newlines, the glob
optionally Go-quoted - "allow git status*, deny rm -rf *". The action leads
"because it is the short, fixed half: a person scanning the row is looking for
the word deny". Order is preserved exactly and FIRST MATCH WINS - a deny at
the top outranks a consent card's allow appended below, and the card refuses
to write one anyway (RememberBashApproval's fourth refusal).

The matching law (internal/approval/bash.go:11-40) is asymmetric and is the
whole safety argument: deny and prompt fire when the glob matches the whole
line OR ANY SINGLE SEGMENT of a compound one; allow fires only on an entire
line and never on a compound line at all. The critical table (rm -rf /, mkfs,
dd of a device, shutdown, the fork bomb - the full table is in
internal/approval/bash.go and quoted in internal/manual/chat/permissions.md)
is a floor under allow only: it turns an allow into a prompt
(`critical command "rm -rf /"`), never a refusal of its own; an explicit deny
still denies.

How the three combine for one bash call (approval.go `Policy.Check` ->
`checkBash`): the tool's own rule (`bash:allow` and friends) or the default is
the starting point; the first matching pattern REPLACES it; the critical table
then floors any allow that is left. A pattern can therefore turn a blanket
allow back into a prompt or a deny - and an allow pattern cannot lift a
standing deny above it, because first match wins.

One quiet interaction: read-only lines (`git status` and flags) are allowed
without a question in default prompt mode ONLY while the pattern list is
EMPTY (approval.go `checkBash`: `len(p.BashPatterns) == 0` and
`liftsReadOnly`). The first rule anybody writes - even a deny - ends the free
`git status`. The manual documents it ("when no shell-command rule list has
been written"); the settings row's own hint does not.

## 8. The self-service guard

`selfServiceGuards` (internal/config/selfservice.go:75-90) refuses the model
the whole gate: `tools.approvalMode`, `tools.approval`, `tools.bashPatterns`,
`approval.guardian`, `approval.timeout_seconds` and `task.autoapprove_seconds`
all map to `guardConsent`. The refusal is built at selfservice.go:139 and
reads, for this row: `"ask before running" (tools.approvalMode) decides what I
may do without asking you first, so it is not mine to change. Open /settings
and change it yourself.` The in-chat settings list can therefore SHOW the row
and cannot act on it - which is vocabulary surface too: the model names the
row by the sheet's label.

## 9. Vocabulary mismatch table

The same three answers, as each surface spells them:

| Surface | ask | allow | deny |
| --- | --- | --- | --- |
| /settings row "ask before running" | `prompt` | `allow` | `deny` (settings.go:1943-1949) |
| the card, single call | - the question itself is the asking - | `allow once`, `always` | `deny`, `esc later` (answers.go:218-222) |
| the card, bash widening | - | `always, this command` (consent.go:393) | - |
| the card, frame | - | `allow all N` | `deny all`, `one by one` (questionset.go:120-122) |
| /permissions tails | `asks every time` (permissions.go:76) | `every call` (:77) | `refused` (:75) |
| /permissions heading | - | `what runs without asking` (:69) | - |
| transcript annotation | - | `allowed` | `denied` (consent.go:440-445) |
| saved-always receipt | - | `always · saved — /permissions to change` (consent.go:278) | - |
| rules rows' syntax | `bash:prompt` | `read:allow`, `allow git status*` | `deny rm -rf *` (settings.go:1952-1975) |
| what the model is told | - | - | `denied by approval rule`, `denied by the person`, `not approved: …` (consent.go:231-309) |
| task settle row | `ask` | `auto` | - (settings.go:579-587) |
| connected capabilities | `ask first` (consent.go:197) | `you said yes to …` (:188) | off, answered before the gate (consent.go:222-224) |
| on/off rows beside them | `off` / `on` - guardian, memory, task audit (settings.go:519-526) | | |
| YOLO badge | absent is the safe state | `YOLO` (render.go:3169-3176) | - |
| --yolo help | | "run every tool without asking: the approval default becomes allow" | |
| remote wire badge | empty | `allow` (wire.go:898-903) | - |

The pairs a person has to hold in their head at once:

1. The sheet's `prompt` and the card's existence. "prompt" appears only in
   /settings and in the rules rows' syntax; nowhere at the moment of asking.
   Matching "I am being asked" back to "the row is on prompt" is pure recall.
2. The sheet's `allow` and the card's `allow once`. On the card, "allow"
   unqualified does not exist; the plain word always carries a scope ("once",
   "all N"). In the sheet, `allow` means allow-everything-forever - what the
   card calls `always`. The card's nearest spelling of the sheet's `allow` is
   not on the card at all.
3. `deny` in the sheet, `refused` in /permissions, `denied` on the transcript
   row, `denied by approval rule` to the model. One answer, four spellings,
   three of them on surfaces the same person visits in one sitting.
4. `ask` means the approval value in the task settle row and in capabilities
   ("ask first"), and is a RETIRED task-start word a profile may still hold
   (settings.go:615-625), but the approval row's own value is `prompt`. Two
   words for asking on the same Safety tab.
5. The Safety tab mixes three vocabularies in one column: a three-word choice
   (prompt/allow/deny), two on/off cycles (guardian), and two counts
   (approval countdown, task countdown). "Ask before running" reads as an
   on/off row - its label is a question with a yes/no shape - but it is a
   three-way choice whose middle value is the default and is spelled with a
   word from none of the neighbouring rows.
6. "always" on the card writes `allow` into a rules row - the card's widening
   word and the row's action word differ, and the row is where the person goes
   to take it back (the receipt says so).
7. `esc` is `later` on the card and was once `cancel` - and cancelling meant
   denying (manual, permissions.md). A hand that learned the old word is
   holding a no it no longer has.

## 10. Mismatches and bugs

Each entry: what, evidence, one-line severity.

1. The sheet can display the profile's answer while the gate runs the
   project's. Read path: settings.go:1948 (profile only) vs cmd/codeaf/
   chatv3.go:1590 + projectconfig.go:269-286 (project wins). Severity: real but
   conditional on a `.codeaf/config.json` in the workspace; silent while it
   applies - no row, badge or card names the layer in force.
2. Inside such a repository a sheet edit is a live-acting no-op: the write
   lands, `approvalsReloaded` returns true (the rebuild succeeds - it re-reads
   the project ladder, chatv3_approval.go:115-122), the badge moves
   (approvalPosture reads the PROFILE, app.go:8999 +
   tui3/settings.go:2294-2295),
   and the gate is unchanged. The badge and the gate then disagree in the
   exact direction the false-safety comments warn about (#322, #325,
   tui3/settings.go:2261-2265): a profile of `allow` under a project `prompt` draws
   the YOLO badge over a gate that asks, and the reverse shows no badge over a
   gate that runs everything. Severity: high when a project file answers this
   row; the only surface that says anything is the sheet's foot line
   (tui3/settings.go:3163).
3. A consent card's "always" writes a preference that cannot apply inside a
   repository answering `tools.approval` - the card still prints
   `always · saved — /permissions to change` (consent.go:278). The law is
   stated in comments (chatv3_approval.go:37-43, approvalmemory.go head,
   permissions.go:57-60) and in the manual, never on the card itself. Severity:
   low in effect (the rule does apply everywhere else), but the receipt
   promises a change the workspace will not feel.
4. `prompt` has no spelling at the point of asking. The card never says it;
   /permissions says "asks every time"; capabilities say "ask first"; the task
   settle row says `ask`. Severity: the live complaint - pure vocabulary, and
   it is the one word that names the DEFAULT.
5. `deny` is spelled `refused` on /permissions (permissions.go:75), the surface
   whose whole job is showing what you wrote. Severity: wording, cheap to
   trip over exactly when a person is checking their own rules.
6. The first bash pattern anybody writes ends the free `git status` (approval.go
   `checkBash`: the read-only lift requires `len(p.BashPatterns) == 0`;
   seeded floor covers tools, not bash). Documented in the manual, absent from
   the row's hint. Severity: a real behaviour change made by an unrelated edit,
   invisible until the next `git status` asks.
7. The approval row's label "ask before running" is question-shaped and its
   default value `prompt` re-answers the question in the label - the row reads
   "ask before running: ask" at rest, and the on/off cycles beside it (guardian)
   invite reading it as a toggle that is already on. Severity: wording - this
   is the double-speak the person reported, and it is the row a first-time
   reader lands on.
8. `tools.approvals` is labelled "tool exceptions" in the sheet's skin
   (internal/tui3/settings.go:197) but "tool approvals" in the registry
   (settings.go:1952-1955) - the manual teaches the registry's spelling. Two
   labels for one row on the two surfaces a person crosses between. Severity:
   wording.
9. The in-chat settings list shows the gate rows but cannot write them
   (selfservice.go:81, refusal built at :139). Not a defect - a guard - but it
   is one more surface showing the row, in the sheet's own label words, that
   answers nothing. Severity: none as wiring; it belongs on the table.
10. Nothing "allow or deny" - the reported phrase does not exist as a string
    anywhere (grep: no matches). The card's actual answers are `allow once`,
    `always[, this command]`, `deny`, `esc later`. Severity: none - recorded so
    the next pass quotes the card rather than the report of it.

## 11. What this file is not

Not a proposal. Every fix question - one word for ask, the badge reading the
layer the gate reads, a project-layer note on the row - is deliberately left
open; this file is the evidence base those decisions get made against.
