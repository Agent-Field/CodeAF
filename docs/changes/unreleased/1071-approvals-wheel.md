---
kind: changed
title: UX changes — consistent message boxes, Home sessions and task trees
pr: 1071
surface: [chat, engine, remote]
invalidates:
  - "Home no longer has separate threads and tasks lists. One sessions list combines open tabs and saved history, deduplicated and limited to the fifteen most recent conversations; its heading opens the renamed Sessions tab. Closed conversations are dim and remain searchable."
  - "Closing a Home conversation with Right then x closes its tab and chats-menu entry while preserving history and running work. A conversation this window still holds never claims another window merely because its tab was closed."
  - "Mouse hover no longer overrides a later keyboard selection. The most recent navigation method owns the single selection; Right opens actions in the middle description column: x close, n new in project, o open folder and p copy project where applicable."
  - "The empty Home message box starts its footer with → options, followed by alt+p project, alt+e effort, alt+a approvals, alt+k chats and / commands where supported. Conversation boxes share effort, approvals, chats and commands; project cycling belongs to Home. Mac modifiers display as opt rather than alt."
  - "Home no longer advertises pick, Enter, ctrl+o, Tab or model-selection chords. Model selection uses /model or a press on the model; the old alt+o model shortcut is absent on Home."
  - "Home and conversation seams start with the full provider/model identifier, bold in the bright data hue, followed by separately styled :effort with no space, dot or badge. The approvals cell follows; Home places its clickable project path at the far right. Model and project hover underline, and conversation seams omit the git branch."
  - "The headed model picker and its alt+s / alt+shift+s sorting remain available alongside Sessions age-click sorting. Home shows draft per-model reasoning on the seam immediately; an explicit message-box effort choice takes precedence."
  - "Home project cycling uses every visible project and is sticky across alt+p and seam clicks. Project paths preserve their absolute root, abbreviate only the actual home prefix to ~, and truncate on the right. The redundant next-conversation-project footer sentence is gone."
  - "Shift+Enter inserts a newline in both message boxes. Home removes its placeholder on the first newline and keeps the cursor aligned with the edited line."
  - "Home no longer draws ask and new-conversation choice rows among search results. Enter on a draft starts a new conversation by default; /ask opens an inline answer with an animated activity indicator."
  - "A leading slash command token shows only the live-filtered alphabetical command list, scrollable above the seam on both Home and conversations. /approvals and /yolo are removed; approvals change through alt+a, the seam cell or settings."
  - "A failed attachment does not consume pasted text. A folder pasted into an empty Home box can offer a new conversation in that project; the next non-Enter input deactivates that offer, while Enter accepts it."
  - "Escape backs out of the current layer and ultimately reaches Home. Double-space navigation is removed. Conversation footers say esc home, task conversation footers say esc main, and Sessions, Spend and Settings say esc home at rest. Editors and filters dismiss before leaving their page."
  - "Conversation naming requests one five-to-eight-word phrase, without a separate tab label. Home and tabs use the same full title truncated to fit; new conversations use the initial prompt until naming finishes, and empty unsent tabs are not saved. Model control-token output is rejected both during naming and when reading old saved titles."
  - "The needs-you heading is gone. Conversation and task bullets distinguish answering, unread and unanswered questions; questions use a question-mark bullet with their actions retained. Since you left opens Memory, and the spend heading no longer repeats today's cost and allowance."
  - "Sessions contains conversation trees under running and completed. A whole tree moves together as its work changes state. Trees start expanded, newest activity comes first, and clicking age reverses sorting. Project has its own column, connectors show ancestry, and the collapse arrow follows the title in the left column."
  - "Stopped, interrupted, errored and incomplete ordinary tasks, quick tasks, designs and saved workflows can retry through their original runners. Successful or running tasks and separate background-job or adaptive-run summaries do not offer this action. Old records lacking required restart inputs explain the refusal."
  - "The compact task rail retains running-first ordering and finished-child folding without changing the full Sessions tree. Run tabs are views of their parent conversation and do not create duplicate Home or chats-menu rows or replace its title. The ctrl+g hide control sits above + /task in the conversation task panel."
  - "Interactive conversations default to YOLO, including remote conversations. Ordinary destructive commands and file overwrites can run without asking; tool-specific rules and the critical-command floor still apply. Headless runs require explicit authorization such as local --yolo or saved approval settings instead of inheriting the interactive default. Invalid saved approval values fall back to asking."
  - "Conversation approval choices and effort survive direct launch, picker reopen and engine reopen. An explicit --yolo launch outranks the saved approvals choice. Restoring one conversation's choice does not change the default for new conversations."
  - "Remote protocol version 17 replaces version 16 because clarification now keeps the original question pending while opening a discussion, ReplaceQuestion withdraws and replaces the request, and headless callers identify their lack of an approval resolver. Incompatible peers refuse at the handshake."
  - "Reattaching to an engine replays the last serving-provider sighting with its age. Direct model services have no provider rider; routed models retain their last serving machine until another answers."
  - "Settings search and model-picker carets live in their visible body fields. Other full-screen places have no general conversation composer; Home and conversations keep their shared message-box behavior."
---

This entry describes the final UX wave. Intermediate key bindings and layouts
have been removed so it can correct stale assumptions without introducing new
ones. Proposed additions to standing preferences and the design law are reviewed
separately from the product changes.
