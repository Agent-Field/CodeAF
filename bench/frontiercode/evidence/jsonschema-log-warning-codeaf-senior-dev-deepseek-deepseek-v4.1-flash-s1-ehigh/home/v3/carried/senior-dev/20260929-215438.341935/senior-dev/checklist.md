# Checklist

[x] Add `LOG_WARNING()` to `src/logger.h` next to `LOG_VERBOSE`, returning `std::ostream &` and always writing to `std::cerr` with a `warning: ` prefix
[x] `LOG_WARNING()` ignores `--verbose` and every other option (always emits)
[x] Convert the warning site in `src/command_lint.cc` to `LOG_WARNING()`
[x] Convert both warning sites in `src/command_validate.cc` to `LOG_WARNING()`
[x] Convert the multi-line warning in `src/resolver.h`, including every continuation statement, to a single prefixed `LOG_WARNING()`
[x] Convert the multi-line warning in `src/command_bundle.cc`, including every continuation statement, to a single prefixed `LOG_WARNING()`
[x] No remaining ad-hoc `warning: ` / `std::cerr` warning sites in the CLI sources
[x] Update `test/validate/pass_jsonl_empty.sh` expectation for the now-unconditional JSONL warning
[x] `make configure compile` succeeds and the clang-format target is clean
[x] `ctest --test-dir build -R 'pass_jsonl_empty' --output-on-failure` passes
