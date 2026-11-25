---
kind: added
title: comment on a passage of an organization workstream's document, kept on the organization's server
pr: 2724
surface: [engine, factory]
invalidates:
  - "Was true: comments on a passage of a document existed only in a Personal workstream; an organization's server kept none, and the desk left Comments off every organization page. Is true now: with the hello word `org_workstream_page_comments` the engine serves `factory.org_workstream_page_comments`, `_page_comment`, `_page_comment_reply`, `_page_comment_resolve`, `_page_comment_seen` and `_page_comments_for_you` (params name the workstream `ws`, the page `page`, a thread `thread`), over `/v1/workstreams/{id}/pages/{pid}/comments` and `/v1/workstreams/{id}/comments/{cid}/reply|resolve|seen|for_you`. View reads, edit comments and resolves, seen is each person's own, and every change relays as `factory.org_workstream_changed` with parts `page` and `files`."
  - "Was true (for anyone reading the Personal store): a thread's answer says `anchored`, decided against the page. Is true now for an organization: the server keeps the anchor exactly as posted and never sends `anchored` — the page is a live document it never parses — so the desk finds the words again in its editor."
  - "Was true: an organization document's file never carried `open_comments`. Is true now: it counts the page's open threads."
---
