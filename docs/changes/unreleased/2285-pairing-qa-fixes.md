---
kind: fixed
title: Pairing QA fixes for failures, approvals, removed computers and the devices roster
pr: 2285
surface: [chat, engine, app, docs]
invalidates:
  - "A failed app pairing put its failure sentence in both the view's words and its error, so the app showed it twice, and a finished pairing kept showing its check number; now the failure is said once, in the error, and a finished pairing clears its words."
  - "A terminal approval that timed out said no answer from your devices, try again, even when the answer had been sent; now it says sync did not answer in time, and when an approval was already sent it adds that the answer may still have arrived and to check /devices before answering again."
  - "An approval from the terminal chat screen whose reply was lost left the joining computer unpaired and the link still asking; now that screen settles the approval it already gave, and opening there a link this computer already approved says the device is already paired instead of failing. Approvals from the desktop app and codeaf pair approve are unchanged."
  - "The terminal pairing question ended with a / d on the same line; now the keys are printed on their own line as a approve · d decline."
  - "A removed computer's chat said run codeaf pair to bring it back, and the app showed wireauth: device revoked; now the chat says the computer was removed and to run codeaf pair --replace, and the app says it was removed and to choose Pair a computer. The earlier sentence is still recognised as a removal notice."
  - "A removed computer could not pair back from the app because the join was treated as already joined; now a removed computer's own pairing from the app replaces its device key instead of reusing the revoked one. Two-computer testing still showed neither computer listing the other afterwards, so re-adding a removed computer is not finished."
  - "Every devices roster read published devices.changed because this computer's own last-seen stamp moved, so the app re-read the roster in a loop; now a read whose only difference is last-seen stamps of this computer or online rows publishes nothing."
  - "A chat that could not be shared, or a continue that failed, left no reason in the log; now sharing logs each chat and reason once, and a failed continue logs reason=."
---

The pairing code instruction now says dashes are optional, and the devices
manual page is corrected for how a removed computer rejoins.
