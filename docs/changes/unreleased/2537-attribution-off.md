---
kind: changed
title: Attribution can be turned off again with the attribution row or CODEAF_ATTRIBUTION
pr: 2537
surface: [chat, engine, docs]
invalidates:
  - "codeaf always signed its commits, pull requests, issues and comments and there was no off; the attribution row and CODEAF_ATTRIBUTION are back, on by default, and off adds no marks anywhere."
  - "CODEAF_ATTRIBUTION or a profile's \"attribution\": false only printed a warning that the row was gone; both are obeyed again and print nothing, and the retired-row note is deleted."
  - "The only signing choice was attribution.model, whether Assisted-by names the model; that row still exists but only matters while attribution is on."
  - "Task landings, kept work and codeaf do or codeaf run end-of-run signing always added the two trailer lines unless CONTRIBUTING forbade them; with attribution off they add none and leave commit hashes untouched."
---
People kept asking how to stop codeaf adding `Assisted-by` and
`Co-Authored-By` lines to their commits. Signing stays the default. Turning the
row off in `/settings`, or setting `CODEAF_ATTRIBUTION=off`, removes every mark
codeaf adds, and the model is told not to add any of its own. The model cannot
change the row for the person. A CONTRIBUTING ban still wins with the row on.
