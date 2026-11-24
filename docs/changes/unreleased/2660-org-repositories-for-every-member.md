---
kind: changed
title: an organization's members make jobs on its repositories without a GitHub account of their own
pr: 2660
surface: [factory, engine, docs]
invalidates:
  - "On an organization server a signed-in person read only the repositories their own GitHub token reached, so a person who signed in with a password or a passkey read none and could never make a job (film D10). Now every signed-in member also reads the organization's repositories: the ones turned on in Repos. A member's own token is still asked exactly as before and only ever adds."
  - "A repository that was turned on but that a member's own token could not read was hidden from that member (the queue answered no_visible_repos). It is now visible to every member, because turning it on is the operator's choice for the organization."
  - "A password operator signed in remotely read nothing in Repos and could not turn a repository on. The operator now also reads what the server's own GitHub grant last discovered."
  - "POST /v1/jobs and factory.job_create took no workstream; the desk made a job and added it to a workstream in a second call. They now take workstream: the caller needs edit on it and the job joins it in the same request. A job on a repository the caller reads only through the organization is refused 403 without one (\"Make this job in a workstream you can edit: this repository is the organization's.\")."
  - "The chat manual said a factory's people each sign in with their own GitHub. They sign in with a username and password, a passkey, or GitHub."
---

A link's join check still judges a GitHub person by their own token, so the
organization's repositories never let a stranger in. Turning a repository on
or off stays operator-only.
