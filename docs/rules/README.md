# The rules

How work moves from somebody's branch to somebody else's machine. Five short
pages, each answering one question, because the question you have is rarely the
whole story and reading the whole story to find one command is how a rule stops
being followed.

| Page | Read it when |
| --- | --- |
| [branching.md](branching.md) | you are starting work, or wondering which branch anything belongs on |
| [ci.md](ci.md) | a check is red, or you want to know what will run before you push |
| [changelog.md](changelog.md) | you are writing the entry your pull request owes |
| [promotion.md](promotion.md) | you are moving `dev` to `staging`, or cutting a release |
| [../../.github/rulesets/README.md](../../.github/rulesets/README.md) | you are changing what the server enforces |

The short version, which is also in `CLAUDE.md` because it is the part that must
never be looked up:

- **Branch off `dev`. Open the pull request against `dev`.**
- **Never push to `dev`, `staging` or `main` directly, and never force-push any
  of the three.**
- **`staging` and `main` only ever fast-forward to a commit that is already on
  `dev`.** They are pointers at tested history, not places work is done.
- **Releases come off `staging`, from a tag, cut by a person.** Nothing
  publishes on its own.
- **Every pull request carries a change entry** in `docs/changes/unreleased/`,
  and what it carries is not what shipped but **what somebody now believes
  wrongly**.
