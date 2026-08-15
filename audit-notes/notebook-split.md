# The notebook split — approved design (2026-08-11)

User-approved decision: **doing vs knowing**. The work page holds everything the
resident is *doing* (tasks, standing watches, services). The notebook holds
everything it has *learned* (beliefs, know-how, practice). The name "notebook"
stays — the split is what makes it honest. This document is the canon for the
three lanes building it; `audit-notes/design-law-v2.md` still governs every
rendering decision (tiers, glyphs, time, money, expand law, no-labels law).

## 1. What the resident actually accumulates (inventory, verified 2026-08-11)

Persisted kinds and where they live:

| kind | storage | key reads |
|---|---|---|
| beliefs (facts: preference/quirk/lesson/fact/unsettled) | `facts` table, `internal/store/facts.go:240` | `Facts`, kind/status/channel fields |
| taste rules ("you keep correcting…") | facts, kind `preference`, scope `taste:` (`facts.go:946`) | `TasteRules` `facts.go:990` |
| traits (measured about the user) | facts, kind `trait` (`internal/store/meta.go:234`) | `Trait` `meta.go:264`, `MeasureTraits` `traits.go:62` |
| playbooks (per-repo/tool/domain strategy) | facts, kind `playbook` (`facts.go:65`) | scope-restricted `facts.go:1970` |
| crafts (learned workflows, YAML in git) | git repo `<dir(db)>/craft/workflows/` (`internal/craft/repo.go:114`) | `Commander.Crafts()` `internal/command/craft.go:53`, `CraftDetail` `:79`; survival record as trait `craft:<name>` (`internal/resident/craftmind.go:314`) |
| skills (forged executables) | facts kind `skill` + `~/.aforge/skills/` (`internal/store/skills.go:14`) | `SkillFacts` `facts.go:730` |
| questions + practice runs | facts kind `question` + `question_practices` (`internal/store/practice.go`) | `Questions` `:141`, practice reads `:216,307` |
| self receipts (cost of self-directed work) | `self_receipts` (`internal/store/self_receipt.go:47`) | `SelfReceipts(since)` `:84`, `SelfSpendToday` `:116` |
| competence map (strengths/frontier) | derived, `internal/store/competence.go:151` | `CompetenceMap(opts)` |
| self-tuned dials (meta-parameters) | `meta_parameters` (`internal/store/meta.go:800`) | four tunables `meta.go:829`, changes journaled `parameter_changed` |
| standing charters + sentinels + tenure | `charters` (`internal/store/charter.go:524`), tenure `tenure.go` | — |
| services | `services` (`internal/store/services.go:112`) | — |
| territories | graph groups (`internal/store/territory.go`) | digest-only, stays invisible this wave |

Internal machinery (scope aliases, channel credibility, activation decay) never
appears as items — it surfaces only as receipt words ("trusted", "forming") on
the items it affects. Model bindings live in the model picker, not here.

## 2. Page contents

**work** (chat/board.go): the task board as landed, plus two bands below it —
**watching** (charters: invariant · cadence · state · last-fired · next-due ·
today count · cost/run) and **services** (name · life glyph · uptime ·
restarts). Same shared-column grammar as the board rows.

**notebook** (homes package): three sections, underline strip as landed.

```
 beliefs 512 · know-how 9 · practice
 ───────
  beliefs
  · prefers tables over prose in reports        you · trusted · 12d
  · deploy needs the VPN up first               env · trusted · 3d
  · you keep correcting: shorter commit lines   taste · forming
  · measured: you usually accept first drafts   trait · 14 samples

  know-how
  ⚒ release-notes      workflow · proved 4× · $0.31/run · v3 · 8d
  ⚒ fetch-pr-context   workflow · draft, never run · 1d
  ⌁ imgshrink          tool · used 11× · 21d

  practice
  ? how flaky is the e2e suite    practicing · 2 runs · $0.12
  ✓ uv beats pip in this repo     resolved · 4d
    strongest repo:aforge · frontier tool:docker
    today $0.84 · 42m practiced · 3 learned
```

Beliefs section holds facts + taste rows + trait rows + playbooks, each with an
honest kind word in the receipt (taste/trait/playbook; plain facts get their
scope word). Know-how holds crafts and skills chip-tagged `workflow`/`tool`.
Practice holds questions with their lifecycle, the competence line, and the
day receipt. Counts only where meaningful; empty shows nothing, never `0`.

## 3. Detail pages (drill)

