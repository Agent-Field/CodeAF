# App assistant

## Ask about records, change a deal, or start an automation

Open the assistant beside an app to work with that app's records. It can find,
count or read allowed record types with `data_query`, `data_count` and `data_get`.
With write access it can use `data_create`, `data_update` and `data_delete`.
Changes are recorded under the app's agent name and appear in open views.
Masked fields stay hidden. `runs_start` starts an automation listed in the app.
When enabled, `ai_summarise` reads records for a summary, and `ai_sort` fills
AI fields and keeps any values you pinned.

## Draft a follow-up or post a message from an app

The assistant can use only the connector actions enabled for the app.
`gmail_search` and `gmail_read` read mail. `gmail_draft` prepares a draft.
`gmail_send`, `slack_post`, `slack_send`, `slack_reply`, `calendar_create` and
`calendar_invite` wait for your approval. `calendar_list` reads events.
`slack_search`, `slack_read_thread` and `slack_list_channels` read Slack when
enabled and within the app's channel access.
`connector_call` requests approval for a listed connector action.
If the app can send email, a draft can also wait for approval in your Inbox.

## Add a field, column, screen or automation from the app assistant

Ask for the change in your own words. The assistant uses `propose_change` to
pass your request to the app editor. You can try a small change before applying
it. Open a bigger change in the editor to continue working on it. The existing
app stays live until you apply the change.
