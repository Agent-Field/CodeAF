---
kind: fixed
title: Kept copies open from Home, held chats refetch failed files, and a deleted folder opens
pr: 2792
surface: [chat, engine, app, docs]
invalidates:
  - "The chat manual said to press enter on the kept turns' row to open or discard a copy kept from unsaved edits, but Home folds every copy into a dim `N copy` note on its chat's row, so that row never existed and the copy could not be reached. On a chat this computer holds, Home's strip now names `k kept turns` while the row has copies: select the row, press `→`, then `k` for the `What should happen to these turns?` card, one copy per press. A chat another computer holds still shows its copies as a count only; continue it here first."
  - "Opening a chat this computer already holds (launch, Home, resume, or the app's `agents.open`) never restarted files that failed to arrive, so workspace tools and saves stayed refused until the chat was taken again. Opening it now fetches the files again when the earlier arrival was abandoned and its owner is gone. It never starts a second arrival while a live one is on its way, and never touches a chat another computer holds."
  - "A chat whose project folder was deleted on this computer failed every launch with `could not be put in this folder`, because the local-edits check read the missing folder. A missing folder now means there are no local files to keep: only the conversation is checked, the chat opens in its own copy folder with the files arriving there, and the deleted path is not recreated. Unsaved words in that case are still kept as a branch. A file where the folder was, or an unreadable folder, is still refused."
  - "When the check of this computer's copy before a continue failed, the card blamed the folder (`could not be put in this folder`). It now says `could not check this computer's copy before continuing; your words are still in the box`, and the cause goes to the engine log."
---

Follow-ups found while hand-testing the fast chat move (#2714), plus two test fixes:
`TestALedgerWriteThatFailsIsCountedRatherThanSwallowed` no longer races a real two-second
ceiling against a real retry timer (#2784), and the `cmd/codeaf` factory and memory tests
no longer lose `bash` and the shell tools when their helper narrows `PATH` to git's
folder, which is `/usr/local/bin` on some Linux machines and `/usr/bin` on macOS (#2621).
