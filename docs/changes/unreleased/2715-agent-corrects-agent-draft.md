---
kind: fixed
title: A chat can fix another chat's wrong draft in Files, and a refusal names who made the file
pr: 2715
surface: [app, docs, chat]
invalidates:
  - "A workstream agent could rewrite only a document it made itself; another chat's draft took neither a rewrite nor a suggestion, so a wrong draft stayed wrong. Now any agent may rewrite a document an agent made (itself, Afi or another chat) that no person has edited since, and every other document takes ws_file_suggest."
  - "The refusal to rewrite said the file had been edited by a person even when only another chat had made it. Now it names the maker (Afi, the chat by its title, or the person) and says a person has edited it only when one has."
  - "A suggestion on an agent's draft that a person had since edited was refused as the agent's own page. Now a person's edit makes the page the person's, so a suggestion on it is accepted (desk PR)."
---
