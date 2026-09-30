---
kind: fixed
title: Flags after the brief, run-engine token counts, long-home ssh paths and auth failures
pr: 1669
surface: [engine, remote, docs]
invalidates:
  - "Flags after the brief (`codeaf senior-dev \"<brief>\" --max-cost 0.5`) were folded into the brief and the default ceiling applied. They are parsed wherever they sit for every delegate program, an unknown flag is refused, and `--` still ends flags."
  - "`codeaf do --json` on the run engine reported zero tokens beside a non-zero spend. The run engine's workers now carry input and output tokens to the receipt."
  - "The ssh control-path check ignored OpenSSH's 17-character temporary suffix, so `--host` under a long codeaf home failed to dial. Both the final and the temporary path must fit, or multiplexing is left off."
  - "A senior-dev run that ended with passing checks but no submission said only that it ended without submitting. It says the checks passed, names the kept tree and gives the command to submit it; the exit code is still 2."
  - "A 401 or 403 was retried as a provider 5xx by the chat and senior-dev. It is a terminal auth failure everywhere, the failure names the key's source on the default service, and `codeaf do` carries the reason into its error field. The key order is unchanged."
---

Re-cut from the docs-audit batch. `--workspace` still defaults to the home directory; that item of the CLI audit stays open.
