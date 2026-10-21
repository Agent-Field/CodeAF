---
kind: added
title: codeaf atlas draws the two-computer architecture map in the terminal
pr: 1739
surface: [build, docs]
invalidates:
  - "The architecture map of the pairing work lived only in a Bun + OpenTUI app outside codeaf, one a person had to leave codeaf to run. `codeaf atlas` now opens the same map from the codeaf binary itself, drawn with the Bubble Tea the other surfaces already use, with the draggable boxes, the detail panes and the step-through flows that version had."
  - "The atlas data was written from the PR text and repeated a few of its mistakes. The Go data file now says what the code says: the four-digit check is the first two bytes of sha256(pubkey) mod 10000, approving happens in the chat card or `codeaf pair approve` and not on a home card, CODEAF_CELLS=0 is the off switch, the takeover is run by syncsetup's Continuer.Take through internal/handoff, the repository type is FurrowRepository, and `furrow fork COMMAND` refuses --json."
---

The map is a registry of named maps — `internal/atlas/registry.go`, with the
`pairing` map in `internal/atlas/pairing.go` — and the view renders only from
it; `data_test.go` checks every registered map, and fails the build when a
file the map points at no longer exists or an edge or flow names a box that is
not there, so the diagram cannot drift silently from the code it describes.
The old Bun + OpenTUI version in `tools/atlas` is deleted: the map is drawn by
the codeaf binary and nothing else.
