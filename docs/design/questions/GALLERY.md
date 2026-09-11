# The questions wave, photographed

Every picture below is a **real screen**: `bin/aforge` in tmux at 100×36, talking
to `deepseek/deepseek-v4-flash` over OpenRouter, with the glyph vocabulary on its
plain tier. They are captured and rendered by `internal/e2e/questions_e2e_test.go`
(build tag `e2e`) — nothing here was mocked, staged or drawn by hand, and re-running
that suite rewrites this folder:

```sh
make build
go test -tags e2e -run TestQuestionsE2E -count=1 -timeout 90m -v ./internal/e2e/
```

Each caption says the moment and the keys that got there. `docs/design/questions/DESIGN.md`
is the contract these are evidence about; `internal/manual/chat/questions.md` is what
the running chat says about the same rows.

A picture with **✗** on it is a screen that does NOT keep the contract. Those are
the findings the suite is red on, and they are here for the same reason the rest
are: what a lane owes next is easier to see than to describe.

---

## The line

The smallest form. One row for the question and its answers, the reason dim under
it, the subject's own row above where there is one.

![the line, pending](screens/a-line-asks-and-the-keys-answer-it-pending.png)
*A permission raised by the model's `ask`. Nothing pressed yet — the mark is amber, the
chip on the status line reads `? 1 question · alt+a`.*

![folded to the chip](screens/a-line-asks-and-the-keys-answer-it-folded.png)
*After `esc`. Nothing is cancelled: the rows fold away, the turn stays paused on the
question, and the chip goes on counting it. A digit typed now goes into the box.*

![the receipt](screens/a-line-asks-and-the-keys-answer-it-receipt.png)
*`alt+a` brought it back and `1` answered it. The dim line stays where the question
was — the same sentence that goes into `decisions.jsonl`.*

## The card

Head, the reason and who is asking, a row per answer with what it costs, then the
answers row.

![the card, counting down](screens/a-card-counts-down-to-its-pick-and-enter-takes-it-clock.png)
*A choice under `/autonomy choice recommend 30s`. `▸` marks the asker's pick — a
recommendation, never a cursor — and the tail says which answer is about to be taken
and when: `start it in 27s · your rule`.*

![enter took the pick](screens/a-card-counts-down-to-its-pick-and-enter-takes-it-taken.png)
*`enter`. The record says `you`, because a key was pressed.*

## The room

A page over the conversation, in the task-room idiom. The turn under it keeps
streaming; `esc` restores the conversation with its scroll untouched.

![the card before it is opened](screens/the-room-compares-annotates-asks-back-and-sends-card.png)
*Three answers with bodies, blocks and dimensions behind them, so the block offers
`[o] open it`.*

![the room](screens/the-room-compares-annotates-asks-back-and-sends-open.png)
*`o`. The first answer open with its body, its consequence, the pick's reason and
`would switch if …`; the other two folded. The foot says `nothing chosen yet` rather
than offering an `enter →` line with nothing behind it.*

