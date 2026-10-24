# Benchmarks

Build the binary with `make build`. The issue benchmark requires Git, Python 3,
GitHub CLI authentication, and `timeout` (`gtimeout` on macOS).

Preview one cell without running a model:

```sh
CODEAF_MODE=node ISSUES=21 HARNESSES=codeaf bench/run.sh --dry-run
```

Run cells against a repository and fixed commit:

```sh
REPO=https://github.com/example/project BASE_COMMIT=<commit> \
ISSUES="1 2" HARNESSES=codeaf MODEL=<model> bench/run.sh
```

Configure the provider credentials before running cells. Each cell uses a fresh
clone, installs the target repository's test dependencies, runs the selected
command, and records the test output and a CSV row in the results directory.
Set `RESULTS` to choose that directory and `CELL_TIMEOUT` to limit each cell.

Check the benchmark helpers without calling a model:

```sh
make test-tooling
```

Summarize a model-call log without modifying it:

```sh
make census LOG=/path/to/calls.jsonl OUT=/tmp/census.md
make census TOP=40 DAYS=7
```
