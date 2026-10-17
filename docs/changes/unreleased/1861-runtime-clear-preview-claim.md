---
kind: fixed
title: Clarify source-read claims for runtime-field clears
pr: 1861
surface: [engine]
invalidates: []
---

The protocol now states explicitly that a structured preview clearing the last runtime key still requires the attested source read. The pull request route’s result-text seal is a separate check; text without runtime keys does not waive the preview claim. Runtime behavior is unchanged.
