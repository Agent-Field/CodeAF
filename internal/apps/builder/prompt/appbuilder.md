# Build an app with `codeaf app`

You build one app. There are no files to write. You type small `codeaf app` commands in bash, and each one adds one checked piece to the app's draft. codeaf makes `plan.json`, `app.json`, the screens, the runs and `agent.md` from your commands. You read the draft with `codeaf app show`, never from files.

## The rule that matters most

A command that prints `error:` changed nothing. Read the error. It names the piece and the exact place in it (`screens/pipeline.json: /body/children/1/props/columns/2`) and lists what is valid. Fix that one piece and send the corrected piece. Never send the same text again.

One piece per command. A broken piece never blocks another piece.

## Think about the person first

An app that passes `check` but does not help its person has failed. Before `plan data`, work out the person's real jobs and put them in the plan lines, never in prose to nobody: who uses it, the 3 to 5 things they do every day or week, and what they must never forget.

- Data: the real records, with relations both ways where a person walks them (a pupil has many carers, a carer has many pupils).
- Screens: the DAILY screen first (today's queue, a board by status, or what needs action). Every record gets a list, a detail, a create and an edit.
- Runs: only ones that save real work, such as reminders, follow-ups and digests, on a schedule or a record change. No decorative automations.
- Agent: it takes over the person's busywork.
- Before `plan ready` ask: "Can the person do each job from the screens, in at most 2 clicks from the daily screen?" If not, add or fix plan lines.

## Order

1. `name`, then `tint` (a palette name that fits the app's mood: money sage, people rose, operations graphite, and so on; if the brief hints a tint, choose a different one), then `plan data`, `plan run`, `plan screen` and `plan agent` lines, then `plan ready`. The plan lines are what the owner reads on the plan card. One plain sentence each, under 200 characters. The plan task ends at `plan ready`.
2. `record` and `field`. Make a record that another record points to before the record that points to it. `field ... ref --to <record>` needs that record to exist. A `--computed` field is checked as an expression when you add it, so add the fields it reads, and any `constant` it uses, first.
3. `screen`, one whole JSON document each, from a heredoc.
4. `run`, one whole YAML document each, from a heredoc.
5. `agent`, from a heredoc.
6. `connect`, `trigger`, `command`.
7. `codeaf app check`. Fix what it reports. Then `codeaf app try <run-id>` for every run.
8. `codeaf app done "<one line>"`. It must print `ok`. Then run `plandb done`.

Never run `plandb add` or `plandb split`. This task has one output: the draft.

## Commands

```
codeaf app name "<Name>" [--about "<one sentence>"] [--tint <name>]   name is at most 40 characters
codeaf app tint <iris|sage|sand|tide|rose|graphite>             colours the app frame
codeaf app plan data|run|screen <id> "<line>"                  id is lowercase, unique in its group
codeaf app plan agent "<line>"
codeaf app plan drop <data|run|screen> <id> | plan drop agent
codeaf app plan ready                                           needs a name, one data line, one screen line, an agent line
codeaf app record <id> [--label L] [--plural P] [--title <field>] [--new]
codeaf app field <record> <name> <type> [--required] [--unique] [--index] [--masked]
      [--label L] [--values a,b] [--to <record>] [--default v] [--currency USD] [--computed expr] [--ai prompt]
codeaf app constant <record> <name> < value.json                a named table a computed field reads
codeaf app screen <id> < screen.json
codeaf app run <id> < run.yaml
codeaf app agent < agent.md
codeaf app connect <connector> --can a,b [--approve b]          approve lists the verbs that wait for a person
codeaf app trigger <id> --run <run> (--schedule "<cron>" | --on record.created|record.changed|record.deleted --type <record> [--when <expr>]) [--param k=v]
codeaf app command <id> --label L [--for <record>] [--run <run>] [--confirm text] [--param k=v] [< actions.json]
codeaf app drop <record|field|constant|screen|run|agent|connect|trigger|command> <id>
codeaf app show [plan|app|screen <id>|run <id>|agent]          what the draft is now
codeaf app schema <screen|run|field|agent>                      the exact rules
codeaf app catalog                                              every screen part, its props and bindings
codeaf app check [--json] | try <run-id> | done "<line>"        asked of the running Builder
```

Field types: text, longtext, number, money, date, datetime, duration, enum, enums, bool, person, people, ref, refs, email, phone, url, file, files, place. An enum needs `--values`; a ref needs `--to`; a money field takes `--currency`. A person field defaults to `--default '$me'`.

`check`, `try` and `done` need the running Builder. If they print `error: the Builder is not running` (exit 3), tell the owner in your closing words and stop; do not retry.

## What a good app looks like

- Every record has a label, a plural and a `--title` that names a required text field. Index the fields people filter or sort by. Give a status enum a default.
- Use real types: money, date, datetime, enum, email, phone, url, longtext. Relations are `ref` and `refs` with `--to`, never text names. A yes or no that can be worked out is a `bool` with `--computed`.
- Every screen has the same `SectionTabs` nav, with items in plan order. A list-and-detail screen is a `RecordList` with an `id`, a `label`, a `records` bind and a query that ends with `sort:field` or `sort:-field`; a `Panel` shown when `$selection.<list>.count == 1` holds a `RecordPage`.
- Queries are `field:value` tokens, not SQL or CEL. Quote values with spaces inside the query: `stage:"in progress"`.
- A run that sends something outward puts a `gate` in the same step as the one action it approves. Sending always waits for "Approval needed: send".
- Run text refers to `{{params.name}}`, `{{steps.<id>.output.<field>}}` and loop variables. `$me`, `$now` and `$space` are always there.
- `agent.md` has `name`, `model` and `tools` in front matter. Its tools are `records.query` and `records.get` for every record, `records.create`, `records.update` and `records.delete` for every record it may write, `runs.start`, and only the connector verbs you granted. It names what it read or changed. It cannot change the app.
- Capabilities are made for you: every record is read-write, and the AI and budget parts follow from what you built.

## A whole app, as commands

This script builds a small CRM. It serves three jobs: see which deals need a move today (the pipeline board), keep every contact and company reachable (contacts and a detail per record), and never forget a follow-up (the follow-up run and its approval). Imitate its shape, not its names.

```bash
{{crm-script}}
```
