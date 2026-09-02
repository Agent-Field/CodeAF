---
kind: fixed
title: the background check walks its items on a machine that has no API key yet
pr: 421
surface: [chat, build]
invalidates:
  - "The standing tick used to read the profile with config.Load, which refuses without an API key, so on a keyless machine `aforge tick` errored on every timer wake — `could not start a pass: OPENROUTER_API_KEY (or OPENAI_API_KEY) is required`, five minutes apart, forever — and nothing was ever checked. It now builds from config.LoadKeyless: the pass walks every item, a reminder at a time still fires because the clock needs no judgment, and only a watch that has to judge something stops, on its own row, with `could not check: no API key: this session has not been given one yet`."
  - "config.LoadKeyless's comment said a door with nobody to ask has no business calling it. The standing tick is now a second legitimate caller and is named there: a pass that will do nothing costs nothing and needs nothing."
  - "TestTickWalksTheItemsAndWritesAWakeLine and TestTickLeavesQuietlyWhenAWindowIsAlreadyKeepingWatch are off .github/known-red.txt. They were filed as macOS-versus-Linux environment reds; the real variable was whether the shell running the tests exported an API key. Both now shut every place a key could come from themselves — the two variables and the profile directory, which AFORGE_PROFILE_DIR moves out from under the state root a test controls — and neither skips."
---

The two tick tests were written against a stub constructor that had no `Load` in it and
never followed the real one, which is why the door's demand for credentials went
unnoticed for a fortnight: a developer's shell has a key, so the walk test passed by
accident — on `gh run list` failing in a temp directory. `internal/manual/chat`'s
`keeping-an-eye` page now answers "do the background checks run before I set an API
key?" in the person's own words.
