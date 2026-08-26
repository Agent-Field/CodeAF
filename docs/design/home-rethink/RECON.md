<!-- Recon of internal/tui3 home, written 2026-08-25 by an Explore agent on branch home/rethink-v0. Line numbers are as of commit 6fd0a2da. -->

# HOME — architecture map (branch `home/rethink-v0`)

All paths absolute; line numbers from the current tree. Nothing was modified.

Surface facts up front: `internal/tui3` is the live v3 chat surface (`/home/santosh/af-home/CLAUDE.md:10`). Home is ~430 KB of Go across 14 non-test files plus ~370 KB of tests. `home.go` alone is 4,690 lines.

---

## 1. Home model, rows, cursor, columns

### 1.1 The state struct

**`homeView`** — `/home/santosh/af-home/internal/tui3/home.go:502` — the whole surface's state. Zero value is closed; **closing is assigning the zero value** (`closeHome`, `home.go:904`, sets `a.home = homeView{}`), so no field survives a close. Fields, grouped:

| Field | Line | What |
|---|---|---|
| `open bool` | 503 | |
| `world session.World` | 507 | the reading rows were built from, replaced whole per rescan |
| `lines []homeLine`, `cursor`, `top` | 510-512 | the left column in draw order; `cursor` never rests on heading/blank |
| `hover int` | 514 | line under pointer, or -1 |
| `pane []int` | 520 | per screen row → which right-pane row landed there |
| `box editor` | 528 | the one foot box: **new message AND live query at once**, no mode |
| `picked bool` | 534 | person walked off the action row onto a match |
| `expanded map[string]bool` | 538 | folds a person opened, by key (see §1.5) |
| `items map[string][]StandingItemView` | 544 | per-project standing band, keyed by BUCKET dir |
| `bare []homeBare` | 550 | projects known only through standing items |
| `itemsOpen map[string]bool` | 555 | item-band folds (separate map on purpose) |
| `archived []session.SessionRow`, `archiveOpen bool` | 561-562 | put-away rows, gathered across projects at build |
| `seen time.Time` | 573 | the look stamp, read once on open; **does not move while home is up** |
| `bucket string` | 576 | this window's project dir |
| `gone map[string]bool` | 590 | one `os.Stat` per project per reading, never per frame |
| `exchanges []*homeExchange` | 598 | `ask here` errands (owned by the app, copied here) |
| `exchangeIn map[*homeExchange]string` | 601 | which block each errand row draws in |
| `last map[string]session.Summary` | 605 | cache of `session.Peek` by transcript |
| `news`, `deliverables` maps | 609-610 | disk-backed band caches, expire on home's refresh clock |
| `msg`, `msgPath string` | 617-618 | the single refusal line + the path it names (a `pathLink` door) |
| `tier homeTier` | 627 | which of home's three shapes; settled BEFORE the column is built |
| `zoneTop`, `zoneRows []int` | 633-634 | the zones' own column state at the columns tier |
| `spin int` | 637 | the ONE animating line (`homespinner.go`) |
| `phone bool`, `sheet homeSheet`, `sheetHits`, `sections map[string]bool`, `inbox []homePhoneNote`, `inboxAt`, `standRoot`, `bar []hudSpan`, `barRow` | 642-650 | phone tier |
| `bandOpen map[string]bool`, `foldLines []bandFoldLine`, `repos map[string]homeRepoReading` | 655-657 | right-card fold state and git cache |
| `machine machineFacts`, `machineAt`, `machineDoors []machineDoor` | 664-666 | the machine reading (≤1 per `homeEvery`) |
| `week map[string]standing.Spend`, `weekAt` | 671-672 | one weekly ledger reading serves every card |

`homeView.say(msg, path)` — `home.go:682` — the one writer of the refusal line.

**`homeBare`** — `home.go:494` — a project known only through its watches.

Companion app fields: `a.home homeView`, `a.homeGen int` (clock generation), `a.homeRoot string` (test door for the places root, `app.go:1365`), `a.exchanges`, `a.handsRing []int`, `a.keepSpan/keepRow`.

### 1.2 Line kinds

**`homeRowKind uint8`** — `home.go:331`. Iota block `home.go:343-421`; three lanes append **outside** the block to avoid merge conflicts.

