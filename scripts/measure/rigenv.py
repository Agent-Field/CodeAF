"""The one place the cross-computer rigs read their settings from.

A rig names machines, folders and a relay that differ for every person who runs it, so none of
them is written into a script. Each comes from an environment variable with a documented name
(docs/testing-anywhere.md), read here, and a missing one stops the run with a message that names
the variable and says what it is for.

    CODEAF_RELAY         the relay under test, for example https://relay.example.com
    CODEAF_SECOND_HOST   the second machine, reachable with `ssh $CODEAF_SECOND_HOST` and no prompt
    CODEAF_SECOND_ROOT   an absolute folder on the second machine that the rig may fill and wipe
    CODEAF_FIRST_ROOT    the same on this machine (default: ~/caf-rig)
    CODEAF_CORPUS        a folder holding the corpus repositories (r18-pareto-c365, r02-mj-base, ...)
    CODEAF_RIG_BIN       a folder holding the built programs (codeaf, codeaf-vd, s1probe)
    CODEAF_EVIDENCE      where a rig writes its result folders (default: ~/caf-evidence)
"""
import os
import sys

HOME = os.path.expanduser("~")

# Why one table: a setting's name, its meaning and its default live together, so the message that
# names a missing variable can never disagree with the list in the documentation.
SETTINGS = {
    "CODEAF_RELAY": ("the relay under test, for example https://relay.example.com", None),
    "CODEAF_SECOND_HOST": ("the second machine, reachable with `ssh <host>` and no password prompt", None),
    "CODEAF_SECOND_ROOT": ("an absolute folder on the second machine that the rig may fill and wipe", None),
    "CODEAF_FIRST_ROOT": ("an absolute folder on this machine that the rig may fill and wipe", os.path.join(HOME, "caf-rig")),
    "CODEAF_CORPUS": ("a folder holding the corpus repositories (r18-pareto-c365, r02-mj-base, r06-agentfield)", None),
    "CODEAF_RIG_BIN": ("a folder holding the built programs (codeaf, codeaf-vd, s1probe)", None),
    "CODEAF_EVIDENCE": ("the folder a rig writes its result folders into", os.path.join(HOME, "caf-evidence")),
}


def get(name):
    """The value of a setting, or the exit that names it when it has neither a value nor a default."""
    why, default = SETTINGS[name]
    value = os.environ.get(name) or default
    if value is None:
        sys.exit(f"{name} is not set: it is {why}. Export it and run again (docs/testing-anywhere.md lists every setting).")
    return value


def optional(name, default):
    """A setting a rig can do without, with the value to use when it is absent."""
    return os.environ.get(name) or default
