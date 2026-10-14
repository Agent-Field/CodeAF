#!/usr/bin/env bash
# The golden CRM as codeaf app verbs. A test runs this script and requires the
# rendered draft to equal internal/apps/spec/testdata/crm by meaning.
set -euo pipefail

# 1. Name and purpose.
codeaf app name CRM --about 'Sales pipeline: companies, deals and contacts. The agent drafts follow-ups; every email waits for approval.'
codeaf app tint sage

# 2. The plan card, one line per piece.
codeaf app plan data deal 'Deal · name, company, stage (Lead → Won/Lost), next step, value, close date, owner'
codeaf app plan data company 'Company · name, domain, size, owner'
codeaf app plan data contact 'Contact · name, email, phone (hidden until revealed), company'
codeaf app plan run follow-up 'Deal moves to Proposal, or you ask → draft a follow-up email → approval: send'
codeaf app plan screen pipeline 'Pipeline · full-width deals table, board by stage, bulk moves'
codeaf app plan screen deal 'Deal · one deal with its runs and approvals'
codeaf app plan screen contacts 'Contacts · searchable list, add and edit contacts'
codeaf app plan agent 'CRM agent · drafts emails, updates deals, answers pipeline questions'
codeaf app plan ready

# 3. Records and fields. A record that another one points to comes first.
codeaf app record company --label Company --plural Companies --title name
codeaf app field company name text --required --unique --label 'Company name'
codeaf app field company domain url --index --label Website
codeaf app field company size enum --label 'Company size' --values 1-10,11-50,51-200,201+
codeaf app field company owner person --label Owner --default '$me'
codeaf app record contact --label Contact --plural Contacts --title name
codeaf app field contact name text --required --label 'Contact name'
codeaf app field contact email email --index --label Email
codeaf app field contact phone phone --masked --label Phone
codeaf app field contact company ref --index --label Company --to company
codeaf app record deal --label Deal --plural Deals --title name
codeaf app constant deal stage_weight <<'JSON'
{"Lead": 0.1, "Qualified": 0.25, "Proposal": 0.5, "Negotiation": 0.7, "Won": 1, "Lost": 0}
JSON
codeaf app field deal name text --required --label Deal
codeaf app field deal company ref --required --index --label Company --to company
codeaf app field deal contacts refs --label Contacts --to contact
codeaf app field deal stage enum --required --index --label Stage --values Lead,Qualified,Proposal,Negotiation,Won,Lost --default Lead
codeaf app field deal amount money --required --label Value --currency USD
codeaf app field deal close_on date --label 'Close date'
codeaf app field deal owner person --label Owner --default '$me'
codeaf app field deal last_touch datetime --index --label Touch
codeaf app field deal weighted money --label 'Weighted value' --computed 'amount * stage_weight[stage]'
codeaf app field deal likely enum --label 'Likely to close · AI' --ai 'likely to close this month' --values Likely,Maybe,Unlikely
codeaf app field deal next_step text --required --label 'Next step'

