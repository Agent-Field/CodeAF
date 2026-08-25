<!-- Page/overlay inventory and router proposal for the home rethink, written 2026-08-25 on
     branch home/rethink-v0. Companion to RECON.md (home's own architecture) and SCREENS.txt
     (the 18 mockups). Line numbers are as of the current tree. No Go file was modified. -->

# PAGES — every surface that takes the frame, and what a seven-place router would cost

All paths are absolute. Everything below was read, not inferred; where a claim could not be
confirmed in the tree it says so.

**Three corrections to the brief and to RECON.md, stated up front because the rest depends on
them:**

1. **`standingpage.go` is not a fullscreen page.** `a.standPage` is an *overlay drawn under the
   draft*, sized by `palette.go:1121` and drawn by `palette.go:1178`, capped at
   `standRowsMax = 12` (`/home/santosh/af-home/internal/tui3/standingpage.go:64`). It does **not**
   call `standDownFullscreen()`, and `view.go`'s frame dispatch never mentions it. There are
   **four** fullscreen pages, not five.
2. **The app field is `a.memPanel`, not `a.memoryPanel`** (`/home/santosh/af-home/internal/tui3/app.go:1006`).
3. **RECON.md:384 is half wrong.** `store.Memory.UseCount` and `store.Memory.MissCount` are
   exported (`/home/santosh/af-home/internal/store/memory.go:134-135`), written by v3's own engine
   (`/home/santosh/af-home/internal/session/memory.go:515`), and **already arrive in the memory
   panel's `p.all`** through `ListMemories`. Screen 1f's `helped 14 times` / `helped 6 · bore on 2`
   need no new seam. What is genuinely unreachable is the *consolidation* receipt and the whole
   `store.Fact` table — see §1.9.

---

## 1. INVENTORY

### 1.0 The whole set, at a glance

| Surface | Field | File | Takes the frame? | In `standDownFullscreen`? | Has a text box? | Bare letters |
|---|---|---|---|---|---|---|
| first run | `a.setup` | firstrun.go | yes, above everything (`view.go:220`) | no | yes | type |
| settings | `a.sheet` | settings.go | yes (`view.go:228`) | yes (`settings.go:935`) | yes, `query editor` | type |
| task page | `a.taskSheet` | taskview.go | yes (`view.go:236`) | yes (`settings.go:938`) | yes, `query editor` | type |
| home | `a.home` | home.go | yes (`view.go:244`) | yes (`settings.go:941`) | yes, `box editor` | type |
| rewind timeline | `a.rewSheet` | rewindsheet.go | yes (`view.go:253`) | yes (`settings.go:948`) | yes, `query editor` | type |
| phone status sheet | `a.deck` | statusdeck.go | phone only (`view.go:277`) | no, by design (`settings.go:931-933`) | no | `q` closes |
| phone tool detail | `a.expand` | expand.go | phone only (`view.go:291`) | no, by design | no | `q` closes |
| standing page | `a.standPage` | standingpage.go | **no — overlay** (`palette.go:1178`) | **no** | **no** | **`p` `s` `n` are verbs** |
| memory panel | `a.memPanel` | memorypanel.go | no — overlay (`palette.go:1163`) | no | yes, `filter editor` | **`u`, and `e` when expanded** |
| permissions | `a.permPanel` | permissions.go | no — overlay | no | no | `d` drops a rule |
| connections | `a.connPanel` | connectpanel.go | no — overlay | no | yes | type |
| harnesses | `a.harnPanel` | harnesspanel.go | no — overlay | no | no | — |
| deliverables | `a.shelf` | deliverables.go | no — overlay | no | yes | type |
| model picker | `a.pick` | palette.go:33 | no — overlay | no | yes | type |
| crew picker | `a.crewPick` | crew.go:183 | no — overlay | no | **no filter** | — |
| resume roster | `a.roster` | resume.go | no — overlay | no | yes | type |
| effort ladder | `a.effPick` | effortchip.go | no — overlay | no | no | — |
| subharness page | `a.subPage` | subharness.go | no — overlay | no | yes | type |

The overlays share one height switch (`palette.go:1096-1145`) and one draw switch
(`palette.go:1149-1185`), and each is modal for the keyboard except `ctrl+c`
(`input.go:328-410`).

---

### 1.1 home — `a.home homeView`

Fully mapped in RECON.md §1–§4. Summarised here only where the router touches it.

- **State**: `homeView` — `/home/santosh/af-home/internal/tui3/home.go:502`. Closing is
  `a.home = homeView{}` (`home.go:921`).
- **Open**: `openHome()` — `home.go:698`. Refuses over `--host` (`home.go:699-706`), calls
  `standDownFullscreen()` **first** (`home.go:711`), then `closeLists()`, `dismissWelcome()`,
  builds the view, reads the world, the stand bands and the folder stats, and returns its own
  3-second clock. Doors: `/home` (`commands.go:84`), `space space` (`home.go:2945`), the legend
  door `homeDoorWord = "space space home"` (`home.go:2911`), and the launch landing
  (`landHome`, `home.go:752`).
- **Close**: `closeHome()` — `home.go:904`. Bumps `a.homeGen` and — load-bearing for the
  router — **writes the look stamp**: `session.NoteLook(a.placesRoot(), a.now())`
  (`home.go:911`). "Closing is the look."
- **Draws**: `homeFrame(width, height)` — `home.go:3276`. Pulse, blank, dim rule, blank; foot
  measured before the body (`home.go:3333-3350`); body; blank; rule; box rows or
  `homeFootWord`; the answer strip; then `msg` or `homeHint()`. Tail-clamp keeps row 0 and the
  last `height-1` rows and **the caret rides the clamp** (`home.go:3439-3449`).
- **Seams**: the whole table is RECON.md §5. The law that governs them:
  *"It must NOT block: home calls it on every three-second beat and on the keystroke that opens
  the screen"* (`tui3.go:633`).
- **Keys**: `homeKey` — `home.go:1958`. `tab` cycles zones at the columns tier only
  (`home.go:2032` → `homeTab`, `homebridge.go:493`); `esc` peels the box then closes
  (`home.go:2050`); `alt+enter`/`ctrl+enter` = `askHere` (`home.go:2083`). **A bare letter always
  types** (`home.go:2010-2017`); the one printable exception is the digit answer block
  (`home.go:2026`), and it is earned by the chips being drawn on the row.
- **Width**: two ladders settled in one breath at `home.go:3287` — the program's `layoutTier`
  (`view.go:1132`) for phone, and home's own `homeTierAt` (`homebridge.go:121`) for
  list/card/columns. `homeMinColumns = 110` is derived from the three column floors and two
  gutters (`homebridge.go:110`).
- **Tests**: RECON.md §7 — `home_test.go` (94), `homecard_test.go` (8), `homebridge_test.go` (16),
  `homesection_test.go` (7), `homehover_test.go` (6), `homearrows_test.go` (4), plus
  `homeattention_test.go`, `homestanding_test.go`, `homeexchange_test.go`, `homephone_test.go`
  and eight band test files.

**A live discrepancy the redesign has to settle.** `homeItemActions = "enter open where it was
asked · p pause · s stop"` (`homestanding.go:73`) is drawn on home's standing-item card
(`homestanding.go:603`) and on home's own hint line (`home.go:4612`) — but home binds those two
actions to `ctrl+e` (`home.go:2152`) and `ctrl+x` (`home.go:2179`), and `homeKey`'s switch has no
`"p"` or `"s"` case, so both letters fall to the default and type (`home.go:2320`). The string is
the **standing page's** legend, reused verbatim; on the standing page `p` and `s` really are
bound (`standingpage.go:551-554`). So home today advertises two keys it does not have. Screen
3a's clause — *no key does anything that isn't drawn on screen right now* — has an inverse that
is already being broken here, and the `→` verb strip is the fix for both directions at once.

---

### 1.2 settings — `a.sheet sheet` (settings.go)

- **State**: `sheet` — `settings.go:686`. `open` :687, **`tab int` :688**, `registry
  *config.Settings` :690, `conns Connections` :694, `conn connTab` :696, `rows []config.Setting`
  :697, `defaults map[string]string` :700, `sessionModel string` :706, `items []sheetItem` :710,
  `cursor` :711, `top` :712 (in **display lines**, not items), **`query editor` :717**, `edit
  *sheetEdit` :721, `sel *sheetSelect` :722, `msg string` :725. Ceiling `sheetRows = 16` :664 —
  for pgup/pgdown only.
- **Open**: `openSettings()` — `settings.go:889`; notes `settingsRemoteWord` when hosted
  (:897-899), calls `standDownFullscreen()` (:900), rebuilds. Doors: `/settings` `/set` `/config`
  (`commands.go:66`, dispatched `app.go:5001`) and `ctrl+,` (`input.go:674`).
- **Close**: `closeSettings()` — `settings.go:913` — `a.sheet = sheet{}`. `esc` is layered:
  search → `connEsc()` → close (`settings.go:1480-1496`).
- **Draws**: `sheetFrame` — `settings.go:1835`. Head `sheetTitle` (:1933) → blank →
  **`sheetTabBar(width, s.tab, pal)` (:1848, defined :2010)** → dim rule (:1849) → blank → body
  (`listLines` :2038, or `selectLines` :2166 when the model submenu is up) → dim rule (:1896) →
  foot (edit box / picker filter / `pal.bad(s.msg)` / `footNote`) → keys line (:1911). Short
  frames keep row 0 and the tail (:1913-1919).
- **Sections**: `var settingTabs = []string{tabSession, tabContext, tabWorkspace, tabDisplay,
  tabProviders, tabConnections}` — `settings.go:85`; words at `settings.go:69,71,73,76,78` and
  `connectcaps.go:109`. One sub-heading, `rolesHead = "roles"` (`settings.go:1182`), emitted per
  `roles.Tier` (`settings.go:1246`).
- **Seams**: `Options.Settings *config.Settings` (`tui3.go:480`) + `Options.ProfileDir`
  (`tui3.go:418`) → `a.registry()` (`settings.go:829`); `Options.Connections`
  (`tui3.go:490`) → `s.buildConnections()` (:970); `internal/roles` → `s.rolesSource()` (:1278);
  models through `a.modelsFor(keep)` (:1586). **No setting is declared in tui3** (:30-33).
- **Keys**: `sheetKey` — `settings.go:1457`. Submenus claim the keyboard first (:1463-1477).
  **`left`/`shift+tab` = previous tab (:1498); `right`/`tab` = next tab (:1500)** — the only
  `shift+`-modified binding in the whole package. `enter`, `" "`, `space` activate (:1516).
  Every other printable except a bare space types into the search (:1535-1539). **No bare-letter
  verbs.**
- **Width**: one column at every width. The only branch is `phoneList(width)` (`palette.go:473`)
  turning a row into two lines. Tab-bar geometry: `tabGap = 1` :1965, `tabPad = " "` :1969,
  `tabPadCols = 2` :1970, `tabLead = 1` :1973, `tabSpans()` :1976; an overflowing bar is
  `fit()`-cut, never wrapped (:2025-2027).
- **Tests**: `chrome_test.go:96, :117, :145, :193, :242, :262, :281, :412, :458`;
  `chip_test.go:162`; `palettephone_test.go:359, :403`; `settingsfix_test.go:59, :126, :157,
  :203, :260, :315, :329`; `settingsroles_test.go:92, :186, :216, :275, :307, :329, :350, :369`;
  `connectkey_test.go:649`; `host_test.go:210`; `firstrun_test.go:279`.
  (`TestSettingsSheetIsOneCalmColumnAtEveryWidth` is in **`internal/tui`**, v1, and does not pin
  this file — CLAUDE.md:135 lists it as a known clean-tree failure.)

**Against the mockups.** SCREENS.txt has **no settings screen** — settings appears only as a word
in the tab strip (`SCREENS.txt:161`, `:324`), as the destination for "Crew, accounts, permissions,
fixes, harnesses → settings, one page, checked monthly" (`SCREENS.txt:9`), and as the open
question "is spend a place or a section of settings?" (`SCREENS.txt:217`). `SCREENS.txt:352` lists
"design standing and settings the same way" as *future* work. So settings needs **no page work at
all** for this design — it is already a fullscreen page with a tab bar, a cross-tab search and a
one-column body at every width. What it contributes is the opposite: **it is the best seed for
the place tab bar itself** (§4.4).

---

### 1.3 the task page — `a.taskSheet taskSheet` (taskview.go)

- **State**: `taskSheet` — `taskview.go:217`. `open` :218, `cursor` :219 (**an item index, not a
  line index**, :212-216), `top` :220, **`query editor` :225**, `detail
  session.TaskIndexEntry` :238 (a **copy**, :231-237), `detailOn` :239, `detailTop` :243,
  `tail string` :248, `tailRead` :249. Row model `taskSheetItem` :259-290; `pick()` :301.
  `taskSheetRows = 12` :208 (pgup/pgdown only).
- **Open**: `openTaskSheet() bool` — `taskview.go:488`; refuses on `!taskSheetHasAnything()`
  (:495), calls `standDownFullscreen()` (:502), then `a.taskSheet = taskSheet{open: true}` (:503).
  `openTaskPage() tea.Cmd` :517 wraps it and notes `taskSheetEmpty` on refusal. Doors: `/history`
  (`commands.go:220`, dispatched `app.go:5117`), `ctrl+.` (`taskSheetKey`, `taskview.go:85`,
  handled `taskview.go:844`), bare `/task` (`taskcommand.go:62`), the column foot doors, the phone
  strip (`taskphone.go:162`), the room door (`room.go:2152`), and `openTaskRecord`
  (`taskrecord.go:140`, a second door that states the exclusion law itself).
- **Close**: `closeTaskSheet()` :528 — `a.taskSheet = taskSheet{}`. `esc` clears the filter first
  (:877-889); `ctrl+.` closes from anywhere (:890-895).
- **Draws**: `taskSheetFrame` — `taskview.go:1157`. Card mode delegates the whole frame away
  (:1166-1169). Otherwise: title `" history … esc close"` (:1181, defined :1358) → blank → dim
  rule (:1183) → body (:1209-1228) with a depth fade over the cut-off tail (:1233-1240) → dim
  rule (:1245) → **tally at the foot** (:1246) → filter line while filtering (:1247-1249) → keys
  line (:1257). Caret is always (0,0) (:1154-1156).
- **Sections — exactly two**: `taskSheetNowHead = "running"` (:100, emitted :610) and
  `taskSheetPastHead = "earlier"` (:104, emitted :622). An empty section is not drawn (:601-604).
  Inside `running`: this session's tree (`taskSheetForest` :724, which **never folds**, :718-723),
  then other windows' flat rows. Inside `earlier`: the index's own newest-first order,
  **un-resorted** (:751-754).
- **Row columns**: a 2-cell lead (`› ` cursor / `· ` hover / blank, :1491-1499); tree rows carry
  connectors + glyph + title + right margin `#<id>` (`railMetaWord`, `task.go:4349`) dropped below
  `railTitleFloor = 12` (:1535), with a `railUnder` block beneath; record rows carry
  `taskRecordLabel` (:1620) and **one** right-margin cell `taskRecordNote` (:1643) = `running` /
  `incomplete` / an age.
- **Seams**: `TaskIndex()` (`/home/santosh/af-home/internal/session/task_index.go:437`) read
  off-loop through `a.loadTasks()` (`taskmention.go:109`); other windows through
  `elsewhereAgent.Elsewhere()` / `ElsewhereExcept` (interfaces `taskview.go:355-369`, impl
  `/home/santosh/af-home/internal/session/taskelsewhere.go:143,:154`), refreshed on a 3-second
  gate (`elsewhereEvery` :338, `refreshElsewhere` :393) — **and that refresh is synchronous, from
  inside layout** (:371-379). The card's journal tail is a `tea.Cmd` (`taskrecord.go:183`).
  Nothing shells out.
- **Keys**: `taskSheetKeyPress` — `taskview.go:844`, reached from `input.go:457` (**below**
  `ctrl+c`). Card mode takes every key first (:873). `esc` :877, `ctrl+.` :890, arrows/pgup/pgdown
  /home/end :896-908, `enter` :909. **Every printable key, space included, types into the filter**
  (:923-936, law stated :924-932). `tab`, `left`, `right` and everything else are **swallowed**
  (:937-941). Inside the record card, `m` is the one bare-letter verb (`taskrecord.go:253`).
- **Width**: one breakpoint only — `layoutTier(width) == tierPhone` (:1112, :1253, :1459, :1283).
  **No card column and no two-column layout at any width.** Row room is `width - 2` (:1451).
- **Tests**: `taskview_test.go:57, :99, :119, :151, :208, :231, :252, :287, :309, :349, :374,
  :413, :463, :505, :561, :581, :662, :730, :778, :814, :847, :903, :940`;
  `taskaway_test.go:84, :129, :182, :218, :238, :262, :290, :313`;
  `taskphone_test.go:47, :83, :102, :143, :190, :222, :272`;
  `depthfade_test.go:146, :167, :177, :209`; `chrome_test.go:1261`;
  `hoverpressable_test.go:296`; `brieffold_test.go:357`; `margin_test.go:323`;
  `homeattention_test.go:499, :594`; `notice_test.go:361, :405`; `effortscope_test.go:370`.

#### Against SCREEN 1e (`SCREENS.txt:302-319`) — the tasks place

**Already drawn** — a fullscreen page of everything the project ran (`taskview.go:1157`); the
heading word `running` (:100); a left state glyph on every row (:1620, :1525); the task-name
column; a right-margin time cell (`taskNoteWord`, `taskmention.go:366`); a "what happened" note
under **running** rows (`railUnder`); cost, but **only** inside `railUnder` on running rows
(`task.go:4419-4425`); `enter open its room` (`taskSheetRoomKeys` :155); type-to-filter behaviour
(:923); a counts line (`taskSheetTally` :1373).

**Missing, and what it costs:**

| 1e wants | Today | Change |
|---|---|---|
| group by state — `needs your look` / `running` / `done today` | grouped **live vs record**, two sections (:590-628) | a third bucket and a third heading. The vocabulary already exists: `taskStateWord` (:1666) returns `running`, `incomplete`, the fail word, `taskUnverifiedWord` (= needs your look) and the done word — **nothing groups on it** |
| `done today` — a time window | `earlier` is unbounded and un-windowed (:749-754) | new window state + `shift+←/→` handling. There is no `left`/`right` case in the key switch at all (:876-937) |
| the header sentence `51 since aug 2, $34.10 of it` | head is `history … esc close` (:1358); the count is `"3 running · 45 earlier"` at the **foot** (:1246, :1373), **with no money** | move it to the head, add a date anchor, add a spend sum |
| a `$` column on record rows | `taskSheetPastRow` (:1584) has exactly two cells | a second right-hand column — new row arithmetic, not a longer tail |
| a project / harness tag column | none | new column |
| `adaptive` as a word on the row | none | the run's kind is not drawn anywhere on this page |
| `▸45 more, back to aug 2` | the tail **fades** (:1229-1240) and the page scrolls; the forest never folds (:718-723) | a fold line |
| a visible `type to filter` hint | the behaviour exists; the hint does not. The words live only in `commands.go:776` | one clause on `taskSheetKeysLine` (:1416) |
| `→ verbs: run it again, stop it` | `right` is swallowed (:937) | see §3.5 — and note the head-on collision with :924-932 |
| `tab next place` | `tab` is swallowed (:937) | §3.3 |

---

### 1.4 the standing page — `a.standPage standPage` (standingpage.go)

- **State**: `standPage` — `standingpage.go:278`. `open` :279, `rows []standRow` :280 (a
  **snapshot**, re-adopted after a write), `cursor` :284 (**-1** when nothing is actionable), `top`
  :285, `owner []int` :289 (screen line → row, written at draw). **No query, no editor, no box.**
  Row model `standRow` :173, kinds :165-169.
- **Open**: `openStanding()` :504 → `openStandingAt(id)` :515. Refuses with `standNothingWord`
  when empty (:518-521), then `closeLists()` (:522), `dismissWelcome()` (:523), `start(rows)`
  (:524), `land(id)` (:525). **It never calls `standDownFullscreen()`.** Doors: `/standing`
  `/orders` (`commands.go:127,131`, dispatched `app.go:5046`), the send-door tag
  (`input.go:1003`), a margin row (`margin.go:429`), and the status-line `◦ keeping an eye on N`
  segment (`standdoor.go:62`, guarded :74).
- **Close**: `p.close()` :292 — `*p = standPage{}`. `esc` :539; also on a successful enter (:623,
  :610) and on any press outside the overlay (:576-578).
- **Draws**: `(p *standPage) draw(width, n, pal, hover, now)` — `standingpage.go:457`, height from
  `height()` :449, both wired into the chrome at `palette.go:1121` / `palette.go:1178`. **No rule,
  no head bar, no foot of its own** — one heading line `standHeading = "standing orders"` (:70,
  drawn :466), then rows, then padding.
- **Sections — three, outward**: `standInHereWord = "in this conversation"` :71,
  `standProjectWord = "for this project"` :72, `standEverywhereWord = "everywhere"` :73, mapped by
  `standShelfWord` :184. **An empty shelf draws nothing, heading included** (:265-268). Grouping is
  **by reach**, never by state, time or project. Order within a shelf is the store's own, never
  re-sorted (:231-234). A comment at :36-39 explicitly forbids a count beside a heading.
- **Row columns — exactly two**: `label` :405 (glyph + title) and `note` :424
  (`standRollup`, `homestanding.go:398`), clipped at `standWordsFloor = 18`
  (`homestanding.go:369`).
- **Seams**: `standingHereAgent` — `standingpage.go:134-146`: `StandingHere()`, `StandingExcept`,
  `StandingStandDown`, `StandingPause`; impl `/home/santosh/af-home/internal/session/standing_orders.go:39,
  :97, :130, :149`; accessor `standingSeam()` :150. **`StandingHere` is called synchronously on the
  UI goroutine** from `openStandingAt` (:517) and after every write (:688); it does
  `store.Applicable` + `store.List()` — a directory of documents off disk (`standing_orders.go:45,
  :52`). The file acknowledges this at :500-503. The three writes are synchronous disk writes from
  the key handler (:665-676).
- **Keys**: `standPageKey` — `standingpage.go:535`, reached from `input.go:395` where **every key
  but `ctrl+c` goes here**. `esc` :539, arrows/pgup/pgdown :541-548, `enter` :549, and
  **`p` pause :551, `s` stop :553, `n` not-here :555 — bare letters as verbs**. Everything else,
  `tab` and the arrows included, falls off the switch and is swallowed (:557). The law is stated
  at :531-534: *"The letters are bare rather than chords, which is what being modal buys: no draft
  is under this list for a letter to fall through into."* — **the exact inverse of taskview.go's
  rule**, and the tension SCREEN 3a exists to resolve.
- **Verbs line**: `standPageVerbs = homeItemActions + " · n " + standNotHereWord + " · esc"` :116
  = `"enter open where it was asked · p pause · s stop · n not here · esc"`. It is **not drawn by
  this file** — it is returned into the global hint slot at `render.go:2828`.
- **Width**: one column at every tier. `standRowsMax = 12` :64; `overlayWindow`
  (`palette.go:532`), `overlayItems` (`palette.go:548`); `phoneList(width)` (`palette.go:473`)
  consulted once at :431. No min-width refusal.
- **Tests**: `standingpage_test.go` :140, :177, :220, :239, :269, :286, :304, :321, :369, :384,
  :403, :419, :435, :451, :470 (`TestTheStandingPageHoldsAtEveryWidth`, widths 44/60/80/120/200),
  :504, :540; `margin_test.go:133, :234, :258, :279, :295, :308`;
  `standmark_test.go:236, :247, :258, :270`.

#### Against SCREEN 2f (`SCREENS.txt:202-217`) — the standing place

**Already drawn** — the list; one heading line; a left glyph; the order's own words as the main
column; a cadence in the tail; a "last look" clause (`homestanding.go:415-427`); `needs your look`;
`enter open it`; `pause` and `retire` as verbs (plus a fourth, `n`, the mockup does not have); a
scroll ceiling.

**Missing:**

| 2f wants | Today | Change |
|---|---|---|
| one flat list, ungrouped | three **reach** shelves (:71-73, :242-244) | either the shelves go or the rope column fights them for the same cells |
| `things aforge does without being asked. 6 standing, 1 waiting to be stood up.` | one line, `standing orders`, **no counts, no prose**; counts on headings are forbidden at :36-39 | a header sentence; and `waiting to be stood up` has **no representation** — the seam returns only `stand` and `excepted` (:136-138), a proposed-but-unratified order is a card, never a row here |
| the **how much rope** column — `asks first` / `earning tenure 3/5` / `tenured` / `wants standing up` | **zero occurrences**; `standRollup` has no tenure clause | the mockup's own "one column that earns its place" (`SCREENS.txt:216`) is entirely absent |
| a cost column — `under 1¢`, `not measured`, `$0.31`, `—` | **no money anywhere in the file** | new column + the words-not-figures rule |
| three right-hand cells | the row has **two** (:405, :424), the second already clipped at 18 cols | new row layout |
| `▸3 more, all quiet this week` | scrolls under `standRowsMax` (:64) | a fold line and a "quiet" classification |
| the two-line detail block under the cursor | squeezed into the row tail and clipped (`standFitNote`, `homestanding.go:357`) | an under-row block |
| `→ verbs` | verbs are **bare letters, always live** (:551-556) | inverting :531-534 — which is what 3a asks for, and which **frees `p`/`s`/`n` for the composer** |
| a `shift+←→` window | nothing reads a window | new |
| being a page under a tab bar | a **≤12-row overlay** under the draft | joining the exclusion law at `settings.go:918-950` and gaining a frame function |

---

### 1.5 the rewind timeline — `a.rewSheet rewindSheet` (rewindsheet.go)

- **State**: `rewindSheet` — `rewindsheet.go:157`. `open` :158, `points` :162 and `rows` :163
  captured on the way in, `cursor` :166 (indexes the **filtered** list), `top` :167, `at` :171,
  `armed` :175, **`query editor` :179**, `draft []rune` / `caret` :182-183 (the person's stashed
  sentence), `said string` :186. `rewindSheetPreviewRows = 3` :104, `rewindSheetPage = 12` :108.
- **Open**: `openRewindSheet()` :197 — refuses under six conditions (:198), needs `a.rewinder()`
  (:201) and non-empty points (:205), then `standDownFullscreen()` (:216) and stashes the draft
  (:217-223). `liftRewind()` :241 is `tab` from the inline mode. Doors: `/rewind` `/undo` `/back`
  (`commands.go:94`, dispatched `app.go:5164`), and `tab` inside inline rewind
  (`rewind.go:331`, key name `rewindSheetLiftKey = "tab"` :93).
- **Close**: `closeRewindSheet(restore bool)` :266 — puts the sentence back when `restore`
  (:270-273), then zeroes. `standDownFullscreen` passes `true` (`settings.go:948-950`), and the
  comment at :944-947 says why: the stashed sentence belongs to the person, not to the page.
- **Draws**: `rewindSheetFrame` — `rewindsheet.go:832`. Title (:844) → blank → a fixed 3-row
  preview (:846) → dim rule (:849) → list (:865-875) → dim rule (:877) → foot line (:878) →
  search line while searching (:879) → keys (:882). Caret always (0,0) (:890). **No named
  sections** — head / preview / rule / one flat list / rule / foot.
- **Seams**: `rewindAgent` — `rewind.go:124-129` (`RewindPoints`, `RewindAt`), asserted off
  `a.agent` by `a.rewinder()` (`rewind.go:132`); rows from `Agent.Transcript()` (`tui3.go:172`)
  at :292.
- **Keys**: `rewindSheetKey` :664, reached from `input.go:447` (below `ctrl+c`). `esc` layered
  :670, `enter` **two-stage** (place, then cut) :681, arrows :683-698, and **every other
  printable, space included, types into the search** (:711-718). **No bare-letter verbs.**
- **Width**: **no tier check anywhere in the file.** One column at every width; adaptation is
  subtraction only (`width-2` / `width-4`, :979-996) plus a foot clause that drops when it does not
  fit (:1046-1051).
- **Tests**: `rewindsheet_test.go:56, :88, :122, :135, :159, :192, :235, :250, :279, :325, :344,
  :367, :412`.
- **Gap worth knowing**: the rewind sheet is **not** in
  `TestOpeningOneFullscreenPageClosesTheOtherTwo` (`chrome_test.go:1261`), which still covers only
  three pages even though `standDownFullscreen` closes four.

The mockups do not draw rewind, and it is not one of the seven places. It stays a page reached by
a command.

---

### 1.6 the memory panel — `a.memPanel memoryPanel` (memorypanel.go)

- **State**: `memoryPanel` — `memorypanel.go:39`. `open` :40, `all []store.Memory` :41, `lower`
  :42, `score` :43, `hits` :44, `cursor` :45, `top` :46, **`scope int` :47** (an index into
  `memoryScopes`), **`filter editor` :48**, `expanded string` :49 (a memory ID), `edit *editor`
  :50, `origins` :51, `undoID`/`undoName` :52-53 (one-deep undo), `footer string` :54.
  `var memoryScopes = []string{"", store.MemoryScopeUser, store.MemoryScopeProject,
  store.MemoryScopeEnv}` :57 — **three real scopes plus "all"**. `memoryPanelRows = 12` :13.
- **Open**: `openMemory()` :255. Requires `a.brain()` (`memory.go:44`) **and** `a.memory != nil`,
  else notes `"memory is off · turn it on under /settings"` (:258). Then
  `ListMemories("", 500)` (:261) **and one `MemoryProvenance` call per memory, up to 500,
  synchronously** (:267-272). Door: `/memory` **with no argument only** — `app.go:5073-5081`;
  `/memories` and `/memory <query>` print into the transcript instead.
- **Close**: `p.close()` :59. `esc` at :315.
- **Draws**: `rows(width, n, pal, hover)` :156. List state: one line per memory (:200-212), label
  = `Title` plus `" — " + Text` when they differ (:203), a `"* "` prefix at `UseCount >= 5`
  (:206-208), right-hand note `Type · Scope[ · age]` (`p.note` :146, `store.AgeLabel`
  `facts.go:29`), empty state `"nothing is remembered here"` (:197), and a **last-line footer**
  `"scope · all"` (:214-217). Expanded state (:161-195): bold title, wrapped text, tags,
  `used · N` (:177), `created · <age> ago` (:183), `learned <age> in '<session>'` (:184-191),
  `enter edit · esc list` (:192). Hints `memoryFilterHint` :16, `memoryEditHint` :17.
- **Seam**: `MemoryStore` — `memorypanel.go:22-28`: `ListMemories`, `UpdateMemory`,
  `ForgetMemory`, `RestoreMemory`, `MemoryProvenance`. Wired by `Options.Memory`
  (`tui3.go:250-252`, "Nil means the panel is unavailable"), held at `app.go:1009`. Backed by
  `/home/santosh/af-home/internal/store/memory.go:552, :420, :452, :332, :889`.
- **Keys**: `memoryKey` :276. Three modes. In the list: `esc` :315, `enter` expands :317,
  `delete`/`ctrl+d` forget :321, **`u` undo :327**, `tab` cycles scope :338, default types into the
  filter :341. In the expanded state, **`e`** opens the edit box :303.
  **Consequence worth flagging: `u` is matched before the default, so a filter query containing
  the letter `u` cannot be typed** — and `u` is not in `memoryFilterHint` (:16).
- **Width**: an **overlay**, not a page — one arm of `palette.go:1106` (height) and
  `palette.go:1163` (draw), clamped so the status line and one conversation row survive
  (`palette.go:1137-1140`), and its box replaces the draft via `draftBlock`
  (`input.go:1085-1090`).
- **Tests**: `memory_test.go:282, :309, :338, :373, :407` (panel); `:134, :158, :172, :191, :209,
  :230, :251, :267` (the commands beside it).

#### Against SCREENS 1f (`:321-338`) and 2d (`:158-183`) — the memory place

**1f is roughly 45% built.** Present: the flat list, the body text on the row, a kind/scope tail,
ages in the store's own words, type-to-filter, forget (on `delete`, not `f`), fix-the-wording
(`e`, but only inside the expanded view), and provenance.

Missing for 1f, cheapest first:
1. **The outcome prose — `helped 14 times`, `helped 6 · bore on 2`.** These are
   `store.Memory.UseCount` and `MissCount` (`/home/santosh/af-home/internal/store/memory.go:134-135`),
   populated by `queryMemories` (:948), written by v3's engine
   (`/home/santosh/af-home/internal/session/memory.go:515`), pinned by
   `store/memory_test.go:415`, and **already in `p.all`**. `UseCount` is drawn as a `*` (:206) and
   as `used · N` (:177); `MissCount` is read by nothing. **No new seam — wording and layout only.**
2. **`e` and `f` as list-level verbs.** `e` exists one layer in (:303); `f` is unbound.
3. **The `4 held · 1 let go` line** and the three-sentence teaching paragraph. Nothing exists.
4. **The struck let-go row.** `ListMemories` filters `WHERE status = MemoryActive`
   (`store/memory.go:553-556`) and `p.remove` drops the row outright (:115-123). **Needs a new
   listing method on `MemoryStore`.**
5. **Scope grouping instead of scope cycling**, and promotion from a 12-row overlay to a page.

**2d is roughly 5% built, and it is not the same object.** 2d's shelves are the `store.Fact`
scope ladder, not `store.Memory`'s three scopes; its kinds (`quirk`, `lesson`, `skill`,
`playbook`, `question`, `trait`, `preference`) are `store.FactKind`
(`/home/santosh/af-home/internal/store/facts.go:48-76`), a different table from
`store.MemoryFact/Preference/Decision/Correction/ProjectState` (`store/memory.go:70-76`); its
`rode 19, held up 18` is `FactOutcome{Rides, Bad}` (`facts.go:866-871`, `FactOutcomes()` :876);
its retracted rows are `RetractedFacts` (:1169) and `CorrectionFacts` (:1151); its unsettled
pairs are `UnsettledPair` (:99); its open questions and taste candidates are `TasteRules` (:990);
its shelf aliasing is `AliasScope` (:383) / `ResolveScope` (:412). **`MemoryStore`
(memorypanel.go:22-28) exposes five memory methods and zero fact methods**, and
`Options.Memory` (`tui3.go:250-252`) is the only door. And the tidy receipt 2d is built around —
`alt+t only what was tidied` (`SCREENS.txt:182`) — lives at
`/home/santosh/af-home/internal/session/memory_consolidate.go` (`NewMemoryTidy` :204,
`applyConsolidatePlan` :441, `consolidateNoticeLine` :562) whose only person-facing exit is a
**notice** plus a mark file (:634-659). **Nothing on that path is exported to tui3.**

