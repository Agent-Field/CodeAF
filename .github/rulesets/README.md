# The branch rules, checked in

GitHub keeps branch rules in its own database, where they are invisible to the
tree they govern and changeable by anyone with admin from a settings page that
records no reason. These files are the same rules as a diff: what is enforced,
when it changed, and who signed for it.

They are **not applied automatically.** Apply them by hand:

This repository is public. The live `protection` ruleset covers `main`, `dev`,
and `staging`. The checked-in `promotion-pointers.json` has no bypass actors.
Whoever applies it must add the owner of `PROMOTION_TOKEN` (or that account's
role) to its bypass list; otherwise Friday's push is refused. The token also
needs Contents and Workflows read and write to publish the staging release.

```sh
gh api -X POST repos/Agent-Field/codeaf/rulesets --input .github/rulesets/dev.json
gh api -X POST repos/Agent-Field/codeaf/rulesets --input .github/rulesets/promotion-pointers.json
```

To update one that already exists, find its id and PUT over it:

```sh
gh api repos/Agent-Field/codeaf/rulesets --jq '.[] | "\(.id)\t\(.name)"'
gh api -X PUT repos/Agent-Field/codeaf/rulesets/<id> --input .github/rulesets/dev.json
```

## Inspect the live rules

The repository is public and its `protection` ruleset is live. Inspect the
rulesets and confirm the promotion account can bypass the pointer rules before
enabling the Friday workflow:

```sh
gh api repos/Agent-Field/codeaf/rulesets
gh api repos/Agent-Field/codeaf/rulesets/<id> --jq .current_user_can_bypass
```

## `required_status_checks` names are job names

The contexts named in `dev.json` and `promotion-pointers.json` are the `name:`
fields of jobs in `.github/workflows/`. Rename a job and the check it was
standing in for silently stops being required — the branch is then unprotected
and nothing says so. The two move in the same commit.
