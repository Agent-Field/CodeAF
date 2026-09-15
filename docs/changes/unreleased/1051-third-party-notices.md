---
kind: added
title: Every release carries THIRD-PARTY-NOTICES.md beside its binaries
pr: 1051
surface: [build, docs]
invalidates:
  - "This repository reproduced no third-party licence anywhere, and a release published only `dist/codeaf-*` and `checksums.txt`. `THIRD-PARTY-NOTICES.md` now sits at the root and is copied into `dist/` before the checksums are written, so every release on every channel carries it and `checksums.txt` covers it."
  - "A licence notice was nobody's job and was written nowhere. `cmd/codeaf-notices` generates the whole file from `go list -deps ./cmd/codeaf` plus the two copies this tree carries, so it is regenerated with `go run ./cmd/codeaf-notices generate` and never edited by hand."
  - "Adding a dependency used to cost nothing beyond `go.mod`. A law in `cmd/codeaf-notices` now fails when a module compiled into the binary has no section in the committed notice, so a new dependency means regenerating that file in the same change."
---

`internal/pair/cpace` is BSD-3-Clause and is linked into every build; its clause 2
asks for the notice in the materials that go out with a binary distribution, and
`internal/connect/ampcatalog/providers.json` is an embedded copy of the amp-labs
catalog with the same shape of obligation. The notice covers 57 components — the
55 modules in the build and those two copies — each with its version, its licence
identifier and its licence text word for word.