![compare](screens/the-room-compares-annotates-asks-back-and-sends-compare.png)
*`x`. The table is built from the asker's own dimensions and says `only what differs
is here`.*

![a comment on one answer](screens/the-room-compares-annotates-asks-back-and-sends-comment.png)
*`x` to close, `c`, words, `enter`. The note sits under the answer it is about and the
foot counts it: `nothing chosen yet · 1 noted`.*

![one question back](screens/the-room-compares-annotates-asks-back-and-sends-askback.png)
*`?`, words, `enter`. One exchange per answer, and the prompt says so.*

![the answer composed](screens/the-room-compares-annotates-asks-back-and-sends-composed.png)
*`2`, then words typed into the box, which stayed live the whole time. The foot is
what `enter` would send: `answering 2 sqlite · with … · 1 noted`.*

![answered](screens/the-room-compares-annotates-asks-back-and-sends-answered.png)
*`enter`. **✗** Two lines for one decision: the block's receipt and the room's own foot,
differently spelled. One of them is owed a deletion.*

![the answer reaches the asker](screens/the-room-compares-annotates-asks-back-and-sends-reply.png)
*The model's next turn, with the pick and the typed words in the tool result behind it.*

## Fill in the blanks

![the card first](screens/a-sentence-with-holes-is-filled-in-card.png)
*The block draws the two answers and offers `[o] open it`, because there is a shape
behind this one that a single row cannot hold.*

![holes with their own names](screens/a-sentence-with-holes-is-filled-in-open.png)
*A clarification whose input is `blanks`. Each hole wears the name the asker gave it,
one dim line says what the hole under the cursor takes, and the foot says `enter when
it reads right` — there is nothing here to choose.*

![filled](screens/a-sentence-with-holes-is-filled-in-filled.png)
*Typing, then `tab`, then typing.*

## Pick several

![a checklist](screens/a-checklist-ticks-several-answers-open.png)
*`space` is on the row now, ranked with the answers rather than sixth in the give-up
order — before this wave a checklist at a hundred columns drew as four plain rows with
no verb that works them.*

![two ticked](screens/a-checklist-ticks-several-answers-ticked.png)
*`space`, `↓`, `space`. The foot lists what is ticked, in the order it was given.*

![the record](screens/a-checklist-ticks-several-answers-receipt.png)
*`enter`. Every ticked answer is in the line, and it says `you`.*

## This or that

![a run of two-way questions](screens/pairs-are-answered-one-row-at-a-time-open.png)
*One pair at a time, with `[a] the first` and `[b] the second` on the row.*

![answered, and walked on](screens/pairs-are-answered-one-row-at-a-time-answered.png)
*`a`. The next pair is up.*

## A dial

![a dial reads as words](screens/a-dial-is-moved-with-the-arrows-open.png)
*The asker's labels, not a bare number, with the current notch in brackets.*

![moved](screens/a-dial-is-moved-with-the-arrows-moved.png)
*`→` twice.*

## Ratify — the ladder's third rung

![already done](screens/a-ratified-act-says-what-it-did-and-how-to-undo-it-line.png)
***✗** The work happened and the row says so, wearing the settled mark rather than the
attention one. `[u] undo` is not on it and cannot be: nothing in the engine marks a
ratify row's work as still undoable, so the key is never offered.*

## Assumptions — the ladder's second rung

![assumptions stand](screens/assumptions-stand-until-one-is-struck-card.png)
***✗** Every assumption stands until struck, and the clock says they go on by themselves.
Two things the design asks for are missing: the `≈` mark (the vocabulary has no slot
for it) and a sentence about assumptions — the row borrows the task proposal's
`starts on its own in 9m 57s`.*

![one struck](screens/assumptions-stand-until-one-is-struck-struck.png)
*`2`.*

## Several at once — the sheet

![two questions raised together](screens/two-questions-in-one-step-arrive-as-a-sheet-raised.png)
*Two `ask` calls in one step, grouped by shape, each row carrying that question's own
keys. `▸` is the cursor, `?` is a row still waiting.*

![one answered](screens/two-questions-in-one-step-arrive-as-a-sheet-answered.png)
*`1`. The row wears `✓` and the answer beside it, the cursor walks on, and `[g] same
answer for all like this` is offered now that there is an answer to spread.*

![spread](screens/two-questions-in-one-step-arrive-as-a-sheet-spread.png)
*`g`. Both rows answered, and `[s] send what is answered (2)`.*

![sent](screens/two-questions-in-one-step-arrive-as-a-sheet-sent.png)
*`s`. One record per row — which this wave added; before it, a batch you sent vanished
without a line.*

## Two windows

![the first window asks](screens/another-window-answers-and-the-first-says-who-raised.png)
*A question raised in one terminal.*

![home in the second window](screens/another-window-answers-and-the-first-says-who-home.png)
*A second terminal on the same machine, opened into the same project, which lands on
the ranked list. The row says what is waiting and the band counts it, `1 want you`.*

![the key does nothing](screens/another-window-answers-and-the-first-says-who-answered.png)
***✗** `1` pressed over that row. Nothing happens: the row shows the question and does
not offer its answers, so it cannot be answered from here.*

![and the first window still waits](screens/another-window-answers-and-the-first-says-who-told.png)
***✗** Which is why the first window is still on the question rather than on a receipt
saying `another window`.*

## Withdrawal

![raised](screens/a-question-whose-subject-went-away-is-withdrawn-raised.png)
*A permission holding a turn open.*

![withdrawn](screens/a-question-whose-subject-went-away-is-withdrawn-withdrawn.png)
*`esc` folded it, and a second `esc` let go of the turn it was holding. One dim line,
once, and the chip stops counting.*

## The dial that answers for you

![the rules](screens/a-project-rule-decides-a-choice-and-the-row-wears-it-sheet.png)
*`/autonomy`. Every shape, this project's rule for it, and the two rows no rule may
cover said out loud rather than discovered by being refused.*

![one changed](screens/a-project-rule-decides-a-choice-and-the-row-wears-it-rule.png)
*`/autonomy choice recommend 5s`.*

![counting down under that rule](screens/a-project-rule-decides-a-choice-and-the-row-wears-it-counting.png)
*A choice raised under it. The tail wears `your rule`, because a clock running for a
reason a person set is a clock they are owed the reason for.*

![decided](screens/a-project-rule-decides-a-choice-and-the-row-wears-it-decided.png)
*Nobody pressed anything. The record says `aforge, on your settings` — never `you`.*

## The road that ships

![the ordinary road](screens/the-ordinary-road-carries-a-question-and-its-answer-asked.png)
***✗** A plain `aforge` in a project, on a state root short enough for its session host's
socket. Three minutes after the model called `ask`: no block, no chip, and the turn
still running. Questions do not cross that link.*

![and a key changes nothing](screens/the-ordinary-road-carries-a-question-and-its-answer-answered.png)
***✗** `1` pressed anyway. There is nothing on this screen to press it at.*

## The blocks that have not moved yet

![the approval gate](screens/the-older-blocks-have-not-moved-onto-the-object-consent.png)
***✗** A shell command under an approval rule — the oldest and most common asker in the
product. It keeps its own row, its own keys and `[esc] cancel`, which is the word this
wave retired. Nothing counts it and nothing records it.*
