---
kind: fixed
title: Flags after the brief, run-engine token counts, long-home ssh paths and auth failures
pr: 1700
surface: [engine, remote, docs]
invalidates:
  - "Flags after the brief (`codeaf senior-dev \"<brief>\" --max-cost 0.5`) were folded into the brief and the default ceiling applied. They are parsed wherever they sit for every delegate program, an unknown flag is refused, and `--` still ends flags."
  - "`codeaf do --json` on the default run road reported a positive `spend` with both spend halves at zero, zero tokens and an empty `workspace`. The halves now add up to `spend`, `tokens` are the call totals (including calls in a turn that later errored and auxiliary summary calls), and `workspace` is the absolute project directory."
  - "The ssh control-path check ignored OpenSSH's 17-character temporary suffix, so `--host` under a long codeaf home failed to dial. Both the final and the temporary path must fit, or multiplexing is left off."
  - "A senior-dev run that ended with passing checks but no submission said only that it ended without submitting. It says the checks passed, names the kept tree and gives the command to submit it; the exit code is still 2."
  - "A 401 or 403 was retried as a provider 5xx by the chat and senior-dev. It is a terminal auth failure everywhere: senior-dev stops without another model call (landing turn included), the explanation names the source of the key the served model actually used (a direct provider no longer names OPENROUTER_API_KEY), and `codeaf do` carries the reason into its error field. The model API still answers an upstream account refusal with 502, by design. The key order is unchanged."
  - "`codeaf disconnect --help` said it forgets a service, and connect's `--region` described a service region. Both say provider."
---

Re-cut of #1669 (itself re-cut from the docs-audit batch) onto current dev. `--workspace` still defaults to the home directory; that item of the CLI audit stays open.
