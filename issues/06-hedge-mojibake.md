# 06 · Reject a mojibake hedge winner before persisting
Pareto: TRUST. Evidence: F20/F21 — a 429-rescued stream persisted mojibake as the turn.

Fix: validate a rescued/hedged stream (decode sanity, printable ratio, finish_reason)
before accepting it as the assistant turn; on failure, fall back or say so.

Test: a corrupted rescued stream is rejected and never persisted to the transcript.