# 4. Screens, one whole JSON document each.
codeaf app screen pipeline <<'JSON'
{"$schema":"https://codeaf.dev/schema/screen/1.json","screen":"pipeline","title":"Pipeline","params":["view"],"nav":{"part":"SectionTabs","label":"CRM sections","props":{"items":["Pipeline","Contacts"]}},"body":{"part":"Stack","children":[{"part":"Row","children":[{"part":"Text","props":{"text":"Deals","size":"title"}},{"part":"Button","label":"New deal","props":{"kind":"primary"},"on":{"click":[{"records.create":{"type":"deal","set":{"name":"$input.name","company":"$input.company","amount":"$input.amount","stage":"$input.stage","next_step":"$input.next_step"}},"ask":{"name":"text","company":"ref","amount":"money","stage":"enum","next_step":"text"}}]}}]},{"part":"RecordList","id":"deals","label":"Deals","bind":{"records":"deal","query":"stage:-Lost sort:-amount"},"props":{"views":["List","Board by stage"],"meter":"stage","groupBy":"stage","columns":["company","name","stage","next_step","amount","last_touch"]},"on":{"open":[{"open":"deal","id":"$row.id"}]},"slots":{"empty":{"part":"Empty","props":{"text":"No deals yet"}}}},{"part":"BulkBar","when":"$selection.deals.count > 0","props":{"list":"deals"},"children":[{"part":"Button","label":"Move to…","on":{"click":[{"records.update":{"ids":"$selection.deals.ids","set":{"stage":"$input.stage"}},"ask":{"stage":"enum"}}]}},{"part":"Button","label":"Draft follow-ups","on":{"click":[{"runs.start":"follow-up","params":{"deals":"$selection.deals.ids"}}]}},{"part":"Button","label":"Likely to close · AI","on":{"click":[{"ai.sort":"deals","field":"likely"}]}}]},{"part":"Panel","label":"Selected deal","when":"$selection.deals.count == 1","children":[{"part":"RecordPage","bind":{"record":"$selection.deals.id","type":"deal"},"props":{"fields":["stage","amount","next_step","close_on","owner","weighted","likely"],"actions":["follow-up","mark-won"]},"label":"Selected deal details"}]}],"props":{"align":"stretch"}}}
JSON
codeaf app screen deal <<'JSON'
{"$schema":"https://codeaf.dev/schema/screen/1.json","screen":"deal","title":"Deals","params":["id"],"nav":{"part":"SectionTabs","label":"CRM sections","props":{"items":["Pipeline","Contacts"]}},"body":{"part":"Stack","children":[{"part":"RecordPage","label":"Deals › {{record.name}}","bind":{"record":"$params.id","type":"deal"},"props":{"fields":["name","company","contacts","stage","amount","close_on","owner","last_touch","weighted","likely","next_step"],"history":true}},{"part":"Row","children":[{"part":"Button","label":"Draft follow-up","on":{"click":[{"runs.start":"follow-up","params":{"deals":["$params.id"]}}]},"props":{"kind":"primary"}},{"part":"Button","label":"Ask CRM agent about this deal","props":{"kind":"quiet"},"on":{"click":[{"agent.ask":"Summarise this deal and suggest the next step.","record":"$params.id"}]}},{"part":"Button","label":"Mark won…","props":{"kind":"secondary"},"when":"$record.stage != 'Won'","on":{"click":[{"records.update":{"id":"$params.id","set":{"stage":"Won"}},"confirm":"Mark {{record.name}} won?"}]}},{"part":"Button","label":"Back to deals","props":{"kind":"quiet"},"on":{"click":[{"open":"pipeline"}]}},{"part":"Menu","label":"More deal actions","children":[{"part":"Button","label":"Delete deal…","props":{"kind":"destructive"},"on":{"click":[{"records.delete":{"id":"$params.id"},"confirm":"Delete {{record.name}}?"},{"open":"pipeline"}]}}]}]},{"part":"Text","props":{"text":"Approvals","size":"small","tone":"neutral"}},{"part":"ApprovalCard","label":"Approvals for this app","bind":{"approvals":"open"},"slots":{"empty":{"part":"Empty","props":{"text":"No approvals waiting. Draft a follow-up to prepare an email for approval."}}}},{"part":"Text","props":{"text":"Follow-up runs","size":"small","tone":"neutral"}},{"part":"RunTimeline","bind":{"run":"follow-up","for":"$params.id"},"slots":{"empty":{"part":"Empty","props":{"text":"No follow-up runs yet. Choose Draft follow-up to start one."}}}}]}}
JSON
codeaf app screen contacts <<'JSON'
{"$schema":"https://codeaf.dev/schema/screen/1.json","screen":"contacts","title":"Contacts","nav":{"part":"SectionTabs","label":"CRM sections","props":{"items":["Pipeline","Contacts"]}},"body":{"part":"Row","children":[{"part":"Stack","children":[{"part":"Row","children":[{"part":"Text","props":{"text":"Contacts","size":"title"}},{"part":"Button","label":"New contact","props":{"kind":"primary"},"on":{"click":[{"records.create":{"type":"contact","set":{"name":"$input.name","email":"$input.email","company":"$input.company"}},"ask":{"name":"text","email":"email","company":"ref"}}]}}]},{"part":"RecordList","id":"contacts","label":"Contacts","bind":{"records":"contact","query":"sort:name"},"props":{"columns":["name","email","company"]},"slots":{"empty":{"part":"Empty","props":{"text":"No contacts yet"}}}}]},{"part":"Panel","label":"Selected contact","when":"$selection.contacts.count == 1","children":[{"part":"Form","label":"Edit contact","bind":{"record":"$selection.contacts.id","type":"contact"},"props":{"type":"contact","fields":["name","email","phone","company"],"submit":"Save"},"on":{"submit":[{"records.update":{"id":"$selection.contacts.id","set":{"name":"$input.name","email":"$input.email","phone":"$input.phone","company":"$input.company"}}}]}}]}]}}
JSON

