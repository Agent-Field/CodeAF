---
kind: changed
title: one fzf-style fuzzy matcher ranks every quick-search picker, and the settings search takes spaces
pr: 1089
surface: [chat]
invalidates:
  - "Every picker on the chat surface kept its own quick-search scorer. The model picker, the harness picker, the subharness page, the resume roster and the deliverables shelf ranked by a prefix-then-substring-then-subsequence token ladder (palette.go's [tokenScore], lower score better); the settings sheet, the autonomy rows, the connections catalog, the memory place, the folder picker's loose rung and the rewind sheet matched by substring. All of them now rank with one matcher, internal/fuzzy — a port of fzf's FuzzyMatchV2 with helix/nucleo's two-matrix correction — and a higher score is better everywhere."
  - "The model picker's ranking direction is inverted: score sort is now descending within [GroupOrder]. Queries keep their behavior — same splits, same `@lane` and `<1s` terms, same fold-narrowing (issue #1022) — but the rungs that used to hold prefix above substring above subsequence are gone; the boundary and consecutive bonuses produce that order instead."
  - "The settings search reads the query as typed and applies the matcher's smart case: a word with no uppercase in it matches anywhere, a word with any uppercase in it has to be found as typed. The old search folded the query and left the words it searched folded too, so an uppercase word matched nothing at all."
  - "The settings search ranks the rows each tab keeps best-first, stable on registry order within equal scores, and searches a row's five fields — shown label, registry key, about line, the VALUE the row currently holds, and the tab name — so `prompt` finds the gate by what it answers with, not just by what it is called."
  - "A space typed while the settings search is open inserts into the query instead of activating the row under the cursor, so phrases like `shell command` can be typed and a setting cannot be flipped by a word's separator. Out of a search, space still activates rows exactly as before."
  - "internal/registry's own scorer is gone: FuzzyMatch is a pass-through to the shared matcher over the verb and the description — best field per term, higher better like everywhere else — and the greedy position-sum scoreSubsequence walk it used to be, with the precomputed lowerVerb/lowerDescription folds that walk read, is deleted. No live caller of FuzzyMatch remains outside its own tests; the exported shape stays as the registry's door."
---

The picker's old ladder existed because its scorer had no bonus model — it
needed tiers to keep loose matches at the bottom. fzf's scoring produces that
order on its own: a boundary bonus for landing after a word start, a consecutive
bonus for a tight run, the first character's boundary doubled. The port is
hand-written in-repo (no dependency), credits fzf (MIT) and nucleo (MPL-2.0) in
its header, keeps the camelCase retune nucleo made (5, not fzf's 7), fixes the
non-optimality nucleo documented (`foo` against `xf foo` finds `x__foo`, not
fzf's `xf_oo`), and allocates nothing per matched row on the hot path — one
pooled slab across calls.
