---
kind: added
title: Share contacts and other common records across apps
pr: 2949
surface: [app, engine]
invalidates:
  - Apps used the existing type name to share records. Kit 2 also lets them bind a portable common key to the same type and record IDs.
---

The builder offers common types and names existing records it can reuse.
Use `codeaf app common-types` to list the library and
`codeaf app bind-common-type` to adopt a type you already have.
