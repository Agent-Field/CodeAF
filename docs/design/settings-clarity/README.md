# Settings clarity

A settings page should answer four questions together: what this changes, what it is set to, where it applies, and when the change takes effect.

## Design decisions

- Nine categories separate everyday preferences, models, memory, task behavior, AI team defaults, permissions, spending, connections, and privacy. AI teams explicitly names coordinated AI work; it does not suggest a multi-user organization.
- Wide terminals use a category sidebar; narrower terminals use a scrolling category bar. The top bar uses text in sentence case. No new font-dependent icons are required.
- Search sits above the list and covers every category, including Advanced controls and the connection catalog. Existing names and config keys still work. Model selection has a separate **Filter models** box.
- General contains **show hints**, with a positive on/off reading. Turning tips off leaves keyboard instructions and setting explanations available. Selected and hovered controls share the same help area, whose fixed height prevents rows moving under the pointer.
- Less common controls remain under Advanced. Search can find them while collapsed. Memory has a direct route to inspecting saved memories.
- Names and values describe behavior: task scope assessment, assess while working, collapsed tool details, ask by default, and use-only model recommendations. Scope and restart requirements remain explicit.
- Invalid edits retain the draft and error. Escape cancels the editor; from search it clears the query first. Hosted team edits wait for acknowledgement.

## Compatibility and architecture

`Setting.ChatPresentation()` supplies chat-only categories, labels, descriptions, aliases, scope and activation timing. The existing registry remains the source of validators, writers and saved values. The UI value adapter changes readings without migrating configuration. Existing profiles retain their keys, values, unknown fields and defaults; opening settings writes nothing.

The shared settings surface combines registry controls with live connection and model-account catalogs. It uses the existing editors, provider picker, memory view and crew panel. Moving a setting does not create another settings store. Document-reader selection now reaches the v3 session configuration, closing a consumer wiring gap found during the audit.

Resident-only practice controls and unwritable legacy role slots are excluded from live chat settings. Their stored values remain intact. Canonical role and tier controls remain under Advanced.

## Capture method

These are real terminal frames, not mockups. The capture script starts the built CLI inside a private tmux server, with an isolated fixture profile, no host, automatic updates disabled, and a loopback model endpoint. No user account or model request is needed. `agg` and `ffmpeg` render the ANSI capture to PNG. Plain-text captures accompany each image for inspection.

Before: `4fd9cfaa0fb63b1c78db5aa6ebe9479e512d1640` (dev base). After revision is recorded in [verification](verification.md). Both use 80- and 120-column terminals, 48 rows, and the same fixture.

From a built checkout on Spark:

```sh
python3 docs/design/settings-clarity/capture.py --binary "$(pwd)/bin/codeaf" --out /tmp/settings-proof --version after --columns 120
python3 docs/design/settings-clarity/render.py /tmp/settings-proof
```

The fixture's key is dummy data. Live third-party OAuth, billing, and provider availability are not established by these screenshots.

## Category comparisons

Each pair below shows the primary former location and the new category. The complete baseline index follows because several old categories were combined or split.

### General

| Before · 120 columns | After · 120 columns |
| --- | --- |
| ![Before General](before/04-display-120c.png) | ![After General](after/01-general-120c.png) |

[Before · 80 columns](before/04-display-80c.png) · [After · 80 columns](after/01-general-80c.png)

### Models

| Before · 120 columns | After · 120 columns |
| --- | --- |
| ![Before Models](before/09-providers-120c.png) | ![After Models](after/02-models-120c.png) |

[Before · 80 columns](before/09-providers-80c.png) · [After · 80 columns](after/02-models-80c.png)

### Memory

| Before · 120 columns | After · 120 columns |
| --- | --- |
| ![Before Memory](before/01-session-120c.png) | ![After Memory](after/03-memory-120c.png) |

[Before · 80 columns](before/01-session-80c.png) · [After · 80 columns](after/03-memory-80c.png)

### Tasks

| Before · 120 columns | After · 120 columns |
| --- | --- |
| ![Before Tasks](before/07-tasks-120c.png) | ![After Tasks](after/04-tasks-120c.png) |

[Before · 80 columns](before/07-tasks-80c.png) · [After · 80 columns](after/04-tasks-80c.png)

### AI teams

| Before · 120 columns | After · 120 columns |
| --- | --- |
| ![Before AI teams](before/08-teams-120c.png) | ![After AI teams](after/05-ai-teams-120c.png) |

[Before · 80 columns](before/08-teams-80c.png) · [After · 80 columns](after/05-ai-teams-80c.png)

### Permissions

| Before · 120 columns | After · 120 columns |
| --- | --- |
| ![Before Permissions](before/06-safety-120c.png) | ![After Permissions](after/06-permissions-120c.png) |

[Before · 80 columns](before/06-safety-80c.png) · [After · 80 columns](after/06-permissions-80c.png)

### Spending

| Before · 120 columns | After · 120 columns |
| --- | --- |
| ![Before Spending](before/05-spending-120c.png) | ![After Spending](after/07-spending-120c.png) |

[Before · 80 columns](before/05-spending-80c.png) · [After · 80 columns](after/07-spending-80c.png)

### Connections

