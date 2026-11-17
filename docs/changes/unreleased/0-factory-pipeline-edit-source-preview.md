---
kind: added
title: The engine reads a repository's pipeline file at a pinned commit and previews a structured edit
pr: 0
surface: [engine]
invalidates:
  - "The earlier pipeline pull request change said the file the edit starts from had no commit and could be the host's checkout, the markdown recipe or the defaults. `GET /v1/repos/{owner}/{repo}/pipeline/edit-source` and `factory.pipeline_source` now read the raw `.codeaf/factory.yaml` of the DEFAULT BRANCH at one pinned commit with the caller's own GitHub credential, and say `missing`, `invalid` or `too_large` instead of offering defaults as the file. `factory.pipeline` is unchanged and is still not an edit base."
  - "Whoever edited the file had to hand-write YAML and could not know what the pull request route would commit. `factory.pipeline_preview` applies `set_step` and `set_budget` with the route's own parser and encoder and answers the exact canonical text, whether anything really changed, and whether comments or layout are rewritten."
  - "The source did not say whether the repository is archived, and `can_push` alone read as 'may open a pull request'. `factory.pipeline_source` now always carries a boolean `archived` (false included); the engine refuses an answer without it. A pull request is possible only when `can_push` is true and `archived` is false."
  - "A pipeline file the parser accepts could name a step the runner silently drops (more than 12 letters once hyphens are removed, or `lint-pass` beside `lintpass`). The editor now reads such a file as `invalid` and refuses to edit it; the parser and the runner are unchanged."
---

A distinct optional feature, `factory.pipeline_edit`
(`FactoryCapabilities.pipeline_edit`), adds two methods that write nothing:

- `factory.pipeline_source {factory, repo}` makes three GETs through the
  factory (repository, default-branch tip, the file at that commit) and
  answers `{path, default_branch, base_sha, state, editable, text, the parsed
  pipelines, steps and default budget, fixed}`. Team never falls back to a host
  token; a solo host may use its own profile token, as the pull request route
  already does. It reads nothing from the host's checkout or personal file and
  takes no parameter. The engine parses `text` itself and refuses an answer
  that disagrees.
- `factory.pipeline_preview {factory, repo, base_sha, base_text, edits[]}`
  takes the file exactly as the source gave it and exactly two kinds of edit,
  `set_step {name, prompt?, until?, max?}` and `set_budget {value}`. Unknown
  fields, locked steps (`approve`, anything under `fixed`), unknown names and
  out-of-bounds values are refused, with a typed `detail`. The answer is the
  canonical text the pull request route would commit, byte for byte; `changed`
  compares with the base as the parser reads it, so reformatting alone is not a
  change and the text is then the base unchanged; `format_rewritten` and
  `comments_dropped` say what the commit would rewrite; `steps[]` and `budget`
  summarise what changed. It asks only that the caller still holds the
  connection.

Opening the pull request is still `factory.pipeline_pr`, with the preview's text
and the source's `base_sha` as `expected_base_sha`. There is no reorder, add or
remove of a step, no per-step model, effort or budget, no `only_if` and no loop:
the file format holds none of them. `pr: 0` because no pull request is open yet.
