---
kind: fixed
title: the permission frame names its way out wherever the pointer stands
pr: 992
surface: [chat, docs]
invalidates:
  - "A grouped permission frame said `safe answer` on its `deny all` row ONLY while the
    pointer was standing on that row. The mark is not a label for the pointer — it names the
    way out — so it is now drawn wherever the pointer stands, on any frame whose members
    would each mark their own refusal when asked alone. `deny all` is the answer that loses
    nothing by construction: the frame forms only where every member has one
    (internal/tui3/questionset.go's questionGrantAndSafe) and the row sends each question its
    own. The two spellings were one sentence while #933 opened every permission on the
    refusal, and diverged the moment #953/#985 graded the calls."
  - "Whether a question's refusal says `safe answer` was decided in two places that had
    already drifted apart: internal/tui3/questionpanel.go asked
    `option.Safe && questionHandsOnly(q) && q.Pick == nil` on a panel's own rows, and
    questionset.go asked about its pointer on the frame's `deny all`. There is now ONE
    predicate, questionMarksSafe, folded over a set by questionSetMarksSafe exactly as
    questionGroupStart folds the pointer's rule; the word itself is read only through
    app.questionSafeAside in questionpanel.go, and TestTheSafeAnswerIsMarkedByOnePredicate
    fails any file that spells questionSafeWord for itself. The visible consequence: a frame
    whose member carries its asker's pick, or whose calls are plainly reversible, no longer
    marks a row that its own tabs leave bare after `2 one by one`."
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
    section above it already did, under a heading in the asker's own words
    (`If I press enter on a frame of approvals, does it allow all of them or deny them all`).
    questions.md said the same thing twice — in its pointer paragraph and in the `enter` row
    of `Every key on a question` — and both now split the MARK from the POINTER. What
    `safe answer` means has its own heading rather than a clause inside the grave case, since
    it is drawn on every approval, ordinary ones included. docs/design/questions/GALLERY.md
    said the frame opens on `deny all` `until the calls are graded (#953)` — that condition
    has landed; the screen captures under that caption are pre-grading records of a real run
    and the caption now says so. internal/e2e/tuiwords_test.go's questionGroupSafeWord needle
    was explained as `DENY-FIRST UNTIL THE CALLS ARE GRADED`; the needle itself is unchanged
    and still waits for `safe answer` on four ordinary reads."
  - "Nothing checked a permission FRAME's pointer against the grade the consent gate
    actually stamps — the live suite's only needle for that screen is the mark, which is
    deliberately pointer-independent. internal/tui3's
    TestTheFrameOpensWhereTheEngineGradedTheCalls now does, without a model: the grave
    control is the gate's own question read from internal/session/testdata/consentask-rm-rf.json
    (which must refuse to join a set at all) and the ordinary answers are
    session.AnswerOptions' own, so neither half is a fixture that invented its own grade."
  - "internal/tui3/questionset.go and TestAGroupedPermissionOffersNoLifetimeRow both said the
    grouped permission's lifetime row `comes back here with the grading (#953)`, the test
    adding `THIS TEST FAILS THE DAY THE GRADING LANDS`. The grading landed as f3a734ba1, the
    row did not come back and nothing went red, because only a comment was guarding the
    promise. Both now state what the code does; whether the row is owed is issue #995.
    docs/design/questions/DESIGN.md carried the same promise in its `tabs` bullet, and its
    `pointer` bullet still described the pre-grading deny-first rule."
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
