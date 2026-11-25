---
kind: fixed
title: codeaf update leaves a codeaf inside a macOS app bundle to its app
pr: 2820
surface: [engine, docs]
invalidates:
  - "Was true: `InstallRefusal` refused to replace a codeaf only when a package manager owned its path or its folder was not writable, so an explicit `codeaf update` could replace the engine inside a signed application bundle in a writable `/Applications`. Is true now: a target whose path sits inside a `*.app/Contents` folder is refused, with a message naming the app and saying to update it there, whether or not its folder is writable; `CODEAF_NO_UPDATE_CHECK` still leaves an explicit update working."
---
