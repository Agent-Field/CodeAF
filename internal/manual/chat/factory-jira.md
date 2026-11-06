# Jira and the factory

## connect Jira — Jira Cloud site, email, API token and a JQL query

The factory on this machine can read Jira Cloud tickets as work. Connecting
takes four things: the site (such as `https://acme.atlassian.net`; a bare
`acme.atlassian.net` is read as https), the email the Jira account signs in
with, an Atlassian API token for that account (made at id.atlassian.com, under
Security, API tokens), and a JQL query saying which tickets become jobs.

The app's engine connects with `factory.jira_connect` (`factory`, `site`,
`email`, `token`, `jql`). Before it keeps anything it asks Jira who the token
belongs to and runs the query once; a wrong token answers "Jira did not accept
that email and API token", a query Jira cannot read answers "Jira refused the
search" with Jira's own reason, and either way nothing is kept.

The token is kept the way the GitHub token is: by the desktop app when the app
runs codeaf, otherwise in the profile. It is never written to the factory's
folder and never sent back in any answer. The site, the email and the query
are kept with the factory's settings.

There is no slash command for this in the terminal chat: Jira is connected
from the app (or a script speaking to its engine), and only the factory on
this machine can be connected; an attached factory keeps its own connections
on the machine that serves it.

## Jira tickets become factory jobs — how often Jira is read, polling not webhooks

Once connected, the factory reads Jira every minute, beside its GitHub read
and whenever it is reading GitHub by itself. Each ticket the query finds becomes one job, the same
way a labelled GitHub issue does: the ticket's summary is the title, its
description is the body (as plain text), its reporter is the author, and its
link is kept on the job. A reporter is trusted as a collaborator; a
service-desk customer is a stranger, whose job never changes files (see "who
outside work is trusted as"). Editing the issue in Jira later edits that job (the
rules for outside work are on the factory's own page).

Jira is POLLED, not pushed: codeaf does not register a webhook, so a new
ticket arrives within about a minute, not instantly. Each read asks only for
tickets changed since the last one (with a minute of overlap, because Jira's
dates stop at the minute). A read that fails is tried again on the next pass
and nothing is lost. While the factory is not connected, or its runner is
off, nothing reads Jira at all.

Whether a job then starts by itself, waits to be asked, or sits in Backlog is
decided by the factory's routing rules and start rules, exactly as for GitHub
work. A ticket no rule sends to a repository waits in Backlog asking which
repository.

## which Jira tickets — writing the JQL, project, labels, status

The JQL is the person's own and is sent to Jira as written, with two things
added: `AND updated >= "<last read>"` once there has been a read, and
`ORDER BY updated ASC`. Any `ORDER BY` in the query is dropped, because the
factory needs changes oldest first to know where it stopped.

Good queries are narrow:

- `project = FL` — every ticket in one project
- `project = FL AND labels = codeaf` — only tickets someone marked for codeaf
- `project in (FL, OPS) AND statusCategory != Done` — open work in two projects

A query must have at least one condition: an empty one, or one that is only
`ORDER BY`, is refused ("the JQL needs at least one condition"), because the
factory never reads a whole site. The query is at most 2000 bytes. One
factory has one query; to change it, connect again with the new one.

## send a Jira project to a repository — jira-project label, routing rules, Jira priority

Every job made from a Jira ticket carries a label naming its project:
ticket `FL-12` carries `jira-project:FL`, alongside the ticket's own labels.
A routing rule that matches that label (and source `jira`) is how one
project's tickets go to one repository and one pipeline; the connector itself
never picks a repository.

Jira's priority becomes the factory's, which orders the queue:

| Jira | factory |
|---|---|
| Highest | p0 |
| High | p1 |
| Medium | p2 |
| Low | p3 |
| Lowest | none |

Any other priority name (a site's own scheme) is no priority.

## Jira comment with the PR link — what codeaf writes back on the ticket

The factory answers on the ticket a job came from:

- when the job opens its pull request: `codeaf opened a pull request: <link>`
- when that pull request merges: `Merged: <link>`
- when the job stops and needs a person: the job's own words about why

Each is one comment, with the link a real link. Nothing else is written to
Jira: codeaf does not change a ticket's status, assignee, fields or labels,
and does not close it. A failed comment never fails the job. A factory that
has been disconnected comments nothing.

## disconnect Jira and check the Jira connection — status, last read, last error

`factory.jira_status` answers whether the factory is connected, the site, the
email, the query, the account's name, when Jira was last read without an
error (`last_read`), and what the last failed read said (`last_error`, cleared
by the next good read). It never carries the token. An engine that serves
these methods says `factory.jira` in its hello.

`factory.jira_disconnect` forgets the token, the site and the query, and
stops the Jira read at once; the jobs already made from tickets stay as they
are. Disconnecting a factory that was not connected is not an error.

## what the Jira connection cannot do — Jira Cloud only, no webhooks, one site

- Jira Cloud only (REST v3 with an API token). Jira Server and Data Center,
  and sign-in with OAuth, are not supported.
- Polling every minute, no webhooks; there is no instant delivery.
- One site and one query per factory, and only the factory on this machine.
- It reads summary, description, labels, priority, reporter, status and
  when the ticket changed. Attachments, comments on the ticket and custom
  fields are not read.
- It writes only the comments above; it never moves a ticket's status.