Enter/click on a row opens a **full page** for that item — not a fold. Esc goes
back to the list, scrolled to the row it left. The page draws its own trail
line (the footer is another lane's file):

```
 notebook ‹ know-how ‹ release-notes
 ⚒ release-notes        workflow · v3 · proved 4× against 1 · ~$0.31/run
 collect merged PRs, draft the notes, verify links, deliver
 ceilings $0.50 · 10m

  1 gather merged PRs since last tag
  2 draft the notes                        after 1
  3 check every link resolves              verify
  4 deliver                                after 2·3

 v3  tightened the link check     2d
 v2  added the verify step        6d
 v1  forged from “ship 0.4”       9d

 run · revert · retire
```

Per kind, the body varies; the grammar (trail · title+receipt · body · verbs)
does not:
- **belief**: body text, where it came from (channel word: you said it /
  inferred / settled by trials), evidence lines, uses + last used. Verbs:
  forget · edit.
- **craft**: description, ceilings, steps in file order (needs/model/verify),
  version history from git, survival record. Verbs: run · revert · retire.
- **skill**: what it does, installed path, uses, status note if retired.
  Verbs: retire.
- **practice question**: lifecycle, attempts (each with cost + surprise delta),
  what changed. No verbs (steer conversationally).
- **charter** (work page): invariant, rails, tenure ladder position, firing
  history (last 5 with outcomes). Verbs: pause · cadence · probation · retire.
- **service** (work page): command, dir, health, restart policy, live log tail
  (10 lines / 32KiB off LogPath, the v1 read). Verbs: stop · restart ·
  auto-restart.
- Traits and dials are **read-only** — measured, not edited. Disputing them is
  a conversation, not a form.

## 4. Verbs — one registry spelling

Registry ids, fixed across all three lanes (Lane R creates the entries; UI
lanes reference these exact ids):

```
charter.pause charter.cadence charter.probation charter.retire
service.stop service.restart service.autorestart
belief.forget belief.edit
craft.run craft.revert craft.retire
skill.retire
```

Plumbing status: charter/service verbs have real CommandKinds
(`internal/store/thread.go:151-169`) reached via head belt tools `rule`/
`service`; belief forget/edit exist via belt tool `forget` and
`Commander.RetractNotebook`; craft run/revert/retire and skill.retire are NEW
(Lane R builds them). Per the one-mouth law, a verb chip that carries an
argument the resident must interpret (edit, cadence wording) seeds the
composer as a steer; a pure command (stop, forget, pause) fires directly with
the standard confirm where destructive.

## 5. Announcements ("hey, I learned these")

Three existing channels stay the mouth: per-job learning moments
(`internal/resident/learning.go:18`), the retrospective digest
(`learning.go:185`), the arrival brief (`internal/resident/brief.go`). Fixes
this wave (Lane R): add a `BriefCraft` item kind (crafts currently ride as
`BriefSkill`, `craftforge.go:223`); give the digest a craft branch (it has
none). Clickable moment lines and the notebook-tab dot are a later wave —
they live in chat files another lane owns today.

## 5b. Work-board row redesign (user feedback 2026-08-11, same wave, Lane W)

The single-line row with a far-right receipt fails at width — the eye can't
match a name on the left to `1m · $0.09` eighty columns away. New row anatomy:

- **Two-line rows.** Line 1: gutter · state glyph · name (primary tier).
  Line 2: indented dim receipt, directly under the name — proximity solved:
  `2 running · 12m · $0.09 · sonnet·haiku · 45K tok`. Blank line between jobs.
- Receipt vocabulary, in order, each part present only when known: live count
  or census · wall-clock · money · model words (deduped, humane words, never
  slugs — via `Commander.NodeModels`) · `~NK tok` total. `needs you` in amber
  replaces the census when a question is open.
- **Tree as progress, not as a toggle.** A job in `working` auto-shows its
  live subtree: children indented under the parent with their own state
  glyphs, dim, depth-capped at 2 with `…` beyond. Settled jobs (recent /
  history) stay collapsed to their two-line row. Click/enter anywhere on a
  job opens its room — no separate expand affordance on the board.
- Empty board says one calm sentence, not an empty frame.

## 6. File ownership (this wave)

- **Lane N (notebook content)**: `internal/tui2/homes/**`,
  `internal/tui2/chat/notebook.go`, new `internal/command/notebook_reads.go`.
  May make minimal edits to the notebook state-fill block in
  `internal/tui2/chat/rooms.go` (~line 1382-1425) ONLY. Must NOT touch
  app.go, panes.go, footer, composer, board.go, scope.go — report wiring
  diffs instead.
- **Lane W (work bands)**: `internal/tui2/chat/board.go` + its test, new
  `internal/command/board_reads.go`. Same do-not-touch list; report wiring
  diffs.
- **Lane R (backend)**: `internal/registry/**`, `internal/store` (new
  CommandKinds + craft/skill verb plumbing, new files where possible),
  `internal/resident/**` (verb handlers, BriefCraft, digest craft branch),
  `internal/craft` if needed. No `internal/tui2` edits at all.

The hug lane (in flight) owns app.go's footer/composer regions, the footer
and composer packages. Nothing in this wave touches those.
