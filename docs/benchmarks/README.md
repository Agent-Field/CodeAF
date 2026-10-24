# Benchmarks

Classified by what is being measured.

- [deepswe/](deepswe/): coding task quality on the full DeepSWE set. codeaf,
  run in senior-dev mode, beside the other harnesses, with the tested versions,
  release dates, and the run record.
- [performance/](performance/): the footprint of the CLI itself. Disk,
  startup, memory at rest, memory as parallel sessions multiply, memory over
  a long session, and behaviour through interrupts and crashes. Seven agent
  CLIs, one harness, every table regenerable from the scripts beside the
  results.
