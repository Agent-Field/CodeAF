[x] `src/logger.h` declares `inline auto LOG_WARNING() -> std::ostream &` next to `LOG_VERBOSE`
[x] `LOG_WARNING()` always writes to `std::cerr`, independent of `--verbose`
[x] `LOG_WARNING()` writes the `warning: ` prefix itself and returns `std::cerr`
[x] `src/command_lint.cc` warning uses `LOG_WARNING()` with only its message
[x] `src/command_validate.cc` pre-compiled-template warning uses `LOG_WARNING()`
[x] `src/command_validate.cc` empty-JSONL warning uses `LOG_WARNING()`
[x] `src/resolver.h` multi-line warning goes through one `LOG_WARNING()` chain
[x] `src/command_bundle.cc` multi-line warning goes through one `LOG_WARNING()` chain
[x] `src/command_bundle.cc` includes `logger.h` in the existing include block
[x] No source hand-spells a `warning: ` prefix outside `LOG_WARNING()`
[x] `test/validate/pass_jsonl_empty.sh` expects the now-unconditional warning (already registered)
[x] `make configure compile` succeeds (clang-format and shellcheck targets clean)
[x] `ctest --test-dir build -R 'pass_jsonl_empty'` passes; full ctest suite passes
