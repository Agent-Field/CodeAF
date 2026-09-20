# Launched process tree recovery, 2026-09-18

## Finding and timeline

The controlled probe command was:

```text
python3 /tmp/codeaf-late-child-probe-parent.py &
```

Its retained evidence in `/tmp/codeaf-late-child-probe.log` sampled these trees:

```text
sample elapsed_ms=1 tree_pids=424
sample elapsed_ms=557 tree_pids=424
sample elapsed_ms=1260 tree_pids=424 433
sample elapsed_ms=2014 tree_pids=424 433
```

The helper appeared between 557 ms and 1260 ms and remained a descendant through 2014 ms. Exact-PID cleanup first confirmed PID 433 as `/usr/bin/sleep 30` and PID 424 as the probe parent, then confirmed both `/proc` entries absent.

The benchmark walk did not already see such a helper because Linux proc `children` files do not end in a newline. Bash `read` filled the child array, returned nonzero on EOF, and the old `|| continue` discarded the populated array. Every walk therefore reported only its root. The four-second settle was also unsampled, so a child that detached there would be unreachable when idle sampling began. A helper first created during teardown remains outside the measured frame, settle and idle interval and is not counted.

The fix tests the populated child array rather than `read` status. It samples from launch through frame, settle and idle, retaining each PID discovered below the launched pane root while that PID remains alive. This is one label-neutral rule for every measured command. It has no global, profile or workspace discovery seed, so processes in `prior_own_processes` cannot enter the count.

## Mutation verification

Command:

```text
docs/benchmarks/test-process-tree.sh
```

Output:

```text
command: LATE_HELPER=1 IDLE_SECONDS=2 SETTLE_SECONDS=2 STARTUP_RUNS=1 STARTUP_WARMUP=0 WORKDIR=/tmp/tmp.d0TEnvL0wg/work /home/santosh/src/c262tree/docs/benchmarks/measure-cli.sh helper-one-second /tmp/tmp.d0TEnvL0wg/home /home/santosh/src/c262tree/docs/benchmarks/test-process-tree.sh run
name=helper-one-second phase=proc pid=87777 comm=bash rss_kb=3368 pss_kb=312 peak_rss_kb=3368 threads=1 fds=4 cmd=bash /home/santosh/src/c262tree/docs/benchmarks/test-process-tree.sh run 
name=helper-one-second phase=proc pid=87814 comm=sleep rss_kb=1796 pss_kb=104 peak_rss_kb=1796 threads=1 fds=3 cmd=sleep 30 
name=helper-one-second phase=summary startup_tool=shell-loop startup_ms=4 first_paint_ms=39 frame_ms=39 trust_gate=absent procs=2 rss_kb=5164 rss_kb_median=5164.0 rss_kb_max=5164 pss_kb=416 peak_rss_kb=5164 threads=2 fds=7 cpu_pct_of_one_core=0.00 voluntary_ctxsw_per_s=0.00 window_ms=2037
command: LATE_HELPER=0 IDLE_SECONDS=2 SETTLE_SECONDS=2 STARTUP_RUNS=1 STARTUP_WARMUP=0 WORKDIR=/tmp/tmp.d0TEnvL0wg/work /home/santosh/src/c262tree/docs/benchmarks/measure-cli.sh no-helper /tmp/tmp.d0TEnvL0wg/home /home/santosh/src/c262tree/docs/benchmarks/test-process-tree.sh run
name=no-helper phase=proc pid=88063 comm=sleep rss_kb=1800 pss_kb=100 peak_rss_kb=1800 threads=1 fds=3 cmd=sleep 30 
name=no-helper phase=summary startup_tool=shell-loop startup_ms=3 first_paint_ms=43 frame_ms=43 trust_gate=absent procs=1 rss_kb=1800 rss_kb_median=1800.0 rss_kb_max=1800 pss_kb=100 peak_rss_kb=1800 threads=1 fds=3 cpu_pct_of_one_core=0.00 voluntary_ctxsw_per_s=0.00 window_ms=2033
```

The first case creates its helper one second after launch and reports two `phase=proc` lines and `procs=2`. The second case creates no helper and reports one `phase=proc` line and `procs=1`. The fixture creates no load and the benchmark tears down only the launched instance tree.
