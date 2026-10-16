---
kind: internal
title: provider can tell an optional observer about each actual completion send
pr: 0
surface: [engine]
invalidates:
  - "Was true: nothing outside the provider could see a completion's individual sends, so a retry, a repaired body, a hedge arm or a discarded answer was only visible as one call. Is true now: a context may carry one `ActualAttemptObserver` (`provider.WithActualAttemptObserver`) that is asked immediately before EACH completion `Do` in `Client.send` and returns a token told that send's transport end and price. Nothing installs one yet; with none installed the sends, retries, hedging and billing sinks behave exactly as before."
  - "Was true: a cut stream's late receipt was priced only through `Billed`, whose `Empty()` cannot tell a stated zero cost from a missing one. Is true now: the observer's token receives the typed fact (known including exact zero, unknown for missing/negative/non-finite or a not-ready all-zero receipt) straight from final usage or its receipt; `BillingSink`, `ReconcileSink` and `ReceiptPending` are unchanged."
  - "Was true: probes, the document parser, media jobs and the SDK tool loop could be called under any context. Is true now: with an observer installed they return a typed `UnsupportedAttemptSurfaceError` (probes simply buy nothing) before any network use; `provider.RequireActualAttemptCoverage` is the preflight. Only the completion adapter is covered; search, shell, delegation and the Codex adapter are not claimed, and an observed send never follows a redirect."
  - "Was true: no failure meant 'the caller's own admission authority said no'. Is true now: `taxonomy.Evidence.AdmissionRefused` (set by `provider.Evidence` for an observer's `AttemptDeniedError` or an `UnsupportedAttemptSurfaceError`, before any prose is read) classifies as a generic report with reason `taxonomy.ReasonAdmissionRefused`; the hedge walk, endpoint walk and model hop do not buy past it. It is not `ReasonUnauthorized`."
---

Foundation only: no cap, ledger, UI or manual claim rides on it. A custom
`Config.HTTPClient` is counted only if it declares `SingleSendTransport`.
`funnel_law_test.go` names `readReceiptAt` as the receipt sender now that it
holds the request (`fetchReceiptAt` keeps its old tuple view).
