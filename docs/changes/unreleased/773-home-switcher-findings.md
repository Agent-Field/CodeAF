---
kind: fixed
title: home says the gate's own sentence, and names a reminder that fired while you were away
pr: 773
surface: [chat]
invalidates:
  - "internal/tui3's `switcherConversationNote` no longer writes any grammar around a consent question. It prefixed `wants to ` onto the line internal/session's `consent.go` banks, and that line has been a whole predicate — `needs your ok to run bash` — since 1dfb9e0a2, so what a person actually read on home was `consentws wants to needs your ok to run bash`. The row is the engine's sentence now, verbatim and unprefixed. `consent.go` is untouched: it already promises that line is \"the one line another window may answer this from\", so a lane that ever writes a bare action owes the whole sentence rather than expecting a surface to complete one."
  - "`internal/manual/chat/home.md` said the note on a row asking for permission reads `wants to send on your behalf` / `wants to <the command>`, in four places. It reads `needs your ok to run <the tool>`, and the page now states the law: the sentence is repeated whole and nothing is put in front of it."
  - "`switcherReading.addLedger` walked only standing items that STILL STAND, so home's `since you left` block could never mention a one-off. internal/standing's `tick.go` stamps `LastFired` and retires a one-off in the same pass, and `app.standItems` drops every retired item — correctly, because a thing that is over is not keeping an eye on anything — so `remind me in 1 minute`, fired with the terminal shut, left the screen a person came back to saying nothing had happened. The block walks what stands AND what went now. The dead item is still on no list and in no band: the band is what is true now, the block is what happened."
  - "`app.standItems` returns two slices rather than one — the project's band, and what the SAME walk of the store found retired with a firing on it since the look stamp. They come back together because the seam walks a directory of documents and the second half is a test on the documents the first half was throwing away; asking twice would be one directory walk per project spent deciding what to ignore. `app.readBareBands` returns the same second half for a workspace that has orders and no conversation. Both are carried on `homeView.fired` and handed to `readSwitcher`."
  - "The first-look law is paid where the WALK is taken and not only where the block is built. `standFiredSince` answers no on a zero stamp, so a machine nobody has ever closed home on does not hand the ledger every reminder it ever fired for the block to discard one line later."
  - "internal/e2e's `the_firing_reaches_the_person` no longer logs `FINDING:` and passes when home draws no `since you left` block — it fails. `answer_from_home_across_two_windows` demands the gate's sentence whole and fails on the prefix, through a new `consentRowLine` needle in `tuiwords_test.go` whose spelling is owed by internal/session rather than by the surface."
---

Two defects #756's tmux run read off a real screen and wrote down in-test rather
than fixing. Both were on the switcher's side of home, and both were green in
unit tests: the consent one because the fixture fed a sentence the engine does
not write, and the ledger one because no fixture ever held an item that had
retired.
