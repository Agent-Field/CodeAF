---
kind: removed
title: a window holds as many conversations as you open
pr: 137
surface: [chat]
invalidates:
  - "a window holds at most 8 open conversations and refuses a ninth until /quit — no longer true: there is no cap. Every door that counted what was open — home's enter, home's typed path and typed sentence, the switcher, search, a place's composer, /new — opens another without asking."
  - "`8 open is as many as aforge holds — /quit closes this one` was a sentence the surface could say. Nothing says it now, and the manual pages that quoted it say what is true instead."
---

The cap was eight, and its own comment had already written down the test it
failed: a cap hit in practice by somebody who was not testing it is evidence the
number is wrong. The owner hit it in a day of ordinary use and ruled the limit
pointless, so it is gone rather than larger.

What that costs is worth knowing, because nothing evicts. Every open
conversation is fully alive — a few goroutines, its transcript, one file
descriptor, a five-second presence tick, its transcript's lock — and stays that
way until a person closes it. A window's memory grows with the number of
conversations somebody opens and comes back on `/quit`, which closes the one in
front, or `ctrl+w` on the switcher, which closes the one under the cursor. The
switcher card still draws twelve rows and gives digits to the first nine; home
is the page that shows every conversation a window holds.
