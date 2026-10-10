# Desktop plan card

## Can you propose a plan and wait for my go-ahead?

On the desktop, when the `plan` tool is on the belt, the chat can propose a short
sequence of actions as a card. A plan that stays inside this conversation runs
at once. A plan that reaches beyond it (another task, another chat, a place)
waits, and nothing runs until you choose Go, Edit or Cancel. The `plan` tool is
absent on surfaces that cannot draw the card.

## What steps can a plan card hold?

Six actions: hold a task, start a task, stop a task, steer a task or chat with
words, ask a place a question, and remember a fact. Each step names its target
by id (exactly one of chat, task or place), never by a name in the sentence.
A step that cannot run as written is refused before anything runs.

## What happens when I press Go, Edit or Cancel on a plan?

Go runs the steps in order and answers with one receipt that groups every line.
Pressing Go twice runs the steps once. Edit replaces the steps and the card
waits again. Cancel runs nothing and has no side effects; a cancelled plan
cannot be run afterwards.

## What if a task in the plan was deleted, and can I undo a step?

A step whose task, chat or place is gone shows as a skipped line, for example
"Skipped: t1 is gone", and the other steps still run. Holding a task can be
undone by resuming it, starting by holding it, and remembering by forgetting
exactly that saved line. Stopping a task, a note already sent and a place's
answer have no undo and show none.