So: **1f is a wave. 2d is a project**, and it needs `internal/store`'s facts table brought across
a seam that does not exist. The honest first cut of the memory place is 1f.

---

### 1.7 the other overlays

| Overlay | Struct | Opened by | Closed by | Draws |
|---|---|---|---|---|
| permissions | `permPanel` — `permissions.go:136` | `/permissions` → `openPermissions()` `permissions.go:265` (`app.go:5030`) | `esc` (`input.go:388`) | shell + tool approval rules under `permHeading = "what runs without asking"` (:69); **`d` then enter twice drops a rule** (`armed` :143); rows from `permissionRules()` :288 |
| connections | `connectPanel` — `connectpanel.go:103` | `/connect` → `openConnect()` :463 (`app.go:5021`) | `esc`, layered | held accounts, then one section per catalog category (`groups` :113, `heads` :131); filter :141; key-entry box :146; `armed` :151 |
| harnesses | `harnessPanel` — `harnesspanel.go:74` | `/harness`, `/harnesses` → `openHarness()` :209 (`app.go:5057`); also `taskstrip.go:503`, `harnesspick.go:466` | `esc` | one row per registered harness (`harnessRow` :62) |
| deliverables | `shelf` — `deliverables.go:206` | `/files` → `openFiles()` :417 (`app.go:4962`) | `esc`, layered :470-484 | files newest-first, filtered on title only (:212); a destination box `dest *copyTo` :227 |
| crew picker | `crewPicker` — `crew.go:183` | bare `/crew` → `runCrew("")` :32 (`app.go:5092`) | `esc`/`enter` :332-346 | the three `config.CrewPresets`; **no filter**; cursor wraps :202 |
| model picker | `picker` — `palette.go:33` | `/model` (`app.go:4971`), the status-line model word, and **reused inside settings** as `sheetSelect.pick` (`settings.go:759`) | `esc` (`input.go:328`) | ranked model rows, filter box, `pickerHint` `palette.go:805` |

