---
kind: added
title: Share contacts and other common records across apps
pr: 2949
surface: [app, engine]
invalidates:
  - was: Apps could ask for records by the existing type name.
    now: Kit 2 also lets apps bind portable common keys to a space's existing type and record IDs.
---

The builder offers common types and names existing records it can reuse.
Use `codeaf app common-types` to list the library and
`codeaf app bind-common-type` to adopt a type you already have.
