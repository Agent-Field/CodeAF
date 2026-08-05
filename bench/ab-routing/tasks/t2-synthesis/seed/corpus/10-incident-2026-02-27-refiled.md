# Incident report — 2026-02-27 — auth outage (identity team filing)

**Affected service:** svc-auth
**Severity:** SEV-1
**Status:** closed
**Filed:** 2026-03-02 09:20 UTC by team-identity

## Summary

Filed by team-identity. Overnight authentication outage caused by an expired
mesh certificate between svc-auth and its session store. Renewal automation had
been failing silently since a runner image change.

## Timeline

- 2026-02-27 22:40 UTC — certificate expires.
- 2026-02-27 23:30 UTC — cause identified.
- 2026-02-28 00:10 UTC — certificate issued by hand.
- 2026-02-28 01:45 UTC — we consider the incident closed.

## Customer impact

**Impact lasted 3 hours and 5 minutes.** We measure from the page at 22:44
rather than from the expiry itself, and we close the window when the last
alert cleared on our dashboard.

## Note

team-platform also filed a report for this event. Both filings describe the
same certificate expiry on the same service on the same night; ours was raised
independently because the page reached two rotations. The numbers differ
because the two teams measured the impact window from different points.
