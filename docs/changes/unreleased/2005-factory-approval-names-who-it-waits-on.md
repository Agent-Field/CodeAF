---
kind: added
title: An approve step can name who it waits on, a person or the factory's operators
pr: 2005
surface: [engine, docs]
invalidates:
  - "Was true: every approval waited on the job's person; on a team nobody else could answer it, and a pipeline could not say that an approval belongs to someone else. Is true now: an approve step (or a step of kind gate) may say `who: \"@handle\"` or `who: operator` in `.codeaf/factory.yaml`; without it the approval stays the job's person's. A job waiting at such an approval says so on the wire (`needs_you.who_can_answer` approver, `needs_you.approver`, each step's `who`), and only the named person (or any operator, for the role) may answer or approve it: the job's person is refused with reason `not_approver`, and `can.approve` and `can.answer` follow (for the operators' role the serving factory sets them for a caller it knows is an operator). A landed job's proof waits on whom its last approve step names. Neither Auto nor the manager answers an approval that names someone other than the job's person. A solo factory has one person, who answers everything. The pipeline editor sets it with `set_approver {name, who}` behind the feature `factory.pipeline_edit_approvers`; `who` is a runtime key, so it is previewed only for a file its factory attested."
---
