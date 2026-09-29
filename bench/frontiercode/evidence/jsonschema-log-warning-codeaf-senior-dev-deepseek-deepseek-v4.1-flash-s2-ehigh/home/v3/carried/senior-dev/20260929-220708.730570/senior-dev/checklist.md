[x] Add LOG_WARNING() helper to src/logger.h returning std::ostream &, always writing to std::cerr and emitting the "warning: " prefix itself
[x] Convert warning in src/command_lint.cc to LOG_WARNING()
[x] Convert both warning sites in src/command_validate.cc to LOG_WARNING()
[x] Convert warning in src/resolver.h (multi-line) to LOG_WARNING()
[x] Convert multi-line warning in src/command_bundle.cc to LOG_WARNING() and include logger.h
[x] Ensure no remaining ad-hoc "warning: " std::cerr sites in src/
[x] Update pass_jsonl_empty test for the now-unconditional warning and keep it registered
[x] Build via make configure compile (clang-format, shellcheck) cleanly
[x] Run ctest -R 'pass_jsonl_empty' and relevant warning tests passing
