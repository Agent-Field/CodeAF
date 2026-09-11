---
kind: fixed
title: every question is raised and settled through one door, and answering leaves the prompt alone
pr: 910
surface: [engine]
invalidates:
  - "Answering a question used to rewrite message[0]: ResolveQuestion wrote the decision
    record into the system prompt, so the next request paid the uncached rate for the whole
    conversation, and every tool consent — every `allow once` — took that road. It does not
    any more. The record in message[0] is a snapshot, brought up to date only when that
    message is rebuilt for a reason of its own (a folder attached, a standing order agreed,
    the clock brought forward); `decisions.jsonl` is read once when the conversation opens
    and re-rendered off a.mu by recordDecision. What the model reads about the decision it
    just got is the answer's own result, and the gate (Question.Check) still reads the whole
    file."
  - "Five lanes — connect, harness (offer and design), subharness, subharness-ask and fuel —
    used to reach the questions stream only through the OpenQuestions replay, and never
    emitted EventQuestionAnswered or EventQuestionWithdrawn. Every lane now raises through
    Agent.raiseQuestion and settles or withdraws on the lane, so any window, home's needs-you
    row and the status chip clear when the question does. A structural law
    (internal/session/questionlaw_test.go, on make test-laws) fails the build if a lane
    emits EventQuestion or banks words itself."
  - "The QuestionRecovery kind, Agent.ResolveRecovery, RecoveryChoice and askAboutLoop are
    DELETED. The stuck-turn question borrowed the consent gate's wait with an empty question
    and had no caller left; the revert helpers and the change ledger behind it stay as
    explicit operations. consent's askAnswer no longer takes `memo` or a question line: every
    approval question is about a tool, offers `always`, and is answerable from any window."
  - "A ratify no longer waits. AskKind.Waits() is the one reading of it: the `ask` call comes
    straight back, the turn carries on, the question stands until somebody looks at it, and
    presence does not count it as the session being stopped on somebody. A surface counting
    open questions should ask the same predicate."
  - "Asking back on the model's own `ask` used to be queued as a steer and read only after
    the tool returned, i.e. after the question was answered, so the design's 'the question
    stays open' could not happen. An answer carrying only AskedBack now returns the call with
    those words, leaves the question open, writes no record and announces nothing as
    answered; the person's answer reaches the model as a message when the call is gone. The
    gate refuses a second copy of a question that is still open with `already asked and still
    open:`."
  - "The `ask` schema's enums are now written from the Go constants. pick.confidence offered
    low|medium|high while the code reads sure|fairly|unsure, so a model's confidence never
    reached a screen; the `input.pairs` PROPERTY was advertised with no Go field behind it
    and is gone — a pairs question pairs the options, which stay in `options`, so the kind
    itself is unchanged; input.blanks and input.dial now carry their shapes, and subject.kind
    carries `order`."
  - "A KEY YOU TYPE IS NEVER WRITTEN DOWN. An answer to a question whose input is a secret —
    the API key for an account you are connecting — used to be recorded in decisions.jsonl as
    the answer's `change`, rendered into message[0] as `with: sk-…`, sent to the provider on
    the very next request, and carried whole on EventQuestionAnswered to every attached
    surface and every `--host` frame. It now goes to the lane that asked for it and nowhere
    else: Agent.ResolveQuestion strips every typed field from the answer as it leaves, keyed
    on Question.SecretAnswer(), after the lane has been handed the real words. The record
    still says an account was connected, when, and by whom."
  - "AN ANSWERED QUESTION USED TO COME BACK ON EVERY ATTACH. internal/remote's waiting room
    dropped a card only when that card's own resolve-door was called — a line apiece in
    MethodConsent, MethodStandingResolve, MethodHarness, MethodConnect, MethodConnectKey —
    and since the chat began answering every lane through the one door MethodQuestionResolve,
    which had no such line, nothing dropped anything. A standing proposal answered at 14:55
    was on the block again forty minutes later and after every tab switch, conversation
    switch and new window, with the conversation reading `waiting · your call` and home
    counting it in `want you`. The room is now reconciled against the engine's own
    OpenQuestions every time it is read or counted, and the six per-door lines are deleted."
  - "ONE READING OF WHAT WAITS. Question.Waiting() — AskKind.Waits() and Blocking.Blocks()
    together — is what the waiting desk, the presence file and the open-question cap all ask.
    The desk answers with its oldest row, so a standing ratify beside a bash approval put the
    RATIFY's sentence on home under `waiting on you` and a key press there answered the
    ratify. A standing card now says the turn is stopped on it, which it always was, and an
    `ask` whose call this engine parks says so whatever its arguments wished for."
  - "A QUESTION THAT OUTLIVED ITS CALL IS RETIRED WITH ITS TURN. An unanswered ratify and a
    question somebody asked back on and never returned to had no withdrawal trigger at all,
    so they stood in OpenQuestions, on the presence desk and against QuestionCap for the rest
    of the session, about a turn that ended long ago. Both are now taken back when the turn
    ends, with `the turn moved on without it`. What may stay is one predicate,
    questionOutlivesTurn: the new session.Question.Later (additive, omitempty — the `ask`
    tool sets it when the call says nothing is blocked on the turn) or a task waiting on it."
  - "session.Question carries Batch, the token every question raised by ONE step of a turn
    shares (Agent.stepToken). The approval gate and the model's own `ask` set it, so a
    surface can group three approvals from one tool batch and answer them as one thing."
---

The measurement behind the first line, on the Spark with deepseek/deepseek-v4-flash: the
request after an `allow once` reported 13,568 of 13,861 prompt tokens served from the
cache, across an answer that used to move the first byte of the prompt.
