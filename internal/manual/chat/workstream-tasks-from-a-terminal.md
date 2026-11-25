# Workstream tasks from a terminal — sign in to your organization, follow a task on one line, answer quick questions

## A workstream task from your terminal — the terminal door into your organization

The terminal is one of WorkAF's doors: from it you can start a task in one of your organization's workstreams, follow it on one line that changes in place, and answer its quick questions. Everything else stays in WorkAF. A task started here is an ordinary task in that workstream, owned by you, and everyone in the workstream sees it in its Tasks tab as if you had added it in the app.

```
codeaf org login https://work.example.com
codeaf task "Write the release notes for 0.9"
codeaf task follow tk_01M8...
```

Every message the door prints ends with the line "Made with WorkAF".

## Sign in to your organization from a terminal — org login, org logout, where the sign-in is kept

`codeaf org login <server-url>` asks for your username and then your password; the password is not shown as you type. It uses the same password sign-in as WorkAF, so it is the account you sign in to the app with. `--user name` gives the username instead of typing it. In a script, the password is read from the first line of standard input; it is never taken as a flag, where other programs could read it.

The sign-in is kept in `doors/orgs.json` in codeaf's home folder (`~/.codeaf`), readable by you only, and is never printed. `codeaf org` lists the organizations this terminal is signed in to and which one `codeaf task` uses: the last one you signed in to. `codeaf org logout` ends the sign-in on the server and forgets it here; `codeaf org logout <server-url>` signs out of one when you have several. An organization that signs in only with GitHub or a passkey is answered "does not take a password from a terminal": sign in from WorkAF instead.

When the sign-in has ended, every door command says "your sign-in to … has ended" with the `codeaf org login` line to run.

## Which workstream and who does the task — Afi or you, and the task command's flags

`codeaf task "<what to do>"` makes the task in a workstream you can edit:

- `--workstream <name or id>` names it; the start of a name is enough when only one matches.
- Without it, the task goes in your only workstream. With several, a terminal lists them to pick by number; a script is refused with their names, so add `--workstream`.
- `--afi` has Afi do it, started at once. It is the default when your organization's agents run on its server.
- `--me` makes it your own task, up next, for you to do. It is the default when the agents run on someone's computer, and the door does not follow it.
- `--request "<more words>"` adds what is wanted beyond the title. Words longer than a title, or on several lines, keep their first line as the title and go whole into the request.
- `--org <url or name>` picks one of several signed-in organizations.
- `--no-follow` starts it and returns at once with the task's id.

## Following a task on one line — state, step and time, then what it made

While Afi works, the door shows one line, redrawn in place: the state in the app's words (Up next, Queued · 2nd, Running, Waiting on an answer, Done, Failed, Interrupted, Stopped by you, Didn't report back), what Afi is doing now in its own words ("made the document Release notes"), and how long the task has been going. Printed to a file or a pipe, a new line appears only when the state or the step changes.

When the task ends, the door lists what it made: a pull request's address, a file in the workstream's Files, Afi's one-line note. It ends with "Open it in WorkAF: <workstream> › <task>". It leaves with 0 when the task is Done and 1 when it failed, was stopped or did not report back.

Press Ctrl-C to stop following: the task goes on without you, and `codeaf task follow <task-id>` picks it up again. `follow` finds a task this terminal started by its id alone; for one started elsewhere it looks through your workstreams, or takes `<workstream-id>/<task-id>`.

## Answering a question from a terminal — quick questions here, decisions in WorkAF

When the task waits on a question, the door answers it here only if it is quick and yours: it can be undone, it recommends an answer, none of its answers needs your own words, and you are the one who decides it. The question is shown with its numbered answers and "(recommended)" beside one; Enter takes the recommended answer, a number picks another, and `s` leaves it for WorkAF. After answering, the door says "It can be undone in WorkAF" and keeps following.

Anything else is a decision, and a decision is never answered in a door. The door says "This one is a decision: open WorkAF to answer it", names the workstream and the task, and keeps following, so the line moves on as soon as you answer in the app. A question someone else decides reads "Waiting on Ann to answer: …". With no terminal to type at, even a quick question is pointed at WorkAF.
