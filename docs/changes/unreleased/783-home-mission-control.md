---
kind: changed
title: Home is seven panels, each answering one question, under a four-place bar
pr: 783
surface: [chat, docs]
invalidates:
  - "Home at rest was one flat ranked list under a `N chats · what wants you first` section line, with one fold at its foot (`▸ 15 more, quiet since 6d`). It is seven panels in one, two or three columns (under 110 cells, to 170, past it), each folding inside itself as `N more · <place>` or `N more · type to find one`; the section line and the foot fold are gone."
  - "A card stood beside home's list from 136 columns, and a three-column tier put list, card and more side by side. There is no card at rest at any width; the one card left stands beside a search, from 136 columns, and the phone sheet is unchanged."
  - "`needs you` was the `?` rows at the top of the list and `running` the `◐` rows under them. They are panels with two marks only — the amber `?` and one spinner on the first running row — and a digit answers the top question from anywhere on home, its answers drawn on its own row rather than on the strip above the box."
  - "`since you left` was a ledger block above home's list. It is a panel in the right column, with a line per landed task, per file made, per firing and for memory; a task call older than two days leaves `needs you` for its fold, `N older · tasks`."
  - "Home drew the band registry's bands (`homebands.go`) on its resting card. The resting grid draws no bands; each panel reads what it needs from the cached reading, and the registry is left serving only the search card and the phone sheet."
  - "`ctrl+t` on a home row was the way to start a conversation in another folder. `enter` on a row of the `projects` panel is; `ctrl+t` still works on a conversation row but is no longer what home teaches."
  - "`alt+g` grouped home by project and `alt+q` hid the quiet rows. Both are unbound on home; the `projects` panel is the view by project."
  - "The tab bar drew seven words and `tab` walked all seven. It draws four — `home  tasks  spend  settings` — and `tab` walks those; standing, memory and search are reached by `/standing`, `/memory`, `/search` and `alt+5`…`alt+7`, and their word is drawn after the four only while you stand in one."
  - "`alt+3` opened standing, `alt+4` memory, `alt+5` spend, `alt+6` search and `alt+7` settings. The digits follow the bar: `alt+1`…`alt+4` are home, tasks, spend, settings and `alt+5`…`alt+7` are standing, memory, search."
  - "The chat's head had adaptive blank rows around the tab strip that grew and shrank with the terminal, and no pulse. Every frame — home, every place, the chat — has the same four-row head: pulse, strip or bar, rule, blank; inside a chat the pulse carries `N want you · N moving` and the budget, on home only the budget and the clock."
  - "The emptiness law said nothing zero or unknown is ever drawn, and an empty zone drew nothing. It is narrowed for panels only: an empty panel keeps its heading and one dim whisper naming what arrives there (`work you send off with /task runs here on its own`); numbers still draw nothing at zero."
  - "The manual's home page described the flat list, the card and its five bands, `alt+g`/`alt+q` and the fold. `internal/manual/chat/home.md` is rewritten around the panels, and states what home cannot do: stop another window's task (the row says `another window`), or show a `something is wrong` panel, which is wave 2."
  - "A click on a home row selected it and a second click opened it, while every other place opened on one. One click is `enter` on home too, a click on a panel heading opens the place it names, and a click on a fold line is `enter` on it."
  - "Home's panel headings were inert. Each opens its place: `needs you`, `running` and `since you left` open tasks, `spend` opens spend, `next up` opens standing, `where you were` opens the search."
  - "A `running` row's verb read `s stop`. It reads `s stop it`, and `ctrl+x` asks to stop a held running task from any column, so a three-column home, where `→` crosses out of the middle column instead of opening the strip, still has the door; the foot names it."
  - "A short frame dropped panels by priority alone, so at 120×14 home could keep `needs you`'s whisper and drop every conversation. A whispering panel now drops before any panel with rows."
  - "`TestWalkingPastTheWindowScrollsEveryPlacesList` covered home. The grid has no window to scroll — it squeezes, grows and folds — so the test leaves home to the places that scroll."
  - "A part that asked `your call` on its card and was then decided stayed `your call` in the conversation; only the task column knew (since #776). It lands a second time, as done or incomplete, where the question was, and a folded batch counts work rather than landings."
  - "On a window opened on the engine host — a bare `aforge` — Settings → Spending's `this one` read only the conversation's own books, because the tree reading needed a ledger path that door never sets. A cut errand's receipt banked after the turn ended (a naming call past its patience, a route judge the turn finished in front of) reached `today` and never `this one`, so one conversation read $0.0056 and $0.0044 side by side. The tree now reads through the same ledger seam `today` does (internal/tui3's treespend.go, `app.treeLines`); over a connection it sums the spend place's fortnight."
  - "On the engine host's door Settings→Spending's `this one` fell back to the conversation's own books, which move only when the frame clock asks the agent, so a provider receipt banked after the turn ended (a cut route judge, a title call past its patience) counted in `today` and never in `this one`. The tree now reads through the same ledger seam `today` reads, and over a connection sums the spend place's fortnight."
---

The ruling is `docs/design/home-mission-control/DESIGN.md`. Home answers the
questions a person has walking up to the machine — what needs me, what is running,
where was I, what happened, what did it cost, what is next — one panel each, act on
the left and watch on the right. A short terminal squeezes the panels in a fixed
order (`next up` first, `needs you` last); a tall one grows them to their budgets,
and `spend` never grows. `space space` lands on the conversation you were in before
this one, so a switch back is two keys. The tmux suite's home needles and subtests
are re-pointed at the panels in the same change.
