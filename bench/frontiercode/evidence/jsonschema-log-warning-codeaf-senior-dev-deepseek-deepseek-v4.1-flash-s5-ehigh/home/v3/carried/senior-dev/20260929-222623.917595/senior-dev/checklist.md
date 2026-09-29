[x] Add `LOG_WARNING()` to `src/logger.h` next to `LOG_VERBOSE`, declared `inline auto ... -> std::ostream &`
[x] `LOG_WARNING()` always writes to `std::cerr`, regardless of `--verbose` or any other option
[x] `LOG_WARNING()` writes the `warning: ` prefix itself, so call sites spell only their message
[x] Convert the `src/command_lint.cc` warning to `LOG_WARNING()`
[x] Convert both `src/command_validate.cc` warnings to `LOG_WARNING()` (pre-compiled template and empty JSONL)
[x] Convert the multi-line `src/resolver.h` warning, every continuation statement included
[x] Convert the multi-line `src/command_bundle.cc` warning, every continuation statement included
[x] Add the `logger.h` include to `command_bundle.cc` in the existing include block
[x] Update affected shell tests (`pass_jsonl_empty`, `pass_without_id`, `pass_without_id_verbose`) to the new stderr
[x] Build and shell tests pass, clang-format and shellcheck clean
