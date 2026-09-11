---
kind: fixed
title: the parity bench's own verdict, corrected — one regression, one flake, and a grep read as a screen
pr: 865
surface: [docs]
invalidates:
  - "docs/design/prompt-diet/BENCH.md §1a landed saying TWO subtests regressed on the prompt diet. One did. `TestTUIE2E/space_in_the_task_room_pages_the_card` was FLAKE: it failed once at 249s against the baseline's 71.7s because the task landed `your call · ran 2m 8s` where the baseline's landed `done · ran 10s`, so two 30-second waits blew before the paging assertion was reached — and on two further runs a side it passed 2/2 on both. The draft was written before those repeats came back and says so in a sentence (\"Both are candidates, not verdicts\") that is easy to read past. Anyone acting on it would have spent a day hunting a behaviour change that never happened."
  - "The same section said the failing screen of `TestQuestionsE2E/TheOrdinaryRoadCarriesAQuestionAndItsAnswer` \"contains `· you ·` elsewhere in the same dump\" and might therefore be a frame race. It does not. That match was the TEST'S OWN ERROR TEXT — `the screen never said \" · you · \"` — counted as though it were screen content, because a grep count was read instead of the lines it matched. The screen carries `· another window ·` twice and `· you ·` nowhere, which makes the finding stronger rather than weaker: there is no race, the word was simply wrong."
  - "That subtest WAS a real regression on `prompt-diet/integrate` at 2a90be7e8 — 3/3 fail there against 3/3 pass on the `dev` baseline at 6aa6a946e, the same assertion and the same wrong word every time, on the attribution of an answer the person gave in their own window. It is NOT on the trunk: the same subtest against `dev` at c9f24f2b6, after the wave merged as #844, passes 3/3. Whatever spelled that row `· another window ·` was fixed before the merge landed, so the finding is a record of what the bench caught and where, and there is nothing to fix in the code."
  - "Both errors in the landed draft came from reading a grep count instead of the lines it matched — the same shape as the 94%-cut figure BENCH.md §1b already records one layer up, where a median over every request measured the mix of call kinds rather than the size of the prompt. §1a now carries its own account of what it got wrong, beside §1b, because a bench whose corrections live only in a changelog is a bench whose next reader repeats them."
---

The prompt-diet wave's evidence page was written while its own confirmation runs
were still in flight, and merged with the draft's wording. The repeats landed
afterwards and said something different from what the page claims.

The rule this breaks is #176's, and the bench states it: reproduce a red on a
live-model suite before calling it a regression, do not rerun it in isolation and
move on. The repeats were queued and the page was written anyway. What this
change lands is the answer they gave.
