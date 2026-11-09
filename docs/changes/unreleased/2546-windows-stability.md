---
kind: fixed
title: Windows sessions can finish - stops that stop, state that survives, a first run that works
pr: 2546
surface: [chat, engine, resident, remote, factory, app, build]
invalidates:
  - "Windows teardown depended on live parent PID chains, so npm's cmd.exe chain kept serving after Stop. Launches now run inside a Job Object that survives shim and parent exits."
  - "A stop could report completion after its waits expired. An unverified ending now stays running and sends no stopped note."
  - "Windows could not register an open window for the automation clock, so due reminders never fired. Presence now keeps its lock while publishing and removing its window file."
  - "Append-only Windows usage handles could not take the ledger lock, losing spending history and weakening the daily cap. Accounting now locks its matching read handle."
  - "A current spending window could omit calls sharing the clock's current reading. Its default end now includes that reading, while explicit end times remain exclusive."
  - "Permanent conversation deletion and sync, service and task state updates failed on Windows file semantics. They now use the shared fsreplace operations, which replace a held file with a POSIX-semantics rename where Windows supports it."
  - "Fresh app setup omitted its default model and could rank a tiny one first. It now supplies the CLI default; saved model choices remain intact."
  - "Launch and doctor trusted the presence of an OpenRouter key. Launch now checks it once in the background and reports ok, invalid or unknown; a 401 on a real turn marks it invalid."
  - "Doctor omitted git identity, gh authentication and Windows shell readiness. Doctor and health.check now report them with bounded probes and fix hints."
  - "Windows could not open the browser for sign-in, and the Git Bash install left codeaf off PATH. The opener now uses the file protocol handler and the installer updates the user PATH or prints the command."
  - "Headless foreground commands loaded a login profile each time, about 300 ms per command on Windows. They now use non-login bash and inherit the launching environment, like chat."
  - "BOM-marked UTF-16 files reached file tools as raw bytes. Reads now decode them, and edits, rewrites and appends keep their encoding, BOM and line endings."
  - "Folder browsing and report discovery assumed slash-prefixed paths. They now accept native Windows drive and UNC paths."
  - "On Windows the desk could not preview a new file's diff or open a changed file. The contained open now works there through the same os.Root walk."
---

POSIX process groups, signals, replacement and removal keep their behavior.
Background jobs, services and health checks keep login shells so their login
profiles can still supply PATH; health checks now pick Git Bash on Windows.
Windows shell facts tell the model about the Python Store stub, `.cmd` shims and
using jobs to start and stop servers. A new advisory CI job runs the tests that
pass natively on Windows.
