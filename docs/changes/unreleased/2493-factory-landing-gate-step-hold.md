---
kind: fixed
title: Runs never land past an approve step, Manual holds read as steps, removed pipelines drop steps
pr: 2493
surface: [engine, factory, app, docs]
invalidates:
  - "An approve step that was a run's last step passed itself on landing (marked done locally, left pending on a worker) and the proof approval published past it. It now lands waiting, only the proof approval passes it, and an approval is refused while any other approve step is unpassed: `has an approve step nobody has passed yet: <step>`."
  - "Manual's hold before a step read on the wire as needs_you.kind approve_plan, gate plan, reason approval_plan, approval kind plan. It is now approve_step, gate step, reason approval_step, approval kind step, with needs_you.step naming the step by its label; send back and Auto passing the hold are unchanged."
  - "remove_pipeline kept the definitions of the steps the removed pipeline listed. It now drops those only it used, keeping any another pipeline lists, the fixed list names or codeaf ships."
  - "An add_step clashing with a step the file already defines with other words reached the app as \"That value is out of bounds.\" It now says \"There is already a step named <step> with other words. Pick another name, or add it with the words it has.\" in its place."
  - "After the manager marked a pull request ready it could still say merge was not offered (repeating its draft-era status) while the job page offered Merge. The mark-ready answer now reads the job again and says what the page offers, and the manager's status, its press and the page's merge card all read one merge check, factory.MergeOffered."
  - "factory_status named steps by their stored key (`followpr`, `runvetand`), and the manager repeated them. It now names a step by its label, or Follow PR for the follow step."
---

Found in the live desk walk (#176 published PR #179 with its approve step "to come").
Auto still passes a routine, non-fixed last approve step through its recorded proof
approval, as before; it no longer does so by landing past it. Desks must learn the
`approve_step` kind and the `step` gate.
