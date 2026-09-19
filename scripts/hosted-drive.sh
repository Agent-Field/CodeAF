#!/bin/bash
# hosted-drive.sh drives a run on the real binary, hosted, the way a person
# would: it types into `codeaf chat` in a terminal of its own and reads the
# screen and the folder back. A surface change is accepted on this, never on a
# test with a stand-in agent: what a stand-in proves is what the stand-in does.
#
#   scripts/hosted-drive.sh <path to bin/codeaf> [folder to keep failed screens in]
#
# IT MAKES REAL MODEL CALLS, so it is never part of `make check` or any default
# target. Run it by hand, after `make build`, when a change touches the run, the
# side list of tasks or a task's page.
#
# WHAT IT NEEDS: tmux, git, python3, and a key the product can find, either in
# the environment or in your profile's config file. Everything it makes is
# thrown away: a home of its own, a repository of its own, and a tmux server on
# a socket of its own. It never writes to your own home folder's state.
#
# YOUR KEY: the drive copies your profile's key into a private temporary home,
# readable by you alone, and removes that copy on every exit: a pass, a failed
# check, an interrupt. Nothing else the product writes there carries the key.
# A screen kept from a failed check has any key the drive knows struck out. It
# knows a key by a field whose name says so, or by an environment variable that
# ends in _API_KEY. A credential kept under any other name it cannot know, so
# kept screens are private files: read them before you share them.
#
# WHAT IT ENDS, and how it chooses: its own tmux server, by its own socket's
# name; and the engine the chat started for this home, found as the process
# holding THIS home's own socket and ended only when its executable is the
# binary you named. It selects no process by the text of a command line. On a
# system with neither `lsof` nor a table of sockets to read, the engine is left
# alone: it leaves by itself once it has been idle, or you can end it.
#
# WHAT IT ASSERTS, in the order it happens. It exits with the number that failed.
#
#  1. Two tasks are handed off six seconds apart, with an unsaved file of your
#     own in the folder.
#     - While the run works, your folder holds only your own file.
#     - The run works in one copy of its own, kept under the conversation.
#     - The second hand-off joins the first and shows on the side list.
#  2. The run ends and its work comes home.
#     - One new commit holds the run's files.
#     - Your unsaved file is untouched and still unsaved.
#     - The run's copy is given back.
#     - No task branch is left behind.
#     - The landing card says merged, and never that a branch was kept.
#     - No row on the side list is still running.
#  3. A task handed off after the run has ended is a run of its own, and the
#     first run stays readable.
#     - The first run's record is kept beside the live one.
#     - A press on the first run's row opens that task's page over the
#       conversation, with the task as it was given.
#     - No id of the record's own is drawn on the page.
#     - A note left on the ended run is answered in words, not dropped.
#     - esc returns to the conversation with your draft as you left it.
#
# The checks below are small functions handed BY NAME to `check` and
# `wait_for`, which is a call the linter cannot see.
# shellcheck disable=SC2329
set -u

BIN=${1:?usage: scripts/hosted-drive.sh <path to bin/codeaf> [folder for failed screens]}
[ -x "$BIN" ] || { echo "no binary at $BIN: run make build first"; exit 2; }
for tool in tmux git python3; do
	command -v "$tool" >/dev/null || { echo "this drive needs $tool"; exit 2; }
done
BIN=$(cd "$(dirname "$BIN")" && pwd -P)/$(basename "$BIN")
umask 077
OUT=${2:-$(mktemp -d /tmp/codeaf-drive-screens.XXXXXX)}
mkdir -p "$OUT"
SOCK=codeaf-drive-$$
HOME_DIR=$(mktemp -d /tmp/codeaf-drive-home.XXXXXX)
WORK=$(mktemp -d /tmp/codeaf-drive-work.XXXXXX)
# mktemp -d makes a folder only its owner can enter on every system this runs
# on; said again here because a key is about to be copied into it.
chmod 700 "$HOME_DIR"
fails=0
passed=no

say() { printf '%s\n' "$*"; }
t() { tmux -L "$SOCK" "$@"; }

# screen prints the pane with private-use glyphs shown as *, so a needle is text.
screen() {
	t capture-pane -p -t x | python3 -c "
import sys
sys.stdout.write(''.join('*' if 0xe000 <= ord(c) <= 0xf8ff else c for c in sys.stdin.read()))"
}

