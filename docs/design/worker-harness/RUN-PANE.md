# The run pane: one look says what this is and where it stands

Approved by the owner 2026-09-18. It is the right side of the `work` tab
(`WORK-TAB.md`) for the selected run, and one of its lines reaches the chat's
rail. It builds on `TREE.md` and `CHAT-ROLE.md`. Existing words only, plus the
four labels below and `ask`.

## Who reads it

Three people, one pane, and the pane has to satisfy each without the others'
reading getting in the way.

1. **Back after a long gap, no memory of it.** Needs: what was this for, where
   is it, does it need me.
2. **Moving between many runs all day.** Needs: what changed since I last
   looked at this one.
3. **Checking what it did about something.** Needs: to ask, and to be shown
   where the answer came from.

## The pane, top to bottom

```
 rewrite the auth flow              ●●●●●◐○○○✘  8 of 14 · 1 failed
 from Sweeping the Frame Budget · started 2h ago · $0.41

 what   You asked to move login to short-lived tokens without breaking mobile.
 since  you looked 40m ago: the handler landed. The first middleware pass
        failed on empty claims and is being redone.
 now    Two tasks running: the handler's tests and the middleware retry.
 next   Tests, fixtures, the manual. Nothing needs you.

 your call
  ? keep the old table for a week?                       1 yes  2 no

  ▶ write the handler      12 steps            glm-5.3-flash
      $ go test ./internal/auth/...
  ○ write the tests        waits: write the handler
  ✔ 3 done

 models
  glm-5.3-flash  ███████░░░  $0.29 · 38 calls   work
  glm-5.3        ███░░░░░░░  $0.11 ·  6 calls   plan

 › ask about this run                                                   a
```

Order is the order of reading cost. The four sentences answer the first two
readers in five seconds. Under them are the facts every sentence is about, so
a sentence can be checked at a glance: `your call` first because it is the
only block a person can act on, then the tree with each task's model, then
the spend by model. The ask box is at the foot. A block with nothing in it is
not drawn.

## The four sentences

| Label | What it says | Written |
| --- | --- | --- |
| `what` | The person's goal in their own terms. Never the task's title. | Once, at the first summary; kept unless the ask changes. |
| `since` | What changed since this person last looked at this run: what landed, what failed and why, what was decided. | Every refresh. Absent when nothing changed. |
| `now` | What is happening this minute and what stands in its way. For a run that ended: how it ended. | Every refresh. |
| `next` | What comes after, and what it needs from the person, or `Nothing needs you.` | Every refresh. |

The laws that keep them honest and cheap:

1. **One small call on the worker model** (the owner's choice), through the
   conversation's own auxiliary door, so it is journalled and billed like
   every other errand.
2. **Bounded by its input, not by a ceiling.** It is fed the person's ask,
   the run's rows (title, state, first line of the result), the open
   questions, the last look and the lines it wrote last time. Nothing it can
   walk.
3. **Written only when the run has moved and somebody is looking**, and once
   when the run lands. The store keeps it beside a stamp of the run's shape;
   reopening a run that has not moved costs nothing.
4. **Every statement is about a row on the screen below it.** The page says
   so, and says to write less when a result is vague.
5. **Absent, never broken.** No key, a refused call, an answer that does not
   parse: the block is not drawn and the facts stand alone.
6. **One summary.** The last one written is the landing's digest line in the
   conversation.

The page the model reads is `internal/session/prompts/runsummary.md`. It is
short on purpose; the four rules that matter are: outcomes and not activity,
a failure named with its cause, nothing the dots already say, and at most
twenty five words a sentence.

## The ask box

```
 › what did it change in the handler?
   It replaced the session lookup with a token check and added refresh on 401.
   from write the handler · steps 7 to 11 · internal/auth/handler.go
   enter opens that task · ctrl+o continue in the chat · esc clears
```

- `a` opens it, because plain typing on this tab filters the list.
- **It reads and never writes.** One bounded turn on the worker model: the
  run's rows and results in front of it, and one verb to open a task's result
  and last steps, a few rounds at most. It is the chat's `ask about` rule
  (`CHAT-ROLE.md`) offered outside the conversation.
- **Every answer says where it came from**, and `enter` on that line opens
  the task's page.
- **It is thrown away.** The exchange lives in the pane, is written to no
  conversation, and goes when the pane closes.
- **`ctrl+o` continues it for real**: it opens the conversation the run came
  from with the question, the answer and the run attached. That link is the
  first edge of the graph of conversations.
- **A steer is recognised, never sent.** "tell it to skip the fixtures" is
  answered `that is a note to the run · enter sends it`, and the note is the
  same note row the task page writes.

## The rail

The chat's rail gains one thing: the `now` sentence under the run's dot row,
dim, two lines at most. The rail is twenty eight cells; anything more crowds
the tree it is there to show.

## Parked

Asking across all work ("what did we do about auth this week") is the view by
sentence of `WORK-TAB.md`, and waits for the graph of conversations.

## The cells

1. **the summary** (session): the stored summary, its staleness stamp, the
   bounded input, the call and its page, the parse, the landing line.
2. **the ask turn** (session): the bounded read-only turn, its one verb, the
   sources on every answer, the note that is recognised and never sent.
3. **the pane** (tui3, after the work tab lays out runs): the four lines, the
   blocks in order, the bars, the `a` key and the box, `ctrl+o`.