# 5. Runs, one whole YAML document each.
codeaf app run follow-up <<'YAML'
run: follow-up
title: Follow up on deals
budget: { per_run: 0.50 }
params:
  deals: { type: refs, to: deal, default: "stage:Proposal,Negotiation last_touch<-14d" }
steps:
  - id: each
    loop: { over: $params.deals, as: deal, max: 25 }
    steps:
      - id: deal
        records.query: { type: deal, query: "id:{{deal}}", limit: 1 }
      - id: draft
        agent: CRM agent
        prompt: "Draft a short, plain follow-up email for {{steps.deal.output.name}} at stage {{steps.deal.output.stage}}."
        output:
          type: object
          required: [subject, body]
          properties: { subject: { type: string }, body: { type: string } }
        retry: { max: 2, backoff: 30s }
      - id: send
        gate: { approval: send, who: $me, title: "Approval needed: send email" }
        tool: gmail.send
        as: $me
        args: { subject: $steps.draft.output.subject, body: $steps.draft.output.body }
      - id: touch
        records.update: { type: deal, id: $deal, set: { last_touch: $now } }
YAML

# 6. The agent.
codeaf app agent <<'MD'
---
name: CRM agent
model: deepseek/deepseek-v4.1-flash
tools: [records.query, records.get, records.create, records.update, gmail.draft, gmail.send]
---
You are the CRM agent. You work on this CRM's content only: companies, deals and contacts.

- Answer questions about the pipeline from the records. Say which records you read.
- Update deals and contacts when asked. Every write goes through the bridge and gets Undo.
- Draft follow-up emails. Sending always waits for "Approval needed: send".
- Never reveal masked phone numbers.
- If someone asks you to change the app itself (fields, screens, runs, this file), answer
  "That changes the app" and offer Shape this. You cannot change the app.
MD

# 7. Connectors, triggers and commands.
codeaf app connect gmail --can read,draft,send --approve send
codeaf app trigger proposal-sent --run follow-up --on record.changed --type deal --when "changed('stage') && stage == 'Proposal'" --param 'deals=["$record.id"]'
codeaf app command follow-up --label 'Draft follow-up' --for deal --run follow-up --param 'deals=$selection.deals.ids'
codeaf app command mark-won --label 'Mark won…' --for deal --confirm 'Mark {{count}} deals won?' <<'JSON'
[{"records.update": {"ids": "$selection.deals.ids", "set": {"stage": "Won"}}}]
JSON

# 8. Check the whole app, try each run, then finish.
codeaf app check
codeaf app try follow-up
codeaf app done "CRM built: deals, companies, contacts and a follow-up run"
