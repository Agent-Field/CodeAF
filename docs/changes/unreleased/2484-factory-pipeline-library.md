---
kind: added
title: Named pipelines as edits, and a factory pipeline library Start and routing rules pick from
pr: 2484
surface: [factory, engine, app, docs]
invalidates:
  - "Named pipelines could not be made from the app or the wire: factory.pipeline_preview had no op for it and add_step to a new pipeline was refused as unknown_step (\"That is not a step in this pipeline\"). Now add_pipeline {name, steps|from}, remove_pipeline {name} and rename_pipeline {name, to} are edits behind hello feature factory.pipeline_edit_pipelines; pr, ci and issue are refused as names, and a refusal about a missing pipeline says \"That pipeline is not in this file\"."
  - "A pipeline lived only in one repository's .codeaf/factory.yaml (or the factory's copy of it), and the only reusable pipelines were the desk's per-Mac My pipelines. Now the factory keeps a pipeline library (pipeline: and steps: only) behind factory.library, factory.library_set (operator only) and factory.library_preview, stored in the factory's store as factory-library.json."
  - "factory.job_start {pipeline}, a routing rule's then.pipeline and a job's own kind resolved only against the repository's file. Now one function, pipeline.File.Resolve, resolves every name: the repository's file (the factory's copy when kept), then the library, then the built-in steps; a repository pipeline of the same name wins. factory.pipeline carries pipeline.pipelines, every choice with from repo, factory_copy, library or built_in."
  - "Encode wrote a shipped step's inherited until and max beside a prompt the file changed, and wrote steps: {} for a file with no steps of its own. Now it writes only what the file wrote."
---

With no library and no routing rules, GitHub jobs run exactly what they ran before.
