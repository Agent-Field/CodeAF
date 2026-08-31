# The branch rules, checked in

GitHub keeps branch rules in its own database, where they are invisible to the
tree they govern and changeable by anyone with admin from a settings page that
records no reason. These files are the same rules as a diff: what is enforced,
when it changed, and who signed for it.

They are **not applied automatically.** Apply them by hand:

```sh
gh api -X POST repos/Agent-Field/aforge-v2/rulesets --input .github/rulesets/dev.json
gh api -X POST repos/Agent-Field/aforge-v2/rulesets --input .github/rulesets/promotion-pointers.json
```

To update one that already exists, find its id and PUT over it:

```sh
gh api repos/Agent-Field/aforge-v2/rulesets --jq '.[] | "\(.id)\t\(.name)"'
gh api -X PUT repos/Agent-Field/aforge-v2/rulesets/<id> --input .github/rulesets/dev.json
```

## They do not work yet

`Agent-Field` is on the **free** plan and this repository is **private**, and
that combination has no branch rules at all — both the rulesets API and the
older protection API answer `403 Upgrade to GitHub Pro`. Until the org moves to
**GitHub Team**, everything in `docs/rules/` is convention that a careless
`git push --force` can undo without being asked a question.

Check whether it has started working:

```sh
gh api repos/Agent-Field/aforge-v2/rulesets
```

## `required_status_checks` names are job names

The contexts named in `dev.json` and `promotion-pointers.json` are the `name:`
fields of jobs in `.github/workflows/`. Rename a job and the check it was
standing in for silently stops being required — the branch is then unprotected
and nothing says so. The two move in the same commit.
