---
kind: changed
title: codeaf always signs what it writes, and names only the bare model in `Assisted-by`
pr: 1412
surface: [chat, engine, resident, docs]
invalidates:
  - "The `attribution` settings row and `CODEAF_ATTRIBUTION` turned all signing off. Both are retired: every commit, pull request, issue and comment codeaf writes is signed. A profile that still says `attribution: false`, or a shell that sets the variable, is told once at start that the row is gone and that `attribution.model` is the part that can still be turned off. It is not obeyed and not silently ignored."
  - "There was no way to keep the signature but drop the model. The new `attribution.model` row (`CODEAF_ATTRIBUTION_MODEL`, on by default, labelled `model in commits`) does that: on, `Assisted-by: CodeAF (<model>)`; off, `Assisted-by: CodeAF`. The one commit that never names a model is a task landed on the worker harness, the belt tasks run on by default: a run's record names no model, so that landing carries the bare line either way."
  - "`Assisted-by` carried the full router id, for example `Assisted-by: CodeAF (deepseek/deepseek-v4-flash)`. It now carries the bare model, `deepseek-v4-flash`: the provider or company prefix and a routing suffix such as `:free` or `:nitro` come off, and the model's own version or date stays. One function, `exec.BareModelName`, does it for every writer of the line."
  - "The harness's own landing commits, the run engine's landing and the leaf loop's contract wrote the `Co-Authored-By` line alone, while the chat told the model to write two lines. Every commit codeaf writes now ends with one blank line, `Assisted-by`, then `Co-Authored-By`, and nothing else. A landing on the older node belt names the model the task ran on; the run engine's landing, which is the worker harness's and records no model, writes the bare line."
---

A repository's own CONTRIBUTING policy against AI trailers still wins. That is
the repository's rule and not a person's setting, and the law still says to
leave the marks out and say so.
