---
kind: fixed
title: A factory job clones a repository it has no checkout of; a start that cannot clone writes nothing
pr: 1899
surface: [engine, app, chat, docs]
invalidates:
  - "Was true: the factory's clone door existed but nothing called it, so a job on a repository this machine had no checkout of (in the app, every repository: the workspace never matches one) could not be started from anywhere; Start, the floor's launch and the manager's `factory_start` all refused with `not run · codeaf does not know where acme/api is checked out`. Is true now: the runner clones the repository on the run's first step into `~/.codeaf/v3/factory/repos/<owner>/<name>`, records it as the checkout, and the job's log says `cloning acme/api…`; two jobs of one repository started together clone it once. A clone that fails fails that step with git's reason (`could not clone acme/api · repository not found`), and Run again tries again."
  - "Was true: the clone went over `gh repo clone` when gh was installed, which follows the person's git_protocol and may use ssh. Is true now: it is always `git clone https://github.com/<owner>/<name>.git` with the factory's GitHub token as an http header in git's environment, the token the poll and the push use."
  - "Was true: a start is refused for having no checkout whenever this machine knows none. Is true now: a start is refused only when the repository cannot be cloned: 409 `reason` `needs_checkout` with `acme/api isn't checked out on this machine · connect GitHub so the factory can clone it.` (no GitHub token) or `notes isn't checked out on this machine · watch it in Repos so the factory can clone it, or open codeaf in its folder.` (not a repository the factory can name on GitHub). The header's Start, `factory_start`, run, run_again and `POST /v1/jobs` with autostart refuse alike, and the refusal is typed (`factory.NeedsCheckout`), not matched by its words."
  - "Was true: start_now wrote the job's mark and its budget before the launch could refuse, so every refused start moved the job's version; an unknown refusal was a 500 with no reason, the app turned it into `uncertain` and dropped its words, then re-read the job, saw the version moved, and said the job had changed since the person asked. Is true now: every refusal of a start (the question it waits on, GitHub's word that it ended, the repository being off, the checkout, the recipe read) comes before any write; then the mark and the budget go in one write, only when either is missing, and a launch that still refuses takes the mark back. run_again clears a failed run only after the same refusals."
  - "Was true: the app answered any 5xx from a job write with only `the factory failed while acting; …`. Is true now: it is still `uncertain`, and a 5xx carrying the factory's own `error` sentence keeps it in front: `<sentence> · the action may have happened, so read the job before trying again`."
  - "Was true: a job's read said nothing about where its repository is checked out, and the queue's repositories only carried a `checkout` path when one was known. Is true now: `GET /v1/jobs/{id}` (`factory.job`) carries `checkout`: `here`, `will_clone` or `missing`, never a path, absent on a factory that does not know its machine's checkouts; each repository on the queue carries `checked_out: true` when a checkout is known."
---

Not done: `Store.Update` still writes, and moves the job's version, when a
change leaves the job as it was. The lease's plan-revision tests rely on a
write that changes nothing a person would see still moving the version, and
every Update stamps the job's changed time, so a no-op skip would change what
those writes mean. The start no longer makes such a write.
