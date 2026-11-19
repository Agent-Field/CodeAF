---
kind: fixed
title: the line a reply check says as it sends a reply back reaches the desktop app and is kept with the chat
pr: 2851
surface: [engine, app, chat]
invalidates:
  - "Was believed (qloop run26 report): the reply check (#2848) never fired in the canvas path, because its line 'its reply said more than it checked this turn' was in no screenshot and no kept engine file. Is true: it fired on 15 of run26's 28 turns (6 contradicts, 3 unshown, 6 stale-look; see the `rule … sent-back` lines in each `.cell/transcript.jsonl`). The app dropped every check's line by its words, and the engine writes no notice into a transcript."
  - "Was true: the desktop app shows no row for a check on a finished reply (#2836, #2848's change entry, tasks.md). Is true now: `Agent.sendBack` types its line `NoticeKind` `sent_back`, and `codeaf app` sends it as a quiet `notice` entry with `notice_kind: \"sent_back\"`, after the answer it sent back, kept in the app sidecar (`app-footers.json`) so a reopened chat has it. The completion check's carry-on and the nudges are still left out; untyped carry-on words are still dropped."
  - "Was true: the reply-check reader got the mark reader's 30-second window. Is true now: it has its own 60-second window (`replyCheckWindow`). Two of run26's 24 reads were cut at 30.0s and their answers lost; the rest took 2 to 29.6 seconds. It is still one small-tier call per turn."
  - "Was believed (run26 judge A): café-site t2 'rendered all four pages at phone width' and t3 'Checked at 1200px and at phone width' were claims with only desktop looks behind them. Is true: both turns rendered at 390px with `canvas render --width 390` and looked at those files with `view_image`. The judge was handed only `canvas look` images as the agent's looks."
---

The desk still has to place a `sent_back` line with the work it started, not under
the turn's final answer, and should fold away the answer it replaced. PR #2851's body
says exactly what the desk lane needs.
