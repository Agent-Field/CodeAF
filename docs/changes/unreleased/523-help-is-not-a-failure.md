---
kind: fixed
title: asking a command for help is not a failure any more, and every command has a usage
pr: 523
surface: [build]
invalidates:
  - "Asking any subcommand for help was an error exit: `aforge <anything> --help` printed Go's own internal sentinel `error: flag: help requested` and exited 1. On nine of them — doctor, logs, why, wake, notebook, competence, rebuild, cache and their kin — that one line was the whole answer, with no usage at all, so the only way to learn what those commands take was to read the source. Now `--help` (and `-h`) prints that command's own usage on stdout and exits 0, on every command including the three that parse no flags."
  - "`aforge show --help` answered `open --help: no such file or directory` — a filesystem error about a flag — because that door read its first argument as a path, and `aforge models --help` ran the command with the flag silently ignored and exited 0. Both now print their usage and exit 0."
  - "A bad flag was reported twice: the flag package printed `flag provided but not defined: -x` and dumped the whole flag list, and then the dispatch printed the same sentence again under `error:`. It is one fact said once now — the sentence and that command's usage, on stderr, exit 1, and nothing on stdout."
  - "A per-command usage did not exist, so there was nothing to keep in step with the table `aforge --help` prints. Now every one of them is READ OUT OF that table (`usageForCommand` in cmd/aforge/usage.go), so there is no second synopsis that can drift."
---

Every flag set in `cmd/aforge/` is now built and parsed at one seam, `cmd/aforge/usage.go`,
because the same three lines had been copied into eight doors and left out of ten others and
the two halves disagreed about the first gesture any developer makes. A Makefile that ran
`aforge do --help` to check the binary was healthy read a failing command; a person reading
`error:` over their own request read the word for something that had gone wrong.
