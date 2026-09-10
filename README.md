# aforge

## Install

Install the latest stable, release candidate, dev build, or staging build:

```sh
curl -fsSL https://raw.githubusercontent.com/Agent-Field/aforge-v2/main/scripts/install.sh | bash -s -- --stable
curl -fsSL https://raw.githubusercontent.com/Agent-Field/aforge-v2/main/scripts/install.sh | bash -s -- --rc
curl -fsSL https://raw.githubusercontent.com/Agent-Field/aforge-v2/main/scripts/install.sh | bash -s -- --dev
curl -fsSL https://raw.githubusercontent.com/Agent-Field/aforge-v2/main/scripts/install.sh | bash -s -- --staging
```

Pin one published tag with `VERSION`:

```sh
curl -fsSL https://raw.githubusercontent.com/Agent-Field/aforge-v2/main/scripts/install.sh | VERSION=v0.2.0 bash
```

While this repository is private, export `GITHUB_TOKEN` before using any of
these commands. The installer puts aforge at `~/.aforge/bin/aforge` by default.
