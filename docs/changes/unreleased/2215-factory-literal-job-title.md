---
kind: fixed
title: codeaf keeps separate job titles and details as written
pr: 2215
surface: [engine]
invalidates:
  - "App job titles and details were joined and parsed as terminal chips, changing the title, budget and pipeline. Separate titles and details now stay literal."
---

Creating a job in the app or handing off an agent keeps the trimmed title
separate from its details. Words such as `auto`, `$5`, `security`, `two review
rounds` and `cheaper` change neither the budget nor the pipeline. The details
keep their original spacing and line breaks. Draft pull requests use the job's
title alone. Terminal factory sentences still interpret chips. Existing jobs
are not rewritten.
