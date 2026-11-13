---
kind: fixed
title: A saved pipeline is what its author wrote, and a step edit before Start keeps the repository's steps
pr: 2478
surface: [factory, app, engine]
invalidates:
  - "pipeline.Encode wrote every shipped step, every default pipeline, fixed: and rules: [] into every saved file (the AI preview, a pipeline pull request, the factory's copy). Now it writes only steps that differ from the shipped ones, pipelines the file named or changed, and fixed/rules when said."
  - "Editing a step of a job that had never run made the built-in steps the job's own, so it ran them and dropped the repository's pipeline. Now lease.Doors.EditSteps stamps the repository's steps first (pipeline.StampRecipe) and the edit lands on them."
---