| Before · 120 columns | After · 120 columns |
| --- | --- |
| ![Before Connections](before/10-connections-120c.png) | ![After Connections](after/08-connections-120c.png) |

[Before · 80 columns](before/10-connections-80c.png) · [After · 80 columns](after/08-connections-80c.png)

### Privacy

| Before · 120 columns | After · 120 columns |
| --- | --- |
| ![Before Privacy](before/04-display-120c.png) | ![After Privacy](after/09-privacy-120c.png) |

[Before · 80 columns](before/04-display-80c.png) · [After · 80 columns](after/09-privacy-80c.png)

## All former categories

- Session: [120 columns](before/01-session-120c.png) · [80 columns](before/01-session-80c.png)
- Context: [120 columns](before/02-context-120c.png) · [80 columns](before/02-context-80c.png)
- Workspace: [120 columns](before/03-workspace-120c.png) · [80 columns](before/03-workspace-80c.png)
- Display: [120 columns](before/04-display-120c.png) · [80 columns](before/04-display-80c.png)
- Spending: [120 columns](before/05-spending-120c.png) · [80 columns](before/05-spending-80c.png)
- Safety: [120 columns](before/06-safety-120c.png) · [80 columns](before/06-safety-80c.png)
- Tasks: [120 columns](before/07-tasks-120c.png) · [80 columns](before/07-tasks-80c.png)
- Teams: [120 columns](before/08-teams-120c.png) · [80 columns](before/08-teams-80c.png)
- Providers: [120 columns](before/09-providers-120c.png) · [80 columns](before/09-providers-80c.png)
- Connections: [120 columns](before/10-connections-120c.png) · [80 columns](before/10-connections-80c.png)

## Advanced controls and interactions

- 02 models advanced: [120 columns](after/02-models-advanced-120c.png) · [80 columns](after/02-models-advanced-80c.png)
- 02 models advanced end: [120 columns](after/02-models-advanced-end-120c.png) · [80 columns](after/02-models-advanced-end-80c.png)
- 04 tasks advanced: [120 columns](after/04-tasks-advanced-120c.png) · [80 columns](after/04-tasks-advanced-80c.png)
- 04 tasks advanced end: [120 columns](after/04-tasks-advanced-end-120c.png) · [80 columns](after/04-tasks-advanced-end-80c.png)
- 05 ai teams advanced: [120 columns](after/05-ai-teams-advanced-120c.png) · [80 columns](after/05-ai-teams-advanced-80c.png)
- 05 ai teams advanced end: [120 columns](after/05-ai-teams-advanced-end-120c.png) · [80 columns](after/05-ai-teams-advanced-end-80c.png)
- 06 permissions advanced: [120 columns](after/06-permissions-advanced-120c.png) · [80 columns](after/06-permissions-advanced-80c.png)
- 06 permissions advanced end: [120 columns](after/06-permissions-advanced-end-120c.png) · [80 columns](after/06-permissions-advanced-end-80c.png)
- 08 connections advanced: [120 columns](after/08-connections-advanced-120c.png) · [80 columns](after/08-connections-advanced-80c.png)
- 08 connections advanced end: [120 columns](after/08-connections-advanced-end-120c.png) · [80 columns](after/08-connections-advanced-end-80c.png)
- 09 privacy advanced: [120 columns](after/09-privacy-advanced-120c.png) · [80 columns](after/09-privacy-advanced-80c.png)
- 09 privacy advanced end: [120 columns](after/09-privacy-advanced-end-120c.png) · [80 columns](after/09-privacy-advanced-end-80c.png)
- edit hints after: [120 columns](after/edit-hints-after-120c.png) · [80 columns](after/edit-hints-after-80c.png)
- edit hints before: [120 columns](after/edit-hints-before-120c.png) · [80 columns](after/edit-hints-before-80c.png)
- edit invalid value: [120 columns](after/edit-invalid-value-120c.png) · [80 columns](after/edit-invalid-value-80c.png)
- edit limit cleared: [120 columns](after/edit-limit-cleared-120c.png) · [80 columns](after/edit-limit-cleared-80c.png)
- edit limit reopened: [120 columns](after/edit-limit-reopened-120c.png) · [80 columns](after/edit-limit-reopened-80c.png)
- edit limit saved: [120 columns](after/edit-limit-saved-120c.png) · [80 columns](after/edit-limit-saved-80c.png)
- edit limit unlimited: [120 columns](after/edit-limit-unlimited-120c.png) · [80 columns](after/edit-limit-unlimited-80c.png)
- hover category: [120 columns](after/hover-category-120c.png)
- navigation activity: [120 columns](after/navigation-activity-120c.png) · [80 columns](after/navigation-activity-80c.png)
- navigation ai teams: [120 columns](after/navigation-ai-teams-120c.png) · [80 columns](after/navigation-ai-teams-80c.png)
- search old label: [120 columns](after/search-old-label-120c.png) · [80 columns](after/search-old-label-80c.png)
- search privacy: [120 columns](after/search-privacy-120c.png) · [80 columns](after/search-privacy-80c.png)
- search service: [120 columns](after/search-service-120c.png) · [80 columns](after/search-service-80c.png)

## Behavior verification

See [verification.md](verification.md) for tested revisions, Spark job IDs, results, and limitations.
