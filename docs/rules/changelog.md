# Change entries

## One entry per pull request

Every pull request into `dev` carries a file in `docs/changes/unreleased/`.
Name it `<pr>-<slug>.md`, using the pull request number. Create and validate it:

```sh
make changelog-new PR=82 KIND=changed SLUG=example-change
make changelog-check
```

```yaml
---
kind: changed
title: Describe the resulting behavior
pr: 82
surface: [chat, docs]
invalidates: []
---
```

Kinds are `added`, `changed`, `renamed`, `fixed`, `removed` and `internal`.
Optional surfaces are `chat`, `engine`, `resident`, `remote`, `build` and `docs`.
Keep the title under 100 characters without a trailing full stop. Explain any
necessary limits in a short body.

## Changed assumptions

Use `invalidates` for specific claims about behavior that changed. State what
used to be true and what is true now, in complete sentences ending with a full
stop and at least 25 characters long. Leave the list empty when no prior claim
needs correcting. Read recent entries before relying on earlier documentation.

For housekeeping with no user-facing change, a `kind: internal` entry with only
a title is sufficient. The `no-changelog` pull request label is available for
changes such as comment typos. Documentation changes normally still need entries.

## Release notes

Roll up entries on a contribution branch and merge through a pull request to
`dev` before promotion:

```sh
make changelog-preview VERSION=v0.2.0
make changelog VERSION=v0.2.0
```

The roll-up writes `CHANGELOG.md` and consumes the included fragments. Keep
release section headings intact: `codeaf-changes notes <tag>` reads them for
release notes. Oversized notes retain every headline and link to the changelog
for the full record. See [promotion.md](promotion.md).
