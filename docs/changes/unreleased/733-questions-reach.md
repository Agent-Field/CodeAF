---
kind: added
title: questions reach home, another page, a sheet and an unattended window as one decision
pr: 733
surface: [chat, engine]
invalidates:
  - "A question was delivered according to which lane raised it, and the surface decided per-lane where it went. There is now ONE presence rule (`internal/tui3/questiondelivery.go`): on the conversation it pins now; on any other page of the same window it pins AND one dim row says `<head> · waiting in this conversation · alt+a`; with nobody at the keyboard for ten minutes the project's rule takes what it may and everything else goes to home, the desktop notification and — for a blocking question only, once — the terminal bell."
  - "The `ask` tool's questions (`session.QuestionAsk`) reached no surface at all: a real run left two `ask` calls waiting with `still waiting for an answer` on the step and nothing on any screen to answer with. The block now draws that lane, for the same reason it draws `subharness-ask`, `fuel` and `conflict` — there is no older block to retire."
  - "Several questions raised inside one step arrived one on top of another. Quiet ones now gather and arrive together at the step's end — the moment the model speaks again, or the turn finishes — as a SHEET: grouped by the kind of decision, `1`-`9` move to a row, `enter` opens one on its own, `s` sends what is answered and delegates the rest to their own picks, `g` gives one answer to every row of that kind that offers the same key, `esc` puts the whole thing off. A question something is blocked on never waits for a boundary."
  - "There was no per-project control over how question kinds behave while nobody is there. `/autonomy` reads and changes the engine's rows, and `D` on a question now writes the rule its own word promises (`decide these from now on`) instead of answering exactly one question. A question a project rule is about to take wears `· your rule` beside its countdown — there are no hidden rules."
  - "`SetAutonomy` refused an automatic policy over a clarification and accepted one over a confirmation. Both are refused now, at the write AND at the read, so a hand-edited `autonomy.json` cannot make either run on a clock: confirmation is what is asked before something destructive and always asks."
  - "`D` wrote this project's rule twice after the room landed (#737) — once silently through `questionDialDoor` and once through the sheet's door. There is ONE write now, and it says what it wrote and prints the engine's refusal; `autonomyAgent` widens `questionDialDoor` rather than sitting beside it."
  - "A window that learned another window had answered wrote the receipt with `you` on it. It writes `another window` (`session.DecidedByWindow`), and two windows answering differently within a second leave BOTH lines on screen with `two windows answered that · the first one is the decision` — nothing is merged."
  - "Home could answer three lanes (consent, task, standing) and dropped every other key. It answers any lane now: the whole question rides in the presence file, the key is looked up in that question's own options, and the answer goes through `Agent.ResolveQuestion`."
  - "`Agent.AskQuestion` banked only the word book, so the `ask` lane reached the window holding the conversation and nowhere else — home drew that conversation as `working` while three questions waited on it. It banks the presence row too (`presenceAskingWhole`), which is what home, another window and the `--host` link read."
  - "A quiet question was held for its step to end, and the step could not end until it was answered: the `ask` tool blocks its turn, so questions raised by it deadlocked behind a boundary that would never come. The hold is bounded by three seconds and the boundary fires on whichever comes first."
  - "A withdrawn question could leave a step's batch holding it. The delivery rule forgets it, an open sheet loses that row and re-flows without moving anybody else's answer, and the one dim `⊘ <head> — no longer needed · <reason>` line stays where the question was."
---

The delivery rule is deliberately ONE component shared by the conversation, by
every other page, by home and by the unattended window, so that "away", "blocking"
and "a step's boundary" cannot acquire a different meaning on each surface.

Answering over `--host` is still not built and the manual now says so where it
used to imply otherwise: events cross the wire and `ResolveQuestion` does not, so
a hosted window draws no engine question rather than drawing one nobody can
resolve (A CAPABILITY THAT CANNOT WORK IS ABSENT, NOT BROKEN).
