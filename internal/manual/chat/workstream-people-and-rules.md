# A workstream's People and rules — who is in it, workstreams inside workstreams and start without asking

## People and rules — the sheet behind People in a workstream's header

Every workstream has one People and rules sheet, opened from People in its header (below 1100 points wide, from More › People). It shows, in order: the people and teams in it, "Owner of incoming work", "If unanswered for a day", "Start without asking", and "Keeps running" (the Factory and each standing order, each with a switch). Every change applies at once and can be undone for five seconds; there is no Save button, and Done only closes the sheet.

Only the person who looks after the workstream (its steward — the person who made it, until they hand it on) changes this sheet. Everyone else in it sees the same sheet, read-only. Asking to change it when you do not look after it is refused with "only the person who looks after this workstream can change its rules or hand it on".

## adding people or a team to a workstream — add a team, everyone in it now and later

"Add people or a team…" picks persons and teams of your organization. Adding a team adds everyone in it, now and later: someone who joins Engineering next month is in every workstream Engineering is in, with nothing else to do. A team is a group of people, not a place; one workstream can have several teams, and making a workstream never asks "which team".

## removing a person from a workstream — what happens to their open tasks

Removing a person (or a team they were in through) hands their open tasks in that workstream, and in every workstream inside it, to the fallback person ("If unanswered for a day"). With no fallback set, they go to the person who looks after the workstream.

## owner of incoming work, and the fallback person — who is asked

"Owner of incoming work" owns tasks nobody asked for directly, such as ones the Factory creates: a named person, or Everyone (the first person to answer decides, and the task records who answered). It starts as the person who made the workstream. "If unanswered for a day" names a person who is also asked when a task's question has waited a day, and who takes the open tasks of anyone removed.

## start without asking — how much an agent may spend before it must ask

"Start without asking" is one amount per task: the most an agent may spend on one task before it must ask its owner. $0 means never start without asking — every task waits for a person. It replaces the older go-ahead allowances and the autonomy setting, which no longer appear on a workstream.

On an organization, the amount is honoured by the organization's server with no app open: it keeps one go-ahead over the workstream's Factory jobs, holding each job's budget to the amount, rewritten whenever the amount or the jobs change.

## the Factory switch and standing orders in a workstream — what works here without anyone asking

"Keeps running" lists what works in the workstream without anyone asking. The Factory switch says whether the Factory turns new issues into tasks here. Each standing order belonging to the workstream has its own switch; its results land in the workstream's task list as a group named after the order. Before an order is switched on, the sheet shows what it will create. An order that belongs to no workstream runs exactly as it always did.

## a workstream inside a workstream — nesting, one level only, and what it shares with its parent

A workstream can sit inside one other workstream (Meal planner inside Pantry), and the nesting stops there: a workstream that is already inside another cannot be a parent, and one that has workstreams inside it cannot move inside another. Making one goes through New workstream, "Where: Inside Pantry", which offers only top-level workstreams.

A workstream inside another:

- always has its parent's people. Its sheet says "Same as Pantry" and its people cannot be changed there; change them on the parent and they change on every workstream inside it.
- reads its parent's files, shown as "From Pantry".
- starts with a copy of its parent's rules and may change its own copy; changing it never changes the parent's.
- keeps its own tasks and chats.

## archiving a workstream inside another, or folding it back into a heading

Archiving a workstream inside another asks which of its files should stay in the parent first. Folding it back turns it into a heading in the parent's task list, named after it: its tasks and Factory jobs move into the parent and it is archived.
