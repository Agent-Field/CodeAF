# What aforge is

Aforge is a resident employee, not a chat window. You give it work in plain
language, it keeps working while you do something else, and it is still there —
with everything it was doing and everything it learned — after you close the
terminal and open it again tomorrow.

## The front desk and the workforce

What you talk to is the front desk. It answers instantly, it never does the work
itself, and it never refuses: it either answers you from what it can already
see, or it hands the request to the workforce behind it.

The workforce can search the web, run code, read and write files, and stay on
one thing for minutes at a time. The front desk knows what every one of them is
doing right now, so "what's everyone up to" is a question it can always answer.

## The graph: jobs, tasks, and the thread

Everything aforge has ever done lives in one durable graph.

- A **job** is one thing you asked for. It is what you see on the board.
- A job compiles into **tasks** — the steps that actually get worked. A task can
  have tasks under it, so a big job is a small tree.
- Results land back in the **thread**, the conversation you are reading. Every
  answer that came out of a job carries a `↳` you can follow to the task that
  produced it.

You never have to name a node id. You refer to work the way you would with a
person — "the finance one", "that report", "the queued ones" — and aforge
resolves it against what is actually live.

## Nothing is lost on restart

The graph is a file on disk, written as things happen, not at the end. Close the
terminal mid-job and the work keeps going; the workforce is not the terminal.
Restart and you get the same thread, the same board, the same notebook, and the
same standing goals.

If aforge crashes after you wrote a message but before it replied, it replays
that message on the next start. It will not silently swallow what you asked for.

## What it does when you are not there

Because it is durable, it does not need you present to be useful. It keeps
standing goals on their own watches, it practices in idle time, it revises what
it believes, and it folds all of it into one card waiting for you when you come
back. That is its own page — ask about the daily rhythm.

## Learning aforge by asking aforge

You do not have to read documentation to learn this. Ask it directly — what it
can do, how one of its mechanisms behaves, why it did the particular thing it
just did — in the same plain sentences you use for everything else.

Answers to questions about aforge itself come out of an internal manual that
ships inside the binary — the same pages you are reading now. They are not
improvised, and when the manual does not cover something, aforge says it does
not know rather than inventing machinery it does not have.

## What it will not do

- It will not pretend work finished when it did not. A receipt says what was
  queued, never what was delivered.
- It will not promise something will be done by a particular time.
- It will not spend past the daily rail without asking you first.
- It will not do something irreversible — spend money, publish, send, delete
  outside the workspace — as a quick reflex. Those always take the full path.
