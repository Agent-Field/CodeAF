# Documentation ownership

Keep each fact in the document used by its reader. Link to the owner rather than
copying a second specification that can drift.

## What belongs in this public repository

| Kind | Location | Reader and contents |
| --- | --- | --- |
| Product knowledge | `internal/manual/chat/`, `internal/manual/pages/` | Users and codeaf's manual lookup: capabilities, workflows, commands, keys, defaults, limits, refusals and recovery |
| Developer knowledge | [ARCHITECTURE.md](ARCHITECTURE.md) and linked engineering docs | Contributors and future coding agents: entry points, ownership, data flow, interfaces, invariants, tradeoffs and validation |
| Contributor guidance | [CONTRIBUTING.md](../CONTRIBUTING.md), [AGENTS.md](../AGENTS.md), [rules](rules/README.md) | How to change, test, review and release this repository |
| History | [Historical designs](history/README.md), [change entries](changes/README.md), [changelog](../CHANGELOG.md) | Superseded decisions, changed assumptions and release records |

The root README introduces the product; [GUIDE.md](GUIDE.md) helps a person start.
Neither should duplicate the complete command reference or developer architecture.
Users' learned chat knowledge belongs in their profile database, not in
this repository's manual or contributor docs.

Public docs should describe reproducible behavior and public interfaces. Keep
credentials, personal profiles, private infrastructure details, local run logs,
and task-specific scratch notes out. Put PR-specific validation evidence in the
PR rather than adding an evidence directory to the source tree.

## Authority and scope

Implementation and observable tests determine what a build does. The manual must
accurately explain that behavior for its own surface. A maintained contract such
as [HEADLESS.md](HEADLESS.md) describes promises callers depend on; disagreement
with implementation is a defect to resolve, not permission to ignore the contract.

Design rationale explains why. It must distinguish implemented behavior from a
proposal, and chat from resident. Historical records explain what used to be true;
they do not override the current manual or contracts.

A current engineering reference should open with its scope and role. For an old
page, add a visible historical or superseded notice and a replacement link. Keep
existing links usable when reorganizing; avoid renaming pages for capitalization
alone. When an architecture changes, update the current map and preserve useful
superseded reasoning under `docs/history/`.

## Update documentation with the change

1. Update the relevant product manual when behavior changes; keep chat and resident separate.
2. Update any affected interface contract, operational procedure or architecture map in the same PR.
3. Add a change entry describing the result and any invalidated assumption.
4. Check relative links and follow the relevant [CI checks](rules/ci.md); manual edits also need [manual validation](../internal/manual/README.md#validate-a-manual-change).

Do not use a release fragment as the only place that explains current behavior.
Do not copy a long manual page into `docs/` just to make it discoverable: add a
link from the [documentation map](README.md).