`closeLists()` (`app.go:5993`) only closes `menu`, `comp`, `harnPick`. The heavier overlays are
cleared by `closeForSwitch()` (`switcher.go:312-335`).

---

### 1.8 What has no surface at all today

- **spend** — there is no spend page and no spend surface in `internal/tui3`. The only spend
  surfaces are the `spend` band (`homeband_spend.go:6`), the pulse's `$X today` segment
  (`pulse.go:109`), the status line, `/cost` (aliases `usage`, `tokens`, **`spend`** —
  `commands.go:237`) and `/status`.
  **`internal/tui2/homes/spend.go` is not a spend page** — it is tui2's money-segment inline
  editor (`type Spend` :75, `Render` :309, `Key` :188), on the older, non-live surface
  (CLAUDE.md:12). SCREENS.txt cites it repeatedly (`:13`, `:156`) as if it were the precedent; it
  is a precedent for *editing the rail on the segment that shows it*, not for a page.
- **search** — there is no cross-conversation index. Home's search is `homeRank`
  (`home.go:1682`) over `session.MatchQuality`
  (`/home/santosh/af-home/internal/session/task_index.go:880`), and the no-index decision is
  stated at `home.go:1625-1643`. `internal/search` is **web** search. A `search` place would be
  home's own ranker given a page.

