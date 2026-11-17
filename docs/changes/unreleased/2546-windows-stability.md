---
kind: fixed
title: Windows sessions can finish - stops that stop, state that survives, a first run that works
pr: 2546
surface: [chat, engine, resident, remote, factory, app, build]
invalidates:
  - "Windows teardown depended on live parent PID chains, so npm's cmd.exe chain kept serving after Stop. Launches now run inside a Job Object that survives shim and parent exits."
  - "A stop could report completion after its waits expired. On Windows an unverified ending now stays running and sends no stopped note; Unix keeps its existing signal receipts and waits."
  - "Windows could not register an open window for the automation clock, so due reminders never fired. Presence now keeps its lock while publishing and removing its window file."
  - "Append-only Windows usage handles could not take the ledger lock, losing spending history and weakening the daily cap. Accounting now locks its matching read handle."
  - "A current spending window could omit calls sharing the clock's current reading. Its default end now includes that reading, while explicit end times remain exclusive."
  - "Permanent conversation deletion and sync, service and task state updates failed on Windows file semantics. They now use the shared fsreplace operations, which replace a held file with a POSIX-semantics rename where Windows supports it."
  - "Fresh app setup omitted its default model and could rank a tiny one first. Every OS now supplies the CLI default while preserving all saved model choices."
  - "Launch could probe OpenRouter before any setup or health read. Every engine now checks it once in the background when setup or health is first read and reports ok, invalid or unknown; reads never wait, key handovers also start the check, and a 401 on a real turn marks it invalid. Doctor checks directly on every OS."
  - "Doctor omitted git identity, gh authentication and Windows shell readiness. Doctor reports them on every OS with bounded probes and fix hints. Windows desk health reads also run them; Unix desk setup/health reads run no git or gh commands, avoiding login Keychain prompts."
  - "Windows could not open the browser for sign-in, and the Git Bash install left codeaf off PATH. The opener now uses the file protocol handler and the installer updates the user PATH or prints the command."
  - "Windows headless foreground commands loaded a login profile each time. Windows now uses non-login bash and inherits the launching environment; Linux and macOS retain login profiles."
  - "BOM-marked UTF-16 files reached file tools as raw bytes. Reads now decode them, and edits, rewrites and appends keep their encoding, BOM and line endings; ordinary Unix writes and headless edits retain literal bytes."
  - "Folder browsing and report discovery assumed slash-prefixed paths. They now accept native Windows drive and UNC paths."
  - "On Windows the desk could not preview a new file's diff or open a changed file. The contained open now works there through the same os.Root walk."
---

POSIX process groups, signals, replacement and removal keep their behavior.
Background jobs, services and health checks keep login shells so their login
profiles can still supply PATH; health checks now pick Git Bash on Windows.
Windows shell facts tell the model about the Python Store stub, `.cmd` shims and
using jobs to start and stop servers. A new advisory CI job runs the tests that
pass natively on Windows.

Linux and macOS retain foreground login shells, command output bytes (including
CRLF), ordinary file rewrite bytes, health-command pipeline status, Unix writer
locks, folder parent spellings, and non-login patch shell dispatch. Existing homes
and wire methods, fields and events stay compatible. Provider key health, fresh
setup defaults and explicit doctor checks are shared on every OS; their wire
fields remain optional. Only Windows desk health reads run git/gh readiness
commands. UTF-16 support and the current-clock spending fix remain shared
corrections. The C27 quiet-new-chat law keeps launch auth silent on every OS.
