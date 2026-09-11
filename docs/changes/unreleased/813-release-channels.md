---
kind: changed
title: Pushes publish dev, staging and rc builds, and a curl line installs any of them
pr: 813
surface: [build, docs]
invalidates:
  - "`main` was parked at v0.1.0, outside the pipeline, with nothing promoting to it. It is the release branch now: `staging` fast-forwards onto it and that push publishes a release candidate."
  - "Nothing used to publish by itself, and that was written down as a law. A push to `dev`, `staging` or `main` now publishes a build; only the stable release is still a person's decision."
  - "A release used to be a semver `v*` tag cut by hand on a commit that was on `staging`. There is no tag trigger any more and a tag pushed by hand publishes nothing; the workflow makes its own tags."
  - "The release workflow was `.github/workflows/release-binaries.yml`. It is deleted; `.github/workflows/release.yml` replaces it and is named `Release`."
  - "There was no installer, so getting aforge meant cloning and running `make build`. `scripts/install.sh` installs any channel or a pinned tag with one curl line, into `~/.aforge/bin/aforge`."
  - "The old release binaries carried no furrow at all, because the workflow never fetched it before building. Every published aforge now embeds furrow for its own platform."
---

`dev` merges tens of times a day and none of it was installable, so trying a
change meant building it yourself. Four channels fix that: `dev` and `staging`
publish disposable builds, `main` opens or continues a release candidate, and a
person still dispatches the stable one. Retention keeps the newest forty on each
disposable channel.
