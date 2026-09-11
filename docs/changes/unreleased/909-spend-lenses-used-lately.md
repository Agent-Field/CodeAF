---
kind: added
title: spend lenses (rhythm/models/days/year) and a framed /model sheet with used-lately
pr: 909
surface: [chat]
invalidates:
  - "The spend place was one reading — sparkline, what ran it, what it was for. It has four lenses now (`rhythm`, `models`, `days`, `year`), cycled with `[` and `]`, and `/spend models|days|year|export|7d|month` open them by name."
  - "`/spend` took no argument and opened the place alone. Words after it pick a lens, a window, or write a JSON export of the current window's priced lines beside the usage ledger."
  - "A day on the spend place was never a door. On the `days` lens, `enter` drills into that day's `rhythm` window — priced or quiet — and `esc` returns to `days` on the day you left."
  - "`/spend month` is the current calendar month (one month bucket from the 1st), not the last thirty days."
  - "The active lens is named on the spend place's head row (`rhythm · …`); the foot says `[ ] lenses · ] <next>` so the next lens is named, not only the keys."
  - "The model picker ranked the catalog with no memory of what you had paid. Every door onto it (`/model`, Providers, task model, home draft, composer) now draws `used lately` above `all models` when the fortnight ledger has priced rows, and a dim chip `this fortnight $12 · 2.1M` on those rows (absent when unused)."
  - "`/model` (and the status model word) used to be bottom chrome — a filter box in the draft's place with a short list under it. It is a framed sheet over a faded conversation now: title `choose model` (or `choose task model`), filter inside the sheet, `esc · cancel` on the foot, the same covering grammar as the context chooser. Settings, home and the composer still embed the same picker type in their bodies."
  - "`/model used` was not a filter. It opens the same picker already narrowed to models this machine has run in the last fortnight; `used` is a filter word, not a refuse of bare `/model <slug>`."
  - "A quiet spend window kept only `nothing spent` over blank air; a warming ledger looked like an empty machine. Quiet windows now carry a dim guide (`this stretch is quiet · shift+←→ moves the days · [ ] another reading`); a ledger still on the wire draws a skeleton (`reading what this machine has spent`) rather than the empty whisper; `/model used` with no spenders says `no models used this fortnight · drop used for the full list`."
  - "The Models lens sorted only by cost/tokens via `c`/`t`. It sorts by every column now (model, in/out, role, `$/M`, spend); the head cells are the sort doors (`enter` cycles; `s` cycles; `c`/`t` still jump to cost/tokens)."
  - "`session.ModelSpend` and `DaySpend` carried only a token sum. They carry `Input` and `Output` as well, so the Models and Days lenses can compare the two halves; `PeakHour` and `ActiveStreak` answer the Year lens facts."
---

The Spending tab remains the only editor for the rails. Lenses are readings of the
same machine ledger; `/cost` is still this conversation's tree bill.
