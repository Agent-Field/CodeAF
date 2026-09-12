---
kind: fixed
title: the permission frame names its way out wherever the pointer stands
pr: 992
surface: [chat, docs]
invalidates:
  - "A grouped permission frame said `safe answer` on its `deny all` row ONLY while the
    pointer was standing on that row. It now says it on every frame it draws, wherever the
    pointer is. The mark is not a label for the pointer: `deny all` is the answer that loses
    nothing by construction — the frame forms only where every member has one
    (internal/tui3/questionset.go's questionGrantAndSafe), and the row sends each question
    its own — which is the same rule the panel of ONE has always kept
    (questionPanelOption marks any hands-only answer that loses nothing whatever the pointer
    is doing). The two spellings were one sentence while #933 opened every permission on the
    refusal, and diverged the moment #953/#985 graded the calls."
  - "The permission frame opened on `deny all` for a set of ORDINARY calls, and
    internal/tui3's TestPermissionsFromOneStepAreOneFrame asserted it. That was #933's
    deny-first-while-ungraded rule and it is no longer anyone's: since the gate grades
    (#953) the frame opens where its members' own pointers would, so four reads open on
    `allow all N` and `enter` allows them all once. A frame holding one call nobody graded
    still opens on `deny all`, and an irreversible call never joins a frame at all. The test
    now asserts the pointer the grading puts there and, as a SEPARATE claim, that the
    refusal is named though the pointer is not on it."
  - "internal/manual/chat/permissions.md's `Several approvals at once` section said the
    pointer starts on `deny all` `so enter on a frame you have not moved denies them all`,
    and drew it that way. It now reads the pointer off the stakes, the way the single-approval
    section above it already did. docs/design/questions/GALLERY.md said the frame opens on
    `deny all` `until the calls are graded (#953)` — that condition has landed; the screen
    captures under that caption are pre-grading records of a real run and the caption now
    says so. internal/e2e/tuiwords_test.go's questionGroupSafeWord needle was explained as
    `DENY-FIRST UNTIL THE CALLS ARE GRADED`; the needle itself is unchanged and still waits
    for `safe answer` on four ordinary reads."
---

#946 and #985 landed a day apart and disagreed about one row. #946 was cut before the gate
graded its calls, so marking the refusal only under the pointer was, at the time, the same
sentence as marking it always. #985 then moved an ordinary call's pointer onto `allow all`,
and the mark went missing from precisely the frames where it matters most — the pointer
standing on the act, with nothing on screen naming the way out. Neither pull request was
wrong on its own; what was wrong was the merge, and both halves of it were stale.

internal/e2e's live assertion is what says this was a defect in the frame rather than only
in a test: it waits for `safe answer` on a frame of four ordinary reads, so the next live
run of TestQuestionsE2E would have gone red.
