# security-audit — a security audit of your code

## What security-audit is — a security review, a vulnerability scan, find security bugs in my code, sec-af

security-audit is a program codeaf carries that audits code for security problems: injection, broken authentication, server-side request forgery, cross-site scripting, weak cryptography, exposed secrets and data, unsafe dependencies, denial of service, business-logic and API flaws. It reads the code, hunts for problems, then tests each one against the code to show it can really be exploited or to rule it out, and reports only what stands, each with its file, line and a suggested fix.

It is sec-af, the security-audit program from AgentField, built into codeaf. There is nothing to install and no key to set: every model call it makes goes through codeaf, so it is priced into your spending and held to the run's ceiling like any program's (see *Programs codeaf carries*). It does not run on its own outside codeaf.

**It changes nothing.** It reads your folder and writes nothing there: no branch, no commit, no file. Its answer comes back to the conversation, and its full report goes to the task's record folder.

## How do I run a security audit — /security-audit, codeaf security-audit, audit the whole repository, ask the chat for a security review

In the chat, type `/security-audit` on its own to audit the whole repository the conversation is in, or add words:

```
/security-audit
/security-audit changes
/security-audit changes since main
/security-audit whole repository thorough
```

It starts as a task at once, shows on the rail with what it is doing (`mapping the code`, `hunting`, `testing what it found`, `writing fixes`, `writing the report`), and can be stopped. The turn goes on while it runs.

**Asking in words works too.** "Do a security review of this repo" or "audit my changes for security problems" makes the chat propose the work with `via: "security-audit"`; you answer its card like any proposal's.

At a shell, `codeaf security-audit` audits the folder you are in, or the one `--dir` names. `codeaf security-audit --changes` audits the changes, `--base <ref>` measures them from another commit, and `--depth quick|standard|thorough` sets how hard it looks. `--max-cost` and `--max-hours` set its ceilings. `codeaf security-audit run --help` lists every flag.

## Audit only my changes — this branch, a pull request, uncommitted work, since main, a diff

`/security-audit changes` (or `--changes` at a shell) audits the work on your branch instead of the whole repository: the commits since the branch left its base, **plus your uncommitted edits and files not yet tracked**, as the work stands now. The base is where your branch left the remote's default branch (`origin/HEAD`), else `origin/main`, `origin/master`, `origin/dev`, `main`, `master`, `dev` or `trunk`; on the default branch itself it is your last commit, so the changes are what you have not committed. `changes since <ref>` (`--base <ref>`) names another base.

Only code counts as a change: docs, config, lock files and data files (`.md`, `.txt`, `.yml`, `.yaml`, `.json`, `.toml`, `.cfg`, `.ini`, `.lock`) and anything under a top-level `tests/`, `test/`, `vendor/` or `node_modules/` are left out. The files near the change, those that name a changed file's module, come along, because a change is unsafe where its callers meet it.

The hunters are shown the change and told to report only what it touches or makes reachable; the rest of the code is read for context. Findings outside the changed and nearby files are dropped, and at most 15 findings are tested unless `--max-provers` says otherwise. With nothing changed it ends at once: `security-audit had nothing to audit: nothing has changed since <base> in a file this audit reads (code, not docs or config)`. Outside a git repository it is refused: `an audit of the changes needs a git repository with at least one commit; audit the whole repository instead`.

## What security-audit does, step by step — why it takes so long, how it works, the phases

1. **Mapping the code** (`map` on its page): agents read the architecture, the dependencies, the configuration, and (except at `quick`) the data flows and security context.
2. **Hunting** (`hunt`): a hunter per kind of problem — injection, denial of service, request forgery, authentication, data exposure and secrets in configuration always; cross-site scripting and business logic at `standard` and `thorough`; cryptography, supply chain and API security when the code has them. Each finds candidate locations and describes each one; duplicates are merged.
3. **Testing what it found** (`test`): the most serious findings first, each goes through four agents — one traces the data from its source to the dangerous call, one looks for sanitization on the way, one builds a concrete attack, and one weighs the three. Findings come out confirmed, likely, unclear or ruled out. `quick` tests at most 10, `standard` 30, `thorough` all.
4. **Writing fixes** (`fix`): a suggested fix for each confirmed or likely finding.
5. **Writing the report** (`report`).

