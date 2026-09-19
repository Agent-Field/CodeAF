---
kind: fixed
title: person-origin launch-or-join issues a grant; joiners see the shared work
pr: 1225
surface: [chat, engine]
invalidates:
  - "IssueGrant existed on wsapi and was only called from tests. A person-origin launch-or-join with an empty grant now issues that grant in software, then LaunchOrJoin; an agent turn still needs a cited grant."
  - "LaunchOrJoin returned Joined=true but ListBindingsForChat only matched owner or coordinator, so the joining discussion painted nothing. The joining chat is recorded on the binding row; both chats list it and Joined is visible."
  - "collections.md claimed spend job-category reservation and exhaustion as pending/deferred on the launch path. That is not implemented; the overclaim is deleted."
  - "CODEAF_TASK_BELT in wsexec was read through os.Getenv. It now goes through internal/env, the same door bashBeltAsked uses."
---

Software stamps OriginPerson at IssuePersonGrant. Schema v6 adds joiner_json on the existing binding; listing still does not migrate.
No new slash command. `/folders` is unchanged.
