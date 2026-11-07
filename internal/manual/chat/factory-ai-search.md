# Ask AI in the jobs search bar

## ask AI to find jobs — search issues by meaning, "features related to exports", "anything touching auth tokens"

The factory's jobs search bar (Search, or type repo: size: priority:) matches
words and keys like `state:`, `repo:`, `kind:`, `step:`, `age:` and `is:`. When
the words you want are not in the titles, ask the AI instead: type a question
in your own words and press **Ask AI** at the end of the bar, or start the text
with `?` and press Enter. For example `? anything touching auth tokens`.

The AI never runs while you type. Ask AI (or Enter on a `?` question) first
shows, above the list, what it will read and what it is likely to cost, such
as "Reads 61 issues · about $0.0095", and asks no model. It runs only when you
press **Run** there. A question asked before over the same issues says "Asked
before · nothing new to read".

It reads the jobs the bar lists at that moment, so `repo:` and the other keys
narrow what it reads. Dropped and dismissed jobs are never read, and it reads
only jobs your factory sign-in can see.

While it works it says how far it has got: "Reading 80 issues… 12 look
related", then "Checking 12… 7 match". The answer is a list of the matching
jobs, each with one sentence saying why it matched, and what it cost. Cancel
ends it early; Ask again runs it again; the close button puts the list back.

## how the AI search finds matches — a quick read of every issue, then one yes or no per issue, and how many it reads

The search runs in two passes so it stays cheap at 80 issues and at 5,000.

1. A quick read: the AI reads the issues 40 at a time, with each one's title,
   labels, area and the start of its description, and keeps any that might be
   about your question.
2. A careful check: each kept issue gets the same yes-or-no question a routing
   rule asks (see "a routing rule that asks the AI"), with its whole
   description. Only a yes is a match, and the AI's one sentence is the reason
   shown.

Limits: it reads at most 5,000 jobs and checks at most 60 of the issues the
quick read kept. When more than 60 looked related, the answer says how many
more were not checked, so narrow the question or the bar. A question is at most
300 characters. A read or a check that fails is never a match, and the answer
says it is incomplete. There is no search index behind it: every issue is read
by the AI.

## what an AI search costs — Spending, factory search, the estimate before it runs

Before it runs, the bar shows the estimate: how many issues it will read and
about what it will cost at the factory's default model's price (no figure when
the price is not known). After, the answer shows what it actually cost.

On deepseek/deepseek-v4.1-flash a search of 61 issues cost between $0.004 and
$0.013, depending on how many issues looked related. Asking the same question
again over the same issues cost $0.00. Every call goes on Spending as
`factory search` and counts toward the factory's day, like `factory filter` and the issue read. It runs on the
factory's default model; with none set, the issue read's model.

## save an AI search as a view — the question is kept, reopening is cheap, new issues

Press **Save as view** beside an AI answer to keep it in the sidebar's Views,
named after your question; then **Open view** opens it. It is a topic view:
it keeps your question and the matches with their reasons.

Opening the view shows the kept matches straight away. **Refresh** asks the
question again, and every answer the AI gave before is reused: only issues that
arrived or were edited since are read, so a refresh usually costs nothing or
close to it. The view says when it was last matched.

New issues are not checked against saved views by themselves; nothing runs
while you are not looking. To have arriving work judged as it comes in, make a
routing rule that asks the AI instead.

## AI search on the wire — factory.ai_search and its status

The app's engine serves `factory.ai_search_estimate` (free, asks no model),
`factory.ai_search` (starts one run and answers its id), `factory.ai_search_status`
(the run's progress and, at the end, its matches) and `factory.ai_search_stop`.
A run belongs to the engine, so closing the window does not stop it.
