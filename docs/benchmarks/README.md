# Benchmarks

Classified by what is being measured.

- [performance/](performance/): the footprint of the CLI itself. Disk,
  startup, memory at rest, memory as parallel sessions multiply, memory over
  a long session, and behaviour through interrupts and crashes. Seven agent
  CLIs, one harness, every table regenerable from the scripts beside the
  results.
- [deepswe/](deepswe/): task-solving outcomes for senior-dev, the coding
  harness vendored into this product, on the 113 scored tasks of the DeepSWE
  v1.1 corpus. Three campaigns — a ten-harness comparison and two single-arm
  model sweeps — as one per-task table each, with reward, F2P, P2P, wall time
  and cost. The protocol for this repository's own `codeaf` runs on the same
  corpus is separate, in [`bench/deepswe/`](../../bench/deepswe/).
