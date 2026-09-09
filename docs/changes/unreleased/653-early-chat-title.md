---
kind: fixed
title: Name chats from the first message without waiting for the answer
pr: 653
surface: [chat, engine, remote]
invalidates:
  - "Chat naming waited for the first completed reply and tried only once. It now starts alongside the first accepted message, with bounded background retries for temporary failures."
  - "A title could only reach the UI through a running turn. A standing subscription now updates idle, background, and reconnected chats, with conversation ownership and revision ordering preserved."
  - "Conversation metadata updates could overwrite an asynchronously saved title with an older usage snapshot. Serialized field-specific updates now preserve the title and the usage together."
---

The answer and naming run independently. Existing names win, session close cancels
unfinished naming, and title costs remain in session history without entering another
turn's spend. The existing title role and title sanitization are retained. Three naming
attempts share a two-minute window; permanent failures or invalid replies leave the
placeholder without interrupting the conversation.