| Kind | Line | Cursor stop? | Meaning |
|---|---|---|---|
| `homeHeading` | 347 | no | project name |
| `homeSession` | 350 | yes | one conversation |
| `homeQuiet` | 356 | yes | `…2 more, quiet since Tue` — a door |
| `homeAction` | 377 | yes | `start a new conversation`, only while typing, always LAST |
| `homeBlank` | 379 | no | |
| `homeItem` | 389 | yes | one standing item |
| `homeItemFold` | 394 | yes | the item band's tail |
| `homeElsewhereRule` | 399 | no | `─ elsewhere ─────` |
| `homeProject` | 408 | yes | a whole folded project on one line |
| `homeMoreProjects` | 413 | yes | fold over the folded block |
| `homeArchiveFold` | 420 | yes | `▸ archive · 4 put away` |
| `homeEmptyRow = 230` | 341 | no | the dim empty-machine sentence |
| `homeAskHere = 200` | homeexchange.go:176 | yes | `ask here` row |
| `homeExchangeRow = 201` | homeexchange.go:157 | yes | a live errand |
| `homePhoneSection = 210` | homephone.go:92 | no | phone triage heading |
| `homePhoneNews = 211` | homephone.go:97 | yes | one thing since you left |
| `homePhoneMore = 212` | homephone.go:100 | yes | phone section fold |
| `homeAttentionZone = 220` | homeattention.go:86 | no | strip label |
| `homeAttentionMore = 221` | homeattention.go:90 | yes | strip fold |
| `homeAttentionTeach = 222` | homeattention.go:95 | no | dim teaching line under an empty strip |
| `homeAttentionGap = 223` | homeattention.go:104 | no | the blank between strips (a zone line, so `zoneSplit` doesn't cut early) |

`homeLine.stop()` — `home.go:1882` — is the single answer for "may the cursor rest here".

**`homeLine`** — `home.go:427` — one drawn line, resolved against the world once at build. Fields: `kind`, `project`, `dir`, `row session.SessionRow`, `quiet/since/folded`, `bare`, `proj session.Project`, `ex *homeExchange`, `note *homePhoneNote`, `zone *homeAttention`, `task *session.TaskIndexEntry` (the record row a `needs you` landed row was named after — carried by value so the door aims at the right task), `view StandingItemView`, `item standing.Item`.

### 1.3 How rows are built

`build()` — `home.go:969` → `buildFor()` — `home.go:2453` → either `buildPhone()` (homephone.go:172) or `buildAttention()` + `buildWorld()`.

**`buildWorld`** — `home.go:1175`. Two shapes:

*At rest* (nothing typed):
1. Walk `h.world.Projects`; archived rows split out into `h.archived` (`home.go:1186`).
2. `homeRank` filters/scores (`home.go:1682`).
3. Bare projects (items, no conversations) appended (`home.go:1281`).
4. Stable sort by `hit.at` descending (`home.go:1291`).
5. `homeTiers(found, bucket)` — `home.go:1382` — **this window's project is always first and always open**, then the next `homeOpenProjects` = 3 by recency; the rest fold.
6. `placeExchanges(open)`, then per open project: blank, `homeHeading`, `projectBlock`.
7. `buildElsewhere(folded)` — `home.go:1482` — dim rule + one line per project, sorted **triage then recency** (`projectHot`), capped at `homeFoldedProjects` = 8 then a `homeMoreProjects` fold.
8. `buildArchive()` — `home.go:1338`.
9. If nothing at all: two `homeEmptyRow` lines from `homeEmptyLines()` (`home.go:1333`).

*While typing*: no tiers at all — every project holding a match draws open; sections and rows are sorted best-first then **reversed**, because the list is a drop-up read upward (`home.go:1214-1238`, law stated at `home.go:1141-1160`). Then a blank, `homeAskHere`, `homeAction` as the last two lines.

**`projectBlock`** — `home.go:1411` — order inside a project: errand rows (`exchangeLines`) → **hot** standing items → conversations (`split`) → **cold** standing items → `homeItemFold` → `homeQuiet`. A search draws **no** standing band at all (`home.go:1424`).

**`split`** — `home.go:1578` — everything with `Tasks.Running > 0 || Tasks.Incomplete > 0` is drawn whatever the count; then up to `homeShown` = 4 (`home.go:74`); the rest collapse with the newest of their stamps. A search or a hand-expanded project is never collapsed.

**The two strips** — `homeattention.go`:
- `homeZone` table — `homeattention.go:158`, instances at `:183`. Two rows: `needs you` (`attentionNeedsWord`, teach `"questions and landed work"`, mark `▲`, ink `pal.askBold`, no cap) and `moving` (`attentionMovingWord`, teach `"turns, tasks and watches"`, mark `●`, ink `pal.muted`, `shown: attentionMovingShown` = 5). Everything downstream walks the table; a third strip is a row here and no new branch.
- `attentionNeeds()` — `homeattention.go:406` — gathers: waiting errands; standing items with `NeedsPerson != ""`; sessions where `row.NeedsPerson()`; and **task entries with `Status == session.TaskUnverified`** (landed, needs your look) which carry `lead: "landed"` and `line.task`.
- `attentionMoving()` — `homeattention.go:470` — working errands; `view.Running` items; sessions with `Tasks.Running > 0` or live `PresenceWorking`. Waiting outranks working (skipped if `NeedsPerson`).
- Sort: needs-you by `attentionOlder(at)`; moving by `busy` desc then age.
- Constructors `attentionChat` (:522), `attentionTask` (:537), `attentionItem` (:545), `attentionErrand` (:555) build **the list's own line kinds** with a `zone` pointer attached — which is why enter, the card and the digit keys all work unchanged from a strip.
- A search draws no zones at all; the zones stand down on an empty machine.

**Rescan**: `refreshHome()` — `home.go:942` — re-reads world + stand bands + `machineAt = zero` + `readGone()` + `build()`. Clock: `homeEvery` = 3s (`home.go:113`), `homeTickMsg{gen}` (`:124`), `homeTick` (:127), `homeBeat` (:135), `homeAnimating` (:165). Paint clock only joins when a row is truly running.

### 1.4 Cursor

- `homeRest = -1` — `home.go:1778` — a real place: cursor on no row, card is the machine's.
- `resting()` :1781, `restable()` :1790 (not while searching, not on phone).
- `move(delta)` — `home.go:1901` — walks by stops, clamps at both ends; walking up off the top reaches rest; **first ↓ off rest lands at `h.wake()`** (homebridge.go:448 — the middle column at the three-column tier). Walking off the action row sets `picked = true`.
- `clamp(at)` :1848 — forward then back to the nearest stop.
- `focused()` :1794 (session row only), `focusedLine()` :1805 (any stop), **`previewLine()` :1828 — the pointer previews, the cursor selects**; the card follows `previewLine`.
- Cursor restoration across rebuilds (`build`, `home.go:974-996`): rest, then exchange, then transcript, then item id, then project dir — and `defer h.pointZone(h.cursorZone())` puts it back in the strip it was standing in.
- `openAt(file)` — `homebridge.go:264` — home opens on **this window's own conversation**, falling back to `placesTop()` and then rest.

### 1.5 Fold keys

One map (`expanded`) at three scales — `home.go:2333-2360`:
- a project's quiet tail: keyed by bucket dir alone
- `homeProjectKey(dir)` = `"\x00project\x00" + dir` (`home.go:2346`)
- `homeElsewhereKey` = `"\x00elsewhere"` (`home.go:2344`)
- zones: `attentionFoldKey(word)` = `"\x00zone\x00" + word` (`homeattention.go:247`)
- item bands: separate `itemsOpen` map; phone sections: separate `sections` map.

### 1.6 Layout and breakpoints

Two independent ladders, settled in the same breath at `home.go:3287`:

**Program-wide** — `/home/santosh/af-home/internal/tui3/view.go:1132` `layoutTier(width)`: `tierWide ≥120`, `tierStandard ≥80`, `tierNarrow ≥60`, else `tierPhone`. Home only uses `tierPhone` from this.

**Home's own** — `/home/santosh/af-home/internal/tui3/homebridge.go`:

```
homeTierList    < 80    list takes the frame, no card          (homeMinDetail = 80, home.go:173)
homeTierCard    80..109 list + card; zones are strips over the list
homeTierColumns ≥ 110   zones own a column; places middle; card right
```
- `homeTier` type :68, constants :70-84, `homeTierAt` :121 — **the one place a width is compared**.
- Column floors: `homeAttentionCol = 28` (:92, never grows), `homePlacesCol = 38` (:98), `homeCardCol = 36` (:102).
- `homeMinColumns = 28 + 4 + 38 + 4 + 36 = 110` (:110) — derived from the parts, not chosen.
- `homeGutter = 4` (`home.go:185`) — the only separator; this surface draws no borders.
- `homeListCap = 46` (:115), `homeListFloor = 30` (:118), `homeDetailFloor = 34` (`home.go:191`).
- `homeColumns(width)` — `home.go:3486` — returns `(left, right)`. Below 80: `(width, 0)`. At ≥110: `homeThreeColumns` → `(zone+gutter+places, card)`. Between: `left = clamp(width/2, 30, 46)`, `right = width - left - 4`, and if `right < 34` the card is dropped entirely.
- `homeThreeColumns(width)` — homebridge.go:172 — floors first, surplus split card/places with the odd cell to places, **so places is the widest column at every width**.
- `homeLeftColumns(left)` — :183 — splits the left half back for the body and the pointer.
- Cut points into `h.lines`: `zoneSplit()` :201 (found, not counted — `attentionOwns`), `placesFrom()` :217, `placesTop()` :233.
- `threeColumns()` :159 = wide enough AND the zones found something.

**Frame assembly** — `homeFrame(width, height)` — `home.go:3276`:
1. settle phone/tier, rebuild if changed
2. pulse line, blank, dim rule, blank
3. **foot measured before the body**: `draftBlock` built early so its height comes out of the list, not the frame
4. `room = height - head - foot - spacingRuleClearance`
5. `homeColumns`, `homeWindow(room)` (homebridge.go:289), `homeBody`
6. blank, rule, box rows (or `homeFootWord` at rest, and the caret is hidden), answer strip, then `msg` or `homeHint()`
7. tail-clamp keeps row 0 and the last `height-1` rows; **the caret rides the clamp**
8. writes `a.home.pane` and `a.home.zoneRows` — the pointer hit maps

`homeBody` — `home.go:3509` — draws left (`homeLeft`) and right (`homeDetail`), pads the gutter on **every** row so the card's edge is a straight vertical line, and emits `homeDrawn{text, hit, pane, zone}` (`home.go:3464`). `homeLift` :3603 makes the drop-up. The card is deliberately **not** lifted.

`homeRows` :3638 fades the last three rows with `pal.fadeRow` unless the list's end is on screen; cursor, hover and the marked heading are spared (`homesection.go`).

Section marking: `homesection.go` — `markedSection()` :80, `marksSection()` :97, `sectionInk()` :113 — the heading over the section your cursor is in steps up one ink tier and wears no ground.

---

## 2. The band registry (right card)

File: `/home/santosh/af-home/internal/tui3/homebands.go`.

### The interface

```go
type homeBand struct {        // homebands.go:214
    name  string              // fold key + test handle
    order int
    kinds []bandKind          // empty ⇒ bandKindSession only
    draw  func(a *app, ctx bandContext) []string
}
```

- `bandKind` — :144: `bandKindSession` (1), `bandKindItem`, `bandKindProject`, `bandKindMachine`.
- `bandSubject` — :166 — `{kind, row session.SessionRow, item StandingItemView, project string, world session.World, dir string}`. `id()` :184 → transcript / item id / dir / `machineSubjectID = "\x00machine"` (:202).
- `bandContext` — :206 — `{subject, width, now, pal}`, handed in whole so a band's signature never grows.
- `registerHomeBand` :253 (called from each file's `init`), `homeBandsFor(kind)` :259 (lazy stable sort by order), `draws` :275, `drawHomeBands` :289 (drops empty bands).
- Folding: `bandFoldKey` :311, `bandFolded` :316 (folded is the default), `toggleBandFold` :324, `setAllBandFolds` :338, `anyBandFoldOpen` :351, `allBandFoldsOpen` :362, `bandFold` :377, `bandFoldGroups` :405, `bandFoldPacked` :437, marks `▸`/`▾` :305/:308, `noteBandFoldLine` :494 / `bandFoldAt` :502 (click resolution by row text).

### Order keys — `homebands.go:227-244`

| Order | Name | Meaning |
|---|---|---|
| 5 | `bandOrderGone` | folder is not there any more |
| 10 | `bandOrderState` | what it is doing right now |
| 15 | `bandOrderAnswer` | the question it is stopped on |
| 25 | `bandOrderWatchlist` | machine: everything keeping an eye |
| 28 | `bandOrderHands` | machine: what its hands have been doing |
| 30 | `bandOrderNews` | what happened since you last looked |
| 35 | `bandOrderSinceLeft` | machine: the same across every project |
| 40 | `bandOrderWork` | tasks and what they came to |
| 50 | `bandOrderDeliverable` | files produced |
| 60 | `bandOrderNextUp` | standing items that will wake |
| 70 | `bandOrderLeftOff` | where the conversation left off |
| 80 | `bandOrderRepo` | where the repository stands |
| 90 | `bandOrderSpend` | what it cost |
| 93 | `bandOrderThinking` | the rung it thinks at |
| 95 | `bandOrderToday` | machine: what the day came to |
| 100 | `bandOrderKeys` | the legend, always last |

### The registered bands (17)

| Band `name` | Order | Kinds | File:line | Data source |
|---|---|---|---|---|
| `gone` | 5 | session, project | homeband_gone.go:4 | `a.homeGone(dir)` (cached stat map) |
| `state` | 10 | session | homebands.go:515 (`drawStateBand` :521) | `SessionRow.Doing/Reason/Presence` |
| `answer` | 15 | session | homeband_answer.go:57 | `session.PresenceQuestion` + `Options.Answer` seam |
| `watchlist` | 25 | **machine** | homeband_watchlist.go:20 | `machineFacts.watching` |
| `agents` | 28 | **machine** | homeband_spark.go:90 | `a.handsRing` spark |
| `news` | 30 | session, project | homeband_news.go:24 | `standing.PeekProjectInbox` / inbox files, cached in `home.news` |
| `sinceleft` | 35 | **machine** | homeband_sinceleft.go:33 | `machineFacts.news` = `phoneNotes()` |
| `work` | 40 | session | homeband_work.go:54 | `row.Tasks.Rows` |
| `projectsessions` | 40 | **project** | homeband_projectsessions.go:33 | `session.Project.Sessions` |
| `deliverables` | 50 | session | homeband_deliverables.go:17 | `session.ReadArtifacts(a.artifactsIndex())`, cached |
| `nextup` | 60 | session, project | homeband_nextup.go:13 | `a.home.items[...]`, `standing.Item.When` |
| `projectstanding` | 60 | **project** | homeband_projectstanding.go:35 | `a.home.items[project.Dir]` + `standWeek` |
| `leftoff` | 70 | session | homeband_leftoff.go:10 | `session.Peek(transcript)`, cached in `home.last` |
| `repo` | 80 | session | homeband_repo.go:23 | `git status --porcelain=v2 --branch`, TTL-cached in `home.repos` |
| `spend` | 90 | session | homeband_spend.go:6 | `row.Spend/Tokens` + `Tasks.Spend/Tokens` |
| `projectfacts` | 90 | **project** | homeband_projectfacts.go:30 | counted from the project |
| `thinking` | 93 | machine, item | homeband_thinking.go:50 | `config.DefaultEffortAt` / `item.Does.Effort` |
| `today` | 95 | **machine** | homeband_today.go:31 | `machineDay` + `machineAllowance` |
| `keys` | 100 | session, item, machine | homeband_keys.go:4 | the legend |

**No band draws for `bandKindProject` beyond the four project-specific ones**; the project card's title + place lines are owned by `homeDetail` (`home.go:4245-4262`).

### Who assembles the card

`homeDetail(width, room, pal)` — `home.go:4221` — dispatch: rest → `machineCard` (homemachine.go:358); `homeExchangeRow` → `exchangePane`; `homeItem` → `StandingItemCard` (homestanding.go:548); `homeProject` → title + place + project bands; `homeSession` → title + place + session bands; anything else → nil.
`homeBands(bands, room)` — `home.go:4313` — **drops whole bands from the bottom, never truncates**; the title (`bands[0]`) is never dropped; one blank between survivors.
`homeSubject()` — `home.go:4654` — the registry's view of what the card is about, read off `previewLine()` so keys act on the card you are looking at.

---

## 3. The pulse (top line)

File: `/home/santosh/af-home/internal/tui3/pulse.go`. Drawn at `home.go:3318` as frame row 0.

```
aforge                      on watch · 4 orders · 3 working · $1.10 today · fri 9:41am
```

- `pulseLine(width, pal)` :77 — name left (`pal.bold(pal.muted("aforge"))` — deliberately **not** the accent; the accent budget is one live thing per screen), segments right-aligned, **name never gives way**.
- `pulseSegments(now, pal)` :109 — every segment comes off `a.machineFactsAt(now)`; **one reader, two surfaces** (the pulse and the machine card cannot disagree).
  1. `on watch` + `N order(s)` — from `len(facts.watching)`; both clauses appear and go together; ink lifts `dim → muted` while `facts.firing`.
  2. `N working` — `facts.hands`; the **figure** takes `pal.data`, the word stays `pal.dim` (payload rule); drawn at 1, absent at 0.
  3. `$X today` — `facts.spent`; `pal.dim`, rising to `pal.warn` when `facts.nearCeiling()`. **Never a quota fraction here** — the allowance belongs to the `today` band only.
  4. `pulseClock(now)` :172 — `strings.ToLower(now.Format("Mon 3:04pm"))`, the one segment that always draws.
- Words: `pulseName`, `pulseWatchWord`, `pulseOrderWord`, `pulseWorkingWord`, `pulseTodayWord`, `pulseGap` — `pulse.go:47-69`, quoted verbatim in the manual.

`machineFactsAt` — `homemachine.go:107` — cached on the view for `homeEvery`; also the beat the hands spark samples on (`sampleHands`, :156, ring of `handsRingSize` = 60 ≈ 3 minutes).

---

## 4. The typed surface, keys, and pages

### 4.1 What typing does

One box, two readings, no mode (`homeView.box`, `home.go:528`). Every character both filters the machine and composes a first message. `query()` :1603 = lowercased trimmed box; `searching()` :1608. `homeDraftRows = 3` (`home.go:231`) caps the foot box; `draftBlock` wraps rather than truncates.

**Ranking** — `homeRank(row, project, query, now)` — `home.go:1682`:
- underlying matcher `session.MatchQuality` (`/home/santosh/af-home/internal/session/task_index.go:880`), rungs `MatchWord 1000 / MatchPrefix 800 / MatchWordStart 600 / MatchInside 400 / MatchScattered 200` (:846-864).
- field weights (`home.go:1650-1653`): name ×10, project ×6, task label ×5, task outcome ×3.
- state boosts (`home.go:1671-1673`): needs-you 400, running 200, incomplete 60; recency `homeRecencyBoost` 120 decayed straight-line over `homeRecencySpan` 30 days (`homeRecency` :1751).
- **every query word must land somewhere** (a second word narrows); scores summed.
- Explicit non-goal: no embeddings, no index — stated at `home.go:1625-1643`.

Draw order under a query is **inverted** (best match nearest the box) — `home.go:1214-1238`.

Empty-query results: `homeRank` returns `(0, true)` so the world keeps `session.sortSessions` triage order.

### 4.2 Key handling — `homeKey` at `/home/santosh/af-home/internal/tui3/home.go:1958`

Order of routing: phone sheet first (`homeSheetKeyFirst`, homesheet.go:715) → `settleExchangeFocus` → focused exchange takes it (`exchangeKey`, homeexchange.go:1023) → `defer sweepExchanges` → digit answer keys when the box is empty (`answerKey`) → the switch.

| Key | Line | Effect |
|---|---|---|
| digits (box empty) | 2026 | answer the consent/standing question the card advertises |
| `tab` | 2032 | at columns tier: cycle zones (`homeTab`, homebridge.go:493); otherwise focus the pane's exchange |
| `esc` | 2050 | one layer: clear box, else `closeHome()` |
| `up` / `ctrl+p` | 2063 | `move(-1)` + `refreshHomeRepo` |
| `down` / `ctrl+n` | 2067 | `move(1)` |
| `pgup` / `pgdown` | 2071/2075 | `move(∓homeShown)` (4) |
| `enter` | 2080 | `homeEnter()` |
| `ctrl+enter`, `alt+enter` | 2083 | `askHere(box)` — two spellings because terminals disagree |
| `ctrl+t` | 2106 | new conversation in the previewed row's folder (or here); refuses on a gone folder; refuses if the box is non-empty and the folder is elsewhere |
| `ctrl+o` | 2129 | open the workspace in the OS file manager (`processOpener`) |
| `ctrl+y` | 2140 | copy workspace path via OSC 52 |
| `ctrl+e` | 2152 | session → `session.SetArchived` toggle; item → pause (`standing.StatusPaused`) |
| `ctrl+x` | 2179 | item → `standing.StatusRetired` |
| `ctrl+v` | 2187 | `cycleHomeEffort()` — machine rung or item rung only; a session's rung is deliberately untouchable from home |
| `backspace` / `ctrl+u` / `ctrl+w` | 2202-2213 | box edits + rebuild |
| `right` | 2214 | fold-open ladder: quiet → items → project → elsewhere → cross columns → **open every band on the card**; else `box.right()` |
| `left` | 2266 | fold every band closed first, then cross columns back, then quiet/items/project/elsewhere; else `box.left()` |
| `ctrl+b` / `ctrl+f` | 2313/2316 | caret left/right |
| default | 2320 | `msg.Key().Text` inserted, `build()` |

**A bare letter always types** — stated at `home.go:2010-2025`. The one printable exception is the digit block, which is earned by being drawn on the row.

`homeEnter` — `home.go:2471`: phone sheet first; at rest with an empty box, enter **closes home into the conversation you were holding**; `homeAction` → `homeStart`; `homeAskHere` → `askHere`; `homeExchangeRow` → focus; `homeQuiet`/`homeItemFold`/`homeProject`/`homeMoreProjects`/`homeArchiveFold`/`homeAttentionMore` → fold toggles; `homeItem` → `homeItemEnter` (opens the conversation that asked for it); otherwise `homeOpenLine` (:2549) with a fixed check order: **held-by-this-process → `canOpen` → locked elsewhere → folder gone → room for another → `openBeside`**, then `homeLandOnTask` (:2645) raises the task record when the row carried one.

`homeStart` — `home.go:2749` — `typedPlace` (:2788) first: an absolute/`~` path or a project name matching exactly one heading starts a conversation THERE (`startBeside`); otherwise `/new` + submit in this project.

**Alt bindings across tui3** (grep `"alt+`, non-test): only three sites —
- `home.go:2083` `"ctrl+enter", "alt+enter"` → ask here
- `input.go:605` `"alt+enter", "ctrl+j"` → newline; `input.go:820/835` `alt+left/alt+b/ctrl+left`, `alt+right/alt+f/ctrl+right` → word jump; `input.go:798` `alt+backspace` → delete word (mirrored in `palette.go:1069`, `settings.go:1686`)
- `commands.go:758` help text.

**There is no `alt+<digit>` binding anywhere in `internal/` (grep returns nothing).** `alt+1` is explicitly *resident* vocabulary and banned from the chat manual corpus (`CLAUDE.md:88`, pinned by `TestTheChatManualDoesNotSpeakOfTheResident`, `manual_test.go:44`). The alt+number namespace is entirely free.

Mouse: `homePress` :3013, `homePane` :3161 (which column a click landed in), `homeHover` :3218, `homeZoneHit` (homebridge.go:367), `homeDoorPress` :2985, `machinePress` (homemachine.go:396), `bandFoldAt` (homebands.go:502).

Entry gesture: `homeGesture` — `home.go:2945` — **space space** in an empty box; `homeDoorOpen` :2972 (`canOpen && !hosted && !home.open`); `homeDoorShowing` :2980 advertises it in the legend slot.

### 4.3 Places / pages today

There is **no page router**. Home is one of five mutually-exclusive fullscreen surfaces, all app struct fields, torn down by `standDownFullscreen()` — `/home/santosh/af-home/internal/tui3/settings.go:934`:

| Page | Field | File | Opened by |
|---|---|---|---|
| settings panel | `a.sheet sheet` | settings.go | `/settings` (`/set`, `/config`), `ctrl+,` |
| task page ("history") | `a.taskSheet taskSheet` | taskview.go | `/history`, `ctrl+.` |
| home | `a.home homeView` | home.go | `/home`, `space space`, the legend door, launch landing |
| rewind timeline | `a.rewSheet rewindSheet` | rewindsheet.go | `/rewind` (`/undo`, `/back`) |
| standing page | `a.standPage standPage` | standingpage.go | `/standing`, `/orders`, pressing `◦ keeping an eye on N` on the status line (`standdoor.go:58`) |

Plus non-fullscreen overlays drawn over the conversation: `permPanel` (permissions.go), `memoryPanel` (memorypanel.go), `connectPanel` (connectpanel.go), `harnessPanel` (harnesspanel.go), `deliverables` (deliverables.go), `pick picker` (model), `crewPicker`, `statusdeck` (phone status sheet), `consent`.

`homesheet.go` is **not** a page router — it is home's **phone-tier card sheet**: at `tierPhone` enter on a row raises that row's bands as a full-screen sheet with `‹ back` and an action bar. Order: `homeSheetOrder` (:90) `answer, state, news, work, nextup, leftoff, deliverables`; behind `▸ more` (`homeSheetMore`, :97) `repo, keys, spend, projectfacts`; unnamed bands keep registry position after the named ones (`homeSheetBands`, :418).

None of the five pages is reachable *from* home — home closes to reach any of them, except `homeLandOnTask` which opens the task record after the conversation.

Command table: `/home/santosh/af-home/internal/tui3/commands.go:53-322`. `{name: "home", desc: "every project and conversation on this machine"}` at `:84`.

---

## 5. Data sources available to home

"Cached / non-blocking" below means: does it obey the law that a draw never touches the disk and a seam never blocks (`tui3.go:633`: *"It must NOT block: home calls it on every three-second beat and on the keystroke that opens the screen"*).

### Already read by home

| Data | Go API | Cached? | Where home reads it |
|---|---|---|---|
| **Sessions & projects** | `session.ReadWorld(root)` → `session.World{Projects []Project, Read time.Time}` (`internal/session/world.go:305`, `:71`); `Project` :95, `SessionRow` :154, `PlacesRoot()` :68, `World.Adopt` :330, `World.Sessions()` :84 | walk per 3s beat only; `readWorld` also `Adopt`s this window's own fresh folder | `a.readWorld()` home.go:892; `refreshHome` :942 |
| **Task rollup** | `session.TaskRollup` (world.go:270) — `Rows []TaskIndexEntry, Running, Incomplete, Done, Failed, Spend, Tokens, Newest`; `SessionRow.Runs(entry)` :256; `session.ReadTaskIndex` (task_index.go:329) | comes free with the world walk | `work` band, `homeRank`, `split`, `attentionNeeds/Moving`, `machineDay` |
| **Standing items** | `StandingSeam.Items(workspace) []standing.Item` (tui3.go:637), `.Running(id) (standing.RunningMark, bool)` :662, `.Save` :646, `.SetEffort` :736, `.Runs(since) map[string]standing.Spend` :720, `.Watch` :668, `.Ticking` :706. Behind the seam: `standing.Store` (`internal/standing/store.go:34-207`), `standing.Item` (standing.go:304) | **one walk per reading of the world**, held on `home.items`/`home.bare` | `readStandBands` homestanding.go:968, `readBareBands` :1016, `standItems` :188 |
| **Standing ledger / weekly spend** | `Store.RunsSince` (`internal/standing/ledger.go:101`), `Store.Today` :57, surfaced as `StandingSeam.Runs` | `a.standWeek(now)` homestanding.go:923 — **one reading serves every card**, cached on `home.week/weekAt` | `projectstanding` band, item cards |
| **Standing inbox / news** | `standing.PeekProjectInbox(root, workspace)` (`internal/standing/inbox.go:172`), `standing.Drain/Deliver` :53/:28 | cached in `home.news` per transcript, expires on the refresh clock; **peeked, never drained** | `news` band homeband_news.go:72-84; `phoneNotes` homephone.go:398 |
| **Spend / money** | `SessionRow.Spend/Tokens` (talking) + `TaskRollup.Spend/Tokens` (work) — **never added by the layer, added at the draw**; day total `machineDay` homemachine.go:278; ceiling `config.DailyBudgetUSDAt(profileDir)` (`internal/config/budget.go:20`) via `machineAllowance` homemachine.go:335 | `machineFactsAt` caches for `homeEvery` | `spend` band, `today` band, pulse |
| **Deliverables ledger** | `session.ReadArtifacts(path)` / `session.Artifact` / `ArtifactsIndexName = "artifacts.jsonl"` (`internal/session/artifacts.go:92`, :36, :30); path from `Options.ArtifactsIndex` (tui3.go:404), default `~/.aforge/v3/artifacts.jsonl` | cached in `home.deliverables` per transcript on home's clock | `deliverables` band |
| **Conversation tail** | `session.Peek(path) (Summary, bool)` (`internal/session/peek.go:73`) | cached in `home.last` — the arrow key moves the cursor on every press | `leftoff` band |
| **Look stamp** | `session.LastLook(root)` / `session.NoteLook(root, at)` (`internal/session/look.go:33`, :52) | read once on open, written once on close | `home.seen`, the ✓ and `landed` marks |
| **Archive flag** | `session.SetArchived(dir, bool)` (`internal/session/place.go:235`) + `SessionRow.Archived` | write on `ctrl+e`, then immediate `refreshHome` | `buildArchive` |
| **Repository state** | `exec.CommandContext(ctx, "git", "-C", ws, "status", "--porcelain=v2", "--branch")` (homeband_repo.go:19) | **the one shell-out**; TTL cache `home.repos` (`homeRepoTTL`), refreshed only on cursor moves (`refreshHomeRepo`) | `repo` band |
| **Effort ladder** | `effort.Rung` / `effort.Rungs` / `effort.Parse` (`internal/effort/effort.go:27,58,95`), `effort.Resolve(Scope)` (resolve.go:93); `config.DefaultEffortAt(profileDir)` (`internal/config/effort.go:52`), `config.WriteDefaultEffort` | pure file read, per draw of the thinking band | `thinking` band homeband_thinking.go:72; `cycleHomeEffort` homeeffort.go:45; clause words `effortscope.go:70-98` |
| **Presence / questions** | `SessionPresence`, `PresenceQuestion`, `QuestionKind`, `AnswerOptions`, `PresenceWorking/Waiting` (session); answered through `Options.Answer(dir, kind, id, key)` (tui3.go:319) | presence file, believed for 15s; read with the world | `state`, `answer` bands; both strips |
| **Errands (`ask here`)** | `Options.Errand(dir, workspace) (Agent, error)` (tui3.go:301); `homeExchange` (homeexchange.go:240); stored under `a.errandsDir()` = `standingHome()/exchanges` (:795) | live objects on the app, outlive the screen | `askHere` :805, `exchangePane` :1352 |

### Available but **not** read by home today

| Data | Go API | Blocking? | Notes |
|---|---|---|---|
| **Memory facts** | `store.Fact` / `FactKind` / `RecordFact` / `ReplaceFact` (`internal/store/facts.go:240,48,452,489`), `store.Memory` + `MemoryScopeUser/Project/Env`, `store.AgeLabel` (:29), `Store.Recall(terms, cues, limit) []RecallHit` (`internal/store/recall.go:155`, `RecallHit` :73, `FormatRecall` :85), scope aliasing `AliasScope/ResolveScope` :383/:412 | **SQLite — blocking**. `internal/store` is the *resident's* store, not v3's | tui3 touches it only through `MemoryStore` (`memorypanel.go:22`: `ListMemories(scope, limit)`, `ForgetMemory`) wired via `Options.Memory` (tui3.go:252). v3's own memory is `session.MemoryLine` + agent methods (`memory.go:27-44`, `/remember`, `/forget`, `/memories`). **Home reads neither.** No `UseCount`/`MissCount`/consolidation surface exists on the v3 side — consolidation lives at `internal/session/memory_consolidate.go` |
| **Usage / rails** | `store.TotalUsage`, `NodeUsage`, `DailyRail`, `Store.Usage()`, `SpendToday()`, `DailyRailToday(base)`, `RaiseDailyRail` (`internal/store/usage.go:75,36,112,255,270,286,342`) | SQLite — blocking | resident-side. Home's money comes from `TaskIndexEntry.Cost`, `SessionRow.Spend` and the standing ledger instead |
| **Model slots** | `config.ModelSlot`, `config.ModelSlots()`, `config.ModelSlotFor(slot)` (`internal/config/modelslots.go:29,89,119`) | file read | used by settings.go; **home does not read** |
| **Crew / role bindings** | `internal/roles` (`roles.Tiers`), `crew.go:32 runCrew` / `:58 applyCrew` / `:120 talkingTo` / `:159 crewSegment`; `store.role_bindings.go` | file | status line + `/crew`; **home does not read** |
| **Learned fixes** | `internal/session/fixstore.go` — `fixStore`/`fixDocument`/`fixEntry` (`:356,:338,:298`), `consult` :442, `confirm/blame/tally` :490-502, decay :416; `fixrecall.go`. `fixes.json` | file, agent-internal | entirely unexported; **no surface reads it**. A home band would need an exported reader |
| **Subharness / harness registries** | `Options.Harnesses *subharness.Store` (tui3.go:499); `internal/subharness`; `harness.go`, `harnesspanel.go`, `harnesspick.go`, `harnesscard.go` | store handle | `/harness`, `/subharness`; **home does not read** |
| **Connected accounts** | `Options.Connections Connections` (tui3.go:490); `internal/connect`; `connect.go`, `connectcaps.go`, `connectpanel.go` | seam | `/connect`; **home does not read** |
| **Permissions posture** | `internal/approval` (`approval.Action`, `ActionDeny/ActionPrompt`), `internal/guard`; `permissions.go:265 openPermissions`, `:288 permissionRules`; seams `SaveApproval`/`SaveBashApproval`/`ApplyApprovals` (tui3.go:441-474) | config read | `/permissions`; **home does not read** |
| **Remote hosts** | `internal/remote`; `Options.Host` (tui3.go:357), `LinkSeam` (:607), `host.go`, `hostlink.go`, `remotefiles.go`, `remoteopen.go` | network | **home refuses entirely over `--host`** (`homeRemoteWord`, `home.go:249`; `openHome` :699) |
| **Jobs** | `internal/session/jobs.go`, `jobrow.go` | in-process | surfaced on the task rail; **home does not read** |
| **Typed history** | `history.Store` (`internal/history/history.go:69`), `New(path)` :90, `Recent(limit)` :122, `RecentFor(cwd, limit)` :128, `Entry` :57, `history.jsonl`; slice `History` interface (`tui3/recall.go:25`), `recallDepth = 200` (:20) | **background flush, never blocks** (recall.go:135) | drives the ↑ recall in the chat box; **home's box does not use it** |
| **Web search** | `internal/search` is **DuckDuckGo/Exa/Jina web search** — not conversation search | network | Conversation search is home's own `homeRank` + `session.MatchQuality`. There is no cross-conversation full-text index |

---

## 6. Palette and glyph tokens

### `/home/santosh/af-home/internal/tui2/tokens/palette.go`

**The `Token` inventory** — `:156-243`. Bases at `:100-142`.

| Token | Line | Hex (dark) | Meaning |
|---|---|---|---|
| `TextPrimary` | 159 | `#E6E6F0` | tier 1: speech, titles |
| `TextSecondary` | 160 | `#A0A6BB` | tier 2: status lines, receipts |
| `TextTertiary` | 161 | `#7C8296` | tier 3: telemetry, chips at rest |
| `Amber` | 163 | `#EECE96` | **needs a human** |
| `Cyan` | 164 | `#A4D7EA` | **alive** |
| `Green` | 165 | `#A2E2BC` | **money + success** |
| `Coral` | 166 | `#EFA99F` | broken |
| `Identity0..7` | 168-175 | `#DDE6B3` lime, `#BDE6B3` leaf, `#B3E6DD` teal, `#BDC7E5` blue, `#C9BDE5` indigo, `#DBB3E6` violet, `#E6B3D6` orchid, `#E6B3BF` rose | the 8-hue wheel, ≥20° off every semantic hue |
| `Ground` | 177 | `#12121A` | default dark ground |
| `Band` | 178 | `#262633` | selection band |
| `Sheet` | 196 | `#1B1B25` (derived, `SheetTowardBand`) | dialog's own ground |
| `BandIdentity0..7` | 198-205 | band tinted by identity | |
| `HugGroundBar` | 220 | `#14141D` (derived) | composer hug: the "where you are" row |
| `HugGroundInput` | 221 | `#1A1A24` (derived) | the row you type into |
| `CardGroundWorking` | 235 | `#171720` (derived) | task card while running |
| `CardGroundDelivered` | 236 | `#1F1F2A` (derived) | task card once delivered |

Contract machinery: `Class` (`ClassBody/Support/Chrome/Surface`) :245-268, `MinContrast` :278, `BandSeparationMin/Max` = 1.15/1.70 :299-300, `SheetSeparationMin` = 1.08 :315, `HugSeparationMin` = 1.05 :332, `Focus` :339. Also `ContextToken(used, window)` (breakpoints.go) returns `Amber` past the warn point, `TextTertiary` before it.

**The accent rules, as written**: amber = "needs a human: question badges, waiting states, ctx meter near limit" (:163); cyan = "alive: working glyphs, stream caret, thinking pulse" (:164); green = "money + success: cost figures, settled ✓" (:165); coral = "broken". `IdentityCount = 8` (:148).

### `/home/santosh/af-home/internal/tui2/tokens/glyph.go`

`GlyphQueued ○`, `GlyphWorking ◐`, `GlyphSettled ✓`, `GlyphFailed ✕` (:17-20); `GlyphPaused =` (:23); `GlyphNeedsHuman ?` **always amber** (:26); `GlyphWaitsOn ⚑` (:27); `GlyphCollapsed ▸` / `GlyphExpanded ▾` (:30-31); `GlyphScopeUp ‹` (:32); `GlyphTruncated ⋯` / `GlyphEllipsis …` (:33, :50); `GlyphCut ╌` (:59); `GlyphPromptChat ›` / `GlyphPromptSteer ↦` (:63-64); `GlyphThought ✳`, `GlyphShell $`, `GlyphSearch ⌕`, `GlyphWrite ✎` (:76-79); `GlyphBoosted ⇡`, `GlyphSeparator ·`, `GlyphMissing —`, `GlyphEstimate ~` (:82-85); `GlyphAccentRail ▎`, `GlyphDragHandle ⋮` (:89-90); `GlyphHugEdge ▍` (:104); chip caps `▐`/`▌` (:118-119); step glyphs `●◐○⚑` (:123-126); `GlyphQueuePill ▶` (:129); diff `+`/`−` (:134-135); tree `├└│─` (:139-142); `GlyphHome ⌂`, `GlyphFolder /`, `GlyphGitBranch ⋔` (:155-157); `GlyphModel ◇`, `GlyphSpend $` (:172-173). Tables: `GaugeCells [5]` (:179), `SpinnerFrames [10]` braille (:191), `SparklineCells [7]` (:195); helpers `Gauge` :207, `Sparkline` :223, `Spinner` :243; `Glyphs()` :268; `BannedGlyphs` :364.

### `/home/santosh/af-home/internal/tui3/styles.go` — what home actually paints with

tui3 has its **own** hue table (`styles.go:244-402`) — this is the surface home uses, not the tokens package directly:

| Hue | Line | Hex | Role |
|---|---|---|---|
| `hueInk` | 248 | `#C6CDDA` | body |
| `hueLive` | 313 | `#D8DEE9` | one step above body — a reply still arriving |
| `hueAccent` | 314 | `#9DC3E6` | the person's own hue; the accent budget |
| `hueMuted` | 315 | `#7FA6C9` | labels, headings, "working" |
| `hueNarr` | 334 | `#848FA6` | demoted prose |
| `hueDim` | 335 | `#6B7280` | telemetry |
| `hueAdd` | 336 | `#A3BE8C` | **it finished / landed** (green) |
| `hueDel` | 337 | `#C67173` | |
| `hueBad` | 338 | `#D08770` | failure |
| `hueAsk` | 339 | `#C08FE8` | **a person is being waited on** (violet; the reservation) |
| `hueWarn` | 347 | `#EBCB8B` | a bound about to be reached |
| `hueData` | 389 | `#91C5D4` | the payload rule's datum ink |
| `hueViolet` | 402 | `#8F6FA8` | held, unspent |

Ground ladder (`styles.go:510-519`): `hueCursor #242932`, `hueSelected #2E3440`, `hueMark #434C5E`. **Rest is not a colour** — an unremarkable row is painted by not painting it.

`palette` struct :907; methods home uses: `paint` :992, `ink` :1014, `live` :1020, `accent` :1022, `muted` :1023, `narr` :1028, `dim` :1030, `add` :1031, `del` :1032, `bad` :1033, `warn` :1038, `data` :1043, `violet` :1047, `underline` :1072, `fade`/`fadeRow` :1087, `ask` :1117, `askBold` :1122, `cursor` :1143, `selected` :1155, `mark` :1166, `chip` :1190, `tint` :1201, `background` :1214, `bold` :1234, `italic` :1245.

Home's own glyph constants — `home.go:214-224`: `homeAskGlyph ▲` (the only shape that points — a conversation stopped on a question), `homeLiveGlyph ●`, `homeStuckGlyph ◌`, `homeIdleGlyph ○`, with ASCII fallbacks `! * o -`. `homeStartGlyph +` (:312). `bandFoldGlyph ▸` / `bandFoldOpenGlyph ▾` (homebands.go:305, :308).

---

## 7. Tests that pin home behaviour

All in `/home/santosh/af-home/internal/tui3/`. Counts: `home_test.go` 94 tests, `homeexchange_test.go`, `homestanding_test.go`, `homeattention_test.go`, `homebridge_test.go`, `homephone_test.go`, plus band tests. `go test ./internal/tui3/` takes ~150s.

### `home_test.go` (94) — the biggest surface of invariants
Shape & frame: `TestHomeTakesExactlyTheWholeFrame` (:240), `TestTheGutterIsAStraightLine` (:1388), `TestTheListIsPaddedOffTheFoot` (:3178), `TestAnEmptyHomeKeepsItsShapeAtEveryWidth` (:2205), `TestHomeWithNothingTypedHangsFromTheTop` (:767), `TestTheTitleBandSurvivesAShortFrame` (:1453).
Typed surface: `TestTypingFiltersLiveWhileTheActionRowStaysTheDefault` (:537), `TestEnterStillStartsAChatWithMatchesOnScreen` (:565), `TestWalkingOffTheActionRowPicksFromTheList` (:603), `TestTypingClustersAtTheFootOfHome` (:646), `TestTheCursorGoesToTheFootWhileTypingAndBackAtRest` (:884), `TestTheBestMatchSitsNextToTheActionRow` (:943), `TestTheInvertedDropUpKeepsHeadingsAboveTheirRows` (:1035), `TestTheDropUpDoesNotLiftTheCardWithIt` (:1156), `TestAQueryMatchesWhatATaskCameTo` (:1286), `TestNeedsYouOutranksAColdRowItTiesWith` (:1320), `TestAMatchBehindTheCollapseIsFoundAnyway` (:1189), `TestSearchSeesThroughTheFoldedTier` (:3088), `TestALetterAlwaysTypesWhateverIsChosen` (:3331), `TestHomeCaretStaysInTheDraftWhenItWraps` (:3206), `TestHomesBoxWrapsALongDraftInsteadOfTruncatingIt` (:2119).
Cursor/esc: `TestEscPeelsTheQueryThenCloses` (:1362), `TestHomeEscClearsTheBoxBeforeItLeaves` (:271), `TestThePreviewCardFollowsTheCursorWhileTyping` (:1077), `TestTheCursorSkipsTheElsewhereRule` (:3149), `TestAFreshLaunchOpensOnTheFirstConversationWhenItsOwnIsNotListed` (:819).
Tiers/folds: `TestHomeOpensThreeProjectsAndFoldsTheRest` (:2868), `TestAFoldedProjectSurfaces{WhatIsWaiting,WhatIsRunning,AStandingItemThatNeedsYou,AStandingItemThatIsFiring}` (:2901-2963), `TestEnterOpensAFoldedProjectInPlaceAndFoldsItAgain` (:3001), `TestAProjectLineGetsAProjectCard` (:3060), `TestTheFoldedBlockFoldsItselfPastEight` (:3110), `TestTheCollapseLineOpensAndFolds` (:1223), `TestHomeCollapsesTheQuietTailOfAProject` (:513).
State truth: `TestHomeWillNotCallAStaleRowRunning` (:308), `TestHomeCallsARowRunningWhenTheSessionSaysItHasThatNodeOut` (:337), `TestHomeCallsARowIncompleteWhenTheLiveSessionDoesNotNameIt` (:380), `TestHomeDoesNotBelieveAStalePresence` (:403), `TestHomePutsASessionThatNeedsYouFirst` (:431), `TestHomeStopsSayingNeedsYouWhenTheWindowIsGone` (:492), `TestHomeSaysNothingAboutNoTasksAndNoSpend` (:293) — the emptiness law.
Doors/refusals: `TestHomeOpensAnotherProjectAndTheOneYouLeaveGoesOnRunning` (:1486), `TestARowThisTerminalHoldsSaysOpenAndNeverAnotherWindow` (:1523), `TestALockedRowSaysSoInTheList` (:2568), `TestEnterOnALockedRowRefusesInHomesOwnVoice` (:2591), `TestASecondEnterOnALockedRowDoesNotStack` (:2638), `TestTheRaceLosesInTheSameWordsNotARawError` (:2662), `TestHomeRefusesARowWhoseFolderIsGone` (:1556), `TestHomeMarksARowWhoseFolderIsGoneOnTheRowAndOnTheCard` (:1582), `TestHomeRefusesOverHost` (:1930), `TestHomeLinksTheProjectDirectoryItNames` (:2774).
Perf laws: **`TestHomeStatsAFolderOncePerReadingAndNotPerFrame` (:1646)** — the syscall counter behind `var homeFolderThere` (`home.go:2665`); **`TestALaunchThatIsNotGreetedNeverWalksTheDiskForTheDoor` (:2400)**; `TestHomeBeatStopsWhenHomeCloses` (:1749).
Landing: `TestHomeIsTheFirstFrameOfAnOrdinaryLaunch` (:1769), `TestAFirstRunGoesStraightToTheChat` (:1796), `TestAnEmptyMachineGoesStraightToTheChat` (:1816), `TestALaunchThatNamedASessionIsNotGreeted` (:1826), `TestTheWelcomeBoxRetiresWhenHomeLands` (:1848).
Gesture: `TestDoubleSpaceInAnEmptyBoxGoesHome` (:2019), `TestASingleSpaceThenALetterTypesNormally` (:2047), `TestAPasteThatStartsWithTwoSpacesDoesNotGoHome` (:2075), `TestTheDoorAndHomeBounceBackAndForth` (:2494), `TestClickingTheDoorGoesHome` (:2455).
Archive: `TestArchivePutsARowAwayAndBringsItBack` (:3252).

### `homecard_test.go` (8)
`TestAQuietCardReadsItsWorkAsALedger` (:63), `TestARunningCardLeadsTheRowWithItsState` (:100), `TestEveryNodeOfAFamilyIsItsOwnRowOnTheCard` (:138), `TestWorkLandedSinceYouLastLookedIsMarked` (:172), `TestTheFirstLookMarksNothing` (:213), `TestYourOwnLandingsAreNotNews` (:237), `TestHomeAnimatesOnlyWhileWorkRuns` (:259), `TestTheFactsLineCarriesTheFilesFigure` (:291).

### Band tests
- `homebands_ambient_test.go` — news/deliverables/leftoff bands read-without-draining, fold, and fit.
- `homebands_ambientb_test.go` — repo band caches + degrades when git cannot answer; keys band legends; nextup shows two + fold; spend band emptiness.
- `homebands_clauses_test.go` — `TestBandClausesKeepsWholeFacts` (:10), `TestFitLeftKeepsThePlaceTail` (:53).
- `homeband_work_test.go` — two-lines-and-a-blank shape, narrow keeps files+cost, done wears no tick, fold at three, click toggles, **`TestTheRightArrowOpensEveryFoldOnTheCard` (:219)**.
- `homeband_project_test.go` — project card content, fold past four, weekly counts, clipping, and **`TestTheProjectBandsAreRegisteredInReadingOrder` (:332)** — pins the registry order.
- `homeband_machine_test.go` — watchlist soonest-first, since-you-left every row is a door, today's counts + emptiness, **`TestThePulseSaysWhatIsOnWatchAndWhatTodayCost` (:210)**, `TestTheMachineCardPaintsMeaningAndNotMood` (:274), `TestWalkingUpOffTheTopRowReachesTheMachineCard` (:337), **`TestTheMachineCardAtRestSpendsNoAccent` (:402)**.
- `homeband_spark_test.go` (12) — the hands count/spark: absent at zero, datum-vs-word painting, never at the list tier, one sample per reading, quantizer bounds.
- `homeband_answer_test.go` (13) — the digit answer path across windows, narrow chip retention, refusing a stale window, clicking a chip, saying no to a standing card.

### Other home tests
`homebridge_test.go` (16) — **`TestTheWidthLadderPicksOneShapePerTier` (:56)**, zones leave the list only at the columns tier, third column is the row's card, `TestHomeOpensOnItsOwnConversationAndRestStillWakesIntoTheList` (:157), first-arrow landing, `TestEnterAtRestReturnsToTheConversationYouAreHolding` (:261), **`TestExactlyOneRowSpinsHoweverManyAreMoving` (:338)**, tab cycling, arrow crossing, `TestTheZoneColumnDoesNotScrollWithTheList` (:534).
`homesection_test.go` (7) — exactly one heading marked or none, hover does not move it, marked heading brightens and wears no ground.
`homehover_test.go` (6) — the pointer previews, the cursor selects.
`homearrows_test.go` (4) — arrows walk the whole column on a narrow home even with a held roster.
Plus `homeattention_test.go`, `homestanding_test.go`, `homeexchange_test.go`, `homephone_test.go`, `homebands_*`.

### Cross-cutting gates a redesign will trip
- `designlanguage_test.go` (11): `TestNothingButStylesAuthorsAColour` (:106) — **no escape sequence outside styles.go**; `TestOnlyTheGroundLadderPaintsABackground` (:155); `TestTheSignalHuesAreIsoluminant` (:217); `TestNoTwoRolesShareA256Index` (:304); `TestTheGroundLadderLandsInItsBand` (:365) / `…StepsNeverCollapse` (:431); `TestTheBodyInkDoesNotGlare` (:499); `TestTheSpacingLadderKeepsItsSharedSteps` (:30); `TestARepeatedBlockBoundaryDoesNotGrowASecondBlankRow` (:50).
- `chrome_test.go` (34): `TestOpeningOneFullscreenPageClosesTheOtherTwo` (:1261) and `TestTheFrameDrawsThePageThatWasOpenedLast` (:1305) — the page-stack law; the welcome-box tests (:834-944) which home's landing retires.
- `bottomchrome_test.go` (9): the breathing gap, the jump chip, `TestTheDraftBlockAnchorsAtTheTop` (:290), `TestTheCaretLandsOnTheDraftsFirstRowWhenTheDraftIsLong` (:362).
- `manual_test.go` (3): `TestTheManualMentionsEveryCommandTheTableOffers` (:22), **`TestTheChatManualDoesNotSpeakOfTheResident` (:44)** — bans `alt+1`, the board, standing watches, front desk from the chat corpus, `TestEveryChatManualPageIsReachableByItsOwnName` (:70).
- `sessionrows_test.go`, `surface_test.go`, `render_bench_test.go`, `SIZE-BUDGET` ratchet in `make check`.

---

## 8. Manual pages that describe home today

`/home/santosh/af-home/internal/manual/chat/` — v3's own account of itself, compiled into the binary and packed to `chat.pack.gz`.

### The primary page
**`home.md` — 2,024 lines, 58 `##` sections** — every section is a self-contained retrieval unit under ~2000 chars. The full heading list (each is a search index entry) covers: `/home` itself; the greeting and how to skip it; what home shows; the `needs you` strip and pressing a landed row; the `moving` strip and the one-spinner rule; empty strips; the three columns and which column you are in; the elsewhere block; archiving; opening a collapsed project; **the pulse line**; where the cursor starts; the machine's own card; the agents chart; the right pane and hovering; the project card; the work band and `▸ …5 more tasks`; the two shapes (list jump on typing); switching sessions; long drafts; refusals (`open in another window`, `folder gone`); opening another project; searching, its order and the semantic-search refusal; starting something new by typing; `space space`; empty home; `is there a key for home?`; the ✓ / `since you last looked`; answering a question from home; `incomplete`; fresh machine and `--host`; whether home updates; the `◦` standing rows; pausing with `ctrl+e`/`ctrl+x`; the item card; `ctrl+v` on an item; `ask here`; deliverables/leftoff/repo/next-up/spend bands; **`## What do the keys on a home card do — open, new chat, folder, and copy path` (:1873)**; narrow cards; **home on a phone (:1940), the sheet and `‹ back` (:1974), answering from a phone (:2008)**.

### Companion pages
- **`asking-from-home.md`** (313 lines, 13 sections) — the whole `ask here` errand lifecycle: the exchange row, the spinner, `tab`/`esc`, two at once, when it goes away, narrow windows, `1 yes / 2 change / 3 once / 0 no`, "continue as a conversation", and "what it will not do".
- **`keeping-an-eye.md`** (738 lines) — standing orders end to end; `## Where is the record of a reminder I made from home` (:614).
- **`keys.md`** — **`## Keys on home, and is there a shortcut for it` (:1003)**; `## Chords that mean more than one thing` (:1499); `## ctrl+v — how hard the thing you are looking at thinks` (:1546); **`## Chords that are not bound` (:1583)**.
- **`screen.md`** — `## What the frame draws, top to bottom` (:13), `## Other width thresholds worth knowing` (:675), `## What phone width reshapes: the eight` (:637), `## What each colour means` (:1626), `## Why the bottom rows of a long list look dimmer` (:484).
- **`empty-screen.md`** — the greeting home replaces, `## Recent sessions on the empty screen — where did the recent sessions list go` (:113).
- Also mentioning home: `starting-aforge.md`, `commands.md`, `sessions-and-rewind.md`, `standing-orders.md`, `tasks.md`, `how-tasks-run.md`, `adaptive-runs.md`, `models-and-cost.md`, `permissions.md`, `attaching-files.md`, `saved-shapes-of-work.md`, `subharnesses.md`, `making-pictures-audio-and-video.md`, `running-on-another-machine.md`, `reaching-this-machine-without-ssh.md`, `opening-files-from-that-machine.md`.

### The three gates (CLAUDE.md:52-60)
1. `/home/santosh/af-home/internal/tui3/manual_test.go` — every slash command **and every alias**, with its leading slash, appears somewhere in the corpus.
2. `/home/santosh/af-home/internal/session/manual_test.go` — every tool on the belt appears by its exact registered name.
3. `/home/santosh/af-home/internal/manual/chat_test.go` — every probe in its table (100+ real questions in a person's own words) still reaches the page that answers it.
Plus `/home/santosh/af-home/internal/manual/packed_test.go` — the packed archive must agree with the folders (`make build` regenerates it).

**Failure mode to plan for:** the gates check that a name is *mentioned*, never that the claim around it is true. A redesign that removes a key or a strip must grep the corpus for the old sentence and delete it in the same change — home.md quotes constants verbatim (`homeFootWord`, `homeEmptyWord`, `homeHeldShort`, `homeGoneShort`, `attentionNeedsWord`, `attentionMovingWord`, `attentionNeedsTeach`, `attentionMovingTeach`, `pulseWatchWord`, `pulseOrderWord`, `pulseWorkingWord`, `homePhoneWaitingWord`/`RunningWord`/`NewsWord`, `homeSheetBackWord`), so changing a string is a manual edit in the same commit.

---

## Notes for a redesign team

- **Everything width-related is derived, not chosen.** `homeMinColumns` is the sum of three floors and two gutters; `RailAtWidth` is `60 + 2 + 28`. `homeTierAt` is the one place a width is compared, and `breakpoints_test.go` asserts the derivations.
- **`homeFrame` writes three parallel hit maps** (`hits`, `panes`, `zones`) at draw time; every pointer answer resolves against what was actually drawn. Any new column needs a fourth.
- **The disk is asked once per 3-second beat and never per frame** — `readGone` (one stat per project), `readStandBands` (one walk), `machineFactsAt` (one reading), `standWeek` (one ledger read), `home.repos`/`home.news`/`home.deliverables`/`home.last` TTL caches. `var homeFolderThere` and `processOpener` exist so tests can *count* the syscalls.
- **The alt+number namespace is completely free** and the corpus is forbidden from using `alt+1` only because that string is resident vocabulary — a new binding would need the manual's phrasing to avoid the banned list in `manual_test.go:44`.
- **The band registry is the only extension point that costs nothing.** A new band is one file with an `init`, an order key between neighbours, and a `kinds` list; `homeSheetOrder`/`homeSheetMore` optionally place it on the phone tier. There is no equivalent registry for the left column, the strips (a table, `homeZones`), or the pulse (a hand-written function).