# strike_keys removes every key the drive knows from what passes through it:
# the ones in the copied profile and the ones in the environment. The drive
# opens no page that draws a key; this is for the day some page does.
strike_keys() {
	python3 -c "
import json, os, sys
known = set()
def walk(node):
    if isinstance(node, dict):
        for name, value in node.items():
            if isinstance(value, str) and 'key' in name.lower() and len(value) > 8:
                known.add(value)
            else:
                walk(value)
    elif isinstance(node, list):
        for value in node:
            walk(value)
try:
    walk(json.load(open(sys.argv[1])))
except Exception:
    pass
for name, value in os.environ.items():
    if name.endswith('_API_KEY') and len(value) > 8:
        known.add(value)
text = sys.stdin.read()
for value in known:
    text = text.replace(value, '[key struck out]')
sys.stdout.write(text)" "$HOME_DIR/config.json"
}

pass() { say "  PASS  $*"; }

# A failed check keeps the screen it judged, so the reader sees what was there.
fail() {
	fails=$((fails + 1))
	say "  FAIL  $*"
	screen | strike_keys >"$OUT/fail-$fails.txt" 2>/dev/null
	say "        the screen it judged is kept at $OUT/fail-$fails.txt"
}

# check <what it asserts> <a function that answers it>
check() {
	local what=$1
	shift
	if "$@"; then pass "$what"; else fail "$what"; fi
}

typed() {
	t send-keys -t x -l "$1"
	sleep 1
	t send-keys -t x Enter
}

