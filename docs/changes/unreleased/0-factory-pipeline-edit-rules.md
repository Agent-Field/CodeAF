---
kind: added
title: The engine edits a repository's own rules, and a repository that has no pipeline file yet
pr: 0
surface: [engine]
invalidates:
  - "The source-preview change entry 0-factory-pipeline-edit-source-preview.md said a missing `.codeaf/factory.yaml` refuses editing. Behind the new feature `factory.pipeline_edit_rules` a `missing` source is `editable: true` with `text: \"\"` and describes the shipped default pipeline the repository runs without a file (`budget_in_file` false, every pipeline and step `in_file` false); `factory.pipeline_preview` with `base_text: \"\"` applies the edits to those defaults and answers the whole canonical file the pull request then creates (`factory.pipeline_pr` already created a file the default branch did not have). Only `\"\"` means no file; a base of blanks is still `base_invalid`."
  - "The edits could not reach a repository's own `rules`, `start.ask_first` or `start.never`. `set_list {list, lines}` now replaces one of them whole, and `factory.pipeline_source` carries all three (the file's OWN lines, never the factory's house rules) for the states `file` and `missing`."
---

A distinct optional feature, `factory.pipeline_edit_rules`
(`FactoryCapabilities.pipeline_edit_rules`), is announced beside
`factory.pipeline_edit`. Without it every answer is what it was: no lists on a
source, a missing file not editable, and `set_list` or `base_text: ""` refused as
`bad_params` before the engine is asked.

- `factory.pipeline_source` gains `rules`, `ask_first` and `never`, string
  arrays, always present (`[]` when empty) for the states `file` and `missing`,
  absent for `invalid` and `too_large`. The engine reads them from the file
  text itself; nothing the factory says about rules is used. For a missing file
  the engine also describes the defaults itself, so the source and the preview
  of `""` can never disagree; a factory that calls a missing file editable, or
  sends a parse for one, is still not believed.
- `set_list {op, list, lines}`: `list` is `rules`, `ask_first` or `never`;
  `lines` is required, every line the list holds after the edit, `[]` to clear
  it. Each line is trimmed and must be one non-empty line of at most 300
  characters; a list holds at most 40 lines, is set once per preview, and holds
  a line once (ignoring case for `rules` and `ask_first`, which the parser reads
  that way, exactly for `never`). A path such as `auth/` or `infra/**` is
  written as given. `set_list` takes no other field, and no other verb takes
  `list` or `lines`. Refusals are `bad_value` and `bad_op`.
- The preview answer gains `lists: [{list, before, after}]`, one entry per list
  whose lines changed, in the order rules, ask_first, never. It is present
  (possibly `[]`) whenever a `set_list` was sent and absent otherwise, so an
  older preview answers byte for byte what it did. For `base_text: ""`,
  `format_rewritten` and `comments_dropped` are false, and an unchanged preview
  answers `text: ""` with `changed: false`.
- A missing read the factory attested (`runtime_fields: true`) is remembered as
  the text `""`, so `when`, `effort` and `fanout` edits of a file that does not
  exist yet take the same seal as edits of one that does, and the pull request
  re-reads the source and refuses if a file has appeared at that commit since.

The file the pull request creates is this engine's shipped defaults plus the
edit, every shipped step written out, so the repository runs what it ran before
and the edit besides. `pr: 0` because no pull request is open yet.
