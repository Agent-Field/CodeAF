---
kind: added
title: The pipeline editor renames a step everywhere the file lists it
pr: 2126
surface: [engine]
invalidates:
  - "Was true: a step in a repository's pipeline file could not be renamed from the editor; a new name meant adding a second step and removing the first. Is true now: rename_step {name, to} renames it in every pipeline that lists it, in the required list and in its definition, behind the feature factory.pipeline_edit_rename; approve and proof are never renamed from or into."
---
