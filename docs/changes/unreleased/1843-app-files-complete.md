---
kind: added
title: codeaf app completes a typed path for the composer's @, directory by directory
pr: 1843
surface: [engine]
invalidates:
  - "The app's @ could only search: files.find fuzzy-matched names across one walk of the agent's workspace, which for an agent working in the home folder meant every repository under ~, and a path such as ~/Downloads/ or ../other/ could not be reached at all. files.complete {agent?, path, limit?} now lists the folder a typed path names, from exactly where it says (absolute, ~/, or relative to the agent's workspace, or home with no agent), folders first, narrowed by the half-typed last name."
  - "The terminal's folder browser read its typed path through app.resolvePath alone. The rule is now resolvePathAt, shared with tui3.CompletePath, so the app and the terminal read `~`, `.`, `..` and a relative path the same way and hide the same names."
---

New method `files.complete {agent?, path, limit?}` → `{dir, entries: [{name,
path, dir?}], more?, state?}`. One readdir per call through the folder
browser's own listing (dot names, `.git`, `node_modules` and `vendor` left out
unless the typed name starts with a dot); `limit` defaults to 50 and is
clamped to 200; `state` is missing or unreadable instead of an empty list.
Nothing is opened or written. files.find is unchanged and still answers a bare
word; an engine without files.complete answers `unknown_method`.
