[x] Add `inline auto LOG_WARNING() -> std::ostream &` to `src/logger.h`, always writing to `std::cerr` with its own `warning: ` prefix, regardless of `--verbose`
[x] Convert the warning in `src/command_lint.cc` to `LOG_WARNING()`
[x] Convert both warnings in `src/command_validate.cc` (pre-compiled template and empty JSONL) to `LOG_WARNING()`
[x] Convert the multi-line warning in `src/resolver.h` down to its last statement, routing every continuation line through `LOG_WARNING()`
[x] Convert the multi-line warning in `src/command_bundle.cc` down to its last statement, routing every continuation line through `LOG_WARNING()`
[x] Include `logger.h` where newly needed (`src/command_bundle.cc`) grouping it with the existing include block
[x] Update the affected shell tests' expected stderr (`test/validate/pass_jsonl_empty.sh`, `test/bundle/pass_without_id.sh`, `test/bundle/pass_without_id_verbose.sh`), keeping them registered as other shell tests are
[x] Configure and compile cleanly with `make configure compile` (clang-format and shellcheck clean)
[x] Keep the targeted warning tests and the full ctest suite passing