### 1.9 A precedent worth reading before designing the router

`/home/santosh/af-home/internal/tui2/homes/` is a package that solved this exact problem once, on
the older surface. Its doc states the conclusion it reached:

> *"The old chat proved it: `self` was a page with a page-local key grammar, **a place enum**, its
> own focus zone and its own esc ladder; services were a second page beside it; the notebook was
> an overlay. Four surfaces, four sets of keys, four ways to be lost."* —
> `/home/santosh/af-home/internal/tui2/homes/doc.go:9-12`
>
> *"Here every one of them is the SAME object the rail already draws."* — `doc.go:14`

And `route.go` is the shape an enum wants: `RouteID` :32, `Routes()` :57, `Valid()` :65,
`Word()` :70, `String()` :93, `RowID()` :102, `ParseRowID()` :110, `Explain()` :123, `Empty()`
:148, and — directly relevant to the tab-bar counts — `Counted()` :173 (*"Practice and Dials are
states rather than collections — a number in front of them would be a number about nothing"*) with
`Route{ID, HasCount}` :185-192.

Read together: **an enum is fine; a per-place key grammar is not.** That is the constraint §4
designs to.

---

## 2. THE FRAME

### 2.1 `app.View()` does no dispatch

`View()` — `/home/santosh/af-home/internal/tui3/view.go:158` — is a declaration wrapper. Line
159 calls `a.frame()`; the rest sets `AltScreen` (:161), mouse mode (:177), focus reporting
(:185), bracketed paste (:190), the window title (:195) and the cursor (:199-204).

### 2.2 `app.frame()` — `view.go:210` — the real dispatch, in order

| # | Line | Condition | Draws |
|---|---|---|---|
| 0 | :211, :215 | `width, height := a.size()`; `a.caret = true` | — |
| 1 | :220 | `if a.setup.open` | `setupFrame` (firstrun.go) |
| 2 | :228 | `if a.sheet.open` | `sheetFrame` (settings.go:1835) |
| 3 | :236 | `if a.taskSheet.open` | `taskSheetFrame` (taskview.go:1157) |
| 4 | :244 | `if a.home.open` | `homeFrame` (home.go:3276) |
| 5 | :253 | `if a.rewSheet.open` | `rewindSheetFrame` (rewindsheet.go:832) |
| 5b | :271 | `if a.deck.open && layoutTier(width) != tierPhone` | **not a return** — resets `a.deck` (:275) |
| 6 | :277 | `if a.deck.open` | `deckSheetFrame` (statusdeck.go) |
| 7 | :291-292 | `if a.expandShowing()` and the frame is non-empty | `expandFrame` (expand.go) |
| 8 | :296 | — | `chrome, chromeMarks, caretX, caretRow := a.chrome(width)` |
| 9 | :301-303 | — | welcome box split off the head of chrome |
| 10 | :309 | — | `head := a.roomHead(width)` — the frame's one pinned row |
| 11 | :317 | — | `strip := a.stripRows(width)` — the task strip |
| 12 | :343 | `if a.railFull()` | roster takes the body, then `frameOut` (:351) |
| 13 | :353-410 | — | body + rail + welcome-into-slack, then `frameOut` (:410) |

The order is written down in prose at `view.go:257-270` — *"an invariant that is only true while
nobody makes a mistake is an invariant that draws a blank frame the day somebody does."*
**Overlays are not in this chain at all**; they are composited inside `a.chrome(width)`
(`view.go:468`), in the order: breathing gap (:469) → jump chip (:489) → welcome unit (:509) →
**legend/rule (:523)** → consent (:530) → connect (:539) → harness (:546) → room approval (:555)
→ guard (:563) → follow row (:566) → parked (:575) → gap (:578) → **input block (:587)** →
spellout (:614) → **the open list, `overlayRows` (:617)** → **status row (:620)**.

### 2.3 The pulse is home's alone

`pulseLine(width, pal)` — `/home/santosh/af-home/internal/tui3/pulse.go:77`; segments
`pulseSegments` :109. **Exactly one non-test caller: `home.go:3318`**, the first row of
`homeFrame`. `homePhoneFrame` does not call it either. Every other hit is a test
(`homeband_machine_test.go:232, :248, :320, :321, :433`; `homeband_spark_test.go:40, :56, :267,
:364`).

This is the single biggest structural fact for the router: **the pulse line and the tab bar under
it are, today, home's private head.** Every other page draws its own title row instead — settings
`sheetTitle` (`settings.go:1933`), the task page `taskSheetTitle` (`taskview.go:1358`), rewind
`rewindSheetTitle` (`rewindsheet.go:895`).

### 2.4 The width ladder

`layoutTier(width)` — `view.go:1132`; `tier` type :1119; constants :1122-1125: `tierWide ≥120`,
`tierStandard ≥80`, `tierNarrow ≥60`, else `tierPhone`. Called from ~30 sites including
`home.go:3287`, `taskview.go:1112`, `palette.go:473`. `rewindsheet.go` calls it **nowhere**.

### 2.5 The composer today, and which one is the better seed

**Both boxes are the same type.** `editor` — `/home/santosh/af-home/internal/tui3/input.go:30`
(`value []rune` :31, `cursor int` :32, `demotedTags []segment` :36), methods :39-227. The chat's
instance is `a.input editor` (`app.go:985`); home's is `homeView.box editor` (`home.go:528`); so
are `a.sheet.query`, `a.taskSheet.query`, `a.rewSheet.query`, `a.memPanel.filter`, `a.pick.filter`,
`a.roster.filter`, `a.shelf.filter`, `a.connPanel.filter`, `homeExchange.box`.

**Both are drawn by the same function.** `draftBlock(e, pal, width, maxRows, hint, lead)` —
`input.go:1198`, delegating to `draftBlockWithTags` :1202; it returns the rows, the caret column
and the caret row. The chat calls it at `input.go:1147` with `draftRows = 6` (:19); home calls it
at `home.go:3345` with `homeDraftRows = 3` (`home.go:231`) and for a focused exchange at
`home.go:3341`; the welcome box calls it at `welcome.go:649`.

**There is no scope chip anywhere on the conversation frame.** `legendLeft`
(`render.go:2615`) returns host and branch only (:2638), and the comment at
`render.go:2661-2663` says why: *"IT IS NO LONGER ON THE LEGEND. The path is on the status
sheet's 'place' row and in /status."* The two existing "where you are" rows are
`statusdeck.go:472` (`add("place", dotted(a.placePath(0), a.branchWord()), …)`, **phone tier
only**) and the `/status` note (`statusnote.go:81, :89`). The helpers a chip would use already
exist: `a.placePath(hard)` — `render.go:2672` = `hostedPath(placeWord(shortPath(a.workspace,
a.tilde, hard)))` — and `a.branchWord()` — `render.go:2591`.

**What the chat's box carries that home's does not** (each a thing a global composer would have to
answer for on six other places): the submit road `enter` → `enterLine` (`input.go:939`, :946);
slash dispatch (:990-995 → `app.go:4888`); send-door tags (`slashchip.go:151`, :168); slash chip
painting (:1255); attachments and the tray (`app.go:1282`, `attach.go:406`, :635, :792, :835);
image paste (`imagepaste.go:56`, :135); bracketed paste (`app.go:2004`, :5728, :5765); history
recall (`recall.go:45`, :49, :79, :96, :118, :138); draft persistence (`draft.go:141`, :177);
typed overlays (`app.go:5887`, `palette.go:1149`, :1096); tab path completion (`input.go:1051`,
:1058); multiline (`input.go:605`); parking (`park.go:78`); the standing chord
(`standmark.go:241`); barge-in (`bargein.go`); follow-up (`followup.go`); spellout
(`spellout.go:417`); click-to-caret (`draftclick.go:30`, marks written `view.go:607`); room
steering (`room.go:3159`, :3082); the redirect lane (`task.go:5330`); the rewind bar standing in
the box (`rewind.go:650`); picked-harness send (`input.go:1016`); `@task` expansion
(`taskmention.go`); and the geometry contract (`input.go:1163`, `view.go:741-743`, :670).

**What home's box carries**: insert (`home.go:2325`), backspace (:2203), `ctrl+u` as a whole reset
(:2207), `ctrl+w` (:2211), `left`/`right` (:2310, :2264), `ctrl+b`/`ctrl+f` (:2314, :2317), raw
paste (`app.go:5846-5855`), a live rebuild after every edit, and two submit roads —
`homeStart` (`home.go:2749`, which ends at :2772-2774 by calling **the chat's own `a.submit`**)
and `askHere` (`homeexchange.go:805`). It has **no** recall, attachments, palette, completion,
parking, persistence, click-to-caret, spellout, multiline or mentions.

**Verdict for §4**: home's `box editor` is the seed. It already draws through `draftBlock`, already
reaches `app.submit`, already carries the two-reading law (filter *and* compose at once,
`home.go:528`), and it costs three keys to bring to parity for a composer (`alt+enter`/`ctrl+j`
newline, word jump, `home`/`end`). Lifting the chat's box instead would drag twenty
conversation-shaped dependencies onto pages that have no conversation.

### 2.6 `bottomchrome_test.go` — what the foot already promises

`TestTheBreathingGapStepsDownWithTheWindow` :60 · `TestTheJumpChipOnlyShowsWhileTheReaderHasScrolledAway`
:126 · `TestTheJumpChipFallsBackToTheGapAboveTheDraft` :169 ·
`TestPressingTheJumpChipReturnsToTheLiveEdge` :191 ·
`TestTheJumpKeyReturnsToTheLiveEdgeWithADraftInTheBox` :222 ·
`TestTheJumpChipStaysOffInCopyMode` :240 ·
`TestTheJumpChipBrightensUnderThePointerAndNowhereElse` :262 ·
**`TestTheDraftBlockAnchorsAtTheTop` :290** (five caret positions against `draftBlock`'s window
law) · **`TestTheCaretLandsOnTheDraftsFirstRowWhenTheDraftIsLong` :362** (the whole-frame
contract: `frameOut`'s counting back through the chrome agrees with `draftBlock`'s reported row).

A global composer inherits both of the last two, on every place.

---

## 3. KEY ROUTING

### 3.1 How a key reaches a page

`Update`'s `tea.KeyPressMsg` arm — `/home/santosh/af-home/internal/tui3/app.go:1912-1958`:

1. `app.go:1923` — `if msg.String() != "ctrl+c" { a.disarmQuit() }`. The only unconditional line.
2. `app.go:1935` — `pasteKey` (`app.go:5728`); inside an open bracket it swallows everything but
   `ctrl+c` (:5732), turning `enter`/`ctrl+j` into `\n` (:5747) and `tab` into `\t` (:5750).
3. `app.go:1944` — `stopKey` (`stop.go:330`).
4. `app.go:1951` — `railKey` (`task.go:3442`), which refuses under every fullscreen page
   (`task.go:3444-3448`).
5. `app.go:1957` — `roomKey` (`room.go:1700`).
6. `app.go:1967` — everything else into `a.key(msg)` (`input.go:234`).

`a.key` is a ~30-rung ladder. The rungs that matter:

| input.go | guard | handler |
|---|---|---|
| :297 | `a.sheet.open && != ctrl+c` | `sheetKey` (settings.go:1479) |
| :306 | `a.home.open && != ctrl+c` | **`homeKey` (home.go:1958)** |
| :313 / :323 | phone deck / expand | their own |
| :331–:387 | `pick`, `crewPick`, `effPick`, **`memPanel` :347**, `roster`, `shelf`, `connPanel`, `harnPanel`, `permPanel` | their own |
| :395 | `a.standPage.open && != ctrl+c` | **`standPageKey` (standingpage.go:534)** |
| :410 | `subPage` | `subPageKey` |
| **:414** | `msg.String() == "ctrl+c"` | interrupt / arm / quit |
| :447 | — | `rewindSheetKey` (rewindsheet.go:664) |
| :457 | — | **`taskSheetKeyPress` (taskview.go:844)** — carries both the in-page keys *and* the `ctrl+.` that opens it |
| :465 / :474 / :482 | copy mode / rewind mode / welcome | their own |
| :511 | `msg.String() == "tab"` | path completion, else last conversation |
| :537 / :545 / :549 | typed lists / spellout / **the plain switch** | |
| :917 / :921 | `homeGesture` / text insert | |

There is **no global key table**. `ctrl+,` is at `input.go:674`, inside the plain switch, so it is
unreachable from home, the task page or any modal. `ctrl+.` is at `input.go:457`, below `ctrl+c`.

### 3.2 `esc`, per surface

chat `input.go:550` (recall cancel → arm rewind → interrupt → send parked) · home `home.go:2050`
(clear box, else close) · home exchange `homeexchange.go:1036` · settings `settings.go:1480`
(search → conn → close) · settings text submenu :1668 · settings model submenu :1712 · task page
`taskview.go:877` (filter → close), card mode `taskrecord.go:243` (`"esc", "left"`) · standing
page `standingpage.go:539` (straight out — there is no layer) · rewind sheet `rewindsheet.go:670`
· rewind mode `rewind.go:324` · rail `task.go:3488` · copy mode `copymode.go:208` · typed lists
`app.go:5919` · picker `palette.go:990` · phone deck `statusdeck.go:683` · phone expand
`expand.go:277`.

The shared shape — **one layer at a time, and the way out is the last clause of every hint** —
is stated at `home.go:2051-2054` and `homebridge.go:548-551`.

### 3.3 `tab` is the most contested key on the surface

| surface | line | today |
|---|---|---|
| chat | `input.go:511` | path completion; if nothing to complete and the box is empty, **the last conversation** (`lastDoorWord = "tab last"`, `render.go:2720`) |
| home | `home.go:2032` | at the columns tier, cycle zones (`homeTab`, `homebridge.go:493`, word `homeTabWord = "tab next zone"` :392); else focus the pane's exchange |
| home exchange | `homeexchange.go:1029` | hand the keyboard back to the list |
| settings | `settings.go:1500` | next tab (`left`/`shift+tab` = previous, :1498) |
| memory panel | `memorypanel.go:338` | cycle scope |
| inline rewind | `rewind.go:328` (`rewindSheetLiftKey = "tab"`, `rewindsheet.go:93`) | lift into the timeline |
| rail | `task.go:3521` | eaten deliberately |
| welcome box | `welcome.go:228` | **the one key that does not dismiss it** |
| task page | `taskview.go:937-941` | swallowed |
| standing page | `standingpage.go:557` | swallowed |
| rewind sheet | `rewindsheet.go:711` | swallowed |
| paste bracket | `app.go:5750` | a literal `\t` |

The manual already states the arbitration rule verbatim
(`/home/santosh/af-home/internal/manual/chat/keys.md:992-997`): *"**Everything else that wants
`tab` gets it first**, and that is the whole rule rather than a claim that `tab` is free. In
order: a paste bracket …; the task roster …; the settings panel changes page with it; the memory
panel changes scope; the rewind timeline and the inline rewind lift with it; and path completion
takes it over `/image ` or `/export `. Only when none of those is claiming it, and the box is
empty, is it the way back."*

**So `tab` = next place is an extension of an existing ordered chain, not a reversal of it** — it
becomes the last claimant, below `tab last`, or it replaces `tab last` outright. Either way the
manual edit is a list edit.

### 3.4 `alt+enter` today

Exactly two sites. **chat: `input.go:605`** — `case "alt+enter", "ctrl+j":` inserts `"\n"`.
**home: `home.go:2083`** — `case "ctrl+enter", "alt+enter":` calls `askHere`. Note the neighbour:
`ctrl+enter` alone is `standMarkKey` (`standmark.go:51`) in the chat (`input.go:583`), so in the
chat `alt+enter ≠ ctrl+enter`, while on home they are synonyms (comment `home.go:2084-2092`:
`ctrl+enter` only arrives under the kitty protocol).

SCREEN 3a assigns `alt+enter` = *send what you typed off as a task* (`SCREENS.txt:23`). On home
that is `askHere` renamed; in the chat it takes the newline key away, and the newline has to move
to `ctrl+j` alone — which is a documented, breaking change (`keys.md:3`,
`home.md:878-879`).

### 3.5 The new binding classes, spelled the way the library spells them

**The library.** `/home/santosh/af-home/go.mod:6` — **`charm.land/bubbletea/v2 v2.0.8`**
(`github.com/charmbracelet/bubbletea v1.3.10` at :13 is v1 and belongs to the older surfaces).
Key strings are not bubbletea's: `charm.land/bubbletea/v2@v2.0.8/key.go:351-353` and `:369-371`
delegate to **`github.com/charmbracelet/ultraviolet`** (`go.mod:16`). tui3 matches keys with
`switch msg.String()` everywhere (`home.go:2031`, `input.go:549`, `taskview.go:876`,
`settings.go:1479`, `rewindsheet.go:669`, `app.go:5746`); **`key.Matches` / `key.Binding` is used
nowhere in the package**, and `msg.Key().Text` appears only in `default:` arms.

**The two-stage rule** — `ultraviolet/key.go:391-396`:

```go
func (k Key) String() string {
	if len(k.Text) > 0 && k.Text != " " { return k.Text }
	return k.Keystroke()
}
```

**`Keystroke()` prefixes, in a fixed order** — `ultraviolet/key.go:412-431`: `ctrl+`, `alt+`,
`shift+`, `meta+`, `hyper+`, `super+`. Base names at :459-469 — note `KeyEscape` → **`"esc"`**.

**A modified key carries no text**, so every chord goes through `Keystroke()`. For the ESC-prefix
path the decoder clears it explicitly — `ultraviolet/decoder.go:262-267`:

```go
n, e := p.Decode(buf[1:])
if k, ok := e.(KeyPressEvent); ok {
	k.Text = ""
	k.Mod |= ModAlt
	return n + 1, k
}
```

(also `decoder.go:304`, `:765`, `:869`, `:1061`; and the kitty path at `decoder.go:1443-1449`
clears text for any modifier above `ModShift`).

**The real strings:**

| gesture | `msg.String()` | source |
|---|---|---|
| alt+1 … alt+7 | `"alt+1"` … `"alt+7"` | decoder.go:264-265, key.go:418, :452 |
| alt+g (any letter) | `"alt+g"` | decoder.go:262-267 |
| option+shift+G | `"alt+shift+g"` | decoder.go:702, :867, :944 |
| shift+left / right / up / down | `"shift+left"` … | key_table.go:297, key.go:421, :465-468 |
| alt+shift+left | `"alt+shift+left"` | key_table.go:298 |
| alt+enter | `"alt+enter"` | decoder.go:264 over `KeyEnter` |
| ctrl+enter | `"ctrl+enter"` | **kitty/win32 only** — legacy terminals send bare CR |
| tab / shift+tab / esc / right | `"tab"` / `"shift+tab"` / `"esc"` / `"right"` | key.go:461, :421, :463, :468 |

Confirmed by ultraviolet's own tests: `key_test.go:1437` `"shift+left"`, `:1446`
`"ctrl+shift+left"`, `:1466` `"ctrl+alt+shift+left"`.

**A caveat with teeth**: `String()` returns `Text` first, so **shift+`<printable>` is `"A"`, not
`"shift+a"`** — a shift+letter binding is not addressable. Shift+arrow and shift+tab are, because
arrows and tab carry no text.

**And one about capability**: the app records `tea.KeyboardEnhancementsMsg` (`app.go:1980`) and
**requests nothing** (:1985-1991). So `ctrl+enter`, `super+*` and `ctrl+backspace` arrive only on
kitty-protocol terminals; **alt+digit, alt+letter and shift+arrow arrive everywhere.**

**What is already claimed.** `alt+` bindings in non-test tui3 are exactly seven sites:
`home.go:2083`, `input.go:605`, `:798`, `:820`, `:835`, `settings.go:1686`, `palette.go:1069`
(plus the classifier `payload.go:272` and help text `commands.go:758`). `shift+`-modified
bindings are exactly two: `settings.go:1498` (`shift+tab`) and `bargeKey = "shift+enter"`
(`bargein.go:98`, consumed `input.go:592`). **`alt+<digit>` and `shift+<arrow>` are entirely
unclaimed in tui3, test files included.**

**The readline map a new class must not collide with** (`input.go`): `ctrl+w` / `alt+backspace` /
`ctrl+backspace` :798 · `alt+left` / `alt+b` / `ctrl+left` :820 · `alt+right` / `alt+f` /
`ctrl+right` :835 · `super+left` / `super+right` / `meta+left` / `meta+right` :842 · `ctrl+u` /
`super+backspace` :786 · `home` / `ctrl+a` :892 · `end` / `ctrl+e` :896 · `ctrl+b` **enter copy
mode, deliberately not backward-char** :657-662 · `ctrl+f` caret right only :885 · `ctrl+j` /
`alt+enter` newline :605 · `ctrl+v` = effort (`effortKey`, `effortchip.go:73`) :680 · `ctrl+l` =
jump (`jumpKey`, `jumpchip.go:43`) :701 · `ctrl+s` = release mouse (`selectKey`,
`copymode.go:96`) :666 · `ctrl+,` settings :674 · `ctrl+o` :619 · `ctrl+q` follow-up :614 ·
`ctrl+g` background :647. The same map is mirrored in `palette.go:1041-1082` (`listNavigate`)
and `settings.go:1664-1697` (`sheetEditKey`).

**Conclusion on classes:** `alt+1..7` and `shift+←→↑↓` are free, portable and cheap to spell.
`alt+<letter>` collides with `alt+b` and `alt+f` (word jump, `input.go:820`, :835) — which are the
two spellings the manual says *"work nearly everywhere"* (`keys.md:389-390`) — so an `alt+letter`
class must reserve `b` and `f`, or accept that the two collide only inside a text box and route
by context.

**And the real blocker is not a collision.** `manual_test.go:48-52` bans the literal strings
`"alt+1"`, `"alt+2"`, `"alt+3"` from the chat corpus as resident vocabulary, and the manual law
(CLAUDE.md:48) requires every new key to be documented. See §5.3.

### 3.6 Threading a new class without breaking the ladder

The structural fact that decides this: **a case added to `input.go`'s plain switch (`:549`) is
invisible to every page**, because home returns at `:306`, settings at `:297` and the standing
page at `:395`, and the task page and rewind sheet swallow everything by design
(`taskview.go:936`, `rewindsheet.go:718`). A cross-place chord has exactly two homes:

1. **`Update`'s pre-dispatch** (`app.go:1912-1958`), above `railKey` — global, and therefore
   above every modal that currently excepts only `ctrl+c`. Cheap to write, but it makes the chord
   fire inside a paste bracket, inside consent, inside first-run.
2. **One shared function called as the first line of each page's key handler** — five call sites,
   each of which keeps its right of first refusal.

(2) is the one that preserves the existing laws. Crucially, it also survives taskview's *"every
printable key is the filter"* rule (`taskview.go:924-932`) untouched, because `alt+`/`shift+`
chords carry no `.Text` and never reach the `default:` arm.

---

## 4. ROUTER PROPOSAL

### 4.1 The choice

**Recommended: a `page` enum plus a shared `placeFrame`, with the existing structs kept as the
places' bodies behind adapters. Not five rewrites, and not a sixth surface.**

The alternative — keep the five structs and their five open/close paths, adding a tab bar to each
— is what `internal/tui2/homes/doc.go:9-12` already reports failing: *"Four surfaces, four sets of
keys, four ways to be lost."* The alternative in the other direction — one new `placeView` struct
that absorbs home, tasks, standing and memory — throws away ~1,940 test functions' worth of pinned
behaviour and the whole band registry.

**A naming caveat that must be settled first.** `place` / `places` is already taken in this
codebase and means something else: `session.PlacesRoot()`, `a.placesRoot()` (`home.go:928`),
`placesDirName`, `SweepPlaces`, `homePlacesCol` (`homebridge.go:98`), `placesFrom()` /
`placesTop()` (`homebridge.go:217`, :233) — 138 non-test occurrences, all meaning *the on-disk
directory of conversations and projects*. And `room` is taken too (`room.go`, `roomHead`,
`roomKinRows`, ~80 identifiers), meaning *a task's room*. **Use `page` for the Go identifier** —
lowercase `page`/`paged`/`pages`/`standingpage` are the only collisions — and keep **"place" as
the person-facing word** in the manual and on screen. Two vocabularies is the smaller cost.

### 4.2 The enum

A new file `internal/tui3/pages.go`, modelled directly on
`/home/santosh/af-home/internal/tui2/homes/route.go:32-192`:

```
type pageID uint8            // pageHome, pageTasks, pageStanding, pageMemory,
                             // pageSpend, pageSearch, pageSettings
func pages() []pageID        // the tab bar's order, and alt+1..7's index
func (p pageID) Word() string      // the tab word — one lowercase word
func (p pageID) Explain() string   // the almost-empty page's teaching sentence (SCREENS.txt:8)
func (p pageID) Counted() bool     // does a number in front of it mean anything
func parsePageWord(s string) (pageID, bool)  // the composer offering places (SCREEN 1g)
```

One field on the app: `a.page pageID`. The rewind timeline is deliberately **not** a place — it is
a page reached by `/rewind`, exactly as today.

### 4.3 What `standDownFullscreen` becomes

It does **not** go away. It becomes the private half of a new `a.showPage(id pageID)`:

```
showPage(id):  standDownFullscreen()      // unchanged body, settings.go:934-950
               a.page = id
               open the one place's own state   (openHome / openTaskSheet / …)
```

Keeping `standDownFullscreen` and keeping every page's `open bool` is what makes
`chrome_test.go:1261` and `:1305` pass **verbatim** (§4.7). The router is a wrapper over the
existing exclusion, not a replacement for it.

Two changes inside it, both deliberate:

- The standing page joins the law (it must, once it takes the frame): a fifth clause and a
  `closeStandPage` guard.
- **`closeHome`'s look stamp has to move.** Today `closeHome` writes
  `session.NoteLook(a.placesRoot(), a.now())` (`home.go:911`) — *"closing is the look."* Under a
  router, `tab`bing from home to tasks is not leaving; it is walking two rooms. The stamp belongs
  on the transition *out of the switcher entirely* (back to the conversation, or on quit), not on
  every place change — otherwise the tab-bar counts (§4.5) reset themselves the moment you look
  at anything.

### 4.4 `homeFrame` → `placeFrame`

`homeFrame` (`home.go:3276`) is already the shape the mockups draw. Generalised, it is:

```
placeFrame(width, height, body func(width, room int) []drawnRow) (lines, hits, caretX, caretY)

  row 0        pulseLine(width, pal)                       pulse.go:77  — unchanged
  row 1        ""
  row 2        tabBar(width, pages(), a.page, counts, pal) — lifted from settings.go:2010
  row 3        pal.dim(rule(width))                        home.go:3320
  row 4        ""
  ...          body(left/right, room)                      the place's own rows
  ...          ""                                          home.go:3376
  ...          pal.dim(rule(width))                        home.go:3378
  ...          the composer                                draftBlock, home.go:3345
  ...          the verb strip / answer strip               answerstrip.go:18
  ...          msg or hint                                 home.go:3418/3423
  tail-clamp   keep row 0 and the last height-1            home.go:3429-3450, caret rides it
```

Three of those rows are lifts, not new code:

- **The tab bar is `sheetTabBar`** (`settings.go:2010`) with the titles passed in. It already
  spends exactly one accent on a band (`pal.selected(pal.bold(pal.accent(chip)), …)` :2019),
  already pads its chips so the padding is clickable (`chip_test.go:162`), already `fit()`-cuts
  rather than wrapping (:2025), and its own comment already claims the job:
  *"this panel IS a tab bar — the same object the task strip is, drawn the same way, so that
  'which page am I on' is one visual question across the app rather than two"* (:2002-2005).
  Constants `tabGap` :1965, `tabPad` :1969, `tabPadCols` :1970, `tabLead` :1973, spans
  `tabSpans()` :1976, hit resolution `tabAtColumn()` :1987 — all reusable.
- **The verb strip is `answerStrip`** (`answerstrip.go:18`) generalised. Its existing law is
  precisely SCREEN 3c's: *"IT IS AN ANSWER, NOT A MIRROR: it draws only when the cursor's row
  carries a question the person can answer from here"* (:10-13), and the digit keys that act on it
  are earned by the chips being drawn (`home.go:2019-2025`). A `→` strip is the same object with
  the trigger moved from "the row has a question" to "the person pressed `→`", and with the rows
  displaced by its height — which `answerStrip` already does, since its height is counted into
  the foot budget at `home.go:3360-3361`.
- **The composer is `draftBlock`** (`input.go:1198`) over each place's own `editor`, at
  `homeDraftRows = 3` — but see §4.6.

The pulse itself does not change. `pulseSegments` (`pulse.go:109`) reads
`a.machineFactsAt(now)` (`homemachine.go:107`), which is cached for `homeEvery` and is *"one
reader, two surfaces"* (:264) — it is already a machine-wide reading, not a home-specific one.

### 4.5 Adapters — how each place becomes a body

Each existing frame function is split at its own body loop; the head and foot it currently writes
by hand are dropped in favour of `placeFrame`'s.

| place | body comes from | head/foot dropped | real work |
|---|---|---|---|
| home | `homeBody` (`home.go:3509`) | none — it already is `placeFrame` | insert the tab bar row; move the look stamp |
| tasks | `taskSheetFrame`'s rows :1209-1228 | `taskSheetTitle` :1358, the tally :1246, `taskSheetKeysLine` :1257 | **group by state** (§1.3), a header sentence with money, a cost column, a fold line, a time window |
| standing | `standPage.draw` :457 | `standHeading` :466 | promote from overlay to page, join the exclusion law, add the rope column, a cost column, a header sentence |
| memory | `memoryPanel.rows` :156 | the footer :214-221 | promote from overlay to page; grouping instead of cycling; `UseCount`/`MissCount` prose; a `held / let go` line |
| settings | `sheetFrame`'s body :1869-1894 | `sheetTitle` :1933, its **own** tab bar :1848 | the inner tabs become a second row, or a `→` fold under the place bar. **Otherwise nothing.** |
| spend | — | — | **entirely new.** No surface exists (§1.8) |
| search | — | — | **entirely new**; home's `homeRank` (`home.go:1682`) given a page |

Costed honestly: **settings is free, home is nearly free, tasks and standing are one wave each,
memory-as-1f is one wave, memory-as-2d and spend and search are new work.**

### 4.6 The composer, and the scope chip

Use **home's `box editor`** as the global composer (§2.5), at `homeDraftRows = 3`, drawn by
`draftBlock` in `placeFrame`'s foot. It gains three things from the chat's box and nothing else:
`alt+enter`/`ctrl+j` newline (`input.go:605`), word jump (`input.go:820`, :835), and
`home`/`end` (`input.go:892`, :896).

The two-reading law comes free and is the whole of SCREEN 1g: home's box already *filters the
machine and composes a first message at once, with no mode* (`home.go:528`), and the settings
sheet already *searches across every tab and moves the tab to the first match*
(`settings.go:714-717`) — which is exactly "typing offers places too, and going to one leaves you
there". Two independent implementations of the mockup's behaviour already ship.

**The scope chip is derived, not stored.** Three existing resolvers, in order:

1. On home, the cursor's own row: `homeWhere(line)`, already used by `ctrl+t` to decide where a
   fresh conversation goes (`home.go:2113-2125`).
2. Otherwise this window's workspace: `a.placePath(hard)` (`render.go:2672`) plus
   `a.branchWord()` (`render.go:2591`) — the same pair the phone status sheet already prints as
   its `place` row (`statusdeck.go:472`).
3. A typed path or project name overrides both: `typedPlace` (`home.go:2788`).

So `here ~/aforge-v2` is `"here " + a.placePath(0)`, and the mockup's `alt+w somewhere else`
(`SCREENS.txt:52`) is `ctrl+t`'s target resolution given a key.

### 4.7 What `chrome_test.go`'s page-stack tests demand

**`TestOpeningOneFullscreenPageClosesTheOtherTwo` — `chrome_test.go:1261`.** Setup
`threePageApp` :1242 (a profile, a task in the record, a second conversation). Two parallel maps
of three pages, then **every ordered pair** (6 subtests, fresh app each) asserting four things:
the first opened; the second opened over it; the first is **not** open; and no third is up. It
asserts **only** on `a.sheet.open`, `a.taskSheet.open`, `a.home.open` — never on rendered text —
and it calls `openSettings()` / `openTaskSheet()` / `openHome()` **directly**, not through keys.

**`TestTheFrameDrawsThePageThatWasOpenedLast` — `chrome_test.go:1305`.** One app, three steps,
each through `a.frame()` and `plain(...)`: after `openSettings()` the frame contains
`tabSession` (= `"Session"`, `settings.go:69`); after `openHome()` it does **not** contain
`tabProviders` (= `"Providers"`, :78) but **does** contain the second session's title; after
`openTaskSheet()` (whose `bool` is asserted) it no longer contains the session title but does
contain the seeded task title.

**How the router keeps both true, unchanged.** Keep every page's `open bool`. Keep
`standDownFullscreen` closing them. Keep `frame()`'s `if X.open` chain in the same order. The
`a.page` field is then a *label* on the state the booleans already carry, and both tests pass
without an edit.

The one hazard: `:1305` asserts a **negative** on the string `"Providers"`. A place tab bar drawn
on *every* place must therefore not spell any settings tab word — the seven place words are
`home tasks standing memory spend search settings`, and `"settings"` lowercase does not match
`tabProviders`. Safe as written; worth a comment so nobody later renames a place to `Providers`.

Two gaps to close while there: the rewind sheet is missing from `:1261` even though
`standDownFullscreen` closes four (`rewindsheet.go:216`, `settings.go:948`), and the standing page
will need adding once it takes the frame.

### 4.8 Files touched

**New**: `internal/tui3/pages.go` (the enum, the tab bar wrapper, `placeFrame`, the shared
place-key function).

**Edited**: `view.go` (frame branches delegate their head/foot) · `input.go` (one rung for the
place keys; `tab`'s last-claimant clause) · `app.go` (`a.page`; the look stamp's new home) ·
`home.go` (`homeFrame` → `placeFrame` + `homeBody`; `closeHome`'s stamp; `tab` gives way) ·
`homebridge.go` (`homeTab` yields `tab`; zones move to another key) · `settings.go`
(`standDownFullscreen` → `showPage`; `sheetTabBar` lifted; `sheetFrame` head) · `taskview.go`
(head/foot; group-by-state; a `→` case) · `standingpage.go` (promoted to a page) ·
`memorypanel.go` (promoted to a page) · `commands.go` (any new command name — **each name and
alias is checked by `manual_test.go:22`**) · `answerstrip.go` (generalised into the verb strip) ·
`pulse.go` (unchanged, called from the new frame).

**Not touched**: `homebands.go` and the 17 band files (the card is home's body and stays there),
`rewindsheet.go` (still a page, not a place), every overlay in §1.7.

### 4.9 Blast radius on the tests

`internal/tui3` holds **1,940 test functions** and takes ~150s. The damage is concentrated, and
it is almost all **head geometry**: a tab bar adds one or two rows above every body, and every
test that asserts an absolute row index or an exact frame shape moves with it.

| Band | Tests | Why |
|---|---|---|
| **Certain to move (row offsets)** | `home_test.go` :240, :271, :767, :884, :1388, :1453, :2119, :2205, :3178, :3206 · `homebridge_test.go` :56, :534 · `taskview_test.go` :57, :151, :208 · `depthfade_test.go` :146, :167, :177, :209 · `rewindsheet_test.go` :56, :250 | one or two new head rows |
| **Certain to move (semantics)** | `homebridge_test.go`'s tab-cycling tests · `keys.md`-pinned hint strings in `home_test.go` · `standingpage_test.go` :470 (`TestTheStandingPageHoldsAtEveryWidth` — it becomes a page) · `memory_test.go` :309, :373 (`tab` no longer cycles scope) | `tab` changes meaning; two overlays become pages |
| **Must be extended** | `chrome_test.go` :1261 (add rewind + standing), :1305 (add the new places) · `manual_test.go` :22 (any new command) | new pages join the law |
| **Should pass unchanged** | the 8 band test files · `homecard_test.go` · `homesection_test.go` · `homehover_test.go` · `homearrows_test.go` · every settings test · every overlay test | bodies and seams do not move |
| **Watch, do not break** | `home_test.go:1646` (`TestHomeStatsAFolderOncePerReadingAndNotPerFrame`) · `home_test.go:2400` (`TestALaunchThatIsNotGreetedNeverWalksTheDiskForTheDoor`) · `inputsmooth_test.go` (PERF.md's "a frame with a four-thousand-line draft costs what a twelve-line draft costs") · `designlanguage_test.go:106` (no escape sequence outside styles.go) | **the router must not make a place read the disk on the frame path** |

**Realistic estimate: 60–120 tests need an updated expected offset, and roughly 10–15 need a real
rewrite** (`tab` semantics, standing-as-page, memory-as-page, the two `chrome_test.go` laws).

**Three perf hazards the router creates and must design around**, because each is a synchronous
disk read that today happens only on an explicit command and would happen on a `tab` press:

1. `openMemory()` — `ListMemories(…, 500)` **plus one `MemoryProvenance` per memory, up to 500,
   on the UI goroutine** (`memorypanel.go:261-272`).
2. `openStandingAt()` — `StandingHere()` → `store.Applicable` + `store.List()`, a directory of
   documents, synchronously (`standingpage.go:517`, `session/standing_orders.go:45, :52`).
3. `refreshElsewhere()` — a directory read plus two files per window, **from inside layout**
   (`taskview.go:393-407`, :371-379).

Home already solved this shape: one reading per 3-second beat, never per frame, with TTL caches
and syscall counters that tests can assert (RECON.md §5, `home.go:2665`). Every place must join
that clock rather than read on entry.

---

## 5. MANUAL

### 5.1 The corpus

`/home/santosh/af-home/internal/manual/chat/` — 34 pages, 21,187 lines, packed to
`internal/manual/chat.pack.gz` by `make build`. The pages this redesign rewrites, by size:
`screen.md` 2211 · `tasks.md` 2178 · `home.md` 2024 · `keys.md` 1724 · `commands.md` 1426 ·
`saved-shapes-of-work.md` 1012 · `how-tasks-run.md` 967 · `models-and-cost.md` 944 ·
`what-i-can-do.md` 799 · `keeping-an-eye.md` 738 · `sessions-and-rewind.md` 675 ·
`permissions.md` 625 · `standing-orders.md` 500 · `what-i-remember.md` 447 ·
`asking-from-home.md` 313 · `empty-screen.md` 133 · `reading-a-task-page.md` 83.

### 5.2 The sections the design falsifies

**A. `tab` means something else, in eight pages.** `keys.md:979` *"Go back to the last
conversation — tab"* and its whole body :981-999 (including the legend quoted verbatim,
`space space home · tab last · / commands`, at :986, and the claimant chain at :992-997);
`keys.md:341`, `:849`; `home.md:302` (legend `↑↓ move · enter open · tab next zone · esc close`),
`:315`, `:319`, `:32`, `:334`, `:389`, `:719`, `:853`, `:941`, `:1016`, `:1029`;
`commands.md:250`, `:761`, `:828`, `:1201` (settings' own `← / shift+tab` and `→ / tab`);
`sessions-and-rewind.md:14`, `:91`, `:169`, `:173`, `:634`; `asking-from-home.md:134` (a
heading), `:23`, `:46-47`, `:139`, `:154`, `:159`, `:170`, `:271`; `screen.md:459`.

**B. Two pages state, as a fact, that there is no key for home and that `alt+` is unreliable.**
`home.md:1328` *"Is there a key for home?"* — :1330-1334: *"**There is no `ctrl+` chord for home**
… and the chords that were left — the `alt+` letters — arrive in some terminals and do nothing at
all in others."* And `keys.md:1003` *"Keys on home, and is there a shortcut for it"* — :1008-1010,
the same sentence. Also `home.md:1224-1225`; `commands.md:804` *"There is no argument form and
**no key chord** — `/home` is the only way in."*

**C. Tasks are documented as living behind a command.** `tasks.md:883-895`: *"`/history`, or
`ctrl+.`, opens a full-screen page … **There is no `/tasks` command.**"*;
`commands.md:1062-1069`: *"It is not `/tasks`, and there is no `/tasks`."*; `tasks.md:723` (the
heading *"Old tasks from previous sessions are not on the column — the `ctrl+. earlier` door"*),
:725, :702, :1248; `keys.md:334`.

**D. Home is documented as a glance you close, not a place you live in.** `home.md:20` *"Home is a
glance you take, not a place you live."*; `home.md:10`, `:302`, `:319`, `:396`, `:871`, `:1086`,
`:1412`; `keeping-an-eye.md:250`.

**E. The composer's contract.** `home.md:873-879`: *"**There is no key to open a new line here**
(that is the chat box's `alt+enter` / `ctrl+j`); on home, `alt+enter` and `ctrl+enter` are `ask
here`."*; `home.md:1068`, `:1138`; `empty-screen.md:41`, `:57`, `:60`.

**F. `→` is the fold key.** `keys.md:1049-1055`, `:1090`, `:1120`, `:1125-1128`; `home.md:321`,
`:389`, `:457`, `:476`, `:612`, `:727`, `:804`, `:971`, `:982`, `:1207`; the `tasks.md` key
tables.

**G. `shift+arrow` appears nowhere in the corpus** — nothing to un-say, and nothing that
documents it. Only `shift+tab` (`commands.md:1201`, `keys.md:906`) and `shift+enter`
(`keys.md:111`, `:322`, `:1590`; `screen.md:255`).

**H. Memory and spend.** Nothing denies a memory page; the panel is documented across
`what-i-remember.md` (whole page), `commands.md:187-191`, `:655`, `what-i-can-do.md:606`, `:795`,
and `keys.md:378`, `:418`, `:995` (*"the memory panel changes scope"* — a tab claim the switcher
overrides). Nothing denies a spend page either; spend appears in 26 of 34 pages, load-bearing at
`home.md:495`, `home.md:1904`, `commands.md:589`, `models-and-cost.md:684`, `:717`, `:922`,
`sessions-and-rewind.md:302`, `screen.md:512`, `keeping-an-eye.md:386`.

**I. Absolutes to re-check when the pages move.** `keys.md:1583` (*"Chords that are not
bound"* — the table names `ctrl+d`, `ctrl+y`, `ctrl+z`, `ctrl+h` and qualifies `ctrl+v`,
`ctrl+k`, `ctrl+r`, `ctrl+x`); `commands.md:1301-1303`; `standing-orders.md:146`, `:232`;
`what-i-remember.md:428`; `screen.md:1770`, `:1850`.

**J. The strings quoted verbatim.** RECON.md:526 lists them and it is still right: `home.md`
quotes `homeFootWord`, `homeEmptyWord`, `homeHeldShort`, `homeGoneShort`, `attentionNeedsWord`,
`attentionMovingWord`, `attentionNeedsTeach`, `attentionMovingTeach`, `pulseWatchWord`,
`pulseOrderWord`, `pulseWorkingWord`, `homePhoneWaitingWord/RunningWord/NewsWord`,
`homeSheetBackWord`. Add to that list `homeItemActions` (`homestanding.go:73`),
`standPageVerbs` (`standingpage.go:116`), `homeTabWord` (`homebridge.go:392`),
`lastDoorWord` (`render.go:2720`), `homeDoorWord` (`home.go:2911`), `taskSheetRoomKeys`
(`taskview.go:155`) and `memoryFilterHint` (`memorypanel.go:16`).

### 5.3 The gates — and the one that blocks the design

**`/home/santosh/af-home/internal/tui3/manual_test.go`** (94 lines):

- **`TestTheManualMentionsEveryCommandTheTableOffers` :22** — every `commands` row's `/name`, and
  every `/alias`, must appear somewhere in the corpus. The table is `commands.go:53-321`: 30
  names and 23 alias words (`set`, `config`, `connections`, `clear`, `clean`, `reset`, `sessions`,
  `orders`, `harnesses`, `sub`, `info`, `context`, `usage`, `tokens`, **`spend`**, `save`,
  `upload`, `perms`, `undo`, `back`, `?`, `exit`, `q`). Adding `/tasks` or `/spend` means adding
  a page section that names it.
- **`TestTheChatManualDoesNotSpeakOfTheResident` :44** — the banned list, verbatim from
  `manual_test.go:48-52`:

  ```go
  foreign := []string{
      "alt+1", "alt+2", "alt+3",
      "the board", "the self page", "standing watch",
      "resident employee", "front desk",
  }
  ```

  The match is a lowercased `strings.Contains` over every page. `alt+4`…`alt+7` are **not**
  banned. `"standing watch"` also catches "standing watches". A grep over the corpus today returns
  **zero** hits for any of them.
- **`TestEveryChatManualPageIsReachableByItsOwnName` :70** — every page name, hyphens turned to
  spaces, must reach itself through `Chat().Search(query, 8)`.

**`/home/santosh/af-home/internal/session/manual_test.go`**: `:17` every belt tool by its exact
registered name; `:46` the same with `revise_design` on the belt; `:70` the two corpora share no
page name.

**`/home/santosh/af-home/internal/manual/chat_test.go`**: `:18`
`TestTheChatManualAnswersTheQuestionsPeopleAsk` — **385 probes** in the table at :19-762, each
asserting a real question reaches a named page through `Chat().Search(q, DefaultResults)`; `:790`
the two corpora share no page name. Plus `internal/manual/packed_test.go` — the archive must agree
with the folders.

**The blocker, stated plainly.** SCREEN 3a's `alt+1…7` jump (`SCREENS.txt:22`) cannot be
documented. Writing `alt+1` into any chat page fails `manual_test.go:44`, and CLAUDE.md:48
requires the key to be documented in the same change. There are exactly three honest ways out,
and the first two are worse than they look:

1. **Never write the literal.** Spell it "hold alt and press the place's number", give the
   examples as `alt+4`…`alt+7`. This passes the gate and lies by omission about the first three
   places — precisely the failure mode CLAUDE.md:83-86 warns about (*"a tool named in a 'what
   aforge cannot do' section satisfies them perfectly while lying"*).
2. **Pick a different class.** `shift+←→` is free and portable, but it is already spent on the
   time window (SCREEN 3d), and `ctrl+<digit>` has no encoding to send (`SCREENS.txt:36-37`).
3. **Change the ban, deliberately, in the same commit, with the reason written down.** The three
   strings are banned because they *were* resident vocabulary and v3 had no such key. Once v3
   binds them, the ban is no longer a fact about the product — it is a stale rule making the
   manual lie. Narrow `foreign` to the phrases that are still resident-only (`the board`,
   `the self page`, `standing watch`, `resident employee`, `front desk`) and delete the three
   `alt+N` entries, with a comment naming the wave that took them.

**Recommend (3).** It is the only option that leaves the manual true, and CLAUDE.md's own rule
for this case — *"when you make something possible, hunt down the page that says it isn't"*
(:83) — points at it directly. It is a gate edit, so it needs the owner's signature, which is why
it is written here rather than done quietly.

### 5.4 The pages to rewrite, in the order the waves land

| Wave | Pages | Sections |
|---|---|---|
| the router + the tab bar | `keys.md`, `home.md`, `screen.md`, `commands.md` | `keys.md:979` (tab), `:1003` (no chord for home), `:341`, `:849`, `:1583`; `home.md:1328`, `:20`, `:302`, `:1224`; `screen.md:13` (*"What the frame draws, top to bottom"*); `commands.md:804` |
| tasks becomes a place | `tasks.md`, `commands.md`, `reading-a-task-page.md` | `tasks.md:883`, `:723`, `:702`, `:1080`, `:1109`, `:1248`; `commands.md:1062` |
| standing becomes a place | `standing-orders.md`, `keeping-an-eye.md` | `standing-orders.md:371` (*"The keys on the standing orders page"*), `:175`, `:191`, `:332`; `keeping-an-eye.md:175`, `:250`, `:614` |
| memory becomes a place | `what-i-remember.md`, `commands.md`, `keys.md` | the whole of `what-i-remember.md`; `commands.md:187`; `keys.md:995` |
| the global composer | `home.md`, `empty-screen.md`, `keys.md` | `home.md:873`, `:1068`, `:1138`; `empty-screen.md:41`, `:57`; `keys.md:3`, `:302` |
| spend / search | new sections in `models-and-cost.md` and `home.md`, plus new `commands.md` rows | — |

Every one of those waves must also add its probes to `internal/manual/chat_test.go`'s table
(currently 385) and rebuild the pack (`make build`), or `internal/manual/packed_test.go` fails.
