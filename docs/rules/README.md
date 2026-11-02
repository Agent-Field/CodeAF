# Contributor rules

Start with the [contributor guide](../../CONTRIBUTING.md). These pages own
repository policy; [documentation ownership](../DOCUMENTATION.md) explains where
product, developer and historical knowledge belongs.

| Page | Purpose |
| --- | --- |
| [branching.md](branching.md) | Contribution branches and promotion pointers |
| [ci.md](ci.md) | Local checks, CI gates and failure diagnosis |
| [changelog.md](changelog.md) | Change fragments and release notes |
| [promotion.md](promotion.md) | Staging validation and releases |
| [Branch rules](../../.github/rulesets/README.md) | Intended server enforcement |

Branch from `dev` and target pull requests there. Stage only your changes.
Promotion pointers advance through tested history by fast-forward; never
force-push `dev`, `staging` or `main`. Include a change fragment in each pull
request and update the shipped manual when behavior changes.
