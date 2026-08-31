# Unreleased changes

One file per pull request. They are rolled into `CHANGELOG.md` when a version is
cut, and until then this directory **is** the changelog for anything newer than
the last release.

## If you are a model, read this directory first

It answers the question `git log` cannot: **which of the things you believe about
this repository stopped being true.** Grep it:

```sh
grep -rn 'invalidates' -A6 docs/changes/unreleased/   # everything that moved
grep -rln 'surface:.*chat' docs/changes/unreleased/   # only the v3 surface
```

Then `CHANGELOG.md` for anything older.

## If you are adding one

```sh
make changelog-new PR=82 KIND=changed SLUG=branch-rules
$EDITOR docs/changes/unreleased/82-branch-rules.md
make changelog-check
```

[`../rules/changelog.md`](../rules/changelog.md) says what belongs in the fields
and, more usefully, what does not.
