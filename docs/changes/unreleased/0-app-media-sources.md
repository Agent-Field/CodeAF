kind: added
title: codeaf app lists and gets a conversation's media as engine-issued sources
pr: 0
surface: [engine]
invalidates:
  - "Generated and attached media existed only as a path inside a tool's prose and an untyped artifact row, so an app had to parse prose to know a picture's size, what a voice was told to say, or whether the provider charged. The artifact index row now carries an optional structured fact, and media.list / media.get answer it."
  - "A source id derived from a path would make a regenerated file the same source as the one it replaced. New ids are minted once per record; only rows older than this change get a stable derived id."
---

New optional methods `media.list {agent, cursor?, limit?}` and
`media.get {agent, source}`. An engine that does not implement them answers
`unknown_method`, which means "this host cannot say", not "no media". The app
can only name a source id the engine issued — there is no path parameter — and
only `state: available` may be opened. `generate_image`, `speak`,
`generate_music`, `generate_video` and `edit_video` record the fact (actual
image size from the file header, hash of the bytes the provider sent, the
exact reference paths and spoken text, charge from the provider's reported
usage) beside their unchanged prose; a message sent with attached images,
sound or video notes those files as sources.
