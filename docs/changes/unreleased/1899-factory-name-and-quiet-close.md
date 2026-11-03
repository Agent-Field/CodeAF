---
kind: changed
title: This machine's factory is named for its person and can be renamed; quit waits for requests in flight
pr: 1899
surface: [engine, docs]
invalidates:
  - "Was true: the factory on this machine was listed as `This machine`, always, and the app could not rename a factory. Is true now: it is listed as `<login>'s factory` once it knows whose GitHub it is (`Your factory` before), and `factory.rename {factory, name}` keeps a person's own name for any connected factory on the app's record of it (never sent to the factory; empty goes back to the default)."
  - "Was true: a queued job's presentation title was `Waiting for runner`. Is true now: `Waiting to start`; the kind stays `waiting_for_runner`."
  - "Was true: quitting closed the local factory's HTTP server at once, so a request already being answered (a runner attaching) could still write into the factory after it was let go. Is true now: every request lives under the host's context, which quitting cancels first, and the server shuts down gracefully within the runner's own stop wait."
---
