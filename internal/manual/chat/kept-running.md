# Kept-up processes — a dev server that stays up after the turn

## Keep a dev server up after the turn — Allow, Allow, never restart, Deny

In the codeaf desktop app, the agent can ask to keep one command up after its turn
ends: a dev server, a database, a watcher. It names the exact command, the folder it runs
in and one health check, and proposes a restart policy. You answer on a card:

- **Allow** — it stays up, restarted the way it proposed.
- **Allow, never restart** — it stays up and is never restarted.
- **Deny** — "It runs as an ordinary process and stops when this turn ends."

Your answer is remembered for that exact command in that exact folder with that check, so
the same command is not asked about twice. The agent cannot change the policy you chose;
a different check is asked about again. This is not the `codeaf services` command in a
shell, which lists background processes of its own.

## How a kept-up process is checked — healthy, unhealthy, restarting

One check every 10 seconds, the first 10 seconds after it starts, each given 5 seconds: a
port that accepts a connection, a URL that answers 200 (only 200), or a command that exits
0 in its folder. Three failures in a row make it unhealthy; one pass makes it healthy again.

Under a restart policy it is restarted after 2, then 4, then 8 seconds — at most 3
automatic restarts in any 10 minutes, the count clearing after 10 healthy minutes. Past
that it stops restarting and says so. A restart you ask for yourself is yours: it is not one
of the 3. A port another program already holds is said as "port N is used by another
program", never started over it.

Each run of trouble is one line in the Inbox, updated as it changes, and one notice in the
chat that started it; when it is healthy again the line moves to done.

## Stop, restart, start again or remove a kept-up process

It is listed with the agent's other processes, in the same list.

- **Stop** sends SIGTERM, then SIGKILL after 5 seconds. Its output is kept. A process
  that would not stop stays listed as up, saying "did not stop".
- **Restart** stops it and starts it again from the same command, outside the automatic
  budget.
- **Start again** starts the saved command after a stop.
- **Remove** takes a stopped one off the list. Its output is kept, and the chat's notice
  still opens it.

## Quitting the app or closing an agent with processes kept up

Quitting the app leaves kept-up processes up unless you choose to stop them; the
next codeaf takes back only a process it can prove is the same one (its process id, its
start time and the machine's boot). After a reboot one is started again only under a
restart policy.

Closing an agent stops what it kept up and waits for it to be gone. If one will not
stop, the agent stays open and the error names it. Letting an agent go idle stops nothing.

## Where keeping a process up is not available

Keeping a process up exists only in the codeaf desktop app on macOS and Linux, and only in a
conversation where you answer cards — not in the terminal chat, not inside a task, and not
when another codeaf already looks after this machine's kept-up processes. There the
agent is told "keep_running is not available here, so nothing was started", and it can run
the command as an ordinary background job instead.

A kept-up process's output is kept while codeaf is open, in two files of 2 MiB that take
turns; output written while no codeaf is open is not cut back until codeaf next runs.