Up to eight agent sessions run at once, each reading the code with up to fifty turns of its own. A standard audit runs a hundred or more of them, which is why it takes minutes to an hour. The task's page shows one line per agent session — which agent, in which step, how it came out — and `ctrl+y` shows the raw model calls.

## What security-audit reports and where the full report is — confirmed, likely, unclear, ruled out, SARIF, JSON, Markdown

When it finishes, the chat is handed a short account and tells you what it found, most serious first: how many problems are **confirmed** (shown exploitable from the code) or **likely**, by severity, then each one on a line — severity, how it came out, title, `file:line`, the weakness's CWE number and a one-line fix. How many stayed **unclear** or were **ruled out** is counted. A finding below `--severity` (default `low`) is left out.

The full report is three files in the task's record folder, named at the end of the account:

- `security-audit.md` — the readable report, with every finding's trace and attack
- `security-audit.json` — every finding in full, as data
- `security-audit.sarif` — SARIF 2.1.0, which code-scanning tools and editors read

`--compliance owasp,pci-dss` (at a shell) maps the findings to compliance frameworks — OWASP, PCI-DSS, SOC2, HIPAA, ISO27001 — and adds `security-audit-compliance.md`. A shell run's record folder is under `~/.codeaf/v3/carried/security-audit/`, one folder per run.

## Does security-audit change my files — read only, no shell, will it edit or commit anything

No. Its agents have four tools — read a file, list a folder, find files by pattern, search file contents — and nothing that writes; there is no shell, so it runs none of your code, tests or builds. Every path is held inside the folder it audits: a path or a link that leads out of it is refused. It makes no branch and no commit, and its working files (its checkpoints, its report) go to the task's record folder, never into your folder. The chat keeps working in the folder while it runs.

## What a security audit costs and how long it takes — limit, ceiling, $5, two hours, quick, thorough

A run nobody set a limit for stops at **$5 and two hours**, or sooner where this conversation has less left; `/budget conversation` can lower that, and at a shell `--max-cost` and `--max-hours` set either. The proposal card and the start line name the ceiling: `up to $5.00 and 2h`.

What a run spends depends on the repository and the models: a `quick` audit of a small project is cents; a `standard` audit of a mid-sized one is commonly under a dollar or two on an inexpensive model, and a `thorough` one more. It keeps three minutes of its time ceiling back to write its report. A run that reaches its dollar ceiling, or its time ceiling, before the report ends with `security-audit reached the run's dollar ceiling before it finished, so it has no report` (or `time ceiling`), and the chat asks whether to spend more rather than running it again.

**Which models.** From the chat, the conversation's working seat reads the code and its light seat makes the single quick calls (merging duplicates, the compliance mapping). A model you name for the hand-off reads the code in place of the working seat. At a shell, the profile's work seat does both unless `--model` (and `--light`) name others.

## Fixing what security-audit found — hand the findings to senior-dev, fix the vulnerabilities

When the audit finds confirmed or likely problems, the chat offers to hand them to senior-dev as one task that fixes them, and names which. It starts nothing until you say yes; then it proposes that task, and you answer its card as usual. You can also ask directly: "fix the two critical findings with senior-dev".

## What security-audit cannot do — URLs, running the app, a CVE database, Windows, telling it something

- **It audits a folder on this machine, never a URL.** To audit a repository you have not cloned, the chat clones it into a new folder first and audits that.
- **It runs nothing.** It does not start your app, send requests to it or run exploits; what it shows exploitable it shows from the code.
- **It looks up nothing online.** Known vulnerabilities in your dependencies come from what its models know, not from a live advisory database, so a very recent advisory can be missed.
- **It cannot be told anything while it runs.** It reads no messages; stop it and start it again with other words.
- **It is absent on Windows**, as every program codeaf carries is there.
