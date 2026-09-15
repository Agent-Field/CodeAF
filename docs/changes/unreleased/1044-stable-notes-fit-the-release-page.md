---
kind: fixed
title: the stable release notes are rendered to fit a GitHub release page instead of copied whole
pr: 1044
surface: [build, docs]
invalidates:
  - "The publish job copied the whole `## <tag>` section of `CHANGELOG.md` into the release body with an awk one-liner, and a rolled-up section carries every fold of every entry — the v0.2.0 section renders at 1.7 MB while GitHub refuses a release body over 125,000 characters, so the first stable dispatch would have built six binaries and failed at `gh release create`. The notes now come from `codeaf-changes notes <tag>`: the section whole when it fits, the headline layer with the folds left out and one line pointing at `CHANGELOG.md` when it does not, and a cut at a line boundary when even that is too long. A tag with no section still falls back to generated notes."
---
