---
kind: fixed
title: Outside work carries a trust tier, and a job says which routing rules the AI declined
pr: 2478
surface: [factory, app, engine]
invalidates:
  - "Every job filed through internal/factory/inbox.Add was an owner's, so a Jira ticket from a service-desk customer could start on an AI yes and write. Now inbox.Entry.Trust sets the tier (empty is owner for the operator's own doors), Jira reporters are collaborators and accountType customer is a stranger, and a repeat delivery may lower the tier, never raise it."
  - "Only the deciding rule's AI answer crossed the wire (job.route.answer), so a job no rule started said nothing. Now job.route_declined lists every other rule the AI answered no about, with why."
---
