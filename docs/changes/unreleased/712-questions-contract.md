---
kind: added
title: one object for every decision the engine hands a person, and one door every answer goes through
pr: 712
surface: [engine]
invalidates:
  - "There was no session.Question. Thirteen engine lanes each raised, waited on and answered their own kind of question — consent, connect, connect key, harness offer, harness design, sub-harness proposal, a running sub-harness's own question, standing card, task proposal, a landed task's your call, merge conflict, fuel gate, stuck-turn recovery — and a surface wanting to draw them had thirteen shapes to learn. There is now one: session.Question, with session.Answer as its answer, and Agent.OpenQuestions() lists every one that is open right now."
  - "session.QuestionKind used to have exactly three values (consent, task, standing) and meant `the three lanes home can answer'. It now has twelve and means THE LANE a question came from, which is also the resolver its answer is applied through. The SHAPE of a decision — permission, choice, judgement, clarification, confirmation, landing, assumption, ratify — is the separate session.AskKind."
  - "session.Answer used to be four fields: At, Kind, ID, Key. Those four are unchanged and AnswerFromKey still answers from a bare key alone, so an older window's answers.jsonl line still reads — but an answer now also carries Picked, Change, Comments, AskedBack, Blanks, Dial, Reframe, DecidedBy, Scope and Why."
  - "session.AnswerOption used to be Key and Label. Those two are unchanged; it also carries Body, Consequence, Safe, Widening, Blocks and Dimensions now, all omitempty."
  - "Agent.ResolveConflict, Agent.TakeBackDecision and Agent.AnswerSubharness had no caller anywhere in the product — work could stop on a question nothing could draw, let alone answer. They are reachable through Agent.ResolveQuestion now."
  - "There was no standing subscription for questions. Agent.WatchQuestions() is one now: EventQuestion, EventQuestionWithdrawn and EventQuestionAnswered ride it, and it replays every question already open when a surface attaches. They deliberately do NOT ride the turn's stream (whose readers walk a strict sequence to its close) or the standing task lane (which is the roster's)."
  - "session.PresenceQuestion used to be four fields — Kind, ID, Text, Options — and still is for any reader that only knows those. It also carries Full *Question now, the whole question the lane raised."
  - "Agent.NeedsPerson() did not count a landed task's `your call'. It does now, so home, the switcher and the tab signal stop saying a conversation is idle while work waits on somebody's word about it."
  - "A session kept no record of what had been decided. It keeps decisions.jsonl in its own folder now; Agent.Decisions() reads it back, and Question.Check refuses a question a record already answers with `already decided: …'."
---

The audit behind this counted twenty-one distinct question mechanisms across
fourteen answer surfaces, three of which could draw an answer row at all. What
was wrong was not any one of them: it was that a person does not have thirteen
kinds of decision, they have one — somebody is asking me something, here is what
it is about, here is what I may say — and nothing in the engine said so.

Nothing a person sees changes yet. Every lane goes on emitting the event it
always emitted, in the same order, with the same fields; the new object is a
second, wider description of the same moment, which the surfaces take up one lane
at a time.
