#!/bin/sh
# clean-run.sh opens bin/codeaf as though it had never run on this machine,
# except that it still knows who you are: your settings (models, crew,
# services) and your keys are copied into a fresh state root, and nothing else
# is. No conversation, project, task, memory, standing order or notice from
# ~/.codeaf is there, so every try of a new build starts from the same place.
#
# The state root is moved with CODEAF_HOME (internal/home), which moves every
# file codeaf writes and leaves HOME alone, so git, your shell and caches
# outside codeaf behave exactly as they do every day. The copy is left behind
# after codeaf exits, and its path is printed, so a run can be looked at later.
#
#   scripts/clean-run.sh [codeaf arguments...]
#   CLEAN_FROM=~/.codeaf   where the settings and keys are copied from
#   CLEAN_INTO=<dir>       use this folder instead of a fresh one (emptied first)
#   CODEAF_BIN=<path>      the binary to open (default: bin/codeaf beside this script)
set -eu

here=$(cd "$(dirname "$0")/.." && pwd)
from=${CLEAN_FROM:-$HOME/.codeaf}
bin=${CODEAF_BIN:-$here/bin/codeaf}

if [ ! -x "$bin" ]; then
	echo "clean-run: no binary at $bin; run make build first" >&2
	exit 1
fi
if [ -n "${CLEAN_INTO:-}" ]; then
	into=$CLEAN_INTO
	rm -rf "$into"
	mkdir -p "$into"
else
	into=$(mktemp -d "${TMPDIR:-/tmp}/codeaf-clean.XXXXXX")
fi

# WHAT MAKES IT YOURS, AND NOTHING THAT MAKES IT USED: the settings file, the
# keys, the model services you connected, the tool servers you added, and the
# model catalog cache (a copy only saves the first launch a fetch). Every
# conversation, project and ledger lives elsewhere under the root and stays out.
for name in config.json credentials.json connections.json toolservers.json model-catalog.json model-quirks.json; do
	if [ -f "$from/$name" ]; then
		cp -p "$from/$name" "$into/$name"
	fi
done
chmod 700 "$into"

echo "clean-run: codeaf state in $into (settings and keys from $from)" >&2
status=0
CODEAF_HOME=$into "$bin" "$@" || status=$?
echo "clean-run: that run's state is kept in $into" >&2
exit $status
