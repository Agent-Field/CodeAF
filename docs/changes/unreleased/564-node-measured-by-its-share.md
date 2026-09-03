---
kind: fixed
title: a node is measured by the material it will read, not the file its words mention
pr: 564
surface: [engine]
invalidates:
  - "a node that names a file is measured by that whole file's size — no longer true. A bare file name still weighs the whole file, but a name followed by a scoping mark weighs only the material selected by a line range, heading count, record or line count, or named block or heading."
  - "a correctly divided lane over a shared file can be corrected beyond reach because the file is larger than one worker's window — no longer true. `correctBeyondReach` never overrules an atomic node that names a file a sibling work node — one under the same parent — names too, whether the source scopes the file or names it bare; material no sibling names is still weighed on its own, so a node alone in naming a whole file past the window is still corrected and still journals `RefusalBeyondReach`."
  - "the planner's reach measurement exists only on #425's branch — no longer true. Its corrected core is on `dev`; #425 rebases on it and retains the spine, grounding, and prompt half."
---

**THE LAW IS THAT WHAT IS MEASURED IS THE MATERIAL THE NODE WILL READ, NOT THE
FILE ITS WORDS MENTION.** The first reach measurement read a file name from each
lane's source line, stat'ed the whole file, and overruled the sizer. Every lane
correctly divided over one large file was therefore stamped oversized with `its
named material exceeds what one worker holds` and could not divide further. The
plan-gate experiment measured that as the largest source of unrecoverable draws:
18 of arm C's 21 front draws (`docs/design/plan-gate-doe/REPORT.md`). That
measurement was introduced on PR #425's branch; this PR lands its corrected core
on `dev`, and #425 rebases on it while keeping the spine, grounding, and prompt
half.

`internal/plan/reach.go` now treats a source as scoped when its file name is
followed by a colon, em dash, en dash, dash-space, open parenthesis, or open
bracket. A bare name still weighs the whole file. Within a scope, four readings
are measured from the file's own bytes: a line range; a count of headings; a
count of records or lines, valued as that many mean lines of the file; or a named
block or heading matched by equality and continuing to the next heading of the
same rank. A heading count reads the rank its scope spells — the `##` of
`` `## chapter N: …` `` — so it charges the headings named and no others. A
heading named as a LINE — "the `# Handbook` line" — weighs that line and not the
section under it, and a heading whose section would be the whole document (a
rank-one title over lower-ranked sections) is not a share reading at all: it has
said nothing narrower than the file, so it is left unmeasured rather than
charged whole through a scope. A
scoping mark with no words after it is not a scope, and the name stands bare. A
file mentioned more than once is weighed by the largest of its mentions, so the
order the sources were written in is not a measurement. Anything else is not
measured, and a file nothing could weigh is still a file the node names.

`correctBeyondReach` does not overrule an atomic node that is a lane of a
division, and the signature of a division is that a sibling work node — one
under the same parent — names the same file, scoped or bare. That is measured rather than assumed: asked at the
plan door for three lanes over one handbook, the model sized every lane atomic
and wrote every lane's source as the bare name `HANDBOOK.md`, with the lane's
share said in the summary; three atomic siblings naming one file cannot each be
holding the whole of it, and the sizer had each summary in view when it judged
them. Material no sibling names is still weighed on its own, so a node alone in
naming a whole file past the window is still corrected and still journals
`RefusalBeyondReach`. `JudgeSplit` reads the same measure, so correction and
division cannot disagree about what a node will read. The three doors now pass
the workspace into that measurement: `aforge plan -w`, `planSubtree`, and
`replanRemainder`.

One edge is deliberate and documented at the seam: a scope can say the whole
file in words — `register.txt (all three blocks, every byte except the dates
unchanged)` — and words no arithmetic reaches are not weighed at all, so that
node is left to the sizer. An over-estimate is still a guess, and this pass does
not guess.
