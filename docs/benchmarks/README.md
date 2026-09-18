# How the footprint numbers are measured

`measure-cli.sh` measures one command-line agent and prints `key=value` lines. It takes a
name, a HOME to run under and the command to run, so it measures any CLI rather than this
one. The README's table is its output.

```bash
docs/benchmarks/measure-cli.sh --name codeaf --home /tmp/bench-home -- ./bin/codeaf chat
```

Run it once per CLI, on the same machine, in the same working directory, within the same
session. Numbers from different machines or different days do not belong in one table.

## What it measures

- **Cold start** — wall clock for `--version`, best of N. It uses `hyperfine` when that is
  installed and a `date +%s%3N` loop when it is not, and says which in its output.
- **First interactive frame** — time from launch until the terminal first paints a
  non-blank character, measured in a tmux pane on a private socket.
- **Idle cost** over a 30 s window, sampling the whole process tree: RSS, PSS, peak RSS
  (`VmHWM`), thread count, open file descriptors, CPU from `utime+stime` deltas, and
  voluntary context switches per second.

## Four rules the numbers depend on

**Run in a real repository, not an empty directory.** Some of these CLIs index the working
tree at startup. In an empty directory that cost is invisible, and the comparison flatters
everything that does it.

**Use an authenticated profile.** A fresh profile measures a login screen or a
folder-trust dialog, not a working session. Two of the CLIs compared read more than 120 MB
higher once actually authenticated in a repository — measured the naive way, every
competitor is understated.

**Report PSS, not RSS, for anything with more than one process.** CodeAF runs a surface and
a detached engine daemon that share one binary's text pages; RSS charges that memory to
both. PSS charges shared pages once. Using RSS here would overstate this binary's own
footprint by about 40 MB, so the honest number is also the less flattering discipline.

**The box has to be quiet. The script does not make it quiet.** It kills only the processes
it started, by pid, and it runs its own tmux server on a private socket so its teardown
cannot reach anything else. It contains no `pkill`, `killall` or other kill-by-pattern, and
neither should anything added to it: `pkill -f` matches the whole command line of every
process the user owns, so a pattern as ordinary as `sleep 60` will take down any unrelated
job whose arguments happen to contain it. If other work is running on the machine, the
idle and CPU figures are measuring that work too — wait, do not clear.

## What these numbers are not

They are a measure of what the binary costs to install and to run: size, startup, and
memory at rest and during one short turn. They say nothing about whether the work is any
good. Task quality — pass rate, cost per issue, time per issue — is a separate measurement
and is not in this table.

## Known limits

- `perf stat` is unavailable on many hosts (`perf_event_paranoid`), so idle CPU is computed
  from `/proc/<pid>/stat` deltas across the process tree rather than from hardware counters.
- **The wakeup figure is noisy.** Repeated runs of one unchanged build have produced
  anything from 0 to 84 voluntary context switches per second. Treat it as a direction, not
  a rate, and do not put it in a comparison table.
- First-frame timing depends on terminal size; the script fixes the pane geometry so runs
  are comparable, but a different geometry gives different numbers.
- Peak RSS is a high-water mark and is sensitive to what the CLI did before the sample.