# press clicks the side list's row whose text holds the needle, at the needle's
# own cell. The side list is the right-hand column, so a match left of column
# 100 is the conversation quoting the same words and is passed over.
press() {
	local spot x y
	spot=$(screen | python3 -c "
import sys
for i, line in enumerate(sys.stdin.read().split('\n')):
    at = line.rfind(sys.argv[1])
    if at > 100:
        print(at + 1, i + 1)
        break
" "$1")
	[ -n "$spot" ] || return 1
	x=${spot% *}
	y=${spot#* }
	t send-keys -t x -l $'\e[<0;'"$x;$y"'M'
	t send-keys -t x -l $'\e[<0;'"$x;$y"'m'
}

# wait_for <tries> <seconds between> <a function that answers>
wait_for() {
	local tries=$1 gap=$2 i
	shift 2
	for ((i = 0; i < tries; i++)); do
		"$@" && return 0
		sleep "$gap"
	done
	return 1
}

# exe_of is the executable a pid is running, asked of the system and never
# read out of a command line.
exe_of() {
	if [ -e "/proc/$1/exe" ]; then
		readlink -f "/proc/$1/exe"
	else
		lsof -p "$1" -a -d txt -Fn 2>/dev/null | sed -n 's/^n//p' | head -1
	fi
}

# end_engine ends the engine the chat started for THIS home. It is found as the
# holder of this home's own socket, and ended only when its executable is the
# binary this drive was given: whole-path equality, one pid at a time.
end_engine() {
	local sock pid
	for sock in "$HOME_DIR"/v3/hosts/*/host.sock; do
		[ -S "$sock" ] || continue
		for pid in $(holders_of "$sock"); do
			[ "$(exe_of "$pid")" = "$BIN" ] && kill "$pid" 2>/dev/null
		done
	done
	return 0
}

# holders_of is every pid holding one socket, by the socket's whole path: asked
# of `lsof` where there is one, and otherwise read from the system's own table
# of sockets and each process's open files.
holders_of() {
	local inode entry
	if command -v lsof >/dev/null; then
		lsof -t -- "$1" 2>/dev/null
		return 0
	fi
	[ -r /proc/net/unix ] || return 0
	awk -v sock="$1" '$NF == sock { print $7 }' /proc/net/unix | while read -r inode; do
		for entry in /proc/[0-9]*; do
			find "$entry/fd" -maxdepth 1 -lname "socket:\[$inode\]" 2>/dev/null | grep -q . &&
				echo "${entry#/proc/}"
		done
	done
	return 0
}

# cleanup runs on EVERY exit. The key's copy goes first and goes always; the
# rest of the home may stay for reading when a check failed.
#
# THE ENGINE IS ENDED BEFORE ANYTHING IS REMOVED, because it is found through a
# socket that lives in the home. A clean pass then removes what the drive made,
# by the names it made them under and nothing else.
cleanup() {
	rm -f "$HOME_DIR/config.json"
	t kill-server 2>/dev/null
	end_engine
	if [ "$passed" = "yes" ]; then
		case "$HOME_DIR" in /tmp/codeaf-drive-home.*) rm -rf "$HOME_DIR" ;; esac
		case "$WORK" in /tmp/codeaf-drive-work.*) rm -rf "$WORK" ;; esac
	fi
}
trap cleanup EXIT
trap 'exit 130' INT TERM

copies() { ls -d "$HOME_DIR"/v3/projects/*/*/trees/* 2>/dev/null; }

only_my_file() { [ "$(git -C "$WORK" status --short)" = "?? notes.txt" ]; }
one_copy() { [ "$(copies | wc -l | tr -d ' ')" = "1" ]; }
second_on_the_list() { screen | grep -q '#2'; }
first_run_home() { [ -f "$WORK/README.md" ] && [ -f "$WORK/muldiv.go" ]; }
one_new_commit() { [ "$(git -C "$WORK" log --oneline | wc -l | tr -d ' ')" = "2" ]; }
my_file_untouched() { [ "$(cat "$WORK/notes.txt")" = "mine, unsaved" ] && only_my_file; }
copy_given_back() { [ -z "$(copies)" ]; }
no_task_branch() { [ -z "$(git -C "$WORK" branch --list 'task/*')" ]; }
card_says_merged() { screen | grep -q '· merged' && ! screen | grep -q 'branch kept'; }
nothing_running() { ! screen | cut -c138- | grep -q '[⠀-⣿]'; }
second_run_home() { grep -q 'func Neg' "$WORK"/*.go 2>/dev/null; }
first_record_kept() { ls "$HOME_DIR"/v3/projects/*/*/plandb.db.1 >/dev/null 2>&1; }
page_drawn() { screen | grep -q '^ brief'; }
page_is_the_first_runs() { page_drawn && screen | grep -q 'add Mul and Div'; }
no_record_id() { ! screen | grep -Eq '(^| )t-[a-z0-9]{6}( |$)|^ [0-9]+ · '; }
ended_run_answers() { screen | grep -q "that task.s run has ended"; }
draft_is_back() { screen | grep -q '› my draft stays'; }

profile=${CODEAF_HOME:-$HOME/.codeaf}/config.json
[ -f "$profile" ] && cp "$profile" "$HOME_DIR/config.json"
(
	cd "$WORK" || exit 1
	git init -q -b work
	printf 'package calc\n\nfunc Add(a, b int) int { return a + b }\n' >calc.go
	printf 'module calc\n\ngo 1.22\n' >go.mod
	git add calc.go go.mod
	git -c user.name=drive -c user.email=drive@example.invalid commit -qm seed
	echo 'mine, unsaved' >notes.txt
)
say "home $HOME_DIR   folder $WORK"
t new-session -d -s x -x 170 -y 50 \
	"cd '$WORK' && CODEAF_HOME='$HOME_DIR' CODEAF_TASK_BELT=bash CODEAF_TELEMETRY=off '$BIN' chat; sleep 3600"
sleep 8
# A home with no profile opens first-run setup; esc leaves it for the conversation.
screen | grep -q 'Daily limit' && {
	t send-keys -t x Escape
	sleep 3
}

say "1. two hand-offs, six seconds apart, with an unsaved file of your own in the folder"
typed '/task add Mul and Div with table tests in muldiv.go and muldiv_test.go'
sleep 6
typed '/task write README.md documenting every exported function with one example each'
sleep 15
check "while the run works the folder holds only your own file" only_my_file
check "the run works in one copy of its own under the conversation" one_copy
check "the second hand-off shows on the side list" second_on_the_list

say "2. the run ends and its work comes home"
wait_for 60 5 first_run_home || fail "the run did not end inside five minutes"
sleep 12
check "one new commit holds the run's files" one_new_commit
check "your unsaved file is untouched" my_file_untouched
check "the copy is given back" copy_given_back
check "no task branch is left behind" no_task_branch
check "the card says merged and never branch kept" card_says_merged
check "no row is still running" nothing_running

say "3. a hand-off after the run has ended is a run of its own, and the first stays readable"
typed '/task add a Neg function with a table test'
wait_for 60 5 second_run_home || fail "the second run did not end inside five minutes"
sleep 12
check "the first run's record was kept beside the live one" first_record_kept
t send-keys -t x -l 'my draft stays'
sleep 1
# AN ENDED RUN'S ROW FOLDS ITS PARTS AWAY AND WEARS THE FOLD'S COUNT WHERE ITS
# NUMBER WAS, so the row is found by its title and not by `#1`.
press 'add Mul and Div' || fail "the first run's row is not on the side list"
# The page is read off the update loop, so it is drawn a moment after the press.
wait_for 100 0.1 page_drawn || fail "the page did not draw inside ten seconds"
check "a press on the first run's row opens its page" page_is_the_first_runs
check "no id of the record's own is on the page" no_record_id
typed 'one more thing'
sleep 3
check "a note on an ended run is answered in words" ended_run_answers
for _ in $(seq 1 14); do t send-keys -t x BSpace; done
t send-keys -t x Escape
sleep 2
check "esc returns to the conversation with your draft as you left it" draft_is_back

say ""
if [ "$fails" -eq 0 ]; then
	say "ALL PASS"
	passed=yes
else
	say "$fails FAILED: home $HOME_DIR and folder $WORK are left for reading"
fi
exit "$fails"
